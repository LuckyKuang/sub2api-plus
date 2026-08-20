package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCodexFingerprintSeed = "11111111-1111-4111-8111-111111111111"

func newTestOAuthAccount(id int64, extra map[string]any) *Account {
	if codexFingerprintModeRequiresSeed(codexFingerprintModeFromExtra(extra)) {
		if extra == nil {
			extra = make(map[string]any)
		}
		if _, exists := extra[codexFingerprintSeedExtraKey]; !exists {
			extra[codexFingerprintSeedExtraKey] = testCodexFingerprintSeed
		}
	}
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    extra,
	}
}

// --- deriveStableUUIDv4 ---

func TestDeriveStableUUIDv4_Deterministic(t *testing.T) {
	a := deriveStableUUIDv4("test-seed-1")
	b := deriveStableUUIDv4("test-seed-1")
	assert.Equal(t, a, b, "同一种子应返回相同结果")
}

func TestDeriveStableUUIDv4_DifferentSeeds(t *testing.T) {
	a := deriveStableUUIDv4("seed-a")
	b := deriveStableUUIDv4("seed-b")
	assert.NotEqual(t, a, b, "不同种子应返回不同结果")
}

func TestDeriveStableUUIDv4_ValidFormat(t *testing.T) {
	result := deriveStableUUIDv4("test-seed")
	parsed, err := uuid.Parse(result)
	require.NoError(t, err, "应返回合法 UUID 格式")
	assert.Equal(t, uuid.Version(4), parsed.Version(), "应为 UUIDv4")
	assert.Equal(t, uuid.RFC4122, parsed.Variant(), "应为 RFC4122 变体")
}

// --- GetCodexFingerprintMode ---

func TestGetCodexFingerprintMode(t *testing.T) {
	tests := []struct {
		name     string
		account  *Account
		expected codexFingerprintMode
	}{
		{"nil 账号", nil, codexFingerprintOff},
		{"非 OAuth 账号", &Account{Platform: PlatformOpenAI, Type: "api_key"}, codexFingerprintOff},
		{"无 extra 默认 session", newTestOAuthAccount(1, nil), codexFingerprintSession},
		{"空值默认 session", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: ""}), codexFingerprintSession},
		{"空白值默认 session", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "  "}), codexFingerprintSession},
		{"非法值默认 session", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "invalid"}), codexFingerprintSession},
		{"显式 off", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "off"}), codexFingerprintOff},
		{"device", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "device"}), codexFingerprintDevice},
		{"session", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "session"}), codexFingerprintSession},
		{"full", newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "full"}), codexFingerprintFull},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.account.GetCodexFingerprintMode())
		})
	}
}

// --- resolveConvergedInstallationID ---

func TestResolveConvergedInstallationID_UsesDeviceID(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{"openai_device_id": "real-device-id"})
	assert.Equal(t, "real-device-id", resolveConvergedInstallationID(account, testCodexFingerprintSeed))
}

func TestResolveConvergedInstallationID_DerivesFromSeed(t *testing.T) {
	account := newTestOAuthAccount(42, nil)
	result := resolveConvergedInstallationID(account, testCodexFingerprintSeed)
	_, err := uuid.Parse(result)
	require.NoError(t, err, "派生值应为合法 UUID")
	assert.Equal(t, result, resolveConvergedInstallationID(account, testCodexFingerprintSeed), "确定性")
}

func TestResolveConvergedInstallationID_DifferentSeeds(t *testing.T) {
	account := newTestOAuthAccount(1, nil)
	a := resolveConvergedInstallationID(account, testCodexFingerprintSeed)
	b := resolveConvergedInstallationID(account, "22222222-2222-4222-8222-222222222222")
	assert.NotEqual(t, a, b)
}

// --- resolveConvergedThreadID ---

func TestResolveConvergedThreadID_PerClientSession(t *testing.T) {
	a := resolveConvergedThreadID(testCodexFingerprintSeed, "session-aaa")
	b := resolveConvergedThreadID(testCodexFingerprintSeed, "session-bbb")
	assert.NotEqual(t, a, b, "不同客户端 session 应得到不同 thread_id")
}

func TestResolveConvergedThreadID_Deterministic(t *testing.T) {
	a := resolveConvergedThreadID(testCodexFingerprintSeed, "session-aaa")
	b := resolveConvergedThreadID(testCodexFingerprintSeed, "session-aaa")
	assert.Equal(t, a, b, "同一客户端 session 应得到相同 thread_id")
}

func TestResolveConvergedThreadID_EmptySession(t *testing.T) {
	assert.Equal(t, "", resolveConvergedThreadID(testCodexFingerprintSeed, ""))
}

// --- off 模式：resolveCodexFingerprintIDsFromRequest 返回 nil ---

func TestResolveCodexFingerprintIDsFromRequest_ExplicitOff(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "off"})
	ids := resolveCodexFingerprintIDsFromRequest(account, nil)
	assert.Nil(t, ids, "显式 off 模式应返回 nil")
}

func TestResolveCodexFingerprintIDsFromRequest_DefaultIsSession(t *testing.T) {
	account := newTestOAuthAccount(1, nil)
	ids := resolveCodexFingerprintIDsFromRequest(account, nil)
	require.NotNil(t, ids)
	assert.Equal(t, codexFingerprintSession, ids.mode)
}

