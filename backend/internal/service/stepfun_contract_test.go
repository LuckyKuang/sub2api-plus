//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func stepFunTestAccount(kind, region string) *Account {
	a := &Account{ID: 42, Platform: "stepfun", Type: kind, Credentials: map[string]any{"api_key": "test-key", "region": region, "api_protocol": "chat_completions", "account_mode": "payg"}}
	if kind == "oauth" {
		a.Credentials = map[string]any{"access_token": "test-key", "oauth_provider": "stepfun", "oauth_region": region, "account_mode": "coding", "api_protocol": "chat_completions"}
	}
	return a
}

// Literal oracle from Step-Code's live model adapter, not its unused versioned
// CLI helper. OpenAI JS 6.40.0 source and Ubuntu 24.04 are independent inputs.
func requireStepFunWire(t *testing.T, req *http.Request, inference bool) {
	t.Helper()
	want := map[string]string{"User-Agent": "step (linux 6.8.0-31-generic; x64)"}
	if inference {
		for k, v := range map[string]string{"X-Step-Client": "stepcode", "X-Stainless-Lang": "js", "X-Stainless-Package-Version": "6.40.0", "X-Stainless-OS": "Linux", "X-Stainless-Arch": "x64", "X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.19.0"} {
			want[k] = v
		}
	}
	normalized := map[string]string{}
	for k, v := range want {
		normalized[http.CanonicalHeaderKey(k)] = v
	}
	want = normalized
	for k, v := range want {
		require.Equal(t, v, req.Header.Get(k), k)
	}
	for k, values := range req.Header {
		if outboundidentity.IsIdentityHeader(k) {
			_, ok := want[http.CanonicalHeaderKey(k)]
			require.True(t, ok, "unexpected identity header %s", k)
		}
		require.NotContains(t, strings.ToLower(k+strings.Join(values, " ")), "sub2api")
	}
}

func TestStepFunOAuthAndAPIKeyActualForwardAndProbe(t *testing.T) {
	for _, scenario := range []struct{ kind, mode string }{{"oauth", "coding"}, {"apikey", "payg"}, {"apikey", "coding"}} {
		kind := scenario.kind
		for _, site := range []struct{ region, host string }{{"cn", "api.stepfun.com"}, {"global", "api.stepfun.ai"}} {
			for _, path := range []string{"chat", "responses", "messages", "probe", "models"} {
				t.Run(kind+"/"+scenario.mode+"/"+site.region+"/"+path, func(t *testing.T) {
					a := stepFunTestAccount(kind, site.region)
					a.Credentials["account_mode"] = scenario.mode
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat-1","object":"chat.completion","model":"step-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))}}
					if path == "probe" {
						upstream.resp.Header.Set("Content-Type", "text/event-stream")
						upstream.resp.Body = io.NopCloser(strings.NewReader("data: {\"id\":\"chat-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
					}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					expectedBase := "https://" + site.host + "/v1"
					if scenario.mode == "coding" {
						expectedBase = "https://" + site.host + "/step_plan/v1"
					}
					require.Equal(t, expectedBase, a.GetOpenAIBaseURL())
					require.Equal(t, "chat_completions", a.GetAPIProtocol())
					require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(a))
					if path == "models" {
						tester := &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
						req, err := tester.buildUpstreamModelsRequest(ctx, a)
						require.NoError(t, err)
						_, err = tester.doUpstreamModelsRequest(req, "", a)
						require.NoError(t, err)
						require.Equal(t, expectedBase+"/models", upstream.lastReq.URL.String())
					} else {
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
						c.Request.Header.Set("User-Agent", "sub2api-inbound")
						c.Request.Header.Set("X-Step-Client", "attacker")
						var err error
						switch path {
						case "probe":
							tester := &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream, cnOAuthService: svc.cnOAuthService}
							err = tester.testCNProviderChatCompletionsConnection(c, a, "step-3.7-flash", "hello")
						case "responses":
							_, err = svc.Forward(ctx, c, a, []byte(`{"model":"step-3.7-flash","input":"hello","stream":false}`))
						case "messages":
							_, err = svc.ForwardAsAnthropic(ctx, c, a, []byte(`{"model":"step-3.7-flash","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`), "", "")
						default:
							_, err = svc.ForwardAsChatCompletions(ctx, c, a, []byte(`{"model":"step-3.7-flash","messages":[{"role":"user","content":"hello"}],"stream":false}`), "", "")
						}
						require.NoError(t, err)
						require.NotNil(t, upstream.lastReq)
						require.Equal(t, expectedBase+"/chat/completions", upstream.lastReq.URL.String())
					}
					require.Equal(t, "Bearer test-key", upstream.lastReq.Header.Get("Authorization"))
					requireStepFunWire(t, upstream.lastReq, path != "models")
				})
			}
		}
	}
}

