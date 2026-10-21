//go:build unit || !integration

package config

import (
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"testing"
)

// Official MiniMax Code uses separate account and quota origins for the same
// OAuth grant. Hardened defaults and the shipped example must permit the chain.
func TestMiniMaxOAuthQuotaDefaultAndExampleHosts(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	example := viper.New()
	example.SetConfigFile("../../../deploy/config.example.yaml")
	require.NoError(t, example.ReadInConfig())
	for _, host := range []string{"agent.minimaxi.com", "www.minimaxi.com", "agent.minimax.io", "platform.minimax.io"} {
		require.Contains(t, cfg.Security.URLAllowlist.UpstreamHosts, host)
		require.Contains(t, example.GetStringSlice("security.url_allowlist.upstream_hosts"), host)
	}
}