// 管理员显式 opt-in 的账号行为不变。
func TestResolveCodexFingerprintIDsFromRequest_ExplicitOptInHonored(t *testing.T) {
	for _, mode := range []string{"device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			account := newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: mode})
			ids := resolveCodexFingerprintIDsFromRequest(account, nil)
			require.NotNil(t, ids, "显式配置必须生效")
			assert.Equal(t, codexFingerprintMode(mode), ids.mode)
			assert.NotEmpty(t, ids.installationID)
		})
	}
}

func TestResolveCodexFingerprintIDsFromRequest_EnabledModesRequireValidSeed(t *testing.T) {
	for _, tt := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "missing", extra: map[string]any{codexFingerprintModeExtraKey: "device"}},
		{name: "missing with device override", extra: map[string]any{codexFingerprintModeExtraKey: "device", "openai_device_id": "real-device"}},
		{name: "blank", extra: map[string]any{codexFingerprintModeExtraKey: "session", codexFingerprintSeedExtraKey: ""}},
		{name: "uppercase", extra: map[string]any{codexFingerprintModeExtraKey: "full", codexFingerprintSeedExtraKey: "11111111-1111-4111-8111-AAAAAAAAAAAA"}},
		{name: "nil uuid", extra: map[string]any{codexFingerprintModeExtraKey: "device", codexFingerprintSeedExtraKey: "00000000-0000-0000-0000-000000000000"}},
		{name: "non string", extra: map[string]any{codexFingerprintModeExtraKey: "session", codexFingerprintSeedExtraKey: 123}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: tt.extra}
			require.Nil(t, resolveCodexFingerprintIDsFromRequest(account, nil))
		})
	}
}

// --- applyCodexFingerprintHeaders: off 模式 ---

func TestApplyCodexFingerprintHeaders_OffMode(t *testing.T) {
	h := http.Header{}
	h.Set("x-codex-installation-id", "original-install-id")
	h.Set("x-codex-window-id", "original-window-id")

	applyCodexFingerprintHeaders(h, nil)

	assert.Equal(t, "original-install-id", h.Get("x-codex-installation-id"), "nil ids 不改写")
	assert.Equal(t, "original-window-id", h.Get("x-codex-window-id"), "nil ids 不改写")
}

// --- applyCodexFingerprintHeaders: device 模式 ---

func TestApplyCodexFingerprintHeaders_DeviceMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "device",
		"openai_device_id":           "converged-device",
	})
	turnMetadata := `{"installation_id":"user-install","session_id":"user-session","sandbox":"seccomp"}`
	h := http.Header{}
	h.Set("x-codex-installation-id", "user-install")
	h.Set("x-codex-window-id", "user-window:0")
	h.Set("x-codex-turn-metadata", turnMetadata)

	ids := resolveCodexFingerprintIDsFromRequest(account, nil)
	applyCodexFingerprintHeaders(h, ids)

	assert.Equal(t, "converged-device", h.Get("x-codex-installation-id"), "installation_id 应收敛")
	assert.Equal(t, "user-window:0", h.Get("x-codex-window-id"), "device 模式不改写 window_id")

	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &meta))
	assert.Equal(t, "converged-device", meta["installation_id"])
	assert.Equal(t, "user-session", meta["session_id"], "device 模式不改写 session_id")
	assert.Equal(t, "seccomp", meta["sandbox"], "非指纹字段保留原样")
}

// --- applyCodexFingerprintHeaders: session 模式 ---

func TestApplyCodexFingerprintHeaders_SessionMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-aaa")

	turnMetadata := `{"installation_id":"user-install","session_id":"user-session","thread_id":"user-thread","turn_id":"user-turn","window_id":"user-thread:0","sandbox":"seccomp","thread_source":"user"}`
	h := http.Header{}
	h.Set("x-codex-installation-id", "user-install")
	h.Set("x-codex-window-id", "user-thread:0")
	h.Set("x-codex-turn-metadata", turnMetadata)
	h.Set("x-client-request-id", "user-thread")

	ids := resolveCodexFingerprintIDsFromRequest(account, clientHeaders)
	applyCodexFingerprintHeaders(h, ids)

	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedInstall := resolveConvergedInstallationID(account, seed)
	convergedSession := resolveConvergedSessionID(seed)
	convergedThread := resolveConvergedThreadID(seed, "client-session-aaa")

	assert.Equal(t, convergedInstall, h.Get("x-codex-installation-id"))
	assert.Equal(t, convergedSession, h.Get("session-id"))
	assert.Equal(t, convergedSession, h.Get("session_id"), "下划线形式也应被改写")
	assert.Equal(t, convergedThread, h.Get("thread-id"))
	assert.Equal(t, convergedThread, h.Get("x-client-request-id"))
	assert.Equal(t, convergedThread+":0", h.Get("x-codex-window-id"))

	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &meta))
	assert.Equal(t, convergedInstall, meta["installation_id"])
	assert.Equal(t, convergedSession, meta["session_id"])
	assert.Equal(t, convergedThread, meta["thread_id"])
	assert.NotEqual(t, "user-turn", meta["turn_id"], "turn_id 应被新生成的值替换")
	assert.Equal(t, "seccomp", meta["sandbox"], "sandbox 保留原样")
	assert.Equal(t, "user", meta["thread_source"], "thread_source 保留原样")
}

// --- session 模式：不同客户端得到不同 thread ---

