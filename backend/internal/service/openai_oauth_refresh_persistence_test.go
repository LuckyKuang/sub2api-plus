//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// A refresh response belongs to the exact grant it consumed. A login completed
// during that call must remain authoritative and usable for the current turn.
func TestOpenAIRefreshSuccessCASPreservesConcurrentAuthorization(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(map[bool]string{false: "pre-expiry", true: "401-recovery"}[force], func(t *testing.T) {
			account := newOpenAIAuthRecoveryTestAccount()
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(account)}
			cache := &refreshAPICacheStub{lockResult: true}
			deletesBeforeCAS := 0
			repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
				current := snapshotOAuthRefreshAccount(r.account)
				current.Credentials["access_token"] = "new-login-token"
				current.Credentials["refresh_token"] = "new-login-refresh"
				current.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
				current.Credentials["_token_version"] = 999
				r.account = current
				deletesBeforeCAS = cache.deleteCalls
			}
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "obsolete-response", "refresh_token": "consumed-refresh"}}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
			var token string
			var err error
			if force {
				token, err = provider.ForceRefreshToken(context.Background(), account, "old-token")
			} else {
				token, err = provider.GetAccessToken(context.Background(), account)
			}
			require.NoError(t, err)
			require.Equal(t, "new-login-token", token)
			require.Equal(t, "new-login-refresh", repo.account.GetCredential("refresh_token"))
			require.Equal(t, "old-token", repo.lastExpectedCredentials["access_token"])
			require.Equal(t, "refresh-1", repo.lastExpectedCredentials["refresh_token"])
			require.Equal(t, 1, repo.successCASCalls)
			require.Zero(t, repo.updateCredentialsCalls, "the obsolete response must never overwrite a concurrent authorization")
			require.Equal(t, 1, executor.refreshCalls)
			require.Equal(t, deletesBeforeCAS, cache.deleteCalls, "a lost refresh CAS must not invalidate the newer grant's cache")
			if force {
				require.Equal(t, 1, deletesBeforeCAS, "401 recovery drops the rejected token before the concurrent login")
				require.Equal(t, OpenAITokenCacheKey(account), cache.deleteKey)
			} else {
				require.Zero(t, deletesBeforeCAS)
			}
		})
	}
}

type openAIRefreshPersistenceGrantCache struct {
	refreshAPICacheStub
	token string
}

func (c *openAIRefreshPersistenceGrantCache) DeleteAccessToken(ctx context.Context, key string) error {
	c.token = ""
	return c.refreshAPICacheStub.DeleteAccessToken(ctx, key)
}

func TestOpenAIRefreshCASMissCannotDeleteConcurrentGrantCache(t *testing.T) {
	for _, scenario := range []string{"reauthorization", "metadata-conflict", "owner-changed", "readback-failure"} {
		t.Run(scenario, func(t *testing.T) {
			selected := newOpenAIAuthRecoveryTestAccount()
			repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(selected)}
			cache := &openAIRefreshPersistenceGrantCache{refreshAPICacheStub: refreshAPICacheStub{lockResult: true}}
			repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
				current := snapshotOAuthRefreshAccount(r.account)
				current.Credentials["model_mapping"] = map[string]any{"a": "b"}
				if scenario != "metadata-conflict" {
					current.Credentials["access_token"] = "new-login-token"
					current.Credentials["refresh_token"] = "new-login-refresh"
				}
				if scenario == "owner-changed" {
					current.Type = AccountTypeAPIKey
				}
				if scenario == "readback-failure" {
					r.getByIDErrAfterCall, r.getByIDErrAfterCallErr = 2, errors.New("current state unavailable")
				}
				r.account = current
				// A concurrent login has already populated the row-ID cache before
				// this obsolete refresh result reaches its failed conditional write.
				cache.token = "current-grant-cache-token"
			}
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "obsolete-response", "refresh_token": "consumed-refresh"}}

			result, err := NewOAuthRefreshAPI(repo, cache).RefreshIfNeeded(context.Background(), selected, executor, time.Hour)

			if scenario == "reauthorization" {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.False(t, result.Refreshed)
				require.Nil(t, result.NewCredentials)
				require.Equal(t, "new-login-token", result.Account.GetCredential("access_token"))
			} else {
				require.Nil(t, result)
				var failure *CredentialFailureError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
			}
			require.Equal(t, "current-grant-cache-token", cache.token)
			require.Zero(t, cache.deleteCalls, "CAS miss is not authorization to delete a current owner's cache")
			require.Equal(t, 1, repo.successCASCalls)
			require.Zero(t, repo.updateCredentialsCalls)
			require.Equal(t, 1, executor.refreshCalls)
			require.Equal(t, "old-token", selected.GetCredential("access_token"))
		})
	}
}

