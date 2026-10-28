package service

import (
	"context"
	"net/http"
	"strings"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

const maxOpenAIAccountUserAgentLength = 512

const (
	openAIOutboundIdentitySourceAccount = "account"
	openAIOutboundIdentitySourceGlobal  = "global"
	openAIOutboundIdentitySourceDefault = "compiled_default"
)

// openAIOutboundIdentity is the trusted client identity used for an upstream
// OpenAI request. It is deliberately resolved from account and system settings
// only; caller request headers never participate in this decision.
type openAIOutboundIdentity struct {
	UserAgent  string
	Originator string
	Version    string
	// Source deliberately records only the selected configuration tier. It is
	// safe to persist in Ops diagnostics and never contains the configured UA.
	Source string
}

type codexDefaultIdentityCacheKey struct {
	owner outboundIdentityOwner
}

// resolveOpenAIOutboundIdentityFromSettings is the single authority for
// selecting a trusted OpenAI Codex outbound identity. A valid account-level
// identity wins over a valid system identity; an empty or invalid candidate
// falls through to the compiled-in default. The selected fingerprint keeps its
// client name and platform details, while the effective version is synchronized
// from the setting service so User-Agent, Originator, and Version agree.
//
// gateway.force_codex_cli is intentionally absent here. It is an inbound
// request-classification switch, not an administrator override for an account's
// configured outbound client identity.
func resolveOpenAIOutboundIdentityFromSettings(ctx context.Context, account *Account, settingService *SettingService) openAIOutboundIdentity {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return openAIOutboundIdentity{}
	}
	account = owner
	ctx = withOutboundIdentitySettingsOwner(ctx, account)
	if scope := outboundIdentityScopeFromContext(ctx); scope != nil {
		candidate := ""
		if account != nil {
			candidate = account.GetOpenAIUserAgent()
		}
		key := requestCredentialIdentityCacheKey(ctx, account, candidate)
		if account == nil {
			// Compatible presets resolve Codex defaults without an account
			// candidate, but still belong to the invoking credential owner.
			settingsOwner, _ := ctx.Value(outboundIdentitySettingsOwnerKey{}).(outboundIdentityOwner)
			key = codexDefaultIdentityCacheKey{owner: settingsOwner}
		}
		if cached, ok := scope.codex.Load(key); ok {
			if identity, valid := cached.(openAIOutboundIdentity); valid {
				return identity
			}
		}
		identity := resolveOpenAIOutboundIdentityFromCurrentSettings(ctx, account, settingService)
		cached, _ := scope.codex.LoadOrStore(key, identity)
		if snapshot, valid := cached.(openAIOutboundIdentity); valid {
			return snapshot
		}
		return identity
	}
	return resolveOpenAIOutboundIdentityFromCurrentSettings(ctx, account, settingService)
}

type codexOutboundSettingsSnapshot struct {
	userAgent                string
	version                  string
	allowLegacyCompatibility bool
}

func resolveOpenAIOutboundIdentityFromCurrentSettings(ctx context.Context, account *Account, settingService *SettingService) openAIOutboundIdentity {
	read := func() codexOutboundSettingsSnapshot {
		settings := codexOutboundSettingsSnapshot{version: codexCLIVersion}
		if settingService != nil {
			settings.userAgent, settings.allowLegacyCompatibility = settingService.GetOpenAICodexOutboundProfile(ctx)
			settings.version = settingService.GetOpenAICodexClientVersion(ctx)
		}
		return settings
	}
	var settings codexOutboundSettingsSnapshot
	if snapshot := requestIdentitySettingsSnapshot(ctx); snapshot != nil {
		snapshot.codexSettingsOnce.Do(func() { snapshot.codexSettings = read() })
		settings = snapshot.codexSettings
	} else {
		settings = read()
	}
	accountUA := ""
	if account != nil {
		accountUA = account.GetOpenAIUserAgent()
	}
	return resolveOpenAIOutboundIdentityWithVersionAndCompatibility(accountUA, settings.userAgent, settings.version, settings.allowLegacyCompatibility)
}

// resolveOpenAIOutboundIdentity uses the account-specific Codex UA when it is
// valid, then the system setting, and finally the compiled-in default. A value
// is only valid when it can be paired with an official Codex originator.
func (s *OpenAIGatewayService) resolveOpenAIOutboundIdentity(ctx context.Context, account *Account) openAIOutboundIdentity {
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil {
		return openAIOutboundIdentity{}
	}
	account = owner
	// Spark shadows never own credentials or an outbound identity. All normal
	// forwarding paths build authentication first and therefore fail closed when
	// this lookup fails; resolving here makes the final header stage agree with
	// the credential owner instead of accidentally falling back to a global UA.
	if account != nil && account.IsCredentialShadow() && s != nil && s.accountRepo != nil {
		if credentialAccount, err := resolveCredentialAccount(ctx, s.accountRepo, account); err == nil && credentialAccount != nil {
			account = credentialAccount
		}
	}
	var settingService *SettingService
	if s != nil {
		settingService = s.settingService
	}
	return resolveOpenAIOutboundIdentityFromSettings(ctx, account, settingService)
}

