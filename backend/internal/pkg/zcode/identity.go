// Package zcode pins the ZCode client identity that Zhipu / GLM platform
// accounts can send upstream, and implements the ZCode platform handshake used
// to link a GLM account.
//
// ZCode is the official open-source client for Zhipu GLM. Every model request it
// makes carries exactly one client declaration built in a single place
// (apps/zcode-cli/packages/bootstrap/src/model-config.ts):
//
//	"User-Agent": `ZCode/${appVersion ?? "unknown"}`
//
// The same helper also attaches ZCode platform attribution and telemetry
// declarations (HTTP-Referer, X-Title, X-Release-Channel, X-Client-Language,
// X-Client-Timezone, X-Platform, X-Os-Category, X-Os-Version) and the Vercel AI
// SDK appends its own runtime fingerprint (`ai-sdk/<pkg>/<ver>`,
// `runtime/node.js/<ver>`) to the User-Agent. None of those are part of this
// identity: the platform attribution headers describe the ZCode product rather
// than the selected preset, and the SDK suffix would claim a JavaScript runtime
// this gateway does not run. Only the versioned product token reaches the wire,
// mirroring the Gemini, Antigravity and DeepSeek rendering rule in
// docs/OUTBOUND_IDENTITY.md.
//
// ZCode declares no Originator and no standalone Version header; the product
// version lives in the User-Agent and in the platform's own
// `X-ZCode-App-Version` declaration, which is likewise not rendered here.
package zcode

import (
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
	return outboundidentity.Identity{
		Preset:     Preset,
		UserAgent:  ua,
		Originator: ProductToken,
		Version:    version,
		Source:     source,
		// Only the User-Agent reaches the wire: ZCode declares no Originator and
		// no standalone version header. See docs/OUTBOUND_IDENTITY.md.
		Headers: map[string]string{"User-Agent": ua},
	}
}
