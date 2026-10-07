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
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/antigravity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/deepseek"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/geminicli"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/kimi"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/minimax"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

const SettingKeyOutboundIdentity = "outbound_identity"
const outboundIdentityCredential = "outbound_identity"

// OutboundIdentitySelection is an account/global candidate. Its fields form one
// validated identity; missing fields use the selected preset, never another
// configuration tier. Codex's existing account credentials remain authoritative.
//
// Headers carries configured values for the preset's runtime declarations only.
// Derived and pinned declarations have no configurable value, so no candidate
// can desynchronize a companion declaration or invent a header the preset does
// not render.
type OutboundIdentitySelection struct {
	Preset    string            `json:"preset"`
	UserAgent string            `json:"user_agent,omitempty"`
	Version   string            `json:"version,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timezone  string            `json:"timezone,omitempty"`
}

type OutboundIdentitySettings struct {
	Profiles map[string]OutboundIdentitySelection `json:"profiles"`
	Defaults map[string]string                    `json:"defaults"`
	// Runtime holds the persisted runtime-class declarations per preset. The
	// official client reads these from its host. The gateway persists fixed
	// Ubuntu defaults and allows explicit overrides, without collecting host facts.
	Runtime map[string]map[string]string `json:"runtime,omitempty"`
}

// OutboundIdentityDeclaration describes one provider-defined identity header a
// preset renders: its class decides whether a configured value is accepted.
type OutboundIdentityDeclaration struct {
	Name     string `json:"name"`
	Class    string `json:"class"`
	Editable bool   `json:"editable"`
	Builtin  string `json:"builtin"`
	Value    string `json:"value"`
}

// OutboundIdentityPresetDeclarations is the declared header block of one preset
// in official declaration order.
type OutboundIdentityPresetDeclarations struct {
	Preset  string                        `json:"preset"`
	Headers []OutboundIdentityDeclaration `json:"headers"`
}

type OutboundIdentityWireView struct {
	Protocol string `json:"protocol"`
	outboundidentity.Identity
}

type OutboundIdentityView struct {
	WireProfiles []OutboundIdentityWireView           `json:"wire_profiles"`
	Settings     OutboundIdentitySettings             `json:"settings"`
	Presets      []outboundidentity.Identity          `json:"presets"`
	ControlPlane []outboundidentity.Identity          `json:"control_plane"`
	Effective    []outboundidentity.Identity          `json:"effective"`
	Declarations []OutboundIdentityPresetDeclarations `json:"declarations"`
}

type cachedOutboundIdentitySettings struct {
	settings OutboundIdentitySettings
	expires  time.Time
}

var outboundClientVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.]+)?$`)
var outboundPresetNames = []string{"codex", "claude", "gemini", "grok", "antigravity", "deepseek", "minimax", "minimax_apikey", "kimi", "zcode"}

// versionlessOutboundUserAgents enumerates the client families whose official
// client publishes no version segment. MiniMax renders the bare product token
// `MiniMaxAgent` for every managed provider request and never puts its package
// version on the wire. Membership is an explicit per-preset exception recorded
// in docs/OUTBOUND_IDENTITY.md: it is not a general relaxation of the client
// version requirement, and an unlisted preset can never select an empty version
// or a versionless User-Agent.
var versionlessOutboundUserAgents = map[string]string{
	"minimax": minimax.ProductToken,
}

func emptyOutboundIdentitySettings() OutboundIdentitySettings {
	return OutboundIdentitySettings{
		Profiles: map[string]OutboundIdentitySelection{},
		Defaults: map[string]string{},
		Runtime:  map[string]map[string]string{},
	}
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
	case PlatformDeepseek:
		// DeepSeek platform accounts advertise the pinned harness identity by
		// default. The provider-defined client family is the published harness,
		// not Codex; an account or type-default selection can still opt back
		// into any compatible preset.
		return "deepseek"
	case PlatformMiniMax:
		// MiniMax platform accounts advertise the pinned MiniMax Code product
		// identity by default. The provider-defined client family is the
		// official MiniMax client, not Codex; an account or type-default
		// selection can still opt back into any compatible preset.
		return "minimax"
	case PlatformKimi:
		// Kimi / Moonshot platform accounts advertise the pinned Kimi Code
		// identity by default. The provider-defined client family is the
		// provider's own official client, not Codex; an account or type-default
		// selection can still opt back into any compatible preset. The device
		// declarations of that family are runtime state owned by the persisted
		// outbound identity settings.
		return "kimi"
	case PlatformZhipu:
		// Zhipu / GLM platform accounts advertise the pinned ZCode identity by
		// default, for both API-key and account-link OAuth accounts. ZCode is the
		// provider's own official client, so it is the provider-defined client
		// family rather than Codex; an account or type-default selection can
		// still opt back into any compatible preset. OAuth/setup-token accounts
		// are pinned to this family by validateAccountIdentityPreset.
		return "zcode"
	case PlatformTypeSafe:
		// TypeSafe is an API-key compatible supplier with no provider-defined
		// client family, version or identity header. It reuses the same
		// configurable Codex preset mapping as the other API-key compatible
		// platforms instead of inventing a TypeSafe CLI identity.
		return "codex"
	default:
		return "codex"
	}
}

