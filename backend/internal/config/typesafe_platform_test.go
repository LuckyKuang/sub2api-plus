//go:build unit || !integration

package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

// TypeSafe 是新的默认上游主机：启用 URL 白名单时 api.typesafe.ai 必须同时存在于
// 内置默认列表和 deploy 示例，否则加固部署里的 System One 平台不可达。
func TestTypeSafeDefaultUpstreamHostIsAllowlisted(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Contains(t, cfg.Security.URLAllowlist.UpstreamHosts, "api.typesafe.ai")

	example := viper.New()
	example.SetConfigFile("../../../deploy/config.example.yaml")
	require.NoError(t, example.ReadInConfig())
	require.Contains(t, example.GetStringSlice("security.url_allowlist.upstream_hosts"), "api.typesafe.ai")
}
