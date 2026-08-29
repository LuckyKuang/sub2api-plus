package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type resetAtLocalQuotaSettingRepo struct {
	SettingRepository
}

func (resetAtLocalQuotaSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyOpenAICodexLocalGroupQuotaEnabled {
		return "true", nil
	}
	return "", ErrSettingNotFound
}

func TestApplyCodexLocalGroupQuotaHeadersForRequestWritesResetAtBeforeWebSocketUpgrade(t *testing.T) {
	gin.SetMode(gin.TestMode)
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	now := time.Now()
	weeklyStart := now.Add(-time.Hour)
	fiveHourStart := now.Add(-time.Hour)
	group := &Group{
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		WeeklyLimitUSD:   &weeklyLimit,
		FiveHourLimitUSD: &fiveHourLimit,
	}
	subscription := &UserSubscription{
		WeeklyUsageUSD:      25,
		WeeklyWindowStart:   &weeklyStart,
		FiveHourUsageUSD:    5,
		FiveHourWindowStart: &fiveHourStart,
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request = request.WithContext(context.WithValue(request.Context(), ctxkey.Group, group))
	c.Set("subscription", subscription)
	c.Writer.Header().Set("X-Codex-Primary-Reset-At", "upstream-primary")
	c.Writer.Header().Set("X-Codex-Secondary-Reset-At", "upstream-secondary")

	svc := &OpenAIGatewayService{
		settingService: NewSettingService(resetAtLocalQuotaSettingRepo{}, &config.Config{}),
	}
	svc.ApplyCodexLocalGroupQuotaHeadersForRequest(c)

	require.Equal(t, strconv.FormatInt(weeklyStart.Add(7*24*time.Hour).Unix(), 10), c.Writer.Header().Get("X-Codex-Primary-Reset-At"))
	require.NotEmpty(t, c.Writer.Header().Get("X-Codex-Primary-Reset-After-Seconds"))
	require.Equal(t, strconv.FormatInt(fiveHourStart.Add(5*time.Hour).Unix(), 10), c.Writer.Header().Get("X-Codex-Secondary-Reset-At"))
	require.NotEmpty(t, c.Writer.Header().Get("X-Codex-Secondary-Reset-After-Seconds"))
}

type localQuotaEnabledSettingRepo struct {
	SettingRepository
	enabled bool
}

func (r localQuotaEnabledSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyOpenAICodexLocalGroupQuotaEnabled {
		return "", ErrSettingNotFound
	}
	return strconv.FormatBool(r.enabled), nil
}

func (localQuotaEnabledSettingRepo) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func newCodexLocalQuotaResponseTestContext(t *testing.T, enabled bool, path string) (*OpenAIGatewayService, *gin.Context, *httptest.ResponseRecorder, *Group, *UserSubscription) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	now := time.Now()
	weeklyStart := now.Add(-time.Hour)
	fiveHourStart := now.Add(-time.Hour)
	group := &Group{
		Platform:         PlatformOpenAI,
		SubscriptionType: SubscriptionTypeSubscription,
		WeeklyLimitUSD:   &weeklyLimit,
		FiveHourLimitUSD: &fiveHourLimit,
	}
	subscription := &UserSubscription{
		WeeklyUsageUSD:      25,
		WeeklyWindowStart:   &weeklyStart,
		FiveHourUsageUSD:    5,
		FiveHourWindowStart: &fiveHourStart,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, path, nil)
	c.Request = request.WithContext(context.WithValue(request.Context(), ctxkey.Group, group))
	c.Set("subscription", subscription)
	svc := &OpenAIGatewayService{
		cfg:            &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
		settingService: NewSettingService(localQuotaEnabledSettingRepo{enabled: enabled}, &config.Config{}),
		toolCorrector:  NewCodexToolCorrector(),
	}
	return svc, c, recorder, group, subscription
}

func requireLocalCodexQuotaHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	require.Equal(t, "25", headers.Get("X-Codex-Primary-Used-Percent"))
	require.Equal(t, "10080", headers.Get("X-Codex-Primary-Window-Minutes"))
	require.Equal(t, "25", headers.Get("X-Codex-Secondary-Used-Percent"))
	require.Equal(t, "300", headers.Get("X-Codex-Secondary-Window-Minutes"))
	require.NotEqual(t, "upstream-primary", headers.Get("X-Codex-Primary-Reset-At"))
	require.NotEqual(t, "upstream-secondary", headers.Get("X-Codex-Secondary-Reset-At"))
}

func codexLocalQuotaUpstreamHeaders() http.Header {
	return http.Header{
		"Content-Type":               []string{"text/event-stream"},
		"X-Codex-Primary-Reset-At":   []string{"upstream-primary"},
		"X-Codex-Secondary-Reset-At": []string{"upstream-secondary"},
	}
}

func codexLocalQuotaCompletedSSE() string {
	return "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_quota\",\"model\":\"gpt-5\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":4,\"output_tokens\":2,\"total_tokens\":6}}}\n\n"
}

func TestFinalizeCodexClientQuotaHeadersPolicy(t *testing.T) {
	enabledSvc, c, _, _, _ := newCodexLocalQuotaResponseTestContext(t, true, "/v1/responses")
	upstream := codexLocalQuotaUpstreamHeaders()
	passthroughAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"openai_passthrough": true},
	}
	staged := enabledSvc.finalizeCodexClientQuotaHeaders(upstream.Clone(), c, passthroughAccount)
	requireLocalCodexQuotaHeaders(t, staged)
	require.Equal(t, "upstream-primary", upstream.Get("X-Codex-Primary-Reset-At"))
	require.Equal(t, "upstream-secondary", upstream.Get("X-Codex-Secondary-Reset-At"))

	disabledSvc, disabledContext, _, _, _ := newCodexLocalQuotaResponseTestContext(t, false, "/v1/responses")
	passthrough := disabledSvc.finalizeCodexClientQuotaHeaders(upstream.Clone(), disabledContext, passthroughAccount)
	require.Equal(t, "upstream-primary", passthrough.Get("X-Codex-Primary-Reset-At"))
	require.Equal(t, "upstream-secondary", passthrough.Get("X-Codex-Secondary-Reset-At"))

	hidden := disabledSvc.finalizeCodexClientQuotaHeaders(upstream.Clone(), disabledContext, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.Empty(t, hidden.Get("X-Codex-Primary-Reset-At"))
	require.Empty(t, hidden.Get("X-Codex-Secondary-Reset-At"))
}

