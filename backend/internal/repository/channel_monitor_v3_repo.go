package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
)

type channelMonitorV3Repository struct{ db *sql.DB }

func NewChannelMonitorV3Repository(db *sql.DB) service.ChannelMonitorV3Repository {
	return &channelMonitorV3Repository{db: db}
}

func (r *channelMonitorV3Repository) GetConfig(ctx context.Context) (*service.ChannelMonitorV3Config, error) {
	return readV3Config(ctx, r.db, "")
}

type v3StoredConfig struct {
	service.ChannelMonitorV3Config
	ObservationStarts map[string]time.Time `json:"observation_starts,omitempty"`
}

func readV3Config(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, lock string) (*service.ChannelMonitorV3Config, error) {
	stored := v3StoredConfig{ChannelMonitorV3Config: service.DefaultChannelMonitorV3Config()}
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT version, config FROM channel_monitor_v3_config WHERE id=1`+lock).Scan(&stored.Version, &raw); err != nil {
		return nil, err
	}
	version := stored.Version
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	stored.Version = version
	stored.ChannelMonitorV3Config.ObservationStarts = stored.ObservationStarts
	return &stored.ChannelMonitorV3Config, stored.Validate()
}

func (r *channelMonitorV3Repository) UpdateConfig(ctx context.Context, cfg service.ChannelMonitorV3Config) (*service.ChannelMonitorV3Config, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	previous, err := readV3Config(ctx, tx, " FOR UPDATE")
	if err != nil {
		return nil, err
	}
	if cfg.Version != previous.Version {
		return nil, service.ErrChannelMonitorV3Conflict
	}
	// This lock waits for any in-flight aggregation before acknowledging a stop.
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	cfg.ObservationStarts = previous.ObservationStarts
	if cfg.ObservationStarts == nil {
		cfg.ObservationStarts = map[string]time.Time{}
	}
	resumed := []string{}
	for _, platform := range cfg.EnabledPlatforms() {
		if !previous.PlatformEnabled(platform) {
			cfg.ObservationStarts[platform] = now.UTC().Truncate(time.Minute).Add(time.Minute)
			resumed = append(resumed, platform)
		}
	}
	if len(resumed) > 0 {
		states, readErr := readV3States(ctx, tx, resumed)
		if readErr != nil {
			return nil, readErr
		}
		for _, item := range states {
			next := service.ResumeChannelMonitorV3State(item.state, now)
			if err = writeV3State(ctx, tx, item.fact, next, next.Incident); err != nil {
				return nil, err
			}
		}
	}
	if cfg.DisabledPlatforms == nil {
		cfg.DisabledPlatforms = []string{}
	}
	sort.Strings(cfg.DisabledPlatforms)
	raw, err := json.Marshal(v3StoredConfig{ChannelMonitorV3Config: cfg, ObservationStarts: cfg.ObservationStarts})
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `UPDATE channel_monitor_v3_config SET config=$1, version=version+1, updated_at=$2 WHERE id=1 RETURNING version`, string(raw), now).Scan(&cfg.Version)
	if err != nil {
		return nil, err
	}
	return &cfg, tx.Commit()
}

func v3ObservationScope(cfg service.ChannelMonitorV3Config, filter string) (string, error) {
	type scope struct {
		Platform string    `json:"platform"`
		Since    time.Time `json:"since"`
	}
	scopes := []scope{}
	for _, platform := range cfg.EnabledPlatforms() {
		if filter == "" || platform == filter {
			scopes = append(scopes, scope{platform, cfg.ObservationStarts[platform]})
		}
	}
	raw, err := json.Marshal(scopes)
	return string(raw), err
}

// Terminal usage rows override intermediate retry errors for the same request.
// No monetary or token field is used as evidence of success. Explicit client
// disconnects override retry errors too, then leave the eligible denominator.
const channelMonitorV3OutcomesSQL = `
WITH candidate_ids AS MATERIALIZED (
 SELECT request_id FROM usage_logs WHERE created_at >= $1 AND created_at < $2 AND NULLIF(request_id,'') IS NOT NULL
 UNION SELECT request_id FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2 AND NULLIF(request_id,'') IS NOT NULL
), outcomes AS (
 SELECT COALESCE(NULLIF(ul.request_id,''),'usage:'||ul.id::text) AS request_key,
        ul.id, 2 AS priority, ul.created_at,
        lower(COALESCE(` + usageLogEffectivePlatformExpr + `,'unknown')) AS platform,
        COALESCE(ul.group_id,0) AS group_id,
        COALESCE(NULLIF(ul.requested_model,''),NULLIF(ul.model,''),'unknown') AS model,
        CASE WHEN ul.completion_status='completed' OR (ul.completion_status='unknown' AND ul.is_complete IS TRUE) THEN 'success'
             WHEN ul.completion_status='incomplete' OR (ul.completion_status='unknown' AND ul.is_complete IS FALSE) THEN 'failure'
             ELSE 'ignored' END AS outcome,
        CASE WHEN ul.timing_version=1 THEN ul.first_token_ms END AS ttft,
        '' AS error_type, '' AS error_owner, '' AS error_source,
        0 AS status_code, 0 AS upstream_status_code, '' AS message
 FROM usage_logs ul LEFT JOIN groups g ON g.id=ul.group_id LEFT JOIN accounts a ON a.id=ul.account_id
 WHERE ul.created_at >= $1 - INTERVAL '90 minutes' AND ul.created_at < $2
   AND (ul.created_at >= $1 OR ul.request_id IN (SELECT request_id FROM candidate_ids))
   AND COALESCE(ul.request_type,0) <> 4
   AND (ul.completion_status IN ('completed','incomplete','client_disconnected') OR ul.is_complete IS NOT NULL)
 UNION ALL
 SELECT COALESCE(NULLIF(e.request_id,''),'error:'||e.id::text),e.id,1,e.created_at,
        lower(CASE WHEN g.platform='composite' THEN COALESCE(NULLIF(a.platform,''),NULLIF(NULLIF(e.platform,''),'composite'),'unknown')
              ELSE COALESCE(NULLIF(g.platform,''),NULLIF(e.platform,''),NULLIF(a.platform,''),'unknown') END),
        COALESCE(e.group_id,0),COALESCE(NULLIF(e.requested_model,''),NULLIF(e.model,''),'unknown'),
        'failure',NULL,COALESCE(e.error_type,''),COALESCE(e.error_owner,''),COALESCE(e.error_source,''),
        COALESCE(e.status_code,0),COALESCE(e.upstream_status_code,0),
        LEFT(CONCAT_WS(' ',e.error_message,e.upstream_error_message),600)
 FROM ops_error_logs e LEFT JOIN groups g ON g.id=e.group_id LEFT JOIN accounts a ON a.id=e.account_id
 WHERE e.created_at >= $1 - INTERVAL '90 minutes' AND e.created_at < $2
   AND (e.created_at >= $1 OR e.request_id IN (SELECT request_id FROM candidate_ids))
   AND NOT e.is_count_tokens AND COALESCE(e.request_type,0) <> 4
   AND (e.status_code >= 400 OR e.upstream_status_code >= 400)
), terminal AS (
 SELECT DISTINCT ON (request_key) * FROM outcomes ORDER BY request_key,priority DESC,created_at DESC,id DESC
), last_errors AS (
 SELECT DISTINCT ON (request_key) * FROM outcomes WHERE priority=1 ORDER BY request_key,created_at DESC,id DESC
)
SELECT t.created_at,t.platform,t.group_id,t.model,t.outcome,t.ttft,
       COALESCE(NULLIF(e.error_type,''),'stream_read_error'),COALESCE(e.error_owner,'platform'),COALESCE(e.error_source,''),
       COALESCE(e.status_code,502),COALESCE(e.upstream_status_code,0),COALESCE(e.message,'missing terminal event')
FROM terminal t LEFT JOIN last_errors e ON e.request_key=t.request_key
JOIN jsonb_to_recordset($3::jsonb) AS scope(platform text,since timestamptz)
 ON scope.platform=t.platform AND t.created_at >= scope.since
WHERE t.created_at >= $1 AND t.created_at < $2 AND t.outcome <> 'ignored'
ORDER BY t.created_at LIMIT 50001`

func (r *channelMonitorV3Repository) Refresh(ctx context.Context, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(724913603)`).Scan(&locked); err != nil || !locked {
		return err
	}
	cfg, err := readV3Config(ctx, tx, " FOR SHARE")
	if err != nil {
		return err
	}
	scope, err := v3ObservationScope(*cfg, "")
	if err != nil {
		return err
	}
	var through sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT data_through FROM channel_monitor_v3_watermark WHERE id=1 FOR UPDATE`).Scan(&through); err != nil {
		return err
	}
	if through.Valid && !now.After(through.Time) {
		return nil
	}
	start := now.Add(-10 * time.Minute)
	facts := map[string]*service.ChannelMonitorV3Fact{}
	if len(cfg.EnabledPlatforms()) > 0 {
		rows, err := tx.QueryContext(ctx, channelMonitorV3OutcomesSQL, start, now, scope)
		if err != nil {
			return err
		}
		count := 0
		for rows.Next() {
			var fact service.ChannelMonitorV3Fact
			var outcome string
			var latency sql.NullInt64
			var input service.ChannelMonitorV2ErrorInput
			if err = rows.Scan(&fact.LastRequest, &fact.Platform, &fact.GroupID, &fact.Model, &outcome, &latency, &input.ErrorType, &input.ErrorOwner, &input.ErrorSource, &input.StatusCode, &input.UpstreamStatusCode, &input.Message); err != nil {
				_ = rows.Close()
				return err
			}
			count++
			if count > 50000 {
				_ = rows.Close()
				return fmt.Errorf("service status observation limit exceeded")
			}
			if fact.GroupID <= 0 || fact.Platform == "unknown" || fact.Platform == "composite" {
				continue
			}
			if outcome == "failure" && !service.ChannelMonitorV3EligibleError(input) {
				continue
			}
			fact.Bucket = fact.LastRequest.UTC().Truncate(time.Minute)
			key := fmt.Sprintf("%d:%s", fact.Bucket.Unix(), service.ChannelMonitorV3Scope(fact.Platform, fact.GroupID, fact.Model))
			merged := facts[key]
			if merged == nil {
				merged = &service.ChannelMonitorV3Fact{Bucket: fact.Bucket, Platform: fact.Platform, GroupID: fact.GroupID, Model: fact.Model}
				facts[key] = merged
			}
			if outcome == "success" {
				merged.Success++
				if latency.Valid && latency.Int64 >= 0 {
					for i, bound := range service.ChannelMonitorV3LatencyBounds {
						if latency.Int64 <= bound {
							merged.Latency[i]++
							break
						}
					}
				}
			} else {
				merged.Failures++
			}
			if fact.LastRequest.After(merged.LastRequest) {
				merged.LastRequest = fact.LastRequest
			}
		}
		if err = rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_monitor_v3_facts f
 USING jsonb_to_recordset($3::jsonb) AS scope(platform text,since timestamptz)
 WHERE f.platform=scope.platform AND f.bucket_start >= GREATEST($1,scope.since) AND f.bucket_start < $2`, start, now, scope); err != nil {
		return err
	}
	for _, fact := range facts {
		if _, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v3_facts VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, fact.Bucket, fact.Platform, fact.GroupID, fact.Model, fact.Success, fact.Failures, fact.LastRequest, pq.Array(fact.Latency[:])); err != nil {
			return err
		}
	}
	states, err := readV3States(ctx, tx, cfg.EnabledPlatforms())
	if err != nil {
		return err
	}
	recent := map[string]service.ChannelMonitorV3Fact{}
	for _, fact := range facts {
		if fact.Bucket.Before(now.Add(-5 * time.Minute)) {
			continue
		}
		key := service.ChannelMonitorV3Scope(fact.Platform, fact.GroupID, fact.Model)
		value, ok := recent[key]
		if !ok {
			value = service.ChannelMonitorV3Fact{Platform: fact.Platform, GroupID: fact.GroupID, Model: fact.Model}
		}
		value.Merge(*fact)
		recent[key] = value
	}
	for key, item := range states {
		if _, ok := recent[key]; !ok {
			recent[key] = item.fact
		}
	}
	for key, fact := range recent {
		next, incident := service.AdvanceChannelMonitorV3State(states[key].state, fact, now, *cfg)
		if err = writeV3State(ctx, tx, fact, next, incident); err != nil {
			return err
		}
	}
	cutoff := now.Add(-31 * 24 * time.Hour)
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_monitor_v3_facts WHERE bucket_start < $1`, cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_monitor_v3_incidents WHERE resolved_at < $1`, cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM channel_monitor_v3_states WHERE data->'incident' IS NULL AND (data->>'last_request')::timestamptz < $1`, cutoff); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE channel_monitor_v3_watermark SET data_through=$1 WHERE id=1`, now); err != nil {
		return err
	}
	return tx.Commit()
}

