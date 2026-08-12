package openai

import (
	"net/http"
	"strings"

	"golang.org/x/mod/semver"
)

// CodexClientProfile is a reviewed wire-identity profile for an OpenAI Codex
// client. It identifies a request shape, not a desktop application binary.
// A product surface may share an underlying transport profile with another
// official Codex integration.
type CodexClientProfile string

const (
	CodexClientProfileCLI     CodexClientProfile = "codex-cli"
	CodexClientProfileTUI     CodexClientProfile = "codex-tui"
	CodexClientProfileIDE     CodexClientProfile = "codex-ide"
	CodexClientProfileDesktop CodexClientProfile = "codex-desktop"
	CodexClientProfileFamily  CodexClientProfile = "codex-product-family"
	// CodexClientProfileLegacyCompatibility is deliberately not an official
	// profile. It represents one of the four historic wire identities that an
	// administrator explicitly enabled while migrating old clients.
	CodexClientProfileLegacyCompatibility CodexClientProfile = "codex-legacy-compatible"
)

// CodexClientProfileMatch is the result of a strict built-in profile match.
// Header values are caller controlled and consequently this is request-feature
// classification, never binary attestation.
type CodexClientProfileMatch struct {
	Profile    CodexClientProfile
	Originator string
	Version    string
}

// codexBuiltInProfileByOriginator is deliberately closed and versioned in
// source. Additions require a reviewed OpenAI upstream source/release and a
// regression fixture; the running service must not download acceptance rules.
//
// The list mirrors the current public first-party originator predicates in
// openai/codex. UI/product labels are deliberately not guessed: Desktop and
// JetBrains use the reviewed, case-sensitive `Codex ` product family below.
var codexBuiltInProfileByOriginator = map[string]CodexClientProfile{
	"codex_cli_rs":          CodexClientProfileCLI,
	"codex-tui":             CodexClientProfileTUI,
	"codex_vscode":          CodexClientProfileIDE,
	"codex_chatgpt_desktop": CodexClientProfileDesktop,
	"codex_atlas":           CodexClientProfileDesktop,
}

// codexLegacyCompatibilityOriginators is a temporary, deliberately closed
// migration set. These names are not present in the current public Codex
// first-party predicates, so callers must opt in with allowLegacyCompatibility
// and must never present a match as an official profile.
var codexLegacyCompatibilityOriginators = map[string]struct{}{
	"codex_app":            {},
	"codex_exec":           {},
	"codex_sdk_ts":         {},
	"codex_vscode_copilot": {},
}

// knownCodexClientEvidenceHeaders is a closed set of request fields declared
// by the public Codex client implementation. It intentionally does not accept
// arbitrary X-Codex-* names: X-Codex-Fake must never be useful evidence.
var knownCodexClientEvidenceHeaders = []string{
	"x-codex-installation-id",
	"x-codex-routing-hint",
	"x-codex-turn-state",
	"x-codex-turn-metadata",
	"x-codex-parent-thread-id",
	"x-codex-window-id",
}

