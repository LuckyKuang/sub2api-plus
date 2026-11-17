//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"fmt"
	coderws "github.com/coder/websocket"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexTypedIdentityPreservesTimeAndReferenceGraph(t *testing.T) {
	ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
	account := turnStateAccount(17, "principal")
	account.Extra = map[string]any{CodexFingerprintModeExtraKey: "off"}
	const thread = "01912345-6789-7abc-8def-0123456789ab"
	const turn = "01912345-7000-7fed-9234-123456789abc"
	const contextWindow = "01912345-6999-7123-b456-789012345678"
	metadata := map[string]any{"thread_id": thread, "turn_id": turn, "root_turn_id": turn, "parent_turn_id": turn, "parent_thread_id": thread, "forked_from_thread_id": thread, "window_id": thread + ":3", "context_window_id": contextWindow, "window_number": 3, "turn_started_at_unix_ms": int64(1722875543552)}
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	body := map[string]any{"client_metadata": map[string]any{"thread_id": thread, "turn_id": turn, "parent_turn_id": turn, "root_turn_id": turn, "x-codex-window-id": thread + ":3", "x-codex-turn-metadata": string(encoded)}}
	changed, err := applyCodexAccountIdentityClientMetadataMap(ctx, body, account, 9)
	require.NoError(t, err)
	require.True(t, changed)
	cm, ok := body["client_metadata"].(map[string]any)
	require.True(t, ok)
	threadID, ok := cm["thread_id"].(string)
	require.True(t, ok)
	mappedThread, err := uuid.Parse(threadID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), mappedThread.Version())
	require.Equal(t, uuid.RFC4122, mappedThread.Variant())
	originalThread := uuid.MustParse(thread)
	require.Equal(t, originalThread[:6], mappedThread[:6], "v7 preserves original millisecond time")
	require.NotEqual(t, originalThread, mappedThread)
	require.Equal(t, cm["turn_id"], cm["parent_turn_id"])
	require.Equal(t, cm["turn_id"], cm["root_turn_id"])
	require.Equal(t, mappedThread.String()+":3", cm["x-codex-window-id"])
	var embedded map[string]any
	metadataJSON, ok := cm["x-codex-turn-metadata"].(string)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal([]byte(metadataJSON), &embedded))
	require.Equal(t, cm["thread_id"], embedded["parent_thread_id"])
	require.Equal(t, cm["thread_id"], embedded["forked_from_thread_id"])
	require.Equal(t, cm["turn_id"], embedded["turn_id"])
	require.Equal(t, float64(1722875543552), embedded["turn_started_at_unix_ms"])
	again, err := resolveCodexAccountIdentityValue(ctx, account, 9, "turn", turn)
	require.NoError(t, err)
	require.Equal(t, cm["turn_id"], again, "same logical turn survives another generation")
	other, err := resolveCodexAccountIdentityValue(ctx, turnStateAccount(18, "other"), 9, "turn", turn)
	require.NoError(t, err)
	require.NotEqual(t, again, other)
}

func TestCodexConvergenceDoesNotInventTurnsOrResetWindows(t *testing.T) {
	for _, mode := range []string{"device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
			account := turnStateAccount(17, "principal")
			account.Extra = map[string]any{CodexFingerprintModeExtraKey: mode}
			body := map[string]any{"client_metadata": map[string]any{"thread_id": "01912345-6789-7abc-8def-0123456789ab", "x-codex-window-id": "01912345-6789-7abc-8def-0123456789ab:2"}}
			_, err := applyCodexAccountIdentityClientMetadataMap(ctx, body, account, 9)
			require.NoError(t, err)
			ids, err := resolveCodexFingerprintIDsContext(ctx, account, http.Header{}, account.GetCodexFingerprintMode(), 9)
			require.NoError(t, err)
			applyCodexFingerprintClientMetadata(body, ids)
			cm, ok := body["client_metadata"].(map[string]any)
			require.True(t, ok)
			require.NotContains(t, cm, "turn_id")
			threadID, ok := cm["thread_id"].(string)
			require.True(t, ok)
			require.Equal(t, threadID+":2", cm["x-codex-window-id"])
			installationID, ok := cm["x-codex-installation-id"].(string)
			require.True(t, ok)
			installation := uuid.MustParse(installationID)
			require.Equal(t, uuid.Version(4), installation.Version())
			h := http.Header{}
			applyCodexFingerprintHeaders(h, ids)
			require.Empty(t, h.Get("x-codex-installation-id"))
		})
	}
}

