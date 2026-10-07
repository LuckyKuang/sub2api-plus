// Package deepseek pins the DeepSeek Harness client identity that DeepSeek
// platform accounts can send upstream.
//
// The published harness renders exactly one provider declaration, the
// User-Agent value `product/version (+url)`, from its own package manifest
// (deepseek-harness packages/llm/llm/src/attribution.ts). This package freezes
// that value in-binary so Sub2API Plus pins one fingerprint instead of tracking
// an upstream release, mirroring the existing pins in internal/pkg/claude,
// internal/pkg/geminicli, internal/pkg/xai and internal/pkg/antigravity.
//
// The trusted triple stays complete and coherent: UserAgent carries the product
// token and version, Originator is the client identifier and Version is the
// client version. Only the User-Agent declaration reaches the wire, matching
// the official client and the Gemini/Antigravity rendering rule in
// docs/OUTBOUND_IDENTITY.md. The per-request harness headers
// (x-deepseek-harness-user-id / -session-id / -compact) are request state owned
// by the protocol layer and are never identity declarations.
package deepseek

import (
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

const (
	// ClientIdentifier is the harness product token. It is the User-Agent
	// product segment and the trusted triple's client identifier.
	ClientIdentifier = "deepseek-harness"

	// HomeURL is rendered as the User-Agent product comment (`+url`).
	HomeURL = "https://github.com/deepseek-ai/deepseek-harness"

	// DefaultVersion is the pinned published harness version. The parenthesized
	// comment is part of the same User-Agent value, not a second declaration.
	DefaultVersion = "0.2.0-rc.2"

	// StableVersion is the oldest client version this build advertises.
	StableVersion = DefaultVersion

	// VersionEnv is the optional operator override for DefaultVersion. It
	// follows the existing SUB2API_CLAUDE_CLI_VERSION / XAI_GROK_CLI_VERSION
	// convention; empty or invalid values fall back to the compiled pin.
	VersionEnv = "SUB2API_DEEPSEEK_HARNESS_VERSION"
)

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
// therefore rejected, matching the existing Grok policy.
func IsSupportedVersion(version string) bool {
	if brandidentity.ContainsBrand(version) {
		return false
	}
	canonical := "v" + strings.TrimSpace(version)
	minimum := "v" + StableVersion
	return semver.IsValid(canonical) &&
		semver.Canonical(canonical) == canonical &&
		semver.Compare(canonical, minimum) >= 0
}

// UserAgent builds the published harness User-Agent value:
// `deepseek-harness/<version> (+https://github.com/deepseek-ai/deepseek-harness)`.
func UserAgent(version string) string {
	if strings.TrimSpace(version) == "" {
		version = DefaultVersion
	}
	return ClientIdentifier + "/" + version + " (+" + HomeURL + ")"
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
		Preset:     "deepseek",
		Language:   "zh-CN",
		Timezone:   outboundidentity.DefaultTimezone,
		UserAgent:  ua,
		Originator: ClientIdentifier,
		Version:    version,
		Source:     source,
		Headers:    map[string]string{"User-Agent": ua},
	}
}

// ControlIdentity mirrors platformClientHeaders(null, client): this server uses
// the official web login flow, whose UI build version is the Harness version.
func ControlIdentity(identity outboundidentity.Identity) outboundidentity.Identity {
	identity = CaptureIdentity(identity, time.Now())
	identity.Headers = maps.Clone(identity.Headers)
	identity.Headers["X-Client-Bundle-Id"] = ""
	identity.Headers["X-Client-Platform"] = "web"
	identity.Headers["X-Client-Version"] = identity.Version
	identity.Headers["X-Client-Locale"] = WireLocale(identity.Language)
	identity.Headers["X-Client-Timezone-Offset"] = identity.ControlHeaders["X-Client-Timezone-Offset"]
	return identity
}

// WireLocale mirrors platformWireLocale: Chinese -> zh_CN, otherwise en_US.
func WireLocale(language string) string {
	if strings.EqualFold(strings.Split(strings.ReplaceAll(language, "_", "-"), "-")[0], "zh") {
		return "zh_CN"
	}
	return "en_US"
}

// CaptureIdentity freezes the offset at operation start so a login resumed from
// Redis, retry or refresh cannot adopt another environment midway through it.
func CaptureIdentity(identity outboundidentity.Identity, at time.Time) outboundidentity.Identity {
	if identity.ControlHeaders["X-Client-Timezone-Offset"] == "" {
		identity.ControlHeaders = maps.Clone(identity.ControlHeaders)
		if identity.ControlHeaders == nil {
			identity.ControlHeaders = map[string]string{}
		}
		identity.ControlHeaders["X-Client-Timezone-Offset"] = strconv.Itoa(identity.TimezoneOffset(at))
	}
	return identity
}
