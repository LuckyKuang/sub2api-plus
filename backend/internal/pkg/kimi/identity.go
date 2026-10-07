// Package kimi pins the Kimi Code client identity that Kimi / Moonshot platform
// accounts advertise upstream.
//
// The published Kimi Code CLI declares one product token plus a device
// description set on every first-party provider request
// (MoonshotAI/kimi-code packages/oauth/src/identity.ts,
// packages/agent-core-v2/src/llm-adapter/model/catalog-service.ts):
//
//	User-Agent: kimi-code-cli/<version>
//	X-Msh-Platform: kimi_code_cli
//	X-Msh-Version: <version>
//	X-Msh-Device-Name: <hostname>
//	X-Msh-Device-Model: <os type> <os version> <arch>
//	X-Msh-Os-Version: <kernel release>
//	X-Msh-Device-Id: <per-install uuid v4>
//
// The engine attaches that whole set only to the first-party `kimi` provider,
// which is registered with `hostHeaders: 'full'` for all three protocols
// (packages/agent-core-v2/src/llm-adapter/provider/provider-definition.ts);
// every other provider receives only a User-Agent whose product token is
// rewritten to the declared agent slug. The official client declares no
// `Originator` and no standalone `Version` header, so neither reaches the wire
// — matching the Gemini/Antigravity/DeepSeek/ZCode rendering rule in
// docs/OUTBOUND_IDENTITY.md.
//
// The product token uses dashes while the platform token uses underscores
// (`kimi-code-cli` vs `kimi_code_cli`). Both are load-bearing: the platform
// value was corrected to the underscored form upstream and the two are not
// interchangeable.
//
// The official client ships several hosts that share the product token and
// differ only by `X-Msh-Platform` / UA suffix: `kimi_code_cli` (CLI, and the
// `kimi web` server with the `(web)` UA suffix), `kimi_code_desktop`, and a
// separate `kimi-code-vscode` product token for the VS Code extension. This
// package pins the CLI family; the other hosts state their own platform
// explicitly and are not inherited silently.
//
// The gateway defaults to a fixed Ubuntu 24.04 x86_64 client named ubuntu.
// The official header formats and persistent device UUID are preserved;
// explicit global/account declarations remain configurable.
package kimi

import (
	"maps"
	"os"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

const (
	// ProductToken is the User-Agent product segment and the trusted triple's
	// client identifier.
	ProductToken = "kimi-code-cli"

	// PlatformToken is the official `X-Msh-Platform` declaration for this host
	// family. It differs from ProductToken by separator on purpose.
	PlatformToken = "kimi_code_cli"

	// Preset is the outbound identity preset key owned by this family.
	Preset = "kimi"

	// DefaultVersion is the pinned published Kimi Code version.
	DefaultVersion = "2.1.1"

	// StableVersion is the oldest client version this build advertises. The CLI,
	// the `kimi web` server and the native binaries share one released version
	// line, so a monotonic floor is safe here; the VS Code extension is a
	// different product token and is not a candidate for this preset.
	StableVersion = DefaultVersion

	// VersionEnv is the optional operator override for DefaultVersion. It
	// follows the existing SUB2API_CLAUDE_CLI_VERSION / XAI_GROK_CLI_VERSION
	// convention; empty or invalid values fall back to the compiled pin.
	VersionEnv = "SUB2API_KIMI_CODE_VERSION"

	// HeaderPlatform declares the client family.
	HeaderPlatform = "X-Msh-Platform"
	// HeaderVersion declares the client version and always equals the version
	// carried by the User-Agent.
	HeaderVersion = "X-Msh-Version"
	// HeaderDeviceName declares the host name the client runs on.
	HeaderDeviceName = "X-Msh-Device-Name"
	// HeaderDeviceModel declares the host operating system.
	HeaderDeviceModel = "X-Msh-Device-Model"
	// HeaderOSVersion declares the host kernel release.
	HeaderOSVersion = "X-Msh-Os-Version"
	// HeaderDeviceID declares the persisted per-install device id.
	HeaderDeviceID = "X-Msh-Device-Id"

	unknownFact = "unknown"
)

// DeviceHeaders lists the runtime device declarations in the order the official
// client renders them.
func DeviceHeaders() []string {
	return []string{HeaderDeviceName, HeaderDeviceModel, HeaderOSVersion, HeaderDeviceID}
}

// ResolveVersion returns the supported version this build advertises.
func ResolveVersion() string {
	version := strings.TrimSpace(os.Getenv(VersionEnv))
	if !IsSupportedVersion(version) {
		return DefaultVersion
	}
	return version
}

// IsSupportedVersion reports whether version is a canonical semver at or above
// StableVersion. Prereleases below a higher release compare lower and are
// therefore rejected, matching the existing Claude/Grok/DeepSeek policy.
func IsSupportedVersion(version string) bool {
	canonical := "v" + strings.TrimSpace(version)
	minimum := "v" + StableVersion
	return semver.IsValid(canonical) &&
		semver.Canonical(canonical) == canonical &&
		semver.Compare(canonical, minimum) >= 0
}

// UserAgent builds the published Kimi Code User-Agent value
// `kimi-code-cli/<version>`.
func UserAgent(version string) string {
	if strings.TrimSpace(version) == "" {
		version = DefaultVersion
	}
	return ProductToken + "/" + version
}

// DefaultIdentity is the compiled declaration set: the trusted triple plus the
// version companion and the fixed Ubuntu device set. The service
// layer overlays the persisted runtime values on top; this function stays free
// of configuration state so settings resolution and protocol clients share one
// fallback.
func DefaultIdentity() outboundidentity.Identity {
	version := ResolveVersion()
	source := "compiled_default"
	if IsSupportedVersion(strings.TrimSpace(os.Getenv(VersionEnv))) {
		source = "environment"
	}
	ua := UserAgent(version)
	headers := map[string]string{
		"User-Agent":   ua,
		HeaderPlatform: PlatformToken,
		HeaderVersion:  version,
	}
	maps.Copy(headers, DeviceDeclarations(ResolveDeviceFacts()))
	return outboundidentity.Identity{
		Preset:     Preset,
		UserAgent:  ua,
		Originator: ProductToken,
		Version:    version,
		Source:     source,
		Headers:    headers,
		Inference:  inferenceProfiles(),
	}
}

// These SDK versions come from kimi-code/pnpm-lock.yaml. Product/device
// declarations are shared with OAuth; the SDK block is inference-only.
func inferenceProfiles() map[string]outboundidentity.WireProfile {
	profiles := map[string]outboundidentity.WireProfile{}
	for protocol, version := range map[string]string{"anthropic": "0.95.2", "chat_completions": "6.34.0", "responses": "6.34.0"} {
		profiles[protocol] = outboundidentity.WireProfile{Headers: map[string]string{
			"X-Stainless-Lang": "js", "X-Stainless-Package-Version": version,
			"X-Stainless-OS": "Linux", "X-Stainless-Arch": "x64",
			"X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.19.0",
		}}
	}
	return profiles
}
