//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newEnvironmentTimezoneTestAccount(tz string) *Account {
	extra := map[string]any{}
	if tz != "" {
		extra[CodexEnvironmentTimezoneExtraKey] = tz
	}
	return &Account{
		ID:       1,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    extra,
	}
}

func newEnvironmentTimezoneGlobalService(tz string) *OpenAIGatewayService {
	repo := &forwardedIPMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexEnvironmentTimezone: tz,
	}}
	return &OpenAIGatewayService{settingService: NewSettingService(repo, &config.Config{})}
}

func loadEnvironmentTimezoneForTest(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	return loc
}

func environmentTimezoneDate(t *testing.T, updated, tz string) string {
	t.Helper()
	loc := loadEnvironmentTimezoneForTest(t, tz)
	currentDate := time.Now().In(loc).Format("2006-01-02")
	require.Contains(t, updated, "<current_date>"+currentDate+"</current_date>")
	return currentDate
}

func TestResolveOpenAICodexEnvironmentTimezone(t *testing.T) {
	svc := &OpenAIGatewayService{}

	t.Run("nil 或非 Codex 协议账号关闭", func(t *testing.T) {
		require.Nil(t, resolveOpenAICodexEnvironmentTimezone(nil, nil, svc.settingService))
		apiKey := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{CodexEnvironmentTimezoneExtraKey: "America/New_York"}}
		require.Nil(t, resolveOpenAICodexEnvironmentTimezone(nil, apiKey, svc.settingService))
	})

	t.Run("账号级有效时区优先", func(t *testing.T) {
		loc := resolveOpenAICodexEnvironmentTimezone(nil, newEnvironmentTimezoneTestAccount("America/New_York"), svc.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "America/New_York", loc.String())
	})

	t.Run("未配置账号时回落全局", func(t *testing.T) {
		withGlobal := newEnvironmentTimezoneGlobalService("Europe/Berlin")
		loc := resolveOpenAICodexEnvironmentTimezone(nil, newEnvironmentTimezoneTestAccount(""), withGlobal.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "Europe/Berlin", loc.String())
	})

	t.Run("非法账号值回落全局", func(t *testing.T) {
		withGlobal := newEnvironmentTimezoneGlobalService("Europe/Berlin")
		invalid := newEnvironmentTimezoneTestAccount("Not/AZone")
		loc := resolveOpenAICodexEnvironmentTimezone(nil, invalid, withGlobal.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "Europe/Berlin", loc.String())
	})

	t.Run("两级都无效则关闭", func(t *testing.T) {
		require.Nil(t, resolveOpenAICodexEnvironmentTimezone(nil, newEnvironmentTimezoneTestAccount("Not/AZone"), svc.settingService))
	})
}

