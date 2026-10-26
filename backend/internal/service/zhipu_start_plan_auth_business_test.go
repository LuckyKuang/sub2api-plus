//go:build unit

package service

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Official ZCode accountProviderRequestAuthService.ts resolves start-plan's
// zcodeJwtToken; the native Anthropic provider sends it as Bearer against
// zcode.z.ai/api/v1/zcode-plan/anthropic. BuildAccountMaterial must produce an
// immediately usable linked account, without a second manual API-key setting.
func TestZhipuStartPlanMaterialForwardsOfficialBearerWithoutManualScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, legacy := range []bool{false, true} {
			name := provider + "/fresh"
			if legacy {
				name = provider + "/legacy-scheme"
			}
			t.Run(name, func(t *testing.T) {
				oauth := newZhipuOAuthTestService(t, &zhipuOAuthStubClient{})
				material, err := oauth.BuildAccountMaterial(context.Background(), ZhipuAccountMaterialInput{
					Provider: provider, PlanKind: ZhipuPlanStartPlan, ZCodeJWT: "business-plan-jwt",
				})
				require.NoError(t, err)
				if legacy {
					// Previously issued rows may contain a declaration that never
					// selected the OAuth scheme. It must not be required or read.
					material.Credentials[anthropicAPIKeyAuthSchemeExtraKey] = "x_api_key"
				}
				account := &Account{ID: 971, Platform: PlatformZhipu, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials: material.Credentials, Concurrency: 1,
					Extra: map[string]any{anthropicAPIKeyAuthSchemeExtraKey: "x_api_key"}}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"application/json"}},
					Body:   io.NopCloser(strings.NewReader(`{"id":"msg_business","type":"message","role":"assistant","model":"glm-5.2","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`))}}
				gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				body := []byte(`{"model":"glm-5.2","max_tokens":32,"messages":[{"role":"user","content":"lookup status"}]}`)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
				c.Request.Header.Set("Authorization", "Bearer inbound-private")
				c.Request.Header.Set("X-Api-Key", "inbound-private-key")
				result, err := gateway.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				var serialized bytes.Buffer
				require.NoError(t, upstream.lastReq.Write(&serialized))
				wire, err := http.ReadRequest(bufio.NewReader(&serialized))
				require.NoError(t, err)
				t.Cleanup(func() { _ = wire.Body.Close() })
				require.Equal(t, "https://zcode.z.ai/api/v1/zcode-plan/anthropic/v1/messages", upstream.lastReq.URL.String())
				require.Equal(t, "Bearer business-plan-jwt", wire.Header.Get("Authorization"))
				require.Empty(t, wire.Header.Get("X-Api-Key"))
				require.Empty(t, wire.Header.Get("X-Dsh-Auth-Token"))
				require.Empty(t, wire.Header.Get("X-Coding-Plan-Api-Key"), "start-plan has no project PAT")
				// Retain the target branch's reviewed ZCode identity baseline.
				require.Equal(t, "ZCode/3.16.2 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22", wire.Header.Get("User-Agent"))
			})
		}
	}
}
