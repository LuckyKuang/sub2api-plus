//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// docs/SECURITY_AUDIT_CONTENT_COVERAGE.md requires valid unknown input to pass
// both engines even for opted-in CLI accounts. The deliberately unavailable
// bridge is a later transport failure, never an audit-derived refusal.
func TestCodexCLIBridgeUnknownResponsesPassBothAuditsBeforeTransportAdmission(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			t.Run(accountType+"/"+engine, func(t *testing.T) {
				coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 0)
				credentials := map[string]any{service.CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": service.DefaultCodexCLIConfigTOML}}
				if accountType == service.AccountTypeAPIKey {
					credentials["api_key"] = "fixture-api-key"
				} else {
					credentials["access_token"] = "fixture-access-token"
					credentials["chatgpt_account_id"] = "fixture-workspace"
				}
				account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: accountType, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: credentials}
				h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, true)
				c, w := codexCLIIngressContext(`{"model":"gpt-5.1","input":[{"type":"future_item","opaque":{"preserve":true}}],"future_sibling":{"preserve":true}}`, true)
				c.Request.URL.Path = "/v1/responses"
				ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				boundary.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) {
					completed, _ := c.Get(securityAuditCompletedContextKey)
					require.Equal(t, true, completed, "audit must permit the unknown envelope before concurrency admission")
					boundary.userSlots.Add(1)
					return true, nil
				}
				h.Responses(c)
				require.Empty(t, supplier.snapshot(), "unextractable content cannot become an audit supplier request")
				require.Positive(t, boundary.repositoryReads.Load())
				require.Equal(t, int32(1), boundary.userSlots.Load())
				require.Equal(t, int32(1), boundary.accountSlots.Load())
				require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "cli_bridge_unavailable")
				require.Empty(t, boundary.wire.calls(), "unavailable CLI permission must never fall back to native transport")
			})
		}
	}
}

// The transparent bridge must retain the ordinary canonical Responses audit:
// recognized user siblings remain auditable beside unknown future items, and
// rejection precedes selection, billing, slots, files and any CLI/provider send.
func TestCodexCLIBridgeOrdinaryResponsesBothRealAuditsPrecedeSideEffects(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			t.Run(accountType+"/"+engine, func(t *testing.T) {
				coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 1)
				account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: accountType, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture", "api_key": "fixture-api-key", service.CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": service.DefaultCodexCLIConfigTOML}}}
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