func nativeAccountOutboundPreset(platform, accountType string) string {
	if platform == PlatformMiniMax && accountType == AccountTypeAPIKey {
		return minimax.APIKeyPreset
	}
	return nativeOutboundPreset(platform)
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
		i.UserAgent, i.Originator, i.Version = claude.DefaultHeaders()["User-Agent"], "claude-cli", claude.CLIVersion()
		if claude.IsSupportedCLIVersion(strings.TrimSpace(os.Getenv(claude.CLIVersionEnv))) {
			i.Source = "environment"
		}
		for k, v := range claude.DefaultHeaders() {
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
		i.Headers["x-grok-client-mode"] = xai.CLIClientMode
	case "antigravity":
		return antigravity.DefaultIdentity()
	case "deepseek":
		return deepseek.DefaultIdentity()
	case "minimax":
		return minimax.DefaultIdentity()
	case minimax.APIKeyPreset:
		return minimax.APIKeyIdentity()
	case "kimi":
		return kimi.DefaultIdentity()
	case "zcode":
		return zcode.DefaultIdentity()
	default:
		return outboundidentity.Identity{}
	}
	i.Headers["User-Agent"] = i.UserAgent
	return i
}

// Outbound identity header classes. The class decides whether a configuration
// tier may supply a value for a declared header.
const (
	// outboundHeaderDerived is rendered from the resolved trusted triple. A
	// configured value could desynchronize it, so it is never overridable.
	outboundHeaderDerived = "derived"
	// outboundHeaderPinned is a fixed provider declaration: a client family
	// token or an SDK fingerprint. It is never overridable.
	outboundHeaderPinned = "pinned"
	// outboundHeaderRuntime is a host fact the provider's official client
	// resolves at run time. It describes this deployment rather than the client
	// family, so the persisted settings and an account selection may supply it.
	outboundHeaderRuntime = "runtime"
)

// outboundDeclaredHeader is one provider-defined identity header a preset
// renders, with the class that decides who may set its value.
type outboundDeclaredHeader struct {
	Name     string
	Class    string
	Validate func(value string) error
}

// outboundPresetHeaderClasses classifies the headers a preset declares beyond
// its User-Agent. A name that is missing keeps the read-only default instead of
// becoming configurable by accident, so extending a preset cannot silently
// widen the configurable surface.
var outboundPresetHeaderClasses = map[string]map[string]string{
	"zcode": {
		zcode.HeaderAppVersion: outboundHeaderDerived,
		"X-Client-Language":    outboundHeaderRuntime,
		"X-Client-Timezone":    outboundHeaderRuntime,
		"X-Platform":           outboundHeaderRuntime,
		"X-Os-Category":        outboundHeaderRuntime,
		"X-Os-Version":         outboundHeaderRuntime,
	},
	"codex": {
		// The Codex Originator and Version declarations follow the resolved
		// Codex triple exactly like its User-Agent. Only their class is recorded
		// here; their values stay owned by the existing Codex identity resolver.
		"Originator": outboundHeaderDerived,
		"Version":    outboundHeaderDerived,
	},
	"grok": {
		"x-grok-client-version": outboundHeaderDerived,
	},
	"kimi": {
		kimi.HeaderPlatform:    outboundHeaderPinned,
		kimi.HeaderVersion:     outboundHeaderDerived,
		kimi.HeaderDeviceName:  outboundHeaderRuntime,
		kimi.HeaderDeviceModel: outboundHeaderRuntime,
		kimi.HeaderOSVersion:   outboundHeaderRuntime,
		kimi.HeaderDeviceID:    outboundHeaderRuntime,
	},
}

var outboundPresetHeaderValidators = map[string]map[string]func(string) error{
	"zcode": {
		"X-Client-Language": outboundidentity.ValidateLanguage,
		"X-Client-Timezone": outboundidentity.ValidateTimezone,
		"X-Platform":        validateOutboundIdentityDeviceFact,
		"X-Os-Category":     validateOutboundIdentityDeviceFact,
		"X-Os-Version":      validateOutboundIdentityDeviceFact,
	},
	"kimi": {
		kimi.HeaderDeviceName:  validateOutboundIdentityDeviceName,
		kimi.HeaderDeviceModel: validateOutboundIdentityDeviceFact,
		kimi.HeaderOSVersion:   validateOutboundIdentityDeviceFact,
		kimi.HeaderDeviceID:    validateOutboundIdentityDeviceID,
	},
}

