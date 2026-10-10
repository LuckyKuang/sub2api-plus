//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func saveV3TestConfig(t *testing.T, cfg service.ChannelMonitorV3Config) service.ChannelMonitorV3Config {
	t.Helper()
	var raw string
	var version int
	require.NoError(t, integrationDB.QueryRow(`SELECT config::text,version FROM channel_monitor_v3_config WHERE id=1`).Scan(&raw, &version))
	t.Cleanup(func() {
		_, err := integrationDB.Exec(`UPDATE channel_monitor_v3_config SET config=$1,version=$2 WHERE id=1`, raw, version)
		require.NoError(t, err)
	})
	cfg.Version = version
	updated, err := NewChannelMonitorV3Repository(integrationDB).UpdateConfig(context.Background(), cfg)
	require.NoError(t, err)
	return *updated
}

func resetV3Observations(t *testing.T) {
	t.Helper()
	truncateIntegrationTables(t, "channel_monitor_v3_facts", "channel_monitor_v3_states", "channel_monitor_v3_incidents")
	_, err := integrationDB.Exec(`UPDATE channel_monitor_v3_watermark SET data_through=NULL WHERE id=1`)
	require.NoError(t, err)
	t.Cleanup(func() {
		truncateIntegrationTables(t, "channel_monitor_v3_facts", "channel_monitor_v3_states", "channel_monitor_v3_incidents")
		_, _ = integrationDB.Exec(`UPDATE channel_monitor_v3_watermark SET data_through=NULL WHERE id=1`)
	})
}

