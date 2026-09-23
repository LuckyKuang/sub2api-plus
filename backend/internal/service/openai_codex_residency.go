package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
)

// DefaultOpenAICodexResidency is the global default: do not send the header.
const DefaultOpenAICodexResidency = "off"

// NormalizeOpenAICodexResidency accepts the official managed values.
// Empty input is the default off. Anything else is rejected.
func NormalizeOpenAICodexResidency(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "off":
		return DefaultOpenAICodexResidency, nil
	case "us":
		return openai.CodexResidencyUS, nil
	default:
		return "", fmt.Errorf("must be \"off\" or \"us\"")
	}
}

// applyOpenAICodexResidencyFromSettings writes the global residency header on
// Codex-protocol surfaces. A missing service or a non-us value removes the
// header so inbound and override values cannot leak through.
func applyOpenAICodexResidencyFromSettings(ctx context.Context, settings *SettingService, headers http.Header, enabled bool) {
	if headers == nil || !enabled {
		return
	}
	value := ""
	if settings != nil && settings.OpenAICodexResidencyUS(ctx) {
		value = openai.CodexResidencyUS
	}
	openai.ApplyCodexResidencyHeader(headers, value)
}

// withManagedOpenAICodexResidency stamps the global residency onto ctx for
// refresh, revoke, and chatgpt.com backend-api calls.
func withManagedOpenAICodexResidency(ctx context.Context, settings *SettingService) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if settings != nil && settings.OpenAICodexResidencyUS(ctx) {
		return openai.WithCodexResidency(ctx, openai.CodexResidencyUS)
	}
	return ctx
}
