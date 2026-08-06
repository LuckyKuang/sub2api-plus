package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateUsageAlertWebhookURL(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateUsageAlertWebhookURL(UsageAlertChannelWeCom, "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc"))
	require.Error(t, validateUsageAlertWebhookURL(UsageAlertChannelWeCom, "https://example.com/cgi-bin/webhook/send?key=abc"))
	require.NoError(t, validateUsageAlertWebhookURL(UsageAlertChannelFeishu, "https://open.feishu.cn/open-apis/bot/v2/hook/abc"))
	require.NoError(t, validateUsageAlertWebhookURL(UsageAlertChannelFeishu, "https://open.larksuite.com/open-apis/bot/v2/hook/abc"))
	require.Error(t, validateUsageAlertWebhookURL(UsageAlertChannelFeishu, "https://open.feishu.cn/hook/abc"))
	require.NoError(t, validateUsageAlertWebhookURL(UsageAlertChannelCustom, "https://hooks.example.com/usage"))
	require.Error(t, validateUsageAlertWebhookURL(UsageAlertChannelCustom, "http://hooks.example.com/usage"))
}

func TestNormalizeUsageAlertRuleThreshold(t *testing.T) {
	t.Parallel()
	now := time.Now()
	_, err := normalizeUsageAlertRule(UsageAlertRule{
		Enabled:          true,
		Channel:          UsageAlertChannelCustom,
		WebhookURL:       "https://hooks.example.com/x",
		Cron:             "0 * * * *",
		ThresholdEnabled: true,
		ThresholdPercent: 0,
	}, now, false)
	require.Error(t, err)

	ok, err := normalizeUsageAlertRule(UsageAlertRule{
		Enabled:          true,
		Channel:          UsageAlertChannelCustom,
		WebhookURL:       "https://hooks.example.com/x",
		Cron:             "0 * * * *",
		ThresholdEnabled: true,
		ThresholdPercent: 80,
	}, now, false)
	require.NoError(t, err)
	require.Equal(t, 80, ok.ThresholdPercent)
}

func TestNormalizeUsageAlertConfigMultipleRules(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cfg, err := normalizeUsageAlertConfig(UsageAlertConfig{Rules: []UsageAlertRule{
		{
			Enabled:    true,
			Channel:    UsageAlertChannelWeCom,
			WebhookURL: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=a",
			Cron:       "0 * * * *",
		},
		{
			Enabled:          true,
			Channel:          UsageAlertChannelFeishu,
			WebhookURL:       "https://open.feishu.cn/open-apis/bot/v2/hook/b",
			Cron:             "30 * * * *",
			ThresholdEnabled: true,
			ThresholdPercent: 90,
		},
	}}, UsageAlertConfig{}, now)
	require.NoError(t, err)
	require.Len(t, cfg.Rules, 2)
	require.NotEmpty(t, cfg.Rules[0].ID)
	require.NotEmpty(t, cfg.Rules[1].ID)
	require.NotNil(t, cfg.Rules[0].NextRunAt)
}

func TestMaxUsageUtilization(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0.0, maxUsageUtilization(nil))
	require.Equal(t, 97.0, maxUsageUtilization(&UsageInfo{
		FiveHour: &UsageProgress{Utilization: 12},
		SevenDay: &UsageProgress{Utilization: 97},
	}))
}

func TestFormatUsageAlertMarkdownThresholdTitle(t *testing.T) {
	t.Parallel()
	account := &Account{ID: 7, Name: "demo", Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	resetAt := time.Date(2026, 8, 6, 19, 0, 0, 0, time.UTC)
	msg := buildUsageAlertMessage(account, &UsageInfo{
		FiveHour: &UsageProgress{
			Utilization: 88,
			ResetsAt:    &resetAt,
			WindowStats: &WindowStats{Requests: 12, Tokens: 3400, Cost: 1.2, UserCost: 1.5},
		},
		SevenDay: &UsageProgress{Utilization: 45, ResetsAt: &resetAt},
	}, time.Date(2026, 8, 6, 14, 0, 0, 0, time.UTC), "用量阈值告警（≥80%）", true, 80, 88)

	require.Contains(t, msg.WeCom, "用量阈值告警")
	require.Contains(t, msg.WeCom, "告警阈值：≥80%")
	require.NotContains(t, msg.WeCom, "最高使用率")
	require.Contains(t, msg.WeCom, "上游限流使用率")
	require.Contains(t, msg.WeCom, "5小时")
	require.Contains(t, msg.WeCom, "88%")
	require.Contains(t, msg.WeCom, "本站窗口统计")
	require.Contains(t, msg.WeCom, "核算成本")
	require.NotContains(t, msg.WeCom, "A $")

	require.Contains(t, msg.Markdown, "| 窗口 | 含义 | 使用率 | 重置时间 |")
	require.Contains(t, msg.Plain, "使用率：上游限流配额已用比例")
	require.NotContains(t, msg.Plain, "最高使用率")
}

func TestUsageAlertConfigMigratesLegacyWeCom(t *testing.T) {
	t.Parallel()
	account := &Account{Extra: map[string]any{
		WeComUsageAlertEnabledExtraKey:    true,
		WeComUsageAlertWebhookURLExtraKey: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x",
		WeComUsageAlertCronExtraKey:       "0 * * * *",
	}}
	cfg := UsageAlertConfigFromAccount(account)
	require.Len(t, cfg.Rules, 1)
	require.Equal(t, UsageAlertChannelWeCom, cfg.Rules[0].Channel)
	require.True(t, cfg.Rules[0].Enabled)
}

func TestPostWebhookWeComAndCustom(t *testing.T) {
	t.Parallel()
	var sawWeCom, sawCustom, sawFeishu bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = r.Body.Close()
		s := string(body)
		if strings.Contains(s, `"msgtype"`) {
			sawWeCom = true
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
			return
		}
		if strings.Contains(s, `"msg_type":"post"`) || strings.Contains(s, `"msg_type": "post"`) {
			sawFeishu = true
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
			return
		}
		if strings.Contains(s, `"markdown"`) && strings.Contains(s, `"account_id"`) {
			sawCustom = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`ok`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	svc := NewUsageAlertService(nil, nil, nil)
	svc.httpClient = server.Client()
	account := &Account{ID: 1, Name: "a", Platform: "openai", Type: "oauth"}
	msg := usageAlertMessage{WeCom: "**hi**", Markdown: "# hi", Plain: "hi"}
	require.NoError(t, svc.postWebhook(t.Context(), UsageAlertChannelWeCom, server.URL, "t", msg, account, nil, UsageAlertRule{}, 0))
	require.NoError(t, svc.postWebhook(t.Context(), UsageAlertChannelFeishu, server.URL, "t", msg, account, nil, UsageAlertRule{}, 0))
	require.NoError(t, svc.postWebhook(t.Context(), UsageAlertChannelCustom, server.URL, "t", msg, account, nil, UsageAlertRule{}, 12))
	require.True(t, sawWeCom)
	require.True(t, sawFeishu)
	require.True(t, sawCustom)
}
