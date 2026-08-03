package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionUsageLimitError_FiveHourWindow(t *testing.T) {
	limit := 10.0
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)
	group := &Group{FiveHourLimitUSD: &limit}

	err := subscriptionUsageLimitError(group, subscriptionUsageSnapshot{
		FiveHourUsage:       limit,
		FiveHourWindowStart: &start,
	}, now)

	require.ErrorIs(t, err, ErrFiveHourLimitExceeded)
	appErr := pkgerrors.FromError(err)
	require.NotNil(t, appErr)
	require.Equal(t, start.Add(subscriptionFiveHourDuration).Format(time.RFC3339), appErr.Metadata["window_resets_at"])
}

func TestSubscriptionUsageLimitError_FiveHourExpiredOrUnstartedDoesNotReject(t *testing.T) {
	limit := 10.0
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	expiredStart := now.Add(-subscriptionFiveHourDuration - time.Second)
	group := &Group{FiveHourLimitUSD: &limit}

	for _, start := range []*time.Time{nil, &expiredStart} {
		err := subscriptionUsageLimitError(group, subscriptionUsageSnapshot{
			FiveHourUsage:       limit + 1,
			FiveHourWindowStart: start,
		}, now)
		require.NoError(t, err)
	}
}

func TestSubscriptionUsageLimitError_MultipleWindowsUsesCombinedReasonAndLatestReset(t *testing.T) {
	limit := 10.0
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	weeklyStart := now.Add(-time.Hour)
	fiveHourStart := now.Add(-4 * time.Hour)
	group := &Group{WeeklyLimitUSD: &limit, FiveHourLimitUSD: &limit}

	err := subscriptionUsageLimitError(group, subscriptionUsageSnapshot{
		WeeklyUsage:         limit,
		WeeklyWindowStart:   &weeklyStart,
		FiveHourUsage:       limit,
		FiveHourWindowStart: &fiveHourStart,
	}, now)

	require.True(t, errors.Is(err, ErrGroupSubscriptionLimitExceeded))
	appErr := pkgerrors.FromError(err)
	require.Equal(t, weeklyStart.Add(7*subscriptionDayDuration).Format(time.RFC3339), appErr.Metadata["window_resets_at"])
}

func TestBuildCodexLocalGroupQuotaUsage(t *testing.T) {
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	weeklyStart := now.Add(-24 * time.Hour)
	fiveHourStart := now.Add(-time.Hour)
	group := &Group{SubscriptionType: SubscriptionTypeSubscription, Platform: PlatformOpenAI, WeeklyLimitUSD: &weeklyLimit, FiveHourLimitUSD: &fiveHourLimit}
	sub := &UserSubscription{
		WeeklyUsageUSD:      25,
		WeeklyWindowStart:   &weeklyStart,
		FiveHourUsageUSD:    20,
		FiveHourWindowStart: &fiveHourStart,
	}

	quota := BuildCodexLocalGroupQuotaUsage(group, sub, now)
	require.NotNil(t, quota)
	require.False(t, quota.RateLimit.Allowed)
	require.True(t, quota.RateLimit.LimitReached)
	require.NotNil(t, quota.RateLimit.PrimaryWindow)
	require.InDelta(t, 25, quota.RateLimit.PrimaryWindow.UsedPercent, 0.001)
	require.Equal(t, int64(7*24*60*60), quota.RateLimit.PrimaryWindow.LimitWindowSeconds)
	require.Equal(t, weeklyStart.Add(7*24*time.Hour).Unix(), quota.RateLimit.PrimaryWindow.ResetAt)
	require.NotNil(t, quota.RateLimit.SecondaryWindow)
	require.InDelta(t, 100, quota.RateLimit.SecondaryWindow.UsedPercent, 0.001)
	require.Equal(t, int64(5*60*60), quota.RateLimit.SecondaryWindow.LimitWindowSeconds)
	require.Equal(t, fiveHourStart.Add(5*time.Hour).Unix(), quota.RateLimit.SecondaryWindow.ResetAt)
}

