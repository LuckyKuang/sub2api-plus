package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexLocalQuotaSettingRepo struct {
	service.SettingRepository
	enabled bool
}

func codexLocalQuotaTimePtr(value time.Time) *time.Time {
	return &value
}

func (r codexLocalQuotaSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != service.SettingKeyOpenAICodexLocalGroupQuotaEnabled {
		return "", service.ErrSettingNotFound
	}
	if r.enabled {
		return "true", nil
	}
	return "false", nil
}

func newCodexLocalQuotaHandler(enabled bool) *OpenAIGatewayHandler {
	settings := service.NewSettingService(codexLocalQuotaSettingRepo{enabled: enabled}, &config.Config{})
	gatewayService := service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, &config.Config{},
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		settings, nil,
	)
	return &OpenAIGatewayHandler{gatewayService: gatewayService}
}

func performCodexLocalQuotaRequest(t *testing.T, handler *OpenAIGatewayHandler, apiKey *service.APIKey, subscription *service.UserSubscription) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/backend-api/wham/usage", nil)
	if apiKey != nil {
		ctx.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	}
	if subscription != nil {
		ctx.Set(string(middleware2.ContextKeySubscription), subscription)
	}
	handler.CodexLocalGroupQuotaUsage(ctx)
	return recorder
}

func TestResponsesWebSocketCodexLocalQuotaHeadersInSwitchingProtocolsResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	now := time.Now().UTC()
	weeklyStart := now.Add(-time.Hour)
	fiveHourStart := now.Add(-time.Hour)
	group := &service.Group{
		ID:               8,
		Platform:         service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeSubscription,
		WeeklyLimitUSD:   &weeklyLimit,
		FiveHourLimitUSD: &fiveHourLimit,
	}
	apiKey := &service.APIKey{
		ID:      7,
		GroupID: &group.ID,
		Group:   group,
		User:    &service.User{ID: 1},
	}
	subscription := &service.UserSubscription{
		ID:                  9,
		UserID:              1,
		GroupID:             group.ID,
		WeeklyUsageUSD:      25,
		WeeklyWindowStart:   &weeklyStart,
		FiveHourUsageUSD:    5,
		FiveHourWindowStart: &fiveHourStart,
	}

	for _, tc := range []struct {
		name    string
		enabled bool
	}{
		{name: "local view enabled", enabled: true},
		{name: "local view disabled", enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := newCodexLocalQuotaHandler(tc.enabled)
			handler.billingCacheService = &service.BillingCacheService{}
			handler.apiKeyService = &service.APIKeyService{}
			handler.concurrencyHelper = NewConcurrencyHelper(
				service.NewConcurrencyService(&concurrencyCacheMock{}),
				SSEPingFormatNone,
				time.Second,
			)
			attachDisabledIPAccessControlForWebSocketTest(handler)

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 1, Concurrency: 1})
				c.Set(string(middleware2.ContextKeySubscription), subscription)
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, group))
				c.Next()
			})
			router.GET("/openai/v1/responses", handler.ResponsesWebSocket)

			server := httptest.NewServer(router)
			defer server.Close()
			dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
			conn, response, err := coderws.Dial(
				dialCtx,
				"ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses",
				nil,
			)
			cancelDial()
			require.NoError(t, err)
			require.NotNil(t, conn)
			defer func() { _ = conn.CloseNow() }()
			require.NotNil(t, response)
			require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)

			if !tc.enabled {
				require.Empty(t, response.Header.Get("X-Codex-Primary-Reset-At"))
				require.Empty(t, response.Header.Get("X-Codex-Primary-Reset-After-Seconds"))
				require.Empty(t, response.Header.Get("X-Codex-Secondary-Reset-At"))
				require.Empty(t, response.Header.Get("X-Codex-Secondary-Reset-After-Seconds"))
				return
			}

			require.Equal(t, strconv.FormatInt(weeklyStart.Add(7*24*time.Hour).Unix(), 10), response.Header.Get("X-Codex-Primary-Reset-At"))
			require.Equal(t, strconv.FormatInt(fiveHourStart.Add(5*time.Hour).Unix(), 10), response.Header.Get("X-Codex-Secondary-Reset-At"))
			primaryResetAfter, err := strconv.ParseInt(response.Header.Get("X-Codex-Primary-Reset-After-Seconds"), 10, 64)
			require.NoError(t, err)
			require.InDelta(t, weeklyStart.Add(7*24*time.Hour).Unix()-now.Unix(), primaryResetAfter, 2)
			secondaryResetAfter, err := strconv.ParseInt(response.Header.Get("X-Codex-Secondary-Reset-After-Seconds"), 10, 64)
			require.NoError(t, err)
			require.InDelta(t, fiveHourStart.Add(5*time.Hour).Unix()-now.Unix(), secondaryResetAfter, 2)
		})
	}
}