// outboundPresetHeaderOrder is the official declaration order of a preset whose
// header block is provider-defined. Headers outside the list keep a stable
// alphabetical order after it.
var outboundPresetHeaderOrder = map[string][]string{
	"kimi": {
		kimi.HeaderPlatform,
		kimi.HeaderVersion,
		kimi.HeaderDeviceName,
		kimi.HeaderDeviceModel,
		kimi.HeaderOSVersion,
		kimi.HeaderDeviceID,
	},
}

const outboundIdentityHeaderValueMaxLength = 256

func outboundHeaderClassName(preset, name string) string {
	if strings.EqualFold(name, "User-Agent") {
		return outboundHeaderDerived
	}
	if class, ok := outboundPresetHeaderClasses[preset][name]; ok {
		return class
	}
	return outboundHeaderPinned
}

// declaredOutboundHeaders lists every header a preset renders, in the order the
// official client declares it. The name set is read from the built-in snapshot,
// so the page can never claim a declaration the preset does not render or omit
// one it does.
func declaredOutboundHeaders(preset string) []outboundDeclaredHeader {
	builtin := builtInOutboundIdentity(preset)
	headers := make([]outboundDeclaredHeader, 0, len(builtin.Headers))
	seen := make(map[string]bool, len(builtin.Headers))
	appendName := func(name string) {
		if seen[name] {
			return
		}
		if _, ok := builtin.Headers[name]; !ok {
			return
		}
		seen[name] = true
		headers = append(headers, outboundDeclaredHeader{
			Name:     name,
			Class:    outboundHeaderClassName(preset, name),
			Validate: outboundPresetHeaderValidators[preset][name],
		})
	}
	appendName("User-Agent")
	for _, name := range outboundPresetHeaderOrder[preset] {
		appendName(name)
	}
	rest := make([]string, 0, len(builtin.Headers))
	for name := range builtin.Headers {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		appendName(name)
	}
	return headers
}

func validateOutboundIdentityHeaderValue(value string) error {
	if len(value) > outboundIdentityHeaderValueMaxLength {
		return fmt.Errorf("header value exceeds %d characters", outboundIdentityHeaderValueMaxLength)
	}
	for _, c := range value {
		if c < 32 || c > 126 {
			return fmt.Errorf("header value must contain printable ASCII")
		}
	}
	return nil
}

// validateOutboundIdentityDeviceFact accepts a host description or kernel
// release. The official sanitizer substitutes `unknown` for an empty fact, so
// no configured value may be blank either.
func validateOutboundIdentityDeviceFact(value string) error {
	if err := validateOutboundIdentityHeaderValue(value); err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("header value must not be empty")
	}
	return nil
}

func validateOutboundIdentityDeviceName(value string) error {
	if err := validateOutboundIdentityDeviceFact(value); err != nil {
		return err
	}
	if brandidentity.ContainsBrand(value) {
		return fmt.Errorf("device name must not contain the project brand")
	}
	return nil
}

func validateOutboundTimezone(preset, zone string) error {
	if zone == "" {
		return nil
	}
	if preset != minimax.Preset && preset != minimax.APIKeyPreset {
		return fmt.Errorf("timezone configuration is only supported for MiniMax presets")
	}
	return outboundidentity.ValidateTimezone(zone)
}

func validateOutboundIdentityDeviceID(value string) error {
	if err := validateOutboundIdentityDeviceFact(value); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(value)
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("device id must be a uuid")
	}
	if parsed.String() != trimmed {
		return fmt.Errorf("device id must use the canonical lowercase uuid form")
	}
	return nil
}

