//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These six literals are the public endpoint/account contracts, rather than
// values derived with the account URL or protocol implementation under test.
type stepCapabilityProfile struct{ name, kind, region, mode, endpoint string }

var stepCapabilityProfiles = []stepCapabilityProfile{
	{"oauth_plan_cn", "oauth", "cn", "coding", "https://api.stepfun.com/step_plan/v1/chat/completions"},
	{"oauth_plan_global", "oauth", "global", "coding", "https://api.stepfun.ai/step_plan/v1/chat/completions"},
	{"apikey_payg_cn", "apikey", "cn", "payg", "https://api.stepfun.com/v1/chat/completions"},
	{"apikey_payg_global", "apikey", "global", "payg", "https://api.stepfun.ai/v1/chat/completions"},
	{"apikey_plan_cn", "apikey", "cn", "coding", "https://api.stepfun.com/step_plan/v1/chat/completions"},
	{"apikey_plan_global", "apikey", "global", "coding", "https://api.stepfun.ai/step_plan/v1/chat/completions"},
}

func stepCapabilityForward(t *testing.T, svc *OpenAIGatewayService, ctx context.Context, account *Account, protocol, request string) (*OpenAIForwardResult, *httptest.ResponseRecorder, error) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	path := map[string]string{"chat": "/v1/chat/completions", "responses": "/v1/responses", "messages": "/v1/messages"}[protocol]
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(request)).WithContext(ctx)
	c.Request.Header.Set("User-Agent", "untrusted-browser/99")
	c.Request.Header.Set("X-Step-Client", "untrusted-client")
	c.Request.Header.Set("X-Stainless-Package-Version", "99.0.0")
	var result *OpenAIForwardResult
	var err error
	switch protocol {
	case "responses":
		result, err = svc.Forward(ctx, c, account, []byte(request))
	case "messages":
		result, err = svc.ForwardAsAnthropic(ctx, c, account, []byte(request), "", "")
	default:
		result, err = svc.ForwardAsChatCompletions(ctx, c, account, []byte(request), "", "")
	}
	return result, recorder, err
}

func stepCapabilityRequest(protocol string, stream bool, turn int) string {
	var input, tools string
	switch protocol {
	case "responses":
		input = `"input":[{"role":"user","content":"check weather and clock"}`
		if turn == 2 {
			input += `,{"type":"function_call","call_id":"call_weather","name":"weather","arguments":"{\"city\":\"Paris\"}"},{"type":"function_call","call_id":"call_clock","name":"clock","arguments":"{\"zone\":\"UTC\"}"},{"type":"function_call_output","call_id":"call_weather","output":"sunny"},{"type":"function_call_output","call_id":"call_clock","output":"12:00"}`
		}
		input += `]`
		tools = `[{"type":"function","name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}},{"type":"function","name":"clock","parameters":{"type":"object","properties":{"zone":{"type":"string"}},"required":["zone"]}}]`
	case "messages":
		input = `"max_tokens":64,"messages":[{"role":"user","content":"check weather and clock"}`
		if turn == 2 {
			input += `,{"role":"assistant","content":[{"type":"tool_use","id":"call_weather","name":"weather","input":{"city":"Paris"}},{"type":"tool_use","id":"call_clock","name":"clock","input":{"zone":"UTC"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_weather","content":"sunny"},{"type":"tool_result","tool_use_id":"call_clock","content":"12:00"}]}`
		}
		input += `]`
		tools = `[{"name":"weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}},{"name":"clock","input_schema":{"type":"object","properties":{"zone":{"type":"string"}},"required":["zone"]}}]`
	default:
		input = `"messages":[{"role":"user","content":"check weather and clock"}`
		if turn == 2 {
			input += `,{"role":"assistant","content":null,"tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}},{"id":"call_clock","type":"function","function":{"name":"clock","arguments":"{\"zone\":\"UTC\"}"}}]},{"role":"tool","tool_call_id":"call_weather","content":"sunny"},{"role":"tool","tool_call_id":"call_clock","content":"12:00"}`
		}
		input += `]`
		tools = `[{"type":"function","function":{"name":"weather","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}},{"type":"function","function":{"name":"clock","parameters":{"type":"object","properties":{"zone":{"type":"string"}},"required":["zone"]}}}]`
	}
	return fmt.Sprintf(`{"model":"step-3.7-flash","stream":%t,%s,"tools":%s}`, stream, input, tools)
}

