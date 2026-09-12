package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/geminicli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

const SettingKeyOutboundIdentity = "outbound_identity"
const outboundIdentityCredential = "outbound_identity"

// OutboundIdentitySelection is an account/global candidate. Its fields form one
// validated identity; missing fields use the selected preset, never another
// configuration tier. Codex's existing account credentials remain authoritative.
type OutboundIdentitySelection struct {
	Preset    string `json:"preset"`
	UserAgent string `json:"user_agent,omitempty"`
	Version   string `json:"version,omitempty"`
}

type OutboundIdentitySettings struct {
	Profiles map[string]OutboundIdentitySelection `json:"profiles"`
	Defaults map[string]string                    `json:"defaults"`
}

type OutboundIdentityView struct {
	Settings  OutboundIdentitySettings    `json:"settings"`
	Presets   []outboundidentity.Identity `json:"presets"`
	Effective []outboundidentity.Identity `json:"effective"`
}

type cachedOutboundIdentitySettings struct {
	settings OutboundIdentitySettings
	expires  time.Time
}

var outboundClientVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?$`)
var outboundPresetNames = []string{"codex", "claude", "gemini", "grok", "antigravity"}

func emptyOutboundIdentitySettings() OutboundIdentitySettings {
	return OutboundIdentitySettings{Profiles: map[string]OutboundIdentitySelection{}, Defaults: map[string]string{}}
}

func nativeOutboundPreset(platform string) string {
	switch platform {
	case PlatformAnthropic:
		return "claude"
	case PlatformGemini:
		return "gemini"
	case PlatformGrok:
		return "grok"
	case PlatformAntigravity:
		return "antigravity"
	default:
		return "codex"
	}
}

func outboundDefaultKey(account *Account) string {
	if account == nil {
		return ""
	}
	return account.Platform + ":" + account.Type
}

func builtInOutboundIdentity(preset string) outboundidentity.Identity {
	i := outboundidentity.Identity{Preset: preset, Source: "compiled_default", Headers: map[string]string{}}
	switch preset {
	case "codex":
		i.UserAgent, i.Originator, i.Version = DefaultOpenAICodexUserAgent, openai.CodexDefaultOriginator, DefaultOpenAICodexVersion
		i.Headers["Originator"], i.Headers["Version"] = i.Originator, i.Version
	case "claude":
		i.UserAgent, i.Originator, i.Version = claude.DefaultHeaders["User-Agent"], "claude-cli", claude.CLIVersion()
		if claude.IsSupportedCLIVersion(strings.TrimSpace(os.Getenv(claude.CLIVersionEnv))) {
			i.Source = "environment"
		}
		for k, v := range claude.DefaultHeaders {
			if outboundidentity.IsIdentityHeader(k) {
				i.Headers[k] = v
			}
		}
	case "gemini":
		i.UserAgent, i.Originator = geminicli.GeminiCLIUserAgent, "GeminiCLI"
		i.Version = strings.Fields(strings.TrimPrefix(i.UserAgent, "GeminiCLI/"))[0]
	case "grok":
		i.Version, i.Originator = xai.ResolveCLIVersion(), xai.CLIClientIdentifier
		if xai.IsSupportedCLIVersion(strings.TrimSpace(os.Getenv(xai.CLIVersionEnv))) {
			i.Source = "environment"
		}
		i.UserAgent = xai.CLIUserAgent(i.Version)
		i.Headers["x-grok-client-identifier"] = i.Originator
		i.Headers["x-grok-client-version"] = i.Version
	case "antigravity":
		i.Version, i.Originator = antigravity.GetDefaultUserAgentVersion(), "antigravity"
		if antigravity.NormalizeUserAgentVersion(os.Getenv(antigravity.AntigravityUserAgentVersionEnv)) != "" {
			i.Source = "environment"
		}
		i.UserAgent = antigravity.BuildUserAgent(i.Version)
	default:
		return outboundidentity.Identity{}
	}
	i.Headers["User-Agent"] = i.UserAgent
	return i
}

func buildOutboundIdentity(selection OutboundIdentitySelection) (outboundidentity.Identity, error) {
	i := builtInOutboundIdentity(selection.Preset)
	if i.UserAgent == "" {
		return i, fmt.Errorf("unknown identity preset")
	}
	ua := strings.TrimSpace(selection.UserAgent)
	if ua == "" {
		ua = i.UserAgent
	}
	if len(ua) > 512 {
		return i, fmt.Errorf("User-Agent exceeds 512 characters")
	}
	for _, c := range ua {
		if c < 32 || c > 126 {
			return i, fmt.Errorf("User-Agent must contain printable ASCII")
		}
	}
	if selection.Preset == "codex" {
		resolved, ok := validOpenAIOutboundIdentityWithCompatibility(ua, false)
		if !ok {
			return i, fmt.Errorf("unsupported Codex identity")
		}
		i.UserAgent, i.Originator, i.Version = resolved.UserAgent, resolved.Originator, resolved.Version
	} else {
		prefix := map[string]string{"claude": "claude-cli/", "gemini": "GeminiCLI/", "grok": "xai-grok-workspace/", "antigravity": "antigravity/"}[selection.Preset]
		if !strings.HasPrefix(ua, prefix) {
			return i, fmt.Errorf("User-Agent must match the selected preset")
		}
		version := strings.Fields(strings.TrimPrefix(ua, prefix))
		if len(version) == 0 || !outboundClientVersionPattern.MatchString(version[0]) {
			return i, fmt.Errorf("invalid User-Agent client version")
		}
		i.UserAgent, i.Version = ua, version[0]
	}
	if version := strings.TrimSpace(selection.Version); version != "" {
		if len(version) > 64 || !outboundClientVersionPattern.MatchString(version) {
			return i, fmt.Errorf("invalid client version")
		}
		if i.Preset == "codex" {
			resolved := resolveOpenAIOutboundIdentityWithVersion(i.UserAgent, "", version)
			i.UserAgent, i.Version = resolved.UserAgent, resolved.Version
		} else {
			i.UserAgent = strings.Replace(i.UserAgent, "/"+i.Version, "/"+version, 1)
			i.Version = version
		}
	}
	if i.Preset == "claude" && !claude.IsSupportedCLIVersion(i.Version) {
		return i, fmt.Errorf("claude version is below the supported baseline")
	}
	if i.Preset == "grok" && !xai.IsSupportedCLIVersion(i.Version) {
		return i, fmt.Errorf("grok version is below the supported baseline")
	}
	if i.Preset == "antigravity" && antigravity.NormalizeUserAgentVersion(i.Version) == "" {
		return i, fmt.Errorf("invalid Antigravity version")
	}
	i.Headers["User-Agent"] = i.UserAgent
	if i.Preset == "codex" {
		i.Headers["Originator"], i.Headers["Version"] = i.Originator, i.Version
	}
	if i.Preset == "grok" {
		i.Headers["x-grok-client-version"] = i.Version
	}
	return i, nil
}

func (s *SettingService) GetOutboundIdentitySettings(ctx context.Context) OutboundIdentitySettings {
	if s == nil || s.settingRepo == nil {
		return emptyOutboundIdentitySettings()
	}
	if cached, ok := s.outboundIdentityCache.Load().(*cachedOutboundIdentitySettings); ok && time.Now().Before(cached.expires) {
		return cloneOutboundIdentitySettings(cached.settings)
	}
	s.outboundIdentityMu.Lock()
	defer s.outboundIdentityMu.Unlock()
	if cached, ok := s.outboundIdentityCache.Load().(*cachedOutboundIdentitySettings); ok && time.Now().Before(cached.expires) {
		return cloneOutboundIdentitySettings(cached.settings)
	}
	settings := emptyOutboundIdentitySettings()
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyOutboundIdentity)
	if err == nil && raw != "" {
		if json.Unmarshal([]byte(raw), &settings) != nil {
			slog.WarnContext(ctx, "outbound identity settings fallback", "reason", "invalid_settings_json")
			settings = emptyOutboundIdentitySettings()
		}
	} else if err != nil && !errors.Is(err, ErrSettingNotFound) {
		slog.WarnContext(ctx, "outbound identity settings fallback", "reason", "settings_read_failed")
	} else {
		// Import the previous setting only before the unified configuration has
		// been saved. Clearing a profile later must genuinely restore defaults.
		legacy, legacyErr := s.settingRepo.GetValue(dbCtx, SettingKeyAntigravityUserAgentVersion)
		if version := antigravity.NormalizeUserAgentVersion(legacy); legacyErr == nil && version != "" {
			settings.Profiles["antigravity"] = OutboundIdentitySelection{Preset: "antigravity", Version: version}
		}
	}
	s.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: settings, expires: time.Now().Add(time.Minute)})
	return cloneOutboundIdentitySettings(settings)
}

func cloneOutboundIdentitySettings(settings OutboundIdentitySettings) OutboundIdentitySettings {
	result := emptyOutboundIdentitySettings()
	maps.Copy(result.Profiles, settings.Profiles)
	maps.Copy(result.Defaults, settings.Defaults)
	return result
}

func (s *SettingService) resolveDefaultOutboundIdentity(ctx context.Context, preset string) outboundidentity.Identity {
	if preset == "codex" {
		identity := resolveOpenAIOutboundIdentityFromSettings(ctx, nil, s)
		return outboundidentity.Identity{Preset: preset, UserAgent: identity.UserAgent, Originator: identity.Originator, Version: identity.Version, Source: identity.Source, Headers: map[string]string{"User-Agent": identity.UserAgent, "Originator": identity.Originator, "Version": identity.Version}}
	}
	settings := s.GetOutboundIdentitySettings(ctx)
	if selection, ok := settings.Profiles[preset]; ok {
		selection.Preset = preset
		if i, err := buildOutboundIdentity(selection); err == nil {
			i.Source = "global"
			return i
		}
	}
	return builtInOutboundIdentity(preset)
}

// WithAccountOutboundIdentity resolves after authentication/audit/account
// selection. Existing OpenAI/Codex paths deliberately keep their own resolver.
func WithAccountOutboundIdentity(ctx context.Context, account *Account) context.Context {
	if account == nil {
		return ctx
	}
	ctx = WithOutboundIdentityScope(ctx, nil)
	scope := outboundIdentityScopeFromContext(ctx)
	key := outboundIdentityOwnerKey(account)
	if cached, ok := scope.presets.Load(key); ok {
		if identity, valid := cached.(outboundidentity.Identity); valid {
			return outboundidentity.WithIdentity(ctx, identity)
		}
	}
	resolved := resolveAccountOutboundIdentityContext(ctx, account)
	identity, _ := outboundidentity.FromContext(resolved)
	// An empty identity records the deliberate choice of the existing Codex
	// resolver. A later mapping update cannot switch families during this request.
	cached, _ := scope.presets.LoadOrStore(key, identity)
	if snapshot, valid := cached.(outboundidentity.Identity); valid {
		return outboundidentity.WithIdentity(resolved, snapshot)
	}
	return resolved
}

func resolveAccountOutboundIdentityContext(ctx context.Context, account *Account) context.Context {
	if account == nil {
		return ctx
	}
	// Legacy OpenAI callers may leave Platform implicit. They keep the existing
	// Codex resolver just like explicitly typed native OpenAI accounts.
	if account.Platform == "" {
		return outboundidentity.WithIdentity(ctx, outboundidentity.Identity{})
	}
	if account.Platform == PlatformOpenAI && account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream {
		return outboundidentity.WithIdentity(ctx, outboundidentity.Identity{})
	}
	if i, ok := outboundidentity.FromContext(ctx); ok && account.ID > 0 && i.AccountID == account.ID {
		return ctx
	}
	preset := nativeOutboundPreset(account.Platform)
	if account.Type == AccountTypeBedrock {
		preset = "claude"
	}
	cleanCtx := outboundidentity.WithIdentity(ctx, outboundidentity.Identity{})
	var selection OutboundIdentitySelection
	if raw, ok := account.Credentials[outboundIdentityCredential]; ok {
		if data, err := json.Marshal(raw); err == nil && json.Unmarshal(data, &selection) == nil {
			if i, err := resolveAccountIdentitySelection(cleanCtx, account, selection, outboundDefaultIdentity); err == nil {
				if account.Platform == PlatformOpenAI && i.Preset == "codex" {
					return cleanCtx
				}
				i.Source, i.AccountID = "account", account.ID
				return outboundidentity.WithIdentity(ctx, i)
			}
		}
	}
	// Default mappings are resolved by the runtime callback with a typed account
	// key, without passing credentials into the package or transport layer.
	// Hide a previous account snapshot while resolving a failover account.
	if i, ok := outboundidentity.Default(cleanCtx, outboundDefaultKey(account)); ok {
		if account.Platform == PlatformOpenAI && i.Preset == "codex" {
			return cleanCtx
		}
		i.AccountID = account.ID
		return outboundidentity.WithIdentity(ctx, i)
	}
	i := builtInOutboundIdentity(preset)
	if account.Platform == PlatformOpenAI {
		return cleanCtx
	}
	i.AccountID = account.ID
	return outboundidentity.WithIdentity(ctx, i)
}

func ApplyAccountOutboundIdentity(ctx context.Context, account *Account, req *http.Request) {
	if req == nil || account == nil {
		return
	}
	*req = *req.WithContext(WithAccountOutboundIdentity(ctx, account))
	outboundidentity.ApplyContext(req)
}

func ApplyAccountOutboundHeaders(ctx context.Context, account *Account, headers http.Header) {
	if i, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account)); ok {
		i.Apply(headers)
	}
}

func outboundDefaultIdentity(ctx context.Context, preset string) outboundidentity.Identity {
	if i, ok := outboundidentity.Default(ctx, preset); ok {
		return i
	}
	return builtInOutboundIdentity(preset)
}

func validateAccountIdentityPreset(account *Account, preset string) error {
	if builtInOutboundIdentity(preset).UserAgent == "" {
		return fmt.Errorf("unknown identity preset")
	}
	if (account.Type == AccountTypeOAuth || account.Type == AccountTypeSetupToken) && preset != nativeOutboundPreset(account.Platform) {
		return fmt.Errorf("OAuth and setup-token accounts must retain their native client family")
	}
	return nil
}

// Selecting a preset alone inherits that preset's configured identity. Explicit
// declarations form a complete account candidate; invalid candidates fall
// through as a unit, never mixing fields from unrelated sources.
func resolveAccountIdentitySelection(ctx context.Context, account *Account, selection OutboundIdentitySelection, resolve func(context.Context, string) outboundidentity.Identity) (outboundidentity.Identity, error) {
	if err := validateAccountIdentityPreset(account, selection.Preset); err != nil {
		return outboundidentity.Identity{}, err
	}
	if selection.UserAgent == "" && selection.Version == "" {
		return resolve(ctx, selection.Preset), nil
	}
	return buildOutboundIdentity(selection)
}

func prepareAccountOutboundRequest(req *http.Request, account *Account) *http.Request {
	if req != nil {
		ApplyAccountOutboundIdentity(req.Context(), account, req)
	}
	return req
}

func accountOutboundUserAgent(ctx context.Context, account *Account) string {
	if i, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account)); ok {
		return i.UserAgent
	}
	return ""
}

// PreviewOutboundIdentity uses the same candidates as forwarding and accepts
// only identity fields; the preview API never needs account secrets.
func (s *SettingService) PreviewOutboundIdentity(ctx context.Context, account *Account, selection *OutboundIdentitySelection) (outboundidentity.Identity, error) {
	if account == nil || !validOutboundAccountKey(account.Platform, account.Type) {
		return outboundidentity.Identity{}, fmt.Errorf("invalid account platform or type")
	}
	if selection != nil {
		candidate := map[string]any{outboundIdentityCredential: *selection}
		if err := NormalizeAccountOutboundIdentity(account.Platform, account.Type, candidate); err != nil {
			return outboundidentity.Identity{}, err
		}
		selection = nil
		if normalized, ok := candidate[outboundIdentityCredential].(OutboundIdentitySelection); ok {
			selection = &normalized
		}
	}
	if selection != nil && selection.Preset != "" {
		i, err := resolveAccountIdentitySelection(ctx, account, *selection, s.resolveDefaultOutboundIdentity)
		if err != nil {
			return i, err
		}
		if account.Platform != PlatformOpenAI || i.Preset != "codex" {
			i.Source = "account"
			return i, nil
		}
	}
	if account.Platform == PlatformOpenAI && (selection != nil && selection.Preset == "codex" || account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream || s.resolveOutboundIdentityKey(ctx, outboundDefaultKey(account)).Preset == "codex") {
		i := resolveOpenAIOutboundIdentityFromSettings(ctx, account, s)
		headers := http.Header{}
		applyResolvedOpenAIOutboundIdentity(headers, i, account.UsesOpenAICodexProtocol())
		result := outboundidentity.Identity{Preset: "codex", UserAgent: i.UserAgent, Originator: i.Originator, Version: i.Version, Source: i.Source, Headers: map[string]string{}}
		for k := range headers {
			result.Headers[k] = headers.Get(k)
		}
		return result, nil
	}
	return s.resolveOutboundIdentityKey(ctx, outboundDefaultKey(account)), nil
}

func (s *SettingService) resolveOutboundIdentityKey(ctx context.Context, key string) outboundidentity.Identity {
	preset := key
	if platform, accountType, ok := strings.Cut(key, ":"); ok {
		preset = nativeOutboundPreset(platform)
		if accountType == AccountTypeBedrock {
			preset = "claude"
		}
		if configured := s.GetOutboundIdentitySettings(ctx).Defaults[key]; validateAccountIdentityPreset(&Account{Platform: platform, Type: accountType}, configured) == nil {
			preset = configured
		}
	}
	return s.resolveDefaultOutboundIdentity(ctx, preset)
}

func NormalizeAccountOutboundIdentity(platform, accountType string, credentials map[string]any) error {
	if credentials == nil {
		return nil
	}
	raw, ok := credentials[outboundIdentityCredential]
	if !ok || raw == nil {
		delete(credentials, outboundIdentityCredential)
		return nil
	}
	var selection OutboundIdentitySelection
	data, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(data, &selection) != nil {
		return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "invalid outbound identity")
	}
	selection.Preset, selection.UserAgent, selection.Version = strings.TrimSpace(selection.Preset), strings.TrimSpace(selection.UserAgent), strings.TrimSpace(selection.Version)
	if selection.Preset == "" && selection.UserAgent == "" && selection.Version == "" {
		delete(credentials, outboundIdentityCredential)
		return nil
	}
	if err := validateAccountIdentityPreset(&Account{Platform: platform, Type: accountType}, selection.Preset); err != nil {
		return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
	}
	if platform == PlatformOpenAI && selection.Preset == "codex" {
		if selection.UserAgent != "" || selection.Version != "" {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "Codex identity declarations use the existing user_agent setting")
		}
	}
	if _, err := buildOutboundIdentity(selection); err != nil {
		return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
	}
	credentials[outboundIdentityCredential] = selection
	return nil
}

func (s *SettingService) SetOutboundIdentitySettings(ctx context.Context, settings OutboundIdentitySettings) error {
	settings = cloneOutboundIdentitySettings(settings)
	for preset, selection := range settings.Profiles {
		if preset == "codex" {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "Codex uses its existing settings")
		}
		selection.Preset = preset
		if _, err := buildOutboundIdentity(selection); err != nil {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
		}
		settings.Profiles[preset] = selection
	}
	for key, preset := range settings.Defaults {
		parts := strings.Split(key, ":")
		if len(parts) != 2 || !validOutboundAccountKey(parts[0], parts[1]) || validateAccountIdentityPreset(&Account{Platform: parts[0], Type: parts[1]}, preset) != nil {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "invalid account default mapping")
		}
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	s.outboundIdentityMu.Lock()
	defer s.outboundIdentityMu.Unlock()
	if err := s.settingRepo.Set(ctx, SettingKeyOutboundIdentity, string(data)); err != nil {
		return err
	}
	s.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: settings, expires: time.Now().Add(time.Minute)})
	return nil
}

func validOutboundAccountKey(platform, accountType string) bool {
	switch platform {
	case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok, PlatformAntigravity, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax:
	default:
		return false
	}
	switch accountType {
	case AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey, AccountTypeUpstream, AccountTypeBedrock, AccountTypeServiceAccount:
		return true
	}
	return false
}

func (s *SettingService) GetOutboundIdentityView(ctx context.Context) OutboundIdentityView {
	view := OutboundIdentityView{Settings: s.GetOutboundIdentitySettings(ctx)}
	for _, preset := range outboundPresetNames {
		view.Presets = append(view.Presets, builtInOutboundIdentity(preset))
		view.Effective = append(view.Effective, s.resolveDefaultOutboundIdentity(ctx, preset))
	}
	return view
}

func (s *SettingService) installOutboundIdentityResolver() {
	outboundidentity.SetDefaultResolver(s.resolveOutboundIdentityKey)
}
