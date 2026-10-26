//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type http401RefreshEvidenceRepo struct {
	*refreshAPIAccountRepo
	fence    http401CredentialFenceRepo
	cooldown http401CredentialCooldownRepo
}

func (r *http401RefreshEvidenceRepo) SetCredentialErrorIfUnchanged(ctx context.Context, expectedOwner *Account, message string) (bool, error) {
	r.fence.current = r.account
	return r.fence.SetCredentialErrorIfUnchanged(ctx, expectedOwner, message)
}

func (r *http401RefreshEvidenceRepo) SetCredentialTempUnschedulableIfUnchanged(ctx context.Context, expectedOwner *Account, until time.Time, message string) (bool, error) {
	r.cooldown.current = r.account
	return r.cooldown.SetCredentialTempUnschedulableIfUnchanged(ctx, expectedOwner, until, message)
}

func TestHTTP401AcquiredCredentialGeneration(t *testing.T) {
	for _, acquisition := range []string{"pre-expiry-refresh", "force-refresh", "cache-winner"} {
		for _, rotated := range []bool{false, true} {
			name := acquisition + "/unchanged"
			if rotated {
				name = acquisition + "/concurrent-login"
			}
			t.Run(name, func(t *testing.T) {
				selected := newOpenAIAuthRecoveryTestAccount()
				if acquisition == "pre-expiry-refresh" {
					selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
				}
				repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(selected)}}
				executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "sent-T1", "refresh_token": "refresh-S1", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}
				provider := NewOpenAITokenProvider(repo, nil, nil)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
				if acquisition == "cache-winner" {
					repo.account.Credentials["access_token"] = "sent-T1"
					repo.account.Credentials["refresh_token"] = "refresh-S1"
					provider = NewOpenAITokenProvider(repo, &concurrentOpenAITokenCache{token: "sent-T1", force: true}, nil)
				}
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				var token string
				var err error
				if acquisition == "force-refresh" {
					token, err = provider.ForceRefreshToken(ctx, selected, "old-token")
				} else {
					token, err = provider.GetAccessToken(ctx, selected)
				}
				require.NoError(t, err)
				require.Equal(t, "sent-T1", token)
				sentOwner := snapshotRequestCredentialAccount(repo.account)
				ctx = withRequestCredentialOwner(ctx, selected, token)
				blocker := &runtimeBlockRecorder{}
				rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
				rl.SetAccountRuntimeBlocker(blocker)
				gateway := &OpenAIGatewayService{accountRepo: repo, rateLimitService: rl}
				wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
					require.Equal(t, "Bearer sent-T1", req.Header.Get("Authorization"))
					if rotated {
						repo.account = snapshotRequestCredentialAccount(repo.account)
						repo.account.Credentials["access_token"] = "new-login-T2"
						repo.account.Credentials["refresh_token"] = "new-login-S2"
					}
					return cnRecoveryResponse(http.StatusUnauthorized, `{"error":{"code":"token_revoked"}}`), nil
				}}
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
				require.NoError(t, err)
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := wire.Do(req, "", selected.ID, 1)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.True(t, gateway.handleOpenAIAccountUpstreamError(ctx, selected, resp.StatusCode, resp.Header, []byte(`{"error":{"code":"token_revoked"}}`)))
				require.Equal(t, 1, repo.fence.casCalls)
				require.Equal(t, sentOwner.Credentials, repo.fence.attempted.Credentials, "the rejected grant was acquired after selection")
				if rotated {
					require.Equal(t, StatusActive, repo.account.Status)
					require.Equal(t, "new-login-T2", repo.account.GetOpenAIAccessToken())
					require.Empty(t, blocker.accounts)
				} else {
					require.Equal(t, StatusError, repo.account.Status)
					require.Len(t, blocker.accounts, 1)
				}
			})
		}
	}
}