func TestApplyCodexFingerprintHeaders_SessionMode_DifferentClients(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "session",
	})

	makeTurnMeta := func() string {
		return `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`
	}

	clientA := http.Header{}
	clientA.Set("session-id", "client-A")
	idsA := resolveCodexFingerprintIDsFromRequest(account, clientA)
	hA := http.Header{}
	hA.Set("x-codex-turn-metadata", makeTurnMeta())
	applyCodexFingerprintHeaders(hA, idsA)

	clientB := http.Header{}
	clientB.Set("session-id", "client-B")
	idsB := resolveCodexFingerprintIDsFromRequest(account, clientB)
	hB := http.Header{}
	hB.Set("x-codex-turn-metadata", makeTurnMeta())
	applyCodexFingerprintHeaders(hB, idsB)

	assert.Equal(t, hA.Get("session-id"), hB.Get("session-id"), "session_id 应相同")
	assert.NotEqual(t, hA.Get("thread-id"), hB.Get("thread-id"), "不同客户端 thread_id 应不同")
	assert.NotEqual(t, hA.Get("x-codex-window-id"), hB.Get("x-codex-window-id"), "不同客户端 window_id 应不同")
	assert.Equal(t, hA.Get("x-codex-installation-id"), hB.Get("x-codex-installation-id"))
}

// --- full 模式 ---

func TestApplyCodexFingerprintHeaders_FullMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "full",
	})
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedSession := resolveConvergedSessionID(seed)

	clientA := http.Header{}
	clientA.Set("session-id", "client-A")
	idsA := resolveCodexFingerprintIDsFromRequest(account, clientA)
	hA := http.Header{}
	hA.Set("x-codex-turn-metadata", `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`)
	applyCodexFingerprintHeaders(hA, idsA)

	clientB := http.Header{}
	clientB.Set("session-id", "client-B")
	idsB := resolveCodexFingerprintIDsFromRequest(account, clientB)
	hB := http.Header{}
	hB.Set("x-codex-turn-metadata", `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`)
	applyCodexFingerprintHeaders(hB, idsB)

	assert.Equal(t, hA.Get("thread-id"), hB.Get("thread-id"), "full 模式 thread_id 应相同")
	assert.Equal(t, convergedSession, hA.Get("thread-id"), "full 模式 thread_id 应等于 session_id")
	assert.Equal(t, hA.Get("x-codex-window-id"), hB.Get("x-codex-window-id"), "full 模式 window_id 应相同")
}

// --- H1 修复验证：头和体的 turn_id 一致性 ---

func TestFingerprintIDs_HeaderAndBody_TurnID_Consistent(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-xyz")

	ids := resolveCodexFingerprintIDsFromRequest(account, clientHeaders)
	require.NotNil(t, ids)

	// 头改写
	h := http.Header{}
	h.Set("x-codex-turn-metadata", `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`)
	applyCodexFingerprintHeaders(h, ids)

	// 体改写（使用同一份 ids）
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "x",
			"session_id":              "x",
			"turn_id":                 "x",
			"x-codex-turn-metadata":   `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`,
		},
	}
	applyCodexFingerprintClientMetadata(reqBody, ids)

	// 从头 turn-metadata JSON 提取 turn_id
	var headerMeta map[string]any
	require.NoError(t, json.Unmarshal([]byte(h.Get("x-codex-turn-metadata")), &headerMeta))
	headerTurnID, ok := headerMeta["turn_id"].(string)
	require.True(t, ok, "头 turn-metadata 应包含 string 类型的 turn_id")

	// 从体 client_metadata 提取 turn_id
	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok, "请求体应包含 client_metadata")
	bodyTurnID, ok := cm["turn_id"].(string)
	require.True(t, ok, "体 client_metadata 应包含 string 类型的 turn_id")

	// 从体内嵌 turn-metadata JSON 提取 turn_id
	embeddedRaw, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok, "体 client_metadata 应包含 x-codex-turn-metadata 字符串")
	var bodyMeta map[string]any
	require.NoError(t, json.Unmarshal([]byte(embeddedRaw), &bodyMeta))
	bodyEmbeddedTurnID, ok := bodyMeta["turn_id"].(string)
	require.True(t, ok, "体内嵌 turn-metadata 应包含 string 类型的 turn_id")
	headerTurnStartedAtUnixMS, ok := headerMeta["turn_started_at_unix_ms"].(float64)
	require.True(t, ok, "头 turn-metadata 应包含 numeric 类型的 turn_started_at_unix_ms")
	bodyTurnStartedAtUnixMS, ok := bodyMeta["turn_started_at_unix_ms"].(float64)
	require.True(t, ok, "体内嵌 turn-metadata 应包含 numeric 类型的 turn_started_at_unix_ms")

	assert.Equal(t, headerTurnID, bodyTurnID, "头和体的 turn_id 必须一致")
	assert.Equal(t, headerTurnID, bodyEmbeddedTurnID, "头和体内嵌 turn-metadata 的 turn_id 必须一致")
	assert.Equal(t, ids.turnID, headerTurnID, "所有 turn_id 都应来自同一份 ids")
	assert.Equal(t, headerTurnStartedAtUnixMS, bodyTurnStartedAtUnixMS, "头和体内嵌 turn-metadata 的 turn_started_at_unix_ms 必须一致")
	assert.Equal(t, float64(ids.turnStartedAtUnixMS), headerTurnStartedAtUnixMS, "所有 turn_started_at_unix_ms 都应来自同一份 ids")
}