// NormalizeOpenAIAccountUserAgent validates and canonicalizes the optional
// account-level Codex client identity. An empty value explicitly means inherit
// the global/default identity. The paired UA is stored so User-Agent,
// Originator, and Version always come from one source of truth.
func NormalizeOpenAIAccountUserAgent(platform string, credentials map[string]any) error {
	return NormalizeOpenAIAccountUserAgentWithCompatibility(platform, credentials, false)
}

// NormalizeOpenAIAccountUserAgentWithCompatibility applies the global legacy
// migration policy while preserving the account > global > default source
// precedence. The policy controls validation only; it never rewrites a client
// family or a platform fingerprint.
func NormalizeOpenAIAccountUserAgentWithCompatibility(platform string, credentials map[string]any, allowLegacyCompatibility bool) error {
	if platform != PlatformOpenAI || credentials == nil {
		return nil
	}
	raw, configured := credentials["user_agent"]
	if !configured || raw == nil {
		delete(credentials, "user_agent")
		return nil
	}
	userAgent, ok := raw.(string)
	if !ok {
		return infraerrors.New(http.StatusBadRequest, "OPENAI_CODEX_USER_AGENT_INVALID", "OpenAI Codex user_agent must be a string")
	}
	userAgent, err := NormalizeOpenAICodexUserAgentWithCompatibility(userAgent, allowLegacyCompatibility)
	if err != nil {
		return err
	}
	if userAgent == "" {
		delete(credentials, "user_agent")
		return nil
	}
	credentials["user_agent"] = userAgent
	return nil
}

// NormalizeOpenAICodexUserAgent validates and canonicalizes a configured
// Codex client identity. An empty value means inherit the compiled-in default.
// Both account and global configuration use this helper so a saved identity can
// always produce a matching Originator and Version for upstream requests.
func NormalizeOpenAICodexUserAgent(userAgent string) (string, error) {
	return NormalizeOpenAICodexUserAgentWithCompatibility(userAgent, false)
}

// NormalizeOpenAICodexUserAgentWithCompatibility validates one complete
// configured identity. Legacy aliases are accepted only through the closed
// migration set when the global compatibility mode is enabled.
func NormalizeOpenAICodexUserAgentWithCompatibility(userAgent string, allowLegacyCompatibility bool) (string, error) {
	if strings.Trim(userAgent, " \t") == "" {
		return "", nil
	}
	// Validate the raw candidate before canonicalizing surrounding whitespace.
	// Trimming first would turn a CR/LF-wrapped invalid account/global value
	// into an accepted identity instead of rejecting it or falling through.
	_, pairedUserAgent, ok := openai.PairConfiguredCodexClientIdentity(userAgent, allowLegacyCompatibility)
	if !ok {
		return "", infraerrors.New(http.StatusBadRequest, "OPENAI_CODEX_USER_AGENT_INVALID", "OpenAI Codex user_agent must be a supported Codex User-Agent")
	}
	if len(pairedUserAgent) > maxOpenAIAccountUserAgentLength {
		return "", infraerrors.Newf(http.StatusBadRequest, "OPENAI_CODEX_USER_AGENT_INVALID", "OpenAI Codex user_agent must be at most %d characters", maxOpenAIAccountUserAgentLength)
	}
	return pairedUserAgent, nil
}

func resolveOpenAIOutboundIdentityCandidates(accountUA, systemUA string) openAIOutboundIdentity {
	return resolveOpenAIOutboundIdentityCandidatesWithCompatibility(accountUA, systemUA, false)
}

func resolveOpenAIOutboundIdentityCandidatesWithCompatibility(accountUA, systemUA string, allowLegacyCompatibility bool) openAIOutboundIdentity {
	if identity, ok := validOpenAIOutboundIdentityWithCompatibility(accountUA, allowLegacyCompatibility); ok {
		identity.Source = openAIOutboundIdentitySourceAccount
		return identity
	}
	if identity, ok := validOpenAIOutboundIdentityWithCompatibility(systemUA, allowLegacyCompatibility); ok {
		identity.Source = openAIOutboundIdentitySourceGlobal
		return identity
	}
	identity, ok := validOpenAIOutboundIdentityWithCompatibility(DefaultOpenAICodexUserAgent, false)
	if ok {
		identity.Source = openAIOutboundIdentitySourceDefault
		return identity
	}
	// DefaultOpenAICodexUserAgent is a compile-time invariant covered by tests.
	// Keep this defensive return aligned with the normal default rather than
	// introducing a second, unreachable built-in identity.
	return openAIOutboundIdentity{
		UserAgent:  DefaultOpenAICodexUserAgent,
		Originator: openai.CodexDefaultOriginator,
		Version:    DefaultOpenAICodexVersion,
		Source:     openAIOutboundIdentitySourceDefault,
	}
}

