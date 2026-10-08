//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newGrokCacheTestContext(apiKeyID int64) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if apiKeyID > 0 {
		c.Set("api_key", &APIKey{ID: apiKeyID, Group: &Group{Platform: PlatformGrok}})
	}
	return c
}

func TestGrokPreviousResponseSessionSeed(t *testing.T) {
	require.Equal(t, "grok-prev-resp:resp_abc123", grokPreviousResponseSessionSeed([]byte(`{"previous_response_id":"resp_abc123"}`)))
	require.Empty(t, grokPreviousResponseSessionSeed([]byte(`{"previous_response_id":"msg_abc123"}`)))
	require.Empty(t, grokPreviousResponseSessionSeed([]byte(`{"previous_response_id":""}`)))
	require.Empty(t, grokPreviousResponseSessionSeed([]byte(`{}`)))
}

func TestResolveGrokCacheIdentityUsesPreviousResponseIDWhenNoOtherSeed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(301)
	// No prompt_cache_key / headers / reusable prefix — only previous_response_id.
	body := []byte(`{"model":"grok","input":[{"role":"user","content":"follow up"}],"previous_response_id":"resp_chain_001"}`)
	got := resolveGrokCacheIdentity(c, body, "", "grok-4.5")
	require.NotEmpty(t, got)

	// Same previous_response_id → same identity (model already in isolated seed).
	again := resolveGrokCacheIdentity(c, body, "", "grok-4.5")
	require.Equal(t, got, again)

	// Different model → different identity (model scope).
	otherModel := resolveGrokCacheIdentity(c, body, "", "grok-4.3")
	require.NotEqual(t, got, otherModel)

	// prompt_cache_key still wins over previous_response_id.
	withCache := []byte(`{"model":"grok","prompt_cache_key":"client-session","previous_response_id":"resp_chain_001","input":[{"role":"user","content":"x"}]}`)
	cacheID := resolveGrokCacheIdentity(c, withCache, "", "grok-4.5")
	require.NotEmpty(t, cacheID)
	require.NotEqual(t, got, cacheID)
}

func TestResolveGrokCacheIdentityStableAcrossAppendOnlyTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(101)
	round1 := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"role":"user","content":"first question"}]}`)
	round2 := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"role":"user","content":"first question"},{"role":"assistant","content":"first answer"},{"role":"user","content":"second question"}]}`)

	first := resolveGrokCacheIdentity(c, round1, "", "grok-4.5")
	second := resolveGrokCacheIdentity(c, round2, "", "grok-4.5")

	require.NotEmpty(t, first)
	require.Len(t, first, 36)
	require.Equal(t, first, second)
}

func TestResolveGrokCacheIdentityStableAcrossIndependentPromptsWithSamePrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(102)
	firstBody := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"lookup"}],"input":[{"role":"user","content":"Question A"}]}`)
	secondBody := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"lookup"}],"input":[{"role":"user","content":"Question B"}]}`)

	first := resolveGrokCacheIdentity(c, firstBody, "", "grok-4.5")
	second := resolveGrokCacheIdentity(c, secondBody, "", "grok-4.5")

	require.NotEmpty(t, first)
	require.Equal(t, first, second)
}

