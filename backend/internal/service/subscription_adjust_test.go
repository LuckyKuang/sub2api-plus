//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type adjustSubscriptionRepo struct {
	userSubRepoNoop

	sub            *UserSubscription
	extendCalls    int
	statusCalls    int
	windowCalls    int
	lastExpiry     time.Time
	lastStatus     string
	lastFiveHour   *time.Time
	lastDaily      *time.Time
	lastWeekly     *time.Time
	lastMonthly    *time.Time
}

func (r *adjustSubscriptionRepo) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	if r.sub == nil || r.sub.ID != id {
		return nil, ErrSubscriptionNotFound
	}
	cp := *r.sub
	return &cp, nil
}

func (r *adjustSubscriptionRepo) GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error) {
	return r.GetByID(ctx, id)
}

func (r *adjustSubscriptionRepo) ExtendExpiry(_ context.Context, id int64, expiresAt time.Time) error {
	if r.sub == nil || r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.extendCalls++
	r.lastExpiry = expiresAt
	r.sub.ExpiresAt = expiresAt
	return nil
}

func (r *adjustSubscriptionRepo) UpdateStatus(_ context.Context, id int64, status string) error {
	if r.sub == nil || r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.statusCalls++
	r.lastStatus = status
	r.sub.Status = status
	return nil
}

func (r *adjustSubscriptionRepo) UpdateQuotaWindowStarts(_ context.Context, id int64, fiveHour, daily, weekly, monthly *time.Time) error {
	if r.sub == nil || r.sub.ID != id {
		return ErrSubscriptionNotFound
	}
	r.windowCalls++
	r.lastFiveHour, r.lastDaily, r.lastWeekly, r.lastMonthly = fiveHour, daily, weekly, monthly
	if fiveHour != nil {
		r.sub.FiveHourWindowStart = fiveHour
	}
	if daily != nil {
		r.sub.DailyWindowStart = daily
	}
	if weekly != nil {
		r.sub.WeeklyWindowStart = weekly
	}
	if monthly != nil {
		r.sub.MonthlyWindowStart = monthly
	}
	return nil
}

func newAdjustSvc(repo *adjustSubscriptionRepo, now time.Time) *SubscriptionService {
	svc := NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil)
	svc.now = func() time.Time { return now }
	return svc
}

func useUTCTimezone(t *testing.T) {
	t.Helper()
	require.NoError(t, timezone.Init("UTC"))
	t.Cleanup(func() { _ = timezone.Init("Asia/Shanghai") })
}

func TestAdjustSubscription_RelativeDaysStillExtendsFromCurrentExpiry(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	expires := now.Add(10 * 24 * time.Hour)
	repo := &adjustSubscriptionRepo{sub: &UserSubscription{
		ID: 1, UserID: 9, GroupID: 4, Status: SubscriptionStatusActive, ExpiresAt: expires, DailyUsageUSD: 3.5,
	}}
	days := 7

	result, err := newAdjustSvc(repo, now).AdjustSubscription(context.Background(), 1, SubscriptionAdjustment{Days: &days})

	require.NoError(t, err)
	require.Equal(t, 1, repo.extendCalls)
	require.Zero(t, repo.windowCalls)
	require.Equal(t, expires.AddDate(0, 0, 7), repo.lastExpiry)
	require.Equal(t, float64(3.5), result.DailyUsageUSD)
}

func TestAdjustSubscription_AbsoluteExpirySetsExactTimestamp(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	target := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	repo := &adjustSubscriptionRepo{sub: &UserSubscription{
		ID: 2, UserID: 9, GroupID: 4, Status: SubscriptionStatusExpired, ExpiresAt: now.Add(-time.Hour),
	}}

	result, err := newAdjustSvc(repo, now).AdjustSubscription(context.Background(), 2, SubscriptionAdjustment{ExpiresAt: &target})

	require.NoError(t, err)
	require.Equal(t, 1, repo.extendCalls)
	require.Equal(t, 1, repo.statusCalls)
	require.Equal(t, target, repo.lastExpiry)
	require.Equal(t, SubscriptionStatusActive, repo.lastStatus)
	require.Equal(t, target, result.ExpiresAt)
}

