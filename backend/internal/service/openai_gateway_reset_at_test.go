package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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