func TestResolveGrokCacheIdentityStablePrefixIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseBody := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"lookup"}],"input":[{"role":"system","content":"System A"},{"role":"user","content":"Question A"}]}`)
	differentInstructions := []byte(`{"model":"grok","instructions":"be detailed","tools":[{"type":"function","name":"lookup"}],"input":[{"role":"system","content":"System A"},{"role":"user","content":"Question B"}]}`)
	differentSystem := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"lookup"}],"input":[{"role":"system","content":"System B"},{"role":"user","content":"Question B"}]}`)
	differentTools := []byte(`{"model":"grok","instructions":"be concise","tools":[{"type":"function","name":"search"}],"input":[{"role":"system","content":"System A"},{"role":"user","content":"Question B"}]}`)

	base := resolveGrokCacheIdentity(newGrokCacheTestContext(103), baseBody, "", "grok-4.5")
	require.NotEqual(t, base, resolveGrokCacheIdentity(newGrokCacheTestContext(104), baseBody, "", "grok-4.5"))
	require.NotEqual(t, base, resolveGrokCacheIdentity(newGrokCacheTestContext(103), baseBody, "", "grok-4.3"))
	require.NotEqual(t, base, resolveGrokCacheIdentity(newGrokCacheTestContext(103), differentInstructions, "", "grok-4.5"))
	require.NotEqual(t, base, resolveGrokCacheIdentity(newGrokCacheTestContext(103), differentSystem, "", "grok-4.5"))
	require.NotEqual(t, base, resolveGrokCacheIdentity(newGrokCacheTestContext(103), differentTools, "", "grok-4.5"))
}

func TestResolveGrokCacheIdentityFallsBackWhenStablePrefixIsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(105)
	firstBody := []byte(`{"model":"grok","tools":[],"input":"Question A"}`)
	secondBody := []byte(`{"model":"grok","tools":[],"input":"Question B"}`)

	first := resolveGrokCacheIdentity(c, firstBody, "", "grok-4.5")
	second := resolveGrokCacheIdentity(c, secondBody, "", "grok-4.5")

	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second)
}

func TestResolveGrokCacheIdentitySkipsUnanchoredFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(106)
	tests := [][]byte{
		[]byte(`{"model":"grok"}`),
		[]byte(`{"model":"grok","messages":[{"role":"assistant","content":"answer"}]}`),
		[]byte(`{"model":"grok","messages":[{"role":"user","content":""}]}`),
		[]byte(`{"model":"grok","input":"  "}`),
	}

	for _, body := range tests {
		require.Empty(t, resolveGrokCacheIdentity(c, body, "", "grok-4.5"))
	}
}

func TestResolveGrokCacheIdentityIsolatesAPIKeyAndMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok","input":"same prompt"}`)

	base := resolveGrokCacheIdentity(newGrokCacheTestContext(201), body, "", "grok-4.5")
	otherTenant := resolveGrokCacheIdentity(newGrokCacheTestContext(202), body, "", "grok-4.5")
	otherModel := resolveGrokCacheIdentity(newGrokCacheTestContext(201), body, "", "grok-4.3")

	require.NotEmpty(t, base)
	require.NotEqual(t, base, otherTenant)
	require.NotEqual(t, base, otherModel)
}

func TestResolveGrokCacheIdentityUsesAndIsolatesNativeConversationHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(301)
	c.Request.Header.Set(grokConversationIDHeader, "raw-native-conversation")
	body1 := []byte(`{"model":"grok","input":"one"}`)
	body2 := []byte(`{"model":"grok","input":"different body that must not replace the explicit session"}`)

	first := resolveGrokCacheIdentity(c, body1, "body-cache-key", "grok-4.5")
	second := resolveGrokCacheIdentity(c, body2, "another-body-cache-key", "grok-4.5")

	require.Equal(t, "raw-native-conversation", (&OpenAIGatewayService{}).ExtractSessionID(c, body1))
	require.Equal(t, first, second)
	require.NotEqual(t, "raw-native-conversation", first)
	require.NotContains(t, first, "raw-native-conversation")
}

func TestResolveGrokCacheIdentityOfficialBodyKeyPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok","prompt_cache_key":"body-key","input":"hi"}`)
	c := newGrokCacheTestContext(401)
	c.Request.Header.Set(grokConversationIDHeader, "grok-key")
	c.Request.Header.Set("conversation_id", "conversation-key")
	c.Request.Header.Set("session_id", "session-key")

	got := resolveGrokCacheIdentity(c, body, "explicit-argument", "grok-4.5")
	onlySession := newGrokCacheTestContext(401)
	// responses.rs: explicit prompt_cache_key outranks conversation/session seeds.
	want := resolveGrokCacheIdentity(onlySession, []byte(`{"model":"grok","prompt_cache_key":"body-key","input":"unrelated"}`), "", "grok-4.5")

	require.Equal(t, want, got)
}

