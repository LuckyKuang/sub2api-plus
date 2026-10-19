//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Oracle: official openai/codex 770ed6dcea40, core/src/client.rs build_websocket_headers
// and codex-api/src/endpoint/responses.rs stream_request: request ID equals thread ID.
// Parent references in responses_metadata.rs identify that same parent's thread.
func TestCodexOfficialThreadRelationshipsOnWire(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, mode := range []string{"off", "device", "session", "full"} {
			for _, passthrough := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/%t", kind, mode, passthrough), func(t *testing.T) {
					svc := &OpenAIGatewayService{cfg: codexTransportTestConfig()}
					account := &Account{ID: 71, Platform: PlatformOpenAI, Type: kind,
						Credentials: map[string]any{"api_key": "fixture", "access_token": "fixture", "chatgpt_account_id": "owner-71"},
						Extra:       map[string]any{CodexFingerprintModeExtraKey: mode}}
					// Legacy stored generic overrides cannot sever a live parent link.
					account.Credentials[credKeyHeaderOverrideEnabled] = true
					account.Credentials[credKeyHeaderOverrides] = map[string]any{"x-codex-parent-thread-id": "foreign-parent", "x-client-request-id": "foreign-request"}
					capture := func(thread, parent string) []http.Header {
						c := newOpenAIPromptCacheIdentityTestContext(t, 42)
						c.Request.Header.Set("thread-id", thread)
						c.Request.Header.Set("session-id", thread)
						c.Request.Header.Set("x-client-request-id", thread)
						metadata := map[string]any{"thread_id": thread}
						if parent != "" {
							metadata["parent_thread_id"] = parent
							c.Request.Header.Set("x-codex-parent-thread-id", parent)
						}
						encoded, err := json.Marshal(metadata)
						require.NoError(t, err)
						c.Request.Header.Set("x-codex-turn-metadata", string(encoded))
						body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello","prompt_cache_key":"conversation"}`)
						_, err = svc.prepareCodexFingerprintMap(context.Background(), c, account, map[string]any{})
						require.NoError(t, err)
						return captureCodexProtocolHeaders(t, svc, c, account, body, passthrough)
					}
					parent := capture("parent-thread", "")
					child := capture("child-thread", "parent-thread")
					for transport := range parent {
						require.Equal(t, parent[transport].Get("thread-id"), parent[transport].Get("x-client-request-id"))
						require.Equal(t, child[transport].Get("thread-id"), child[transport].Get("x-client-request-id"))
						if mode == "off" || mode == "device" || kind == AccountTypeAPIKey {
							require.Equal(t, parent[transport].Get("thread-id"), child[transport].Get("x-codex-parent-thread-id"))
							require.Equal(t, parent[transport].Get("thread-id"), gjson.Get(child[transport].Get("x-codex-turn-metadata"), "parent_thread_id").String())
						}
					}
					require.Equal(t, child[0].Get("thread-id"), child[1].Get("thread-id"), "HTTP and WS use the same thread projection")
				})
			}
		}
	}
}

func TestCodexOfficialRetiredSynchronizationCannotChangeVersion(t *testing.T) {
	// Requirement: a saved automatic-sync result is inert even before migration runs.
	repo := &openAIIdentitySettingRepoStub{values: map[string]string{
		"openai_codex_client_version_synced":     "9.9.9",
		"openai_codex_version_auto_sync_enabled": "true",
	}}
	settings := &SettingService{settingRepo: repo}
	require.Equal(t, "0.158.0", settings.GetOpenAICodexClientVersion(context.Background()))
	for _, tc := range []struct{ candidate, want string }{
		{"", "0.158.0"}, {"0.200.1", "0.200.1"}, {"bad", "0.158.0"}, {"0.1.0", "0.158.0"},
	} {
		repo.values[SettingKeyOpenAICodexClientVersion] = tc.candidate
		settings.InvalidateOpenAICodexClientVersionCache()
		require.Equal(t, tc.want, settings.GetOpenAICodexClientVersion(context.Background()))
	}
}

func TestCodexOfficialForeignAPIKeyCandidatesNeverReachWire(t *testing.T) {
	// Product contract: API-key and OAuth use Codex, with account > global >
	// compiled priority. A stale foreign preset must not override that choice.
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, tc := range []struct{ name, accountUA, globalUA, want string }{
			{"compiled", "", "", "codex_cli_rs/0.158.0 (Ubuntu 24.04; x86_64) xterm-256color"},
			{"global", "", "codex_cli_rs/0.158.0 (Ubuntu 22.4.0; x86_64) xterm-256color", "codex_cli_rs/0.158.0 (Ubuntu 22.4.0; x86_64) xterm-256color"},
			{"account", "codex_cli_rs/0.158.0 (Ubuntu 20.4.0; x86_64) xterm-256color", "codex_cli_rs/0.158.0 (Ubuntu 22.4.0; x86_64) xterm-256color", "codex_cli_rs/0.158.0 (Ubuntu 20.4.0; x86_64) xterm-256color"},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				config := `{"defaults":{"openai:apikey":"grok"}}`
				settings := NewSettingService(&outboundIdentityTestRepo{values: map[string]string{SettingKeyOutboundIdentity: config, SettingKeyOpenAICodexUserAgent: tc.globalUA}}, nil)
				account := &Account{ID: 71, Platform: PlatformOpenAI, Type: kind, Credentials: map[string]any{
					"api_key": "fixture", "access_token": "fixture", "chatgpt_account_id": "owner-71", "user_agent": tc.accountUA,
					outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"},
				}}
				svc := &OpenAIGatewayService{cfg: codexTransportTestConfig(), settingService: settings}
				c := newOpenAIPromptCacheIdentityTestContext(t, 42)
				// The production builder's trusted preset resolver sees stale persisted defaults.
				c.Request = c.Request.WithContext(outboundidentity.WithResolver(c.Request.Context(), settings.resolveOutboundIdentityKey))
				headers := captureCodexProtocolHeaders(t, svc, c, account, []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello"}`), false)
				for _, h := range headers {
					require.Equal(t, tc.want, h.Get("User-Agent"))
					require.Equal(t, "codex_cli_rs", h.Get("Originator"))
					require.Equal(t, "0.158.0", h.Get("Version"))
					require.Empty(t, h.Get("x-grok-client-version"))
				}
			})
		}
	}
}
