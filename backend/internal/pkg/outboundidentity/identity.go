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
	AccountID  int64             `json:"-"`
	Preset     string            `json:"preset"`
	UserAgent  string            `json:"user_agent"`
	Originator string            `json:"originator"`
	Version    string            `json:"version"`
	Source     string            `json:"source"`
	Headers    map[string]string `json:"headers"`
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
	return context.WithValue(ctx, contextKey{}, identity)
}

func FromContext(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	i, ok := ctx.Value(contextKey{}).(Identity)
	i.Headers = maps.Clone(i.Headers)
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

// IsIdentityHeader covers client declarations, never authentication, capability
// flags, request IDs or session/device state.
func IsIdentityHeader(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "user-agent", "originator", "version", "x-app", "x-goog-api-client",
		"x-grok-client-version", "x-grok-client-identifier",
		"x-stainless-lang", "x-stainless-package-version", "x-stainless-os",
		"x-stainless-arch", "x-stainless-runtime", "x-stainless-runtime-version":
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
		i.Apply(req.Header)
	}
}

func ApplyDefault(req *http.Request, preset string) {
	if req == nil {
		return
	}
	if i, ok := Default(req.Context(), preset); ok {
		i.Apply(req.Header)
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
