//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Independent closed set from Codex efdacaf4 codex-api/src/error.rs, not from
// the classifier under test. A misleading message/type cannot authorize retry.
var officialCodexInputTerminalCodes = []string{
	"budget_reached", "audio_too_large", "audio_too_short", "invalid_audio",
	"invalid_audio_data", "invalid_audio_format", "missing_audio_sample_rate",
}

func TestOpenAIInputTerminalCodeAndHTTPStatusBoundaries(t *testing.T) {
	for _, code := range officialCodexInputTerminalCodes {
		for _, status := range []int{0, 400, 401, 403, 404, 429, 500, 503} {
			body := []byte(fmt.Sprintf(`{"error":{"code":%q,"message":"rate limit exceeded; servers overloaded; please retry"}}`, code))
			want := ""
			if status == 0 || status == 400 {
				want = code
			}
			require.Equal(t, want, openAIInputTerminalCode(status, body))
		}
		for _, body := range []string{
			fmt.Sprintf(`{"error":{"code":%q}}`, strings.ToUpper(code)),
			fmt.Sprintf(`{"error":{"code":%q}}`, code+" "),
			fmt.Sprintf(`{"error":{"type":%q,"message":%q}}`, code, code),
			fmt.Sprintf(`{"code":%q}`, code),
			fmt.Sprintf(`{"error":{"code":%q}`, code),
		} {
			require.Empty(t, openAIInputTerminalCode(400, []byte(body)), body)
		}
	}
}

func TestOpenAIInputTerminalErrorsNeverRetryOrPenalizeOwner(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, code := range officialCodexInputTerminalCodes {
			t.Run(kind+"/"+code, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"error":{"code":%q,"type":"rate_limit_error","message":"Our servers are currently overloaded. Please try again later."}}`, code))
				repo := &capacityShedAccountRepoStub{}
				svc := &OpenAIGatewayService{rateLimitService: NewRateLimitService(repo, nil, &config.Config{}, nil, nil)}
				account := &Account{ID: 81, Platform: PlatformOpenAI, Type: kind}
				require.False(t, svc.shouldFailoverOpenAIUpstreamResponse(account, 400, "please retry", body))
				require.False(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, 400, nil, body))
				failure := newOpenAIUpstreamFailoverError(400, nil, body, "connection refused", true)
				require.False(t, failure.RetryableOnSameAccount)
				require.False(t, failure.RequestScopedTransient)
				require.False(t, failure.ShouldRetryNextAccount())
				for _, payload := range []string{
					fmt.Sprintf(`{"type":"error","error":{"code":%q,"type":"rate_limit_error","message":"rate limit exceeded; please retry"}}`, code),
					fmt.Sprintf(`{"type":"response.failed","response":{"error":{"code":%q,"type":"server_error","message":"servers overloaded; please retry"}}}`, code),
				} {
					data := []byte(payload)
					require.Equal(t, 400, openAIStreamFailureStatus(data, "please retry"))
					require.False(t, openAIStreamFailedEventShouldFailover(data, "please retry"))
					require.False(t, openAIStreamErrorEventShouldFailover(data, "please retry"))
					require.Zero(t, openAIWSPayloadTransientStatus(data))
					reason, retry := classifyOpenAIWSErrorEvent(data)
					require.Equal(t, code, reason)
					require.False(t, retry)
					require.Equal(t, 400, openAIWSErrorHTTPStatus(data))
					svc.handleOpenAIWSTerminalTransientFailure(context.Background(), account, "gpt-6.1-sol", nil, data)
					svc.handleOpenAIWSErrorEventTransientFailure(context.Background(), account, "gpt-6.1-sol", nil, data)
				}
				require.Zero(t, repo.tempUnschedCalls)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			})
		}
	}
}

func TestOpenAIInputTerminalErrorsRetainCodesOnHTTPAndSSE(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{enabled: true, expiresAt: time.Now().Add(time.Hour).UnixNano()})
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, code := range officialCodexInputTerminalCodes {
			for _, transport := range []string{"http", "passthrough_http", "sse", "passthrough_sse", "buffered_sse", "passthrough_buffered_sse"} {
				t.Run(kind+"/"+code+"/"+transport, func(t *testing.T) {
					c, rec := newOpenAIUpstreamErrorTestContext(t)
					account := &Account{ID: 82, Platform: PlatformOpenAI, Type: kind}
					observer := &codexModelSchedulingObserver{}
					svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}, openaiScheduler: observer}
					body := fmt.Sprintf(`{"error":{"code":%q,"message":"Please retry","param":"input"}}`, code)
					payload := fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_input","status":"failed","error":{"code":%q,"message":"Please retry","param":"input"}}}`, code)
					stream := "event: response.failed\ndata: " + payload + "\n\n"
					resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"terminal-fixture"}, "X-Untrusted": {"private"}}, Body: io.NopCloser(strings.NewReader(stream))}
					if strings.Contains(transport, "buffered_sse") {
						rec.Header().Set("X-Codex-Primary-Used-Percent", "99")
					}
					var err error
					switch transport {
					case "http":
						_, err = svc.handleErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(400, body), c, account, nil)
					case "passthrough_http":
						err = svc.handleErrorResponsePassthrough(context.Background(), newOpenAIUpstreamErrorResponse(400, body), c, account, nil, []byte(body))
					case "sse":
						_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-6.1-sol", "gpt-6.1-sol")
					case "passthrough_sse":
						_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-6.1-sol", "gpt-6.1-sol")
					case "buffered_sse":
						_, err = svc.handleSSEToJSON(resp, c, account, []byte(stream), "gpt-6.1-sol", "gpt-6.1-sol")
					case "passthrough_buffered_sse":
						_, err = svc.handlePassthroughSSEToJSON(resp, c, account, []byte(stream), "gpt-6.1-sol", "gpt-6.1-sol")
					}
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.NotErrorAs(t, err, &failover)
					svc.ReportOpenAIAccountScheduleResult(account, "gpt-6.1-sol", false, nil, err)
					require.Zero(t, observer.reports, "input errors are not scheduler health evidence")
					require.True(t, isOpenAIRequestTerminalError(err))
					require.Contains(t, rec.Body.String(), `"code":"`+code+`"`)
					if strings.Contains(transport, "buffered_sse") {
						require.Equal(t, "application/json; charset=utf-8", rec.Header().Get("Content-Type"))
						require.Equal(t, "terminal-fixture", rec.Header().Get("X-Request-Id"))
						require.Empty(t, rec.Header().Get("X-Untrusted"))
						require.Empty(t, rec.Header().Get("X-Codex-Primary-Used-Percent"), "hidden quota mode still applies to terminal errors")
					}
					if !strings.HasPrefix(rec.Body.String(), "event:") {
						require.Equal(t, 400, rec.Code)
						require.Equal(t, code, gjson.Get(rec.Body.String(), "error.code").String())
					}
				})
			}
		}
	}
}