// --- applyCodexFingerprintClientMetadata ---

func TestApplyCodexFingerprintClientMetadata_OffMode(t *testing.T) {
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "original",
		},
	}
	modified := applyCodexFingerprintClientMetadata(reqBody, nil)
	assert.False(t, modified, "nil ids 不改写")
}

func TestApplyCodexFingerprintClientMetadata_DeviceMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "device",
		"openai_device_id":           "converged-device",
	})
	ids := resolveCodexFingerprintIDsFromRequest(account, nil)
	require.NotNil(t, ids)

	embeddedMeta := `{"installation_id":"x","session_id":"user-session","sandbox":"seccomp"}`
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "original-install",
			"session_id":              "user-session",
			"x-codex-turn-metadata":   embeddedMeta,
		},
	}

	modified := applyCodexFingerprintClientMetadata(reqBody, ids)
	require.True(t, modified)

	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "converged-device", cm["x-codex-installation-id"])
	assert.Equal(t, "user-session", cm["session_id"], "device 模式不改 session_id")

	turnMetaStr, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(turnMetaStr), &meta))
	assert.Equal(t, "converged-device", meta["installation_id"])
	assert.Equal(t, "seccomp", meta["sandbox"], "非指纹字段保留原样")
}

func TestApplyCodexFingerprintClientMetadata_SessionMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-aaa")

	ids := resolveCodexFingerprintIDsFromRequest(account, clientHeaders)
	require.NotNil(t, ids)

	embeddedMeta := `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0","sandbox":"seccomp"}`
	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"x-codex-installation-id": "original-install",
			"session_id":              "original-session",
			"x-codex-turn-metadata":   embeddedMeta,
		},
	}

	modified := applyCodexFingerprintClientMetadata(reqBody, ids)
	require.True(t, modified)

	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedInstall := resolveConvergedInstallationID(account, seed)
	convergedSession := resolveConvergedSessionID(seed)
	convergedThread := resolveConvergedThreadID(seed, "client-session-aaa")

	assert.Equal(t, convergedInstall, cm["x-codex-installation-id"])
	assert.Equal(t, convergedSession, cm["session_id"])
	assert.Equal(t, convergedThread, cm["thread_id"])
	assert.Equal(t, convergedThread+":0", cm["x-codex-window-id"])

	turnMetaStr, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok)
	var meta map[string]any
	require.NoError(t, json.Unmarshal([]byte(turnMetaStr), &meta))
	assert.Equal(t, convergedInstall, meta["installation_id"])
	assert.Equal(t, convergedSession, meta["session_id"])
	assert.Equal(t, "seccomp", meta["sandbox"], "非指纹字段保留原样")
}

func TestApplyCodexFingerprintClientMetadata_FullMode(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "full",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "any-client")

	ids := resolveCodexFingerprintIDsFromRequest(account, clientHeaders)
	require.NotNil(t, ids)

	reqBody := map[string]any{
		"client_metadata": map[string]any{
			"session_id":            "x",
			"thread_id":             "x",
			"x-codex-turn-metadata": `{"installation_id":"x","session_id":"x","thread_id":"x","turn_id":"x","window_id":"x:0"}`,
		},
	}

	modified := applyCodexFingerprintClientMetadata(reqBody, ids)
	require.True(t, modified)

	cm, ok := reqBody["client_metadata"].(map[string]any)
	require.True(t, ok)
	seed, ok := codexFingerprintSeed(account.Extra)
	require.True(t, ok)
	convergedSession := resolveConvergedSessionID(seed)

	assert.Equal(t, convergedSession, cm["session_id"])
	assert.Equal(t, convergedSession, cm["thread_id"], "full 模式 thread_id 应等于 session_id")
}

// --- extractClientSessionID ---

func TestExtractClientSessionID(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected string
	}{
		{"连字符形式优先", func() http.Header {
			h := http.Header{}
			h.Set("session-id", "hyphen-form")
			h.Set("session_id", "underscore-form")
			return h
		}(), "hyphen-form"},
		{"回退到下划线形式", func() http.Header {
			h := http.Header{}
			h.Set("session_id", "underscore-form")
			return h
		}(), "underscore-form"},
		{"都没有", http.Header{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractClientSessionID(tt.headers))
		})
	}
}

func TestApplyCodexFingerprintHeaders_DoesNotRewriteIdentityTriple(t *testing.T) {
	account := newTestOAuthAccount(1, map[string]any{
		CodexFingerprintModeExtraKey: "session",
	})
	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-aaa")

	h := http.Header{}
	h.Set("User-Agent", "codex-tui/0.150.0 (Ubuntu 24.04; x86_64) xterm-256color")
	h.Set("Originator", "codex-tui")
	h.Set("Version", "0.150.0")
	h.Set("session-id", "client-session-aaa")

	ids := resolveCodexFingerprintIDsFromRequest(account, clientHeaders)
	require.NotNil(t, ids)
	applyCodexFingerprintHeaders(h, ids)

	assert.Equal(t, "codex-tui/0.150.0 (Ubuntu 24.04; x86_64) xterm-256color", h.Get("User-Agent"))
	assert.Equal(t, "codex-tui", h.Get("Originator"))
	assert.Equal(t, "0.150.0", h.Get("Version"))
	assert.NotEqual(t, "client-session-aaa", h.Get("session-id"))
	assert.Equal(t, resolveConvergedSessionID(account), h.Get("session-id"))
}

