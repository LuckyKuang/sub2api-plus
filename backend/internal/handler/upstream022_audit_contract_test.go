//go:build unit

package handler

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstream022UntrustedContextCanonicalAudits(t *testing.T) {
	for _, name := range []string{"browser_info", "goal_objective", "untrusted_context"} {
		for _, frame := range []string{"http", "websocket", "websocket_nested"} {
			for _, callID := range []string{"", "call_real"} {
				t.Run(name+"/"+frame+"/"+callID, func(t *testing.T) {
					item := pageContextItem(name, "audit external context")
					item["call_id"] = callID
					body := pageContextRequest(t, item)
					var envelope map[string]any
					require.NoError(t, json.Unmarshal(body, &envelope))
					envelope["previous_response_id"] = "resp_history"
					envelope["future_options"] = map[string]any{"opaque": true}
					if frame == "websocket" {
						envelope["type"] = "response.create"
					}
					if frame == "websocket_nested" {
						envelope = map[string]any{"type": "response.create", "response": envelope}
					}
					body, err := json.Marshal(envelope)
					require.NoError(t, err)
					input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, body)
					snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIResponses, Body: body})
					if callID != "" {
						require.Empty(t, input.Text)
						require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
						require.Empty(t, snapshot.ScanText)
						return
					}
					expected := "<external_" + name + ">audit external context</external_" + name + ">"
					require.NoError(t, err)
					require.Equal(t, expected, input.Text)
					require.Equal(t, expected, snapshot.ScanText)
				})
			}
		}
	}
}

func TestUpstream022UntrustedContextRealAuditsBeforeSideEffects(t *testing.T) {
	for _, name := range []string{"browser_info", "goal_objective", "untrusted_context"} {
		t.Run(name, func(t *testing.T) {
			body := pageContextRequest(t, pageContextItem(name, "audit external context"))
			testCodexCallLessContextRealAuditsBeforeSideEffects(t, body, "audit external context")
		})
	}
}
