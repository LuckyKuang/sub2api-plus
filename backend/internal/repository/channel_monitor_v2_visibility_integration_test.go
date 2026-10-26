//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Each test owns a schema and connection pool, so it can exercise concurrent
// repository queries without shared traffic fixtures or persistent test data.
func channelMonitorV2VisibilityDB(t *testing.T) *sql.DB {
	t.Helper()
	schema := "monitor_visibility_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := integrationDB.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	dsn := integrationPostgresDSN
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		dsn = parsed.String()
	} else {
		dsn += " search_path=" + schema
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
		_, err := integrationDB.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
		require.NoError(t, err)
	})
	for _, table := range []string{
		"channel_monitor_v2_config", "channel_monitor_v2_watermarks",
		"channel_monitor_v2_metrics_1m", "channel_monitor_v2_user_metrics_1m", "channel_monitor_v2_error_metrics_1m", "channel_monitor_v2_latency_histograms_1m",
		"channel_monitor_v2_metrics_rollup", "channel_monitor_v2_user_metrics_rollup", "channel_monitor_v2_error_metrics_rollup", "channel_monitor_v2_latency_histograms_rollup",
	} {
		_, err := db.Exec("CREATE TABLE " + pq.QuoteIdentifier(table) + " (LIKE public." + pq.QuoteIdentifier(table) + " INCLUDING ALL)")
		require.NoError(t, err)
	}
	_, err = db.Exec(`
	CREATE TABLE groups(id bigint PRIMARY KEY, name text, platform text, status text DEFAULT 'active', deleted_at timestamptz);
	CREATE TABLE accounts(id bigint PRIMARY KEY, platform text);
	CREATE TABLE users(id bigint PRIMARY KEY, email text, username text);
	CREATE TABLE usage_logs(id bigint PRIMARY KEY, request_id text, group_id bigint, account_id bigint, requested_model text, model text, created_at timestamptz, actual_cost numeric, request_type int);
	CREATE TABLE ops_error_logs(id bigint PRIMARY KEY, request_id text, group_id bigint, account_id bigint, requested_model text, model text, platform text, created_at timestamptz,
	  is_count_tokens boolean DEFAULT false, status_code int DEFAULT 504, error_type text DEFAULT 'timeout', error_owner text DEFAULT 'provider', error_source text DEFAULT 'upstream',
	  upstream_status_code int, upstream_error_message text, error_message text, upstream_error_detail text, error_body text);
	INSERT INTO groups(id,name,platform) VALUES(7,'OpenAI','openai'),(8,'Grok','grok'),(9,'Composite','composite');
	INSERT INTO accounts VALUES(1,'openai'),(2,'grok');
	INSERT INTO users VALUES(1,'visible@example.invalid','visible'),(2,'excluded@example.invalid','excluded');`)
	require.NoError(t, err)
	return db
}

func channelMonitorV2ExecAt(t *testing.T, db *sql.DB, statements string, at time.Time) {
	t.Helper()
	for _, statement := range strings.Split(statements, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		_, err := db.Exec(statement, at)
		require.NoError(t, err)
	}
}