// resolveOpenAIOutboundIdentityWithVersion applies the configured Codex
// version only after choosing the account, global, or built-in identity. The
// selected source still owns the client family and platform fingerprint; only
// its version declarations are rebuilt so the User-Agent and Version header
// stay synchronized for upstream overload handling.
func resolveOpenAIOutboundIdentityWithVersion(accountUA, systemUA, configuredVersion string) openAIOutboundIdentity {
	return resolveOpenAIOutboundIdentityWithVersionAndCompatibility(accountUA, systemUA, configuredVersion, false)
}

func resolveOpenAIOutboundIdentityWithVersionAndCompatibility(accountUA, systemUA, configuredVersion string, allowLegacyCompatibility bool) openAIOutboundIdentity {
	identity := resolveOpenAIOutboundIdentityCandidatesWithCompatibility(accountUA, systemUA, allowLegacyCompatibility)
	version, _ := resolveOpenAICodexClientVersion(configuredVersion)
	if userAgent := openai.SetCodexUserAgentVersion(identity.UserAgent, version); userAgent != "" {
		identity.UserAgent = userAgent
		identity.Version = version
	}
	return identity
}

func validOpenAIOutboundIdentityWithCompatibility(userAgent string, allowLegacyCompatibility bool) (openAIOutboundIdentity, bool) {
	profile, pairedUA, ok := openai.PairConfiguredCodexClientIdentity(userAgent, allowLegacyCompatibility)
	if !ok || len(pairedUA) > maxOpenAIAccountUserAgentLength {
		return openAIOutboundIdentity{}, false
	}
	version := openAIOutboundIdentityVersion(pairedUA)
	if version == "" {
		return openAIOutboundIdentity{}, false
	}
	return openAIOutboundIdentity{UserAgent: pairedUA, Originator: profile.Originator, Version: version}, true
}

// openAIOutboundIdentityVersion extracts the client version from a paired
// Codex User-Agent. Its caller has already verified the client originator.
func openAIOutboundIdentityVersion(userAgent string) string {
	_, suffix, ok := strings.Cut(strings.TrimSpace(userAgent), "/")
	if !ok {
		return ""
	}
	version := strings.Fields(suffix)
	if len(version) == 0 {
		return ""
	}
	return version[0]
}

// applyOpenAIOutboundIdentity is the final identity stage for an OpenAI
// request. Account Header Override and all inbound headers must run before it.
// Inference and provider discovery declare UA, Originator and Version for both
// OAuth and API keys, matching the official built-in OpenAI provider. Auxiliary
// ChatGPT usage endpoints deliberately request UA only.
func (s *OpenAIGatewayService) applyOpenAIOutboundIdentity(ctx context.Context, account *Account, headers http.Header, useCodexIdentity bool) openAIOutboundIdentity {
	identity := s.resolveOpenAIOutboundIdentity(ctx, account)
	applyResolvedOpenAIOutboundIdentity(headers, identity, useCodexIdentity)
	var settings *SettingService
	if s != nil {
		settings = s.settingService
	}
	applyOpenAICodexResidencyFromSettings(ctx, settings, headers, useCodexIdentity)
	applyOpenAIRegionalRoutingHeaders(ctx, account, headers)
	return identity
}

func applyResolvedOpenAIOutboundIdentity(headers http.Header, identity openAIOutboundIdentity, useCodexIdentity bool) {
	if headers == nil {
		return
	}
	for name := range headers {
		if outboundidentity.IsIdentityHeader(name) {
			delete(headers, name)
		}
	}
	headers.Set("User-Agent", identity.UserAgent)
	// The endpoint owns header presence, independently of its authentication.
	// Auxiliary endpoints omit provider declarations; inference includes them.
	if useCodexIdentity {
		headers.Set("Version", identity.Version)
		headers.Set("Originator", identity.Originator)
	}
}

// applyOpenAIHeaderOverrides applies only the non-identity account overrides
// supported by OpenAI API-key accounts. It keeps generic overrides available
// to other providers while making reserved OpenAI protocol headers immutable.
func (a *Account) applyOpenAIHeaderOverrides(headers http.Header) {
	if a == nil || headers == nil {
		return
	}
	for name, value := range a.GetHeaderOverrides() {
		if isOpenAIProtectedHeaderOverrideName(name) {
			continue
		}
		for existing := range headers {
			if strings.EqualFold(existing, name) {
				delete(headers, existing)
			}
		}
		headers[resolveWireCasing(name)] = []string{value}
	}
}

func isOpenAIProtectedHeaderOverrideName(lowerName string) bool {
	lowerName = strings.ToLower(strings.TrimSpace(lowerName))
	if lowerName == "user-agent" || lowerName == "originator" || lowerName == "version" || lowerName == "openai-beta" || lowerName == "x-openai-internal-codex-residency" || lowerName == "x-openai-fedramp" {
		return true
	}
	if strings.HasPrefix(lowerName, "x-codex-") {
		return true
	}
	return lowerName == "session_id" || lowerName == "conversation_id"
}
