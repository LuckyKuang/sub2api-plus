package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func newOpenAIPromptCacheIdentityTestContext(t *testing.T, apiKeyID int64) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: apiKeyID})
	return c
}

func requireOpenAIAutoCacheIdentityPreservesBody(
	t *testing.T,
	originalBody []byte,
	upstreamBody []byte,
	headers http.Header,
) string {
	t.Helper()
	identity := strings.TrimSpace(gjson.GetBytes(upstreamBody, "prompt_cache_key").String())
	require.NotEmpty(t, identity)
	require.Equal(t, identity, headers.Get(codexSessionIDHeader))
	require.Equal(t, identity, headers.Get("session_id"))

	withoutIdentity, err := sjson.DeleteBytes(upstreamBody, "prompt_cache_key")
	require.NoError(t, err)
	require.JSONEq(t, string(originalBody), string(withoutIdentity))
	return identity
}

func TestEnsureOpenAIResponsesPromptCacheIdentityAlignsExplicitBodyKey(t *testing.T) {
	c := newOpenAIPromptCacheIdentityTestContext(t, 42)
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_key":"client-session","input":"hello"}`)

	normalized, identity, changed, err := svc.ensureOpenAIResponsesPromptCacheIdentity(c, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body, "", "gpt-5.6-sol")

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, generateSessionUUID(isolateOpenAISessionID(42, "client-session")), identity)
	require.Equal(t, identity, gjson.GetBytes(normalized, "prompt_cache_key").String())
	require.True(t, isOpenAIAlignedPromptCacheIdentity(c, identity))
	require.True(t, isOpenAIAlignedPromptCacheIdentityForAccount(c, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, identity))
	require.LessOrEqual(t, len(identity), 64)
}

func TestEnsureOpenAIResponsesPromptCacheIdentityIsIdempotent(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c := newOpenAIPromptCacheIdentityTestContext(t, 301)
	account := &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	body := []byte(`{"model":"gpt-5.6","prompt_cache_key":"client-cache-key","input":"hello"}`)

	firstBody, firstIdentity, firstChanged, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, account, body, "client-cache-key", "gpt-5.6",
	)
	require.NoError(t, err)
	require.True(t, firstChanged)
	require.NotEmpty(t, firstIdentity)

	secondBody, secondIdentity, secondChanged, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, account, firstBody, firstIdentity, "gpt-5.6",
	)
	require.NoError(t, err)
	require.False(t, secondChanged)
	require.Equal(t, firstIdentity, secondIdentity)
	require.Equal(t, firstBody, secondBody)
}

func TestEnsureOpenAIResponsesPromptCacheIdentityIsIdempotentWhenFinalIdentityIsSeedOnly(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c := newOpenAIPromptCacheIdentityTestContext(t, 302)
	account := &Account{ID: 92, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	originalBody := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	_, firstIdentity, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, account, originalBody, "client-cache-key", "gpt-5.6",
	)
	require.NoError(t, err)
	require.NotEmpty(t, firstIdentity)

	retryBody, retryIdentity, changed, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, account, originalBody, firstIdentity, "gpt-5.6",
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, firstIdentity, retryIdentity)
	require.Equal(t, firstIdentity, gjson.GetBytes(retryBody, "prompt_cache_key").String())
}

func TestEnsureOpenAIResponsesPromptCacheIdentityReScopesFinalIdentitySeed(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c := newOAuthSessionPolicyGinContext(702, 9001, 11)
	firstAccount := newOpenAIOAuthSessionPolicyAccount(103, 11)
	secondAccount := newOpenAIOAuthSessionPolicyAccount(204, 11)
	originalBody := []byte(`{"model":"gpt-5.6","input":"hello"}`)

	_, firstIdentity, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, &firstAccount, originalBody, "client-cache-key", "gpt-5.6",
	)
	require.NoError(t, err)

	secondBody, secondIdentity, changed, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, &secondAccount, originalBody, firstIdentity, "gpt-5.6",
	)
	require.NoError(t, err)
	require.True(t, changed)
	require.NotEqual(t, firstIdentity, secondIdentity)
	require.Equal(t, secondIdentity, gjson.GetBytes(secondBody, "prompt_cache_key").String())
	expected, err := svc.resolveOpenAIPromptCacheIdentity(c, &secondAccount, "client-cache-key")
	require.NoError(t, err)
	require.Equal(t, expected, secondIdentity)
}