func TestFinalizeCodexClientQuotaEventPolicy(t *testing.T) {
	upstream := []byte(`{"type":"codex.rate_limits","rate_limits":{"primary":{"used_percent":90,"window_minutes":10080,"reset_at":1780000001},"secondary":{"used_percent":80,"window_minutes":300,"reset_at":1780003601}}}`)
	passthroughAccount := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"openai_passthrough": true},
	}

	enabledSvc, enabledContext, _, _, _ := newCodexLocalQuotaResponseTestContext(t, true, "/v1/responses")
	local, emit := enabledSvc.finalizeCodexClientQuotaEvent(upstream, enabledContext, passthroughAccount)
	require.True(t, emit)
	require.Equal(t, 25.0, gjson.GetBytes(local, "rate_limits.primary.used_percent").Float())
	require.Equal(t, int64(10080), gjson.GetBytes(local, "rate_limits.primary.window_minutes").Int())
	require.Equal(t, 25.0, gjson.GetBytes(local, "rate_limits.secondary.used_percent").Float())
	require.Equal(t, int64(300), gjson.GetBytes(local, "rate_limits.secondary.window_minutes").Int())

	disabledSvc, disabledContext, _, _, _ := newCodexLocalQuotaResponseTestContext(t, false, "/v1/responses")
	passthrough, emit := disabledSvc.finalizeCodexClientQuotaEvent(upstream, disabledContext, passthroughAccount)
	require.True(t, emit)
	require.Equal(t, upstream, passthrough)

	hidden, emit := disabledSvc.finalizeCodexClientQuotaEvent(upstream, disabledContext, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.False(t, emit)
	require.Nil(t, hidden)

	modelSpecific := []byte(`{"type":"codex.rate_limits","metered_limit_name":"codex_bengalfox","rate_limits":{"primary":{"used_percent":70}}}`)
	preserved, emit := disabledSvc.finalizeCodexClientQuotaEvent(modelSpecific, disabledContext, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.True(t, emit)
	require.Equal(t, modelSpecific, preserved)
}

func TestCodexLocalGroupQuotaHeadersReachResponsesStreamingClient(t *testing.T) {
	svc, c, recorder, _, _ := newCodexLocalQuotaResponseTestContext(t, true, "/v1/responses")
	upstreamHeaders := codexLocalQuotaUpstreamHeaders()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     upstreamHeaders,
		Body:       io.NopCloser(strings.NewReader(codexLocalQuotaCompletedSSE())),
	}

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "gpt-5", "gpt-5")
	require.NoError(t, err)
	require.NotNil(t, result)
	requireLocalCodexQuotaHeaders(t, recorder.Header())
	require.Equal(t, "upstream-primary", upstreamHeaders.Get("X-Codex-Primary-Reset-At"))
}

func TestCodexLocalGroupQuotaHeadersReachResponsesJSONAndCompactClients(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			svc, c, recorder, _, _ := newCodexLocalQuotaResponseTestContext(t, true, path)
			upstreamHeaders := codexLocalQuotaUpstreamHeaders()
			upstreamHeaders.Set("Content-Type", "application/json")
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     upstreamHeaders,
				Body: io.NopCloser(strings.NewReader(`{
					"id":"resp_quota","model":"gpt-5","status":"completed","output":[],
					"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}
				}`)),
			}

			result, err := svc.handleNonStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "gpt-5", "gpt-5")
			require.NoError(t, err)
			require.NotNil(t, result)
			requireLocalCodexQuotaHeaders(t, recorder.Header())
			require.Equal(t, "upstream-primary", upstreamHeaders.Get("X-Codex-Primary-Reset-At"))
		})
	}
}

func TestCodexLocalGroupQuotaHeadersReachConvertedClients(t *testing.T) {
	tests := []struct {
		name string
		path string
		run  func(*OpenAIGatewayService, *http.Response, *gin.Context) error
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			run: func(svc *OpenAIGatewayService, resp *http.Response, c *gin.Context) error {
				_, err := svc.handleChatBufferedStreamingResponse(resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, "gpt-5", "gpt-5", "gpt-5", time.Now())
				return err
			},
		},
		{
			name: "messages",
			path: "/v1/messages",
			run: func(svc *OpenAIGatewayService, resp *http.Response, c *gin.Context) error {
				_, err := svc.handleAnthropicBufferedStreamingResponse(resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, "gpt-5", "gpt-5", "gpt-5", time.Now())
				return err
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, c, recorder, _, _ := newCodexLocalQuotaResponseTestContext(t, true, tc.path)
			upstreamHeaders := codexLocalQuotaUpstreamHeaders()
			resp := &http.Response{StatusCode: http.StatusOK, Header: upstreamHeaders, Body: io.NopCloser(strings.NewReader(codexLocalQuotaCompletedSSE()))}

			require.NoError(t, tc.run(svc, resp, c))
			requireLocalCodexQuotaHeaders(t, recorder.Header())
			require.Equal(t, "upstream-primary", upstreamHeaders.Get("X-Codex-Primary-Reset-At"))
		})
	}
}
