//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The official DeepSeek Chat usage fixture is 100 prompt tokens: 80 cache hits
// and 20 misses. Accounting and the actual downstream JSON/SSE must agree;
// Messages reports uncached input separately, Responses reports total input.
func TestDeepSeekChatCacheUsageSurvivesDownstreamProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"chat", "responses", "messages"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				name, extra string
				wantCache   int
				wantCost    float64
			}{
				{"hit fallback", "", 80, .38},
				{"prompt details zero", `,"prompt_tokens_details":{"cached_tokens":0}`, 0, 1.10},
				{"input details zero", `,"input_tokens_details":{"cached_tokens":0}`, 0, 1.10},
				{"input details wins", `,"input_tokens_details":{"cached_tokens":24},"prompt_tokens_details":{"cached_tokens":32}`, 24, .884},
				{"cache read input zero", `,"cache_read_input_tokens":0`, 0, 1.10},
				{"cache read input wins", `,"cache_read_input_tokens":24,"cache_read_tokens":32,"cached_tokens":48`, 24, .884},
				{"cache read zero", `,"cache_read_tokens":0`, 0, 1.10},
				{"cache read wins", `,"cache_read_tokens":24,"cached_tokens":32`, 24, .884},
				{"cached zero", `,"cached_tokens":0`, 0, 1.10},
			} {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", protocol, stream, tc.name), func(t *testing.T) {
					extra, wantCache := tc.extra, tc.wantCache
					usage := `{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20` + extra + `}`
					upstreamBody := `{"id":"chat-cache","model":"deepseek-flash","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":` + usage + `}`
					contentType := "application/json"
					if stream {
						contentType = "text/event-stream"
						upstreamBody = "data: {\"id\":\"chat-cache\",\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
							"data: {\"id\":\"chat-cache\",\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":" + usage + "}\n\ndata: [DONE]\n\n"
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(upstreamBody))}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
					account := &Account{ID: 18, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Status: StatusActive, Credentials: map[string]any{"api_key": "test-deepseek-key"}}
					body := []byte(fmt.Sprintf(`{"model":"deepseek-flash","messages":[{"role":"user","content":"hello"}],"max_tokens":32,"stream":%t}`, stream))
					if protocol == "responses" {
						body = []byte(fmt.Sprintf(`{"model":"deepseek-flash","input":"hello","stream":%t}`, stream))
					}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, bytes.NewReader(body))
					var result *OpenAIForwardResult
					var err error
					switch protocol {
					case "chat":
						result, err = svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
					case "responses":
						result, err = svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body, time.Now())
					case "messages":
						result, err = svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, body, "")
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 100, result.Usage.InputTokens)
					require.Equal(t, wantCache, result.Usage.CacheReadInputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "Bearer test-deepseek-key", upstream.lastReq.Header.Get("Authorization"))
					var downstream gjson.Result
					if stream {
						for _, line := range strings.Split(recorder.Body.String(), "\n") {
							payload := strings.TrimPrefix(line, "data: ")
							parsed := gjson.Parse(payload)
							if protocol == "responses" && parsed.Get("type").String() == "response.completed" {
								downstream = parsed.Get("response.usage")
							}
							if protocol != "responses" && parsed.Get("usage").IsObject() {
								downstream = parsed.Get("usage")
							}
						}
					} else {
						downstream = gjson.Get(recorder.Body.String(), "usage")
					}
					require.True(t, downstream.IsObject(), recorder.Body.String())
					switch protocol {
					case "responses":
						require.EqualValues(t, 100, downstream.Get("input_tokens").Int())
						require.EqualValues(t, wantCache, downstream.Get("input_tokens_details.cached_tokens").Int())
					case "messages":
						require.EqualValues(t, 100-wantCache, downstream.Get("input_tokens").Int())
						require.EqualValues(t, wantCache, downstream.Get("cache_read_input_tokens").Int())
					case "chat":
						require.EqualValues(t, 80, downstream.Get("prompt_cache_hit_tokens").Int(), "raw Chat retains provider usage verbatim")
					}
					// An explicit operator card is the independent billing oracle:
					// 20 misses at $0.01 + 80 hits at $0.001 + 5 outputs at $0.02 = $0.38.
					// An authoritative zero cache count instead bills $1.10.
					// An authoritative 24 hits gives $0.76 + $0.024 + $0.10 = $0.884.
					logs := &openAIRecordUsageLogRepoStub{inserted: true}
					ledger := &openAIRecordUsageBillingRepoStub{}
					billing := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, ledger, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					billing.resolver = NewModelPricingResolver(nil, billing.billingService)
					group := &Group{ID: 9, Platform: PlatformDeepseek, RateMultiplier: 1, ModelPricing: []ChannelModelPricing{{
						Platform: PlatformDeepseek, Models: []string{"deepseek-flash"}, BillingMode: BillingModeToken,
						InputPrice: billingFloat(.01), OutputPrice: billingFloat(.02), CacheReadPrice: billingFloat(.001),
					}}}
					key := &APIKey{ID: 7, GroupID: &group.ID, Group: group}
					billingCtx := billing.WithBillingPricingSnapshot(WithResolvedTargetPlatform(context.Background(), PlatformDeepseek), key, time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC), "deepseek-flash")
					require.NoError(t, billing.RecordUsage(billingCtx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: &User{ID: 8}, Account: account}))
					require.Equal(t, 1, ledger.calls)
					require.NotNil(t, logs.lastLog)
					require.Equal(t, 100-wantCache, logs.lastLog.InputTokens)
					require.Equal(t, wantCache, logs.lastLog.CacheReadTokens)
					require.InDelta(t, tc.wantCost, logs.lastLog.ActualCost, 1e-12)
					require.InDelta(t, tc.wantCost, ledger.lastCmd.BalanceCost, 1e-12)
				})
			}
		}
	}
}
