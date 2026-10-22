//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTurnStateTestContext(t *testing.T, apiKeyID int64, sessionID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if sessionID != "" {
		c.Request.Header.Set("session_id", sessionID)
	}
	if apiKeyID > 0 {
		c.Set("api_key", &APIKey{ID: apiKeyID})
	}
	return c, rec
}

func TestOpenAICodexTurnStateSeed(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "sess-1")
	require.Equal(t, "7\x00sess-1", openAICodexTurnStateSeed(c))

	// 连字符形式优先（Codex CLI 标准头）
	c.Request.Header.Set("session-id", "sess-hyphen")
	require.Equal(t, "7\x00sess-hyphen", openAICodexTurnStateSeed(c))

	// 无会话标识 → 不跟踪
	cNoSession, _ := newTurnStateTestContext(t, 7, "")
	require.Empty(t, openAICodexTurnStateSeed(cNoSession))

	require.Empty(t, openAICodexTurnStateSeed(nil))
}

func turnStateAccount(id int64, owner string) *Account {
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": owner, "chatgpt_user_id": "user", "access_token": "initial"}}
}

func TestCodexTurnStateOwnerAndBlobLifecycle(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 7, "session")
	a := turnStateAccount(42, "owner-a")
	b := turnStateAccount(43, "owner-b")
	echo := func(account *Account, state string) string {
		h := http.Header{"X-Codex-Turn-State": []string{state}}
		svc.guardOpenAICodexTurnStateEcho(c, account, h)
		return h.Get(openAICodexTurnStateHeader)
	}
	staged := http.Header{"X-Codex-Turn-State": []string{"blob-a"}}
	svc.relayOpenAICodexTurnState(c, a, staged)
	require.Equal(t, "blob-a", c.Writer.Header().Get(openAICodexTurnStateHeader))
	require.Empty(t, echo(a, "blob-a"), "uncommitted response cannot authorize an echo")
	svc.noteStagedOpenAICodexTurnStateCommitted(c, a, staged)
	require.Equal(t, "blob-a", echo(a, "blob-a"))
	refreshed := turnStateAccount(42, "owner-a")
	refreshed.Credentials["access_token"] = "refreshed"
	refreshed.Credentials["refresh_token"] = "rotated"
	require.Equal(t, "blob-a", echo(refreshed, "blob-a"), "refresh preserves principal")
	require.Empty(t, echo(turnStateAccount(42, "owner-c"), "blob-a"), "same row relink changes principal")
	svc.noteStagedOpenAICodexTurnStateCommitted(c, b, http.Header{"X-Codex-Turn-State": []string{"blob-b"}})
	require.Equal(t, "blob-b", echo(b, "blob-b"))
	require.Empty(t, echo(b, "blob-a"), "latest owner must not relabel a different old blob")
	require.Equal(t, "blob-a", echo(a, "blob-a"), "records remain keyed by individual blob")
	require.Empty(t, echo(a, "unknown"))
	require.Empty(t, echo(&Account{ID: 42}, "blob-a"), "local ID alone is not principal evidence")
	svc.relayOpenAICodexTurnState(c, a, http.Header{})
	require.Empty(t, c.Writer.Header().Get(openAICodexTurnStateHeader))
}

type turnStateSharedStore struct {
	*stubGatewayCache
	origins map[string]string
}

func (s *turnStateSharedStore) SetOpenAICodexTurnStateOrigin(_ context.Context, seed string, owner string, _ time.Duration) error {
	if s.origins == nil {
		s.origins = map[string]string{}
	}
	s.origins[seed] = owner
	return nil
}
func (s *turnStateSharedStore) GetOpenAICodexTurnStateOrigin(_ context.Context, seed string) (string, error) {
	return s.origins[seed], nil
}