func TestRewriteOpenAICodexEnvironmentContextText(t *testing.T) {
	nyc := loadEnvironmentTimezoneForTest(t, "America/New_York")

	const block = "<environment_context>\n  <cwd>/repo</cwd>\n  <shell>bash</shell>\n  <current_date>2026-09-17</current_date>\n  <timezone>Asia/Shanghai</timezone>\n</environment_context>"

	t.Run("成对改写为一致值", func(t *testing.T) {
		updated, changed := rewriteOpenAICodexEnvironmentContextText(block, nyc)
		require.True(t, changed)
		require.Contains(t, updated, "<timezone>America/New_York</timezone>")
		require.Contains(t, updated, "<current_date>"+environmentTimezoneDate(t, updated, "America/New_York")+"</current_date>")
	})

	t.Run("幂等：重复改写结果不变", func(t *testing.T) {
		once, _ := rewriteOpenAICodexEnvironmentContextText(block, nyc)
		twice, changed := rewriteOpenAICodexEnvironmentContextText(once, nyc)
		require.False(t, changed)
		require.Equal(t, once, twice)
	})

	t.Run("缺 current_date 时注入成对值", func(t *testing.T) {
		onlyTZ := "<environment_context>\n  <timezone>Asia/Shanghai</timezone>\n</environment_context>"
		updated, changed := rewriteOpenAICodexEnvironmentContextText(onlyTZ, nyc)
		require.True(t, changed)
		require.Contains(t, updated, "<timezone>America/New_York</timezone>")
		require.Contains(t, updated, "<current_date>"+environmentTimezoneDate(t, updated, "America/New_York")+"</current_date>")
	})

	t.Run("缺 timezone 时注入成对值", func(t *testing.T) {
		onlyDate := "<environment_context><current_date>2026-09-17</current_date></environment_context>"
		updated, changed := rewriteOpenAICodexEnvironmentContextText(onlyDate, nyc)
		require.True(t, changed)
		require.Contains(t, updated, "<timezone>America/New_York</timezone>")
		require.Contains(t, updated, "<current_date>")
	})

	t.Run("块内无时间标签则整块不动", func(t *testing.T) {
		noTags := "<environment_context>\n  <cwd>/repo</cwd>\n</environment_context>"
		updated, changed := rewriteOpenAICodexEnvironmentContextText(noTags, nyc)
		require.False(t, changed)
		require.Equal(t, noTags, updated)
	})

	t.Run("非独立块不改写", func(t *testing.T) {
		mixed := "日志引用：<environment_context><timezone>Asia/Shanghai</timezone></environment_context> 之外还有正文"
		updated, changed := rewriteOpenAICodexEnvironmentContextText(mixed, nyc)
		require.False(t, changed)
		require.Equal(t, mixed, updated)
	})
}

func TestRewriteOpenAICodexEnvironmentContextBytes(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newEnvironmentTimezoneTestAccount("America/New_York")

	body := []byte(`{"model":"gpt-5.5","input":[` +
		`{"type":"message","role":"developer","content":"system"},` +
		`{"type":"message","role":"user","content":"<environment_context><current_date>2026-09-17</current_date><timezone>Asia/Shanghai</timezone></environment_context>"},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"},{"type":"input_text","text":"普通正文 <timezone>Asia/Shanghai</timezone> 不改写"}]}` +
		`]}`)

	updated := svc.rewriteOpenAICodexEnvironmentContextBytes(nil, account, body)
	out := gjson.ParseBytes(updated)

	stringContent := out.Get("input.1.content").String()
	require.Contains(t, stringContent, "<timezone>America/New_York</timezone>")
	require.Contains(t, stringContent, "<current_date>"+environmentTimezoneDate(t, stringContent, "America/New_York")+"</current_date>")

	partText := out.Get("input.2.content.0.text").String()
	require.Contains(t, partText, "<timezone>America/New_York</timezone>")

	plainText := out.Get("input.2.content.1.text").String()
	require.Contains(t, plainText, "<timezone>Asia/Shanghai</timezone>", "非独立块文本必须保持原样")

	developer := out.Get("input.0.content").String()
	require.Equal(t, "system", developer, "非 user 消息不改写")

	// Grok 等非 Codex 协议账号不参与。
	grok := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth, Extra: account.Extra}
	require.Equal(t, body, svc.rewriteOpenAICodexEnvironmentContextBytes(nil, grok, body))
}

func TestRewriteOpenAICodexEnvironmentContextMap(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newEnvironmentTimezoneTestAccount("America/New_York")

	payload := map[string]any{
		"type": "response.create",
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "<environment_context><current_date>2026-09-17</current_date><timezone>Asia/Shanghai</timezone></environment_context>"},
			map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"},
			}},
		},
	}
	updated := svc.rewriteOpenAICodexEnvironmentContextMap(nil, account, payload)

	first := updated["input"].([]any)[0].(map[string]any)["content"].(string)
	require.Contains(t, first, "<timezone>America/New_York</timezone>")
	second := updated["input"].([]any)[1].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	require.Contains(t, second, "<timezone>America/New_York</timezone>")

	// 未配置时原样返回（注意：map 改写会原地变更嵌套 content，必须用全新 payload）
	plain := newEnvironmentTimezoneTestAccount("")
	fresh := map[string]any{
		"type": "response.create",
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "<environment_context><timezone>Asia/Shanghai</timezone></environment_context>"},
		},
	}
	untouched := svc.rewriteOpenAICodexEnvironmentContextMap(nil, plain, fresh)
	firstUntouched := untouched["input"].([]any)[0].(map[string]any)["content"].(string)
	require.Contains(t, firstUntouched, "<timezone>Asia/Shanghai</timezone>")
}

