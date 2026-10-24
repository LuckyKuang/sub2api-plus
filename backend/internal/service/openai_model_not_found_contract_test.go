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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIModelNotFound_ExactOfficialCodeAndStatus(t *testing.T) {
	for _, status := range []int{0, 400, 403, 404} {
		for _, body := range []string{
			`{"error":{"code":"model_not_found"}}`,
			`{"type":"response.failed","response":{"error":{"code":"model_not_found"}}}`,
		} {
			require.True(t, isOpenAIModelNotFoundError(status, []byte(body)))
		}
	}
	for _, status := range []int{401, 402, 429, 500, 503} {
		require.False(t, isOpenAIModelNotFoundError(status, []byte(`{"error":{"code":"model_not_found"}}`)))
	}
	for _, body := range []string{
		`{"error":{"message":"model_not_found"}}`,
		`{"error":{"code":"MODEL_NOT_FOUND"}}`,
		`{"error":{"code":"model_not_found_extra"}}`,
		`{"error":{"code":404}}`,
		`{"code":"model_not_found"}`,
		`{"error":`,
		`{"error":{"code":"model_not_found"`,
	} {
		require.False(t, isOpenAIModelNotFoundError(403, []byte(body)), body)
	}
}

type codexModelRejectionRepo struct {
	modelNotFoundAccountRepoStub
	authErrorCalls int
	rateLimitCalls int
}

func (r *codexModelRejectionRepo) SetError(context.Context, int64, string) error {
	r.authErrorCalls++
	return nil
}

func (r *codexModelRejectionRepo) SetRateLimited(context.Context, int64, time.Time) error {
	r.rateLimitCalls++
	return nil
}

func TestOpenAIModelNotFound_HTTPRejectionsNeverPenalizeWholeAccount(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, status := range []int{400, 403, 404} {
			t.Run(fmt.Sprintf("%s/%d", accountType, status), func(t *testing.T) {
				repo := &codexModelRejectionRepo{}
				svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
				account := openAIModelNotFoundTempAccount()
				account.Type = accountType
				body := []byte(`{"error":{"type":"invalid_request_error","code":"model_not_found","message":"Model A unavailable"}}`)
				for range 3 {
					require.True(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, status, http.Header{}, body, "model-A"))
				}
				require.Zero(t, repo.tempCalls)
				require.Zero(t, repo.authErrorCalls)
				require.Len(t, repo.modelRateLimitCalls, 3)
				for _, call := range repo.modelRateLimitCalls {
					require.Regexp(t, `^openai:model_not_found:[0-9a-f]{64}:model-A$`, call.scope)
					require.Equal(t, upstreamModelNotFoundReason, call.reason)
				}
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "model-B"))
				err := newOpenAIUpstreamFailoverError(status, nil, body, "Model A unavailable", true)
				require.False(t, err.RetryableOnSameAccount)
				require.True(t, err.ShouldRetryNextAccount(), "managed pools retain bounded alternate-owner recovery")
				require.False(t, err.ShouldReportAccountScheduleFailure())
			})
		}
	}
}

func TestOpenAIModelNotFound_MissingModelAttributionNeverWidensPenalty(t *testing.T) {
	repo := &codexModelRejectionRepo{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := openAIModelNotFoundTempAccount()
	for range 3 {
		require.True(t, svc.handleOpenAIAccountUpstreamError(context.Background(), account, 403, nil, []byte(`{"error":{"code":"model_not_found"}}`)))
	}
	require.Zero(t, repo.tempCalls)
	require.Zero(t, repo.authErrorCalls)
	require.Empty(t, repo.modelRateLimitCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}

func TestOpenAIModelNotFound_DirectHTTPRetainsTerminalCode(t *testing.T) {
	for _, status := range []int{400, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, rec := newOpenAIUpstreamErrorTestContext(t)
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			_, err := svc.handleErrorResponse(context.Background(), newOpenAIUpstreamErrorResponse(status,
				`{"error":{"code":"model_not_found","message":"Model A unavailable","param":"model"}}`), c, newOpenAIUpstreamErrorTestAccount(), nil)
			require.Error(t, err)
			require.True(t, isOpenAIModelNotFoundTerminalError(err))
			var failoverErr *UpstreamFailoverError
			require.NotErrorAs(t, err, &failoverErr)
			require.Equal(t, status, rec.Code)
			require.Equal(t, "model_not_found", gjson.Get(rec.Body.String(), "error.code").String())
			require.Equal(t, "model", gjson.Get(rec.Body.String(), "error.param").String())
		})
	}
}