func stepCapabilityReply(stream, tools bool) *http.Response {
	message, finish := `{"role":"assistant","reasoning_content":"check facts","content":"Paris is sunny at 12:00 UTC"}`, "stop"
	if tools {
		message = `{"role":"assistant","content":null,"tool_calls":[{"id":"call_weather","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}},{"id":"call_clock","type":"function","function":{"name":"clock","arguments":"{\"zone\":\"UTC\"}"}}]}`
		finish = "tool_calls"
	}
	usage := `{"prompt_tokens":7,"cached_tokens":2,"completion_tokens":5,"completion_tokens_details":{"reasoning_tokens":3},"total_tokens":12}`
	body := fmt.Sprintf(`{"id":"chat_step_matrix","object":"chat.completion","model":"step-3.7-flash","choices":[{"index":0,"message":%s,"finish_reason":%q}],"usage":%s}`, message, finish, usage)
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
		deltas := []string{`{"role":"assistant","reasoning_content":"check "}`, `{"reasoning_content":"facts"}`, `{"content":"Paris is sunny "}`, `{"content":"at 12:00 UTC"}`}
		if tools {
			// Interleaving distinct argument streams detects accidental merging or
			// reassignment even when both function names appear in the output.
			deltas = []string{
				`{"role":"assistant","tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"weather","arguments":""}},{"index":1,"id":"call_clock","type":"function","function":{"name":"clock","arguments":""}}]}`,
				`{"tool_calls":[{"index":1,"function":{"arguments":"{\"zone\":"}}]}`,
				`{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}`,
				`{"tool_calls":[{"index":1,"function":{"arguments":"\"UTC\"}"}}]}`,
				`{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]}`,
			}
		}
		var sse strings.Builder
		for _, delta := range deltas {
			fmt.Fprintf(&sse, "data: {\"id\":\"chat_step_matrix\",\"object\":\"chat.completion.chunk\",\"model\":\"step-3.7-flash\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", delta)
		}
		fmt.Fprintf(&sse, "data: {\"id\":\"chat_step_matrix\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q}]}\n\n", finish)
		fmt.Fprintf(&sse, "data: {\"id\":\"chat_step_matrix\",\"choices\":[],\"usage\":%s}\n\ndata: [DONE]\n\n", usage)
		body = sse.String()
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

type stepCapabilityTool struct{ ID, Name, Arguments string }
type stepCapabilityOutput struct {
	Text, Reasoning string
	Tools           map[int]stepCapabilityTool
	Terminal        bool
}

// Parse client protocol semantics independently from production adapters. No
// assertions use a production converter to manufacture the expected payload.
func stepCapabilityReadOutput(t *testing.T, protocol string, stream bool, body string) stepCapabilityOutput {
	t.Helper()
	out := stepCapabilityOutput{Tools: map[int]stepCapabilityTool{}}
	readItem := func(index int, item gjson.Result) {
		switch item.Get("type").String() {
		case "function_call":
			out.Tools[index] = stepCapabilityTool{item.Get("call_id").String(), item.Get("name").String(), item.Get("arguments").String()}
		case "tool_use":
			out.Tools[index] = stepCapabilityTool{item.Get("id").String(), item.Get("name").String(), item.Get("input").Raw}
		case "message":
			for _, content := range item.Get("content").Array() {
				out.Text += content.Get("text").String()
			}
		case "reasoning":
			for _, summary := range item.Get("summary").Array() {
				out.Reasoning += summary.Get("text").String()
			}
		case "text":
			out.Text += item.Get("text").String()
		case "thinking":
			out.Reasoning += item.Get("thinking").String()
		}
	}
	if !stream {
		require.True(t, json.Valid([]byte(body)))
		root := gjson.Parse(body)
		switch protocol {
		case "responses":
			for i, item := range root.Get("output").Array() {
				readItem(i, item)
			}
			out.Terminal = root.Get("status").String() == "completed"
		case "messages":
			for i, item := range root.Get("content").Array() {
				readItem(i, item)
			}
			out.Terminal = root.Get("stop_reason").String() == "end_turn" || root.Get("stop_reason").String() == "tool_use"
		default:
			message := root.Get("choices.0.message")
			out.Text, out.Reasoning = message.Get("content").String(), message.Get("reasoning_content").String()
			for i, tool := range message.Get("tool_calls").Array() {
				out.Tools[i] = stepCapabilityTool{tool.Get("id").String(), tool.Get("function.name").String(), tool.Get("function.arguments").String()}
			}
			out.Terminal = root.Get("choices.0.finish_reason").String() == "stop" || root.Get("choices.0.finish_reason").String() == "tool_calls"
		}
		return out
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			if protocol == "chat" {
				out.Terminal = true
			}
			continue
		}
		require.True(t, json.Valid([]byte(data)), "invalid SSE data: %s", data)
		event := gjson.Parse(data)
		switch protocol {
		case "responses":
			index := int(event.Get("output_index").Int())
			switch event.Get("type").String() {
			case "response.output_item.added":
				if event.Get("item.type").String() == "function_call" {
					readItem(index, event.Get("item"))
				}
			case "response.function_call_arguments.delta":
				tool := out.Tools[index]
				tool.Arguments += event.Get("delta").String()
				out.Tools[index] = tool
			case "response.output_text.delta":
				out.Text += event.Get("delta").String()
			case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
				out.Reasoning += event.Get("delta").String()
			case "response.completed":
				out.Terminal = event.Get("response.status").String() == "completed"
			}
		case "messages":
			index := int(event.Get("index").Int())
			switch event.Get("type").String() {
			case "content_block_start":
				if event.Get("content_block.type").String() == "tool_use" {
					out.Tools[index] = stepCapabilityTool{ID: event.Get("content_block.id").String(), Name: event.Get("content_block.name").String()}
				}
			case "content_block_delta":
				switch event.Get("delta.type").String() {
				case "text_delta":
					out.Text += event.Get("delta.text").String()
				case "thinking_delta":
					out.Reasoning += event.Get("delta.thinking").String()
				case "input_json_delta":
					tool := out.Tools[index]
					tool.Arguments += event.Get("delta.partial_json").String()
					out.Tools[index] = tool
				}
			case "message_stop":
				out.Terminal = true
			}
		default:
			for _, choice := range event.Get("choices").Array() {
				out.Text += choice.Get("delta.content").String()
				out.Reasoning += choice.Get("delta.reasoning_content").String()
				for _, delta := range choice.Get("delta.tool_calls").Array() {
					index := int(delta.Get("index").Int())
					tool := out.Tools[index]
					if delta.Get("id").Exists() {
						tool.ID = delta.Get("id").String()
						tool.Name = delta.Get("function.name").String()
					}
					tool.Arguments += delta.Get("function.arguments").String()
					out.Tools[index] = tool
				}
			}
		}
	}
	return out
}