// normalizeOutboundHeaderValues validates configured values for a preset's
// declared headers. Only runtime-class declarations accept a value: a derived
// or pinned name is rejected rather than silently ignored, and an undeclared
// name can never be introduced.
func normalizeOutboundHeaderValues(preset string, values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	byName := make(map[string]outboundDeclaredHeader)
	for _, header := range declaredOutboundHeaders(preset) {
		byName[strings.ToLower(header.Name)] = header
	}
	normalized := make(map[string]string, len(values))
	for name, value := range values {
		header, ok := byName[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, fmt.Errorf("preset %q does not declare identity header %q", preset, strings.TrimSpace(name))
		}
		if header.Class != outboundHeaderRuntime {
			return nil, fmt.Errorf("identity header %q is %s and cannot be configured", header.Name, header.Class)
		}
		trimmed := strings.TrimSpace(value)
		if header.Validate != nil {
			if err := header.Validate(trimmed); err != nil {
				return nil, fmt.Errorf("invalid %s: %w", header.Name, err)
			}
		}
		if _, exists := normalized[header.Name]; exists {
			return nil, fmt.Errorf("duplicate identity header %q", header.Name)
		}
		normalized[header.Name] = trimmed
	}
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

// applyOutboundHeaderValues overlays persisted or account values for a preset's
// runtime declarations and reports whether any value was applied. An invalid
// stored candidate falls through as a unit, leaving the built-in declaration in
// place rather than failing the request.
func applyOutboundHeaderValues(identity outboundidentity.Identity, preset string, values map[string]string) (outboundidentity.Identity, bool) {
	if len(values) == 0 || len(identity.Headers) == 0 {
		return identity, false
	}
	normalized, err := normalizeOutboundHeaderValues(preset, values)
	if err != nil || len(normalized) == 0 {
		return identity, false
	}
	headers := maps.Clone(identity.Headers)
	maps.Copy(headers, normalized)
	identity.Headers = headers
	return identity, true
}

// mergeOutboundRuntimeHeaders copies the preset's runtime declarations from a
// fully resolved identity into a candidate that was built from explicit fields.
// A complete account candidate replaces only the declarations it names, so the
// deployment-owned runtime values still come from the global tier.
func mergeOutboundRuntimeHeaders(identity, resolved outboundidentity.Identity) outboundidentity.Identity {
	if identity.Preset == "" || identity.Preset != resolved.Preset || len(resolved.Headers) == 0 {
		return identity
	}
	headers := maps.Clone(identity.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	merged := false
	for _, header := range declaredOutboundHeaders(identity.Preset) {
		if header.Class != outboundHeaderRuntime {
			continue
		}
		value := resolved.Headers[header.Name]
		if value == "" {
			continue
		}
		headers[header.Name] = value
		merged = true
	}
	if !merged {
		return identity
	}
	identity.Headers = headers
	return identity
}

func buildOutboundIdentity(selection OutboundIdentitySelection) (outboundidentity.Identity, error) {
	i := builtInOutboundIdentity(selection.Preset)
	if err := validateOutboundTimezone(selection.Preset, selection.Timezone); err != nil {
		return i, err
	}
	if selection.Timezone != "" {
		i.Timezone = selection.Timezone
	}
	if _, err := normalizeOutboundHeaderValues(selection.Preset, selection.Headers); err != nil {
		return i, err
	}
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
	} else if versionlessToken, ok := versionlessOutboundUserAgents[selection.Preset]; ok {
		// Enumerated versionless client family. The official client publishes no
		// version segment, so the bare product token is the complete declaration.
		// Requiring an exact match keeps the exemption narrow: an arbitrary
		// User-Agent can never borrow it, and a caller-supplied version is
		// rejected instead of being rendered into a family that has none.
		if ua != versionlessToken {
			return i, fmt.Errorf("User-Agent must match the selected preset")
		}
		if strings.TrimSpace(selection.Version) != "" {
			return i, fmt.Errorf("selected preset does not declare a client version")
		}
		i.UserAgent, i.Version = ua, ""
	} else {
		if brandidentity.ContainsBrand(ua) {
			return i, fmt.Errorf("User-Agent must not contain the project brand")
		}
		prefix := map[string]string{"claude": "claude-cli/", "gemini": "GeminiCLI/", "grok": "grok-shell/", "antigravity": "antigravity/", "deepseek": "deepseek-harness/", "kimi": "kimi-code-cli/", "zcode": "ZCode/", "minimax_apikey": "Anthropic/JS "}[selection.Preset]
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
			if i.Preset == minimax.APIKeyPreset {
				i.UserAgent = strings.Replace(i.UserAgent, " "+i.Version, " "+version, 1)
			} else {
				i.UserAgent = strings.Replace(i.UserAgent, "/"+i.Version, "/"+version, 1)
			}
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
	if i.Preset == "deepseek" && !deepseek.IsSupportedVersion(i.Version) {
		return i, fmt.Errorf("deepseek version is below the supported baseline")
	}
	// Kimi Code ships one released version line for the CLI, the `kimi web`
	// server and the native binaries, so a monotonic floor is safe. The VS Code
	// extension is a different product token and never selects this preset.
	if i.Preset == "kimi" && !kimi.IsSupportedVersion(i.Version) {
		return i, fmt.Errorf("kimi version is below the supported baseline")
	}
	// ZCode deliberately has no monotonic version floor: the official client
	// ships two parallel version lines (desktop product and standalone CLI), so a
	// floor drawn on one line would reject the other line's legitimate value.
	// Only the shared client-version shape is enforced.
	if i.Preset == "zcode" && !zcode.IsSupportedVersion(i.Version) {
		return i, fmt.Errorf("invalid ZCode client version")
	}
	if i.Preset == minimax.APIKeyPreset && (i.Version != minimax.SDKVersion || i.UserAgent != minimax.APIKeyIdentity().UserAgent) {
		return i, fmt.Errorf("MiniMax API key SDK version is pinned to the reviewed dependency")
	}
	switch i.Preset {
	case "deepseek":
		if i.UserAgent != deepseek.UserAgent(i.Version) {
			return i, fmt.Errorf("DeepSeek attribution must match the official Harness declaration")
		}
	case "kimi":
		if i.UserAgent != kimi.UserAgent(i.Version) {
			return i, fmt.Errorf("kimi User-Agent must match the selected product version")
		}
	case "zcode":
		if i.UserAgent != zcode.UserAgent(i.Version) {
			return i, fmt.Errorf("ZCode product User-Agent must not override the pinned SDK suffix")
		}
	}
	i.Headers["User-Agent"] = i.UserAgent
	if i.Preset == "codex" {
		i.Headers["Originator"], i.Headers["Version"] = i.Originator, i.Version
	}
	if i.Preset == "grok" {
		i.Headers["x-grok-client-version"] = i.Version
	}
	if i.Preset == "kimi" {
		// A User-Agent or version candidate must not desynchronize the version
		// companion the official client declares next to its User-Agent.
		i.Headers[kimi.HeaderPlatform] = kimi.PlatformToken
		i.Headers[kimi.HeaderVersion] = i.Version
	}
	if i.Preset == "zcode" {
		i.Headers[zcode.HeaderAppVersion] = i.Version
	}
	i, _ = applyOutboundHeaderValues(i, selection.Preset, selection.Headers)
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
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	settings, _ := s.loadOutboundIdentitySettings(dbCtx)
	s.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: settings, expires: time.Now().Add(time.Minute)})
	return cloneOutboundIdentitySettings(settings)
}