func TestResolveGrokCacheIdentityIDEHeaderPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok","prompt_cache_key":"body-key","input":"hi"}`)
	headers := []struct {
		name  string
		value string
	}{
		{name: openCodeSessionAffinityHeader, value: "opencode-affinity"},
		{name: openCodeSessionIDHeader, value: "opencode-session-id"},
		{name: openCodeNativeSessionHeader, value: "opencode-native-session"},
		{name: codeBuddyConversationHeader, value: "codebuddy-conversation"},
		{name: grokConversationIDHeader, value: "grok-conversation"},
	}

	c := newGrokCacheTestContext(402)
	for _, header := range headers {
		c.Request.Header.Set(header.name, header.value)
	}
	for _, header := range headers {
		got := resolveGrokCacheIdentity(c, body, "explicit-argument", "grok-4.5")
		onlyCurrent := newGrokCacheTestContext(402)
		onlyCurrent.Request.Header.Set(header.name, header.value)
		if header.name == grokConversationIDHeader {
			// prompt_cache_key beats X-Grok-Conv-Id: verify the identity is
			// derived from the body key, not the per-call conv label.
			withOnlyBody := newGrokCacheTestContext(402)
			want := resolveGrokCacheIdentity(withOnlyBody, body, "", "grok-4.5")
			require.Equal(t, want, got, header.name)
			withOnlyConv := newGrokCacheTestContext(402)
			withOnlyConv.Request.Header.Set(header.name, header.value)
			convOnly := resolveGrokCacheIdentity(withOnlyConv, []byte(`{"model":"grok","input":"unrelated"}`), "", "grok-4.5")
			require.NotEqual(t, convOnly, got, "body key must not be shadowed by X-Grok-Conv-Id")
		} else {
			want := resolveGrokCacheIdentity(onlyCurrent, body, "", "grok-4.5")
			require.Equal(t, want, got, header.name)
		}
		c.Request.Header.Del(header.name)
	}
}

// TestResolveGrokCacheIdentitySideCallSharesParentCacheKey locks in the
// grok-build side-call fix: recap-style side-calls (turn-summary /
// title-refresh) send a fresh X-Grok-Conv-Id label but the parent session id
// as body prompt_cache_key. The derived identity must follow the body key so
// side-calls share the main turn's server-side cache prefix instead of
// replaying the full conversation at full price on every call.
func TestResolveGrokCacheIdentitySideCallSharesParentCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parentSession := "6f1c2f46-0f5e-4f9d-9d4e-2f0f1c3d5b7a"

	mainTurn := newGrokCacheTestContext(910)
	mainTurn.Request.Header.Set(grokConversationIDHeader, parentSession)
	mainIdentity := resolveGrokCacheIdentity(mainTurn, []byte(`{"model":"grok-4.6","input":"main conversation"}`), "", "grok-4.6")
	require.NotEmpty(t, mainIdentity)

	sideCall := newGrokCacheTestContext(910)
	sideCall.Request.Header.Set(grokConversationIDHeader, "turn-summary-"+uuidNew())
	sideIdentity := resolveGrokCacheIdentity(sideCall, []byte(`{"model":"grok-4.6","prompt_cache_key":"`+parentSession+`","input":"summary replay"}`), "", "grok-4.6")
	require.Equal(t, mainIdentity, sideIdentity,
		"side-call with parent prompt_cache_key must share the main turn cache identity")

	titleRefresh := newGrokCacheTestContext(910)
	titleRefresh.Request.Header.Set(grokConversationIDHeader, "title-refresh-"+uuidNew())
	titleIdentity := resolveGrokCacheIdentity(titleRefresh, []byte(`{"model":"grok-4.6","prompt_cache_key":"`+parentSession+`","input":"title replay"}`), "", "grok-4.6")
	require.Equal(t, mainIdentity, titleIdentity)
}

