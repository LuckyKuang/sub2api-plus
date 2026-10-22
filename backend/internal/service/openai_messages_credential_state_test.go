//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Real Responses requests prove that stored server state follows the acquired
// grant, while an API-key replacement or OAuth relink receives full history.
func TestMessagesContinuationAndTurnStateUseAcquiredCredentialOwner(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			captured := make(chan codexMetadataOwnerWire, 4)
			responseIndex := 0
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
				captured <- wire
				responseIndex++
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set(openAICodexTurnStateHeader, fmt.Sprintf("opaque-state-%d", responseIndex))
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_owner_%d\",\"status\":\"completed\",\"model\":\"gpt-5.4\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", responseIndex)
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			svc := newOpenAIRejectedFieldTestService(nil)
			// API-key destination validation must reach this local wire fixture.
			svc.cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				redirected := request.Clone(request.Context())
				redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
				return server.Client().Do(redirected)
			}}
			newAccount := func(principal, token string) *Account {
				account := codexMetadataOwnerAccount(principal, false)
				account.Type = accountType
				account.Credentials["access_token"] = token
				account.Credentials["api_key"] = token
				account.Credentials["base_url"] = server.URL
				if accountType == AccountTypeAPIKey {
					// Imported API-key metadata cannot substitute for a verified
					// OAuth principal or hide a bearer replacement.
					account.Credentials["chatgpt_account_id"] = "same-unverified-claim"
				}
				return account
			}
			body := []byte(`{"model":"gpt-5.4","max_tokens":16,"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"latest"}]}`)
			for index, stage := range []struct {
				principal, token string
				relink           bool
			}{
				{principal: "old", token: "initial-token"},
				{principal: "old", token: "initial-token"},
				{principal: "old", token: "refreshed-token", relink: true},
				{principal: "new", token: "replacement-token"},
			} {
				// API keys have no refresh: the third call retains the exact old
				// grant, and only the last call replaces it on the same local row.
				if accountType == AccountTypeAPIKey && index == 2 {
					stage.token = "initial-token"
					stage.relink = false
				}
				selected := newAccount(stage.principal, stage.token)
				if accountType == AccountTypeOAuth {
					svc.openAITokenProvider = NewOpenAITokenProvider(nil, &codexMetadataOwnerTokenCache{acquire: func(ctx context.Context) string {
						recordRequestCredentialOwner(ctx, selected, stage.token)
						if stage.relink {
							replacement := newAccount("new", "replacement-token")
							// The acquired OAuth grant retains its full replay and
							// opaque turn state after the selected row changes type.
							selected.Type = AccountTypeAPIKey
							selected.Credentials = replacement.Credentials
						}
						return stage.token
					}}, nil)
				}
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request.URL.Path = "/v1/messages"
				c.Set("api_key", &APIKey{ID: 8871})
				ctx, cancel := context.WithTimeout(WithOutboundIDStore(t.Context(), newTestOutboundIDStore()), 5*time.Second)
				result, err := svc.ForwardAsAnthropic(ctx, c, selected, body, "credential-owner-cache", "gpt-5.4")
				cancel()
				require.NoError(t, err)
				require.NotNil(t, result)
				wire := <-captured
				require.NoError(t, wire.err)
				require.Equal(t, "Bearer "+stage.token, wire.headers.Get("Authorization"))
				if accountType == AccountTypeOAuth {
					require.Equal(t, stage.principal+"-principal", wire.headers.Get("ChatGPT-Account-Id"))
					if index == 1 || index == 2 {
						require.Equal(t, fmt.Sprintf("opaque-state-%d", index), wire.headers.Get(openAICodexTurnStateHeader))
					} else {
						require.Empty(t, wire.headers.Get(openAICodexTurnStateHeader))
					}
					require.False(t, gjson.GetBytes(wire.body, "previous_response_id").Exists())
					require.Contains(t, string(wire.body), "first", "OAuth keeps full replay")
				} else if index == 1 || index == 2 {
					require.Equal(t, fmt.Sprintf("resp_owner_%d", index), gjson.GetBytes(wire.body, "previous_response_id").String())
					require.NotContains(t, string(wire.body), `"text":"first"`, "the same API-key grant can send only its latest turn")
				} else {
					require.False(t, gjson.GetBytes(wire.body, "previous_response_id").Exists(), "a replacement API key cannot use the previous grant's response")
					require.Contains(t, string(wire.body), "first", "replacement receives the complete replay")
				}
				require.Contains(t, strings.ToLower(string(wire.body)), "latest")
			}
		})
	}
}

func TestCodexBearerStateOwnerIgnoresUnverifiedOAuthClaims(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			account := &Account{ID: 51, Platform: PlatformOpenAI, Type: accountType,
				Credentials: map[string]any{"api_key": "old-key", "access_token": "old-key", "chatgpt_account_id": "unverified-a", "chatgpt_user_id": "unverified-user-a"}}
			first := openAICodexStateOwner(t.Context(), account)
			require.NotEmpty(t, first)
			account.Credentials["chatgpt_account_id"], account.Credentials["chatgpt_user_id"] = "unverified-b", "unverified-user-b"
			require.Equal(t, first, openAICodexStateOwner(t.Context(), account), "unverified claims cannot alter a bearer owner's identity")
			account.Credentials["api_key"], account.Credentials["access_token"] = "replacement-key", "replacement-key"
			require.NotEqual(t, first, openAICodexStateOwner(t.Context(), account), "a bearer replacement cannot inherit the prior state")
		})
	}
}

func TestMessagesStateKeepsUnsharedShadowTenantKey(t *testing.T) {
	owner := codexMetadataOwnerAccount("old", false)
	shadow := &Account{ID: 9799, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID}
	ctx := WithOutboundIdentityScope(t.Context(), nil)
	recordRequestCredentialOwner(ctx, owner, "old-token")
	ownerCtx := withRequestCredentialOwner(ctx, owner, "old-token")
	shadowCtx := withRequestCredentialOwner(ctx, shadow, "old-token")
	c := newOpenAIRejectedFieldTestContext(nil)
	c.Set("api_key", &APIKey{ID: 8871})
	service := &OpenAIGatewayService{}
	service.bindOpenAICompatSessionTurnState(ownerCtx, c, owner, "same-cache", "parent-state")
	require.Empty(t, service.getOpenAICompatSessionTurnState(shadowCtx, c, shadow, "same-cache"), "a shared credential alone does not merge unshared selected-route caches")
	service.bindOpenAICompatSessionTurnState(shadowCtx, c, shadow, "same-cache", "shadow-state")
	require.Equal(t, "parent-state", service.getOpenAICompatSessionTurnState(ownerCtx, c, owner, "same-cache"))
	require.Equal(t, "shadow-state", service.getOpenAICompatSessionTurnState(shadowCtx, c, shadow, "same-cache"))
}