func TestOpenAIModelNotFound_StreamingTerminalIsNotRetried(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, errorFields := range []string{
			`"code":"model_not_found","message":"Model A unavailable"`,
			`"code":"model_not_found","type":"invalid_request_error","message":"Please retry with another model"`,
		} {
			t.Run(fmt.Sprintf("passthrough=%v/%s", passthrough, errorFields), func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				payload := `{"type":"response.failed","response":{"id":"resp_model","error":{` + errorFields + `}}}`
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("event: response.failed\ndata: " + payload + "\n\n"))}
				svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}}
				account := &Account{ID: 812, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				var err error
				if passthrough {
					_, err = svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "model-A", "model-A")
				} else {
					_, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "model-A", "model-A")
				}
				require.Error(t, err)
				require.True(t, isOpenAIModelNotFoundTerminalError(err))
				var failoverErr *UpstreamFailoverError
				require.NotErrorAs(t, err, &failoverErr)
				require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.failed"))
				require.Contains(t, rec.Body.String(), `"code":"model_not_found"`)
				require.NotContains(t, rec.Body.String(), `"code":"upstream_error"`)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			})
		}
	}
}

func TestOpenAIModelNotFound_NonStreamingSSEAndPassthroughKeepCode(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, eventType := range []string{"error", "response.failed"} {
			t.Run(fmt.Sprintf("passthrough=%v/%s", passthrough, eventType), func(t *testing.T) {
				c, rec := newNonStreamingFailoverContext(t)
				payload := `{"type":"error","error":{"code":"model_not_found","message":"Model A unavailable, please retry"}}`
				if eventType == "response.failed" {
					payload = `{"type":"response.failed","response":{"error":{"code":"model_not_found","message":"Model A unavailable, please retry"}}}`
				}
				svc := newNonStreamingFailoverService()
				body := sseTerminalBody(eventType, payload)
				var err error
				if passthrough {
					_, err = svc.handlePassthroughSSEToJSON(newNonStreamingSSEResponse(), c, newNonStreamingFailoverAccount(), body, "model-A", "model-A")
				} else {
					_, err = svc.handleSSEToJSON(newNonStreamingSSEResponse(), c, newNonStreamingFailoverAccount(), body, "model-A", "model-A")
				}
				require.Error(t, err)
				require.True(t, isOpenAIModelNotFoundTerminalError(err))
				var failoverErr *UpstreamFailoverError
				require.NotErrorAs(t, err, &failoverErr)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "model_not_found", gjson.Get(rec.Body.String(), "error.code").String())
			})
		}
	}
}

func TestOpenAIModelNotFound_WSTerminalDoesNotBecomeTransient(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 823, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, payload := range []string{
		`{"type":"error","error":{"code":"model_not_found","type":"server_error","message":"Please retry"}}`,
		`{"type":"response.failed","response":{"error":{"code":"model_not_found","message":"Please retry"}}}`,
	} {
		for range 3 {
			svc.handleOpenAIWSErrorEventTransientFailure(context.Background(), account, "model-A", nil, []byte(payload))
			svc.handleOpenAIWSTerminalTransientFailure(context.Background(), account, "model-A", nil, []byte(payload))
		}
		require.Zero(t, openAIWSPayloadTransientStatus([]byte(payload)))
		require.False(t, openAIStreamFailedEventShouldFailover([]byte(payload), "Please retry"))
		require.False(t, openAIStreamErrorEventShouldFailover([]byte(payload), "Please retry"))
	}
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "model-A"))
	reason, canFallback := classifyOpenAIWSErrorEvent([]byte(`{"type":"error","error":{"code":"model_not_found","type":"server_error"}}`))
	require.Equal(t, "model_not_found", reason)
	require.False(t, canFallback)
	require.Equal(t, 400, openAIWSErrorHTTPStatus([]byte(`{"type":"error","error":{"code":"model_not_found"}}`)))
}