func TestChannelMonitorV3PlatformPauseResumeAndHistory(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "v3-toggle-" + uuid.NewString()
	group := mustCreateGroup(t, client, &service.Group{Name: prefix, Platform: service.PlatformOpenAI, RateMultiplier: 1})
	composite := mustCreateGroup(t, client, &service.Group{Name: prefix + "-composite", Platform: "composite", RateMultiplier: 1})
	user := mustCreateUser(t, client, &service.User{Email: prefix + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + prefix, GroupID: &group.ID})
	account := mustCreateAccount(t, client, &service.Account{Name: prefix, Platform: service.PlatformOpenAI})
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM usage_logs WHERE request_id LIKE $1`, prefix+"%")
		for _, row := range []struct {
			table string
			id    int64
		}{{"api_keys", key.ID}, {"accounts", account.ID}, {"groups", composite.ID}, {"groups", group.ID}, {"users", user.ID}} {
			_, _ = integrationDB.Exec("DELETE FROM "+row.table+" WHERE id=$1", row.id)
		}
	})
	resetV3Observations(t)
	cfg := service.DefaultChannelMonitorV3Config()
	cfg.MinimumSamples = 1
	cfg = saveV3TestConfig(t, cfg)
	repo := NewChannelMonitorV3Repository(integrationDB)
	addUsage := func(suffix string, gid int64, complete bool, at time.Time) {
		t.Helper()
		status := "incomplete"
		if complete {
			status = "completed"
		}
		_, err := integrationDB.Exec(`INSERT INTO usage_logs (user_id,api_key_id,account_id,request_id,model,requested_model,group_id,completion_status,is_complete,created_at)
 VALUES ($1,$2,$3,$4,'toggle-model','toggle-model',$5,$6,$7,$8)`, user.ID, key.ID, account.ID, prefix+suffix, gid, status, complete, at)
		require.NoError(t, err)
	}
	now := time.Now().UTC().Truncate(time.Minute)
	addUsage("-first", group.ID, false, now.Add(-2*time.Minute))
	addUsage("-composite", composite.ID, false, now.Add(-2*time.Minute))
	require.NoError(t, repo.Refresh(ctx, now.Add(-time.Minute)))
	addUsage("-second", group.ID, false, now.Add(-time.Second))
	require.NoError(t, repo.Refresh(ctx, now))
	var frozenState, frozenIncident string
	require.NoError(t, integrationDB.QueryRow(`SELECT data::text FROM channel_monitor_v3_states WHERE group_id=$1`, group.ID).Scan(&frozenState))
	require.NoError(t, integrationDB.QueryRow(`SELECT data::text FROM channel_monitor_v3_incidents WHERE group_id=$1`, group.ID).Scan(&frozenIncident))
	var oldFacts int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM channel_monitor_v3_facts WHERE platform='openai'`).Scan(&oldFacts))
	cfg.DisabledPlatforms = []string{"openai"}
	cfg = saveV3TestConfig(t, cfg)
	require.NoError(t, repo.Refresh(ctx, now.Add(time.Minute)))
	data, err := repo.Read(ctx, now.Add(time.Minute), 24*time.Hour, "openai")
	require.NoError(t, err)
	require.Empty(t, data.Catalog)
	require.Empty(t, data.Current)
	require.Empty(t, data.History)
	require.Empty(t, data.KnownModels)
	require.Empty(t, data.States)
	require.Empty(t, data.Incidents)
	var after string
	require.NoError(t, integrationDB.QueryRow(`SELECT data::text FROM channel_monitor_v3_states WHERE group_id=$1`, group.ID).Scan(&after))
	require.Equal(t, frozenState, after)
	require.NoError(t, integrationDB.QueryRow(`SELECT data::text FROM channel_monitor_v3_incidents WHERE group_id=$1`, group.ID).Scan(&after))
	require.Equal(t, frozenIncident, after)
	cfg.DisabledPlatforms = []string{}
	// Client-supplied internal metadata must not override the trusted boundary.
	cfg.ObservationStarts = map[string]time.Time{"openai": now.Add(-time.Hour)}
	cfg = saveV3TestConfig(t, cfg)
	since := cfg.ObservationStarts["openai"]
	require.True(t, since.After(now))
	restarted := NewChannelMonitorV3Repository(integrationDB)
	persisted, err := restarted.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, since, persisted.ObservationStarts["openai"])
	addUsage("-paused", group.ID, true, since.Add(-time.Second))
	data, err = restarted.Read(ctx, since.Add(30*time.Second), 24*time.Hour, "openai")
	require.NoError(t, err)
	require.Empty(t, data.Current, "pre-pause samples cannot determine the resumed current status")
	require.NotEmpty(t, data.History)
	require.Equal(t, "awaiting_data", data.Incidents[0].Phase)
	for _, state := range data.States {
		require.Zero(t, state.Streak)
	}
	for i := 0; i < cfg.RecoveryWindows; i++ {
		addUsage(fmt.Sprintf("-resumed-%d", i), group.ID, true, since.Add(time.Duration(i)*time.Minute+time.Second))
		require.NoError(t, restarted.Refresh(ctx, since.Add(time.Duration(i+1)*time.Minute)))
		data, err = restarted.Read(ctx, since.Add(time.Duration(i+1)*time.Minute), 24*time.Hour, "openai")
		require.NoError(t, err)
		require.Len(t, data.Incidents, 1)
		if i < cfg.RecoveryWindows-1 {
			require.Equal(t, "recovering", data.Incidents[0].Phase)
			require.Nil(t, data.Incidents[0].ResolvedAt)
		} else {
			require.Equal(t, "resolved", data.Incidents[0].Phase)
		}
	}
	var preserved int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM channel_monitor_v3_facts WHERE platform='openai' AND bucket_start < $1`, since).Scan(&preserved))
	require.Equal(t, oldFacts, preserved, "recent facts from before the pause must survive recomputation")
	var successes int
	require.NoError(t, integrationDB.QueryRow(`SELECT SUM(success_requests) FROM channel_monitor_v3_facts WHERE group_id=$1 AND bucket_start >= $2`, group.ID, since).Scan(&successes))
	require.Equal(t, cfg.RecoveryWindows, successes, "stopped-period traffic must never be backfilled")
}

func TestChannelMonitorV3DisabledTrafficLimitAndAllDisabledCleanup(t *testing.T) {
	ctx := context.Background()
	resetV3Observations(t)
	client := testEntClient(t)
	prefix := "v3-limit-" + uuid.NewString()
	group := mustCreateGroup(t, client, &service.Group{Name: prefix, Platform: service.PlatformOpenAI, RateMultiplier: 1})
	kimi := mustCreateGroup(t, client, &service.Group{Name: prefix + "-kimi", Platform: service.PlatformKimi, RateMultiplier: 1})
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM ops_error_logs WHERE request_id LIKE $1`, prefix+"%")
		_, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=ANY($1)`, pq.Array([]int64{group.ID, kimi.ID}))
	})
	cfg := service.DefaultChannelMonitorV3Config()
	cfg.DisabledPlatforms = []string{"openai"}
	cfg = saveV3TestConfig(t, cfg)
	now := time.Now().UTC().Truncate(time.Minute)
	_, err := integrationDB.Exec(`INSERT INTO ops_error_logs (request_id,group_id,platform,model,error_phase,error_type,error_owner,status_code,created_at)
 SELECT $1||n::text,$2,'anthropic','disabled','upstream','request_error','provider',503,$3 FROM generate_series(1,50001) n`, prefix, group.ID, now.Add(-time.Second))
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO ops_error_logs (request_id,group_id,platform,model,error_phase,error_type,error_owner,status_code,created_at)
 VALUES ($1,$2,'openai','enabled','upstream','request_error','provider',503,$3)`, prefix+"-kimi", kimi.ID, now.Add(-time.Second))
	require.NoError(t, err)
	repo := NewChannelMonitorV3Repository(integrationDB)
	require.NoError(t, repo.Refresh(ctx, now), "disabled terminal outcomes must be filtered before the observation limit")
	data, err := repo.Read(ctx, now, 24*time.Hour, "")
	require.NoError(t, err)
	require.Len(t, data.Current, 1)
	require.Equal(t, "kimi", data.Current[0].Platform, "effective group platform wins over protocol classification")
	cfg.DisabledPlatforms = domain.ConcretePlatforms()
	saveV3TestConfig(t, cfg)
	_, err = integrationDB.Exec(`INSERT INTO channel_monitor_v3_facts VALUES ($1,'openai',$2,'expired',1,0,$1,ARRAY[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0])`, now.Add(-32*24*time.Hour), group.ID)
	require.NoError(t, err)
	locked, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = locked.Rollback() }()
	_, err = locked.Exec(`LOCK TABLE usage_logs,ops_error_logs IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)
	deadline, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, repo.Refresh(deadline, now.Add(time.Minute)), "all-disabled must not read either source log table")
	var expired int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM channel_monitor_v3_facts WHERE model='expired'`).Scan(&expired))
	require.Zero(t, expired)
	data, err = repo.Read(ctx, now.Add(time.Minute), 24*time.Hour, "")
	require.NoError(t, err)
	result := service.BuildChannelMonitorV3Snapshot(data, data.Config, now.Add(time.Minute), 24*time.Hour, "")
	require.False(t, result.MonitoringEnabled)
	require.Empty(t, result.Platforms)
	require.Equal(t, service.ChannelMonitorV3Summary{Status: "unknown"}, result.Summary)
}

