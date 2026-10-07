// Package outboundidentity carries trusted, immutable client identity snapshots.
// Account selection and configuration resolution belong to the service layer;
// protocol clients only render the selected snapshot. Inbound headers are never
// an identity source.
package outboundidentity

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"sync"
)

type Identity struct {
	// Timezone is MiniMax request metadata configuration, never an extra wire header.
	Timezone       string                 `json:"timezone,omitempty"`
	AccountID      int64                  `json:"-"`
	Preset         string                 `json:"preset"`
	UserAgent      string                 `json:"user_agent"`
	Originator     string                 `json:"originator"`
	Version        string                 `json:"version"`
	Source         string                 `json:"source"`
	Headers        map[string]string      `json:"headers"`
	ControlHeaders map[string]string      `json:"control_headers,omitempty"`
	Inference      map[string]WireProfile `json:"inference,omitempty"`
}

// WireProfile is part of the trusted snapshot, never read from request headers.
// A protocol changes SDK declarations, not identity source or client family.
type WireProfile struct {
	UserAgentSuffix string            `json:"user_agent_suffix,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
}

func cloneProfiles(profiles map[string]WireProfile) map[string]WireProfile {
	if profiles == nil {
		return nil
	}
	result := make(map[string]WireProfile, len(profiles))
	for key, profile := range profiles {
		profile.Headers = maps.Clone(profile.Headers)
		result[key] = profile
	}
	return result
}

// ForProtocol renders a snapshot-owned SDK profile without changing its source.
func (i Identity) ForProtocol(protocol string) Identity {
	if profile, ok := i.Inference[protocol]; ok {
		i.Headers = maps.Clone(i.Headers)
		if i.Headers == nil {
			i.Headers = map[string]string{}
		}
		maps.Copy(i.Headers, profile.Headers)
		if profile.UserAgentSuffix != "" {
			i.UserAgent += " " + profile.UserAgentSuffix
		}
		i.Headers["User-Agent"] = i.UserAgent
		i.Inference = nil
	}
	return i
}

// RequestProtocol identifies only the SDK wire format; it never selects a preset.
func RequestProtocol(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	path := strings.TrimRight(req.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/messages"), strings.HasSuffix(path, "/messages/count_tokens"):
		return "anthropic"
	case strings.HasSuffix(path, "/chat/completions"):
		return "chat_completions"
	case strings.HasSuffix(path, "/responses"):
		return "responses"
	}
	return ""
}

type contextKey struct{}
type resolverKey struct{}

// WithResolver scopes a settings source to a service operation without mutating
// the application-wide resolver. It also supports isolated protocol tests.
func WithResolver(ctx context.Context, resolve func(context.Context, string) Identity) context.Context {
	return context.WithValue(ctx, resolverKey{}, resolve)
}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	identity.Headers = maps.Clone(identity.Headers)
	identity.ControlHeaders = maps.Clone(identity.ControlHeaders)
	identity.Inference = cloneProfiles(identity.Inference)
	return context.WithValue(ctx, contextKey{}, identity)
}

func FromContext(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	i, ok := ctx.Value(contextKey{}).(Identity)
	i.Headers = maps.Clone(i.Headers)
	i.ControlHeaders = maps.Clone(i.ControlHeaders)
	i.Inference = cloneProfiles(i.Inference)
	return i, ok && i.UserAgent != ""
}

var runtime struct {
	sync.RWMutex
	resolve func(context.Context, string) Identity
}

// SetDefaultResolver is wired once at application startup. The callback owns
// settings caching; requests carry their own snapshot after account selection.
func SetDefaultResolver(resolve func(context.Context, string) Identity) {
	runtime.Lock()
	defer runtime.Unlock()
	runtime.resolve = resolve
}

func Default(ctx context.Context, preset string) (Identity, bool) {
	if i, ok := FromContext(ctx); ok {
		return i, true
	}
	if ctx != nil {
		if resolve, ok := ctx.Value(resolverKey{}).(func(context.Context, string) Identity); ok {
			i := resolve(ctx, preset)
			return i, i.UserAgent != ""
		}
	}
	runtime.RLock()
	resolve := runtime.resolve
	runtime.RUnlock()
	if resolve == nil {
		return Identity{}, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	i := resolve(ctx, preset)
	return i, i.UserAgent != ""
}

func UserAgent(ctx context.Context, preset, fallback string) string {
	if i, ok := Default(ctx, preset); ok {
		return i.UserAgent
	}
	return fallback
}

// IsIdentityHeader covers client declarations the provider's own official
// client renders, never authentication, capability flags, request IDs or
// per-request session state. A host description qualifies when the provider
// itself declares it inside the client identity block rather than per request.
//
// The `X-Msh-*` set is such a block, not request state: the Kimi Code client
// renders the family token, its own client version and the host description as
// the companion declarations of its User-Agent on every first-party provider
// request. Listing them keeps the trusted snapshot authoritative — an inbound
// caller or a generic header override can never select one — while the runtime
// values themselves stay owned and overridable by the outbound identity
// settings rather than by the request. The `X-Mavis-*` session headers and the
// `X-Msh-Tool-Call-Id` request id are deliberately absent: those are
// per-request state.
func IsIdentityHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "user-agent", "originator", "version", "x-app", "x-goog-api-client",
		"x-grok-client-version", "x-grok-client-identifier", "x-grok-client-mode",
		"x-stainless-lang", "x-stainless-package-version", "x-stainless-os",
		"x-stainless-arch", "x-stainless-runtime", "x-stainless-runtime-version",
		"x-msh-platform", "x-msh-version", "x-msh-device-name",
		"x-msh-device-model", "x-msh-os-version", "x-msh-device-id",
		"x-zcode-app-version", "x-zcode-agent", "http-referer", "x-title",
		"x-release-channel", "x-client-language", "x-client-timezone",
		"x-platform", "x-os-category", "x-os-version", "x-device-mid",
		"x-client-bundle-id", "x-client-platform", "x-client-version", "x-client-locale", "x-client-timezone-offset":
		return true
	}
	return false
}

func (i Identity) Apply(headers http.Header) {
	if headers == nil || i.UserAgent == "" {
		return
	}
	for name := range headers {
		if IsIdentityHeader(name) {
			delete(headers, name)
		}
	}
	for name, value := range i.Headers {
		if IsIdentityHeader(name) {
			headers.Set(name, value)
		}
	}
	headers.Set("User-Agent", i.UserAgent)
}

func ApplyContext(req *http.Request) {
	if req == nil {
		return
	}
	if i, ok := FromContext(req.Context()); ok {
		i.ForProtocol(RequestProtocol(req)).Apply(req.Header)
	}
}

func ApplyDefault(req *http.Request, preset string) {
	if req == nil {
		return
	}
	if i, ok := Default(req.Context(), preset); ok {
		i.ForProtocol(RequestProtocol(req)).Apply(req.Header)
	}
}

// Headers adapts a trusted identity to clients accepting string header maps.
func Headers(ctx context.Context, preset string, fallback map[string]string) map[string]string {
	headers := http.Header{}
	for k, v := range fallback {
		headers.Set(k, v)
	}
	if i, ok := Default(ctx, preset); ok {
		i.Apply(headers)
	}
	result := make(map[string]string, len(headers))
	for k := range headers {
		result[k] = headers.Get(k)
	}
	return result
}
