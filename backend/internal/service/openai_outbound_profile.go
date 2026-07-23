package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// openAIOutboundIdentity is the trusted client identity used for an upstream
// OpenAI request. It is deliberately resolved from account and system settings
// only; caller request headers never participate in this decision.
type openAIOutboundIdentity struct {
	UserAgent  string
	Originator string
	Version    string
}

// resolveOpenAIOutboundIdentity uses the account-specific Codex UA when it is
// valid, then the system setting, and finally the compiled-in default. A value
// is only valid when it can be paired with an official Codex originator.
func (s *OpenAIGatewayService) resolveOpenAIOutboundIdentity(ctx context.Context, account *Account) openAIOutboundIdentity {
	systemUA := ""
	if s != nil && s.settingService != nil {
		systemUA = s.settingService.GetOpenAICodexUserAgent(ctx)
	}
	accountUA := ""
	if account != nil {
		accountUA = account.GetOpenAIUserAgent()
	}
	return resolveOpenAIOutboundIdentityCandidates(accountUA, systemUA)
}

func resolveOpenAIOutboundIdentityCandidates(accountUA, systemUA string) openAIOutboundIdentity {
	if identity, ok := validOpenAIOutboundIdentity(accountUA); ok {
		return identity
	}
	if identity, ok := validOpenAIOutboundIdentity(systemUA); ok {
		return identity
	}
	identity, ok := validOpenAIOutboundIdentity(DefaultOpenAICodexUserAgent)
	if ok {
		return identity
	}
	// DefaultOpenAICodexUserAgent is a compile-time invariant covered by tests.
	return openAIOutboundIdentity{UserAgent: codexCLIUserAgent, Originator: "codex_cli_rs", Version: codexCLIVersion}
}

func validOpenAIOutboundIdentity(userAgent string) (openAIOutboundIdentity, bool) {
	originator, pairedUA, ok := openai.PairCodexClientIdentity(strings.TrimSpace(userAgent))
	if !ok {
		return openAIOutboundIdentity{}, false
	}
	version := openAIOutboundIdentityVersion(pairedUA)
	if version == "" {
		return openAIOutboundIdentity{}, false
	}
	return openAIOutboundIdentity{UserAgent: pairedUA, Originator: originator, Version: version}, true
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
// OAuth/ChatGPT internal requests additionally receive the originator derived
// from the final UA. Platform API requests never receive OAuth-only identity.
func (s *OpenAIGatewayService) applyOpenAIOutboundIdentity(ctx context.Context, account *Account, headers http.Header, useCodexIdentity bool) {
	applyResolvedOpenAIOutboundIdentity(headers, s.resolveOpenAIOutboundIdentity(ctx, account), useCodexIdentity)
}

func applyResolvedOpenAIOutboundIdentity(headers http.Header, identity openAIOutboundIdentity, useCodexIdentity bool) {
	if headers == nil {
		return
	}
	headers.Set("User-Agent", identity.UserAgent)
	// Keep the Codex protocol version aligned when an endpoint uses it. OAuth
	// endpoints always require it; API-key compact endpoints set it upstream.
	if useCodexIdentity || headers.Get("Version") != "" {
		headers.Set("Version", identity.Version)
	}
	if !useCodexIdentity {
		headers.Del("Originator")
		return
	}
	headers.Set("Originator", identity.Originator)
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
	if lowerName == "user-agent" || lowerName == "originator" || lowerName == "version" || lowerName == "openai-beta" {
		return true
	}
	if strings.HasPrefix(lowerName, "x-codex-") {
		return true
	}
	return lowerName == "session_id" || lowerName == "conversation_id"
}