func TestChannelMonitorV3ConfigUpgradeAndAggregationLock(t *testing.T) {
	ctx := context.Background()
	cfg := saveV3TestConfig(t, service.DefaultChannelMonitorV3Config())
	_, err := integrationDB.Exec(`UPDATE channel_monitor_v3_config SET config=config-'disabled_platforms'-'observation_starts' WHERE id=1`)
	require.NoError(t, err)
	repo := NewChannelMonitorV3Repository(integrationDB)
	upgraded, err := repo.GetConfig(ctx)
	require.NoError(t, err)
	require.Empty(t, upgraded.DisabledPlatforms)
	require.ElementsMatch(t, []string{"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "stepfun", "opencode_go", "typesafe", "command_code", "cline"}, upgraded.EnabledPlatforms())
	inflight, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = inflight.Rollback() }()
	_, err = readV3Config(ctx, inflight, " FOR SHARE")
	require.NoError(t, err)
	cfg.DisabledPlatforms = []string{"openai"}
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = repo.UpdateConfig(deadline, cfg)
	require.Error(t, err, "saving must wait for the in-flight aggregation's configuration lock")
	require.NoError(t, inflight.Rollback())
	updated, err := repo.UpdateConfig(ctx, cfg)
	require.NoError(t, err)
	require.False(t, updated.PlatformEnabled("openai"))
	_, err = repo.UpdateConfig(ctx, cfg)
	require.ErrorIs(t, err, service.ErrChannelMonitorV3Conflict)
}

func TestChannelMonitorV3CanonicalOutcomesGlobalStatusAndIncidents(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "v3-" + uuid.NewString()
	group := mustCreateGroup(t, client, &service.Group{Name: prefix, Platform: service.PlatformOpenAI, RateMultiplier: 1})
	foreign := mustCreateGroup(t, client, &service.Group{Name: prefix + "-exclusive", Platform: service.PlatformOpenAI, RateMultiplier: 1, IsExclusive: true})
	composite := mustCreateGroup(t, client, &service.Group{Name: prefix + "-composite", Platform: "composite", RateMultiplier: 1})
	user := mustCreateUser(t, client, &service.User{Email: prefix + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + prefix, GroupID: &group.ID})
	otherUser := mustCreateUser(t, client, &service.User{Email: prefix + "-other@example.com"})
	otherKey := mustCreateApiKey(t, client, &service.APIKey{UserID: otherUser.ID, Key: "sk-" + prefix + "-other", GroupID: &foreign.ID})
	account := mustCreateAccount(t, client, &service.Account{Name: prefix, Platform: service.PlatformGemini})
	truncateIntegrationTables(t, "channel_monitor_v3_facts", "channel_monitor_v3_states", "channel_monitor_v3_incidents")
	_, err := integrationDB.ExecContext(ctx, "UPDATE channel_monitor_v3_watermark SET data_through=NULL WHERE id=1")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE request_id LIKE $1", prefix+"%")
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE request_id LIKE $1", prefix+"%")
		truncateIntegrationTables(t, "channel_monitor_v3_facts", "channel_monitor_v3_states", "channel_monitor_v3_incidents")
		_, _ = integrationDB.ExecContext(ctx, "UPDATE channel_monitor_v3_watermark SET data_through=NULL WHERE id=1")
		for _, row := range []struct {
			table string
			id    int64
		}{{"api_keys", key.ID}, {"api_keys", otherKey.ID}, {"accounts", account.ID}, {"groups", group.ID}, {"groups", foreign.ID}, {"groups", composite.ID}, {"users", user.ID}, {"users", otherUser.ID}} {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM "+row.table+" WHERE id=$1", row.id)
		}
	})
	now := time.Now().UTC().Truncate(time.Minute).Add(3 * time.Minute)
	addUsage := func(suffix, model, status string, complete any, gid int64, kind int, at time.Time) {
		t.Helper()
		uid, kid := user.ID, key.ID
		if gid == foreign.ID {
			uid, kid = otherUser.ID, otherKey.ID
		}
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO usage_logs (user_id,api_key_id,account_id,request_id,model,requested_model,group_id,completion_status,is_complete,request_type,actual_cost,timing_version,first_token_ms,created_at)
   VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,0,1,120,$10)`, uid, kid, account.ID, prefix+suffix, model, gid, status, complete, kind, at)
		require.NoError(t, err)
	}
	addError := func(suffix, model, owner, message string, status, upstream int, gid int64, countTokens bool, at time.Time) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs (request_id,group_id,account_id,platform,model,requested_model,error_phase,error_type,error_owner,error_message,status_code,upstream_status_code,is_count_tokens,created_at)
   VALUES ($1,$2,$3,'openai',$4,$4,'upstream','request_error',$5,$6,$7,$8,$9,$10)`, prefix+suffix, gid, account.ID, model, owner, message, status, upstream, countTokens, at)
		require.NoError(t, err)
	}
	at := now.Add(-time.Minute - time.Second)
	for i := 0; i < 5; i++ {
		addUsage("-free-"+string(rune('a'+i)), "free", "completed", true, group.ID, 2, at)
	}
	addError("-retry", "retry", "provider", "retry transport", 502, 502, group.ID, false, now.Add(-8*time.Minute))
	addUsage("-retry", "retry", "completed", true, group.ID, 2, at)
	addUsage("-legacy", "legacy", "unknown", true, group.ID, 1, at)
	addUsage("-incomplete", "broken", "incomplete", false, group.ID, 2, at)
	_, err = integrationDB.ExecContext(ctx, "UPDATE usage_logs SET actual_cost=99 WHERE request_id=$1", prefix+"-incomplete")
	require.NoError(t, err)
	addError("-disconnect", "disconnected", "provider", "transport", 502, 502, group.ID, false, now.Add(-2*time.Minute))
	addUsage("-disconnect", "disconnected", "client_disconnected", false, group.ID, 2, at)
	addError("-policy", "policy", "provider", "content_policy", 502, 502, group.ID, false, at)
	addUsage("-policy", "policy", "incomplete", false, group.ID, 2, at)
	addUsage("-unknown", "unknown", "unknown", nil, group.ID, 2, at)
	addUsage("-cyber", "cyber", "completed", true, group.ID, 4, at)
	addError("-only", "error-only", "provider", "upstream unavailable", 503, 503, group.ID, false, at)
	// Protocol/log classification cannot move failures away from the group's
	// effective platform and split its successful and failed requests.
	_, err = integrationDB.ExecContext(ctx, "UPDATE ops_error_logs SET platform='anthropic' WHERE request_id=$1", prefix+"-only")
	require.NoError(t, err)
	addError("-auth", "provider-auth", "provider", "unauthorized", 502, 401, group.ID, false, at)
	addError("-userquota", "quota", "client", "insufficient quota", 429, 0, group.ID, false, at)
	addError("-cancel", "cancel", "platform", "transport", 499, 0, group.ID, false, at)
	addError("-tokens", "tokens", "provider", "upstream unavailable", 503, 503, group.ID, true, at)
	addUsage("-foreign", "other-user-model", "incomplete", false, foreign.ID, 2, at)
	addUsage("-composite", "gemini-test", "completed", true, composite.ID, 2, at)
	repo := NewChannelMonitorV3Repository(integrationDB)
	cfg := saveV3TestConfig(t, service.DefaultChannelMonitorV3Config())
	cfg.MinimumSamples = 1
	cfg.AbnormalWindows = 1
	cfg.RecoveryWindows = 2
	cfg = saveV3TestConfig(t, cfg)
	require.NoError(t, repo.Refresh(ctx, now))
	data, err := repo.Read(ctx, now, 24*time.Hour, "")
	require.NoError(t, err)
	var totals service.ChannelMonitorV3Fact
	for _, f := range data.Current {
		totals.Merge(f)
	}
	require.Equal(t, int64(8), totals.Success)
	require.Equal(t, int64(4), totals.Failures)
	require.Equal(t, int64(250), *totals.P50())
	result := service.BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Len(t, result.Incidents, 4)
	require.Equal(t, 4, result.Summary.ActiveEvents)
	openai := service.BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "openai")
	require.Contains(t, openai.Platforms[0].Timeline, service.ChannelMonitorV3Timeline{At: now.Add(-30 * time.Minute), Status: "partial"}, "history bins must align to the snapshot window")
	require.InDelta(t, 7.0/11, *openai.Platforms[0].SuccessRate, 0.00001, "platform history must include the other user's failed request")
	require.Contains(t, openai.Platforms[0].Models, service.ChannelMonitorV3Model{GroupID: foreign.ID, GroupName: foreign.Name, Model: "other-user-model", Status: "outage"})
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(raw), "other-user-model")
	require.Contains(t, string(raw), prefix+"-exclusive")
	require.NotContains(t, string(raw), "actual_cost")
	require.NotContains(t, string(raw), "user_id")
	for _, sensitive := range []string{"account_id", "api_key_id", "request_id", "error_message", "request_count", "success_requests", "failed_requests", user.Email, otherUser.Email, key.Key, otherKey.Key} {
		require.NotContains(t, string(raw), sensitive)
	}
	routed := service.BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "gemini")
	require.Len(t, routed.Platforms, 1)
	require.Equal(t, "gemini", routed.Platforms[0].Platform)
	require.Empty(t, routed.Incidents)
	// Same observation never doubles facts or events.
	require.NoError(t, repo.Refresh(ctx, now))
	same, err := repo.Read(ctx, now, 24*time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, data.Current, same.Current)
	require.Len(t, same.Incidents, 4)
	// A delayed successful terminal log replaces the earlier retry error.
	addUsage("-only", "error-only", "completed", true, group.ID, 2, now.Add(-time.Second))
	require.NoError(t, repo.Refresh(ctx, now.Add(time.Minute)))
	recovered, err := repo.Read(ctx, now.Add(time.Minute), 24*time.Hour, "")
	require.NoError(t, err)
	for _, event := range recovered.Incidents {
		if event.Model == "error-only" {
			require.Equal(t, "recovering", event.Phase)
			require.Nil(t, event.ResolvedAt)
		}
	}
	addUsage("-healthy", "error-only", "completed", true, group.ID, 2, now.Add(time.Minute-time.Second))
	require.NoError(t, repo.Refresh(ctx, now.Add(2*time.Minute)))
	recovered, err = repo.Read(ctx, now.Add(2*time.Minute), 24*time.Hour, "")
	require.NoError(t, err)
	for _, event := range recovered.Incidents {
		if event.Model == "error-only" {
			require.Equal(t, "resolved", event.Phase)
			require.NotNil(t, event.ResolvedAt)
		}
	}
	// Silence cannot resolve the other model incidents, even after restart.
	require.NoError(t, NewChannelMonitorV3Repository(integrationDB).Refresh(ctx, now.Add(10*time.Minute)))
	quiet, err := repo.Read(ctx, now.Add(10*time.Minute), 24*time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, 3, service.BuildChannelMonitorV3Snapshot(quiet, cfg, now.Add(10*time.Minute), 24*time.Hour, "").Summary.ActiveEvents)
	for _, event := range quiet.Incidents {
		if event.ResolvedAt == nil {
			require.Equal(t, "awaiting_data", event.Phase)
		}
	}
	// Global observability still excludes inactive and soft-deleted groups
	// consistently from current facts, history, models and incident records.
	for _, update := range []string{"status='inactive'", "status='active',deleted_at=NOW()"} {
		_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET "+update+" WHERE id=$1", foreign.ID)
		require.NoError(t, err)
		visible, readErr := repo.Read(ctx, now.Add(2*time.Minute), 24*time.Hour, "")
		require.NoError(t, readErr)
		for _, fact := range visible.Current {
			require.NotEqual(t, foreign.ID, fact.GroupID)
		}
		for _, fact := range visible.KnownModels {
			require.NotEqual(t, foreign.ID, fact.GroupID)
		}
		for _, incident := range visible.Incidents {
			require.NotEqual(t, foreign.ID, incident.GroupID)
		}
		filtered := service.BuildChannelMonitorV3Snapshot(visible, cfg, now.Add(2*time.Minute), 24*time.Hour, "openai")
		require.InDelta(t, 9.0/11, *filtered.Platforms[0].SuccessRate, 0.00001, "history must also exclude the removed group's failure")
		visibleJSON, marshalErr := json.Marshal(filtered)
		require.NoError(t, marshalErr)
		require.NotContains(t, string(visibleJSON), "other-user-model")
		require.NotContains(t, string(visibleJSON), foreign.Name)
	}
	// Optimistic configuration and the migration defaults are persistent.
	stored, err := repo.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg.MinimumSamples, stored.MinimumSamples)
	stale := *stored
	updated, err := repo.UpdateConfig(ctx, *stored)
	require.NoError(t, err)
	require.Equal(t, stale.Version+1, updated.Version)
	_, err = repo.UpdateConfig(ctx, stale)
	require.ErrorIs(t, err, service.ErrChannelMonitorV3Conflict)
}