func TestOpenAIRefreshSuccessPersistenceFailureCannotReturnStaleToken(t *testing.T) {
	for _, scenario := range []string{"write-failure", "readback-failure", "unchanged-token-conflict", "owner-changed", "proxy-url-changed"} {
		t.Run(scenario, func(t *testing.T) {
			account := newOpenAIAuthRecoveryTestAccount()
			proxyID := int64(7)
			account.ProxyID, account.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "old.invalid", Port: 8080}
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(account)}
			switch scenario {
			case "write-failure":
				repo.updateErr = errors.New("local persistence unavailable")
			case "readback-failure":
				repo.getByIDErrAfterCall, repo.getByIDErrAfterCallErr = 2, errors.New("readback unavailable")
			case "unchanged-token-conflict":
				repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
					current := snapshotOAuthRefreshAccount(r.account)
					current.Credentials["model_mapping"] = map[string]any{"a": "b"}
					r.account = current
				}
			case "owner-changed":
				repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
					current := snapshotOAuthRefreshAccount(r.account)
					current.Type = AccountTypeAPIKey
					r.account = current
				}
			case "proxy-url-changed":
				repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
					current := snapshotOAuthRefreshAccount(r.account)
					current.Proxy.Host = "new.invalid"
					r.account = current
				}
			}
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "rotated", "refresh_token": "rotated-refresh"}})
			token, err := provider.GetAccessToken(context.Background(), account)
			require.Empty(t, token, "a failed persistence decision must not fall back to the expired or ambiguous grant")
			var failure *CredentialFailureError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
			require.False(t, failure.Permanent)
		})
	}
}

type openAIRefreshRecoveryFenceRepo struct {
	*refreshAPIAccountRepo
	mutations int
}

func (r *openAIRefreshRecoveryFenceRepo) SetError(context.Context, int64, string) error {
	r.mutations++
	return nil
}
func (r *openAIRefreshRecoveryFenceRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.mutations++
	return nil
}

// Persistence is a shared dependency. Neither HTTP forwarding mode may turn
// its failure into an account-auth penalty, pool scan or a stale-token replay.
func TestOpenAI401RefreshPersistenceFailureStopsBeforeAuthPenalty(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "forward", true: "passthrough"}[passthrough], func(t *testing.T) {
			account := newOpenAIAuthRecoveryTestAccount()
			repo := &openAIRefreshRecoveryFenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(account), updateErr: errors.New("write unavailable")}}
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "must-not-send", "refresh_token": "rotated"}})
			wire := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_token","message":"rejected"}}`))}}}
			cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, openAITokenProvider: provider, httpUpstream: wire, rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil)}
			body := []byte(`{"model":"gpt-5.6-sol","stream":true,"instructions":"test","input":"hello"}`)
			c := newOpenAIRejectedFieldTestContext(body)
			var err error
			if passthrough {
				_, err = svc.forwardOpenAIPassthrough(context.Background(), c, account, body, body, "gpt-5.6-sol", true, nil, false, time.Now())
			} else {
				_, err = svc.Forward(context.Background(), c, account, body)
			}
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
			require.False(t, failure.ShouldRetryNextAccount())
			require.False(t, failure.ShouldReportAccountScheduleFailure())
			require.Zero(t, repo.mutations)
			require.Len(t, wire.requests, 1)
			require.False(t, c.Writer.Written())
		})
	}
}