func TestStepFunLoginOwnerCancellationAndSingleConsumption(t *testing.T) {
	admin := &cnOAuthAdminStub{}
	svc := NewCNOAuthService(nil, nil, admin)
	view, err := svc.Start(context.Background(), 7, "stepfun", "global", nil, 0)
	require.NoError(t, err)
	auth, _ := url.Parse(view.AuthorizeURL)
	callback := "http://127.0.0.1:53683/callback?state=" + auth.Query().Get("state") + "&api_key=private-step-key"
	_, err = svc.Advance(context.Background(), 8, "stepfun", view.SessionID, callback, false)
	require.Error(t, err)
	_, err = svc.Advance(context.Background(), 7, "kimi", view.SessionID, callback, false)
	require.Error(t, err)
	ready, err := svc.Advance(context.Background(), 7, "stepfun", view.SessionID, callback, false)
	require.NoError(t, err)
	require.Equal(t, "ready", ready.Status)
	for n := 0; n < 2; n++ {
		_, err = svc.Complete(context.Background(), 7, "stepfun", view.SessionID, CNOAuthCompleteInput{Name: "Step"})
		require.NoError(t, err)
	}
	require.Equal(t, 1, admin.writes)
	require.Equal(t, "oauth", admin.input.Type)
	require.Equal(t, "https://api.stepfun.ai/step_plan/v1", admin.input.Credentials["base_url"])
	require.Equal(t, "chat_completions", admin.input.Credentials["api_protocol"])
	require.NotContains(t, admin.input.Credentials, "refresh_token")
	require.NotContains(t, admin.input.Credentials, "expires_at")
	serialized, err := json.Marshal(ready)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), "private-step-key")
	view, err = svc.Start(context.Background(), 7, "stepfun", "cn", nil, 0)
	require.NoError(t, err)
	_, err = svc.Advance(context.Background(), 7, "stepfun", view.SessionID, "", true)
	require.NoError(t, err)
	_, err = svc.Advance(context.Background(), 7, "stepfun", view.SessionID, callback, false)
	require.NoError(t, err)
	_, err = svc.Complete(context.Background(), 7, "stepfun", view.SessionID, CNOAuthCompleteInput{})
	require.Error(t, err)
	require.Equal(t, 1, admin.writes)
}

func TestStepFunGrantCannotEscapeRegionOrInventRefresh(t *testing.T) {
	a := stepFunTestAccount("oauth", "cn")
	a.Credentials["base_url"] = "https://evil.test"
	for _, target := range []string{"https://api.stepfun.ai/step_plan/v1/chat/completions", "https://api.stepfun.com/v1/chat/completions", "https://api.stepfun.com/step_plan/v1/messages", "https://api.stepfun.com/step_plan/v1/responses", "https://evil.test/step_plan/v1/chat/completions"} {
		req, _ := http.NewRequest(http.MethodPost, target, nil)
		require.Error(t, prepareStepFunOAuthRequest(req, a))
	}
	req, _ := http.NewRequest(http.MethodPost, "https://api.stepfun.com/step_plan/v1/chat/completions", nil)
	a.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
	require.Error(t, prepareStepFunOAuthRequest(req, a))
	require.False(t, NewCNTokenRefresher("stepfun").CanRefresh(a))
}