func TestStoreCodexFingerprintIDs_DoesNotLeakAcrossAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	first := newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "session"})
	secondOff := newTestOAuthAccount(2, map[string]any{CodexFingerprintModeExtraKey: "off"})
	third := newTestOAuthAccount(3, map[string]any{CodexFingerprintModeExtraKey: "session"})

	headers := http.Header{}
	headers.Set("session-id", "client-session-aaa")

	firstIDs := resolveCodexFingerprintIDsFromRequest(first, headers)
	require.NotNil(t, firstIDs)
	storeCodexFingerprintIDs(c, firstIDs)
	require.Equal(t, firstIDs, loadCodexFingerprintIDs(c, first))
	require.Nil(t, loadCodexFingerprintIDs(c, secondOff), "another account must not inherit stored IDs")

	storeCodexFingerprintIDs(c, resolveCodexFingerprintIDsFromRequest(secondOff, headers))
	require.Nil(t, loadCodexFingerprintIDs(c, first), "off mode must clear the previous account IDs")
	require.Nil(t, loadCodexFingerprintIDs(c, secondOff))

	thirdIDs := resolveCodexFingerprintIDsFromRequest(third, headers)
	require.NotNil(t, thirdIDs)
	storeCodexFingerprintIDs(c, thirdIDs)
	require.Equal(t, thirdIDs, loadCodexFingerprintIDs(c, third))
	require.Nil(t, loadCodexFingerprintIDs(c, first))
}

func TestStoreCodexFingerprintIDs_CompactClearsPreviousIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	account := newTestOAuthAccount(1, map[string]any{CodexFingerprintModeExtraKey: "session"})

	headers := http.Header{}
	headers.Set("session-id", "client-session-aaa")
	ids := resolveCodexFingerprintIDsFromRequest(account, headers)
	require.NotNil(t, ids)
	storeCodexFingerprintIDs(c, ids)
	require.Equal(t, ids, loadCodexFingerprintIDs(c, account))

	// Compact skips rewrite and must store nil so leftover IDs cannot overwrite
	// the Plus-isolated compact session namespace.
	storeCodexFingerprintIDs(c, nil)
	require.Nil(t, loadCodexFingerprintIDs(c, account))
}

func TestResolveCodexFingerprintAccount_UsesCredentialOwnerForShadow(t *testing.T) {
	owner := newTestOAuthAccount(101, map[string]any{
		CodexFingerprintModeExtraKey: "session",
		"openai_device_id":           "owner-device-id",
	})
	ownerID := owner.ID
	shadow := newTestOAuthAccount(202, map[string]any{
		CodexFingerprintModeExtraKey: "off",
		"openai_device_id":           "shadow-device-id",
	})
	shadow.ParentAccountID = &ownerID
	service := &OpenAIGatewayService{
		accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{owner.ID: owner}},
	}

	resolved, err := service.resolveCodexFingerprintAccount(t.Context(), shadow)
	require.NoError(t, err)
	require.Same(t, owner, resolved)

	headers := http.Header{}
	headers.Set("session-id", "client-session-aaa")
	ids := resolveCodexFingerprintIDsFromRequest(resolved, headers)
	require.NotNil(t, ids)
	assert.Equal(t, owner.ID, ids.accountID)
	assert.Equal(t, "owner-device-id", ids.installationID)
	assert.Equal(t, resolveConvergedSessionID(owner), ids.sessionID)
	assert.NotEqual(t, resolveConvergedSessionID(shadow), ids.sessionID)
}

func TestBuildUpstreamRequest_UsesCredentialOwnerFingerprintIDsForShadow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	owner := newTestOAuthAccount(101, map[string]any{
		CodexFingerprintModeExtraKey: "session",
		"openai_device_id":           "owner-device-id",
	})
	ownerID := owner.ID
	shadow := newTestOAuthAccount(202, map[string]any{
		CodexFingerprintModeExtraKey: "off",
		"openai_device_id":           "shadow-device-id",
	})
	shadow.ParentAccountID = &ownerID
	service := &OpenAIGatewayService{
		accountRepo: &oauthSessionPolicyAccountRepo{accounts: map[int64]*Account{owner.ID: owner}},
	}

	clientHeaders := http.Header{}
	clientHeaders.Set("session-id", "client-session-aaa")
	ids := resolveCodexFingerprintIDsFromRequest(owner, clientHeaders)
	require.NotNil(t, ids)
	storeCodexFingerprintIDs(c, ids)

	req, err := service.buildUpstreamRequest(
		t.Context(), c, shadow, []byte(`{"model":"gpt-5"}`), "test-token", false, "", true,
	)
	require.NoError(t, err)
	assert.Equal(t, "owner-device-id", req.Header.Get("x-codex-installation-id"))
	assert.Equal(t, resolveConvergedSessionID(owner), req.Header.Get("session-id"))
	assert.NotEqual(t, resolveConvergedSessionID(shadow), req.Header.Get("session-id"))
}

// --- 透传路径：raw 字节版 client_metadata 改写 ---