// loadOutboundIdentitySettings reads and decodes the persisted settings without
// consulting the cache, so a writer never merges onto a stale snapshot. A read
// failure reports the error while still returning the documented fallback.
func (s *SettingService) loadOutboundIdentitySettings(ctx context.Context) (OutboundIdentitySettings, error) {
	settings := emptyOutboundIdentitySettings()
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOutboundIdentity)
	if err == nil && raw != "" {
		if json.Unmarshal([]byte(raw), &settings) != nil {
			slog.WarnContext(ctx, "outbound identity settings fallback", "reason", "invalid_settings_json")
			settings = emptyOutboundIdentitySettings()
		}
	} else if err != nil && !errors.Is(err, ErrSettingNotFound) {
		slog.WarnContext(ctx, "outbound identity settings fallback", "reason", "settings_read_failed")
		return normalizeOutboundIdentitySettings(settings), err
	} else {
		// Import the previous setting only before the unified configuration has
		// been saved. Clearing a profile later must genuinely restore defaults.
		legacy, legacyErr := s.settingRepo.GetValue(ctx, SettingKeyAntigravityUserAgentVersion)
		if version := antigravity.NormalizeUserAgentVersion(legacy); legacyErr == nil && version != "" {
			settings.Profiles["antigravity"] = OutboundIdentitySelection{Preset: "antigravity", Version: version}
		}
	}
	return normalizeOutboundIdentitySettings(settings), nil
}

// normalizeOutboundIdentitySettings replaces absent maps so decoded payloads with
// explicit nulls behave like absent keys.
func normalizeOutboundIdentitySettings(settings OutboundIdentitySettings) OutboundIdentitySettings {
	if settings.Profiles == nil {
		settings.Profiles = map[string]OutboundIdentitySelection{}
	}
	if settings.Defaults == nil {
		settings.Defaults = map[string]string{}
	}
	if settings.Runtime == nil {
		settings.Runtime = map[string]map[string]string{}
	}
	return settings
}

func cloneOutboundIdentitySettings(settings OutboundIdentitySettings) OutboundIdentitySettings {
	result := emptyOutboundIdentitySettings()
	for preset, selection := range settings.Profiles {
		selection.Headers = maps.Clone(selection.Headers)
		result.Profiles[preset] = selection
	}
	maps.Copy(result.Defaults, settings.Defaults)
	for preset, values := range settings.Runtime {
		if len(values) == 0 {
			continue
		}
		result.Runtime[preset] = maps.Clone(values)
	}
	return result
}

