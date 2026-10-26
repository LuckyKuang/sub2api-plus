//go:build unit || !integration

package service

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// compactProbeSSESuccessBody 是原生 v2 压缩成功的最小 SSE 形态：
// output_item.done 携带 compaction item + response.completed。
const compactProbeSSESuccessBody = "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"id\":\"cmp_probe\",\"encrypted_content\":\"blob\"}}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_probe\",\"output\":[]}}\n\n"

func TestAccountTestService_TestAccountConnection_OpenAICompactOAuthSuccessPersistsSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          1,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":               "oauth-token",
			"chatgpt_account_id":         "chatgpt-acc",
			"chatgpt_account_is_fedramp": true,
		},
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: "full",
			"openai_device_id":           "probe-owner-installation",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid-probe"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)

	// 原生 v2 探测走普通 /responses，不再调用 ChatGPT Codex OAuth 兼容
	// 上游在当前基线返回 404 的旧 unary 路由。
	require.Equal(t, chatgptCodexAPIURL, upstream.lastReq.URL.String())
	require.Equal(t, "chatgpt.com", upstream.lastReq.Host)
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Contains(t, upstream.lastReq.Header.Get("x-codex-beta-features"), "remote_compaction_v2")
	probeSessionID := upstream.lastReq.Header.Get("session-id")
	require.Equal(t, uuid.Version(7), uuid.MustParse(probeSessionID).Version())
	require.Equal(t, probeSessionID, upstream.lastReq.Header.Get("session-id"))
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Empty(t, upstream.lastReq.Header.Get("x-codex-installation-id"))
	require.Equal(t, "probe-owner-installation", gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String())
	require.NotEmpty(t, upstream.lastReq.Header.Get("x-codex-window-id"))
	require.NotEmpty(t, upstream.lastReq.Header.Get("thread-id"))
	require.Equal(t, upstream.lastReq.Header.Get("thread-id"), upstream.lastReq.Header.Get("x-client-request-id"))
	require.False(t, gjson.GetBytes(upstream.lastBody, "prompt_cache_key").Exists())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, DefaultOpenAICodexUserAgent, upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "chatgpt-acc", upstream.lastReq.Header.Get("chatgpt-account-id"))
	require.Equal(t, "true", upstream.lastReq.Header.Get("x-openai-fedramp"))
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	inputItems := gjson.GetBytes(upstream.lastBody, "input").Array()
	require.NotEmpty(t, inputItems)
	require.Equal(t, "compaction_trigger", inputItems[len(inputItems)-1].Get("type").String())

	updates := <-updateCalls
	require.Equal(t, true, updates["openai_compact_supported"])
	require.Equal(t, http.StatusOK, updates["openai_compact_last_status"])
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestAccountTestService_TestAccountConnection_OpenAICompactOAuthDeviceOmitsSessionAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          11,
		Name:        "openai-oauth-device",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/11/test", bytes.NewReader(nil))

	require.NoError(t, svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact))
	require.Equal(t, uuid.Version(7), uuid.MustParse(upstream.lastReq.Header.Get("session-id")).Version())
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Empty(t, upstream.lastReq.Header.Get("conversation_id"))
	<-updateCalls
}

func TestAccountTestService_TestAccountConnection_OpenAICompactSetupTokenSkipsFingerprint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          9,
		Name:        "openai-setup-token",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeSetupToken,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "setup-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: "full",
			"openai_device_id":           "must-not-apply",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/9/test", bytes.NewReader(nil))

	require.NoError(t, svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact))
	require.Equal(t, uuid.Version(7), uuid.MustParse(upstream.lastReq.Header.Get("session-id")).Version())
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Empty(t, upstream.lastReq.Header.Get("x-codex-installation-id"))
	<-updateCalls
}

func TestAccountTestService_TestAccountConnection_OpenAICompactOAuth404MarksUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          2,
		Name:        "openai-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`404 page not found`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/2/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.Error(t, err)

	updates := <-updateCalls
	require.Equal(t, false, updates["openai_compact_supported"])
	require.Equal(t, http.StatusNotFound, updates["openai_compact_last_status"])
	require.Contains(t, rec.Body.String(), `"type":"error"`)
}

func TestAccountTestService_TestAccountConnection_OpenAICompactAPIKeyUsesNativeResponsesPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          3,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://example.com/v1",
			// post-#5641：compact_model_mapping 仅作用于 legacy /responses/compact，
			// 原生 v2 探测不应用它。
			"compact_model_mapping": map[string]any{"gpt-5.4": "gpt-5.4-openai-compact"},
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/3/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)

	require.Equal(t, "https://example.com/v1/responses", upstream.lastReq.URL.String())
	requireOpenAIAPIKeyProbeHeaders(t, upstream.lastReq.Header)
	require.Contains(t, upstream.lastReq.Header.Get("x-codex-beta-features"), "remote_compaction_v2")
	require.Equal(t, "gpt-5.4", gjson.GetBytes(upstream.lastBody, "model").String(),
		"原生 v2 探测不应用 compact_model_mapping")
	updates := <-updateCalls
	require.Equal(t, true, updates["openai_compact_supported"])
}

func TestAccountTestService_TestAccountConnection_OpenAICompactAPIKeyDefaultBaseURLUsesResponsesPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          4,
		Name:        "openai-apikey-default",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "sk-test",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/4/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
	<-updateCalls
}