// ClassifyCodexClientProfile checks the coherent Originator/User-Agent pair
// used by a reviewed current profile. The legacy migration set is considered
// only when allowLegacyCompatibility is explicitly true. It intentionally does
// not reuse the historical loose helpers in request.go.
func ClassifyCodexClientProfile(userAgent, originator string, allowLegacyCompatibility bool) (CodexClientProfileMatch, bool) {
	ua := strings.TrimSpace(userAgent)
	if originator != strings.TrimSpace(originator) {
		return CodexClientProfileMatch{}, false
	}
	originator = strings.TrimSpace(originator)
	if ua == "" || originator == "" || !isSaneCodexOriginator(originator) {
		return CodexClientProfileMatch{}, false
	}

	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return CodexClientProfileMatch{}, false
	}
	uaOriginator := ua[:slash]
	if uaOriginator == "" || uaOriginator != originator {
		return CodexClientProfileMatch{}, false
	}
	version := CodexUserAgentVersion(ua)
	if !isStrictCodexClientProfileVersion(version) {
		return CodexClientProfileMatch{}, false
	}

	if profile, ok := codexBuiltInProfileByOriginator[originator]; ok {
		return CodexClientProfileMatch{Profile: profile, Originator: originator, Version: version}, true
	}
	if allowLegacyCompatibility {
		if _, ok := codexLegacyCompatibilityOriginators[originator]; ok {
			return CodexClientProfileMatch{Profile: CodexClientProfileLegacyCompatibility, Originator: originator, Version: version}, true
		}
	}
	// Mirrors the case-sensitive upstream `starts_with("Codex ")` family, but
	// keeps the full raw family name coherent across both headers.
	if strings.HasPrefix(originator, "Codex ") && isSaneCodexProductFamilyName(originator) {
		return CodexClientProfileMatch{Profile: CodexClientProfileFamily, Originator: originator, Version: version}, true
	}
	return CodexClientProfileMatch{}, false
}

func isStrictCodexClientProfileVersion(version string) bool {
	if strings.TrimSpace(version) != version || version == "" || strings.HasPrefix(version, "v") {
		return false
	}
	base := version
	if index := strings.IndexAny(base, "-+"); index >= 0 {
		base = base[:index]
	}
	return strings.Count(base, ".") == 2 && semver.IsValid("v"+version)
}

// ClassifyOfficialCodexClientProfile is the strict current-official wrapper.
// The legacy compatibility mode has its own explicit call site and is never
// silently enabled by this helper.
func ClassifyOfficialCodexClientProfile(userAgent, originator string) (CodexClientProfileMatch, bool) {
	return ClassifyCodexClientProfile(userAgent, originator, false)
}

// PairConfiguredCodexClientIdentity parses an administrator-configured
// complete User-Agent into an exact Originator/User-Agent pair. There is no
// case folding, substring matching, or legacy trailer recovery: a configured
// identity must already be coherent in its leading token.
func PairConfiguredCodexClientIdentity(userAgent string, allowLegacyCompatibility bool) (CodexClientProfileMatch, string, bool) {
	ua := strings.TrimSpace(userAgent)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 {
		return CodexClientProfileMatch{}, "", false
	}
	originator := ua[:slash]
	profile, ok := ClassifyCodexClientProfile(ua, originator, allowLegacyCompatibility)
	if !ok {
		return CodexClientProfileMatch{}, "", false
	}
	return profile, ua, true
}

// IsCodexLegacyCompatibilityOriginator exposes the closed migration set for
// trusted callers that need to distinguish it from current official profiles.
func IsCodexLegacyCompatibilityOriginator(originator string) bool {
	_, ok := codexLegacyCompatibilityOriginators[originator]
	return ok
}

func isSaneCodexProductFamilyName(originator string) bool {
	for _, c := range originator {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == ' ' || c == '-' || c == '_' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// HasKnownCodexClientEvidence reports whether a request has one of the known
// non-empty Codex client evidence headers. Evidence is deliberately auxiliary:
// it narrows accidental/non-cooperating requests but is still spoofable.
func HasKnownCodexClientEvidence(header http.Header) bool {
	if header == nil {
		return false
	}
	for _, name := range knownCodexClientEvidenceHeaders {
		if strings.TrimSpace(header.Get(name)) != "" {
			return true
		}
	}
	return false
}

// HasCoherentConfiguredClientIdentity verifies the leading User-Agent identity
// agrees with an explicitly configured compatibility Originator. It is public
// so the administrator-managed allow-list cannot silently degrade to a single
// spoofable header check.
func HasCoherentConfiguredClientIdentity(userAgent, originator string) bool {
	ua := strings.TrimSpace(userAgent)
	originator = strings.TrimSpace(originator)
	slash := strings.IndexByte(ua, '/')
	if slash <= 0 || originator == "" {
		return false
	}
	return normalizeCodexClientHeader(ua[:slash]) == normalizeCodexClientHeader(originator)
}