func stepCapabilityAssertClientUsage(t *testing.T, protocol string, stream bool, body string) {
	t.Helper()
	usage := gjson.Get(body, "usage")
	if stream {
		for _, line := range strings.Split(body, "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			event := gjson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			switch protocol {
			case "responses":
				if event.Get("type").String() == "response.completed" {
					usage = event.Get("response.usage")
				}
			case "messages":
				if event.Get("type").String() == "message_delta" {
					usage = event.Get("usage")
				}
			default:
				if event.Get("usage").IsObject() {
					usage = event.Get("usage")
				}
			}
		}
	}
	require.True(t, usage.IsObject(), "the client must receive an actual usage object")
	switch protocol {
	case "responses":
		require.Equal(t, int64(7), usage.Get("input_tokens").Int())
		require.Equal(t, int64(2), usage.Get("input_tokens_details.cached_tokens").Int())
		require.Equal(t, int64(5), usage.Get("output_tokens").Int())
	case "messages":
		// Anthropic input excludes cache reads; 5 misses + 2 hits == 7 prompt.
		require.Equal(t, int64(5), usage.Get("input_tokens").Int())
		require.Equal(t, int64(2), usage.Get("cache_read_input_tokens").Int())
		require.Equal(t, int64(5), usage.Get("output_tokens").Int())
	default:
		require.Equal(t, int64(7), usage.Get("prompt_tokens").Int())
		require.Equal(t, int64(2), usage.Get("cached_tokens").Int())
		require.Equal(t, int64(5), usage.Get("completion_tokens").Int())
	}
}