func TestEnsureOpenAIResponsesPromptCacheIdentityReScopesFinalizedRetryBody(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c := newOAuthSessionPolicyGinContext(701, 9001, 11)
	firstAccount := newOpenAIOAuthSessionPolicyAccount(101, 11)
	secondAccount := newOpenAIOAuthSessionPolicyAccount(202, 11)
	body := []byte(`{"model":"gpt-5.6","prompt_cache_key":"client-cache-key","input":"hello"}`)

	firstBody, firstIdentity, firstChanged, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, &firstAccount, body, "client-cache-key", "gpt-5.6",
	)
	require.NoError(t, err)
	require.True(t, firstChanged)
	require.True(t, isOpenAIAlignedPromptCacheIdentityForAccount(c, &firstAccount, firstIdentity))

	secondBody, secondIdentity, secondChanged, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, &secondAccount, firstBody, firstIdentity, "gpt-5.6",
	)
	require.NoError(t, err)
	require.True(t, secondChanged)
	require.NotEqual(t, firstIdentity, secondIdentity)
	require.Equal(t, secondIdentity, gjson.GetBytes(secondBody, "prompt_cache_key").String())
	expectedSecondIdentity, err := svc.resolveOpenAIPromptCacheIdentity(c, &secondAccount, "client-cache-key")
	require.NoError(t, err)
	require.Equal(t, expectedSecondIdentity, secondIdentity)
	require.True(t, isOpenAIAlignedPromptCacheIdentityForAccount(c, &secondAccount, secondIdentity))
	require.False(t, isOpenAIAlignedPromptCacheIdentityForAccount(c, &firstAccount, secondIdentity))

	thirdBody, thirdIdentity, thirdChanged, err := svc.ensureOpenAIResponsesPromptCacheIdentity(
		c, &secondAccount, secondBody, secondIdentity, "gpt-5.6",
	)
	require.NoError(t, err)
	require.False(t, thirdChanged)
	require.Equal(t, secondIdentity, thirdIdentity)
	require.Equal(t, secondBody, thirdBody)
}

func TestEnsureOpenAIResponsesPromptCacheIdentityRecognizesCanonicalCodexSessionHeader(t *testing.T) {
	c := newOpenAIPromptCacheIdentityTestContext(t, 7)
	c.Request.Header.Set(codexSessionIDHeader, "official-session")
	svc := &OpenAIGatewayService{}

	normalized, identity, changed, err := svc.ensureOpenAIResponsesPromptCacheIdentity(c, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, []byte(`{"model":"gpt-5.6-sol","input":"hello"}`), "", "gpt-5.6-sol")

	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, generateSessionUUID(isolateOpenAISessionID(7, "official-session")), identity)
	require.Equal(t, identity, gjson.GetBytes(normalized, "prompt_cache_key").String())
	require.Equal(t, "official-session", explicitOpenAIHeaderSessionID(c))
}

func TestEnsureOpenAIResponsesPromptCacheIdentityContentFallbackStableAcrossTurns(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	first := []byte(`{"model":"gpt-5.6-sol","instructions":"be helpful","input":[{"type":"message","role":"user","content":"hello"}]}`)
	later := []byte(`{"model":"gpt-5.6-sol","instructions":"be helpful","input":[{"type":"message","role":"user","content":"hello"},{"type":"message","role":"assistant","content":"hi"},{"type":"message","role":"user","content":"next"}]}`)

	_, firstIdentity, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(newOpenAIPromptCacheIdentityTestContext(t, 9), account, first, "", "gpt-5.6-sol")
	require.NoError(t, err)
	_, laterIdentity, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(newOpenAIPromptCacheIdentityTestContext(t, 9), account, later, "", "gpt-5.6-sol")
	require.NoError(t, err)
	require.NotEmpty(t, firstIdentity)
	require.Equal(t, firstIdentity, laterIdentity)
}

func TestEnsureOpenAIResponsesPromptCacheIdentitySkipsModelOnlyFallback(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.6-sol"}`)

	normalized, identity, changed, err := svc.ensureOpenAIResponsesPromptCacheIdentity(newOpenAIPromptCacheIdentityTestContext(t, 11), &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, body, "", "gpt-5.6-sol")

	require.NoError(t, err)
	require.False(t, changed)
	require.Empty(t, identity)
	require.JSONEq(t, string(body), string(normalized))
}

