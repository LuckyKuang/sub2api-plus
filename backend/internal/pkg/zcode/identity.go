// Package zcode implements the ZCode client identity and GLM account-link protocol.
// The declaration block follows the desktop host in ZCode's bootstrap/model-config.ts:
// product attribution, a coherent app-version companion, and persisted host facts.
// Authentication and per-request ticket/session headers remain protocol-owned.
package zcode

import (
	"context"
	"maps"
	"net/http"
	"os"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

const (
	// ProductToken is the ZCode product token. It is the User-Agent product
	// segment and the trusted triple's client identifier.
	ProductToken = "ZCode"

	// Preset is the outbound identity preset key owned by this family.
	Preset = "zcode"

	// DefaultVersion is the pinned ZCode release version.
	//
	// Two official version lines exist upstream: the desktop / server product
	// version (repository root package.json, 3.14.3) and the standalone CLI
	// package version (apps/zcode-cli/package.json, 0.16.9). The desktop product
	// version is pinned because the User-Agent product token names that product
	// and the desktop application is what spawns the agent runtime.
	DefaultVersion = "3.14.3"

	// VersionEnv is the optional operator override for DefaultVersion. It
	// follows the existing SUB2API_CLAUDE_CLI_VERSION /
	// SUB2API_DEEPSEEK_HARNESS_VERSION convention.
	//
	// Both official version lines are accepted, so this preset deliberately
	// declares no monotonic minimum: a semver floor drawn on one line would
	// reject the other line's legitimate official value.
	VersionEnv = "SUB2API_ZCODE_VERSION"
)

// ResolveVersion returns the version this build advertises. Empty or malformed
// values fall back to the compiled pin.
func ResolveVersion() string {
	version := strings.TrimSpace(os.Getenv(VersionEnv))
	if !IsSupportedVersion(version) {
		return DefaultVersion
	}
	return version
}

// IsSupportedVersion reports whether version is a well-formed ZCode client
// version. Every non-empty value of the official `tools/version.ts` and
// `package.json` shape is accepted; the format is enforced here so an operator
// override can never inject arbitrary bytes into the User-Agent.
func IsSupportedVersion(version string) bool {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 64 {
		return false
	}
	// The gateway's shared client-version shape: X.Y.Z with an optional
	// prerelease suffix (for example 3.14.3 or 0.16.9).
	parts := strings.SplitN(version, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return false
	}
	for _, segment := range core {
		if segment == "" || len(segment) > 6 {
			return false
		}
		for _, c := range segment {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	if len(parts) == 1 {
		return true
	}
	suffix := parts[1]
	if suffix == "" || len(suffix) > 32 {
		return false
	}
	for _, c := range suffix {
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '.' {
			return false
		}
	}
	return true
}

// UserAgent builds the ZCode User-Agent value `ZCode/<version>`.
func UserAgent(version string) string {
	if !IsSupportedVersion(version) {
		version = DefaultVersion
	}
	return ProductToken + "/" + version
}

// DefaultIdentity is shared by settings resolution and standalone protocol
// clients so the fallback has the same complete declarations in both paths.
func DefaultIdentity() outboundidentity.Identity {
	version := ResolveVersion()
	source := "compiled_default"
	if IsSupportedVersion(strings.TrimSpace(os.Getenv(VersionEnv))) {
		source = "environment"
	}
	ua := UserAgent(version)
	headers := map[string]string{
		"User-Agent":        ua,
		HeaderAppVersion:    version,
		"HTTP-Referer":      "https://zcode.z.ai",
		"X-Title":           "Z Code@electron",
		"X-Release-Channel": "production",
		"X-ZCode-Agent":     "glm",
	}
	maps.Copy(headers, RuntimeHeaders())
	return outboundidentity.Identity{
		Preset:         Preset,
		UserAgent:      ua,
		Originator:     ProductToken,
		Version:        version,
		Source:         source,
		Headers:        headers,
		ControlHeaders: controlRuntimeHeaders(),
		Inference:      inferenceProfiles(),
	}
}

// HeaderAppVersion must always follow the selected product version.
const HeaderAppVersion = "X-ZCode-App-Version"

// withIdentity captures the credential owner's snapshot before a multi-call
// operation. Existing contexts win over settings; standalone clients resolve once.
func withIdentity(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	identity, ok := outboundidentity.Default(ctx, Preset)
	if !ok {
		identity = DefaultIdentity()
	}
	return outboundidentity.WithIdentity(ctx, identity)
}

func prepareRequest(req *http.Request) {
	*req = *req.WithContext(withIdentity(req.Context()))
	identity, _ := outboundidentity.FromContext(req.Context())
	*req = *req.WithContext(outboundidentity.WithIdentity(req.Context(), ControlIdentity(identity)))
	outboundidentity.ApplyContext(req)
}

// ControlIdentity follows services/sourceHeaders.ts, not model-config.ts.
// X-ZCode-Agent belongs only to inference; a missing telemetry device ID is
// omitted rather than invented. Keep all chosen product/runtime declarations.
func ControlIdentity(identity outboundidentity.Identity) outboundidentity.Identity {
	identity.Headers = maps.Clone(identity.Headers)
	delete(identity.Headers, "X-ZCode-Agent")
	maps.Copy(identity.Headers, identity.ControlHeaders)
	return identity
}

// runner-options passes bootstrap headers to ai, overriding the provider UA
// suffix with ai/<version>; provider-utils then appends its transport suffix. Pin
// the reviewed lockfile versions and bundled Node host (prepare-prebuilds.mjs).
// Node 22 exposes navigator.userAgent=Node.js/22, preferred by provider-utils.
func inferenceProfiles() map[string]outboundidentity.WireProfile {
	return map[string]outboundidentity.WireProfile{
		"anthropic":        {UserAgentSuffix: "ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22"},
		"chat_completions": {UserAgentSuffix: "ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22"},
		"responses":        {UserAgentSuffix: "ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22"},
	}
}