// rawVsMapClientMetadata 用同一份 ids 分别跑 map 版与 raw 字节版，
// 返回两侧最终的 client_metadata 解码结果。
func rawVsMapClientMetadata(t *testing.T, body []byte, ids *codexFingerprintIDs) (map[string]any, map[string]any) {
	t.Helper()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	applyCodexFingerprintClientMetadata(decoded, ids)
	mapCM, _ := decoded["client_metadata"].(map[string]any)

	rawBody, changed, err := applyCodexFingerprintClientMetadataRaw(body, ids)
	require.NoError(t, err)
	require.True(t, changed)
	var rawDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawBody, &rawDecoded))
	rawCM, _ := rawDecoded["client_metadata"].(map[string]any)
	return mapCM, rawCM
}

func cloneCodexFingerprintIDsForTest(ids *codexFingerprintIDs) *codexFingerprintIDs {
	if ids == nil {
		return nil
	}
	cloned := *ids
	cloned.originalBodySessionID = ""
	cloned.originalBodySessionIDCaptured = false
	return &cloned
}

func applyMapAndRawFingerprintBodiesForTest(t *testing.T, body []byte, ids *codexFingerprintIDs) (map[string]any, map[string]any) {
	t.Helper()

	mapIDs := cloneCodexFingerprintIDsForTest(ids)
	rawIDs := cloneCodexFingerprintIDsForTest(ids)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	applyCodexFingerprintClientMetadata(decoded, mapIDs)

	rawBody, _, err := applyCodexFingerprintClientMetadataRaw(body, rawIDs)
	require.NoError(t, err)
	var rawDecoded map[string]any
	require.NoError(t, json.Unmarshal(rawBody, &rawDecoded))
	return decoded, rawDecoded
}

func TestApplyCodexFingerprintPromptCacheKey_MapRawEquivalence(t *testing.T) {
	for _, mode := range []codexFingerprintMode{codexFingerprintSession, codexFingerprintFull} {
		t.Run(string(mode)+"/default", func(t *testing.T) {
			account := newTestOAuthAccount(4300, map[string]any{codexFingerprintModeExtraKey: string(mode)})
			ids := resolveCodexFingerprintIDs(account, "header-session", mode)
			require.NotNil(t, ids)

			body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"body-session","client_metadata":{"session_id":" body-session ","trace":"keep"},"input":[]}`)
			mapBody, rawBody := applyMapAndRawFingerprintBodiesForTest(t, body, ids)

			require.Equal(t, mapBody["prompt_cache_key"], rawBody["prompt_cache_key"])
			require.Equal(t, ids.sessionID, mapBody["prompt_cache_key"])
			mapCM, _ := mapBody["client_metadata"].(map[string]any)
			rawCM, _ := rawBody["client_metadata"].(map[string]any)
			require.Equal(t, ids.sessionID, mapCM["session_id"])
			require.Equal(t, mapCM["session_id"], rawCM["session_id"])
			require.Equal(t, "keep", rawCM["trace"])
		})
	}

	t.Run("explicit override", func(t *testing.T) {
		account := newTestOAuthAccount(4301, map[string]any{codexFingerprintModeExtraKey: "session"})
		ids := resolveCodexFingerprintIDs(account, "header-session", codexFingerprintSession)
		require.NotNil(t, ids)

		body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"explicit-cache","client_metadata":{"session_id":"body-session"},"input":[]}`)
		mapBody, rawBody := applyMapAndRawFingerprintBodiesForTest(t, body, ids)

		require.Equal(t, "explicit-cache", mapBody["prompt_cache_key"])
		require.Equal(t, "explicit-cache", rawBody["prompt_cache_key"])
		mapCM, _ := mapBody["client_metadata"].(map[string]any)
		rawCM, _ := rawBody["client_metadata"].(map[string]any)
		require.Equal(t, ids.sessionID, mapCM["session_id"])
		require.Equal(t, ids.sessionID, rawCM["session_id"])
	})
}

