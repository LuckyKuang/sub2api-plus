//go:build unit

package handler

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The transparent bridge must retain the ordinary canonical Responses audit:
// recognized user siblings remain auditable beside unknown future items, and
// rejection precedes selection, billing, slots, files and any CLI/provider send.
func TestCodexCLIBridgeOrdinaryResponsesBothRealAuditsPrecedeSideEffects(t *testing.T) {
	for _, engine := range []string{"moderation", "prompt"} {
		t.Run(engine, func(t *testing.T) {
			coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 1)
			account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture", service.CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": service.DefaultCodexCLIConfigTOML}}}
			h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, false)
			body := `{"model":"gpt-5.1","instructions":"developer context","input":[{"type":"future_item","opaque":{"preserve":true}},{"role":"user","content":[{"type":"input_text","text":"audit original input"}]}],"tools":[{"type":"function","name":"caller_tool","parameters":{"type":"object"}}],"future_sibling":{"preserve":true}}`
			c, w := codexCLIIngressContext(body, true)
			c.Request.URL.Path = "/v1/responses"
			h.Responses(c)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			scans := supplier.snapshot()
			require.Len(t, scans, 1)
			require.Contains(t, scans[0], "audit original input")
			require.False(t, service.IsCodexCLITask(c.Request.Context()))
			assertCodexCLIIngressNoSideEffects(t, boundary)
		})
	}
}

func TestCodexCLIBridgeOrdinaryResponsesBothEnginesReceiveOriginalEnvelope(t *testing.T) {
	audits := &codexCLIIngressAudits{blockPrompt: true}
	h, boundary := newCodexCLIIngressHandler(t, securityaudit.NewCoordinator(audits, audits), nil, false)
	body := `{"model":"gpt-5.1","input":[{"role":"user","content":"original"},{"type":"unknown","payload":"opaque"}],"tools":[{"type":"function","name":"caller_tool","parameters":{}}],"future_sibling":true}`
	c, w := codexCLIIngressContext(body, true)
	c.Request.URL.Path = "/v1/responses"
	h.Responses(c)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Len(t, audits.legacy, 1)
	require.Len(t, audits.prompt, 1)
	require.Equal(t, body, string(audits.legacy[0].Body))
	require.Equal(t, body, string(audits.prompt[0].Body))
	require.Equal(t, []bool{false}, audits.legacyTask)
	require.Equal(t, []bool{false}, audits.promptTask)
	assertCodexCLIIngressNoSideEffects(t, boundary)
}
