//go:build unit

package service

import (
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

type zhipuPublicCredentialIngress struct {
	name, path, body string
}

var zhipuPublicCredentialIngresses = []zhipuPublicCredentialIngress{
	{"responses", "/v1/responses", `{"model":"glm-5.3","stream":false,"input":"hello"}`},
	{"chat", "/v1/chat/completions", `{"model":"glm-5.3","stream":false,"messages":[{"role":"user","content":"hello"}]}`},
	{"chat-responses-shape", "/v1/chat/completions", `{"model":"glm-5.3","stream":false,"input":"hello"}`},
}

func TestZhipuLinkedJWTProtocolRoutingIsScopedToAdaptiveOAuth(t *testing.T) {
	for _, plan := range []string{ZhipuPlanStartPlan, ZhipuPlanOffPeak, ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan, ""} {
		for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey, AccountTypeUpstream} {
			for _, protocol := range []string{APIProtocolAdaptive, APIProtocolChatCompletions, APIProtocolAnthropic} {
				// Managed links are created as adaptive. Explicit protocol knobs
				// are API-key controls; unsupported non-API-key combinations remain
				// covered by the unchanged historical routing matrix.
				if protocol != APIProtocolAdaptive && accountType != AccountTypeAPIKey {
					continue
				}
				for _, inbound := range []string{APIProtocolResponses, APIProtocolChatCompletions, APIProtocolAnthropic} {
					t.Run(plan+"/"+accountType+"/"+protocol+"/"+inbound, func(t *testing.T) {
						account := &Account{Platform: PlatformZhipu, Type: accountType, Credentials: map[string]any{
							"plan_kind": plan, "api_protocol": protocol, "oauth_provider": "bigmodel",
							credentialAPIBaseURLs: map[string]any{APIProtocolAnthropic: "https://zcode.z.ai/api/v1/zcode-plan/anthropic"},
						}}
						want := protocol
						if protocol == APIProtocolAdaptive {
							want = APIProtocolChatCompletions
							if inbound == APIProtocolAnthropic || (accountType == AccountTypeOAuth && (plan == ZhipuPlanStartPlan || plan == ZhipuPlanOffPeak)) {
								want = APIProtocolAnthropic
							}
						}
						require.Equal(t, want, resolveUpstreamProtocol(account, inbound, "", nil))
					})
				}
			}
		}
	}
}

func forwardZhipuPublicCredential(t *testing.T, svc *OpenAIGatewayService, account *Account, ingress zhipuPublicCredentialIngress) (*OpenAIForwardResult, error, *httptest.ResponseRecorder) {
	t.Helper()
	body := []byte(ingress.body)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, ingress.path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "untrusted-client/999")
	c.Request.Header.Set("X-ZCode-App-Version", "999")
	c.Request.Header.Set("Authorization", "Bearer untrusted-token")
	if ingress.path == "/v1/responses" {
		result, err := svc.Forward(context.Background(), c, account, body)
		return result, err, recorder
	}
	result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	return result, err, recorder
}

