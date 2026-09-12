package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaFollowObservationRecorder struct {
	accountID   int64
	resetAt     time.Time
	calls       int
	contextErr  error
	observation OpenAIWeeklyQuotaObservation
}

func (r *quotaFollowObservationRecorder) ObserveWeeklyReset(ctx context.Context, accountID int64, observation OpenAIWeeklyQuotaObservation) (int, error) {
	r.contextErr = ctx.Err()
	r.accountID = accountID
	r.resetAt = observation.ResetAt
	r.observation = observation
	r.calls++
	return 0, nil
}

func TestQuotaFollowResetObservationSurvivesDownstreamCancellation(t *testing.T) {
	recorder := &quotaFollowObservationRecorder{}
	observer := &OpenAIGroupQuotaFollowResetService{repo: recorder}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	observer.Observe(ctx, 42, OpenAIWeeklyQuotaObservation{ResetAt: time.Now().Add(7 * 24 * time.Hour), ObservedAt: time.Now()})
	require.Equal(t, 1, recorder.calls)
	require.NoError(t, recorder.contextErr)
}

func (r *quotaFollowObservationRecorder) ProcessNextPending(context.Context) (*GroupQuotaFollowResetResult, error) {
	return nil, nil
}

func (r *quotaFollowObservationRecorder) ClaimWeeklyResetSources(context.Context, int) ([]int64, error) {
	return nil, nil
}

func TestOpenAICodexUsageSnapshotWeeklyResetAt(t *testing.T) {
	weeklyReset := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC).Unix()
	fiveHourReset := weeklyReset - int64((6 * 24 * time.Hour).Seconds())
	weeklyWindow := 7 * 24 * 60
	fiveHourWindow := 5 * 60

	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryResetAtUnix:     &weeklyReset,
		PrimaryWindowMinutes:   &weeklyWindow,
		SecondaryResetAtUnix:   &fiveHourReset,
		SecondaryWindowMinutes: &fiveHourWindow,
	}
	got, ok := snapshot.WeeklyResetAt()
	require.True(t, ok)
	require.Equal(t, time.Unix(weeklyReset, 0).UTC(), got)

	snapshot.PrimaryWindowMinutes = &fiveHourWindow
	_, ok = snapshot.WeeklyResetAt()
	require.False(t, ok, "5-hour windows must never drive group resets")

	for _, window := range []int{24 * 60, 4 * 24 * 60, 30 * 24 * 60} {
		snapshot.PrimaryWindowMinutes = &window
		snapshot.PrimaryResetAtUnix = &weeklyReset
		_, ok = snapshot.WeeklyResetAt()
		require.False(t, ok, "non-weekly window %d must never drive group resets", window)
	}
}

func TestObserveOpenAIWeeklyResetEventUsesRawDefaultWindow(t *testing.T) {
	recorder := &quotaFollowObservationRecorder{}
	observer := &OpenAIGroupQuotaFollowResetService{repo: recorder}
	setOpenAIGroupQuotaFollowResetObserver(observer)
	t.Cleanup(func() { clearOpenAIGroupQuotaFollowResetObserver(observer) })

	resetAt := int64(1_780_000_001)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	observeOpenAIWeeklyResetEvent(
		context.Background(),
		account,
		[]byte(`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":10080,"reset_at":1780000001},"secondary":{"window_minutes":300,"reset_at":1780003601}}}`),
	)
	require.Equal(t, 1, recorder.calls)
	require.Equal(t, int64(42), recorder.accountID)
	require.Equal(t, time.Unix(resetAt, 0).UTC(), recorder.resetAt)

	observeOpenAIWeeklyResetEvent(
		context.Background(),
		account,
		[]byte(`{"type":"codex.rate_limits","metered_limit_name":"codex_bengalfox","rate_limits":{"primary":{"window_minutes":10080,"reset_at":1780600001}}}`),
	)
	require.Equal(t, 1, recorder.calls, "model-specific windows must not drive group resets")
	for _, payload := range []string{
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":10080,"reset_at":"1780600001"}}}`,
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":10080,"reset_at":1780600001.5}}}`,
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":10080,"reset_at":999999999999999999}}}`,
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":10080,"reset_at":1780600001}}`,
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":5760,"reset_at":1780600001}}}`,
		`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":43200,"reset_at":1780600001}}}`,
	} {
		observeOpenAIWeeklyResetEvent(context.Background(), account, []byte(payload))
	}
	require.Equal(t, 1, recorder.calls, "malformed raw timestamps cannot poison the monotonic baseline")
	observeOpenAIWeeklyResetEvent(
		context.Background(),
		account,
		[]byte(`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":1440,"reset_at":1780600001}}}`),
	)
	require.Equal(t, 1, recorder.calls, "daily windows must not drive group resets")
	validPayload := []byte(`{"type":"codex.rate_limits","rate_limits":{"primary":{"window_minutes":300,"reset_at":1780600001},"secondary":{"window_minutes":10080,"reset_at":1780700001}}}`)
	for _, ineligible := range []*Account{
		{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &account.ID},
	} {
		observeOpenAIWeeklyResetEvent(context.Background(), ineligible, validPayload)
	}
	require.Equal(t, 1, recorder.calls)
	observeOpenAIWeeklyResetEvent(context.Background(), account, validPayload)
	require.Equal(t, 2, recorder.calls)
	require.Equal(t, time.Unix(1780700001, 0).UTC(), recorder.resetAt)
}