// The second response belongs to the force-refreshed bearer, including when
// the original account snapshot is still used to build both requests.
func TestHTTP401ForceRefreshRetryBindsSentGeneration(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, rotated := range []bool{false, true} {
			name := map[bool]string{false: "forward", true: "passthrough"}[passthrough] + map[bool]string{false: "/unchanged", true: "/concurrent-login"}[rotated]
			t.Run(name, func(t *testing.T) {
				selected := newOpenAIAuthRecoveryTestAccount()
				repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(selected)}}
				executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "retry-T1", "refresh_token": "refresh-S1", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}
				provider := NewOpenAITokenProvider(repo, nil, nil)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
				var sent []string
				var sentOwner *Account
				wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
					sent = append(sent, req.Header.Get("Authorization"))
					if len(sent) == 1 {
						return cnRecoveryResponse(http.StatusUnauthorized, `{"error":{"code":"invalid_token"}}`), nil
					}
					sentOwner = snapshotRequestCredentialAccount(repo.account)
					if rotated {
						repo.account = snapshotRequestCredentialAccount(repo.account)
						repo.account.Credentials["access_token"] = "new-login-T2"
						repo.account.Credentials["refresh_token"] = "new-login-S2"
					}
					return cnRecoveryResponse(http.StatusUnauthorized, `{"error":{"code":"token_revoked"}}`), nil
				}}
				cfg := rawChatCompletionsTestConfig()
				blocker := &runtimeBlockRecorder{}
				rl := NewRateLimitService(repo, nil, cfg, nil, nil)
				rl.SetAccountRuntimeBlocker(blocker)
				gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, openAITokenProvider: provider, httpUpstream: wire, rateLimitService: rl}
				body := []byte(`{"model":"gpt-5.6-sol","stream":true,"instructions":"test","input":"hello"}`)
				c := newOpenAIRejectedFieldTestContext(body)
				var err error
				if passthrough {
					_, err = gateway.forwardOpenAIPassthrough(context.Background(), c, selected, body, body, "gpt-5.6-sol", true, nil, true, time.Now())
				} else {
					_, err = gateway.Forward(context.Background(), c, selected, body)
				}
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.True(t, failover.ShouldRetryNextAccount())
				require.Equal(t, []string{"Bearer old-token", "Bearer retry-T1"}, sent)
				require.Equal(t, 1, executor.refreshCalls)
				require.Equal(t, 1, repo.fence.casCalls)
				require.Equal(t, sentOwner.Credentials, repo.fence.attempted.Credentials)
				require.False(t, c.Writer.Written())
				if rotated {
					require.Equal(t, StatusActive, repo.account.Status)
					require.Empty(t, blocker.accounts)
				} else {
					require.Equal(t, StatusError, repo.account.Status)
					require.Len(t, blocker.accounts, 1)
				}
			})
		}
	}
}

func TestHTTP401SharedScopeKeepsIndependentAttemptEvidence(t *testing.T) {
	selected := newOpenAIAuthRecoveryTestAccount()
	selected.Credentials["access_token"] = "turn-T1"
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	gateway := &OpenAIGatewayService{}
	identity := gateway.resolveOpenAIOutboundIdentity(ctx, selected)
	recordRequestCredentialOwner(ctx, selected, "turn-T1")
	oldAttempt := withRequestCredentialOwner(ctx, selected, "turn-T1")
	next := snapshotRequestCredentialAccount(selected)
	next.Credentials["access_token"], next.Credentials["refresh_token"] = "turn-T2", "next-refresh"
	recordRequestCredentialOwner(ctx, next, "turn-T2")
	newAttempt := withRequestCredentialOwner(ctx, selected, "turn-T2")
	require.Equal(t, identity, gateway.resolveOpenAIOutboundIdentity(newAttempt, next), "WS attempts retain their frozen outbound identity")
	repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(next)}
	rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	blocker := &runtimeBlockRecorder{}
	rl.SetAccountRuntimeBlocker(blocker)
	body := []byte(`{"error":{"code":"token_revoked"}}`)
	require.True(t, rl.HandleUpstreamError(oldAttempt, selected, http.StatusUnauthorized, nil, body))
	require.Equal(t, "turn-T1", repo.attempted.GetOpenAIAccessToken())
	require.Equal(t, StatusActive, repo.current.Status)
	require.Empty(t, blocker.accounts)
	require.True(t, rl.HandleUpstreamError(newAttempt, selected, http.StatusUnauthorized, nil, body))
	require.Equal(t, "turn-T2", repo.attempted.GetOpenAIAccessToken())
	require.Equal(t, StatusError, repo.current.Status)
	require.Len(t, blocker.accounts, 1)
}