// ensureRuntimeOutboundHeaders materializes the runtime declarations a preset
// takes from the fixed Ubuntu defaults, so the advertised identity is
// stable across restarts and shared instances. It runs at startup and from the
// admin settings view; the forwarding path only reads.
func (s *SettingService) ensureRuntimeOutboundHeaders(ctx context.Context) {
	if s == nil || s.settingRepo == nil {
		return
	}
	if _, err, _ := s.outboundRuntimeSF.Do("materialize", func() (any, error) {
		return nil, s.materializeRuntimeOutboundHeaders(ctx)
	}); err != nil {
		slog.WarnContext(ctx, "outbound identity runtime declarations fallback", "reason", "materialize_failed", "error", err)
	}
}

func (s *SettingService) materializeRuntimeOutboundHeaders(ctx context.Context) error {
	s.outboundIdentityMu.Lock()
	defer s.outboundIdentityMu.Unlock()
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	// This persists the same end state an administrator save would produce,
	// including the one-time legacy Antigravity import when the unified setting
	// has never been written. That import is documented as applying only before
	// the unified configuration is first saved, so materializing here reaches
	// the documented end state deterministically instead of leaving the
	// deployment on a compiled default that mints a new device id per process.
	settings, err := s.loadOutboundIdentitySettings(dbCtx)
	if err != nil {
		return err
	}
	changed := false
	for _, preset := range outboundPresetNames {
		missing := map[string]string{}
		for _, header := range declaredOutboundHeaders(preset) {
			if header.Class != outboundHeaderRuntime || header.Name == "" {
				continue
			}
			if settings.Runtime[preset][header.Name] != "" {
				continue
			}
			if builtin := builtInOutboundIdentity(preset).Headers[header.Name]; builtin != "" {
				missing[header.Name] = builtin
			}
		}
		if len(missing) == 0 {
			continue
		}
		if settings.Runtime[preset] == nil {
			settings.Runtime[preset] = map[string]string{}
		}
		maps.Copy(settings.Runtime[preset], missing)
		changed = true
	}
	if !changed {
		return nil
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(dbCtx, SettingKeyOutboundIdentity, string(data)); err != nil {
		return err
	}
	s.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: settings, expires: time.Now().Add(time.Minute)})
	slog.InfoContext(ctx, "outbound_identity_runtime_declarations_materialized")
	return nil
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
			i, _ = applyOutboundHeaderValues(i, preset, settings.Runtime[preset])
			i, _ = applyOutboundHeaderValues(i, preset, selection.Headers)
			return i
		}
	}
	i, applied := applyOutboundHeaderValues(builtInOutboundIdentity(preset), preset, settings.Runtime[preset])
	if applied {
		// The values come from the persisted global settings rather than from the
		// compiled default, so the reported source follows the actual origin.
		i.Source = "global"
	}
	return i
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
	preset := nativeAccountOutboundPreset(account.Platform, account.Type)
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

// WithStandaloneOutboundIdentity starts an operation using independently owned
// provider credentials (monitor or audit endpoint). It must not inherit the
// forwarding account's identity. Nested requests reuse the returned snapshot;
// a new endpoint operation resolves its own API-key type default.
func WithStandaloneOutboundIdentity(ctx context.Context, platform string) context.Context {
	// A nil-account Codex cache in the forwarding scope is a different owner.
	ctx = context.WithValue(ctx, outboundIdentityScopeKey{}, &outboundIdentityScope{})
	ctx = outboundidentity.WithIdentity(ctx, outboundidentity.Identity{})
	key := platform + ":" + AccountTypeAPIKey
	identity, ok := outboundidentity.Default(ctx, key)
	if !ok {
		identity = (*SettingService)(nil).resolveOutboundIdentityKey(ctx, key)
	}
	if platform == PlatformOpenAI && identity.Preset == "codex" {
		// Match native Platform API-key requests: the UA carries the triple,
		// while OAuth-only Originator/Version declarations remain omitted.
		identity.Headers = maps.Clone(identity.Headers)
		for name := range identity.Headers {
			if strings.EqualFold(name, "Originator") || strings.EqualFold(name, "Version") {
				delete(identity.Headers, name)
			}
		}
	}
	return outboundidentity.WithIdentity(ctx, identity)
}

