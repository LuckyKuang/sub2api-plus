package openai

import (
	"context"
	"net/http"
	"strings"
)

// CodexResidencyHeader is the official managed-residency request header.
// Official Codex sends it from process configuration (enforce_residency = us)
// on the default client and API provider, and never on the raw auth client.
const CodexResidencyHeader = "x-openai-internal-codex-residency"

// CodexResidencyUS is the only residency value official Codex emits.
const CodexResidencyUS = "us"

type codexResidencyCtxKey struct{}

// WithCodexResidency stamps a managed residency onto ctx for credential-plane
// and backend-api callers that do not share the gateway header finalizer.
// Any value other than "us" leaves the context unchanged.
func WithCodexResidency(ctx context.Context, residency string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(residency) != CodexResidencyUS {
		return ctx
	}
	return context.WithValue(ctx, codexResidencyCtxKey{}, CodexResidencyUS)
}

// CodexResidencyFromContext returns "us" when the context carries managed
// residency, otherwise an empty string.
func CodexResidencyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(codexResidencyCtxKey{}).(string)
	if value == CodexResidencyUS {
		return CodexResidencyUS
	}
	return ""
}

// ApplyCodexResidencyHeader writes the managed residency header, or removes it
// when residency is off. Callers use this so an inbound or stored value cannot
// survive a disabled setting.
func ApplyCodexResidencyHeader(headers http.Header, residency string) {
	if headers == nil {
		return
	}
	if strings.TrimSpace(residency) == CodexResidencyUS {
		headers.Set(CodexResidencyHeader, CodexResidencyUS)
		return
	}
	headers.Del(CodexResidencyHeader)
}