func TestHTTP401SameTokenConflictingGrantsCannotBorrowEvidence(t *testing.T) {
	for _, newestFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-first", true: "new-first"}[newestFirst], func(t *testing.T) {
			old := newOpenAIAuthRecoveryTestAccount()
			newGrant := snapshotRequestCredentialAccount(old)
			newGrant.Credentials["refresh_token"] = "new-login-same-access-token"
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			first, second := old, newGrant
			if newestFirst {
				first, second = newGrant, old
			}
			recordRequestCredentialOwner(ctx, first, "old-token")
			firstAttempt := withRequestCredentialOwner(ctx, first, "old-token")
			recordRequestCredentialOwner(ctx, second, "old-token")
			ambiguousAttempt := withRequestCredentialOwner(ctx, second, "old-token")
			owner, ok := requestCredentialOwner(firstAttempt, first)
			require.True(t, ok)
			require.Equal(t, first.Credentials, owner.Credentials, "an already bound attempt remains immutable")
			_, ok = requestCredentialOwner(ambiguousAttempt, second)
			require.False(t, ok)
			repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(newGrant)}
			rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			blocker := &runtimeBlockRecorder{}
			rl.SetAccountRuntimeBlocker(blocker)
			require.True(t, rl.HandleUpstreamError(ambiguousAttempt, second, http.StatusUnauthorized, nil, []byte(`{"error":{"code":"token_revoked"}}`)))
			require.Zero(t, repo.casCalls)
			require.Equal(t, StatusActive, repo.current.Status)
			require.Empty(t, blocker.accounts)
		})
	}
}

func TestHTTP401CredentialEvidenceClonesNestedCredentials(t *testing.T) {
	owner := newOpenAIAuthRecoveryTestAccount()
	owner.Credentials["model_mapping"] = map[string]any{"nested": []any{map[string]any{"model": "original"}}}
	owner.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "codex", Headers: map[string]string{"x-runtime": "original"}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, owner, "old-token")
	bound := withRequestCredentialOwner(ctx, owner, "old-token")
	owner.Credentials["model_mapping"].(map[string]any)["nested"].([]any)[0].(map[string]any)["model"] = "mutated"
	owner.Credentials[outboundIdentityCredential].(OutboundIdentitySelection).Headers["x-runtime"] = "mutated"
	evidence, ok := requestCredentialOwner(bound, owner)
	require.True(t, ok)
	require.Equal(t, "original", evidence.Credentials["model_mapping"].(map[string]any)["nested"].([]any)[0].(map[string]any)["model"])
	require.Equal(t, "original", evidence.Credentials[outboundIdentityCredential].(OutboundIdentitySelection).Headers["x-runtime"])
	evidence.Credentials["model_mapping"].(map[string]any)["nested"].([]any)[0].(map[string]any)["model"] = "mutated-return"
	evidence, ok = requestCredentialOwner(bound, owner)
	require.True(t, ok)
	require.Equal(t, "original", evidence.Credentials["model_mapping"].(map[string]any)["nested"].([]any)[0].(map[string]any)["model"])
}

func TestCredentialFailureAccountEvidenceClonesNestedCredentials(t *testing.T) {
	owner := newOpenAIAuthRecoveryTestAccount()
	owner.Credentials["model_mapping"] = map[string]any{"nested": []any{map[string]any{"model": "original"}}}
	err := WithCredentialFailureAccount(NewAccountCredentialFailure("account_credential_invalid", nil), owner)
	owner.Credentials["model_mapping"].(map[string]any)["nested"].([]any)[0].(map[string]any)["model"] = "mutated"
	var failure *CredentialFailureError
	require.True(t, errors.As(err, &failure))
	require.Equal(t, "original", failure.account.Credentials["model_mapping"].(map[string]any)["nested"].([]any)[0].(map[string]any)["model"])
}