func TestCodexTurnStateCrossReplicaAndExpiredProvenance(t *testing.T) {
	store := &turnStateSharedStore{stubGatewayCache: &stubGatewayCache{}}
	writer := &OpenAIGatewayService{cache: store}
	reader := &OpenAIGatewayService{cache: store}
	c, _ := newTurnStateTestContext(t, 21, "session")
	owner := turnStateAccount(101, "principal")
	writer.noteStagedOpenAICodexTurnStateCommitted(c, owner, http.Header{"X-Codex-Turn-State": []string{"shared"}})
	h := http.Header{"X-Codex-Turn-State": []string{"shared"}}
	reader.guardOpenAICodexTurnStateEcho(c, owner, h)
	require.Equal(t, "shared", h.Get(openAICodexTurnStateHeader))
	reader.guardOpenAICodexTurnStateEcho(c, turnStateAccount(101, "relinked"), h)
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
	local := &OpenAIGatewayService{}
	key := openAICodexTurnStateProvenanceKey(c, "old")
	local.openaiCodexTurnStateOrigins.Store(key, openAICodexTurnStateOrigin{accountID: 101, owner: openAICodexStateOwner(context.Background(), owner), expiresAt: time.Now().Add(-time.Second)})
	h.Set(openAICodexTurnStateHeader, "old")
	local.guardOpenAICodexTurnStateEcho(c, owner, h)
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
	_, ok := local.openaiCodexTurnStateOrigins.Load(key)
	require.False(t, ok)
	// Pre-upgrade records contain neither a blob binding nor principal evidence.
	local.openaiCodexTurnStateOrigins.Store(openAICodexTurnStateSeed(c), openAICodexTurnStateOrigin{accountID: 101, expiresAt: time.Now().Add(time.Hour)})
	h.Set(openAICodexTurnStateHeader, "old")
	local.guardOpenAICodexTurnStateEcho(c, owner, h)
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
}

func TestWriteOpenAIPassthroughResponseHeaders_RelaysAndClearsTurnState(t *testing.T) {
	// filter=nil 走 content-type 兜底分支；turn-state 强制放行不依赖 filter。
	dst := http.Header{}
	src := http.Header{}
	src.Set("X-Codex-Turn-State", "blob-P")
	writeOpenAIPassthroughResponseHeaders(dst, src, nil)
	require.Equal(t, "blob-P", dst.Get("X-Codex-Turn-State"))

	// 上游缺失时清除残留（failover 换号防串扰）
	writeOpenAIPassthroughResponseHeaders(dst, http.Header{"Content-Type": []string{"application/json"}}, nil)
	require.Empty(t, dst.Get("X-Codex-Turn-State"))
}

func TestWriteOpenAIPassthroughResponseHeaders_RelaysReasoningIncluded(t *testing.T) {
	dst := http.Header{}
	src := http.Header{}
	src.Set("X-Reasoning-Included", "1")

	writeOpenAIPassthroughResponseHeaders(
		dst,
		src,
		responseheaders.CompileHeaderFilter(config.ResponseHeaderConfig{}),
	)
	require.Equal(t, "1", dst.Get("X-Reasoning-Included"))
}

func TestEnsureOpenAIRemoteCompactionV2BetaFeature(t *testing.T) {
	t.Run("absent_sets_feature", func(t *testing.T) {
		h := http.Header{}
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("present_unchanged", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "responses_websockets_v2, remote_compaction_v2")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "responses_websockets_v2, remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("other_tokens_merged", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "responses_websockets_v2")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "responses_websockets_v2,remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("multi_line_values_merged_single_line", func(t *testing.T) {
		h := http.Header{}
		h.Add("x-codex-beta-features", "feature_a")
		h.Add("x-codex-beta-features", "feature_b")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, []string{"feature_a,feature_b,remote_compaction_v2"}, h.Values("x-codex-beta-features"))
	})
}

// Official 770ed6dcea40 session/mod.rs advertises v2 independently of auth.
func TestApplyOpenAICodexBetaFeatures(t *testing.T) {
	oauthAccount := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	apiKeyAccount := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	t.Run("oauth_plain_request_gets_default_codex_shape", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(context.Background(), c, oauthAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"),
			"OAuth 的普通请求也必须带会话级 beta 头")
	})

	t.Run("client_declared_features_keep_other_tokens_and_add_v2", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(context.Background(), c, oauthAccount, h)
		require.Equal(t, "some_other_feature,remote_compaction_v2", h.Get("x-codex-beta-features"),
			"a partial declaration cannot disable the official unconditional capability")
	})

	t.Run("native_v2_forces_feature_even_when_client_trimmed_it", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(context.Background(), c, oauthAccount, h)
		require.Contains(t, h.Get("x-codex-beta-features"), "remote_compaction_v2",
			"body 带 compaction_trigger 是实锤，必须确保 v2 在列")
		require.Contains(t, h.Get("x-codex-beta-features"), "some_other_feature")
	})

	t.Run("native_v2_applies_to_non_oauth_too", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		applyOpenAICodexBetaFeatures(context.Background(), c, apiKeyAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("native_api_key_plain_request_advertises_v2", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(context.Background(), c, apiKeyAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("other_preset_keeps_its_session_policy", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		account := &Account{Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Credentials: map[string]any{
			outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"},
		}}
		h := http.Header{}
		applyOpenAICodexBetaFeatures(context.Background(), c, account, h)
		require.Empty(t, h.Get("x-codex-beta-features"))
		MarkOpenAINativeCompactionV2(c)
		applyOpenAICodexBetaFeatures(context.Background(), c, account, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"), "explicit native-v2 requests still require the token")
	})

	t.Run("nil_account_plain_request_untouched", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(context.Background(), c, nil, h)
		require.Empty(t, h.Get("x-codex-beta-features"))
	})
}

// WS 握手与 HTTP 出站必须给出同一份会话级 beta 头：真实 Codex 的
// build_websocket_headers 复用 build_responses_headers（client.rs），
// 两侧不一致还会让预热连接与实际请求落进不同的连接池兼容分桶。
func TestBuildOpenAIWSHeaders_CarriesSessionBetaFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	decision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}

	build := func(t *testing.T, account *Account, clientBeta string) http.Header {
		t.Helper()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		if clientBeta != "" {
			c.Request.Header.Set("x-codex-beta-features", clientBeta)
		}
		headers, _, err := svc.buildOpenAIWSHeaders(
			context.Background(), c, account, "test-token", decision,
			true, "", "", "", "gpt-5.6-codex", "",
		)
		require.NoError(t, err)
		return headers
	}

	oauthAccount := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "test-account"},
	}

	headers := build(t, oauthAccount, "")
	require.Equal(t, "remote_compaction_v2", headers.Get("x-codex-beta-features"),
		"WS 握手也必须带会话级 beta 头")

	declared := build(t, oauthAccount, "some_other_feature")
	require.Equal(t, []string{"some_other_feature,remote_compaction_v2"}, declared.Values("x-codex-beta-features"))

	apiKeyHeaders := build(t, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "")
	require.Equal(t, "remote_compaction_v2", apiKeyHeaders.Get("x-codex-beta-features"))
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		prewarm, _, err := svc.buildOpenAIWSHeaders(context.Background(), nil,
			&Account{Platform: PlatformOpenAI, Type: kind}, "fixture", decision,
			true, "", "", "", "gpt-6.1-sol", "")
		require.NoError(t, err)
		require.Equal(t, "remote_compaction_v2", prewarm.Get("x-codex-beta-features"), "prewarm must not depend on inbound headers")
	}
}