type v3Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}
type v3StoredState struct {
	fact  service.ChannelMonitorV3Fact
	state service.ChannelMonitorV3State
}

func readV3States(ctx context.Context, db v3Queryer, platforms []string) (map[string]v3StoredState, error) {
	query := `SELECT platform,group_id,model,data FROM channel_monitor_v3_states WHERE platform=ANY($1)`
	rows, err := db.QueryContext(ctx, query, pq.Array(platforms))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := map[string]v3StoredState{}
	for rows.Next() {
		var item v3StoredState
		var raw []byte
		if err = rows.Scan(&item.fact.Platform, &item.fact.GroupID, &item.fact.Model, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item.state); err != nil {
			return nil, err
		}
		result[service.ChannelMonitorV3Scope(item.fact.Platform, item.fact.GroupID, item.fact.Model)] = item
	}
	return result, rows.Err()
}

func writeV3State(ctx context.Context, tx *sql.Tx, fact service.ChannelMonitorV3Fact, state service.ChannelMonitorV3State, incident *service.ChannelMonitorV3Incident) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v3_states VALUES ($1,$2,$3,$4) ON CONFLICT (platform,group_id,model) DO UPDATE SET data=EXCLUDED.data`, fact.Platform, fact.GroupID, fact.Model, string(raw)); err != nil {
		return err
	}
	if incident == nil {
		return nil
	}
	raw, err = json.Marshal(incident)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v3_incidents VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (id) DO UPDATE SET data=EXCLUDED.data,resolved_at=EXCLUDED.resolved_at`, incident.ID, incident.Platform, incident.GroupID, incident.Model, incident.StartedAt, incident.ResolvedAt, string(raw))
	return err
}

