package repository

import (
	"context"
	"encoding/json"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/setting"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func touchesV3ProbeSettings(values map[string]string) bool {
	for _, key := range []string{service.SettingKeyChannelMonitorEnabled, service.SettingKeyChannelMonitorMode, service.SettingKeyChannelMonitorV1Enabled} {
		if _, ok := values[key]; ok {
			return true
		}
	}
	return false
}

// Persist runtime changes and the probe observation boundary under the same
// config lock used by aggregation. A quick off/on cannot backfill paused probes.
func syncV3ProbeSettings(ctx context.Context, client *dbent.Client, patch map[string]string) error {
	if !touchesV3ProbeSettings(patch) || client.Driver().Dialect() != dialect.Postgres {
		return nil
	}
	rows, err := client.QueryContext(ctx, `SELECT config,clock_timestamp() FROM channel_monitor_v3_config WHERE id=1 FOR UPDATE`)
	if err != nil {
		return err
	}
	var raw []byte
	var now time.Time
	if !rows.Next() {
		err = rows.Err()
		_ = rows.Close()
		return err
	}
	if err = rows.Scan(&raw, &now); err != nil {
		_ = rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	keys := []string{service.SettingKeyChannelMonitorEnabled, service.SettingKeyChannelMonitorMode, service.SettingKeyChannelMonitorV1Enabled}
	existing, err := client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return err
	}
	vals := map[string]string{}
	for _, item := range existing {
		vals[item.Key] = item.Value
	}
	for _, key := range keys {
		if value, ok := patch[key]; ok {
			vals[key] = value
		}
	}
	enabled := vals[service.SettingKeyChannelMonitorEnabled] != "false" && vals[service.SettingKeyChannelMonitorMode] == service.ChannelMonitorModeV3 && vals[service.SettingKeyChannelMonitorV1Enabled] == "true"
	cfg := map[string]any{}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return err
	}
	previous, _ := cfg["probes_enabled"].(bool)
	if previous == enabled {
		return nil
	}
	cfg["probes_enabled"] = enabled
	if enabled {
		cfg["probe_observation_start"] = now.UTC().Truncate(time.Minute).Add(time.Minute)
		states, readErr := readV3States(ctx, client, service.DefaultChannelMonitorV3Config().EnabledPlatforms())
		if readErr != nil {
			return readErr
		}
		for _, item := range states {
			if item.fact.SourceKind != "probe" {
				continue
			}
			next := service.ResumeChannelMonitorV3State(item.state, now)
			if err = writeV3State(ctx, client, item.fact, next, next.Incident); err != nil {
				return err
			}
		}
	}
	raw, err = json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, `UPDATE channel_monitor_v3_config SET config=$1,updated_at=$2 WHERE id=1`, string(raw), now)
	return err
}
