//go:build unit || !integration

package config

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZhipuOffPeakPersistenceConfiguration(t *testing.T) {
	t.Run("default remains explicitly process local", func(t *testing.T) {
		resetViperWithJWTSecret(t)
		cfg, err := Load()
		require.NoError(t, err)
		require.False(t, cfg.ZhipuOffPeak.PersistenceEnabled)
		require.Equal(t, 168, cfg.ZhipuOffPeak.SnapshotRetentionHours)
	})
	for _, kind := range []string{"missing-key", "wrong-length", "bad-json", "missing-active", "retention"} {
		t.Run(kind, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("ZHIPU_OFFPEAK_PERSISTENCE_ENABLED", "true")
			t.Setenv("ZHIPU_OFFPEAK_ACTIVE_KEY_ID", "key-a")
			key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
			ring := `{"key-a":"` + key + `"}`
			switch kind {
			case "missing-key":
				ring = ""
			case "wrong-length":
				ring = `{"key-a":"a2V5"}`
			case "bad-json":
				ring = "secret-malformed-json"
			case "missing-active":
				t.Setenv("ZHIPU_OFFPEAK_ACTIVE_KEY_ID", "missing")
			case "retention":
				t.Setenv("ZHIPU_OFFPEAK_SNAPSHOT_RETENTION_HOURS", "0")
			}
			t.Setenv("ZHIPU_OFFPEAK_SNAPSHOT_KEYS", ring)
			_, err := Load()
			require.Error(t, err)
			require.NotContains(t, err.Error(), key)
			require.NotContains(t, err.Error(), "secret-malformed-json")
		})
	}
	t.Run("stable env keyring and retention", func(t *testing.T) {
		resetViperWithJWTSecret(t)
		t.Setenv("ZHIPU_OFFPEAK_PERSISTENCE_ENABLED", "true")
		t.Setenv("ZHIPU_OFFPEAK_ACTIVE_KEY_ID", "key-a")
		t.Setenv("ZHIPU_OFFPEAK_SNAPSHOT_KEYS", `{"key-a":"`+base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))+`"}`)
		t.Setenv("ZHIPU_OFFPEAK_SNAPSHOT_RETENTION_HOURS", "24")
		cfg, err := Load()
		require.NoError(t, err)
		require.True(t, cfg.ZhipuOffPeak.PersistenceEnabled)
		require.Equal(t, 24, cfg.ZhipuOffPeak.SnapshotRetentionHours)
		keys, err := cfg.ZhipuOffPeak.Keyring()
		require.NoError(t, err)
		require.Len(t, keys["key-a"], 32)
	})
}