func TestOpenAIModelNotFound_WSPersistencePrefersExactTerminalCode(t *testing.T) {
	repo := &codexModelRejectionRepo{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	account := &Account{ID: 824, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for _, fields := range []string{
		`"code":"model_not_found","type":"rate_limit_error","message":"rate limit exceeded"`,
		`"code":"model_not_found","type":"authentication_error","message":"usage limit reached"`,
	} {
		payload := []byte(`{"type":"error","error":{` + fields + `}}`)
		code, errType, msg := parseOpenAIWSErrorEventFields(payload)
		require.False(t, isOpenAIWSRateLimitEvent(payload, code, errType, msg), "ingress/passthrough must not retry the owner for a model rejection")
		for range 3 {
			svc.persistOpenAIWSRateLimitSignal(context.Background(), account, http.Header{"Retry-After": {"3600"}}, payload, code, errType, msg, "model-A")
		}
	}
	require.Zero(t, repo.rateLimitCalls)
	require.Zero(t, repo.tempCalls)
	require.Zero(t, repo.authErrorCalls)
	require.Empty(t, repo.modelRateLimitCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	for _, payload := range []string{
		`{"type":"error","error":{"code":" model_not_found "}}`,
		`{"type":"error","error":{"code":"model_not_found"`,
	} {
		reason, _ := classifyOpenAIWSErrorEvent([]byte(payload))
		require.NotEqual(t, "model_not_found", reason)
		require.NotEqual(t, 400, openAIWSErrorHTTPStatus([]byte(payload)))
	}
}

func TestOpenAIModelNotFound_WSV2ForwardLoopPreservesTerminalFailure(t *testing.T) {
	for _, eventType := range []string{"error", "response.failed"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", eventType, stream), func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				cfg := newOpenAIWSV2TestConfig()
				cfg.Security.URLAllowlist.Enabled = false
				cfg.Security.URLAllowlist.AllowInsecureHTTP = true
				cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
				errFields := `"code":"model_not_found","type":"rate_limit_error","message":"Model unavailable; rate limit exceeded"`
				event := `{"type":"error","error":{` + errFields + `}}`
				if eventType == "response.failed" {
					event = `{"type":"response.failed","response":{"id":"resp_missing","status":"failed","error":{` + errFields + `},"usage":{"input_tokens":2,"output_tokens":0}}}`
				}
				conn := &openAIWSCaptureConn{events: [][]byte{[]byte(event)}}
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: conn})
				t.Cleanup(pool.Close)
				repo := &codexModelRejectionRepo{}
				upstream := &httpUpstreamRecorder{}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool, rateLimitService: &RateLimitService{accountRepo: repo}}
				account := &Account{ID: 5886, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
				_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":"hello"}`, stream)))
				require.Error(t, err)
				require.True(t, isOpenAIModelNotFoundTerminalError(err))
				var failoverErr *UpstreamFailoverError
				require.NotErrorAs(t, err, &failoverErr)
				if stream {
					require.Contains(t, recorder.Body.String(), `"code":"model_not_found"`)
				} else {
					require.Equal(t, 400, recorder.Code)
					require.Equal(t, "model_not_found", gjson.Get(recorder.Body.String(), "error.code").String())
				}
				require.Empty(t, upstream.requests, "terminal model failure must not fall back to HTTP")
				require.Zero(t, repo.rateLimitCalls)
				require.Zero(t, repo.tempCalls)
				require.Zero(t, repo.authErrorCalls)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			})
		}
	}
}