func TestStepFunCapabilityBasicMatrix(t *testing.T) {
	for _, profile := range stepCapabilityProfiles {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", profile.name, protocol, stream), func(t *testing.T) {
					account := stepFunTestAccount(profile.kind, profile.region)
					account.Credentials["account_mode"] = profile.mode
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					upstream := &httpUpstreamRecorder{resp: stepCapabilityReply(stream, false)}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					result, recorder, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, stream, 1))
					require.NoError(t, err)
					require.Equal(t, http.StatusOK, recorder.Code)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, profile.endpoint, upstream.lastReq.URL.String())
					require.Equal(t, "Bearer test-key", upstream.lastReq.Header.Get("Authorization"))
					requireStepFunWire(t, upstream.lastReq, true)
					require.Equal(t, stream, gjson.GetBytes(upstream.lastBody, "stream").Bool())
					if stream {
						require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
					}
					require.Equal(t, "step-3.7-flash", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "messages.0.role").String())
					require.Equal(t, "check weather and clock", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
					require.Equal(t, 7, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.CacheReadInputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					require.Equal(t, stream, result.Stream)
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					billingRepo := &openAIRecordUsageBillingRepoStub{}
					billing := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					billing.cfg.Default.RateMultiplier = 1
					require.NoError(t, billing.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, Account: account,
						User: &User{ID: 7}, APIKey: &APIKey{ID: 8, Group: &Group{Platform: PlatformStepFun, RateMultiplier: 1}}}))
					require.Equal(t, 1, billingRepo.calls)
					// step-3.7 USD: 5 misses * .20/M + 2 hits * .04/M +
					// 5 output * 1.15/M = .00000683. Three reasoning tokens
					// already belong to the five output tokens; no extra charge.
					require.InDelta(t, .00000683, billingRepo.lastCmd.BalanceCost, 1e-12)
					require.Equal(t, 5, usageRepo.lastLog.InputTokens)
					require.Equal(t, 2, usageRepo.lastLog.CacheReadTokens)
					require.Equal(t, 5, usageRepo.lastLog.OutputTokens)
					require.InDelta(t, .00000683, usageRepo.lastLog.TotalCost, 1e-12)
					require.Equal(t, UsageSourceUpstreamExact, usageRepo.lastLog.UsageSource)
					out := stepCapabilityReadOutput(t, protocol, stream, recorder.Body.String())
					require.Equal(t, "Paris is sunny at 12:00 UTC", out.Text)
					require.Equal(t, "check facts", out.Reasoning)
					require.Empty(t, out.Tools)
					require.True(t, out.Terminal, "response must complete the client protocol")
					stepCapabilityAssertClientUsage(t, protocol, stream, recorder.Body.String())
				})
			}
		}
	}
}

