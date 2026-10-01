//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV3CanonicalOutcomesScopeAndIncidents(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "v3-" + uuid.NewString()
	group := mustCreateGroup(t, client, &service.Group{Name: prefix, Platform: service.PlatformOpenAI, RateMultiplier: 1})
	foreign := mustCreateGroup(t, client, &service.Group{Name: prefix + "-hidden", Platform: service.PlatformOpenAI, RateMultiplier: 1})
	composite := mustCreateGroup(t, client, &service.Group{Name: prefix + "-composite", Platform: "composite", RateMultiplier: 1})
	user := mustCreateUser(t, client, &service.User{Email: prefix + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-" + prefix, GroupID: &group.ID})
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
		}{{"api_keys", key.ID}, {"accounts", account.ID}, {"groups", group.ID}, {"groups", foreign.ID}, {"groups", composite.ID}, {"users", user.ID}} {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM "+row.table+" WHERE id=$1", row.id)
		}
	})
	now := time.Now().UTC().Truncate(time.Minute).Add(3 * time.Minute)
	addUsage := func(suffix, model, status string, complete any, gid int64, kind int, at time.Time) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO usage_logs (user_id,api_key_id,account_id,request_id,model,requested_model,group_id,completion_status,is_complete,request_type,actual_cost,timing_version,first_token_ms,created_at)
   VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$9,0,1,120,$10)`, user.ID, key.ID, account.ID, prefix+suffix, model, gid, status, complete, kind, at)
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
	addUsage("-foreign", "secret-model", "incomplete", false, foreign.ID, 2, at)
	addUsage("-composite", "gemini-test", "completed", true, composite.ID, 2, at)
	repo := NewChannelMonitorV3Repository(integrationDB)
	cfg := service.DefaultChannelMonitorV3Config()
	cfg.MinimumSamples = 1
	cfg.AbnormalWindows = 1
	cfg.RecoveryWindows = 2
	require.NoError(t, repo.Refresh(ctx, now, cfg))
	data, err := repo.Read(ctx, []int64{group.ID}, now, 24*time.Hour)
	require.NoError(t, err)
	var totals service.ChannelMonitorV3Fact
	for _, f := range data.Current {
		totals.Merge(f)
	}
	require.Equal(t, int64(7), totals.Success)
	require.Equal(t, int64(3), totals.Failures)
	require.Equal(t, int64(250), *totals.P50())
	result := service.BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "")
	require.Len(t, result.Platforms, 1)
	require.Len(t, result.Incidents, 3)
	require.Equal(t, 3, result.Summary.ActiveEvents)
	require.Contains(t, result.Platforms[0].Timeline, service.ChannelMonitorV3Timeline{At: now.Add(-30 * time.Minute), Status: "partial"}, "history bins must align to the snapshot window")
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret-model")
	require.NotContains(t, string(raw), prefix+"-hidden")
	require.NotContains(t, string(raw), "actual_cost")
	require.NotContains(t, string(raw), "user_id")
	empty, err := repo.Read(ctx, nil, now, 24*time.Hour)
	require.NoError(t, err)
	require.Empty(t, empty.Incidents)
	require.Empty(t, empty.Catalog)
	routed, err := repo.Read(ctx, []int64{composite.ID}, now, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, "gemini", routed.Current[0].Platform)
	// Same observation never doubles facts or events.
	require.NoError(t, repo.Refresh(ctx, now, cfg))
	same, err := repo.Read(ctx, []int64{group.ID}, now, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, data.Current, same.Current)
	require.Len(t, same.Incidents, 3)
	// A delayed successful terminal log replaces the earlier retry error.
	addUsage("-only", "error-only", "completed", true, group.ID, 2, now.Add(-time.Second))
	require.NoError(t, repo.Refresh(ctx, now.Add(time.Minute), cfg))
	recovered, err := repo.Read(ctx, []int64{group.ID}, now.Add(time.Minute), 24*time.Hour)
	require.NoError(t, err)
	for _, event := range recovered.Incidents {
		if event.Model == "error-only" {
			require.Equal(t, "recovering", event.Phase)
			require.Nil(t, event.ResolvedAt)
		}
	}
	addUsage("-healthy", "error-only", "completed", true, group.ID, 2, now.Add(time.Minute-time.Second))
	require.NoError(t, repo.Refresh(ctx, now.Add(2*time.Minute), cfg))
	recovered, err = repo.Read(ctx, []int64{group.ID}, now.Add(2*time.Minute), 24*time.Hour)
	require.NoError(t, err)
	for _, event := range recovered.Incidents {
		if event.Model == "error-only" {
			require.Equal(t, "resolved", event.Phase)
			require.NotNil(t, event.ResolvedAt)
		}
	}
	// Silence cannot resolve the other model incidents, even after restart.
	require.NoError(t, NewChannelMonitorV3Repository(integrationDB).Refresh(ctx, now.Add(10*time.Minute), cfg))
	quiet, err := repo.Read(ctx, []int64{group.ID}, now.Add(10*time.Minute), 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 2, service.BuildChannelMonitorV3Snapshot(quiet, cfg, now.Add(10*time.Minute), 24*time.Hour, "").Summary.ActiveEvents)
	for _, event := range quiet.Incidents {
		if event.ResolvedAt == nil {
			require.Equal(t, "awaiting_data", event.Phase)
		}
	}
	// Optimistic configuration and the migration defaults are persistent.
	stored, err := repo.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, service.DefaultChannelMonitorV3Config().MinimumSamples, stored.MinimumSamples)
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
	data, err := NewChannelMonitorV3Repository(integrationDB).Read(ctx, []int64{group.ID}, now, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, 205, service.BuildChannelMonitorV3Snapshot(data, service.DefaultChannelMonitorV3Config(), now, 24*time.Hour, "").Summary.ActiveEvents)
}