func TestChannelMonitorV2ExclusionsRemoveAllHistoricalContributions(t *testing.T) {
	db := channelMonitorV2VisibilityDB(t)
	r := &channelMonitorV2Repository{db: db}
	ctx := context.Background()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	_, err := db.Exec(`INSERT INTO channel_monitor_v2_watermarks(id,usage_coverage_start,error_coverage_start,data_through,last_successful_at,backfill_cursor) VALUES(1,$1,$1,$2,$2,$1)`, start, start.Add(31*24*time.Hour))
	require.NoError(t, err)
	type fact struct {
		platform, model                       string
		group, user, success, errors, latency int64
		category                              string
	}
	facts := []fact{
		{"openai", "visible", 7, 1, 8, 2, 100, "timeout"},
		{"openai", "hidden", 7, 2, 900, 900, 9000, "authentication"},
		{"openai", "hidden-other", 7, 2, 700, 700, 9000, "authentication"},
		{"grok", "hidden", 8, 1, 4, 0, 200, "timeout"},
	}
	for _, seconds := range []int{0, 300, 3600, 43200, 86400} {
		suffix, bucketColumns, bucketValues := "_1m", "", ""
		if seconds > 0 {
			suffix, bucketColumns, bucketValues = "_rollup", ",bucket_seconds", fmt.Sprintf(",%d", seconds)
		}
		for _, f := range facts {
			columns := "bucket_start,platform,group_id,model,success_requests,error_requests,input_tokens,output_tokens,cache_read_tokens,ttft_sum_ms,ttft_count,duration_sum_ms,duration_count"
			values := "$1::timestamptz,$2::text,$3::bigint,$4::text,$5::bigint,$6::bigint,$5::bigint*10,$5::bigint*5,$5::bigint*5,$5::bigint*$7::bigint,$5::bigint,$5::bigint*$7::bigint*2,$5::bigint"
			_, err = db.Exec("INSERT INTO channel_monitor_v2_metrics"+suffix+"("+columns+bucketColumns+") VALUES("+values+bucketValues+")", start, f.platform, f.group, f.model, f.success, f.errors, f.latency)
			require.NoError(t, err)
			_, err = db.Exec("INSERT INTO channel_monitor_v2_user_metrics"+suffix+"("+columns+",user_id"+bucketColumns+") VALUES("+values+",$8::bigint"+bucketValues+")", start, f.platform, f.group, f.model, f.success, f.errors, f.latency, f.user)
			require.NoError(t, err)
			_, err = db.Exec("INSERT INTO channel_monitor_v2_error_metrics"+suffix+"(bucket_start,platform,group_id,model,error_category,taxonomy_version,error_requests"+bucketColumns+") VALUES($1::timestamptz,$2::text,$3::bigint,$4::text,$5::text,1,$6::bigint"+bucketValues+")", start, f.platform, f.group, f.model, f.category, f.errors)
			require.NoError(t, err)
			for _, user := range []int64{0, f.user} {
				_, err = db.Exec("INSERT INTO channel_monitor_v2_latency_histograms"+suffix+"(bucket_start,platform,group_id,model,user_id,metric,upper_bound_ms,sample_count"+bucketColumns+") VALUES($1::timestamptz,$2::text,$3::bigint,$4::text,$5::bigint,'ttft',$6::integer,$7::bigint"+bucketValues+")", start, f.platform, f.group, f.model, user, f.latency, f.success)
				require.NoError(t, err)
			}
		}
	}
	channelMonitorV2ExecAt(t, db, `INSERT INTO ops_error_logs(id,request_id,group_id,account_id,requested_model,model,platform,created_at,error_message)
	  VALUES(1,'visible-error',7,1,'visible','mapped','openai',$1,'timeout');
	  INSERT INTO ops_error_logs(id,request_id,group_id,account_id,requested_model,model,platform,created_at,error_message)
	  SELECT n,'hidden-'||n,7,1,'hidden','mapped','openai',$1,'timeout hidden '||n FROM generate_series(2,452) n`, start.Add(time.Second))
	thresholds := service.DefaultChannelMonitorV2HealthThresholds()
	thresholds.MinimumSample = 1
	cfg := service.ChannelMonitorV2Config{Enabled: true, GroupIDs: []int64{7, 8}, HealthThresholds: thresholds, IgnoredErrorCategories: []string{"authentication"}, Platforms: []service.ChannelMonitorV2PlatformConfig{
		{Platform: "openai", Enabled: true, Models: []string{"visible", "hidden"}, ExcludedModels: []string{"hidden", "hidden-other"}},
		{Platform: "grok", Enabled: true},
	}}
	for _, bucket := range []time.Duration{time.Minute, 5 * time.Minute, time.Hour, 12 * time.Hour, 24 * time.Hour} {
		t.Run(bucket.String(), func(t *testing.T) {
			filter := service.ChannelMonitorV2Filter{Start: start, End: start.Add(bucket), Bucket: bucket}
			snap, err := r.GetSnapshot(ctx, filter, cfg, true)
			require.NoError(t, err)
			require.Equal(t, int64(14), snap.Metrics.RequestCount)
			require.Equal(t, int64(240), snap.Metrics.TokenCount)
			require.InDelta(t, 2.0/14.0, snap.Metrics.ErrorRate, 0.00001)
			require.Equal(t, "warning", snap.Health.ErrorRate)
			require.Equal(t, int64(12), snap.Metrics.TTFT.SampleCount)
			require.Equal(t, int64(200), *snap.Metrics.TTFT.P95Ms)
			require.Len(t, snap.Trend, 1)
			require.Equal(t, int64(14), snap.Trend[0].Metrics.RequestCount)
			models, err := r.GetModels(ctx, filter, cfg, true)
			require.NoError(t, err)
			require.Len(t, models.Items, 2)
			for _, model := range models.Items {
				require.False(t, model.Platform == "openai" && model.Model != "visible")
			}
			dims, err := r.GetDimensions(ctx, filter, cfg)
			require.NoError(t, err)
			require.Len(t, dims.Models, 2)
			for _, mode := range []service.ChannelMonitorV2GroupBy{service.ChannelMonitorV2GroupByPlatform, service.ChannelMonitorV2GroupByPlatformGroup, service.ChannelMonitorV2GroupByPlatformModel, service.ChannelMonitorV2GroupByPlatformGroupModel} {
				matrix, err := r.GetMatrix(ctx, filter, cfg, mode, true)
				require.NoError(t, err)
				var requests int64
				for _, row := range matrix.Items {
					requests += row.Metrics.RequestCount
					require.False(t, row.Platform == "openai" && row.Model == "hidden")
				}
				require.Equal(t, int64(14), requests, mode)
			}
			errors, err := r.GetErrors(ctx, filter, cfg, true)
			require.NoError(t, err)
			require.Len(t, errors.Items, 1)
			require.Equal(t, int64(2), errors.Items[0].Count)
			require.Len(t, errors.Items[0].Details, 1, "excluded error samples cannot consume the 400-row limit")
			require.Equal(t, "visible", errors.Items[0].Details[0].Model)
			users, err := r.GetUsers(ctx, filter, cfg, true)
			require.NoError(t, err)
			require.Len(t, users.Items, 1)
			require.Equal(t, int64(1), *users.Items[0].UserID)
			require.Equal(t, int64(14), users.Items[0].Metrics.RequestCount)
			// Restoring the policy restores already-stored history immediately.
			restored := cfg
			restored.Platforms = append([]service.ChannelMonitorV2PlatformConfig(nil), cfg.Platforms...)
			restored.Platforms[0].ExcludedModels = nil
			full, err := r.GetSnapshot(ctx, filter, restored, true)
			require.NoError(t, err)
			require.Equal(t, int64(3214), full.Metrics.RequestCount)
		})
	}
}