func TestHTTP401ModelsShadowUsesManifestCredentialSnapshot(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "concurrent-login"}[rotated], func(t *testing.T) {
			owner := newOpenAIAuthRecoveryTestAccount()
			parentID := owner.ID
			shadow := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID, Concurrency: 1}
			repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(owner)}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests++
				require.Equal(t, "Bearer old-token", req.Header.Get("Authorization"))
				if rotated {
					repo.current = snapshotRequestCredentialAccount(repo.current)
					repo.current.Credentials["access_token"] = "new-login-T2"
					repo.current.Credentials["refresh_token"] = "new-login-S2"
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":{"code":"token_revoked"}}`)
			}))
			defer server.Close()
			originalURL := chatgptCodexModelsURL
			chatgptCodexModelsURL = server.URL
			defer func() { chatgptCodexModelsURL = originalURL }()
			blocker := &runtimeBlockRecorder{}
			cfg := rawChatCompletionsTestConfig()
			rl := NewRateLimitService(repo, nil, cfg, nil, nil)
			rl.SetAccountRuntimeBlocker(blocker)
			gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, rateLimitService: rl}
			_, err := gateway.FetchCodexModelsManifest(context.Background(), shadow, "", "")
			require.Error(t, err)
			require.Equal(t, 1, requests)
			require.Equal(t, 1, repo.casCalls)
			require.Equal(t, owner.ID, repo.attempted.ID)
			require.Equal(t, owner.Credentials, repo.attempted.Credentials)
			if rotated {
				require.Equal(t, StatusActive, repo.current.Status)
				require.Empty(t, blocker.accounts)
			} else {
				require.Equal(t, StatusError, repo.current.Status)
				require.Len(t, blocker.accounts, 1)
			}
		})
	}
}

func TestHTTP401APIKeyPassthroughFailsOverBeforeClientOutput(t *testing.T) {
	selected := rawChatCompletionsTestAccount()
	selected.Status, selected.Schedulable = StatusActive, true
	selected.Extra = map[string]any{"openai_passthrough": true}
	repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(selected)}
	cfg := rawChatCompletionsTestConfig()
	blocker := &runtimeBlockRecorder{}
	rl := NewRateLimitService(repo, nil, cfg, nil, nil)
	rl.SetAccountRuntimeBlocker(blocker)
	wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer sk-test", req.Header.Get("Authorization"))
		return cnRecoveryResponse(http.StatusUnauthorized, `{"error":{"message":"invalid secret-upstream.example token","type":"authentication_error","code":"invalid_api_key","param":"api_key"},"rate_limit":{"remaining":0}}`), nil
	}}
	gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, httpUpstream: wire, rateLimitService: rl}
	body := []byte(`{"model":"gpt-5.6-sol","input":"hello","stream":false}`)
	c := newOpenAIRejectedFieldTestContext(body)
	_, err := gateway.Forward(context.Background(), c, selected, body)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.True(t, failover.ShouldRetryNextAccount())
	require.True(t, failover.AllowsOpenAIAccountSwitch())
	require.False(t, c.Writer.Written(), "the handler must retain the opportunity to switch accounts")
	require.NotContains(t, failover.ClientMessage, "secret-upstream.example")
	require.Equal(t, 1, repo.casCalls)
	require.Equal(t, StatusError, repo.current.Status)
	require.Len(t, blocker.accounts, 1)
}

