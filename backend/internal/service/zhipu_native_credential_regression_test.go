//go:build unit

package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type zhipuNativeCredentialCase struct {
	name, plan string
	legacyJWT  bool
}

var zhipuNativeCredentialCases = []zhipuNativeCredentialCase{
	{name: "personal-project", plan: ZhipuPlanIndividualCodingPlan},
	{name: "team-project", plan: ZhipuPlanTeamCodingPlan},
	{name: "start-plan-api-key", plan: ZhipuPlanStartPlan},
	{name: "off-peak-jwt", plan: ZhipuPlanOffPeak},
	{name: "off-peak-api-key-fallback", plan: ZhipuPlanOffPeak, legacyJWT: true},
}

func zhipuNativeCredentialAccount(provider string, scenario zhipuNativeCredentialCase) (*Account, string) {
	base := "https://open.bigmodel.cn/api/anthropic"
	if provider == "zai" {
		base = "https://api.z.ai/api/anthropic"
	}
	credentials := map[string]any{
		"oauth_provider": provider, "oauth_access_token": "login", "access_token": "business",
		"api_key": "forbidden-long-key", "plan_kind": scenario.plan, "account_mode": AccountModeCoding,
		"api_protocol":       APIProtocolAdaptive,
		"zhipu_organization": "org", "zhipu_project": "project", "zhipu_project_key_id": "location",
		"zhipu_project_key_name":   zcode.PersonalAPIKeyName,
		outboundIdentityCredential: OutboundIdentitySelection{Preset: "zcode", Version: "3.9.1"},
	}
	if scenario.plan == ZhipuPlanTeamCodingPlan {
		credentials["zhipu_project_key_name"] = zcode.TeamAPIKeyName
	}
	if scenario.plan == ZhipuPlanStartPlan || scenario.plan == ZhipuPlanOffPeak {
		credentials["api_key"] = "plan-jwt"
		credentials["zcode_jwt_token"] = "plan-jwt"
		credentials[anthropicAPIKeyAuthSchemeExtraKey] = AnthropicAPIKeyAuthSchemeAuthorizationBearer
		base = "https://zcode.z.ai/api/v1/zcode-plan/anthropic"
		if scenario.plan == ZhipuPlanOffPeak {
			base = "https://zcode.z.ai/api/v1/off-peak/anthropic"
			if !scenario.legacyJWT {
				credentials["api_key"] = "legacy-off-peak-key"
			}
		}
		if scenario.legacyJWT {
			delete(credentials, "zcode_jwt_token")
		}
	}
	credentials[credentialAPIBaseURLs] = map[string]any{APIProtocolAnthropic: base}
	return &Account{ID: 9421, Name: "linked-zcode", Platform: PlatformZhipu, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: credentials}, base + "/v1/messages"
}

func forwardZhipuNativeCredential(t *testing.T, svc *OpenAIGatewayService, account *Account, endpoint string) (*OpenAIForwardResult, error, *httptest.ResponseRecorder) {
	t.Helper()
	body := []byte(`{"model":"glm-5.3","stream":false,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`)
	if endpoint == "/v1/responses" {
		body = []byte(`{"model":"glm-5.3","stream":false,"input":"hello"}`)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", "untrusted-client/999")
	c.Request.Header.Set("X-ZCode-App-Version", "999")
	c.Request.Header.Set("Authorization", "Bearer untrusted-token")
	var result *OpenAIForwardResult
	var err error
	switch endpoint {
	case "/v1/messages":
		// A linked adaptive account reaches the native path through the public
		// Messages entrypoint. The other two calls exercise their actual native
		// adapters, including acquisition, final send and response conversion.
		result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	case "/v1/responses":
		result, err = svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, body, "")
	case "/v1/chat/completions":
		result, err = svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
	}
	return result, err, recorder
}

func installZhipuNativeAdmission(t *testing.T, linked *ZhipuProjectTokenManager) *zhipuOffPeakStub {
	t.Helper()
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "native-ticket", State: zcode.TicketReady}}}
	manager := NewZhipuOffPeakTicketManager(client)
	manager.projectTokens = linked
	previous := zhipuOffPeakProvider.Load()
	SetZhipuOffPeakTicketProvider(manager)
	t.Cleanup(func() {
		manager.Stop()
		if previous != nil {
			zhipuOffPeakProvider.Store(previous)
		} else {
			SetZhipuOffPeakTicketProvider((*ZhipuOffPeakTicketManager)(nil))
		}
	})
	return client
}