func TestStepFunModelCatalogOnlyAdvertisesDiscoveredChatCapabilities(t *testing.T) {
	body := []byte(`{"data":[{"id":"step-3.7-flash","model_type":"大语言模型","max_input_tokens":256000,"enable_reason":true,"enable_vision_input":true,"reasoning_effort_support_list":["low","high"]},{"id":"step-router","model_type":"路由模型"},{"id":"step-image","model_type":"文生图"},{"id":"untyped"}]}`)
	ids, err := extractStepFunModelIDs(body)
	require.NoError(t, err)
	require.Equal(t, []string{"step-3.7-flash", "step-router"}, ids)
	meta := extractStepFunMetadata(body)
	require.EqualValues(t, 256000, meta["step-3.7-flash"].ContextWindow)
	require.Equal(t, []string{"text", "image"}, meta["step-3.7-flash"].InputModalities)
	require.Equal(t, []string{"low", "high"}, meta["step-3.7-flash"].SupportedReasoningLevels)
	require.Zero(t, meta["step-router"].ContextWindow)
	require.Nil(t, meta["step-router"].Reasoning)
	ids, err = extractStepFunModelIDs([]byte(`{"data":[{"id":"future-model"},{"id":"future-model"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"future-model"}, ids)
}

func TestStepFunStreamingToolsAndBillingAcrossInboundProtocols(t *testing.T) {
	chunks := strings.Join([]string{
		`data: {"id":"chat-1","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"weather","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chat-1","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chat-1","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chat-1","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chat-1","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[],"usage":{"prompt_tokens":6,"completion_tokens":5,"total_tokens":11}}`,
		`data: [DONE]`, "",
	}, "\n\n")
	for _, kind := range []string{"oauth", "apikey"} {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			t.Run(kind+"/"+protocol, func(t *testing.T) {
				a := stepFunTestAccount(kind, "global")
				_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(chunks))}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, nil).WithContext(ctx)
				var result *OpenAIForwardResult
				var err error
				switch protocol {
				case "responses":
					result, err = svc.Forward(ctx, c, a, []byte(`{"model":"step-3.7-flash","input":"weather?","tools":[{"type":"function","name":"weather","parameters":{"type":"object"}}],"stream":true}`))
				case "messages":
					result, err = svc.ForwardAsAnthropic(ctx, c, a, []byte(`{"model":"step-3.7-flash","max_tokens":32,"messages":[{"role":"user","content":"weather?"}],"tools":[{"name":"weather","input_schema":{"type":"object"}}],"stream":true}`), "", "")
				default:
					result, err = svc.ForwardAsChatCompletions(ctx, c, a, []byte(`{"model":"step-3.7-flash","messages":[{"role":"user","content":"weather?"}],"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"}}}],"stream":true}`), "", "")
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 6, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				require.True(t, result.Stream)
				requireStepFunWire(t, upstream.lastReq, true)
				require.True(t, strings.HasSuffix(upstream.lastReq.URL.Path, "/chat/completions"))
				require.Equal(t, "weather", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
				require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
				out := recorder.Body.String()
				require.Contains(t, out, "weather")
				require.Contains(t, out, "Paris")
				switch protocol {
				case "responses":
					require.Contains(t, out, "response.completed")
					require.Contains(t, out, "function_call")
				case "messages":
					require.Contains(t, out, `"stop_reason":"tool_use"`)
					require.Contains(t, out, "event: message_stop")
				default:
					require.Contains(t, out, `"finish_reason":"tool_calls"`)
					require.Contains(t, out, "[DONE]")
				}
			})
		}
	}
}

func TestStepFunLoginAcrossReplicasConsumesGrantOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	admin := &cnOAuthAdminStub{}
	first := NewCNOAuthService(nil, nil, admin)
	second := NewCNOAuthService(nil, nil, admin)
	first.store, second.store = cnoauth.NewStore(client), cnoauth.NewStore(client)
	ctx := context.Background()
	view, err := first.Start(ctx, 7, "stepfun", "global", nil, 0)
	require.NoError(t, err)
	auth, err := url.Parse(view.AuthorizeURL)
	require.NoError(t, err)
	_, err = second.Advance(ctx, 7, "stepfun", view.SessionID, "http://127.0.0.1:53683/callback?state="+auth.Query().Get("state")+"&api_key=replica-key", false)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			svc := []*CNOAuthService{first, second}[n%2]
			_, _ = svc.Complete(ctx, 7, "stepfun", view.SessionID, CNOAuthCompleteInput{Name: "Step Plan"})
		}(n)
	}
	wg.Wait()
	require.Equal(t, 1, admin.writes)
	require.Equal(t, "replica-key", admin.input.Credentials["access_token"])
	require.NoError(t, validateStepFunCredentials("stepfun", "oauth", admin.input.Credentials))
	view, err = second.Complete(ctx, 7, "stepfun", view.SessionID, CNOAuthCompleteInput{})
	require.NoError(t, err)
	require.EqualValues(t, 42, view.AccountID)
}

type stepFunReceiptAdmin struct {
	cnOAuthAdminStub
	afterCreate func()
}

func (a *stepFunReceiptAdmin) CreateAccount(ctx context.Context, input *CreateAccountInput) (*Account, error) {
	account, err := a.cnOAuthAdminStub.CreateAccount(ctx, input)
	a.afterCreate()
	return account, err
}

func TestStepFunCompletionReceiptSurvivesConcurrentSessionRead(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	admin := &stepFunReceiptAdmin{}
	svc := NewCNOAuthService(nil, nil, admin)
	svc.store = cnoauth.NewStore(client)
	other := cnoauth.NewStore(client)
	ctx := context.Background()
	id := cnOAuthReady(t, svc, "stepfun", 0)
	reading := make(chan struct{})
	finished := make(chan error, 1)
	admin.afterCreate = func() {
		go func() {
			finished <- other.Update(ctx, id, func(_ context.Context, session *cnoauth.Session) error {
				close(reading)
				time.Sleep(60 * time.Millisecond)
				return nil
			})
		}()
		<-reading // Account write succeeds while another replica holds its lease.
	}
	view, err := svc.Complete(ctx, 7, "stepfun", id, CNOAuthCompleteInput{Name: "Step"})
	require.NoError(t, <-finished)
	require.NoError(t, err)
	require.Equal(t, "completed", view.Status)
	require.EqualValues(t, 42, view.AccountID)
	require.Equal(t, 1, admin.writes)
	view, err = svc.Complete(ctx, 7, "stepfun", id, CNOAuthCompleteInput{})
	require.NoError(t, err)
	require.EqualValues(t, 42, view.AccountID)
	require.Equal(t, 1, admin.writes)
}

func TestStepFunReauthorizationPreservesRegionAndAccountMetadata(t *testing.T) {
	for _, lifetime := range []string{"", "&expires_in=3600"} {
		t.Run(lifetime, func(t *testing.T) {
			a := stepFunTestAccount("oauth", "global")
			a.Credentials["expires_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
			a.Credentials["refresh_token"] = "obsolete"
			a.Credentials["model_mapping"] = map[string]any{"public": "step-3.7-flash"}
			admin := &cnOAuthAdminStub{}
			svc := NewCNOAuthService(nil, &refreshAPIAccountRepo{account: a}, admin)
			ctx := context.Background()
			view, err := svc.Start(ctx, 7, "stepfun", "cn", nil, a.ID)
			require.NoError(t, err)
			auth, err := url.Parse(view.AuthorizeURL)
			require.NoError(t, err)
			require.Equal(t, "platform.stepfun.ai", auth.Host, "relink uses the stored region")
			_, err = svc.Advance(ctx, 7, "stepfun", view.SessionID, "http://127.0.0.1:53683/callback?state="+auth.Query().Get("state")+"&access_token=new-key"+lifetime, false)
			require.NoError(t, err)
			_, err = svc.Complete(ctx, 7, "stepfun", view.SessionID, CNOAuthCompleteInput{})
			require.NoError(t, err)
			creds := admin.update.Credentials
			require.Equal(t, "global", creds["oauth_region"])
			require.Equal(t, "new-key", creds["access_token"])
			require.Equal(t, a.Credentials["model_mapping"], creds["model_mapping"])
			require.NotContains(t, creds, "refresh_token")
			if lifetime == "" {
				require.NotContains(t, creds, "expires_at")
			} else {
				expiry, err := time.Parse(time.RFC3339, creds["expires_at"].(string))
				require.NoError(t, err)
				require.WithinDuration(t, time.Now().Add(time.Hour), expiry, 2*time.Second)
			}
			require.NoError(t, validateStepFunCredentials("stepfun", "oauth", creds))
		})
	}
}