func TestZhipuPublicCredentialManagedPlansKeepProtocolAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, scenario := range zhipuNativeCredentialCases {
			for _, ingress := range zhipuPublicCredentialIngresses {
				t.Run(provider+"/"+scenario.name+"/"+ingress.name, func(t *testing.T) {
					account, target := zhipuNativeCredentialAccount(provider, scenario)
					client := &projectTokenTestClient{}
					client.businessToken = "zai-business"
					linked := newProjectTokenTestService(t, client)
					upstream := &httpUpstreamRecorder{resp: nativeSSETestResponse(miniAnthropicSSEStream())}
					wantUA := "ZCode/3.9.1 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22"
					if scenario.plan == ZhipuPlanIndividualCodingPlan || scenario.plan == ZhipuPlanTeamCodingPlan {
						// Real linked personal/team material declares both endpoints.
						// Public Responses falls back to Chat; native Chat stays Chat.
						base := "https://open.bigmodel.cn/api/coding/paas/v4"
						if provider == "zai" {
							base = "https://api.z.ai/api/coding/paas/v4"
						}
						account.Credentials[credentialAPIBaseURLs].(map[string]any)[APIProtocolChatCompletions] = base
						target = base + "/chat/completions"
						wantUA = "ZCode/3.9.1 ai/6.0.193 ai-sdk/provider-utils/4.0.39 runtime/node.js/22"
						upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))}
					}
					repo := &credentialFenceRepo{applied: true}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo,
						httpUpstream: upstream, projectTokens: linked,
						openAITokenProvider: NewOpenAITokenProvider(nil, nil, nil)}
					if scenario.plan == ZhipuPlanOffPeak {
						installZhipuNativeAdmission(t, linked)
					}
					result, err, recorder := forwardZhipuPublicCredential(t, svc, account, ingress)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, http.StatusOK, recorder.Code)
					require.NotEmpty(t, recorder.Body.String())
					require.Len(t, upstream.requests, 1)
					require.Zero(t, repo.calls)
					require.Equal(t, target, upstream.lastReq.URL.String())
					headers := upstream.lastReq.Header
					require.Equal(t, wantUA, headers.Get("User-Agent"))
					require.Equal(t, "3.9.1", headers.Get("X-ZCode-App-Version"))
					require.Equal(t, "glm", headers.Get("X-ZCode-Agent"))
					require.Equal(t, "https://zcode.z.ai", headers.Get("HTTP-Referer"))
					require.Empty(t, headers.Get("Originator"))
					require.Empty(t, headers.Get("Version"))
					require.Empty(t, headers.Get("X-Api-Key"))
					pat := "pat-login-1"
					if provider == "zai" {
						pat = "pat-zai-business-1"
					}
					switch scenario.plan {
					case ZhipuPlanStartPlan:
						require.Equal(t, "Bearer plan-jwt", headers.Get("Authorization"))
						require.Zero(t, client.calls)
					case ZhipuPlanOffPeak:
						require.Equal(t, "Bearer plan-jwt", headers.Get("Authorization"))
						require.Equal(t, pat, headers.Get("X-Coding-Plan-Api-Key"))
						require.Equal(t, "native-ticket", headers.Get("X-Off-Peak-Ticket-ID"))
						require.Equal(t, 1, client.calls)
					default:
						require.Equal(t, "Bearer "+pat, headers.Get("Authorization"))
						require.Empty(t, headers.Get("X-Coding-Plan-Api-Key"))
						require.Empty(t, headers.Get("X-Off-Peak-Ticket-ID"))
						require.Equal(t, 1, client.calls)
					}
					if scenario.plan == ZhipuPlanTeamCodingPlan {
						require.Equal(t, "org", headers.Get("bigmodel-organization"))
						require.Equal(t, "project", headers.Get("bigmodel-project"))
					} else {
						require.Empty(t, headers.Get("bigmodel-organization"))
						require.Empty(t, headers.Get("bigmodel-project"))
					}
				})
			}
		}
	}
}

func TestZhipuPublicCredentialMissingGrantAllowsTypedFailoverBeforeSideEffects(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, scenario := range zhipuNativeCredentialCases {
			for _, ingress := range zhipuPublicCredentialIngresses {
				t.Run(provider+"/"+scenario.name+"/"+ingress.name, func(t *testing.T) {
					account, _ := zhipuNativeCredentialAccount(provider, scenario)
					for _, key := range []string{"access_token", "oauth_access_token", "api_key", "zcode_jwt_token"} {
						account.Credentials[key] = " \t"
					}
					client := &projectTokenTestClient{}
					linked := newProjectTokenTestService(t, client)
					upstream := &httpUpstreamRecorder{}
					repo := &credentialFenceRepo{applied: true}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo,
						httpUpstream: upstream, projectTokens: linked,
						openAITokenProvider: NewOpenAITokenProvider(nil, nil, nil)}
					result, err, recorder := forwardZhipuPublicCredential(t, svc, account, ingress)
					require.Nil(t, result)
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, GatewayFailureStageAccountAuth, failure.Stage)
					require.Equal(t, GatewayFailureScopeAccount, failure.Scope)
					require.Equal(t, GatewayFailureReason("account_credential_missing"), failure.Reason)
					require.True(t, failure.ShouldRetryNextAccount())
					require.Equal(t, CredentialUnavailableClientMessage, failure.ClientMessage)
					require.Empty(t, recorder.Body.String())
					require.Empty(t, upstream.requests)
					require.Zero(t, client.calls)
					require.Equal(t, 1, repo.calls)
					require.Equal(t, account.ID, repo.observed.ID)
					require.Equal(t, PlatformZhipu, repo.observed.Platform)
					require.Equal(t, AccountTypeOAuth, repo.observed.Type)
				})
			}
		}
	}
}