func TestOpenAIModelNotFound_LateGrantRejectionCannotBlockNewCredentialGeneration(t *testing.T) {
	repo := &codexModelRejectionRepo{}
	svc := &OpenAIGatewayService{rateLimitService: &RateLimitService{accountRepo: repo}}
	old := &Account{ID: 999, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "old-secret-key", "model_mapping": map[string]any{"public-A": "model-A", "model-A": "model-B"}}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, old, "old-secret-key")
	attempt := withRequestCredentialOwner(ctx, old, "old-secret-key")
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["api_key"] = "new-secret-key"
	body := []byte(`{"error":{"code":"model_not_found","message":"Model A unavailable"}}`)
	// This response arrives after the same account row was reauthorized.
	require.True(t, svc.handleOpenAIAccountUpstreamError(attempt, current, http.StatusForbidden, nil, body, "model-A"))
	require.Len(t, repo.modelRateLimitCalls, 1)
	oldCall := repo.modelRateLimitCalls[0]
	require.Equal(t, old.ID, oldCall.accountID)
	require.Regexp(t, `^openai:model_not_found:[0-9a-f]{64}:model-A$`, oldCall.scope)
	require.NotContains(t, oldCall.scope, "old-secret-key")
	require.NotContains(t, oldCall.scope, "new-secret-key")
	limits := map[string]any{oldCall.scope: map[string]any{"rate_limit_reset_at": oldCall.resetAt.UTC().Format(time.RFC3339)}}
	old.Extra = map[string]any{modelRateLimitsKey: limits}
	current.Extra = old.Extra
	require.False(t, old.IsSchedulableForModelWithContext(context.Background(), "public-A"), "the rejected transmitted model remains excluded for the old grant")
	require.True(t, old.IsSchedulableForModelWithContext(context.Background(), "model-A"), "canonical model-A must not be mapped twice to model-B on persistence")
	require.True(t, old.IsSchedulableForModelWithContext(context.Background(), "unrelated-model"))
	require.True(t, current.IsSchedulableForModelWithContext(context.Background(), "public-A"))
	projection := snapshotRequestCredentialAccount(current)
	projection.CredentialGeneration = CredentialStateGeneration(current)
	projection.Credentials = nil
	require.True(t, projection.IsSchedulableForModelWithContext(context.Background(), "public-A"))
	oldProjection := snapshotRequestCredentialAccount(old)
	oldProjection.CredentialGeneration = CredentialStateGeneration(old)
	oldProjection.Credentials = nil
	require.False(t, oldProjection.IsSchedulableForModelWithContext(context.Background(), "model-A"))

	currentAttempt := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), current, "new-secret-key")
	require.True(t, svc.handleOpenAIAccountUpstreamError(currentAttempt, current, http.StatusForbidden, nil, body, "model-A"))
	require.Len(t, repo.modelRateLimitCalls, 2)
	newCall := repo.modelRateLimitCalls[1]
	require.NotEqual(t, oldCall.scope, newCall.scope)
	limits[newCall.scope] = map[string]any{"rate_limit_reset_at": newCall.resetAt.UTC().Format(time.RFC3339)}
	require.False(t, current.IsSchedulableForModelWithContext(context.Background(), "public-A"))
	projection.Extra = current.Extra
	require.False(t, projection.IsSchedulableForModelWithContext(context.Background(), "model-A"), "slim scheduler projection uses the credential digest without retaining secrets")
	require.True(t, current.IsSchedulableForModelWithContext(context.Background(), "unrelated-model"))
	require.Zero(t, repo.tempCalls)
	require.Zero(t, repo.authErrorCalls)
}

type codexModelSchedulingObserver struct {
	OpenAIAccountScheduler
	reports int
}

func (s *codexModelSchedulingObserver) ReportResult(int64, bool, *int) { s.reports++ }

func TestOpenAIModelNotFound_TerminalObservationDoesNotPenalizeScheduler(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	t.Cleanup(resetOpenAIAdvancedSchedulerSettingCacheForTest)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{enabled: true, expiresAt: time.Now().Add(time.Hour).UnixNano()})
	observer := &codexModelSchedulingObserver{}
	svc := &OpenAIGatewayService{openaiScheduler: observer}
	account := &Account{ID: 1000, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	terminal := &openAIModelNotFoundTerminalError{cause: fmt.Errorf("upstream response failed: Model A unavailable")}
	require.False(t, svc.ReportOpenAIAccountScheduleResult(account, "model-A", false, nil, terminal))
	require.False(t, svc.ObserveOpenAIAccountHealthFailure(context.Background(), account, terminal))
	require.Zero(t, observer.reports)
	// An unrelated terminal failure still reaches scheduling health.
	svc.ReportOpenAIAccountScheduleResult(account, "model-A", false, nil, fmt.Errorf("upstream response failed: server unavailable"))
	require.Equal(t, 1, observer.reports)
}

func TestOpenAIModelNotFound_PrewarmTerminalDoesNotSubmitSameOwnerAgain(t *testing.T) {
	for _, event := range []string{
		`{"type":"error","error":{"code":"model_not_found","message":"Model unavailable"}}`,
		`{"type":"response.failed","response":{"id":"resp_prewarm_missing","error":{"code":"model_not_found","message":"Model unavailable"}}}`,
	} {
		t.Run(event, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			conn := &openAIWSCaptureConn{events: [][]byte{[]byte(event)}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: conn})
			t.Cleanup(pool.Close)
			upstream := &httpUpstreamRecorder{}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool}
			account := &Account{ID: 5888, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
			_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`))
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, 400, failover.StatusCode)
			require.False(t, failover.ShouldRetryNextAccount())
			require.False(t, failover.RetryableOnSameAccount)
			require.False(t, failover.ShouldReportAccountScheduleFailure())
			require.Contains(t, string(failover.ResponseBody), `"code":"model_not_found"`)
			require.Len(t, conn.writes, 1, "the terminal prewarm rejection must prevent a second turn send")
			require.Empty(t, upstream.requests, "the terminal rejection must not fall back to HTTP")
		})
	}
}
