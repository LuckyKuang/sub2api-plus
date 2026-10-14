//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// docs/CHANNEL_MONITOR.md: successful observed bytes have their own denominator;
// failed observations stay inspectable but cannot lower the performance average.
func TestChannelMonitorFirstBytePersistenceAndSuccessfulAverage(t *testing.T) {
	ctx := context.Background()
	repo := NewChannelMonitorRepository(integrationEntClient, integrationDB)
	monitor := &service.ChannelMonitor{Name: "byte-" + uuid.NewString(), Provider: "openai", APIMode: "chat_completions", Endpoint: "https://example.test", APIKey: "test", PrimaryModel: "model", Enabled: true, IntervalSeconds: 60}
	require.NoError(t, repo.Create(ctx, monitor))
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, monitor.ID)) })
	now := time.Now().UTC()
	zero, thousand, failedByte := 0, 1000, 2
	text, failedText, stream := 2400, 5, true
	textValues := []*int{nil, &zero, &text, &failedText, &failedText}
	values := []*int{nil, &zero, &thousand, &failedByte, &failedByte}
	statuses := []string{"operational", "operational", "degraded", "error", "failed"}
	rows := make([]*service.ChannelMonitorHistoryRow, 0, 5)
	for i := 0; i < 5; i++ {
		total := (i + 1) * 1000
		rows = append(rows, &service.ChannelMonitorHistoryRow{MonitorID: monitor.ID, Model: "model", Status: statuses[i], FirstByteMs: values[i], FirstTextMs: textValues[i], StreamProbe: &stream, LatencyMs: &total, CheckedAt: now.Add(time.Duration(i-5) * time.Second)})
	}
	require.NoError(t, repo.InsertHistoryBatch(ctx, rows))
	history, err := repo.ListHistory(ctx, monitor.ID, "model", 10)
	require.NoError(t, err)
	require.Len(t, history, 5)
	require.Nil(t, history[4].FirstByteMs)
	require.Equal(t, 0, *history[3].FirstByteMs)
	require.Equal(t, 1000, *history[2].FirstByteMs)
	require.Equal(t, 2, *history[0].FirstByteMs)
	require.Nil(t, history[4].FirstTextMs)
	require.Equal(t, 0, *history[3].FirstTextMs)
	require.Equal(t, 2400, *history[2].FirstTextMs)
	require.True(t, *history[0].StreamProbe)
	latest, err := repo.ListLatestPerModel(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, 2, *latest[0].FirstByteMs)
	require.Equal(t, 5, *latest[0].FirstTextMs)
	require.True(t, *latest[0].StreamProbe)
	batch, err := repo.ListLatestForMonitorIDs(ctx, []int64{monitor.ID})
	require.NoError(t, err)
	require.Equal(t, 2, *batch[monitor.ID][0].FirstByteMs)
	require.Equal(t, 5, *batch[monitor.ID][0].FirstTextMs)
	require.True(t, *batch[monitor.ID][0].StreamProbe)
	timeline, err := repo.ListRecentHistoryForMonitors(ctx, []int64{monitor.ID}, map[int64]string{monitor.ID: "model"}, 10)
	require.NoError(t, err)
	require.Equal(t, 2, *timeline[monitor.ID][0].FirstByteMs)
	require.Equal(t, 5, *timeline[monitor.ID][0].FirstTextMs)
	require.True(t, *timeline[monitor.ID][0].StreamProbe)
	availability, err := repo.ComputeAvailability(ctx, monitor.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 500, *availability[0].AvgFirstByteMs)
	require.Equal(t, 2, availability[0].FirstByteSamples)
	require.Equal(t, 1200, *availability[0].AvgFirstTextMs)
	require.Equal(t, 2, availability[0].FirstTextSamples)
	require.Equal(t, 3000, *availability[0].AvgLatencyMs)
	require.Equal(t, 60.0, availability[0].AvailabilityPct)
	batchAvailability, err := repo.ComputeAvailabilityForMonitors(ctx, []int64{monitor.ID}, 7)
	require.NoError(t, err)
	require.Equal(t, 500, *batchAvailability[monitor.ID][0].AvgFirstByteMs)
	require.Equal(t, 2, batchAvailability[monitor.ID][0].FirstByteSamples)
	require.Equal(t, 1200, *batchAvailability[monitor.ID][0].AvgFirstTextMs)
	require.Equal(t, 2, batchAvailability[monitor.ID][0].FirstTextSamples)
	_, err = repo.UpsertDailyRollupsFor(ctx, now)
	require.NoError(t, err)
	var sum, count int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT sum_first_byte_ms,count_first_byte FROM channel_monitor_daily_rollups WHERE monitor_id=$1`, monitor.ID).Scan(&sum, &count))
	require.Equal(t, int64(1000), sum)
	require.Equal(t, int64(2), count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT sum_first_text_ms,count_first_text FROM channel_monitor_daily_rollups WHERE monitor_id=$1`, monitor.ID).Scan(&sum, &count))
	require.Equal(t, int64(2400), sum)
	require.Equal(t, int64(2), count)
}