func TestOpenAIBackgroundRefreshPersistenceFailureContainsWithoutAccountPenalty(t *testing.T) {
	account := newOpenAIAuthRecoveryTestAccount()
	repo := &tokenRefreshAccountRepo{conditionalSuccessErr: errors.New("durable write unavailable")}
	repo.accountsByID = map[int64]*Account{account.ID: snapshotOAuthRefreshAccount(account)}
	cfg := &config.Config{TokenRefresh: config.TokenRefreshConfig{MaxRetries: 3, RetryBackoffSeconds: 0}}
	svc := NewTokenRefreshService(repo, nil, nil, nil, nil, nil, nil, cfg, nil)
	svc.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil))
	executor := &tokenRefresherStub{credentials: map[string]any{"access_token": "rotated", "refresh_token": "rotated-refresh"}}
	err := svc.refreshWithRetry(context.Background(), account, executor, executor, time.Hour)
	var containment *providerCycleContainmentRefreshError
	require.ErrorAs(t, err, &containment)
	require.Equal(t, 1, executor.calls, "a possibly consumed refresh token must not be retried after persistence failure")
	require.Equal(t, 1, repo.conditionalSuccessCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.setTempUnschedCalls)
	require.Equal(t, "refresh-1", repo.accountsByID[account.ID].GetOpenAIRefreshToken())
}

func TestOpenAIRefreshOwnerFenceIncludesLockedRereadAndInvalidGrantRace(t *testing.T) {
	for _, stage := range []string{"locked-reread", "invalid-grant-race"} {
		for _, field := range []string{"platform", "type", "proxy", "proxy-url", "inactive"} {
			t.Run(stage+"/"+field, func(t *testing.T) {
				selected := newOpenAIAuthRecoveryTestAccount()
				proxyID := int64(7)
				selected.ProxyID, selected.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "old.invalid", Port: 8080}
				repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(selected)}
				change := func() {
					current := snapshotOAuthRefreshAccount(repo.account)
					current.Credentials["refresh_token"] = "new-login-refresh"
					switch field {
					case "platform":
						current.Platform = PlatformKimi
					case "type":
						current.Type = AccountTypeAPIKey
					case "proxy":
						id := int64(999)
						current.ProxyID = &id
					case "proxy-url":
						current.Proxy.Host = "new.invalid"
					case "inactive":
						current.Status = StatusDisabled
					}
					repo.account = current
				}
				executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "must-not-send"}}
				if stage == "locked-reread" {
					change()
				} else {
					executor.err = errors.New("invalid_grant")
					executor.onRefresh = change
				}
				result, err := NewOAuthRefreshAPI(repo, nil).RefreshIfNeeded(context.Background(), selected, executor, time.Hour)
				require.Nil(t, result)
				var failure *CredentialFailureError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
				require.Equal(t, GatewayFailureReason("openai_oauth_owner_changed"), failure.Reason)
				require.Zero(t, repo.updateCredentialsCalls)
				if stage == "locked-reread" {
					require.Zero(t, executor.refreshCalls)
				} else {
					require.Equal(t, 1, executor.refreshCalls)
				}
			})
		}
	}
}

type concurrentOpenAITokenCache struct {
	refreshAPICacheStub
	reads int
	token string
	force bool
}

func (c *concurrentOpenAITokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	if !c.force && c.reads == 1 {
		return "", nil
	}
	return c.token, nil
}

