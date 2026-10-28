//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The pinned official catalog includes Terra and the low/high/max efforts:
// https://github.com/openai/codex/blob/154e8736a51224e12b2d990753622a2cd8118f54/codex-rs/models-manager/models.json
// Effort suffixes and the Messages bridge are local compatibility features;
// they must preserve explicit client effort and the trusted identity contract.
func TestGPT61TerraMessagesForwardBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const globalUA = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"
	const accountUA = "codex_vscode/0.165.0 (Ubuntu 24.04; x86_64) vscode"
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, identity := range []struct {
			name, configuredUA, wantUA, wantOriginator string
		}{
			{name: "account before global", configuredUA: accountUA, wantUA: accountUA, wantOriginator: "codex_vscode"},
			{name: "invalid account falls through to global", configuredUA: "untrusted-client/9", wantUA: globalUA, wantOriginator: "codex_cli_rs"},
		} {
			for _, tc := range []struct {
				name, requested, preference, wantEffort string
			}{
				{name: "suffix max", requested: "openai/GPT-6.1-Terra-max", wantEffort: "max"},
				{name: "explicit effort overrides max suffix", requested: "openai/GPT-6.1-Terra-max", preference: `,"output_config":{"effort":"high"}`, wantEffort: "high"},
				{name: "saved mapping with explicit max", requested: "my-terra", preference: `,"output_config":{"effort":"max"}`, wantEffort: "max"},
			} {
				t.Run(accountType+"/"+identity.name+"/"+tc.name, func(t *testing.T) {
					body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false%s}`, tc.requested, tc.preference))
					c, rec := terraMessagesBoundaryContext(body)
					account := terraMessagesBoundaryAccount(accountType, tc.requested)
					account.Credentials["user_agent"] = identity.configuredUA
					account.Credentials["header_override_enabled"] = true
					account.Credentials["header_overrides"] = map[string]any{"User-Agent": "untrusted-client/9", "Originator": "untrusted", "Version": "9"}
					upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_terra_boundary", "gpt-6.1-terra")}
					svc := &OpenAIGatewayService{
						cfg: &config.Config{}, httpUpstream: upstream,
						settingService: &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
							SettingKeyOpenAICodexUserAgent:     globalUA,
							SettingKeyOpenAICodexClientVersion: "0.165.0",
						}}},
					}
					result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "gpt-6.1-terra", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, tc.wantEffort, gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
					require.True(t, gjson.GetBytes(upstream.lastBody, "input").IsArray())
					require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
					require.Equal(t, "gpt-6.1-terra", result.UpstreamModel)
					require.Equal(t, tc.requested, result.Model)
					require.NotNil(t, result.ReasoningEffort)
					require.Equal(t, tc.wantEffort, *result.ReasoningEffort)
					require.Equal(t, identity.wantUA, upstream.lastReq.Header.Get("User-Agent"))
					require.Equal(t, identity.wantOriginator, upstream.lastReq.Header.Get("Originator"))
					require.Equal(t, "0.165.0", upstream.lastReq.Header.Get("Version"))
					if accountType == AccountTypeOAuth {
						require.Equal(t, "https://chatgpt.com/backend-api/codex/responses", upstream.lastReq.URL.String())
						require.Equal(t, "Bearer oauth-token", upstream.lastReq.Header.Get("Authorization"))
					} else {
						require.Equal(t, "https://api.example.com/v1/responses", upstream.lastReq.URL.String())
						require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
					}
					require.Equal(t, http.StatusOK, rec.Code)
					require.Equal(t, "ok", gjson.Get(rec.Body.String(), "content.0.text").String())
				})
			}
		}
	}
}

// Disabled thinking is rejected by the existing local GPT-6.1 compatibility
// policy. This regression asserts the local boundary, not a universal provider
// API restriction inferred from the official model catalog.
func TestGPT61TerraMessagesDisabledThinkingStopsBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, requested := range []string{"openai/GPT-6.1-Terra-max", "my-terra"} {
			t.Run(accountType+"/"+requested, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"},"output_config":{"effort":"max"},"stream":false}`, requested))
				c, rec := terraMessagesBoundaryContext(body)
				account := terraMessagesBoundaryAccount(accountType, requested)
				before := cloneRequestCredentialMap(account.Credentials)
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_must_not_send", "gpt-6.1-terra")}
				result, err := (&OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}).ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				require.Error(t, err)
				require.Nil(t, result)
				require.Empty(t, upstream.requests)
				require.Nil(t, upstream.lastReq)
				require.Empty(t, upstream.lastBody)
				require.Equal(t, before, account.Credentials)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
				require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), `reasoning effort "none"`)
			})
		}
	}
}

func terraMessagesBoundaryAccount(accountType, requested string) *Account {
	if accountType == AccountTypeOAuth {
		return rawGPT56ResponsesOAuthAccount(requested, "gpt-6.1-terra")
	}
	return rawGPT56ResponsesAPIKeyAccount(requested, "gpt-6.1-terra")
}

func terraMessagesBoundaryContext(body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "untrusted-client/9")
	c.Request.Header.Set("Originator", "untrusted")
	c.Request.Header.Set("Version", "9")
	return c, rec
}
