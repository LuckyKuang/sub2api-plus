package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestV024DeploymentDefaults(t *testing.T) {
	example := viper.New()
	example.SetConfigFile("../../../deploy/config.example.yaml")
	require.NoError(t, example.ReadInConfig())
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	for _, host := range []string{"api.minimaxi.com", "api.minimax.io"} {
		require.Contains(t, example.GetStringSlice("security.url_allowlist.upstream_hosts"), host)
	}
	require.Equal(t, cfg.Ops.Cleanup.SystemLogRetentionDays, example.GetInt("ops.cleanup.system_log_retention_days"))
	require.Positive(t, example.GetInt("ops.cleanup.system_log_retention_days"))
}