func TestCodexLocalGroupQuotaUsageReturnsOnlyLocalQuota(t *testing.T) {
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	now := time.Now().UTC()
	group := &service.Group{
		ID:               8,
		Platform:         service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeSubscription,
		WeeklyLimitUSD:   &weeklyLimit,
		FiveHourLimitUSD: &fiveHourLimit,
	}
	apiKey := &service.APIKey{ID: 7, GroupID: &group.ID, Group: group}
	subscription := &service.UserSubscription{
		ID:                  9,
		UserID:              1,
		GroupID:             group.ID,
		WeeklyUsageUSD:      25,
		WeeklyWindowStart:   codexLocalQuotaTimePtr(now.Add(-time.Hour)),
		FiveHourUsageUSD:    5,
		FiveHourWindowStart: codexLocalQuotaTimePtr(now.Add(-time.Hour)),
	}

	recorder := performCodexLocalQuotaRequest(t, newCodexLocalQuotaHandler(true), apiKey, subscription)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store, private", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "Authorization", recorder.Header().Get("Vary"))

	var response service.CodexLocalGroupQuotaUsage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.RateLimit.Allowed)
	require.False(t, response.RateLimit.LimitReached)
	require.NotNil(t, response.RateLimit.PrimaryWindow)
	require.InDelta(t, 25, response.RateLimit.PrimaryWindow.UsedPercent, 0.001)
	require.Equal(t, int64(7*24*60*60), response.RateLimit.PrimaryWindow.LimitWindowSeconds)
	require.NotNil(t, response.RateLimit.SecondaryWindow)
	require.InDelta(t, 25, response.RateLimit.SecondaryWindow.UsedPercent, 0.001)
	require.Equal(t, int64(5*60*60), response.RateLimit.SecondaryWindow.LimitWindowSeconds)
}

func TestCodexLocalGroupQuotaUsageRejectsUnavailableLocalView(t *testing.T) {
	now := time.Now().UTC()
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	openAISubscriptionGroup := &service.Group{
		ID:               8,
		Platform:         service.PlatformOpenAI,
		SubscriptionType: service.SubscriptionTypeSubscription,
		WeeklyLimitUSD:   &weeklyLimit,
		FiveHourLimitUSD: &fiveHourLimit,
	}
	activeSubscription := &service.UserSubscription{
		ID:                  9,
		GroupID:             openAISubscriptionGroup.ID,
		WeeklyWindowStart:   codexLocalQuotaTimePtr(now.Add(-time.Hour)),
		FiveHourWindowStart: codexLocalQuotaTimePtr(now.Add(-time.Hour)),
	}

	cases := []struct {
		name         string
		enabled      bool
		group        *service.Group
		subscription *service.UserSubscription
	}{
		{
			name:         "switch disabled",
			enabled:      false,
			group:        openAISubscriptionGroup,
			subscription: activeSubscription,
		},
		{
			name:    "standard group",
			enabled: true,
			group: &service.Group{
				ID:               10,
				Platform:         service.PlatformOpenAI,
				SubscriptionType: service.SubscriptionTypeStandard,
			},
			subscription: activeSubscription,
		},
		{
			name:    "non OpenAI group",
			enabled: true,
			group: &service.Group{
				ID:               11,
				Platform:         service.PlatformAnthropic,
				SubscriptionType: service.SubscriptionTypeSubscription,
			},
			subscription: activeSubscription,
		},
		{
			name:         "missing subscription",
			enabled:      true,
			group:        openAISubscriptionGroup,
			subscription: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiKey := &service.APIKey{ID: 7, GroupID: &tc.group.ID, Group: tc.group}
			recorder := performCodexLocalQuotaRequest(t, newCodexLocalQuotaHandler(tc.enabled), apiKey, tc.subscription)
			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Contains(t, recorder.Body.String(), "not_found_error")
		})
	}
}