func TestNormalizeOpenAIPromptCacheControlsForAccount(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_options":{"mode":"extended","ttl":"24h"},"prompt_cache_retention":"24h"}`)

	t.Run("gpt-5.6 platform api key preserves", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		normalized, changed, err := normalizeOpenAIPromptCacheControlsForAccount(body, account, "gpt-5.6-sol")
		require.NoError(t, err)
		require.True(t, changed)
		require.True(t, gjson.GetBytes(normalized, "prompt_cache_options").Exists())
		require.False(t, gjson.GetBytes(normalized, "prompt_cache_retention").Exists())
	})

	t.Run("older platform model removes", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		normalized, changed, err := normalizeOpenAIPromptCacheControlsForAccount(body, account, "gpt-5.5")
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, gjson.GetBytes(normalized, "prompt_cache_options").Exists())
	})

	t.Run("oauth removes", func(t *testing.T) {
		account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		normalized, changed, err := normalizeOpenAIPromptCacheControlsForAccount(body, account, "gpt-5.6-sol")
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, gjson.GetBytes(normalized, "prompt_cache_options").Exists())
	})
}

func TestAlignOpenAICodexThreadHeaders(t *testing.T) {
	headers := make(http.Header)
	headers.Set("thread-id", "thread-123")
	headers.Set("x-client-request-id", "stale")

	alignOpenAICodexThreadHeaders(headers)

	require.Equal(t, "thread-123", headers.Get("x-client-request-id"))
}

func TestOpenAIResponsesCompactPromptCacheFinalization(t *testing.T) {
	tests := []struct {
		name            string
		model           string
		account         Account
		wantOptions     bool
		wantUpstreamURL string
	}{
		{
			name:  "gpt-5.6 platform api key preserves options",
			model: "gpt-5.6-sol",
			account: Account{
				ID: 1, Name: "platform-api-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
			},
			wantOptions:     true,
			wantUpstreamURL: openaiPlatformAPIURL + "/compact",
		},
		{
			name:  "older platform api key removes options",
			model: "gpt-5.5",
			account: Account{
				ID: 2, Name: "platform-api-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
			},
			wantOptions:     false,
			wantUpstreamURL: openaiPlatformAPIURL + "/compact",
		},
		{
			name:  "chatgpt oauth removes options",
			model: "gpt-5.6-sol",
			account: Account{
				ID: 3, Name: "chatgpt-oauth", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
				Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account"},
			},
			wantOptions:     false,
			wantUpstreamURL: chatgptCodexURL + "/compact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"` + tt.model + `","input":[{"type":"message","role":"user","content":"compact me"}],"prompt_cache_key":"client-compact-cache","prompt_cache_options":{"mode":"extended","ttl":"24h"},"store":true,"stream":true}`)
			normalizedBody, changed, err := normalizeOpenAICompactRequestBody(body)
			require.NoError(t, err)
			require.True(t, changed)

			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(normalizedBody))
			c.Request.Header.Set(codexSessionIDHeader, "client-compact-cache")
			c.Request.Header.Set("thread-id", "thread-compact-1")
			c.Request.Header.Set("x-client-request-id", "stale-request-id")
			c.Set("api_key", &APIKey{ID: 77})
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop"}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}

			_, _ = svc.Forward(context.Background(), c, &tt.account, normalizedBody)

			require.NotNil(t, upstream.lastReq)
			require.Equal(t, tt.wantUpstreamURL, upstream.lastReq.URL.String())
			identity := gjson.GetBytes(upstream.lastBody, "prompt_cache_key").String()
			require.NotEmpty(t, identity)
			require.Equal(t, identity, upstream.lastReq.Header.Get(codexSessionIDHeader))
			require.Equal(t, identity, upstream.lastReq.Header.Get("session_id"))
			expectedThreadID := "thread-compact-1"
			if tt.account.UsesOpenAICodexProtocol() {
				expectedThreadID = scopeCodexAccountIdentityValue(&tt.account, 77, "thread", expectedThreadID)
			}
			require.Equal(t, expectedThreadID, upstream.lastReq.Header.Get("thread-id"))
			require.Equal(t, expectedThreadID, upstream.lastReq.Header.Get("x-client-request-id"))
			require.Equal(t, tt.wantOptions, gjson.GetBytes(upstream.lastBody, "prompt_cache_options").Exists())
		})
	}
}