func TestAccountTestService_TestAccountConnection_OpenAICompact2xxWithoutItemMarksUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          5,
		Name:        "openai-oauth-no-item",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	// 200 但流里没有 compaction item：链路吞掉了 compaction_trigger 的形态
	//（#5478 的 "got 0 items"），必须判定为不支持。
	noItemBody := "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"msg_1\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_x\",\"output\":[]}}\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(noItemBody)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/5/test", bytes.NewReader(nil))

	err := svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact)
	require.Error(t, err)

	updates := <-updateCalls
	require.Equal(t, false, updates["openai_compact_supported"])
	require.Contains(t, rec.Body.String(), `"type":"error"`)
}

// 探测与真实转发走同一 /responses 端点：指纹层拥有 installation/thread，
// Plus 探测缓存身份最终拥有两个 session alias。
func TestAccountTestService_TestAccountConnection_OpenAICompactProbeComposesFingerprintAndCacheIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          6,
		Name:        "openai-oauth-identity",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		// 显式启用更强的 session 收敛，以验证探测身份与真实流量同构。
		Extra: map[string]any{"codex_fingerprint_mode": "session"},
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/6/test", bytes.NewReader(nil))

	require.NoError(t, svc.TestAccountConnection(c, account.ID, "gpt-5.4", "", AccountTestModeCompact))

	// Synthetic probe headers and metadata share one real session/thread/window.
	probeSessionID := upstream.lastReq.Header.Get("session-id")
	require.Equal(t, uuid.Version(7), uuid.MustParse(probeSessionID).Version())
	require.Equal(t, probeSessionID, upstream.lastReq.Header.Get("session-id"))
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, probeSessionID, gjson.GetBytes(upstream.lastBody, "client_metadata.session_id").String())
	require.Empty(t, upstream.lastReq.Header.Get("x-codex-installation-id"))
	require.Equal(t, uuid.Version(4), uuid.MustParse(gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String()).Version())
	wantThreadID := gjson.GetBytes(upstream.lastBody, "client_metadata.thread_id").String()
	require.Equal(t, uuid.Version(7), uuid.MustParse(wantThreadID).Version())
	require.Equal(t, wantThreadID, upstream.lastReq.Header.Get("thread-id"))
	require.Equal(t, wantThreadID, upstream.lastReq.Header.Get("x-client-request-id"))
	require.NotContains(t, upstream.lastReq.Header.Get("session-id"), "probe_compact",
		"探测标识不得是可被上游一眼识别的字面量")
	<-updateCalls
}

func TestAccountTestService_TestAccountConnection_OpenAICompactShadowUsesOwnerFingerprintMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	updateCalls := make(chan map[string]any, 1)
	owner := Account{
		ID:          7,
		Name:        "openai-oauth-owner",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
		Extra: map[string]any{CodexFingerprintModeExtraKey: "session"},
	}
	ownerID := owner.ID
	shadow := Account{
		ID:              8,
		Name:            "openai-oauth-shadow",
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		Status:          StatusActive,
		Schedulable:     true,
		Concurrency:     1,
		ParentAccountID: &ownerID,
		QuotaDimension:  QuotaDimensionSpark,
	}
	repo := &snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{owner, shadow}},
		updateExtraCalls:      updateCalls,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(compactProbeSSESuccessBody)),
	}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/8/test", bytes.NewReader(nil))

	require.NoError(t, svc.TestAccountConnection(c, shadow.ID, "gpt-5.4", "", AccountTestModeCompact))

	probeSessionID := upstream.lastReq.Header.Get("session-id")
	require.Equal(t, uuid.Version(7), uuid.MustParse(probeSessionID).Version())
	require.Equal(t, probeSessionID, upstream.lastReq.Header.Get("session-id"))
	require.Empty(t, upstream.lastReq.Header.Get("session_id"))
	require.Equal(t, probeSessionID, gjson.GetBytes(upstream.lastBody, "client_metadata.session_id").String())
	require.Empty(t, upstream.lastReq.Header.Get("x-codex-installation-id"))
	require.Equal(t, resolveConvergedInstallationID(&owner), gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String())
	wantThreadID := gjson.GetBytes(upstream.lastBody, "client_metadata.thread_id").String()
	require.Equal(t, uuid.Version(7), uuid.MustParse(wantThreadID).Version())
	require.Equal(t, wantThreadID, upstream.lastReq.Header.Get("thread-id"))
	require.Equal(t, wantThreadID, upstream.lastReq.Header.Get("x-client-request-id"))
	require.NotEqual(t, resolveConvergedInstallationID(&shadow), gjson.GetBytes(upstream.lastBody, "client_metadata.x-codex-installation-id").String())
	<-updateCalls
}

func TestCompactProbeIDsAreTypedStableAndOwnerScoped(t *testing.T) {
	a := turnStateAccount(776, "owner-a")
	first, err := resolveOpenAICompactProbeIDs(t.Context(), a)
	require.NoError(t, err)
	second, err := resolveOpenAICompactProbeIDs(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, first, second)
	other, err := resolveOpenAICompactProbeIDs(t.Context(), turnStateAccount(776, "owner-b"))
	require.NoError(t, err)
	require.NotEqual(t, first, other)
	require.Equal(t, uuid.Version(7), uuid.MustParse(first.session).Version())
	require.Equal(t, uuid.Version(7), uuid.MustParse(first.thread).Version())
	require.Equal(t, uuid.Version(4), uuid.MustParse(first.installation).Version())
	require.NotEqual(t, first.session, first.thread)
}