func TestApplyCodexFingerprintPromptCacheKey_Negatives(t *testing.T) {
	sessionAccount := newTestOAuthAccount(4310, map[string]any{codexFingerprintModeExtraKey: "session"})
	sessionIDs := resolveCodexFingerprintIDs(sessionAccount, "header-session", codexFingerprintSession)
	require.NotNil(t, sessionIDs)
	deviceAccount := newTestOAuthAccount(4311, map[string]any{codexFingerprintModeExtraKey: "device"})
	deviceIDs := resolveCodexFingerprintIDs(deviceAccount, "header-session", codexFingerprintDevice)
	require.NotNil(t, deviceIDs)

	tests := []struct {
		name          string
		body          []byte
		ids           *codexFingerprintIDs
		wantExists    bool
		wantCacheKey  any
		wantRawString string
	}{
		{
			name:       "missing key is not injected",
			body:       []byte(`{"client_metadata":{"session_id":"body-session"}}`),
			ids:        sessionIDs,
			wantExists: false,
		},
		{
			name:         "empty key preserved",
			body:         []byte(`{"prompt_cache_key":"","client_metadata":{"session_id":"body-session"}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "",
		},
		{
			name:         "whitespace-different key is an explicit override",
			body:         []byte(`{"prompt_cache_key":" body-session ","client_metadata":{"session_id":"body-session"}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: " body-session ",
		},
		{
			name:         "non-string key preserved",
			body:         []byte(`{"prompt_cache_key":123,"client_metadata":{"session_id":"body-session"}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: float64(123),
		},
		{
			name:         "missing source metadata preserves key",
			body:         []byte(`{"prompt_cache_key":"body-session"}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "body-session",
		},
		{
			name:         "non-string source session preserves key",
			body:         []byte(`{"prompt_cache_key":"123","client_metadata":{"session_id":123}}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "123",
		},
		{
			name:         "non-object source metadata preserves key",
			body:         []byte(`{"prompt_cache_key":"body-session","client_metadata":"bad"}`),
			ids:          sessionIDs,
			wantExists:   true,
			wantCacheKey: "body-session",
		},
		{
			name:         "device mode preserves key",
			body:         []byte(`{"prompt_cache_key":"body-session","client_metadata":{"session_id":"body-session"}}`),
			ids:          deviceIDs,
			wantExists:   true,
			wantCacheKey: "body-session",
		},
		{
			name:          "off mode preserves body",
			body:          []byte(`{"prompt_cache_key":"body-session","client_metadata":{"session_id":"body-session"}}`),
			ids:           nil,
			wantExists:    true,
			wantCacheKey:  "body-session",
			wantRawString: `{"prompt_cache_key":"body-session","client_metadata":{"session_id":"body-session"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mapBody map[string]any
			require.NoError(t, json.Unmarshal(tt.body, &mapBody))
			changedMap := applyCodexFingerprintClientMetadata(mapBody, cloneCodexFingerprintIDsForTest(tt.ids))

			rawBody, changedRaw, err := applyCodexFingerprintClientMetadataRaw(tt.body, cloneCodexFingerprintIDsForTest(tt.ids))
			require.NoError(t, err)
			if tt.ids == nil {
				require.False(t, changedMap)
				require.False(t, changedRaw)
				require.JSONEq(t, tt.wantRawString, string(rawBody))
				return
			}
			require.True(t, changedMap)
			require.True(t, changedRaw)

			rawDecoded := map[string]any{}
			require.NoError(t, json.Unmarshal(rawBody, &rawDecoded))
			_, mapExists := mapBody["prompt_cache_key"]
			_, rawExists := rawDecoded["prompt_cache_key"]
			require.Equal(t, tt.wantExists, mapExists)
			require.Equal(t, tt.wantExists, rawExists)
			if tt.wantExists {
				require.Equal(t, tt.wantCacheKey, mapBody["prompt_cache_key"])
				require.Equal(t, tt.wantCacheKey, rawDecoded["prompt_cache_key"])
			}
		})
	}
}

func TestApplyCodexFingerprintClientMetadataRaw_MatchesMapVariant(t *testing.T) {
	embedded := `{\"installation_id\":\"real-install\",\"session_id\":\"real-session\",\"sandbox\":\"seatbelt\"}`
	bodies := map[string]string{
		"no_client_metadata": `{"model":"gpt-5.6-sol","input":[],"stream":true}`,
		"object_with_extras": `{"model":"gpt-5.6-sol","client_metadata":{"session_id":"client-session","traceparent":"00-abc-def-01","x-codex-turn-metadata":"` + embedded + `"},"stream":true}`,
		"non_object_value":   `{"model":"gpt-5.6-sol","client_metadata":"bogus","stream":true}`,
	}
	for _, mode := range []codexFingerprintMode{codexFingerprintDevice, codexFingerprintSession, codexFingerprintFull} {
		account := newTestOAuthAccount(4242, map[string]any{codexFingerprintModeExtraKey: string(mode)})
		ids := resolveCodexFingerprintIDs(account, "client-sess-raw", mode)
		require.NotNil(t, ids)
		for name, body := range bodies {
			t.Run(string(mode)+"/"+name, func(t *testing.T) {
				mapCM, rawCM := rawVsMapClientMetadata(t, []byte(body), ids)
				assert.Equal(t, mapCM, rawCM, "raw 字节版与 map 版的 client_metadata 结果必须逐点一致")
			})
		}
	}
}

func TestApplyCodexFingerprintClientMetadataRaw_PreservesUnrelatedFields(t *testing.T) {
	account := newTestOAuthAccount(4243, map[string]any{codexFingerprintModeExtraKey: "session"})
	ids := resolveCodexFingerprintIDs(account, "client-sess-preserve", codexFingerprintSession)
	require.NotNil(t, ids)

	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"user","content":"hi"}],"stream":true,"prompt_cache_key":"pck-1"}`)
	out, changed, err := applyCodexFingerprintClientMetadataRaw(body, ids)
	require.NoError(t, err)
	require.True(t, changed)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(out, &decoded))
	assert.Equal(t, "gpt-5.6-sol", decoded["model"])
	assert.Equal(t, "pck-1", decoded["prompt_cache_key"])
	assert.Equal(t, true, decoded["stream"])
	cm, _ := decoded["client_metadata"].(map[string]any)
	require.NotNil(t, cm)
	assert.Equal(t, ids.sessionID, cm["session_id"])
	assert.Equal(t, ids.turnID, cm["turn_id"])
}

func TestApplyCodexFingerprintClientMetadataRaw_Noop(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol"}`)
	out, changed, err := applyCodexFingerprintClientMetadataRaw(body, nil)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, body, out)

	out, changed, err = applyCodexFingerprintClientMetadataRaw(nil, &codexFingerprintIDs{mode: codexFingerprintSession, installationID: "x"})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Nil(t, out)
}