func TestHTTP401SameTokenLockWinnerKeepsAcquiredGeneration(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			selected := newOpenAIAuthRecoveryTestAccount()
			selected.Platform = platform
			selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			current := snapshotRequestCredentialAccount(selected)
			current.Credentials["refresh_token"] = "winner-refresh"
			if platform == PlatformGrok {
				current.Credentials["refresh_token"] = selected.Credentials["refresh_token"] // A request can adopt only an access-token rotation of its captured grant.
			}
			current.Credentials["_token_version"] = int64(2)
			current.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			repo := &http401CredentialFenceRepo{current: current}
			cache := &concurrentOpenAITokenCache{token: "old-token"}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			var token string
			var err error
			if platform == PlatformGrok {
				provider := NewGrokTokenProvider(repo, cache)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &refreshAPIExecutorStub{needsRefresh: true})
				token, err = provider.GetAccessToken(ctx, selected)
			} else {
				provider := NewOpenAITokenProvider(repo, cache, nil)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &refreshAPIExecutorStub{needsRefresh: true})
				token, err = provider.GetAccessToken(ctx, selected)
			}
			require.NoError(t, err)
			require.Equal(t, "old-token", token)
			ctx = withRequestCredentialOwner(ctx, selected, token)
			evidence, ok := requestCredentialOwner(ctx, selected)
			require.True(t, ok, "one successful acquisition must not also record its obsolete selection snapshot")
			require.Equal(t, current.Credentials, evidence.Credentials)
			blocker := &runtimeBlockRecorder{}
			rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			rl.SetAccountRuntimeBlocker(blocker)
			body := []byte(`{"error":{"code":"token_revoked"}}`)
			if platform == PlatformGrok {
				// Exercise the provider-independent permanent quarantine helper;
				// a recoverable Grok 401 otherwise enters its OAuth cooldown.
				rl.handleHTTP401CredentialError(ctx, selected, evidence, "rejected bearer")
			} else {
				require.True(t, rl.HandleUpstreamError(ctx, selected, http.StatusUnauthorized, nil, body))
			}
			require.Equal(t, 1, repo.casCalls)
			require.Equal(t, StatusError, repo.current.Status)
			require.Len(t, blocker.accounts, 1)
		})
	}
}

func TestHTTP401PassthroughRespectsExplicitStatusPolicy(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			account := &Account{Platform: platform, Type: kind}
			require.True(t, shouldFailoverOpenAIPassthroughResponse(account, http.StatusUnauthorized, []byte(`{"error":{"code":"invalid_api_key"}}`)), platform+"/"+kind)
			account.Credentials = map[string]any{"custom_error_codes_enabled": true, "custom_error_codes": []any{float64(http.StatusForbidden)}}
			if kind == AccountTypeAPIKey {
				require.False(t, shouldFailoverOpenAIPassthroughResponse(account, http.StatusUnauthorized, []byte(`{"error":{"code":"invalid_api_key"}}`)), "an explicit API-key status exclusion retains its existing meaning")
			} else {
				require.True(t, shouldFailoverOpenAIPassthroughResponse(account, http.StatusUnauthorized, []byte(`{"error":{"code":"invalid_api_key"}}`)), "custom error codes apply only to API-key accounts")
			}
		}
	}
}

func TestHTTP401WebSocketFailureRetainsAttemptCredentialEvidence(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "concurrent-login"}[rotated], func(t *testing.T) {
			selected := newOpenAIAuthRecoveryTestAccount()
			sent := snapshotRequestCredentialAccount(selected)
			sent.Credentials["access_token"], sent.Credentials["refresh_token"] = "socket-T1", "socket-S1"
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			recordRequestCredentialOwner(ctx, sent, "socket-T1")
			ctx = withRequestCredentialOwner(ctx, selected, "socket-T1")
			repo := &http401CredentialFenceRepo{current: snapshotRequestCredentialAccount(sent)}
			if rotated {
				repo.current.Credentials["access_token"], repo.current.Credentials["refresh_token"] = "new-login-T2", "new-login-S2"
			}
			blocker := &runtimeBlockRecorder{}
			rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			rl.SetAccountRuntimeBlocker(blocker)
			gateway := &OpenAIGatewayService{accountRepo: repo, rateLimitService: rl}
			payload := []byte(`{"type":"response.failed","response":{"error":{"type":"authentication_error","code":"token_revoked","message":"rejected"}}}`)
			require.True(t, gateway.handleOpenAIWSFailureAccountSideEffects(ctx, selected, "gpt-5.6-sol", nil, payload))
			require.Equal(t, 1, repo.casCalls)
			require.Equal(t, sent.Credentials, repo.attempted.Credentials)
			if rotated {
				require.Equal(t, StatusActive, repo.current.Status)
				require.Empty(t, blocker.accounts)
			} else {
				require.Equal(t, StatusError, repo.current.Status)
				require.Len(t, blocker.accounts, 1)
			}
		})
	}
}