// withNativeOAuthOutboundIdentity starts a pre-account authorization operation.
// Its native family and snapshot belong to these credentials, independently of
// a caller's account snapshot, compatible API-key default, or cached scope.
func withNativeOAuthOutboundIdentity(ctx context.Context, platform string) context.Context {
	ctx = context.WithValue(ctx, outboundIdentityScopeKey{}, &outboundIdentityScope{})
	ctx = outboundidentity.WithIdentity(ctx, outboundidentity.Identity{})
	return outboundidentity.WithIdentity(ctx, outboundDefaultIdentity(ctx, nativeOutboundPreset(platform)))
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
// through as a unit, never mixing fields from unrelated sources. Runtime
// declarations are deployment state rather than account identity, so a complete
// candidate still inherits them from the global tier before its own header
// declarations are laid on top.
func resolveAccountIdentitySelection(ctx context.Context, account *Account, selection OutboundIdentitySelection, resolve func(context.Context, string) outboundidentity.Identity) (outboundidentity.Identity, error) {
	if err := validateAccountIdentityPreset(account, selection.Preset); err != nil {
		return outboundidentity.Identity{}, err
	}
	if _, err := normalizeOutboundHeaderValues(selection.Preset, selection.Headers); err != nil {
		return outboundidentity.Identity{}, err
	}
	if err := validateOutboundTimezone(selection.Preset, selection.Timezone); err != nil {
		return outboundidentity.Identity{}, err
	}
	if selection.UserAgent == "" && selection.Version == "" {
		identity := resolve(ctx, selection.Preset)
		resolved, _ := applyOutboundHeaderValues(identity, selection.Preset, selection.Headers)
		if selection.Timezone != "" {
			resolved.Timezone = selection.Timezone
		}
		return resolved, nil
	}
	identity, err := buildOutboundIdentity(selection)
	if err != nil {
		return identity, err
	}
	global := resolve(ctx, selection.Preset)
	identity = mergeOutboundRuntimeHeaders(identity, global)
	if selection.Timezone == "" {
		identity.Timezone = global.Timezone
	}
	resolved, _ := applyOutboundHeaderValues(identity, selection.Preset, selection.Headers)
	return resolved, nil
}

func prepareAccountOutboundRequest(req *http.Request, account *Account) *http.Request {
	if req != nil {
		if account != nil && account.Platform == PlatformGrok && HTTPUpstreamProfileFromContext(req.Context()) == HTTPUpstreamProfileDefault {
			*req = *req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileGrok))
		}
		ApplyAccountOutboundIdentity(req.Context(), account, req)
		prepareMiniMaxRequestState(req, account)
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
		preset = nativeAccountOutboundPreset(platform, accountType)
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
	if selection.Preset == "" && selection.UserAgent == "" && selection.Version == "" && selection.Timezone == "" && len(selection.Headers) == 0 {
		delete(credentials, outboundIdentityCredential)
		return nil
	}
	if err := validateAccountIdentityPreset(&Account{Platform: platform, Type: accountType}, selection.Preset); err != nil {
		return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
	}
	if platform == PlatformOpenAI && selection.Preset == "codex" {
		if selection.UserAgent != "" || selection.Version != "" || selection.Timezone != "" || len(selection.Headers) > 0 {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "Codex identity declarations use the existing user_agent setting")
		}
	}
	if _, err := buildOutboundIdentity(selection); err != nil {
		return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
	}
	headers, err := normalizeOutboundHeaderValues(selection.Preset, selection.Headers)
	if err != nil {
		return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
	}
	selection.Headers = headers
	credentials[outboundIdentityCredential] = selection
	return nil
}

// normalizeBulkAccountOutboundIdentity validates a shared JSONB credential
// update against every credential owner before the repository writes any row.
// A null/empty selection stays null because bulk updates merge JSONB keys;
// runtime resolution treats that value as an explicit request to inherit.
func normalizeBulkAccountOutboundIdentity(credentials map[string]any, accounts []*Account) error {
	raw, ok := credentials[outboundIdentityCredential]
	if !ok {
		return nil
	}

	var normalized any
	validated := false
	for _, account := range accounts {
		if account == nil {
			continue
		}
		candidate := map[string]any{outboundIdentityCredential: raw}
		if err := NormalizeAccountOutboundIdentity(account.Platform, account.Type, candidate); err != nil {
			return err
		}
		normalized = candidate[outboundIdentityCredential]
		validated = true
	}
	if !validated {
		candidate := map[string]any{outboundIdentityCredential: raw}
		if err := NormalizeAccountOutboundIdentity("", "", candidate); err != nil {
			return err
		}
		normalized = candidate[outboundIdentityCredential]
	}
	credentials[outboundIdentityCredential] = normalized
	return nil
}