func v3AggregateColumns() string {
	parts := make([]string, 16)
	for i := range parts {
		parts[i] = fmt.Sprintf("COALESCE(SUM(f.ttft_counts[%d]),0)", i+1)
	}
	return `SUM(f.success_requests),SUM(f.failed_requests),MAX(f.last_request_at),ARRAY[` + strings.Join(parts, ",") + `]`
}

func (r *channelMonitorV3Repository) readFacts(ctx context.Context, db v3Queryer, start, end time.Time, bucket time.Duration, models bool, scope string) ([]service.ChannelMonitorV3Fact, error) {
	groupExpr := "0::bigint,''::text,''::text"
	groupBy := "1,2"
	if models {
		groupExpr = "f.group_id,COALESCE(g.name,''),f.model"
		groupBy += ",3,4,5"
	}
	observationFilter := ""
	if models {
		observationFilter = " AND f.bucket_start >= scope.since"
	}
	query := `SELECT date_bin($3::interval,f.bucket_start,$1::timestamptz),f.platform,` + groupExpr + `,` + v3AggregateColumns() + `
 FROM channel_monitor_v3_facts f JOIN groups g ON g.id=f.group_id AND g.deleted_at IS NULL AND g.status='active'
 JOIN jsonb_to_recordset($4::jsonb) AS scope(platform text,since timestamptz) ON scope.platform=f.platform
 WHERE f.bucket_start >= $1 AND f.bucket_start < $2` + observationFilter + ` GROUP BY ` + groupBy + ` ORDER BY 1,2,3,5`
	rows, err := db.QueryContext(ctx, query, start, end, fmt.Sprintf("%d seconds", int64(bucket.Seconds())), scope)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []service.ChannelMonitorV3Fact{}
	for rows.Next() {
		var fact service.ChannelMonitorV3Fact
		var hist []int64
		if err = rows.Scan(&fact.Bucket, &fact.Platform, &fact.GroupID, &fact.GroupName, &fact.Model, &fact.Success, &fact.Failures, &fact.LastRequest, pq.Array(&hist)); err != nil {
			return nil, err
		}
		copy(fact.Latency[:], hist)
		result = append(result, fact)
	}
	return result, rows.Err()
}