func TestStepFunCapabilityCompletedWithoutUsageKeepsUnknownBillingSource(t *testing.T) {
	for _, profile := range stepCapabilityProfiles {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", profile.name, protocol, stream), func(t *testing.T) {
					account := stepFunTestAccount(profile.kind, profile.region)
					account.Credentials["account_mode"] = profile.mode
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					body := `{"id":"step-no-meter","object":"chat.completion","model":"step-3.7-flash","choices":[{"index":0,"message":{"role":"assistant","content":"completed without a meter"},"finish_reason":"stop"}]}`
					contentType := "application/json"
					if stream {
						contentType = "text/event-stream"
						body = "data: " + `{"id":"step-no-meter","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[{"index":0,"delta":{"role":"assistant","content":"completed without a meter"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					result, recorder, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, stream, 1))
					require.NoError(t, err)
					require.True(t, result.UsageComplete(), "response completion and the existence of a usage meter are independent")
					out := stepCapabilityReadOutput(t, protocol, stream, recorder.Body.String())
					require.Equal(t, "completed without a meter", out.Text)
					require.True(t, out.Terminal)
					require.Len(t, upstream.requests, 1, "missing usage cannot replay a completed request")
					require.Zero(t, result.Usage.InputTokens)
					require.Zero(t, result.Usage.OutputTokens)
					usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
					billingRepo := &openAIRecordUsageBillingRepoStub{}
					billing := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					billing.cfg.Default.RateMultiplier = 1
					require.NoError(t, billing.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, Account: account,
						User: &User{ID: 7}, APIKey: &APIKey{ID: 8, Group: &Group{Platform: PlatformStepFun, RateMultiplier: 1}}}))
					require.NotNil(t, usageRepo.lastLog)
					require.Equal(t, UsageCompletionCompleted, usageRepo.lastLog.CompletionStatus)
					require.Zero(t, usageRepo.lastLog.TotalTokens())
					require.Zero(t, usageRepo.lastLog.TotalCost)
					require.Zero(t, usageRepo.lastLog.ActualCost)
					require.Equal(t, UsageSourceUnknown, usageRepo.lastLog.UsageSource, "missing provider metering cannot be reported as exact zero usage")

					// A provider explicitly reporting zero is different evidence.
					// The missing-meter fix must not infer absence from token sums.
					if stream {
						body = strings.Replace(body, "data: [DONE]", "data: "+`{"id":"step-zero-meter","choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`+"\n\ndata: [DONE]", 1)
					} else {
						body = strings.TrimSuffix(body, "}") + `,"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
					}
					upstream.resp = &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
					zeroResult, _, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, stream, 1))
					require.NoError(t, err)
					require.True(t, zeroResult.UsageComplete())
					require.NoError(t, billing.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: zeroResult, Account: account,
						User: &User{ID: 7}, APIKey: &APIKey{ID: 8, Group: &Group{Platform: PlatformStepFun, RateMultiplier: 1}}}))
					require.Zero(t, usageRepo.lastLog.TotalTokens())
					require.Zero(t, usageRepo.lastLog.TotalCost)
					require.Equal(t, UsageSourceUpstreamExact, usageRepo.lastLog.UsageSource, "an explicit zero meter remains exact")
				})
			}
		}
	}
}

func TestStepFunCreditsAndWalletsNeverBecomePercentageQuota(t *testing.T) {
	for _, scenario := range []struct{ name, body string }{
		{"token-plan", strings.Replace(stepCodingUsageFixture, `"family":"coding"`, `"family":"token"`, 1)},
		{"credit-bucket", strings.Replace(stepCodingUsageFixture, `"credit_buckets":[]`, `"credit_buckets":[{"id":"plan-credit","remaining":999999,"total":1000000}]`, 1)},
		{"cny-wallet", `{"currency":"CNY","balance":999999,"is_available":true}`},
		{"usd-wallet", `{"currency":"USD","balance":999999,"is_available":true}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			account := nativeControlAccount(PlatformStepFun)
			account.Extra = map[string]any{"stepfun_5h_used_percent": 87.0, "stepfun_weekly_used_percent": 93.0, "stepfun_usage_updated_at": "2026-10-24T00:00:00Z"}
			repo := &kimiQuotaTestRepo{fakeCNProbeAccountRepo: fakeCNProbeAccountRepo{account: account}}
			upstream := &httpUpstreamRecorder{resp: nativeControlReply(scenario.body)}
			result, err := NewCNProviderQuotaService(repo, nil, upstream, rawChatCompletionsTestConfig()).QueryUsage(context.Background(), account.ID)
			require.NoError(t, err)
			require.False(t, result.Success, "credits or money cannot establish percentage-window permission")
			require.False(t, result.Persisted)
			require.Empty(t, result.Tiers)
			require.Nil(t, repo.updates, "unsupported units cannot overwrite the last authoritative window observation")
			require.Equal(t, 87.0, account.Extra["stepfun_5h_used_percent"])
			require.Equal(t, 93.0, account.Extra["stepfun_weekly_used_percent"])
			require.Equal(t, "2026-10-24T00:00:00Z", account.Extra["stepfun_usage_updated_at"])
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestStepFunCapabilityParallelToolsAndResults(t *testing.T) {
	for _, profile := range stepCapabilityProfiles {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", profile.name, protocol, stream), func(t *testing.T) {
					account := stepFunTestAccount(profile.kind, profile.region)
					account.Credentials["account_mode"] = profile.mode
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					upstream := &httpUpstreamRecorder{responses: []*http.Response{stepCapabilityReply(stream, true), stepCapabilityReply(stream, false)}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					result, recorder, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, stream, 1))
					require.NoError(t, err)
					require.Equal(t, 5, result.Usage.OutputTokens)
					out := stepCapabilityReadOutput(t, protocol, stream, recorder.Body.String())
					require.Len(t, out.Tools, 2)
					byID := map[string]stepCapabilityTool{}
					for _, tool := range out.Tools {
						byID[tool.ID] = tool
					}
					require.Len(t, byID, 2, "the tools must keep distinct IDs")
					require.Equal(t, "weather", byID["call_weather"].Name)
					require.JSONEq(t, `{"city":"Paris"}`, byID["call_weather"].Arguments)
					require.Equal(t, "clock", byID["call_clock"].Name)
					require.JSONEq(t, `{"zone":"UTC"}`, byID["call_clock"].Arguments)
					require.True(t, out.Terminal)
					_, next, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, stream, 2))
					require.NoError(t, err)
					require.Len(t, upstream.requests, 2, "one upstream attempt per accepted tool-result turn")
					require.Equal(t, profile.endpoint, upstream.lastReq.URL.String())
					requireStepFunWire(t, upstream.lastReq, true)
					messages := gjson.GetBytes(upstream.lastBody, "messages").Array()
					require.Len(t, messages, 4)
					require.Equal(t, "assistant", messages[1].Get("role").String())
					require.Len(t, messages[1].Get("tool_calls").Array(), 2)
					require.Equal(t, "call_weather", messages[1].Get("tool_calls.0.id").String())
					require.JSONEq(t, `{"city":"Paris"}`, messages[1].Get("tool_calls.0.function.arguments").String())
					require.Equal(t, "call_clock", messages[1].Get("tool_calls.1.id").String())
					require.JSONEq(t, `{"zone":"UTC"}`, messages[1].Get("tool_calls.1.function.arguments").String())
					for i, want := range []struct{ id, content string }{{"call_weather", "sunny"}, {"call_clock", "12:00"}} {
						require.Equal(t, "tool", messages[i+2].Get("role").String())
						require.Equal(t, want.id, messages[i+2].Get("tool_call_id").String())
						require.Equal(t, want.content, messages[i+2].Get("content").String())
					}
					nextOut := stepCapabilityReadOutput(t, protocol, stream, next.Body.String())
					require.Equal(t, "Paris is sunny at 12:00 UTC", nextOut.Text)
					require.True(t, nextOut.Terminal)
				})
			}
		}
	}
}
