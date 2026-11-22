//go:build unit

package handler

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUpstream024CodexExternalContextAuditsBeforeSideEffects(t *testing.T) {
	for _, name := range []string{"untrusted_content", "ide_context", "proposed_plan"} {
		t.Run(name, func(t *testing.T) {
			body := pageContextRequest(t, pageContextItem(name, "audit external context"))
			testCodexCallLessContextRealAuditsBeforeSideEffects(t, body, "<external_"+name+">audit external context</external_"+name+">")
		})
	}
}

func TestUpstream024KimiDynamicToolsKeepCurrentUserAuditable(t *testing.T) {
	// The protocol adapter consumes tool-only Kimi system messages after
	// audit. Their declarations stay in context and cannot hide user text.
	const body = `{"model":"kimi-for-coding","messages":[{"role":"user","content":"old user text"},{"role":"assistant","content":"old response"},{"role":"user","content":"audit current question"},{"role":"system","tools":[{"type":"function","function":{"name":"lookup","description":"local tool description","parameters":{"type":"object"}}}]}],"future_options":{"opaque":true}}`
	for _, kind := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			t.Run(kind+"/"+engine, func(t *testing.T) {
				coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 1)
				account := service.Account{ID: 101, Platform: service.PlatformKimi, Type: kind, Status: service.StatusActive, Schedulable: true, Concurrency: 1}
				h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, false)
				c, w := codexCLIIngressContext(body, true)
				key, ok := middleware.GetAPIKeyFromContext(c)
				require.True(t, ok)
				key.Group.Platform = service.PlatformKimi
				c.Request.URL.Path = "/v1/chat/completions"
				h.ChatCompletions(c)

				require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
				if engine == "prompt" {
					require.Equal(t, "permission_error", gjson.Get(w.Body.String(), "error.type").String())
					require.Equal(t, securityaudit.ErrorCodeBlocked, gjson.Get(w.Body.String(), "error.code").String())
				} else {
					require.Equal(t, "content_policy_violation", gjson.Get(w.Body.String(), "error.type").String())
				}
				scans := supplier.snapshot()
				require.Len(t, scans, 1)
				require.Contains(t, scans[0], "audit current question")
				require.NotContains(t, scans[0], "old user text")
				require.NotContains(t, scans[0], "local tool description")
				assertCodexCLIIngressNoSideEffects(t, boundary)
			})
		}
	}
}