// --- context 暂存与出站头应用（透传/非透传共用 seam）---

func newFingerprintStageTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c
}

func TestBuildUpstreamRequestOpenAIPassthrough_AppliesStoredFingerprint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newTestOAuthAccount(2001, map[string]any{
		"openai_oauth_passthrough": true,
		"codex_fingerprint_mode":   "session",
	})

	c := newFingerprintStageTestContext(t)
	c.Request.Header.Set("session_id", "real-client-session")
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color")
	c.Request.Header.Set("originator", "codex_cli_rs")
	c.Request.Header.Set("x-codex-turn-metadata", `{"installation_id":"real-install","session_id":"real-session","sandbox":"seatbelt"}`)

	// Reproduce the passthrough parse/store seam with one shared ID set.
	ids := resolveCodexFingerprintIDsFromRequest(account, c.Request.Header)
	require.NotNil(t, ids)
	storeCodexFingerprintIDs(c, ids)

	body := []byte(`{"model":"gpt-5.6-sol","input":[],"stream":true}`)
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
	require.NoError(t, err)

	assert.Equal(t, ids.sessionID, req.Header.Get("session-id"), "无 body cache key 时应保留指纹收敛 session-id")
	assert.Equal(t, ids.sessionID, req.Header.Get("session_id"), "session 模式下出站 session_id 应为账号级收敛值")
	assert.Equal(t, ids.installationID, req.Header.Get("x-codex-installation-id"))
	assert.Equal(t, ids.windowID, req.Header.Get("x-codex-window-id"))
	assert.Equal(t, ids.threadID, req.Header.Get("x-client-request-id"))
	assert.Equal(t, ids.threadID, req.Header.Get("thread-id"))
	turnMetadata := req.Header.Get("x-codex-turn-metadata")
	require.NotEmpty(t, turnMetadata)
	assert.Contains(t, turnMetadata, ids.sessionID, "turn-metadata JSON 中的 session_id 应被收敛")
	assert.Contains(t, turnMetadata, `"sandbox":"seatbelt"`, "turn-metadata 未指定字段应原样保留")
}

func TestBuildUpstreamRequestOpenAIPassthrough_PromptCacheIdentityOverridesSessionFingerprint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newTestOAuthAccount(2003, map[string]any{
		"openai_oauth_passthrough": true,
		"codex_fingerprint_mode":   "session",
	})

	c := newFingerprintStageTestContext(t)
	c.Request.Header.Set("session-id", "real-client-session")
	c.Request.Header.Set("thread-id", "real-client-thread")
	c.Request.Header.Set("originator", "codex_cli_rs")

	ids := resolveCodexFingerprintIDsFromRequest(account, c.Request.Header)
	require.NotNil(t, ids)
	storeCodexFingerprintIDs(c, ids)

	cacheIdentity := "51e296c6-3942-45c4-9e36-a909f2709590"
	markOpenAIAlignedPromptCacheIdentity(c, account, cacheIdentity, cacheIdentity)
	body := []byte(`{"model":"gpt-5.6-sol","input":[],"stream":true,"prompt_cache_key":"` + cacheIdentity + `"}`)
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
	require.NoError(t, err)

	assert.Equal(t, cacheIdentity, req.Header.Get("session-id"))
	assert.Equal(t, cacheIdentity, req.Header.Get("session_id"))
	assert.Equal(t, ids.installationID, req.Header.Get("x-codex-installation-id"))
	assert.Equal(t, ids.threadID, req.Header.Get("thread-id"))
	assert.Equal(t, ids.threadID, req.Header.Get("x-client-request-id"))
}

func TestBuildUpstreamRequestOpenAIPassthrough_OffModeKeepsIsolatedSession(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := newTestOAuthAccount(2002, map[string]any{
		"openai_oauth_passthrough": true,
		"codex_fingerprint_mode":   "off",
	})

	c := newFingerprintStageTestContext(t)
	c.Request.Header.Set("session_id", "real-client-session")
	c.Request.Header.Set("originator", "codex_cli_rs")

	ids := resolveCodexFingerprintIDsFromRequest(account, c.Request.Header)
	require.Nil(t, ids)
	storeCodexFingerprintIDs(c, ids)

	body := []byte(`{"model":"gpt-5.6-sol","input":[],"stream":true}`)
	req, err := svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
	require.NoError(t, err)

	assert.NotEmpty(t, req.Header.Get("session_id"))
	assert.NotEqual(t, resolveConvergedSessionID(testCodexFingerprintSeed), req.Header.Get("session_id"), "off 模式不得收敛 session_id")
	assert.Empty(t, req.Header.Get("x-codex-window-id"))
}

func TestApplyCodexFingerprintClientMetadataRaw_NonObjectBodyUntouched(t *testing.T) {
	account := newTestOAuthAccount(4244, map[string]any{codexFingerprintModeExtraKey: "session"})
	ids := resolveCodexFingerprintIDs(account, "client-sess-nonobj", codexFingerprintSession)
	require.NotNil(t, ids)

	for _, body := range []string{`[1,2,3]`, `"plain string"`, `not json at all`} {
		out, changed, err := applyCodexFingerprintClientMetadataRaw([]byte(body), ids)
		require.NoError(t, err)
		assert.False(t, changed, "非 JSON 对象 body 不应被改写: %s", body)
		assert.Equal(t, []byte(body), out)
	}
}
