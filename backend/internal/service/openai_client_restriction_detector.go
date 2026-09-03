package service

import (
	"fmt"
	"net/http"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

// CodexOfficialClientsOnlyMessage is intentionally precise: this is a strict
// request-profile restriction, not cryptographic proof of an official binary.
const CodexOfficialClientsOnlyMessage = "This account only allows approved Codex client profiles"

const (
	CodexClientRestrictionReasonDisabled                          = "codex_cli_only_disabled"
	CodexClientRestrictionReasonMatchedOfficialProfile            = "official_client_profile_matched"
	CodexClientRestrictionReasonMatchedLegacyCompatibilityProfile = "legacy_compatibility_client_profile_matched"
	CodexClientRestrictionReasonMatchedCompatibilityEntry         = "compatibility_client_profile_matched"
	CodexClientRestrictionReasonNotMatchedProfile                 = "codex_client_profile_not_matched"
	CodexClientRestrictionReasonMatchedAppServerClient            = "app_server_client_matched"
	CodexClientRestrictionReasonBlacklisted                       = "blacklist_matched"
	CodexClientRestrictionReasonMissingKnownEvidence              = "missing_known_codex_evidence"
	CodexClientRestrictionReasonMissingEngineFingerprint          = "missing_engine_fingerprint"
	CodexClientRestrictionReasonVersionTooLow                     = "codex_version_too_low"
	CodexClientRestrictionReasonVersionTooHigh                    = "codex_version_too_high"
)

// CodexRestrictionPolicy contains the global policy surrounding the closed
// built-in official profile registry. Whitelist entries are explicitly approved
// compatibility profiles and never turn an unknown request into an official
// profile. There is deliberately no generic App Server or generic fingerprint
// bypass.
type CodexRestrictionPolicy struct {
	Whitelist                               []openai.AllowedClientEntry
	Blacklist                               []openai.AllowedClientEntry
	MinCodexVersion                         string
	MaxCodexVersion                         string
	LegacyClientProfileCompatibilityEnabled bool
	AllowAppServerClients                   bool
	EngineFingerprintSignals                []openai.EngineFingerprintSignal
}

// CodexClientRestrictionDetectionResult is the account-policy decision.
type CodexClientRestrictionDetectionResult struct {
	Enabled bool
	Matched bool
	Reason  string

	// Profile identifies the reviewed current or explicitly enabled legacy
	// transport profile. It is intentionally empty for an administrator
	// allow-list compatibility entry.
	Profile string
	// DetectedVersion is emitted only for a recognized built-in profile.
	DetectedVersion string
	MinCodexVersion string
	MaxCodexVersion string
}

type CodexClientRestrictionDetector interface {
	Detect(c *gin.Context, account *Account, policy CodexRestrictionPolicy, body []byte) CodexClientRestrictionDetectionResult
}

type OpenAICodexClientRestrictionDetector struct{}

// cfg remains an argument for construction compatibility. Inbound profile
// authorization deliberately does not read gateway.force_codex_cli or any
// other outbound-formatting option.
func NewOpenAICodexClientRestrictionDetector(_ *config.Config) *OpenAICodexClientRestrictionDetector {
	return &OpenAICodexClientRestrictionDetector{}
}

// Detect applies one fail-closed account policy:
//
//  1. disabled accounts bypass the restriction;
//  2. configured deny entries win;
//  3. requests must match a coherent current-official profile, an explicitly
//     enabled legacy profile, or an explicit compatibility entry;
//  4. every allowed profile needs one known, non-empty Codex evidence header;
//  5. reviewed current and legacy profiles honour the configured engine-version
//     range.
//
// All headers remain spoofable. This gate restricts supported request profiles;
// it never claims to attest the client binary or prevent credential sharing.
func (d *OpenAICodexClientRestrictionDetector) Detect(c *gin.Context, account *Account, policy CodexRestrictionPolicy, body []byte) CodexClientRestrictionDetectionResult {
	if account == nil || !account.IsCodexCLIOnlyEnabled() {
		return CodexClientRestrictionDetectionResult{Reason: CodexClientRestrictionReasonDisabled}
	}

	userAgent, originator := "", ""
	var headers http.Header
	if c != nil {
		userAgent = c.GetHeader("User-Agent")
		originator = c.GetHeader("originator")
		if c.Request != nil {
			headers = c.Request.Header
		}
	}

	if openai.MatchDenyEntries(userAgent, originator, policy.Blacklist) {
		return CodexClientRestrictionDetectionResult{Enabled: true, Reason: CodexClientRestrictionReasonBlacklisted}
	}

	profile, recognizedProfile := openai.ClassifyCodexClientProfile(userAgent, originator, policy.LegacyClientProfileCompatibilityEnabled)
	compatibilityEntry := false
	skipFingerprint := false
	appServerAllowed := false
	if !recognizedProfile {
		entry, ok := openai.MatchClientEntry(userAgent, originator, policy.Whitelist)
		compatibilityEntry = ok
		if ok {
			skipFingerprint = entry.SkipEngineFingerprint
		}
		if !compatibilityEntry {
			if policy.AllowAppServerClients || account.IsCodexCLIOnlyAppServerAllowed() {
				appServerAllowed = true
			} else {
				return CodexClientRestrictionDetectionResult{Enabled: true, Reason: CodexClientRestrictionReasonNotMatchedProfile}
			}
		}
	}

	if !openai.HasKnownCodexClientEvidence(headers) {
		return CodexClientRestrictionDetectionResult{Enabled: true, Reason: CodexClientRestrictionReasonMissingKnownEvidence}
	}

	if compatibilityEntry {
		if !skipFingerprint && !openai.EvaluateEngineFingerprint(headers, body, policy.EngineFingerprintSignals) {
			return CodexClientRestrictionDetectionResult{Enabled: true, Reason: CodexClientRestrictionReasonMissingEngineFingerprint}
		}
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedCompatibilityEntry,
		}
	}
	if appServerAllowed {
		if !openai.EvaluateEngineFingerprint(headers, body, policy.EngineFingerprintSignals) {
			return CodexClientRestrictionDetectionResult{Enabled: true, Reason: CodexClientRestrictionReasonMissingEngineFingerprint}
		}
		return CodexClientRestrictionDetectionResult{
			Enabled: true,
			Matched: true,
			Reason:  CodexClientRestrictionReasonMatchedAppServerClient,
		}
	}

	if policy.MinCodexVersion != "" && CompareVersions(profile.Version, policy.MinCodexVersion) < 0 {
		return CodexClientRestrictionDetectionResult{
			Enabled: true, Reason: CodexClientRestrictionReasonVersionTooLow,
			Profile: string(profile.Profile), DetectedVersion: profile.Version, MinCodexVersion: policy.MinCodexVersion,
		}
	}
	if policy.MaxCodexVersion != "" && CompareVersions(profile.Version, policy.MaxCodexVersion) > 0 {
		return CodexClientRestrictionDetectionResult{
			Enabled: true, Reason: CodexClientRestrictionReasonVersionTooHigh,
			Profile: string(profile.Profile), DetectedVersion: profile.Version, MaxCodexVersion: policy.MaxCodexVersion,
		}
	}

	reason := CodexClientRestrictionReasonMatchedOfficialProfile
	if profile.Profile == openai.CodexClientProfileLegacyCompatibility {
		reason = CodexClientRestrictionReasonMatchedLegacyCompatibilityProfile
	}
	if !openai.EvaluateEngineFingerprint(headers, body, policy.EngineFingerprintSignals) {
		return CodexClientRestrictionDetectionResult{Enabled: true, Reason: CodexClientRestrictionReasonMissingEngineFingerprint}
	}
	return CodexClientRestrictionDetectionResult{
		Enabled: true, Matched: true, Reason: reason,
		Profile: string(profile.Profile), DetectedVersion: profile.Version,
	}
}

func CodexClientRestrictionMessage(r CodexClientRestrictionDetectionResult) string {
	switch r.Reason {
	case CodexClientRestrictionReasonVersionTooLow:
		return fmt.Sprintf(
			"Your Codex version (%s) is below the minimum required version (%s). Please update Codex.",
			r.DetectedVersion, r.MinCodexVersion)
	case CodexClientRestrictionReasonVersionTooHigh:
		return fmt.Sprintf(
			"Your Codex version (%s) exceeds the maximum allowed version (%s). Please downgrade Codex to %s or lower.",
			r.DetectedVersion, r.MaxCodexVersion, r.MaxCodexVersion)
	default:
		return CodexOfficialClientsOnlyMessage
	}
}
