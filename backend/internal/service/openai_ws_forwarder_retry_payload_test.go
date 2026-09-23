//go:build unit || !integration

package service

import (
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Codex-protocol declarations are part of the request contract: a WebSocket
// retry must replay the same payload, so `include` (the always-declared
// encrypted-reasoning contract), `prompt_cache_key` (the session identity), and
// every client declaration survive any number of attempts.
func TestOpenAIWSRetryPayloadKeepsCodexContract(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	reqBody := map[string]any{
		"type":                "response.create",
		"model":               "gpt-5.3-codex",
		"instructions":        "test instructions",
		"input":               []any{map[string]any{"role": "user", "type": "message", "content": "hello"}},
		"tools":               []any{map[string]any{"type": "function", "name": "exec"}},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"include":             []any{"reasoning.encrypted_content"},
		"prompt_cache_key":    "ws-cache-key",
		"store":               false,
	}

	first := svc.buildOpenAIWSCreatePayload(reqBody, account)
	require.NotEmpty(t, first, "payload must be built")
	require.Contains(t, first, "include", "the built payload must carry the Codex contract")

	for attempt := 2; attempt <= 6; attempt++ {
		retried := svc.buildOpenAIWSCreatePayload(reqBody, account)
		require.Equal(t, first, retried,
			"attempt %d must replay the identical Codex payload", attempt)
		require.Contains(t, retried, "include",
			"the encrypted-reasoning contract must survive attempt %d", attempt)
		require.Contains(t, retried, "prompt_cache_key",
			"the session identity must survive attempt %d", attempt)
		require.Contains(t, retried, "tool_choice")
		require.Contains(t, retried, "parallel_tool_calls")
	}
}