func TestCodexTurnStateProvenanceUsesImmutableShadowSendOwner(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 7, "session")
	parent := turnStateAccount(100, "sent-owner")
	parentID := parent.ID
	shadow := turnStateAccount(200, "unused-shadow-claims")
	shadow.ParentAccountID = &parentID
	ctx := WithOutboundIdentityScope(t.Context(), c)
	// The acquisition evidence must point at the real parent, not shadow fields.
	recordRequestCredentialOwner(ctx, parent, parent.GetOpenAIAccessToken())
	ctx = withRequestCredentialOwner(ctx, shadow, parent.GetOpenAIAccessToken())
	defer bindRequestCredentialOwnerContext(ctx, c)()
	c.Set(codexAccountIdentitySourceContextKey, parent)
	parent.Credentials["chatgpt_account_id"] = "relinked-owner"
	svc.noteStagedOpenAICodexTurnStateCommitted(c, shadow, http.Header{"X-Codex-Turn-State": []string{"sent-blob"}})
	next, _ := newTurnStateTestContext(t, 7, "session")
	headers := http.Header{"X-Codex-Turn-State": []string{"sent-blob"}}
	svc.guardOpenAICodexTurnStateEcho(next, turnStateAccount(100, "sent-owner"), headers)
	require.Equal(t, "sent-blob", headers.Get(openAICodexTurnStateHeader))
	headers.Set(openAICodexTurnStateHeader, "sent-blob")
	svc.guardOpenAICodexTurnStateEcho(next, parent, headers)
	require.Empty(t, headers.Get(openAICodexTurnStateHeader))
}

func TestCodexAgentStateOwnerTracksSigningIdentityNotTask(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	account := &Account{ID: 481, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID, "agent_private_key": privateKey, "task_id": key.taskID}}
	owner := openAICodexStateOwner(t.Context(), account)
	require.NotEmpty(t, owner)
	namespace := codexAccountIdentityNamespace(account)
	account.Credentials["task_id"] = "next-task"
	require.Equal(t, owner, openAICodexStateOwner(t.Context(), account))
	require.Equal(t, namespace, codexAccountIdentityNamespace(account))
	account.Credentials["agent_runtime_id"] = "different-runtime"
	require.NotEqual(t, owner, openAICodexStateOwner(t.Context(), account))
	require.NotEqual(t, namespace, codexAccountIdentityNamespace(account))
}
