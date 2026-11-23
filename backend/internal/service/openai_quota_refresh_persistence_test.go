//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type quotaRefreshPersistenceRepo struct {
	*quotaProxyOwnerRepo
	persistCalls    int
	persistCtxErr   error
	persistDeadline time.Time
	persistErr      error
	beforePersist   func()
}

type quotaRefreshPersistenceCache struct {
	refreshAPICacheStub
	tokens         map[string]string
	deleteDeadline time.Time
	deleteErr      error
	setCalls       int
}

func (c *quotaRefreshPersistenceCache) GetAccessToken(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return c.tokens[key], nil
}

func (c *quotaRefreshPersistenceCache) SetAccessToken(ctx context.Context, key, token string, _ time.Duration) error {
	c.setCalls++
	if err := ctx.Err(); err != nil {
		return err
	}
	c.tokens[key] = token
	return nil
}

func (c *quotaRefreshPersistenceCache) DeleteAccessToken(ctx context.Context, key string) error {
	_ = c.refreshAPICacheStub.DeleteAccessToken(ctx, key)
	c.deleteDeadline, _ = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.deleteErr != nil {
		return c.deleteErr
	}
	delete(c.tokens, key)
	return nil
}

func (r *quotaRefreshPersistenceRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.quotaProxyOwnerRepo.GetByID(ctx, id)
}

func (r *quotaRefreshPersistenceRepo) UpdateOpenAIQuotaCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	r.persistCalls++
	r.persistCtxErr = ctx.Err()
	r.persistDeadline, _ = ctx.Deadline()
	if r.beforePersist != nil {
		r.beforePersist()
	}
	if r.persistErr != nil {
		return false, r.persistErr
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return r.quotaProxyOwnerRepo.UpdateOpenAIQuotaCredentialsIfUnchanged(ctx, expected, credentials)
}

// Quota acquisition rejects an old row-ID cache entry before refreshing. A
// canceled caller cannot republish the new access token, so the committed
// rotation must evict the old entry before a later ordinary request uses it.
func TestOpenAIQuotaRefreshLateRotationClearsStaleCacheForNextRequest(t *testing.T) {
	base, account, _ := (quotaProxyOwnerCase{provider: PlatformOpenAI}).fixture(t)
	account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	repo := &quotaRefreshPersistenceRepo{quotaProxyOwnerRepo: base}
	key := OpenAITokenCacheKey(account)
	cache := &quotaRefreshPersistenceCache{
		refreshAPICacheStub: refreshAPICacheStub{lockResult: true},
		tokens:              map[string]string{key: "cached-before-rotation", "openai:account:999999": "other-account-token"},
	}
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx, err := withOpenAIQuotaTokenOwner(parent, account)
	require.NoError(t, err)
	credentials := cloneRequestCredentialMap(account.Credentials)
	credentials["access_token"], credentials["refresh_token"] = "completed-access", "completed-refresh"
	credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: credentials, onRefresh: cancel}
	tokens := NewOpenAITokenProvider(repo, cache, nil)
	tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

	_, err = tokens.GetAccessToken(ctx, account)

	require.Error(t, err, "the quota caller keeps its cancellation")
	require.ErrorIs(t, parent.Err(), context.Canceled)
	require.Equal(t, 1, repo.persistCalls)
	require.Equal(t, "completed-refresh", base.accounts[account.ID].GetOpenAIRefreshToken())
	// Exercise the real next-request cache-hit/miss path before checking cache
	// internals, so a stale entry exposes its actual cache-conflict failure.
	token, err := tokens.GetAccessToken(t.Context(), base.accounts[account.ID])
	require.NoError(t, err)
	require.Equal(t, "completed-access", token)
	require.Equal(t, 1, executor.refreshCalls, "a committed rotation must not be repeated")
	require.Equal(t, 1, cache.deleteCalls)
	require.Equal(t, key, cache.deleteKey)
	require.NoError(t, cache.deleteCtxErr)
	require.False(t, cache.deleteDeadline.IsZero())
	require.LessOrEqual(t, time.Until(cache.deleteDeadline), defaultRefreshPostPersistCleanupTimeout)
	require.Equal(t, "completed-access", cache.tokens[key])
	require.Equal(t, "other-account-token", cache.tokens["openai:account:999999"])
}