func TestChannelMonitorV3IncidentHistoryKeepsAllActive(t *testing.T) {
	ctx := context.Background()
	group := mustCreateGroup(t, testEntClient(t), &service.Group{Name: "v3-events-" + uuid.NewString(), Platform: "composite", RateMultiplier: 1})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM channel_monitor_v3_incidents WHERE group_id=$1", group.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id=$1", group.ID)
	})
	now := time.Now().UTC()
	for i := 0; i < 205; i++ {
		event := service.ChannelMonitorV3Incident{ID: uuid.NewString(), Platform: "openai", GroupID: group.ID, Model: uuid.NewString(), Severity: "partial", Phase: "detected", StartedAt: now, UpdatedAt: now, Updates: []service.ChannelMonitorV3Update{}}
		raw, err := json.Marshal(event)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO channel_monitor_v3_incidents VALUES ($1,$2,$3,$4,$5,NULL,$6)`, event.ID, event.Platform, event.GroupID, event.Model, now, string(raw))
		require.NoError(t, err)
	}
	data, err := NewChannelMonitorV3Repository(integrationDB).Read(ctx, now, 24*time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, 205, service.BuildChannelMonitorV3Snapshot(data, service.DefaultChannelMonitorV3Config(), now, 24*time.Hour, "").Summary.ActiveEvents)
	// Hidden platforms must not consume the 200 resolved-event slots, including
	// an explicit platform filter when all platforms are otherwise enabled.
	for i := 0; i < 202; i++ {
		platform := "openai"
		started := now
		if i == 201 {
			platform, started = "kimi", now.Add(-time.Minute)
		}
		event := service.ChannelMonitorV3Incident{ID: uuid.NewString(), Platform: platform, GroupID: group.ID, Model: uuid.NewString(), Severity: "partial", Phase: "resolved", StartedAt: started, UpdatedAt: now, ResolvedAt: &now, Updates: []service.ChannelMonitorV3Update{}}
		raw, marshalErr := json.Marshal(event)
		require.NoError(t, marshalErr)
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO channel_monitor_v3_incidents VALUES ($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.Platform, event.GroupID, event.Model, started, now, string(raw))
		require.NoError(t, err)
	}
	repo := NewChannelMonitorV3Repository(integrationDB)
	filtered, err := repo.Read(ctx, now, 24*time.Hour, "kimi")
	require.NoError(t, err)
	require.Len(t, filtered.Incidents, 1)
	cfg := service.DefaultChannelMonitorV3Config()
	cfg.DisabledPlatforms = []string{"openai"}
	saveV3TestConfig(t, cfg)
	filtered, err = repo.Read(ctx, now, 24*time.Hour, "")
	require.NoError(t, err)
	require.Len(t, filtered.Incidents, 1)
	require.Equal(t, "kimi", filtered.Incidents[0].Platform)
}
