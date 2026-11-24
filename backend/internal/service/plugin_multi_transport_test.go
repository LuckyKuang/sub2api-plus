//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginManifestV2MultiTransport(t *testing.T) {
	manifest := validRequestManifest()
	manifest.Capabilities = []PluginCapability{{
		ID: PluginCapabilityOutboundTransport, Platform: PlatformStepFun, AccountType: AccountTypeOAuth,
	}}
	manifest.Runtimes = map[string]PluginRuntime{manifest.RuntimeKey(): {Path: "bin/plugin"}}
	manifest.UI = PluginUIManifest{Entrypoint: "ui/index.html"}
	manifest.Files = map[string]string{
		"bin/plugin":    strings.Repeat("a", 64),
		"ui/index.html": strings.Repeat("b", 64),
	}
	require.NoError(t, manifest.Validate())
	require.False(t, manifest.IsRequestOnly())
	require.True(t, manifestHasMultiTransportCapability(manifest))

	manifest.Capabilities[0].Platform = "not-a-platform"
	require.ErrorContains(t, manifest.Validate(), "具体平台")
}

func TestPluginTransportRouteMatchingAndConflicts(t *testing.T) {
	manager := &PluginManager{multiRuntimes: map[int64]*pluginRuntime{}}
	installations := []*PluginInstallation{{
		ID: 1,
		Bindings: []PluginBinding{{
			ID: 11, Capability: PluginCapabilityOutboundTransport,
			Platform: PlatformStepFun, AccountType: AccountTypeOAuth,
			Enabled: true, RolloutPercent: 100,
		}},
	}}
	manager.publishMultiTransportRoutes(installations)
	routes, _ := manager.transportRoutes.Load().([]pluginTransportRoute)
	require.Len(t, routes, 1)
	require.Equal(t, "插件进程未运行", routes[0].unavailable)

	request := httptest.NewRequest("POST", "https://example.test/chat/completions", nil)
	_, handled, err := manager.RoundTripTransport(context.Background(), request, "", &Account{ID: 1, Platform: PlatformStepFun, Type: AccountTypeOAuth})
	require.True(t, handled)
	require.ErrorContains(t, err, "插件进程未运行")
	_, handled, err = manager.RoundTripTransport(context.Background(), request, "", &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.False(t, handled)
	require.NoError(t, err)

	installations = append(installations, &PluginInstallation{
		ID: 2,
		Bindings: []PluginBinding{{
			ID: 21, Capability: PluginCapabilityOutboundTransport,
			Platform: PlatformStepFun, AccountType: AccountTypeOAuth,
			Enabled: true, RolloutPercent: 100,
		}},
	})
	manager.publishMultiTransportRoutes(installations)
	routes, _ = manager.transportRoutes.Load().([]pluginTransportRoute)
	require.Len(t, routes, 2)
	for _, route := range routes {
		require.Contains(t, route.unavailable, "多个启用的传输插件")
	}
}
