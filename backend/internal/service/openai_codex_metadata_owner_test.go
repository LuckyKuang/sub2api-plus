//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexMetadataOwnerTokenCache struct {
	OpenAITokenCache
	acquire func(context.Context) string
}

func (c *codexMetadataOwnerTokenCache) GetAccessToken(ctx context.Context, _ string) (string, error) {
	return c.acquire(ctx), nil
}

type codexMetadataOwnerWire struct {
	headers http.Header
	body    []byte
	err     error
}

func codexMetadataOwnerAccount(principal string, passthrough bool) *Account {
	installation := "11111111-1111-4111-8111-111111111111"
	if principal == "new" {
		installation = "22222222-2222-4222-8222-222222222222"
	}
	return &Account{
		ID: 9700, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": principal + "-token", "refresh_token": principal + "-refresh",
			"chatgpt_account_id": principal + "-principal", "chatgpt_user_id": principal + "-user",
		},
		Extra: map[string]any{
			CodexFingerprintModeExtraKey: "device", "openai_device_id": installation,
			"openai_oauth_passthrough": passthrough,
		},
	}
}

func codexMetadataOwnerInput(t *testing.T, messages bool) ([]byte, string) {
	t.Helper()
	metadata := map[string]any{
		"session_id": "legacy-session", "thread_id": "legacy-thread", "parent_thread_id": "legacy-parent",
		"forked_from_thread_id": "legacy-parent", "turn_id": "legacy-turn", "parent_turn_id": "legacy-turn",
		"root_turn_id": "legacy-turn", "window_id": "legacy-thread:4", "context_window_id": "legacy-context-window",
		"turn_started_at_unix_ms": int64(1730000000000), "installation_id": "legacy-installation",
	}
	encoded, err := json.Marshal(metadata)
	require.NoError(t, err)
	clientMetadata := map[string]any{
		"session_id": "legacy-session", "thread_id": "legacy-thread", "parent_thread_id": "legacy-parent",
		"forked_from_thread_id": "legacy-parent", "turn_id": "legacy-turn", "parent_turn_id": "legacy-turn",
		"root_turn_id": "legacy-turn", "x-codex-window-id": "legacy-thread:4",
		"context_window_id": "legacy-context-window", "x-codex-turn-metadata": string(encoded),
	}
	payload := map[string]any{
		"model": "gpt-5.4", "instructions": "be helpful", "stream": false,
		"input": "hello", "client_metadata": clientMetadata,
	}
	if messages {
		delete(payload, "input")
		delete(payload, "instructions")
		payload["max_tokens"] = 32
		payload["messages"] = []any{map[string]any{"role": "user", "content": "hello"}}
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	return body, string(encoded)
}

// OUTBOUND_IDENTITY and OFFICIAL_AGENT_REQUEST_STATE require bearer, typed
// references and installation to belong to the acquired credential snapshot.
// An independently sent old-only request is the comparison oracle; no expected
// namespace is calculated using the typed mapper under test.
func TestCodexHTTPMetadataUsesAcquiredOwnerAfterSelectedRowRelink(t *testing.T) {
	for _, adapter := range []string{"forward", "passthrough", "chat-responses-bridge", "messages-bridge"} {
		t.Run(adapter, func(t *testing.T) {
			store := newTestOutboundIDStore()
			captures := make(chan codexMetadataOwnerWire, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wire := codexMetadataOwnerWire{headers: r.Header.Clone()}
				wire.body, wire.err = io.ReadAll(r.Body)
				if wire.err == nil && r.Header.Get("Content-Encoding") == "zstd" {
					decoder, err := zstd.NewReader(nil)
					if err != nil {
						wire.err = err
					} else {
						wire.body, wire.err = decoder.DecodeAll(wire.body, nil)
						decoder.Close()
					}
				}
				captures <- wire
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"fixture stop"}}`)
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			body, turnMetadata := codexMetadataOwnerInput(t, adapter == "messages-bridge")
			send := func(principal string, relink bool) codexMetadataOwnerWire {
				t.Helper()
				selected := codexMetadataOwnerAccount(principal, adapter == "passthrough")
				upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					redirected := request.Clone(request.Context())
					redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
					return server.Client().Do(redirected)
				}}
				svc := newOpenAIRejectedFieldTestService(nil)
				svc.httpUpstream = upstream
				acquisitions := 0
				svc.openAITokenProvider = NewOpenAITokenProvider(nil, &codexMetadataOwnerTokenCache{acquire: func(ctx context.Context) string {
					acquisitions++
					token := selected.GetOpenAIAccessToken()
					recordRequestCredentialOwner(ctx, selected, token)
					if relink {
						newGrant := codexMetadataOwnerAccount("new", adapter == "passthrough")
						selected.Credentials, selected.Extra = newGrant.Credentials, newGrant.Extra
					}
					return token
				}}, nil)
				c := newOpenAIRejectedFieldTestContext(body)
				c.Set("api_key", &APIKey{ID: 9701})
				c.Request.Header.Set("session-id", "legacy-session")
				c.Request.Header.Set("thread-id", "legacy-thread")
				c.Request.Header.Set("x-client-request-id", "legacy-thread")
				c.Request.Header.Set("x-codex-parent-thread-id", "legacy-parent")
				c.Request.Header.Set("x-codex-window-id", "legacy-thread:4")
				c.Request.Header.Set("x-codex-turn-metadata", turnMetadata)
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				ctx, cancel := context.WithTimeout(WithOutboundIDStore(t.Context(), store), 5*time.Second)
				defer cancel()
				switch adapter {
				case "chat-responses-bridge":
					c.Request.URL.Path = "/v1/chat/completions"
					_, err = svc.ForwardAsChatCompletions(ctx, c, selected, body, "", "")
				case "messages-bridge":
					c.Request.URL.Path = "/v1/messages"
					_, err = svc.ForwardAsAnthropic(ctx, c, selected, body, "", "")
				default:
					_, err = svc.Forward(ctx, c, selected, body)
				}
				require.Error(t, err, "the fixture ends the operation after capturing the actual HTTP request")
				require.Equal(t, 1, acquisitions)
				select {
				case wire := <-captures:
					require.NoError(t, wire.err)
					return wire
				default:
					t.Fatal("forwarding must reach the HTTP server")
					return codexMetadataOwnerWire{}
				}
			}

			oldOnly := send("old", false)
			raced := send("old", true)
			newOnly := send("new", false)
			require.Equal(t, "Bearer old-token", raced.headers.Get("Authorization"))
			require.Equal(t, "old-principal", raced.headers.Get("Chatgpt-Account-Id"))
			for _, name := range []string{"thread-id", "x-client-request-id", "x-codex-parent-thread-id", "x-codex-window-id", "x-codex-turn-metadata"} {
				require.NotEmpty(t, oldOnly.headers.Get(name), name)
				require.Equal(t, oldOnly.headers.Get(name), raced.headers.Get(name), name+" must use the acquired old owner")
				require.NotEqual(t, newOnly.headers.Get(name), raced.headers.Get(name), name+" must not use the new grant")
			}
			switch adapter {
			case "chat-responses-bridge":
				// OPENAI_RESPONSES.md retains adapter cache policy; Responses-shaped
				// Chat input without a cache seed does not manufacture a session.
				for _, wire := range []codexMetadataOwnerWire{oldOnly, raced, newOnly} {
					require.Empty(t, wire.headers.Get("session-id"))
					require.False(t, gjson.GetBytes(wire.body, "prompt_cache_key").Exists())
				}
			case "messages-bridge":
				// Messages aligns its compatibility session header to the tenant's
				// logical cache, which remains stable across credential failover.
				// The OAuth bridge carries it only in the session header; the
				// Responses body does not carry prompt_cache_key on this path.
				cacheIdentity := oldOnly.headers.Get("session-id")
				require.NotEmpty(t, cacheIdentity)
				for _, wire := range []codexMetadataOwnerWire{oldOnly, raced, newOnly} {
					require.Equal(t, cacheIdentity, wire.headers.Get("session-id"))
					require.False(t, gjson.GetBytes(wire.body, "prompt_cache_key").Exists())
				}
			default:
				require.NotEmpty(t, oldOnly.headers.Get("session-id"))
				require.Equal(t, oldOnly.headers.Get("session-id"), raced.headers.Get("session-id"))
				require.NotEqual(t, newOnly.headers.Get("session-id"), raced.headers.Get("session-id"))
			}
			require.Equal(t, raced.headers.Get("thread-id"), raced.headers.Get("x-client-request-id"))
			require.Equal(t, raced.headers.Get("thread-id")+":4", raced.headers.Get("x-codex-window-id"))
			// Every adapter retains the canonical metadata header. It proves
			// installation and execution ownership even when the adapter drops
			// client_metadata from its converted body or uses a cache session.
			racedTurnMetadata := gjson.Parse(raced.headers.Get("x-codex-turn-metadata"))
			newTurnMetadata := gjson.Parse(newOnly.headers.Get("x-codex-turn-metadata"))
			require.True(t, racedTurnMetadata.IsObject())
			require.Equal(t, "11111111-1111-4111-8111-111111111111", racedTurnMetadata.Get("installation_id").String())
			require.Equal(t, "22222222-2222-4222-8222-222222222222", newTurnMetadata.Get("installation_id").String())
			for _, field := range []string{"session_id", "thread_id", "parent_thread_id", "forked_from_thread_id", "turn_id", "parent_turn_id", "root_turn_id", "window_id", "context_window_id"} {
				require.NotEmpty(t, racedTurnMetadata.Get(field).String(), field)
				require.NotEqual(t, newTurnMetadata.Get(field).String(), racedTurnMetadata.Get(field).String(), field+" must retain the acquired owner in every adapter")
			}
			require.Equal(t, raced.headers.Get("thread-id"), racedTurnMetadata.Get("thread_id").String())
			require.Equal(t, raced.headers.Get("x-codex-parent-thread-id"), racedTurnMetadata.Get("parent_thread_id").String())
			require.Equal(t, int64(1730000000000), racedTurnMetadata.Get("turn_started_at_unix_ms").Int())
			for _, name := range []string{"User-Agent", "Originator", "Version"} {
				require.Equal(t, oldOnly.headers.Get(name), raced.headers.Get(name), name)
			}
			require.Empty(t, raced.headers.Get("x-codex-installation-id"))
			if adapter != "messages-bridge" {
				oldMetadata := gjson.GetBytes(oldOnly.body, "client_metadata")
				racedMetadata := gjson.GetBytes(raced.body, "client_metadata")
				newMetadata := gjson.GetBytes(newOnly.body, "client_metadata")
				require.True(t, racedMetadata.IsObject())
				require.JSONEq(t, oldMetadata.Raw, racedMetadata.Raw)
				require.Equal(t, "11111111-1111-4111-8111-111111111111", racedMetadata.Get("x-codex-installation-id").String())
				for _, field := range []string{"session_id", "thread_id", "parent_thread_id", "turn_id", "x-codex-window-id", "context_window_id"} {
					require.NotEmpty(t, racedMetadata.Get(field).String(), field)
					require.NotEqual(t, newMetadata.Get(field).String(), racedMetadata.Get(field).String(), field)
				}
				require.Equal(t, raced.headers.Get("thread-id"), racedMetadata.Get("thread_id").String())
				require.Equal(t, raced.headers.Get("x-codex-parent-thread-id"), racedMetadata.Get("parent_thread_id").String())
				require.Equal(t, uuid.Version(7), uuid.MustParse(racedMetadata.Get("thread_id").String()).Version())
				require.JSONEq(t, raced.headers.Get("x-codex-turn-metadata"), racedMetadata.Get("x-codex-turn-metadata").String())
			}
		})
	}
}

func TestCodexMetadataMapperRejectsAmbiguousSelectedAndShadowOwner(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "shadow"}[shadow], func(t *testing.T) {
			owner := codexMetadataOwnerAccount("old", false)
			ctx := WithOutboundIdentityScope(WithOutboundIDStore(t.Context(), newTestOutboundIDStore()), nil)
			recordRequestCredentialOwner(ctx, owner, "old-token")
			relinked := codexMetadataOwnerAccount("new", false)
			relinked.Credentials["access_token"] = "old-token"
			recordRequestCredentialOwner(ctx, relinked, "old-token")
			selected := relinked
			if shadow {
				selected = &Account{ID: 9702, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID}
			}
			ctx = withRequestCredentialOwner(ctx, selected, "old-token")
			for _, source := range []*Account{selected, relinked} {
				body, _ := codexMetadataOwnerInput(t, false)
				var parsed map[string]any
				require.NoError(t, json.Unmarshal(body, &parsed))
				changed, err := applyCodexAccountIdentityClientMetadataMap(ctx, parsed, source, 9701)
				require.Error(t, err, "an ambiguous acquisition cannot fall back to the selected row or a staged shadow parent")
				require.False(t, changed)
				unchanged, err := json.Marshal(parsed)
				require.NoError(t, err)
				require.JSONEq(t, string(body), string(unchanged))
				headers := http.Header{"Thread-Id": {"legacy-thread"}}
				require.Error(t, applyCodexAccountIdentityHeaders(ctx, headers, source, 9701))
				require.Equal(t, "legacy-thread", headers.Get("thread-id"))
			}
		})
	}
}

func TestCodexMetadataShadowParentUsesAcquiredGrantWithoutReread(t *testing.T) {
	store := newTestOutboundIDStore()
	owner := codexMetadataOwnerAccount("old", false)
	shadow := &Account{ID: 9702, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID}
	ctx := WithOutboundIdentityScope(WithOutboundIDStore(t.Context(), store), nil)
	recordRequestCredentialOwner(ctx, owner, "old-token")
	ctx = withRequestCredentialOwner(ctx, shadow, "old-token")
	relinked := codexMetadataOwnerAccount("new", false)
	original, _ := codexMetadataOwnerInput(t, false)
	var expected map[string]any
	require.NoError(t, json.Unmarshal(original, &expected))
	_, err := applyCodexAccountIdentityClientMetadataMap(WithOutboundIDStore(t.Context(), store), expected, owner, 9701)
	require.NoError(t, err)
	for _, source := range []*Account{shadow, relinked} {
		var body map[string]any
		require.NoError(t, json.Unmarshal(original, &body))
		_, err := applyCodexAccountIdentityClientMetadataMap(ctx, body, source, 9701)
		require.NoError(t, err)
		require.Equal(t, expected, body)
		fingerprintOwner, err := (&OpenAIGatewayService{}).resolveCodexFingerprintAccount(ctx, source)
		require.NoError(t, err, "the acquired parent must not trigger a later repository lookup")
		require.Equal(t, "old-principal", fingerprintOwner.GetChatGPTAccountID())
		require.Equal(t, "old-token", fingerprintOwner.GetOpenAIAccessToken())
	}
}
