//go:build integration

package repository

import (
	"context"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestChannelMonitorStreamMigrationUpgrade(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE channel_monitors(id INT,provider TEXT,check_mode TEXT,body_override_mode TEXT,body_override JSONB);
 CREATE TEMP TABLE channel_monitor_request_templates(id INT,provider TEXT,body_override_mode TEXT,body_override JSONB);
 CREATE TEMP TABLE channel_monitor_histories(first_byte_ms INT,latency_ms INT);
 CREATE TEMP TABLE channel_monitor_daily_rollups(marker INT);
 INSERT INTO channel_monitors VALUES
 (1,'openai','probe','replace','{"stream":true,"input":"ok"}'),
 (2,'openai','probe','merge','{"stream":true}'),
 (3,'openai','probe','off','{"stream":true}'),
 (4,'anthropic','probe','replace','{"stream":true}'),
 (5,'openai','quota','replace','{"stream":true}'),
 (6,'grok','quota_probe','replace','{"stream":true}'),
 (7,'openai','probe','replace','{"stream":"true"}');
 INSERT INTO channel_monitor_request_templates VALUES(1,'openai','replace','{"stream":true,"input":"ok"}'),(2,'openai','merge','{"stream":true}');
 INSERT INTO channel_monitor_histories VALUES(17,1000);INSERT INTO channel_monitor_daily_rollups VALUES(1);`)
	require.NoError(t, err)
	raw, err := migrations.FS.ReadFile("287_channel_monitor_stream_probe.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = tx.ExecContext(ctx, string(raw))
		require.NoError(t, err)
	}
	for id, want := range map[int]bool{1: true, 2: false, 3: false, 4: false, 5: false, 6: true, 7: false} {
		var got bool
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT stream_probe FROM channel_monitors WHERE id=$1`, id).Scan(&got))
		require.Equal(t, want, got)
	}
	var preserved, missing, enabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT body_override='{"input":"ok"}'::jsonb FROM channel_monitors WHERE id=1`).Scan(&preserved))
	require.True(t, preserved)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT stream_probe,body_override='{"input":"ok"}'::jsonb FROM channel_monitor_request_templates WHERE id=1`).Scan(&enabled, &preserved))
	require.True(t, enabled)
	require.True(t, preserved)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT first_text_ms IS NULL AND stream_probe IS NULL AND first_byte_ms=17 AND latency_ms=1000 FROM channel_monitor_histories`).Scan(&missing))
	require.True(t, missing)
	var sum, count int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT sum_first_text_ms,count_first_text FROM channel_monitor_daily_rollups`).Scan(&sum, &count))
	require.Zero(t, sum)
	require.Zero(t, count)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_histories(first_text_ms,stream_probe) VALUES(0,true)`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_histories(first_text_ms) VALUES(-1)`)
	require.Error(t, err)
}

func TestChannelMonitorStreamingTemplateSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := NewChannelMonitorRepository(integrationEntClient, integrationDB)
	templates := NewChannelMonitorRequestTemplateRepository(integrationEntClient, integrationDB)
	tpl := &service.ChannelMonitorRequestTemplate{Name: "stream-template", Provider: "openai", APIMode: "responses", StreamProbe: true, BodyOverrideMode: "off"}
	require.NoError(t, templates.Create(ctx, tpl))
	t.Cleanup(func() { require.NoError(t, templates.Delete(ctx, tpl.ID)) })
	got, err := templates.GetByID(ctx, tpl.ID)
	require.NoError(t, err)
	require.True(t, got.StreamProbe)
	monitor := &service.ChannelMonitor{Name: "stream-snapshot", Provider: "openai", APIMode: "responses", Endpoint: "https://example.test", APIKey: "test", PrimaryModel: "model", IntervalSeconds: 60, TemplateID: &tpl.ID}
	require.NoError(t, repo.Create(ctx, monitor))
	t.Cleanup(func() { require.NoError(t, repo.Delete(ctx, monitor.ID)) })
	count, err := templates.ApplyToMonitors(ctx, tpl.ID, []int64{monitor.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	loaded, err := repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.True(t, loaded.StreamProbe)
	tpl.StreamProbe = false
	require.NoError(t, templates.Update(ctx, tpl))
	loaded, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.True(t, loaded.StreamProbe, "templates are snapshots, not live references")
	count, err = templates.ApplyToMonitors(ctx, tpl.ID, []int64{monitor.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	loaded, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.False(t, loaded.StreamProbe)
	loaded.StreamProbe = true
	require.NoError(t, repo.Update(ctx, loaded))
	loaded, err = repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.True(t, loaded.StreamProbe)
}