func TestOpenAIQuotaRefreshCacheDeletionFailureContainsCommittedRotation(t *testing.T) {
	base, account, _ := (quotaProxyOwnerCase{provider: PlatformOpenAI}).fixture(t)
	account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	repo := &quotaRefreshPersistenceRepo{quotaProxyOwnerRepo: base}
	key := OpenAITokenCacheKey(account)
	cache := &quotaRefreshPersistenceCache{
		refreshAPICacheStub: refreshAPICacheStub{lockResult: true},
		tokens:              map[string]string{key: "cached-before-rotation"},
		deleteErr:           errors.New("cache unavailable"),
	}
	ctx, err := withOpenAIQuotaTokenOwner(t.Context(), account)
	require.NoError(t, err)
	credentials := cloneRequestCredentialMap(account.Credentials)
	credentials["access_token"], credentials["refresh_token"] = "committed-access", "committed-refresh"
	credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: credentials}
	tokens := NewOpenAITokenProvider(repo, cache, nil)
	tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

	token, err := tokens.GetAccessToken(ctx, account)

	require.ErrorIs(t, err, errOpenAIQuotaTokenOwnerChanged)
	require.Empty(t, token, "cache cleanup failure cannot authorize a token or quota request")
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, 1, repo.persistCalls)
	require.Equal(t, "committed-refresh", base.accounts[account.ID].GetOpenAIRefreshToken())
	require.Equal(t, 1, cache.deleteCalls)
	require.Equal(t, key, cache.deleteKey)
	require.Zero(t, cache.setCalls, "failed cleanup must not fall through to cache publication")
	require.Equal(t, "cached-before-rotation", cache.tokens[key])
}

// The v0.2.24 completed-rotation contract also applies to WHAM usage/reset token
// acquisition. Cancellation during optional enrichment cannot discard the new
// refresh token, and its quota owner must still fence concurrent authorization.
func TestOpenAIQuotaRefreshCompletedRotationSurvivesCancellation(t *testing.T) {
	for _, cancellation := range []string{"deadline", "parent"} {
		t.Run(cancellation, func(t *testing.T) {
			base, account, _ := (quotaProxyOwnerCase{provider: PlatformOpenAI}).fixture(t)
			repo := &quotaRefreshPersistenceRepo{quotaProxyOwnerRepo: base}
			parent, cancel := context.WithCancel(t.Context())
			if cancellation == "deadline" {
				cancel()
				parent, cancel = context.WithTimeout(t.Context(), 10*time.Millisecond)
			}
			defer cancel()
			ctx, err := withOpenAIQuotaTokenOwner(parent, account)
			require.NoError(t, err)
			credentials := cloneRequestCredentialMap(account.Credentials)
			credentials["access_token"] = "completed-access"
			credentials["refresh_token"] = "completed-refresh"
			executor := &refreshAPIExecutorStub{
				needsRefresh: true,
				credentials:  credentials,
				onRefresh: func() {
					if cancellation == "parent" {
						cancel()
					}
					<-parent.Done()
				},
			}

			result, err := NewOAuthRefreshAPI(repo, nil).RefreshIfNeeded(ctx, account, executor, time.Hour)

			require.NoError(t, err)
			require.True(t, result.Refreshed)
			require.Equal(t, 1, executor.refreshCalls)
			require.Equal(t, 1, repo.persistCalls)
			require.NoError(t, repo.persistCtxErr)
			require.False(t, repo.persistDeadline.IsZero())
			require.LessOrEqual(t, time.Until(repo.persistDeadline), 10*time.Second)
			require.Equal(t, "completed-refresh", base.accounts[account.ID].GetOpenAIRefreshToken())
			require.Equal(t, "completed-access", result.Account.GetOpenAIAccessToken())
			require.NoError(t, openAIQuotaTokenOwnerFromContext(ctx).validate(result.Account))
			require.Error(t, parent.Err(), "persisting the rotation must preserve caller cancellation")
		})
	}
}

func TestOpenAIQuotaRefreshLateRotationStillFencesOwner(t *testing.T) {
	for _, change := range []string{"relinked", "proxy", "identity", "write failure"} {
		t.Run(change, func(t *testing.T) {
			base, account, _ := (quotaProxyOwnerCase{provider: PlatformOpenAI}).fixture(t)
			repo := &quotaRefreshPersistenceRepo{quotaProxyOwnerRepo: base}
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, err := withOpenAIQuotaTokenOwner(parent, account)
			require.NoError(t, err)
			credentials := cloneRequestCredentialMap(account.Credentials)
			credentials["access_token"] = "late-access"
			credentials["refresh_token"] = "late-refresh"
			if change == "write failure" {
				repo.persistErr = errors.New("quota persistence unavailable")
			} else {
				repo.beforePersist = func() {
					current := snapshotRequestCredentialAccount(account)
					switch change {
					case "relinked":
						current.Credentials["access_token"] = "replacement-access"
						current.Credentials["refresh_token"] = "replacement-refresh"
					case "proxy":
						current.Proxy.Host = "replacement.proxy.example"
					case "identity":
						current.Credentials["user_agent"] = "codex_cli_rs/0.170.0 (Mac OS X 14.0; arm64) iTerm"
					}
					base.accounts[account.ID] = current
				}
			}
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: credentials, onRefresh: cancel}
			key := OpenAITokenCacheKey(account)
			cache := &quotaRefreshPersistenceCache{
				refreshAPICacheStub: refreshAPICacheStub{lockResult: true},
				tokens:              map[string]string{key: "current-owner-cache"},
			}

			result, err := NewOAuthRefreshAPI(repo, cache).RefreshIfNeeded(ctx, account, executor, time.Hour)

			require.ErrorIs(t, err, errOpenAIQuotaTokenOwnerChanged)
			require.Nil(t, result)
			require.Equal(t, 1, executor.refreshCalls)
			require.Equal(t, 1, repo.persistCalls, "completed rotation must reach the exact-owner conditional write")
			require.Zero(t, base.writes)
			require.NotEqual(t, "late-refresh", base.accounts[account.ID].GetOpenAIRefreshToken())
			require.Zero(t, cache.deleteCalls, "a failed owner CAS cannot evict the current grant's token")
			require.Equal(t, "current-owner-cache", cache.tokens[key])
		})
	}
}