func (s *SettingService) SetOutboundIdentitySettings(ctx context.Context, settings OutboundIdentitySettings) error {
	settings = normalizeOutboundIdentitySettings(cloneOutboundIdentitySettings(settings))
	for preset, selection := range settings.Profiles {
		if preset == "codex" {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "Codex uses its existing settings")
		}
		selection.Preset = preset
		selection.UserAgent = strings.TrimSpace(selection.UserAgent)
		selection.Version = strings.TrimSpace(selection.Version)
		if _, err := buildOutboundIdentity(selection); err != nil {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
		}
		headers, err := normalizeOutboundHeaderValues(preset, selection.Headers)
		if err != nil {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
		}
		selection.Headers = headers
		settings.Profiles[preset] = selection
	}
	for key, preset := range settings.Defaults {
		preset = strings.TrimSpace(preset)
		parts := strings.Split(key, ":")
		if len(parts) != 2 || !validOutboundAccountKey(parts[0], parts[1]) || validateAccountIdentityPreset(&Account{Platform: parts[0], Type: parts[1]}, preset) != nil {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", "invalid account default mapping")
		}
		settings.Defaults[key] = preset
	}
	for _, preset := range outboundRuntimePresetKeys(settings.Runtime) {
		if builtInOutboundIdentity(preset).UserAgent == "" {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", fmt.Sprintf("unknown identity preset %q", preset))
		}
		values, err := normalizeOutboundHeaderValues(preset, settings.Runtime[preset])
		if err != nil {
			return infraerrors.BadRequest("OUTBOUND_IDENTITY_INVALID", err.Error())
		}
		if len(values) == 0 {
			delete(settings.Runtime, preset)
			continue
		}
		settings.Runtime[preset] = values
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

// outboundRuntimePresetKeys returns the runtime map keys in a deterministic
// order so a rejected payload reports the same preset every time.
func outboundRuntimePresetKeys(runtime map[string]map[string]string) []string {
	keys := make([]string, 0, len(runtime))
	for preset := range runtime {
		keys = append(keys, preset)
	}
	sort.Strings(keys)
	return keys
}

func validOutboundAccountKey(platform, accountType string) bool {
	switch platform {
	case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok, PlatformAntigravity, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformTypeSafe:
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
	// Opening the settings page also materializes any runtime declaration this
	// deployment has not generated yet, so the values shown are the values that
	// will actually be sent and stay stable once persisted.
	s.ensureRuntimeOutboundHeaders(ctx)
	view := OutboundIdentityView{Settings: s.GetOutboundIdentitySettings(ctx)}
	for _, preset := range outboundPresetNames {
		builtin := builtInOutboundIdentity(preset)
		effective := s.resolveDefaultOutboundIdentity(ctx, preset)
		for _, protocol := range []string{"anthropic", "chat_completions", "responses"} {
			if _, ok := effective.Inference[protocol]; ok {
				view.WireProfiles = append(view.WireProfiles, OutboundIdentityWireView{Protocol: protocol, Identity: effective.ForProtocol(protocol)})
			}
		}
		view.Presets = append(view.Presets, builtin)
		view.Effective = append(view.Effective, effective)
		switch preset {
		case "deepseek":
			view.ControlPlane = append(view.ControlPlane, deepseek.ControlIdentity(effective))
		case "minimax":
			view.ControlPlane = append(view.ControlPlane, minimax.ControlIdentity(effective))
		case "zcode":
			view.ControlPlane = append(view.ControlPlane, zcode.ControlIdentity(effective))
		}
		view.Declarations = append(view.Declarations, outboundPresetDeclarations(preset, builtin, effective))
	}
	return view
}

// outboundPresetDeclarations reports every header a preset renders, its class,
// and the built-in and effective values, so the settings page never has to know
// a preset's header block in advance.
func outboundPresetDeclarations(preset string, builtin, effective outboundidentity.Identity) OutboundIdentityPresetDeclarations {
	declarations := OutboundIdentityPresetDeclarations{Preset: preset, Headers: []OutboundIdentityDeclaration{}}
	for _, header := range declaredOutboundHeaders(preset) {
		declarations.Headers = append(declarations.Headers, OutboundIdentityDeclaration{
			Name:     header.Name,
			Class:    header.Class,
			Editable: header.Class == outboundHeaderRuntime,
			Builtin:  builtin.Headers[header.Name],
			Value:    effective.Headers[header.Name],
		})
	}
	return declarations
}

func (s *SettingService) installOutboundIdentityResolver() {
	outboundidentity.SetDefaultResolver(s.resolveOutboundIdentityKey)
}

func selectionFromIdentity(identity outboundidentity.Identity) OutboundIdentitySelection {
	headers := map[string]string{}
	for _, h := range declaredOutboundHeaders(identity.Preset) {
		if h.Class == outboundHeaderRuntime {
			headers[h.Name] = identity.Headers[h.Name]
		}
	}
	return OutboundIdentitySelection{Preset: identity.Preset, UserAgent: identity.UserAgent, Version: identity.Version, Headers: headers, Timezone: identity.Timezone}
}