func TestOpenAIInputTerminalHTTPToWebSocketBridge(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, code := range officialCodexInputTerminalCodes {
			for _, status := range []int{400, 200} {
				t.Run(fmt.Sprintf("%s/%s/%d", kind, code, status), func(t *testing.T) {
					body := fmt.Sprintf(`{"error":{"code":%q,"message":"Please retry"}}`, code)
					if status == 200 {
						body = fmt.Sprintf("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":%q,\"message\":\"Please retry\"}}}\n\n", code)
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}}
					svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
					account := &Account{ID: 42, Platform: PlatformOpenAI, Type: kind, Concurrency: 1}
					payload := []byte(`{"type":"response.create","model":"gpt-6.1-sol","input":"hello"}`)
					var writes [][]byte
					result, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "fixture", payload, len(payload), "gpt-6.1-sol", "", "", "", "", 1, func(message []byte) error { writes = append(writes, message); return nil })
					var failover *UpstreamFailoverError
					require.NotErrorAs(t, err, &failover)
					observationErr := err
					if observationErr == nil && result != nil {
						observationErr = result.requestTerminalError
					}
					require.True(t, isOpenAIRequestTerminalError(observationErr), "bridge hooks must receive a request-scoped observation")
					require.NotEmpty(t, writes)
					require.Contains(t, string(writes[len(writes)-1]), `"code":"`+code+`"`)
				})
			}
		}
	}
}