func uuidNew() string {
	return fmt.Sprintf("%08x-%04x-4%03x-9%03x-%012x", 0x1234, 0x5678, 0x9abc, 0xdef0, 0x1234567890ab)
}

func TestExplicitGrokCacheSeedPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(403)
	headers := []struct {
		name  string
		value string
	}{
		{name: claudeCodeSessionHeader, value: "claude-session"},
		{name: "session_id", value: "generic-session"},
		{name: "conversation_id", value: "generic-conversation"},
		{name: openCodeSessionAffinityHeader, value: "opencode-affinity"},
		{name: openCodeSessionIDHeader, value: "opencode-session-id"},
		{name: openCodeNativeSessionHeader, value: "opencode-native-session"},
		{name: codeBuddyConversationHeader, value: "codebuddy-conversation"},
		{name: grokConversationIDHeader, value: "grok-conversation"},
	}
	for _, header := range headers {
		c.Request.Header.Set(header.name, header.value)
	}

	body := []byte(`{"model":"grok","prompt_cache_key":"body-key","input":"hi"}`)
	for _, header := range headers {
		require.Equal(t, "body-key", explicitGrokCacheSeed(c, body, "explicit-argument"), header.name)
		c.Request.Header.Del(header.name)
	}

	require.Equal(t, "body-key", explicitGrokCacheSeed(c, body, "explicit-argument"))
	require.Equal(t, "explicit-argument", explicitGrokCacheSeed(c, []byte(`{"model":"grok"}`), "explicit-argument"))
}

func TestResolveGrokCacheIdentityIDEHeadersAreStableIsolatedAndOpaque(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name   string
		header string
	}{
		{name: "OpenCode affinity", header: openCodeSessionAffinityHeader},
		{name: "OpenCode session ID", header: openCodeSessionIDHeader},
		{name: "OpenCode native session", header: openCodeNativeSessionHeader},
		{name: "CodeBuddy conversation", header: codeBuddyConversationHeader},
	}

	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawSession := "raw-ide-session-" + tt.name
			apiKeyID := int64(800 + index)
			c := newGrokCacheTestContext(apiKeyID)
			c.Request.Header.Set(tt.header, rawSession)
			firstBody := []byte(`{"model":"grok","input":"first turn"}`)
			secondBody := []byte(`{"model":"grok","input":"different second turn"}`)

			first := resolveGrokCacheIdentity(c, firstBody, "", "grok-4.5")
			second := resolveGrokCacheIdentity(c, secondBody, "", "grok-4.5")
			require.NotEmpty(t, first)
			require.Equal(t, first, second)
			require.NotEqual(t, rawSession, first)
			require.NotContains(t, first, rawSession)

			otherTenant := newGrokCacheTestContext(apiKeyID + 100)
			otherTenant.Request.Header.Set(tt.header, rawSession)
			require.NotEqual(t, first, resolveGrokCacheIdentity(otherTenant, firstBody, "", "grok-4.5"))
			require.NotEqual(t, first, resolveGrokCacheIdentity(c, firstBody, "", "grok-4.3"))
		})
	}
}

func TestOpenCodeResponsesHeaderAndBodyCacheSignalsConverge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const rawSession = "opencode-session-42"
	c := newGrokCacheTestContext(901)
	c.Request.Header.Set(openCodeSessionAffinityHeader, rawSession)
	c.Request.Header.Set(openCodeSessionIDHeader, rawSession)
	firstBody := []byte(`{"model":"grok","prompt_cache_key":"opencode-session-42","input":"first turn"}`)
	secondBody := []byte(`{"model":"grok","prompt_cache_key":"opencode-session-42","input":"different second turn"}`)

	first := resolveGrokCacheIdentity(c, firstBody, "", "grok-4.5")
	second := resolveGrokCacheIdentity(c, secondBody, "", "grok-4.5")
	bodyOnly := resolveGrokCacheIdentity(newGrokCacheTestContext(901), secondBody, "", "grok-4.5")
	require.NotEmpty(t, first)
	require.Equal(t, first, second)
	require.Equal(t, first, bodyOnly)

	patched, err := applyGrokResponsesCacheIdentity(secondBody, second)
	require.NoError(t, err)
	require.Equal(t, second, gjson.GetBytes(patched, "prompt_cache_key").String())
	headers := make(http.Header)
	applyGrokConversationHeaders(headers, grokConversationSnapshot{ConversationID: second})
	require.Equal(t, second, headers.Get(grokConversationIDHeader))
	require.NotContains(t, string(patched), rawSession)
}