func TestResolveOpenAICodexEnvironmentTimezoneProxyLayer(t *testing.T) {
	svc := &OpenAIGatewayService{}
	proxyNY := &Proxy{EgressTimezone: "America/New_York", EgressCountry: "US"}

	t.Run("代理标注生效", func(t *testing.T) {
		account := newEnvironmentTimezoneTestAccount("")
		account.Proxy = proxyNY
		loc := resolveOpenAICodexEnvironmentTimezone(nil, account, svc.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "America/New_York", loc.String())
	})

	t.Run("账号 extra 覆盖代理标注", func(t *testing.T) {
		account := newEnvironmentTimezoneTestAccount("Europe/Berlin")
		account.Proxy = proxyNY
		loc := resolveOpenAICodexEnvironmentTimezone(nil, account, svc.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "Europe/Berlin", loc.String())
	})

	t.Run("代理未标注跳过到全局", func(t *testing.T) {
		withGlobal := newEnvironmentTimezoneGlobalService("Europe/Berlin")
		account := newEnvironmentTimezoneTestAccount("")
		account.Proxy = &Proxy{EgressCountry: "US"}
		loc := resolveOpenAICodexEnvironmentTimezone(nil, account, withGlobal.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "Europe/Berlin", loc.String())
	})

	t.Run("Proxy 未加载跳过到全局", func(t *testing.T) {
		withGlobal := newEnvironmentTimezoneGlobalService("Europe/Berlin")
		account := newEnvironmentTimezoneTestAccount("")
		loc := resolveOpenAICodexEnvironmentTimezone(nil, account, withGlobal.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "Europe/Berlin", loc.String())
	})

	t.Run("代理标注非法跳过到全局", func(t *testing.T) {
		withGlobal := newEnvironmentTimezoneGlobalService("Europe/Berlin")
		account := newEnvironmentTimezoneTestAccount("")
		account.Proxy = &Proxy{EgressTimezone: "Not/AZone"}
		loc := resolveOpenAICodexEnvironmentTimezone(nil, account, withGlobal.settingService)
		require.NotNil(t, loc)
		require.Equal(t, "Europe/Berlin", loc.String())
	})

	t.Run("无任何来源则关闭", func(t *testing.T) {
		account := newEnvironmentTimezoneTestAccount("")
		require.Nil(t, resolveOpenAICodexEnvironmentTimezone(nil, account, svc.settingService))
	})
}

func TestNormalizeProxyTimezoneCountry(t *testing.T) {
	t.Run("两者为空合法", func(t *testing.T) {
		tz, country, err := normalizeProxyTimezoneCountry("", "")
		require.NoError(t, err)
		require.Empty(t, tz)
		require.Empty(t, country)
	})

	t.Run("合法值", func(t *testing.T) {
		tz, country, err := normalizeProxyTimezoneCountry(" America/New_York ", "us")
		require.NoError(t, err)
		require.Equal(t, "America/New_York", tz)
		require.Equal(t, "US", country)
	})

	t.Run("非法时区", func(t *testing.T) {
		_, _, err := normalizeProxyTimezoneCountry("Not/AZone", "")
		require.Error(t, err)
	})

	t.Run("非法国家代码", func(t *testing.T) {
		_, _, err := normalizeProxyTimezoneCountry("", "USA")
		require.Error(t, err)
	})
}