func TestHTTP401HTTPStreamRetainsAcquiredCredentialEvidence(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, rotated := range []bool{false, true} {
			name := map[bool]string{false: "forward", true: "passthrough"}[passthrough] + map[bool]string{false: "/unchanged", true: "/concurrent-login"}[rotated]
			t.Run(name, func(t *testing.T) {
				selected := newOpenAIAuthRecoveryTestAccount()
				selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
				repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotRequestCredentialAccount(selected)}}
				provider := NewOpenAITokenProvider(repo, nil, nil)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "stream-T1", "refresh_token": "stream-S1", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}})
				var sentOwner *Account
				wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
					require.Equal(t, "Bearer stream-T1", req.Header.Get("Authorization"))
					sentOwner = snapshotRequestCredentialAccount(repo.account)
					if rotated {
						repo.account = snapshotRequestCredentialAccount(repo.account)
						repo.account.Credentials["access_token"], repo.account.Credentials["refresh_token"] = "new-login-T2", "new-login-S2"
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"authentication_error\",\"code\":\"token_revoked\",\"message\":\"rejected\"}}}\n\n"))}, nil
				}}
				blocker := &runtimeBlockRecorder{}
				cfg := rawChatCompletionsTestConfig()
				rl := NewRateLimitService(repo, nil, cfg, nil, nil)
				rl.SetAccountRuntimeBlocker(blocker)
				gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, openAITokenProvider: provider, httpUpstream: wire, rateLimitService: rl}
				body := []byte(`{"model":"gpt-5.6-sol","stream":true,"instructions":"test","input":"hello"}`)
				c := newOpenAIRejectedFieldTestContext(body)
				original := c.Request.Context()
				var err error
				if passthrough {
					_, err = gateway.forwardOpenAIPassthrough(context.Background(), c, selected, body, body, "gpt-5.6-sol", true, nil, true, time.Now())
				} else {
					_, err = gateway.Forward(context.Background(), c, selected, body)
				}
				require.Error(t, err)
				require.Positive(t, repo.fence.casCalls)
				require.Equal(t, sentOwner.Credentials, repo.fence.attempted.Credentials)
				require.True(t, original == c.Request.Context(), "attempt evidence cannot leak into a handler retry")
				if rotated {
					require.Equal(t, StatusActive, repo.account.Status)
					require.Empty(t, blocker.accounts)
				} else {
					require.Equal(t, StatusError, repo.account.Status)
					require.Len(t, blocker.accounts, 1)
				}
			})
		}
	}
}

func TestHTTP401GinCredentialEvidenceRestoresNestedAttempts(t *testing.T) {
	first := newOpenAIAuthRecoveryTestAccount()
	second := snapshotRequestCredentialAccount(first)
	second.Credentials["access_token"] = "next-token"
	c := newOpenAIRejectedFieldTestContext(nil)
	original := c.Request.Context()
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, first, "old-token")
	recordRequestCredentialOwner(ctx, second, "next-token")
	firstCtx := withRequestCredentialOwner(ctx, first, "old-token")
	secondCtx := withRequestCredentialOwner(ctx, second, "next-token")
	finishFirst := bindRequestCredentialOwnerContext(firstCtx, c)
	finishSecond := bindRequestCredentialOwnerContext(secondCtx, c)
	owner, ok := requestCredentialOwner(c.Request.Context(), second)
	require.True(t, ok)
	require.Equal(t, "next-token", owner.GetOpenAIAccessToken())
	finishSecond()
	owner, ok = requestCredentialOwner(c.Request.Context(), first)
	require.True(t, ok)
	require.Equal(t, "old-token", owner.GetOpenAIAccessToken())
	finishFirst()
	require.True(t, original == c.Request.Context(), "the exact original request context must be restored")
}