func TestResolveGrokCacheIdentityPrefersClaudeCodeSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(701)
	c.Request.Header.Set(claudeCodeSessionHeader, "cc-session-abc")
	c.Request.Header.Set("session_id", "session-key")
	body1 := []byte(`{"model":"grok","input":"turn-1"}`)
	body2 := []byte(`{"model":"grok","input":"turn-2-different-user-text"}`)

	first := resolveGrokCacheIdentity(c, body1, "", "grok-4.5")
	second := resolveGrokCacheIdentity(c, body2, "", "grok-4.5")
	require.NotEmpty(t, first)
	require.Equal(t, first, second, "same Claude Code session must keep stable Grok cache identity across turns")

	// metadata.user_id JSON form used by Claude Code clients
	metaBody := []byte(`{"model":"grok","metadata":{"user_id":"{\"session_id\":\"meta-session-xyz\"}"},"input":"hi"}`)
	metaOnly := newGrokCacheTestContext(702)
	metaID := resolveGrokCacheIdentity(metaOnly, metaBody, "", "grok-4.5")
	require.NotEmpty(t, metaID)
	metaBody2 := []byte(`{"model":"grok","metadata":{"user_id":"{\"session_id\":\"meta-session-xyz\"}"},"input":"later turn"}`)
	require.Equal(t, metaID, resolveGrokCacheIdentity(metaOnly, metaBody2, "", "grok-4.5"))
}

func TestResolveGrokCacheIdentityFailsClosedWithoutAPIKeyContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newGrokCacheTestContext(0)
	c.Request.Header.Set(grokConversationIDHeader, "native-session")

	require.Empty(t, resolveGrokCacheIdentity(c, []byte(`{"model":"grok","input":"hi"}`), "", "grok-4.5"))
	require.Empty(t, resolveGrokCacheIdentity(nil, []byte(`{"model":"grok","prompt_cache_key":"key"}`), "key", "grok-4.5"))
}