func TestOpenAIInputTerminalWebSocketForwardingAndPrewarm(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, code := range officialCodexInputTerminalCodes {
			for _, event := range []string{
				fmt.Sprintf(`{"type":"error","error":{"code":%q,"message":"Please retry"}}`, code),
				fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_input","error":{"code":%q,"message":"Please retry"}}}`, code),
			} {
				for _, prewarm := range []bool{false, true} {
					for _, stream := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/%s/%s/prewarm=%v/stream=%v", kind, code, gjson.Get(event, "type").String(), prewarm, stream), func(t *testing.T) {
							recorder := httptest.NewRecorder()
							c, _ := gin.CreateTestContext(recorder)
							c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
							cfg := newOpenAIWSV2TestConfig()
							cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = prewarm
							cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
							cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
							cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
							conn := &openAIWSCaptureConn{events: [][]byte{[]byte(event)}}
							pool := newOpenAIWSConnPool(cfg)
							pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: conn})
							t.Cleanup(pool.Close)
							upstream := &httpUpstreamRecorder{}
							svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
							account := &Account{ID: 5888, Platform: PlatformOpenAI, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
							_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6.1-sol","stream":%v,"input":"hello"}`, stream)))
							require.Error(t, err)
							if prewarm {
								var failure *UpstreamFailoverError
								require.ErrorAs(t, err, &failure)
								require.Equal(t, 400, failure.StatusCode)
								require.False(t, failure.ShouldRetryNextAccount())
								require.False(t, failure.RetryableOnSameAccount)
								require.False(t, failure.ShouldReportAccountScheduleFailure())
								require.Contains(t, string(failure.ResponseBody), `"code":"`+code+`"`)
							} else {
								require.True(t, isOpenAIRequestTerminalError(err))
								require.Contains(t, recorder.Body.String(), `"code":"`+code+`"`)
								if !stream {
									require.Equal(t, 400, recorder.Code)
								}
							}
							require.Len(t, conn.writes, 1, "a terminal rejection must prevent another WS send")
							require.Empty(t, upstream.requests, "a terminal rejection must not fall back to HTTP")
						})
					}
				}
			}
		}
	}
}

func TestOpenAIInputTerminalCodePrecedesRequestRepairHeuristics(t *testing.T) {
	for _, code := range officialCodexInputTerminalCodes {
		for _, message := range []string{"Unknown parameter: 'truncation'.", "The model requested does not exist", "Unknown parameter: 'prompt_cache_breakpoint', not supported by this model"} {
			response := []byte(fmt.Sprintf(`{"error":{"code":%q,"message":%q}}`, code, message))
			body := []byte(`{"model":"gpt-5.6","input":"hello","truncation":"auto","prompt_cache_breakpoint":true}`)
			_, _, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(400, body, response)
			require.NoError(t, err)
			require.False(t, changed, "a terminal input rejection cannot authorize body repair")
			require.False(t, isOpenAICompactModelFailure(400, message, response), "a terminal input rejection cannot authorize model fallback")
		}
	}
}

func TestOpenAIInputTerminalIngressHooksDoNotPenalizeOwner(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, mode := range []string{OpenAIWSIngressModePassthrough, OpenAIWSIngressModeCtxPool} {
			for _, code := range officialCodexInputTerminalCodes {
				t.Run(kind+"/"+mode+"/"+code, func(t *testing.T) {
					ctx, cancel := context.WithCancelCause(context.Background())
					defer cancel(context.Canceled)
					upstream := newStagedPassthroughConn()
					upstream.Send(fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_input","error":{"code":%q,"type":"rate_limit_error","message":"Please retry"}}}`, code))
					cfg := passthroughLifecycleConfig()
					cfg.Gateway.OpenAIWS.OAuthEnabled = true
					svc := newPassthroughLifecycleService(cfg, upstream)
					pool := newOpenAIWSConnPool(cfg)
					pool.setClientDialerForTest(&stagedPassthroughDialer{conn: upstream})
					t.Cleanup(pool.Close)
					svc.openaiWSPool = pool
					account := passthroughLifecycleAccount()
					account.Type = kind
					account.Credentials["access_token"] = "sk-test"
					account.Credentials["chatgpt_account_id"] = "fixture-workspace"
					account.Extra = map[string]any{
						"responses_websockets_v2_enabled":            true,
						"openai_apikey_responses_websockets_v2_mode": mode,
						"openai_oauth_responses_websockets_v2_mode":  mode,
					}
					observed := make(chan error, 1)
					server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(*gin.Context) *OpenAIWSIngressHooks {
						return &OpenAIWSIngressHooks{AfterTurn: func(_ int, _ *OpenAIForwardResult, turnErr error) { observed <- turnErr }}
					})
					defer server.Close()
					client := dialPassthroughLifecycleClient(t, server)
					defer func() { _ = client.CloseNow() }()
					payload, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
					if err != nil {
						select {
						case upstreamErr := <-serverErr:
							t.Fatalf("read terminal event: %v; ingress: %v", err, upstreamErr)
						default:
						}
					}
					require.NoError(t, err)
					require.Contains(t, string(payload), `"code":"`+code+`"`)
					select {
					case turnErr := <-observed:
						require.True(t, isOpenAIRequestTerminalError(turnErr), "the scheduler hook must receive a request-scoped failure")
					case <-time.After(3 * time.Second):
						t.Fatal("terminal input rejection did not reach the turn hook")
					}
					require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
					select {
					case err := <-serverErr:
						require.NoError(t, err)
					case <-time.After(3 * time.Second):
						t.Fatal("input rejection session did not close")
					}
				})
			}
		}
	}
}