func TestChannelMonitorV2CatalogUsesCompleteRequestedNamesFromSuccessAndFailure(t *testing.T) {
	db := channelMonitorV2VisibilityDB(t)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	channelMonitorV2ExecAt(t, db, `INSERT INTO usage_logs VALUES
	  (1,'success',7,1,'Requested-Only','mapped',$1,1,1),
	  (2,'success',7,1,'Requested-Only','mapped',$1,1,1),
	  (3,'fallback',7,1,'','Fallback-Model',$1,1,1),
	  (4,'count-token',7,1,'excluded-type','mapped',$1,1,4),
	  (5,'async',7,1,'excluded-type','mapped',$1,1,6),
	  (6,'free',7,1,'free-model','mapped',$1,0,1),
	  (7,'composite',9,2,'Composite-Requested','mapped',$1,1,1);
	INSERT INTO ops_error_logs(id,request_id,group_id,account_id,requested_model,model,platform,created_at,is_count_tokens,status_code,error_type) VALUES
	  (1,'error',7,1,'Failed-Only','mapped','openai',$1,false,504,'timeout'),
	  (2,'error',7,1,'Failed-Only','mapped','openai',$1,false,504,'timeout'),
	  (3,'cyber',7,1,'Policy-Only','mapped','openai',$1,false,200,'cyber_policy'),
	  (4,'tokens',7,1,'excluded-error','mapped','openai',$1,true,504,'timeout'),
	  (5,'invalid',7,1,'excluded-error','mapped','openai',$1,false,200,'timeout'),
	  (6,'composite-error',9,2,'Composite-Failed','mapped','composite',$1,false,504,'timeout'),
	  (7,'cross-scope',7,1,'Cross-Scope-Old','mapped','openai',$1,false,504,'timeout'),
	  (8,'cross-scope',9,2,'Cross-Scope-Latest','mapped','composite',$1,false,504,'timeout')`, start.Add(time.Second))
	r := &channelMonitorV2Repository{db: db}
	cfg := service.ChannelMonitorV2Config{Version: 9, Enabled: false, GroupIDs: []int64{999}, Platforms: []service.ChannelMonitorV2PlatformConfig{
		{Platform: "openai", Enabled: false, Models: []string{"Seed"}, ExcludedModels: []string{"Requested-Only", "Restorable"}},
	}}
	filter := service.ChannelMonitorV2Filter{Range: "30d", Start: start, End: start.Add(24 * time.Hour)}
	catalog, err := r.GetModelCatalog(context.Background(), filter, cfg)
	require.NoError(t, err)
	require.Equal(t, []service.ChannelMonitorV2ModelCatalogItem{
		{Platform: "grok", Model: "Composite-Failed", ErrorRequests: 1, Sources: []string{"error"}},
		{Platform: "grok", Model: "Composite-Requested", SuccessRequests: 1, Sources: []string{"success"}},
		{Platform: "grok", Model: "Cross-Scope-Latest", ErrorRequests: 1, Sources: []string{"error"}},
		{Platform: "openai", Model: "Failed-Only", ErrorRequests: 1, Sources: []string{"error"}},
		{Platform: "openai", Model: "Fallback-Model", SuccessRequests: 1, Sources: []string{"success"}},
		{Platform: "openai", Model: "Policy-Only", ErrorRequests: 1, Sources: []string{"error"}},
		{Platform: "openai", Model: "Requested-Only", SuccessRequests: 1, Sources: []string{"success", "excluded"}},
		{Platform: "openai", Model: "Restorable", Sources: []string{"excluded"}},
		{Platform: "openai", Model: "Seed", Sources: []string{"configured"}},
	}, catalog.Items)
	filter.Platforms, filter.GroupIDs = []string{"openai"}, []int64{7}
	catalog, err = r.GetModelCatalog(context.Background(), filter, cfg)
	require.NoError(t, err)
	for _, item := range catalog.Items {
		require.NotEqual(t, "Cross-Scope-Old", item.Model, "filtering cannot resurrect an earlier error after a retry changed platform/group")
	}
	filter.Platforms, filter.GroupIDs = []string{"grok"}, []int64{7}
	catalog, err = r.GetModelCatalog(context.Background(), filter, cfg)
	require.NoError(t, err)
	require.Empty(t, catalog.Items, "explicit draft platform/group scope applies to raw observations")
}