func TestZhipuNativeCredentialAcquisitionPreservesManagedPlans(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, scenario := range zhipuNativeCredentialCases {
			for _, endpoint := range []string{"/v1/messages", "/v1/responses", "/v1/chat/completions"} {
				t.Run(provider+"/"+scenario.name+endpoint, func(t *testing.T) {
					account, target := zhipuNativeCredentialAccount(provider, scenario)
					client := &projectTokenTestClient{}
					client.businessToken = "zai-business"
					linked := newProjectTokenTestService(t, client)
					upstream := &httpUpstreamRecorder{resp: nativeSSETestResponse(miniAnthropicSSEStream())}
					if endpoint == "/v1/messages" {
						upstream.resp = nativeAnthropicBufferedResponse()
					}
					repo := &credentialFenceRepo{applied: true}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo,
						httpUpstream: upstream, projectTokens: linked,
						openAITokenProvider: NewOpenAITokenProvider(nil, nil, nil)}
					var admission *zhipuOffPeakStub
					if scenario.plan == ZhipuPlanOffPeak {
						admission = installZhipuNativeAdmission(t, linked)
					}
					result, err, recorder := forwardZhipuNativeCredential(t, svc, account, endpoint)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, http.StatusOK, recorder.Code)
					require.NotEmpty(t, recorder.Body.String())
					require.Len(t, upstream.requests, 1)
					require.Zero(t, repo.calls, "valid managed credentials must never enter quarantine")
					require.Equal(t, StatusActive, account.Status)
					require.Equal(t, target, upstream.lastReq.URL.String())
					headers := upstream.lastReq.Header
					require.Equal(t, "ZCode/3.9.1 ai/6.0.193 ai-sdk/provider-utils/4.0.27 runtime/node.js/22", headers.Get("User-Agent"))
					require.Equal(t, "3.9.1", headers.Get("X-ZCode-App-Version"))
					require.Equal(t, "glm", headers.Get("X-ZCode-Agent"))
					require.Equal(t, "https://zcode.z.ai", headers.Get("HTTP-Referer"))
					require.Empty(t, headers.Get("Originator"))
					require.Empty(t, headers.Get("Version"))
					pat := "pat-login-1"
					if provider == "zai" {
						pat = "pat-zai-business-1"
					}
					if scenario.plan == ZhipuPlanStartPlan {
						require.Equal(t, "Bearer plan-jwt", headers.Get("Authorization"))
						require.Zero(t, client.calls, "start-plan JWT does not initiate a project-token exchange")
					} else {
						require.Equal(t, 1, client.calls)
						if scenario.plan == ZhipuPlanOffPeak {
							require.Equal(t, "Bearer plan-jwt", headers.Get("Authorization"))
							require.Equal(t, pat, headers.Get("X-Coding-Plan-Api-Key"))
							require.Equal(t, "native-ticket", headers.Get("X-Off-Peak-Ticket-ID"))
							require.Equal(t, zcode.OffPeakAuth{JWT: "plan-jwt", PlanKey: pat}, admission.lastAuth)
							require.Equal(t, 1, admission.takeCalls)
							require.NotEmpty(t, admission.identities)
							require.Equal(t, "ZCode/3.9.1", admission.identities[0].UserAgent)
							require.Equal(t, account.ID, admission.identities[0].AccountID)
						} else {
							require.Equal(t, "Bearer "+pat, headers.Get("Authorization"))
							require.Empty(t, headers.Get("X-Coding-Plan-Api-Key"))
							require.Empty(t, headers.Get("X-Off-Peak-Ticket-ID"))
						}
					}
					require.Empty(t, headers.Get("X-Api-Key"), "linked authentication must replace inbound and protocol placeholder keys")
					require.NotContains(t, headers.Get("Authorization"), "forbidden-long-key")
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

func TestZhipuNativeCredentialMissingMaterialAllowsTypedFailover(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, scenario := range zhipuNativeCredentialCases {
			for _, endpoint := range []string{"/v1/messages", "/v1/responses", "/v1/chat/completions"} {
				t.Run(provider+"/"+scenario.name+endpoint, func(t *testing.T) {
					account, _ := zhipuNativeCredentialAccount(provider, scenario)
					for _, name := range []string{"access_token", "oauth_access_token", "api_key", "zcode_jwt_token"} {
						account.Credentials[name] = " \t"
					}
					client := &projectTokenTestClient{}
					linked := newProjectTokenTestService(t, client)
					upstream := &httpUpstreamRecorder{}
					repo := &credentialFenceRepo{applied: true}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo,
						httpUpstream: upstream, projectTokens: linked,
						openAITokenProvider: NewOpenAITokenProvider(nil, nil, nil)}
					result, err, recorder := forwardZhipuNativeCredential(t, svc, account, endpoint)
					require.Nil(t, result)
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.True(t, failure.IsCredentialFailure())
					require.Equal(t, GatewayFailureStageAccountAuth, failure.Stage)
					require.Equal(t, GatewayFailureScopeAccount, failure.Scope)
					require.Equal(t, GatewayFailureReason("account_credential_missing"), failure.Reason)
					require.True(t, failure.ShouldRetryNextAccount())
					require.Equal(t, CredentialUnavailableClientMessage, failure.ClientMessage)
					require.Empty(t, upstream.requests, "missing local material must fail before opening the model transport")
					require.Zero(t, client.calls, "missing local material must not consume a project token")
					require.Empty(t, recorder.Body.String(), "credential recovery must remain possible before client output")
					require.Equal(t, 1, repo.calls)
					require.Equal(t, account.ID, repo.observed.ID)
					require.Equal(t, PlatformZhipu, repo.observed.Platform)
					require.Equal(t, AccountTypeOAuth, repo.observed.Type)
				})
			}
		}
	}
}