func TestApplyCodexLocalQuotaHeadersDoesNotExposeUpstreamValues(t *testing.T) {
	weeklyLimit := 100.0
	fiveHourLimit := 20.0
	now := time.Now()
	weeklyStart := now.Add(-24 * time.Hour)
	fiveHourStart := now.Add(-time.Hour)
	dst := http.Header{
		"X-Codex-Primary-Used-Percent":                 []string{"77"},
		"X-Codex-Primary-Reset-After-Seconds":          []string{"999"},
		"X-Codex-Primary-Reset-At":                     []string{"111"},
		"X-Codex-Secondary-Used-Percent":               []string{"88"},
		"X-Codex-Secondary-Reset-After-Seconds":        []string{"888"},
		"X-Codex-Secondary-Reset-At":                   []string{"222"},
		"X-Codex-Primary-Over-Secondary-Limit-Percent": []string{"66"},
	}
	group := &Group{WeeklyLimitUSD: &weeklyLimit, FiveHourLimitUSD: &fiveHourLimit}
	sub := &UserSubscription{
		WeeklyUsageUSD:      25,
		WeeklyWindowStart:   &weeklyStart,
		FiveHourUsageUSD:    5,
		FiveHourWindowStart: &fiveHourStart,
	}

	applyCodexLocalQuotaHeaders(dst, group, sub)

	require.Equal(t, "25", dst.Get("X-Codex-Primary-Used-Percent"))
	require.Equal(t, strconv.FormatInt(weeklyStart.Add(7*24*time.Hour).Unix(), 10), dst.Get("X-Codex-Primary-Reset-At"))
	require.NotEmpty(t, dst.Get("X-Codex-Primary-Reset-After-Seconds"))
	require.NotEqual(t, "999", dst.Get("X-Codex-Primary-Reset-After-Seconds"))
	require.Equal(t, "25", dst.Get("X-Codex-Secondary-Used-Percent"))
	require.Equal(t, "300", dst.Get("X-Codex-Secondary-Window-Minutes"))
	require.Equal(t, strconv.FormatInt(fiveHourStart.Add(5*time.Hour).Unix(), 10), dst.Get("X-Codex-Secondary-Reset-At"))
	require.NotEmpty(t, dst.Get("X-Codex-Secondary-Reset-After-Seconds"))
	require.NotEqual(t, "888", dst.Get("X-Codex-Secondary-Reset-After-Seconds"))
	require.Empty(t, dst.Get("X-Codex-Primary-Over-Secondary-Limit-Percent"))
}

type fiveHourResetRepo struct {
	userSubRepoNoop

	sub        *UserSubscription
	resetAt    time.Time
	resetCalls int
}

func (r *fiveHourResetRepo) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *fiveHourResetRepo) ResetFiveHourUsage(_ context.Context, id int64, start time.Time) error {
	if r.sub == nil || r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.resetCalls++
	r.resetAt = start
	r.sub.FiveHourUsageUSD = 0
	r.sub.FiveHourWindowStart = &start
	return nil
}

func TestAdminResetQuota_FiveHourResetsAtActionTime(t *testing.T) {
	oldStart := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	repo := &fiveHourResetRepo{
		sub: &UserSubscription{
			ID:                  42,
			UserID:              7,
			GroupID:             8,
			DailyUsageUSD:       1,
			WeeklyUsageUSD:      2,
			MonthlyUsageUSD:     3,
			FiveHourUsageUSD:    4,
			FiveHourWindowStart: &oldStart,
		},
	}
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	before := time.Now()

	result, err := svc.AdminResetQuota(context.Background(), repo.sub.ID, false, false, false, true)
	after := time.Now()

	require.NoError(t, err)
	require.Equal(t, 1, repo.resetCalls)
	require.WithinDuration(t, before, repo.resetAt, time.Second)
	require.WithinDuration(t, after, repo.resetAt, time.Second)
	require.Equal(t, float64(0), result.FiveHourUsageUSD)
	require.NotNil(t, result.FiveHourWindowStart)
	require.WithinDuration(t, repo.resetAt, *result.FiveHourWindowStart, 0)
	require.Equal(t, float64(1), result.DailyUsageUSD)
	require.Equal(t, float64(2), result.WeeklyUsageUSD)
	require.Equal(t, float64(3), result.MonthlyUsageUSD)
}
