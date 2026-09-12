package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWeeklyQuotaQueryObservesAndCachesRawWindow(t *testing.T) {
	account := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "weekly-test-account"}}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{100: account}}
	provider := NewOpenAITokenProvider(repo, &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "test-token"}}, nil)
	resetAt := time.Now().Add(6*24*time.Hour + 22*time.Hour).Unix()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/backend-api/wham/usage", r.URL.Path, "background refresh must not query or consume credits")
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		identity := resolveOpenAIOutboundIdentityFromSettings(context.Background(), account, nil)
		expected := make(http.Header)
		applyResolvedOpenAIOutboundIdentity(expected, identity, true)
		for _, key := range []string{"User-Agent", "Originator", "Version"} {
			require.Equal(t, expected.Get(key), r.Header.Get(key), key)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OpenAIQuotaUsage{RateLimit: &OpenAIRateLimit{SecondaryWindow: &OpenAIRateLimitWindow{
			UsedPercent: 7, LimitWindowSeconds: 604800, ResetAt: resetAt, ResetAfterSeconds: 1,
		}}})
	}))
	t.Cleanup(server.Close)
	service := NewOpenAIQuotaService(repo, nil, provider, newQuotaRedirectingFactory(server))
	recorder := &quotaFollowObservationRecorder{}
	observer := &OpenAIGroupQuotaFollowResetService{repo: recorder}
	setOpenAIGroupQuotaFollowResetObserver(observer)
	t.Cleanup(func() { clearOpenAIGroupQuotaFollowResetObserver(observer) })
	usage, err := service.queryUsage(context.Background(), account.ID, false)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, recorder.calls)
	require.Equal(t, time.Unix(resetAt, 0).UTC(), recorder.resetAt)
	require.Equal(t, 7.0, *recorder.observation.UsedPercent)
	require.True(t, recorder.observation.QuotaQuery)
	require.Equal(t, usage.weeklyObservedAt, recorder.observation.ObservedAt)
	require.Equal(t, usage.weeklyObservedAt.Format(time.RFC3339Nano), repo.extraUpdates[account.ID]["codex_usage_updated_at"], "cache ordering must retain subsecond sample precision")
	require.Equal(t, time.Unix(resetAt, 0).UTC().Format(time.RFC3339), repo.extraUpdates[account.ID]["codex_7d_reset_at"])
	// Post-reset cache handling must retain the same observation identity even
	// when the separate credit-details endpoint has failed.
	before := recorder.observation.ObservedAt
	require.Error(t, service.CachePostResetSnapshot(context.Background(), account.ID, usage))
	require.Equal(t, before, recorder.observation.ObservedAt)
}

func TestWeeklyQuotaSnapshotEntryPoints(t *testing.T) {
	now := time.Now().UTC()
	resetAt := now.Add(7 * 24 * time.Hour).Unix()
	for _, path := range []string{"gateway", "rate_limit", "account_probe", "post_reset", "auto_reset"} {
		t.Run(path, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			repo := &autoResetTestAccountRepo{account: account}
			recorder := &quotaFollowObservationRecorder{}
			observer := &OpenAIGroupQuotaFollowResetService{repo: recorder}
			setOpenAIGroupQuotaFollowResetObserver(observer)
			t.Cleanup(func() { clearOpenAIGroupQuotaFollowResetObserver(observer) })
			headers := make(http.Header)
			headers.Set("X-Codex-Secondary-Reset-At", strconv.FormatInt(resetAt, 10))
			headers.Set("X-Codex-Secondary-Window-Minutes", "10080")
			headers.Set("X-Codex-Secondary-Used-Percent", "7")
			usage := &OpenAIQuotaUsage{FetchedAt: now.Unix(), RateLimit: &OpenAIRateLimit{SecondaryWindow: &OpenAIRateLimitWindow{
				UsedPercent: 7, LimitWindowSeconds: 604800, ResetAt: resetAt,
			}}, RateLimitResetCredits: &OpenAIRateLimitResetCredits{}}
			switch path {
			case "gateway":
				(&OpenAIGatewayService{accountRepo: repo}).UpdateCodexUsageSnapshotFromHeaders(context.Background(), account.ID, headers)
			case "rate_limit":
				(&RateLimitService{accountRepo: repo}).persistOpenAICodexSnapshot(context.Background(), account, headers)
			case "account_probe":
				updates, err := (&AccountUsageService{accountRepo: repo}).recordOpenAICodexProbeResponse(context.Background(), account, &http.Response{StatusCode: 429, Header: headers}, now)
				require.NoError(t, err)
				require.Equal(t, 7.0, updates["codex_7d_used_percent"])
			case "post_reset":
				require.NoError(t, (&OpenAIQuotaService{accountRepo: repo}).CachePostResetSnapshot(context.Background(), account.ID, usage))
			case "auto_reset":
				require.NoError(t, (&OpenAIQuotaAutoResetService{accountRepo: repo, quota: &autoResetTestQuota{}}).persistFreshUsage(context.Background(), account.ID, usage, now))
			}
			require.Equal(t, 1, recorder.calls)
			require.Equal(t, time.Unix(resetAt, 0).UTC(), recorder.resetAt)
			require.Equal(t, 7.0, *recorder.observation.UsedPercent)
		})
	}
}

func TestWeeklyQuotaMissingUtilizationDoesNotInventZero(t *testing.T) {
	for _, value := range []string{"", `,"used_percent":null`} {
		var usage OpenAIQuotaUsage
		require.NoError(t, json.Unmarshal([]byte(`{"rate_limit":{"primary_window":{"limit_window_seconds":604800,"reset_at":1780000001`+value+`}}}`), &usage))
		snapshot := openAIQuotaUsageSnapshot(&usage, time.Now())
		require.Nil(t, snapshot.PrimaryUsedPercent)
		_, ok := snapshot.WeeklyResetAt()
		require.True(t, ok, "the valid raw deadline remains usable")
	}
	var usage OpenAIQuotaUsage
	require.NoError(t, json.Unmarshal([]byte(`{"rate_limit":{"primary_window":{"limit_window_seconds":604801,"reset_at":1780000001,"used_percent":0}}}`), &usage))
	_, ok := openAIQuotaUsageSnapshot(&usage, time.Now()).WeeklyResetAt()
	require.False(t, ok, "rounding a non-weekly window to minutes must not make it eligible")
}

func TestWeeklyQuotaDoesNotObserveSyntheticExpiredDisplay(t *testing.T) {
	recorder := &quotaFollowObservationRecorder{}
	observer := &OpenAIGroupQuotaFollowResetService{repo: recorder}
	setOpenAIGroupQuotaFollowResetObserver(observer)
	t.Cleanup(func() { clearOpenAIGroupQuotaFollowResetObserver(observer) })
	now := time.Now()
	progress := buildCodexUsageProgressFromExtra(map[string]any{
		"codex_7d_used_percent": 100,
		"codex_7d_reset_at":     now.Add(-time.Minute).Format(time.RFC3339),
	}, "7d", now)
	require.Zero(t, progress.Utilization)
	require.Zero(t, recorder.calls)
}