func (r *channelMonitorV3Repository) Read(ctx context.Context, now time.Time, window time.Duration, filter string) (*service.ChannelMonitorV3Data, error) {
	data := &service.ChannelMonitorV3Data{States: map[string]service.ChannelMonitorV3State{}, Incidents: []service.ChannelMonitorV3Incident{}}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	cfg, err := readV3Config(ctx, tx, "")
	if err != nil {
		return nil, err
	}
	data.Config = *cfg
	scope, err := v3ObservationScope(*cfg, filter)
	if err != nil {
		return nil, err
	}
	platforms := []string{}
	for _, name := range cfg.EnabledPlatforms() {
		if filter == "" || name == filter {
			platforms = append(platforms, name)
		}
	}
	// A transaction-wide snapshot prevents mixing fresh facts and stale state.
	// Refresh commits all facts, states, events and the watermark atomically.
	rows, err := tx.QueryContext(ctx, `WITH catalog(platform,group_id,group_name) AS (
 SELECT DISTINCT lower(CASE WHEN g.platform='composite' THEN a.platform ELSE g.platform END),g.id,g.name
 FROM groups g LEFT JOIN account_groups ag ON ag.group_id=g.id LEFT JOIN accounts a ON a.id=ag.account_id AND a.deleted_at IS NULL
 WHERE g.deleted_at IS NULL AND g.status='active'
 UNION SELECT DISTINCT f.platform,g.id,g.name FROM channel_monitor_v3_facts f JOIN groups g ON g.id=f.group_id
 WHERE g.deleted_at IS NULL AND g.status='active' AND f.bucket_start >= $1
 UNION SELECT DISTINCT i.platform,g.id,g.name FROM channel_monitor_v3_incidents i JOIN groups g ON g.id=i.group_id
 WHERE g.deleted_at IS NULL AND g.status='active' AND (i.resolved_at IS NULL OR i.resolved_at >= $1)
 ) SELECT * FROM catalog WHERE platform=ANY($2)`, now.Add(-31*24*time.Hour), pq.Array(platforms))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var platform sql.NullString
		var item service.ChannelMonitorV3Catalog
		if err = rows.Scan(&platform, &item.GroupID, &item.GroupName); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if platform.Valid && platform.String != "" && platform.String != "composite" && platform.String != "unknown" {
			item.Platform = platform.String
			data.Catalog = append(data.Catalog, item)
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	end := now.UTC().Truncate(time.Minute)
	if data.Current, err = r.readFacts(ctx, tx, end.Add(-5*time.Minute), end, 5*time.Minute, true, scope); err != nil {
		return nil, err
	}
	if data.History, err = r.readFacts(ctx, tx, end.Add(-window), end, window/48, false, scope); err != nil {
		return nil, err
	}
	data.Totals = data.History
	states, err := readV3States(ctx, tx, platforms)
	if err != nil {
		return nil, err
	}
	for key, item := range states {
		data.States[key] = item.state
		for _, scope := range data.Catalog {
			if scope.Platform == item.fact.Platform && scope.GroupID == item.fact.GroupID {
				item.fact.GroupName = scope.GroupName
				data.KnownModels = append(data.KnownModels, item.fact)
				break
			}
		}
	}
	var through sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT data_through FROM channel_monitor_v3_watermark WHERE id=1`).Scan(&through); err != nil {
		return nil, err
	}
	if through.Valid {
		data.DataThrough = &through.Time
	}
	rows, err = tx.QueryContext(ctx, `WITH visible AS (
 SELECT i.*,g.name AS group_name FROM channel_monitor_v3_incidents i JOIN groups g ON g.id=i.group_id AND g.deleted_at IS NULL AND g.status='active' WHERE i.platform=ANY($2)
 ), selected AS (
 SELECT * FROM visible WHERE resolved_at IS NULL
 UNION ALL (SELECT * FROM visible WHERE resolved_at >= $1 ORDER BY started_at DESC LIMIT 200)
 ) SELECT data,group_name FROM selected ORDER BY resolved_at IS NULL DESC,started_at DESC`, now.Add(-30*24*time.Hour), pq.Array(platforms))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item service.ChannelMonitorV3Incident
		var raw []byte
		var name string
		if err = rows.Scan(&raw, &name); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		item.GroupName = name
		data.Incidents = append(data.Incidents, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	return data, tx.Commit()
}
