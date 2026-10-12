//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// docs/CHANNEL_MONITOR.md: [now-5m,now), including current-minute requests
// and probes, without awaiting history aggregation or advancing incidents.
func TestChannelMonitorV3ExactCurrentWindowIncludesUnaggregatedRequestsAndProbes(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "v3-current-" + uuid.NewString()
	group := mustCreateGroup(t, client, &service.Group{Name: prefix, Platform: "openai", RateMultiplier: 1})
	composite := mustCreateGroup(t, client, &service.Group{Name: prefix + "-composite", Platform: "composite", RateMultiplier: 1})
	user := mustCreateUser(t, client, &service.User{Email: prefix + "@example.test"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "current", GroupID: &group.ID})
	account := mustCreateAccount(t, client, &service.Account{Name: prefix, Platform: "openai"})
	monitorRepo := NewChannelMonitorRepository(client, integrationDB)
	monitor := &service.ChannelMonitor{Name: prefix, Provider: "openai", APIMode: "chat_completions", Endpoint: "https://example.test", APIKey: "test", PrimaryModel: "model", Enabled: true, IntervalSeconds: 60}
	require.NoError(t, monitorRepo.Create(ctx, monitor))
	t.Cleanup(func() {
		_ = monitorRepo.Delete(ctx, monitor.ID)
		_, _ = integrationDB.Exec(`DELETE FROM usage_logs WHERE request_id LIKE $1`, prefix+"%")
		for _, row := range []struct {
			table string
			id    int64
		}{{"api_keys", key.ID}, {"accounts", account.ID}, {"groups", group.ID}, {"groups", composite.ID}, {"users", user.ID}} {
			_, _ = integrationDB.Exec("DELETE FROM "+row.table+" WHERE id=$1", row.id)
		}
	})
	resetV3Observations(t)
	cfg := saveV3TestConfig(t, service.DefaultChannelMonitorV3Config())
	_, err := integrationDB.Exec(`UPDATE channel_monitor_v3_config SET config=(config-'observation_starts') || '{"probes_enabled":true,"probe_observation_start":"2000-01-01T00:00:00Z"}'::jsonb WHERE id=1`)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Minute).Add(45 * time.Second)
	for i, at := range []time.Time{now.Add(-5*time.Minute - time.Millisecond), now.Add(-5 * time.Minute), now.Add(-5*time.Minute + time.Millisecond), now.Add(-time.Second), now, now.Add(time.Second)} {
		_, err = integrationDB.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,group_id,completion_status,is_complete,created_at) VALUES($1,$2,$3,$4,'model',$5,'completed',true,$6)`, user.ID, key.ID, account.ID, prefix+string(rune('a'+i)), group.ID, at)
		require.NoError(t, err)
	}
	_, err = integrationDB.Exec(`INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,group_id,completion_status,is_complete,created_at) VALUES($1,$2,$3,$4,'composite-model',$5,'completed',true,$6)`, user.ID, key.ID, account.ID, prefix+"-composite", composite.ID, now.Add(-time.Second))
	require.NoError(t, err)
	require.NoError(t, monitorRepo.InsertHistoryBatch(ctx, []*service.ChannelMonitorHistoryRow{{MonitorID: monitor.ID, Model: "model", Status: "degraded", CheckedAt: now.Add(-time.Second)}}))
	repo := NewChannelMonitorV3Repository(integrationDB)
	data, err := repo.Read(ctx, now, 24*time.Hour, "openai")
	require.NoError(t, err)
	var requests, probes int64
	for _, fact := range data.Current {
		if fact.Model == "model" {
			if fact.Source() == "probe" {
				probes += fact.Success
				require.Equal(t, "degraded", fact.ProbeStatus)
				require.Nil(t, fact.P50())
			} else {
				requests += fact.Success
			}
		}
	}
	require.Equal(t, int64(3), requests)
	require.Equal(t, int64(1), probes)
	require.Nil(t, data.DataThrough, "current observations do not depend on the history watermark")
	require.Equal(t, now, *data.CurrentThrough)
	snapshot := service.BuildChannelMonitorV3Snapshot(data, cfg, now, 24*time.Hour, "openai")
	require.Equal(t, now.Add(-5*time.Minute), snapshot.CurrentWindowStart)
	require.Equal(t, now, snapshot.CurrentWindowEnd)
	require.Equal(t, "degraded", snapshot.Platforms[0].Status)
	seenComposite := false
	for _, model := range snapshot.Platforms[0].Models {
		if model.Model == "composite-model" {
			seenComposite = true
			require.Equal(t, "insufficient", model.Status)
		}
	}
	require.True(t, seenComposite, "the first composite result must appear before history exists")
	var states int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM channel_monitor_v3_states`).Scan(&states))
	require.Zero(t, states, "reading current observations must not advance confirmation")
	_, err = integrationDB.Exec(`UPDATE groups SET status='inactive' WHERE id=$1`, composite.ID)
	require.NoError(t, err)
	later, err := repo.Read(ctx, now.Add(20*time.Second), 24*time.Hour, "openai")
	require.NoError(t, err)
	requests = 0
	for _, fact := range later.Current {
		require.NotEqual(t, composite.ID, fact.GroupID)
		if fact.Model == "model" && fact.Source() == "request" {
			requests += fact.Success
		}
	}
	require.Equal(t, int64(3), requests, "two rows at the prior end enter the next exact window while the two oldest expire")
	// A configured model contributes a placeholder before persisted state is
	// loaded. That placeholder must not erase the last real, now expired probe.
	require.NoError(t, repo.Refresh(ctx, now.UTC().Truncate(time.Minute).Add(time.Minute)))
	old, err := repo.Read(ctx, now.Add(6*time.Minute), 24*time.Hour, "openai")
	require.NoError(t, err)
	snapshot = service.BuildChannelMonitorV3Snapshot(old, old.Config, now.Add(6*time.Minute), 24*time.Hour, "openai")
	seenProbe := false
	for _, platform := range snapshot.Platforms {
		for _, model := range platform.Models {
			if model.SourceKind == "probe" && model.MonitorID != nil && *model.MonitorID == monitor.ID {
				seenProbe = true
				require.Equal(t, "unknown", model.Status)
				require.Equal(t, "no_observations", model.ObservationReason)
				require.NotNil(t, model.LastObserved)
				require.Equal(t, now.Add(-time.Second), *model.LastObserved)
			}
		}
	}
	require.True(t, seenProbe)
}
