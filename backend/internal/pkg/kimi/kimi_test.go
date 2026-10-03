//go:build unit

package kimi

import (
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// resetDeviceFacts clears the per-process memo so a test can observe a fresh
// resolution.
func resetDeviceFacts() {
	deviceFactsOnce = sync.Once{}
	deviceFactsValue = DeviceFacts{}
	defaultDeviceIDOnce = sync.Once{}
	defaultDeviceIDValue = ""
}

func TestDefaultIdentityRendersTheOfficialDeclarationSet(t *testing.T) {
	t.Setenv(VersionEnv, "")
	resetDeviceFacts()

	identity := DefaultIdentity()

	require.Equal(t, "kimi", identity.Preset)
	require.Equal(t, "kimi-code-cli/"+DefaultVersion, identity.UserAgent)
	require.Equal(t, "kimi-code-cli", identity.Originator)
	require.Equal(t, DefaultVersion, identity.Version)
	require.Equal(t, "compiled_default", identity.Source)

	// The exact wire declaration set: the User-Agent plus the platform, version
	// and device companions. The official client declares no Originator and no
	// standalone Version header, so neither may reach the wire.
	require.Len(t, identity.Headers, 7)
	require.Equal(t, identity.UserAgent, identity.Headers["User-Agent"])
	require.Equal(t, "kimi_code_cli", identity.Headers[HeaderPlatform])
	require.Equal(t, DefaultVersion, identity.Headers[HeaderVersion])
	// The platform token is underscored while the product token is dashed.
	require.NotEqual(t, identity.Originator, identity.Headers[HeaderPlatform])
	require.NotContains(t, identity.Headers, "Originator")
	require.NotContains(t, identity.Headers, "Version")
	for _, name := range DeviceHeaders() {
		require.NotEmpty(t, identity.Headers[name], name)
	}
	deviceID, err := uuid.Parse(identity.Headers[HeaderDeviceID])
	require.NoError(t, err, "the device id must be a uuid")
	require.Equal(t, deviceID.String(), identity.Headers[HeaderDeviceID])
}

func TestDefaultIdentityReusesOneDeviceIDPerProcess(t *testing.T) {
	resetDeviceFacts()
	first := DefaultIdentity().Headers[HeaderDeviceID]
	second := DefaultIdentity().Headers[HeaderDeviceID]
	require.Equal(t, first, second)
	require.Equal(t, first, DefaultDeviceID())
}

func TestDeviceDeclarationsMirrorTheOfficialSanitizer(t *testing.T) {
	facts := DeviceFacts{Name: "  kimi\tbox  ", Model: "Linux 6.14.0 x64", OSVersion: "6.14.0", DeviceID: "  "}
	headers := DeviceDeclarations(facts)
	require.Equal(t, "kimibox", headers[HeaderDeviceName])
	require.Equal(t, "Linux 6.14.0 x64", headers[HeaderDeviceModel])
	require.Equal(t, "6.14.0", headers[HeaderOSVersion])
	// An absent device id is minted rather than sent blank.
	_, err := uuid.Parse(headers[HeaderDeviceID])
	require.NoError(t, err)
}

func TestASCIIHeaderFallsBackToUnknown(t *testing.T) {
	require.Equal(t, "unknown", asciiHeader(""))
	require.Equal(t, "unknown", asciiHeader("   "))
	require.Equal(t, "unknown", asciiHeader("桌面"))
	require.Equal(t, "ho st", asciiHeader(" ho st "))
}

func TestNodeArchReportsTheNodeToken(t *testing.T) {
	require.Equal(t, "x64", nodeArchFor("amd64"))
	require.Equal(t, "ia32", nodeArchFor("386"))
	require.Equal(t, "mipsel", nodeArchFor("mipsle"))
	require.Equal(t, "arm64", nodeArchFor("arm64"))
	require.Equal(t, "riscv64", nodeArchFor("riscv64"))
	require.Equal(t, nodeArchFor(runtime.GOARCH), nodeArch())
}

func TestJoinDeviceModelSkipsUnresolvedComponents(t *testing.T) {
	require.Equal(t, "Linux 6.14.0 x64", joinDeviceModel("Linux", "6.14.0", "x64"))
	require.Equal(t, "Linux x64", joinDeviceModel("Linux", "", "x64"))
	require.Equal(t, "", joinDeviceModel("", "", ""))
	require.Equal(t, "Darwin 24.3.0 arm64", joinDeviceModel("Darwin", "24.3.0", "arm64"))
}

func TestResolveVersionHonorsTheOverrideAndTheFloor(t *testing.T) {
	t.Setenv(VersionEnv, "2.4.0")
	require.Equal(t, "2.4.0", ResolveVersion())
	require.Equal(t, "environment", DefaultIdentity().Source)

	// The CLI, the `kimi web` server and the native binaries share one released
	// version line, so an older or malformed candidate falls back to the pin.
	for _, version := range []string{"", "2.1", "2.0.9", "v2.1.1", "2.1.1.1", "not-a-version"} {
		t.Setenv(VersionEnv, version)
		require.False(t, IsSupportedVersion(version), version)
		require.Equal(t, DefaultVersion, ResolveVersion(), version)
	}
	t.Setenv(VersionEnv, "2.1.1")
	require.True(t, IsSupportedVersion("2.1.1"))
	require.Equal(t, "environment", DefaultIdentity().Source)
}

func TestUserAgentFallsBackToThePinnedVersion(t *testing.T) {
	require.Equal(t, "kimi-code-cli/"+DefaultVersion, UserAgent(""))
	require.Equal(t, "kimi-code-cli/9.9.9", UserAgent("9.9.9"))
	require.True(t, strings.HasPrefix(UserAgent(""), ProductToken+"/"))
}

func TestResolveDeviceFactsReadsOncePerProcess(t *testing.T) {
	resetDeviceFacts()
	first := ResolveDeviceFacts()
	require.Equal(t, first, ResolveDeviceFacts())
	require.Equal(t, first.DeviceID, DefaultDeviceID())
	if runtime.GOOS == "linux" {
		// Linux is the platform every supported deployment and every validation
		// container runs on, so the kernel release must resolve there.
		require.NotEmpty(t, first.OSVersion)
		require.Contains(t, first.Model, first.OSVersion)
		require.NotEmpty(t, first.Name)
	}
}
