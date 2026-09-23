//go:build unit

package service

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 官方 api_bridge.rs 把 insufficient_quota / credit_balance_exhausted /
// *_spend_limit_exceeded / usage_not_included 视为终态：账号预算已用尽，同账号
// 重试与窗口内换号都是无收益请求。
func TestClassifyOpenAIOAuth429_TerminalQuotaErrorTypes(t *testing.T) {
	bodies := map[string]string{
		"insufficient_quota":       `{"error":{"type":"insufficient_quota","message":"You exceeded your current quota"}}`,
		"credit_balance_exhausted": `{"error":{"type":"credit_balance_exhausted","message":"no credits"}}`,
		"usage_not_included":       `{"error":{"type":"usage_not_included","message":"plan does not include this"}}`,
		"daily spend limit":        `{"error":{"type":"daily_spend_limit_exceeded","message":"spend limit"}}`,
		"code spelling":            `{"error":{"code":"insufficient_quota","message":"no quota"}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			disposition, resetAt := classifyOpenAIOAuth429(http.Header{}, []byte(body))
			require.Equal(t, openAIOAuth429QuotaExhausted, disposition)
			require.Nil(t, resetAt, "terminal quota errors carry no window reset")
		})
	}

	// A transient 429 keeps the same-account retry window.
	disposition, _ := classifyOpenAIOAuth429(http.Header{}, []byte(`{"error":{"type":"rate_limit_exceeded","message":"slow down"}}`))
	require.Equal(t, openAIOAuth429Transient, disposition)
}

// A terminal quota body must not open a same-account retry window, while an
// ordinary transient 429 still does.
func TestShouldRetryOpenAIOAuth429_TerminalQuotaNeverRetries(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 90, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}

	terminal := []byte(`{"error":{"type":"insufficient_quota","message":"no quota"}}`)
	require.False(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, http.StatusTooManyRequests, false, http.Header{}, terminal),
		"a terminal quota error must not retry the same account")
	require.False(t, svc.ShouldRetryOpenAIOAuth429(account, http.Header{}, terminal),
		"a terminal quota error must not be deferred by the rate-limit service")

	transient := []byte(`{"error":{"type":"rate_limit_exceeded","message":"slow down"}}`)
	require.True(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, http.StatusTooManyRequests, false, http.Header{}, transient),
		"an ordinary transient 429 still uses the same-account retry window")
}

// 官方在 429 上用 x-codex-active-limit 指明命中的计量族；非默认族耗尽时不能只看
// 默认 codex 族的 5h/7d 窗口。
func TestClassifyOpenAIOAuth429_ActiveLimitFamilyDrivesDisposition(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-active-limit", "codex_other")
	// The default family looks healthy...
	headers.Set("x-codex-primary-used-percent", "10")
	headers.Set("x-codex-primary-window-minutes", "300")
	headers.Set("x-codex-secondary-used-percent", "20")
	headers.Set("x-codex-secondary-window-minutes", "10080")
	// ...while the named family is exhausted.
	headers.Set("x-codex-other-primary-used-percent", "100")
	headers.Set("x-codex-other-primary-window-minutes", "300")
	headers.Set("x-codex-other-primary-reset-at", "1790000000")

	disposition, resetAt := classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Quota5h, disposition,
		"the named active family must decide, not the healthy default family")
	require.NotNil(t, resetAt)
	require.Equal(t, time.Unix(1790000000, 0).UTC(), resetAt.UTC())
}

func TestClassifyOpenAIOAuth429_ActiveLimitSecondaryWindow(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-active-limit", "codex-secondary")
	headers.Set("x-codex-secondary-primary-used-percent", "100")
	headers.Set("x-codex-secondary-primary-window-minutes", "1440")
	headers.Set("x-codex-secondary-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-secondary-window-minutes", "10080")
	headers.Set("x-codex-secondary-secondary-reset-at", "1790000600")

	disposition, resetAt := classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Quota7d, disposition)
	require.NotNil(t, resetAt)
	require.Equal(t, time.Unix(1790000600, 0).UTC(), resetAt.UTC())
}

// Without the header the default family keeps deciding, so nothing regresses.
func TestClassifyOpenAIOAuth429_DefaultFamilyStillApplies(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-window-minutes", "300")
	headers.Set("x-codex-other-primary-used-percent", "42")

	disposition, _ := classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Quota5h, disposition)
}

// An active-limit header naming the default family, or an unknown family, must
// not change the default path.
func TestClassifyOpenAIOAuth429_ActiveLimitDefaultsAndUnknown(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-codex-active-limit", "codex")
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-window-minutes", "300")
	disposition, _ := classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Quota5h, disposition)

	unknown := http.Header{}
	unknown.Set("x-codex-active-limit", "codex_mystery")
	unknown.Set("x-codex-primary-used-percent", "100")
	unknown.Set("x-codex-primary-window-minutes", "300")
	disposition, _ = classifyOpenAIOAuth429(unknown, nil)
	require.Equal(t, openAIOAuth429Quota5h, disposition)
}

func TestIsOpenAITerminalQuotaErrorType(t *testing.T) {
	for _, value := range []string{
		"insufficient_quota", "Insufficient_Quota", " credit_balance_exhausted ",
		"usage_not_included", "spend_limit_exceeded", "daily_spend_limit_exceeded",
		"monthly-spend-limit-exceeded",
	} {
		require.True(t, isOpenAITerminalQuotaErrorType(value), "%q must be terminal", value)
	}
	for _, value := range []string{
		"", "rate_limit_exceeded", "usage_limit_reached", "invalid_request_error",
		"quota", "spend_limit",
	} {
		require.False(t, isOpenAITerminalQuotaErrorType(value), "%q must not be terminal", value)
	}
}

// A malformed or absurdly large family reset timestamp must never reach
// BlockAccountScheduling: it only clamps past/zero times, and the Spark
// model-level path persists the value, so one bad header could park an account
// or model across restarts.
func TestClassifyOpenAIOAuth429_FamilyResetBounds(t *testing.T) {
	build := func(resetAt string) http.Header {
		headers := http.Header{}
		headers.Set("x-codex-active-limit", "codex_other")
		headers.Set("x-codex-other-primary-used-percent", "100")
		headers.Set("x-codex-other-primary-window-minutes", "300")
		headers.Set("x-codex-other-primary-reset-at", resetAt)
		return headers
	}

	// In-range timestamps keep driving the disposition.
	for _, resetAt := range []string{"1790000000", "253402300799"} {
		disposition, at := classifyOpenAIOAuth429(build(resetAt), nil)
		require.Equal(t, openAIOAuth429Quota5h, disposition)
		require.NotNil(t, at, "%s must produce a reset time", resetAt)
	}

	// Out-of-range values fall back to a window-only quota decision.
	for _, resetAt := range []string{"0", "-1", "9223372036854775807", "253402300800"} {
		disposition, at := classifyOpenAIOAuth429(build(resetAt), nil)
		require.Equal(t, openAIOAuth429Quota5h, disposition)
		require.Nil(t, at, "%s must not produce a reset time", resetAt)
	}
}

// 上游并不保证 primary 是长窗。窗口朝向必须像默认族 Normalize() 那样按
// window-minutes 推导，否则两个窗口会被贴反。
func TestClassifyOpenAIOAuth429_FamilyWindowOrientation(t *testing.T) {
	cases := []struct {
		name           string
		primaryMinutes *int
		want           openAIOAuth429Disposition
	}{
		{"primary 300min is the 5h window", ptrInt429Window(300), openAIOAuth429Quota5h},
		{"primary 360min is still 5h", ptrInt429Window(360), openAIOAuth429Quota5h},
		{"primary 1440min is the 7d bucket", ptrInt429Window(1440), openAIOAuth429Quota7d},
		{"primary 10080min is the 7d bucket", ptrInt429Window(10080), openAIOAuth429Quota7d},
		{"no window minutes falls back to primary=7d", nil, openAIOAuth429Quota7d},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			headers.Set("x-codex-active-limit", "codex_other")
			headers.Set("x-codex-other-primary-used-percent", "100")
			if tc.primaryMinutes != nil {
				headers.Set("x-codex-other-primary-window-minutes", strconv.Itoa(*tc.primaryMinutes))
			}
			disposition, _ := classifyOpenAIOAuth429(headers, nil)
			require.Equal(t, tc.want, disposition)
		})
	}

	// Official discovers a metered family only from a `-primary-used-percent`
	// header (rate_limits.rs header_name_to_limit_id), so a family that reports
	// only a secondary window is invisible on both sides and falls through to
	// the default-family path.
	headers := http.Header{}
	headers.Set("x-codex-active-limit", "codex_other")
	headers.Set("x-codex-other-secondary-used-percent", "100")
	disposition, _ := classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Transient, disposition,
		"a family without a primary-used-percent header is not discovered")

	// With both exhausted, the short window (the one the user feels first) wins.
	headers = http.Header{}
	headers.Set("x-codex-active-limit", "codex_other")
	headers.Set("x-codex-other-primary-used-percent", "100")
	headers.Set("x-codex-other-primary-window-minutes", "10080")
	headers.Set("x-codex-other-secondary-used-percent", "100")
	headers.Set("x-codex-other-secondary-window-minutes", "300")
	disposition, _ = classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Quota5h, disposition)
}

func ptrInt429Window(value int) *int {
	return &value
}