func TestOpenAIQuotaRefreshIncompleteRotationCannotBypassCancellation(t *testing.T) {
	// Only a successfully completed token rotation authorizes detached local
	// persistence. Partial credentials or issuer failure cannot mutate the grant.
	for _, tc := range []struct {
		name        string
		credentials map[string]any
		refreshErr  error
	}{
		{name: "no credentials"},
		{name: "access only", credentials: map[string]any{"access_token": "partial-access"}},
		{name: "refresh only", credentials: map[string]any{"refresh_token": "partial-refresh"}},
		{name: "issuer failed", credentials: map[string]any{"access_token": "failed-access", "refresh_token": "failed-refresh"}, refreshErr: errors.New("issuer failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, account, _ := (quotaProxyOwnerCase{provider: PlatformOpenAI}).fixture(t)
			repo := &quotaRefreshPersistenceRepo{quotaProxyOwnerRepo: base}
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, err := withOpenAIQuotaTokenOwner(parent, account)
			require.NoError(t, err)
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: tc.credentials, err: tc.refreshErr, onRefresh: cancel}

			result, err := NewOAuthRefreshAPI(repo, nil).RefreshIfNeeded(ctx, account, executor, time.Hour)

			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, result)
			require.Equal(t, 1, executor.refreshCalls)
			require.Zero(t, repo.persistCalls)
			require.Zero(t, base.writes)
			require.Equal(t, "quota-owner-refresh", base.accounts[account.ID].GetOpenAIRefreshToken())
		})
	}
}

func TestOpenAIQuotaRefreshPersistenceDoesNotResumeCanceledOperation(t *testing.T) {
	// The rotation write has an independent local budget. That budget must
	// never authorize a WHAM usage request or credit consumption after the
	// original caller has canceled its operation.
	for _, operation := range []string{"usage", "reset", "targeted-reset"} {
		t.Run(operation, func(t *testing.T) {
			base, account, _ := (quotaProxyOwnerCase{provider: PlatformOpenAI}).fixture(t)
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			repo := &quotaRefreshPersistenceRepo{quotaProxyOwnerRepo: base}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			credentials := cloneRequestCredentialMap(account.Credentials)
			credentials["access_token"] = "completed-access"
			credentials["refresh_token"] = "completed-refresh"
			credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
			executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: credentials, onRefresh: cancel}
			tokens := NewOpenAITokenProvider(repo, nil, nil)
			tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
			var received atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				received.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"plan_type":"pro","code":"ok"}`))
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			require.NoError(t, err)
			factory := func(string) (*req.Client, error) {
				client := req.C().SetTimeout(time.Second)
				client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
					return func(request *http.Request) (*http.Response, error) {
						forwarded := request.Clone(request.Context())
						forwarded.URL.Scheme, forwarded.URL.Host = target.Scheme, target.Host
						return http.DefaultTransport.RoundTrip(forwarded)
					}
				})
				return client, nil
			}
			service := NewOpenAIQuotaService(repo, nil, tokens, factory)
			switch operation {
			case "usage":
				_, err = service.QueryUsage(ctx, account.ID)
			case "reset":
				_, err = service.ResetCredit(ctx, account.ID)
			case "targeted-reset":
				_, err = service.ResetCreditTargeted(ctx, account.ID, "quota-credit", "quota-redeem")
			}
			require.Error(t, err)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, 1, executor.refreshCalls)
			require.Equal(t, 1, repo.persistCalls)
			require.Equal(t, "completed-refresh", base.accounts[account.ID].GetOpenAIRefreshToken())
			require.Equal(t, int64(0), received.Load(), "canceled quota operation must not send upstream HTTP")
			require.Equal(t, 1, base.writes, "only the completed credential rotation may be persisted")
		})
	}
}