func TestOpenAIRefreshLockWinnerTokenRequiresSameDurableOwner(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, changed := range []string{"same-owner", "changed-proxy", "changed-proxy-url"} {
			t.Run(map[bool]string{false: "pre-expiry", true: "401-recovery"}[force]+"/"+changed, func(t *testing.T) {
				selected := newOpenAIAuthRecoveryTestAccount()
				proxyID := int64(7)
				selected.ProxyID, selected.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "old.invalid", Port: 8080}
				selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
				current := snapshotOAuthRefreshAccount(selected)
				current.Credentials["access_token"] = "lock-winner-token"
				if changed == "changed-proxy" {
					id := int64(999)
					current.ProxyID = &id
				}
				if changed == "changed-proxy-url" {
					current.Proxy.Host = "new.invalid"
				}
				repo := &refreshAPIAccountRepo{account: current}
				cache := &concurrentOpenAITokenCache{token: "lock-winner-token", force: force}
				provider := NewOpenAITokenProvider(repo, cache, nil)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &refreshAPIExecutorStub{needsRefresh: true})
				var token string
				var err error
				if force {
					token, err = provider.ForceRefreshToken(context.Background(), selected, "old-token")
				} else {
					token, err = provider.GetAccessToken(context.Background(), selected)
				}
				if changed != "same-owner" {
					var failure *CredentialFailureError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
					require.Empty(t, token)
				} else {
					require.NoError(t, err)
					require.Equal(t, "lock-winner-token", token)
				}
				require.Zero(t, repo.updateCredentialsCalls)
			})
		}
	}
}

func TestOpenAIRefreshNeverReplaysActualRejectedTokenFromCache(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(map[bool]string{false: "already-refreshed", true: "lock-winner"}[locked], func(t *testing.T) {
			selected := newOpenAIAuthRecoveryTestAccount()
			current := snapshotOAuthRefreshAccount(selected)
			current.Credentials["access_token"] = "actual-wire-token"
			repo := &refreshAPIAccountRepo{account: current}
			cache := &concurrentOpenAITokenCache{token: "actual-wire-token", force: true}
			cache.lockResult = !locked
			provider := NewOpenAITokenProvider(repo, cache, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &refreshAPIExecutorStub{needsRefresh: false})
			token, err := provider.ForceRefreshToken(context.Background(), selected, "actual-wire-token")
			require.Error(t, err)
			require.Empty(t, token, "the actual rejected wire credential must not be retried even when the selected snapshot is older")
			require.Zero(t, repo.updateCredentialsCalls)
		})
	}
}

func TestOpenAICacheTokenChangeRequiresDurableOwnerAndGrant(t *testing.T) {
	for _, scenario := range []string{"same-owner", "proxy-url-changed", "proxy-id-changed", "inactive", "cached-token-stale"} {
		t.Run(scenario, func(t *testing.T) {
			selected := newOpenAIAuthRecoveryTestAccount()
			proxyID := int64(7)
			selected.ProxyID, selected.Proxy = &proxyID, &Proxy{ID: proxyID, Protocol: "http", Host: "old.invalid", Port: 8080}
			current := snapshotOAuthRefreshAccount(selected)
			current.Credentials["access_token"] = "cached-new-token"
			switch scenario {
			case "proxy-url-changed":
				current.Proxy.Host = "new.invalid"
			case "proxy-id-changed":
				id := int64(8)
				current.ProxyID = &id
			case "inactive":
				current.Status = StatusDisabled
			case "cached-token-stale":
				current.Credentials["access_token"] = "newer-durable-token"
			}
			repo := &refreshAPIAccountRepo{account: current}
			cache := &concurrentOpenAITokenCache{token: "cached-new-token", force: true}
			provider := NewOpenAITokenProvider(repo, cache, nil)
			token, err := provider.GetAccessToken(context.Background(), selected)
			if scenario == "same-owner" {
				require.NoError(t, err)
				require.Equal(t, "cached-new-token", token)
			} else {
				var failure *CredentialFailureError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
				require.Empty(t, token)
			}
			require.Equal(t, 1, repo.getByIDCalls)
			require.Zero(t, repo.updateCalls)
		})
	}
}