func TestGrokConversationHeaderIsScopedToGrokRequestScheduling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"grok","prompt_cache_key":"body-session","input":"hi"}`)

	grokContext := newGrokCacheTestContext(601)
	grokContext.Request.Header.Set(grokConversationIDHeader, "native-grok-session")
	require.Equal(t, "native-grok-session", (&OpenAIGatewayService{}).ExtractSessionID(grokContext, body))

	openAIContext := newGrokCacheTestContext(601)
	openAIContext.Set("api_key", &APIKey{ID: 601, Group: &Group{Platform: PlatformOpenAI}})
	openAIContext.Request.Header.Set(grokConversationIDHeader, "must-be-ignored")
	require.Equal(t, "body-session", (&OpenAIGatewayService{}).ExtractSessionID(openAIContext, body))

	withoutGrokHeader := newGrokCacheTestContext(601)
	withoutGrokHeader.Set("api_key", &APIKey{ID: 601, Group: &Group{Platform: PlatformOpenAI}})
	require.Equal(t,
		(&OpenAIGatewayService{}).GenerateSessionHash(withoutGrokHeader, body),
		(&OpenAIGatewayService{}).GenerateSessionHash(openAIContext, body),
	)
}

func TestApplyGrokRequestMetadataUsesTrustedRequestValues(t *testing.T) {
	headers := http.Header{
		"X-Grok-Req-Id":         {"pinned"},
		"X-Grok-Model-Override": {"wrong-model"},
		"X-Grok-Session-Id":     {"wrong-session"},
	}
	applyGrokRequestMetadata(headers, []byte(`{"model":"grok-4.7"}`), grokConversationSnapshot{ConversationID: "trusted-session", SessionID: "trusted-session", RootSessionID: "root-session-123"}, " user-42 ")

	require.NotEmpty(t, headers.Get("x-grok-req-id"))
	require.NotEqual(t, "pinned", headers.Get("x-grok-req-id"))
	requestID, err := uuid.Parse(headers.Get("x-grok-req-id"))
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), requestID.Version())
	require.Equal(t, "grok-4.7", headers.Get("x-grok-model-override"))
	require.Equal(t, "trusted-session", headers.Get("x-grok-conv-id"))
	require.Equal(t, "trusted-session", headers.Get("x-grok-session-id"))
	require.Equal(t, "user-42", headers.Get("x-grok-user-id"))
	agentID, err := uuid.Parse(headers.Get("x-grok-agent-id"))
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), agentID.Version())
	require.Equal(t, grokAgentID, agentID.String())
	require.Equal(t,
		"111fc242-925b-5a7d-826e-2974daad239f",
		headers.Get("x-grok-conv-group-id"),
	)

	previousRequestID := headers.Get("x-grok-req-id")
	applyGrokRequestMetadata(headers, []byte(`{}`), grokConversationSnapshot{}, "")
	require.NotEqual(t, previousRequestID, headers.Get("x-grok-req-id"))
	require.Equal(t, grokAgentID, headers.Get("x-grok-agent-id"))
	for _, name := range []string{"x-grok-model-override", "x-grok-conv-id", "x-grok-session-id", "x-grok-conv-group-id", "x-grok-user-id"} {
		require.Empty(t, headers.Get(name), name)
	}
}

func TestDoGrokNativeResponsesJSONAppliesSamplerMetadata(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"output":[]}`)),
	}}
	svc := &GatewayService{httpUpstream: upstream}
	account := &Account{
		ID: 73, Platform: PlatformGrok, Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{"api_key": "key"},
	}

	_, err := svc.DoGrokNativeResponsesJSON(context.Background(), account, []byte(`{"model":"grok-4.7","input":"search"}`))
	require.NoError(t, err)
	require.NotEmpty(t, upstream.lastReq.Header.Get("x-grok-req-id"))
	require.NotEmpty(t, upstream.lastReq.Header.Get("x-grok-agent-id"))
	require.Equal(t, "grok-4.7", upstream.lastReq.Header.Get("x-grok-model-override"))
	require.Equal(t, HTTPUpstreamProfileGrok, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
}

func TestResolveGrokCacheIdentityConcurrentDeterminism(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const workers = 50
	body := []byte(`{"model":"grok","messages":[{"role":"system","content":"stable"},{"role":"user","content":"hello"}]}`)
	identities := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			identities <- resolveGrokCacheIdentity(newGrokCacheTestContext(501), body, "", "grok-4.5")
		}()
	}
	wg.Wait()
	close(identities)

	var first string
	for identity := range identities {
		if first == "" {
			first = identity
		}
		require.Equal(t, first, identity)
	}
	require.NotEmpty(t, first)
}

// Compact is not a conversation turn and must not gain cache or tool declarations.
func TestGrokCompactRequestSkipsCacheIdentityAndNativeTools(t *testing.T) {
	c := newGrokCacheTestContext(701)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	body := []byte(`{"model":"grok","input":"compact this","prompt_cache_key":"raw-client-key"}`)
	identity := resolveGrokCacheIdentity(c, body, "", "grok-4.5")
	patched, err := applyGrokResponsesCacheIdentity(body, identity)
	require.NoError(t, err)
	require.Empty(t, identity)
	for _, field := range []string{"prompt_cache_key", "tools", "tool_choice"} {
		require.False(t, gjson.GetBytes(patched, field).Exists(), field)
	}
}