func TestAdjustSubscription_WindowResetPreservesUsage(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	useUTCTimezone(t)
	weeklyReset := now.Add(3 * 24 * time.Hour)
	fiveHourReset := now.Add(2 * time.Hour)
	dailyReset := timezone.StartOfDay(now).AddDate(0, 0, 2)
	oldWeekly := now.Add(-24 * time.Hour)
	repo := &adjustSubscriptionRepo{sub: &UserSubscription{
		ID: 3, UserID: 9, GroupID: 4, Status: SubscriptionStatusActive,
		StartsAt: now.Add(-10 * 24 * time.Hour), ExpiresAt: now.Add(40 * 24 * time.Hour),
		WeeklyUsageUSD: 12, FiveHourUsageUSD: 4, DailyUsageUSD: 2,
		WeeklyWindowStart: &oldWeekly,
	}}

	result, err := newAdjustSvc(repo, now).AdjustSubscription(context.Background(), 3, SubscriptionAdjustment{
		WeeklyResetAt:   &weeklyReset,
		FiveHourResetAt: &fiveHourReset,
		DailyResetAt:    &dailyReset,
	})

	require.NoError(t, err)
	require.Zero(t, repo.extendCalls)
	require.Equal(t, 1, repo.windowCalls)
	require.Equal(t, weeklyReset.Add(-7*24*time.Hour), *repo.lastWeekly)
	require.Equal(t, fiveHourReset.Add(-subscriptionFiveHourDuration), *repo.lastFiveHour)
	require.Equal(t, dailyReset.AddDate(0, 0, -1), *repo.lastDaily)
	require.Equal(t, float64(12), result.WeeklyUsageUSD)
	require.Equal(t, float64(4), result.FiveHourUsageUSD)
	require.Equal(t, float64(2), result.DailyUsageUSD)
}

func TestAdjustSubscription_RejectsPastWindowResetAndNonMidnightDaily(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	useUTCTimezone(t)
	repo := &adjustSubscriptionRepo{sub: &UserSubscription{
		ID: 4, UserID: 9, GroupID: 4, Status: SubscriptionStatusActive, ExpiresAt: now.Add(10 * 24 * time.Hour),
	}}
	svc := newAdjustSvc(repo, now)

	past := now.Add(-time.Minute)
	_, err := svc.AdjustSubscription(context.Background(), 4, SubscriptionAdjustment{WeeklyResetAt: &past})
	require.ErrorIs(t, err, ErrWindowResetNotInFuture)

	notMidnight := timezone.StartOfDay(now).AddDate(0, 0, 1).Add(15 * time.Hour)
	_, err = svc.AdjustSubscription(context.Background(), 4, SubscriptionAdjustment{DailyResetAt: &notMidnight})
	require.ErrorIs(t, err, ErrDailyResetNotMidnight)
	require.Zero(t, repo.windowCalls)
}

func TestAdjustSubscription_RejectsDaysAndExpiresAtTogether(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	days := 7
	expires := now.Add(24 * time.Hour)
	_, err := newAdjustSvc(&adjustSubscriptionRepo{sub: &UserSubscription{ID: 5, ExpiresAt: now.Add(48 * time.Hour)}}, now).
		AdjustSubscription(context.Background(), 5, SubscriptionAdjustment{Days: &days, ExpiresAt: &expires})
	require.ErrorIs(t, err, ErrAdjustmentConflict)
}

func TestBulkSubscriptionAction_FiveHourOnlyResetIsValid(t *testing.T) {
	input := BulkSubscriptionActionInput{SubscriptionIDs: []int64{1}, Action: "reset_quota", FiveHour: true}
	require.NoError(t, input.Validate())
}