func TestCodexIdentityMappingStorageFailureDoesNotFallBack(t *testing.T) {
	_, err := resolveCodexAccountIdentityValue(WithOutboundIDStore(context.Background(), nil), turnStateAccount(17, "principal"), 9, "thread", "legacy-session")
	require.Error(t, err)
}

func TestNativeCodexCacheOmissionAndCustomSessionIndependence(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := turnStateAccount(17, "principal")
	for _, body := range []string{`{"input":"hello"}`, `{"input":"hello","prompt_cache_key":"custom-cache"}`} {
		c := newOpenAIRejectedFieldTestContext([]byte(body))
		c.Request.Header.Set("session-id", "01912345-6789-7abc-8def-0123456789ab")
		output, key, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(c, account, []byte(body), "fallback", "gpt-5.5")
		require.NoError(t, err)
		h := http.Header{}
		require.NoError(t, svc.alignOpenAIUpstreamSessionIdentityFromBody(c, account, h, output))
		require.Equal(t, uuid.Version(7), uuid.MustParse(h.Get("session-id")).Version())
		if !gjson.Get(body, "prompt_cache_key").Exists() {
			require.False(t, gjson.GetBytes(output, "prompt_cache_key").Exists())
			require.Empty(t, key)
		} else {
			require.NotEmpty(t, key)
			require.NotEqual(t, key, h.Get("session-id"))
			b := turnStateAccount(18, "other")
			outputB, keyB, _, err := svc.ensureOpenAIResponsesPromptCacheIdentity(c, b, []byte(body), "fallback", "gpt-5.5")
			require.NoError(t, err)
			require.NotEmpty(t, outputB)
			require.Equal(t, key, keyB, "logical cache remains stable across credential failover")
		}
	}
}