// Execute the real migration on pre-existing rows in isolated temporary tables.
// The repository harness separately applies every migration to a fresh database.
func TestFirstByteMigrationUpgradePreservesUnknownAndAllowsZero(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE usage_logs(marker INTEGER); CREATE TEMP TABLE channel_monitor_histories(marker INTEGER); CREATE TEMP TABLE channel_monitor_daily_rollups(marker INTEGER); INSERT INTO usage_logs VALUES(1); INSERT INTO channel_monitor_histories VALUES(1); INSERT INTO channel_monitor_daily_rollups VALUES(1)`)
	require.NoError(t, err)
	raw, err := migrations.FS.ReadFile("285_first_byte_timing.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(raw))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(raw))
	require.NoError(t, err)
	var unknown bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT first_byte_ms IS NULL FROM usage_logs WHERE marker=1`).Scan(&unknown))
	require.True(t, unknown)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT first_byte_ms IS NULL FROM channel_monitor_histories WHERE marker=1`).Scan(&unknown))
	require.True(t, unknown)
	var sum, count int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT sum_first_byte_ms,count_first_byte FROM channel_monitor_daily_rollups WHERE marker=1`).Scan(&sum, &count))
	require.Zero(t, sum)
	require.Zero(t, count)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs(marker,first_byte_ms) VALUES(2,0)`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs(marker,first_byte_ms) VALUES(3,-1)`)
	require.Error(t, err)
}

// The latest probe means latest checked_at, then latest inserted result.
// V1 cards, timelines and V3 current observations must agree on a time tie.
func TestChannelMonitorLatestFirstByteAndStatusUseInsertionOrderOnTimeTie(t *testing.T) {
	ctx := context.Background()
	repo := NewChannelMonitorRepository(integrationEntClient, integrationDB)
	monitor := &service.ChannelMonitor{Name: "byte-tie-" + uuid.NewString(), Provider: "openai", APIMode: "chat_completions", Endpoint: "https://example.test", APIKey: "test", PrimaryModel: "model", Enabled: true, IntervalSeconds: 60}
	require.NoError(t, repo.Create(ctx, monitor))
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, monitor.ID)) })
	resetV3Observations(t)
	saveV3TestConfig(t, service.DefaultChannelMonitorV3Config())
	_, err := integrationDB.Exec(`UPDATE channel_monitor_v3_config SET config=(config-'observation_starts') || '{"probes_enabled":true,"probe_observation_start":"2000-01-01T00:00:00Z"}'::jsonb WHERE id=1`)
	require.NoError(t, err)
	now := time.Now().UTC()
	at := now.Add(-time.Second)
	for _, value := range []struct {
		status    string
		firstByte int
	}{{"operational", 125}, {"error", 250}} {
		byteMs := value.firstByte
		require.NoError(t, repo.InsertHistoryBatch(ctx, []*service.ChannelMonitorHistoryRow{{MonitorID: monitor.ID, Model: "model", Status: value.status, FirstByteMs: &byteMs, CheckedAt: at}}))
	}
	history, err := repo.ListHistory(ctx, monitor.ID, "model", 2)
	require.NoError(t, err)
	require.Equal(t, "error", history[0].Status)
	require.Equal(t, 250, *history[0].FirstByteMs)
	latest, err := repo.ListLatestPerModel(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, "error", latest[0].Status)
	require.Equal(t, 250, *latest[0].FirstByteMs)
	batch, err := repo.ListLatestForMonitorIDs(ctx, []int64{monitor.ID})
	require.NoError(t, err)
	require.Equal(t, "error", batch[monitor.ID][0].Status)
	require.Equal(t, 250, *batch[monitor.ID][0].FirstByteMs)
	timeline, err := repo.ListRecentHistoryForMonitors(ctx, []int64{monitor.ID}, map[int64]string{monitor.ID: "model"}, 2)
	require.NoError(t, err)
	require.Equal(t, "error", timeline[monitor.ID][0].Status)
	require.Equal(t, 250, *timeline[monitor.ID][0].FirstByteMs)
	data, err := NewChannelMonitorV3Repository(integrationDB).Read(ctx, now, 24*time.Hour, "openai")
	require.NoError(t, err)
	var observed bool
	for _, fact := range data.Current {
		if fact.Source() == "probe" && fact.MonitorID == monitor.ID {
			observed = true
			require.Equal(t, "error", fact.ProbeStatus)
			require.Equal(t, "outage", service.ChannelMonitorV3Health(fact, service.DefaultChannelMonitorV3Config()))
		}
	}
	require.True(t, observed)
}

func (s *UsageLogRepoSuite) TestFirstByteNullZeroAndObservedRoundTrip() {
	user := mustCreateUser(s.T(), s.client, &service.User{Email: "bytes-" + uuid.NewString() + "@example.test"})
	key := mustCreateApiKey(s.T(), s.client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "bytes"})
	account := mustCreateAccount(s.T(), s.client, &service.Account{Name: "bytes"})
	zero, observed, token := 0, 125, 2500
	for _, value := range []*int{nil, &zero, &observed} {
		log := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "model", FirstByteMs: value, FirstTokenMs: &token, TimingVersion: 1}
		_, err := s.repo.Create(s.ctx, log)
		s.Require().NoError(err)
		stored, err := s.repo.GetByID(s.ctx, log.ID)
		s.Require().NoError(err)
		s.Equal(value, stored.FirstByteMs)
		s.Equal(2500, *stored.FirstTokenMs)
	}
}
