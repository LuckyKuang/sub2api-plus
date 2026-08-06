package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateWeComWebhookURL(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateWeComWebhookURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc"))
	require.Error(t, validateWeComWebhookURL("http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc"))
	require.Error(t, validateWeComWebhookURL("https://example.com/cgi-bin/webhook/send?key=abc"))
	require.Error(t, validateWeComWebhookURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send"))
}

func TestNormalizeWeComUsageAlertConfigRequiresFieldsWhenEnabled(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	_, err := normalizeWeComUsageAlertConfig(WeComUsageAlertConfig{Enabled: true}, now)
	require.Error(t, err)

	cfg, err := normalizeWeComUsageAlertConfig(WeComUsageAlertConfig{
		Enabled:    true,
		WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc",
		Cron:       "0 * * * *",
	}, now)
	require.NoError(t, err)
	require.NotNil(t, cfg.NextRunAt)

	disabled, err := normalizeWeComUsageAlertConfig(WeComUsageAlertConfig{Enabled: false}, now)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
}

func TestFormatWeComUsageAlertMarkdownIncludesWindows(t *testing.T) {
	t.Parallel()
	reset := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	account := &Account{ID: 42, Name: "codex-main", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	usage := &UsageInfo{
		FiveHour: &UsageProgress{
			Utilization: 12,
			ResetsAt:    &reset,
			WindowStats: &WindowStats{Requests: 5, Tokens: 126400, Cost: 0.34, UserCost: 0.34},
		},
		SevenDay: &UsageProgress{
			Utilization: 97,
			ResetsAt:    &reset,
		},
	}

	md := formatWeComUsageAlertMarkdown(account, usage, time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC))
	require.Contains(t, md, "codex-main")
	require.Contains(t, md, "#42")
	require.Contains(t, md, "**5h**")
	require.Contains(t, md, "97%")
	require.Contains(t, md, "A $0.34")
}

func TestPostWeComMarkdownSuccess(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer server.Close()

	svc := NewWeComUsageAlertService(nil, nil, nil)
	svc.httpClient = server.Client()
	err := svc.postWeComMarkdown(t.Context(), server.URL, "hello")
	require.NoError(t, err)
}

func TestPostWeComMarkdownUpstreamErrCode(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer server.Close()

	svc := NewWeComUsageAlertService(nil, nil, nil)
	svc.httpClient = server.Client()
	err := svc.postWeComMarkdown(t.Context(), server.URL, "hello")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "93000"))
}