// Exercise the real HTTP forwarding boundary for native OAuth, passthrough and
// API keys. Each fixture states protocol IDs independently of the implementation.
func TestCodexForwardWireKeepsIDGraphAndNativeCacheSemantics(t *testing.T) {
	for _, accountType := range []string{"oauth", "oauth-passthrough", "apikey"} {
		for _, withCache := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cache=%v", accountType, withCache), func(t *testing.T) {
				const session = "01912345-6789-7abc-8def-0123456789ab"
				const thread = "01912345-7000-7fed-9234-123456789abc"
				const turn = "01912345-7777-7123-b456-789012345678"
				account := turnStateAccount(871, "wire-owner")
				account.Extra = map[string]any{CodexFingerprintModeExtraKey: "device"}
				if accountType == "oauth-passthrough" {
					account.Extra["openai_oauth_passthrough"] = true
				}
				if accountType == "apikey" {
					account.Type = AccountTypeAPIKey
					account.Credentials = map[string]any{"api_key": "wire-key"}
				}
				metadata := map[string]any{"session_id": session, "thread_id": thread, "turn_id": turn, "parent_turn_id": turn, "root_turn_id": turn, "window_id": thread + ":4", "turn_started_at_unix_ms": int64(1730000000000)}
				encoded, err := json.Marshal(metadata)
				require.NoError(t, err)
				payload := map[string]any{"model": "gpt-5.6-sol", "instructions": "be helpful", "input": "hello", "stream": false, "client_metadata": map[string]any{"session_id": session, "thread_id": thread, "turn_id": turn, "x-codex-window-id": thread + ":4", "x-codex-turn-metadata": string(encoded)}}
				if withCache {
					payload["prompt_cache_key"] = "custom-account-independent-cache"
				}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				send := func() (*http.Request, []byte) {
					upstream := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"fixture stop"}}`)}
					svc := newOpenAIRejectedFieldTestService(upstream)
					c := newOpenAIRejectedFieldTestContext(body)
					c.Set("api_key", &APIKey{ID: 901})
					c.Request.Header.Set("session-id", session)
					c.Request.Header.Set("thread-id", thread)
					c.Request.Header.Set("x-client-request-id", thread)
					c.Request.Header.Set("x-codex-window-id", thread+":4")
					c.Request.Header.Set("x-codex-turn-metadata", string(encoded))
					c.Request.Header["x-CoDeX-InStAlLaTiOn-Id"] = []string{"must-not-escape"}
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					_, err = svc.Forward(t.Context(), c, account, body)
					require.Error(t, err)
					require.NotNil(t, upstream.lastReq)
					return upstream.lastReq, upstream.lastBody
				}
				request, sent := send()
				again, sentAgain := send()
				for _, h := range []string{"session-id", "thread-id", "x-client-request-id", "x-codex-window-id", "x-codex-turn-metadata"} {
					require.Equal(t, request.Header.Get(h), again.Header.Get(h), h)
				}
				for key := range request.Header {
					require.False(t, strings.EqualFold(key, "x-codex-installation-id"), "unsupported direct installation carrier")
				}
				require.Equal(t, uuid.Version(7), uuid.MustParse(request.Header.Get("session-id")).Version())
				require.Equal(t, request.Header.Get("thread-id"), request.Header.Get("x-client-request-id"))
				require.Equal(t, request.Header.Get("thread-id")+":4", request.Header.Get("x-codex-window-id"))
				cm := gjson.GetBytes(sent, "client_metadata")
				require.Equal(t, request.Header.Get("session-id"), cm.Get("session_id").String())
				require.Equal(t, request.Header.Get("thread-id"), cm.Get("thread_id").String())
				require.Equal(t, cm.Get("turn_id").String(), gjson.GetBytes(sentAgain, "client_metadata.turn_id").String())
				var headerMetadata, bodyMetadata map[string]any
				require.NoError(t, json.Unmarshal([]byte(request.Header.Get("x-codex-turn-metadata")), &headerMetadata))
				require.NoError(t, json.Unmarshal([]byte(cm.Get("x-codex-turn-metadata").String()), &bodyMetadata))
				require.Equal(t, headerMetadata, bodyMetadata)
				require.Equal(t, float64(1730000000000), headerMetadata["turn_started_at_unix_ms"])
				require.Equal(t, headerMetadata["turn_id"], headerMetadata["parent_turn_id"])
				require.Equal(t, headerMetadata["turn_id"], headerMetadata["root_turn_id"])
				require.Equal(t, withCache, gjson.GetBytes(sent, "prompt_cache_key").Exists())
				if withCache {
					require.NotEqual(t, request.Header.Get("session-id"), gjson.GetBytes(sent, "prompt_cache_key").String())
				}
			})
		}
	}
}

func TestCodexForwardMappingFailurePreventsUpstreamWrite(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello","client_metadata":{"thread_id":"legacy-thread"}}`)
	upstream := &httpUpstreamRecorder{}
	svc := newOpenAIRejectedFieldTestService(upstream)
	_, err := svc.Forward(WithOutboundIDStore(t.Context(), nil), newOpenAIRejectedFieldTestContext(body), turnStateAccount(993, "owner"), body)
	require.Error(t, err)
	require.Nil(t, upstream.lastReq)
}

func TestCodexAPIKeyRelinkChangesExecutionButPreservesDeviceScope(t *testing.T) {
	a := &Account{ID: 800, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key-a"}}
	b := &Account{ID: 800, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "key-b"}}
	for _, kind := range []string{"installation", "session", "thread", "turn", "context-window", "window"} {
		left, err := resolveCodexAccountIdentityValue(t.Context(), a, 7, kind, "legacy")
		require.NoError(t, err)
		right, err := resolveCodexAccountIdentityValue(t.Context(), b, 7, kind, "legacy")
		require.NoError(t, err)
		if kind == "installation" {
			require.Equal(t, left, right)
		} else {
			require.NotEqual(t, left, right, kind)
		}
	}
}

func TestCodexWSMappingFailureIsInternalAndHasNoUpstreamWrite(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			upstream := newStagedPassthroughConn()
			account := passthroughLifecycleAccount()
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = mode
			svc := newPassthroughLifecycleService(cfg, upstream)
			ctx := WithOutboundIDStore(t.Context(), nil)
			server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
			defer server.Close()
			conn, _, err := coderws.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			require.NoError(t, conn.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello","client_metadata":{"thread_id":"legacy-thread"}}`)))
			select {
			case err := <-serverErr:
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, coderws.StatusInternalError, closeErr.StatusCode())
			case <-time.After(3 * time.Second):
				t.Fatal("identity preparation failure did not end WS admission")
			}
			select {
			case payload := <-upstream.writes:
				t.Fatalf("unexpected upstream write: %d bytes", len(payload))
			default:
			}
		})
	}
}
