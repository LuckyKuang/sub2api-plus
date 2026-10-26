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

type grokTokenOwnerRepo struct {
	AccountRepository
	current        *Account
	writes         int
	legacyWrites   int
	storageFailure bool
	afterWrite     func()
}

func (r *grokTokenOwnerRepo) GetByID(context.Context, int64) (*Account, error) { return r.current, nil }

// This legacy port intentionally models its actual narrower credential/ID
// contract so the requirement test can detect same-row proxy edits.
func (r *grokTokenOwnerRepo) UpdateGrokOAuthCredentialsIfUnchanged(_ context.Context, _ int64, credentials map[string]any, proxyID *int64, replacement map[string]any) (bool, error) {
	r.legacyWrites++
	if r.storageFailure {
		return false, errors.New("injected storage failure")
	}
	if r.current.GetCredential("refresh_token") != credentials["refresh_token"] || !grokCredentialProxyIDsEqual(r.current.ProxyID, proxyID) {
		return false, nil
	}
	r.current.Credentials = shallowCopyMap(replacement)
	r.writes++
	if r.afterWrite != nil {
		r.afterWrite()
	}
	return true, nil
}

func (r *grokTokenOwnerRepo) UpdateOAuthCredentialsIfUnchanged(_ context.Context, expected *Account, accountType string, credentials, extra map[string]any, recover bool) (bool, error) {
	if r.storageFailure {
		return false, errors.New("injected storage failure")
	}
	if !testGrokOutcomeOwnersEqual(r.current, expected) {
		return false, nil
	}
	if accountType != AccountTypeOAuth || len(extra) != 0 || recover {
		return false, errors.New("request refresh cannot clear independently managed recovery state")
	}
	r.current.Credentials = shallowCopyMap(credentials)
	r.writes++
	if r.afterWrite != nil {
		r.afterWrite()
	}
	return true, nil
}

type grokTokenOwnerRefresh struct {
	calls     int
	onRefresh func()
	issued    map[string]any
}

func (*grokTokenOwnerRefresh) CanRefresh(account *Account) bool {
	return account.IsGrokOAuth() && account.GetCredential("refresh_token") != ""
}
func (*grokTokenOwnerRefresh) NeedsRefresh(account *Account, window time.Duration) bool {
	expires := account.GetCredentialAsTime("expires_at")
	return expires == nil || time.Until(*expires) <= window
}
func (*grokTokenOwnerRefresh) CacheKey(account *Account) string { return GrokTokenCacheKey(account) }
func (r *grokTokenOwnerRefresh) Refresh(_ context.Context, account *Account) (map[string]any, error) {
	r.calls++
	credentials := shallowCopyMap(account.Credentials)
	credentials["access_token"] = "same-owner-rotated-access"
	credentials["refresh_token"] = "same-owner-rotated-refresh"
	credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for key, value := range r.issued {
		credentials[key] = value
	}
	if r.onRefresh != nil {
		r.onRefresh()
	}
	return credentials, nil
}

// Request acquisition must not borrow a later login's token, identity or route.
// A rotation issued for the captured grant remains a positive control.
func TestGrokRequestTokenAcquisitionRetainsCapturedOwner(t *testing.T) {
	for _, phase := range []string{"cache-hit", "version-adoption", "refresh-reread", "refresh-response", "refresh-persisted", "lock-wait"} {
		for _, change := range []string{"unchanged", "relinked", "proxy-edited", "identity-edited", "storage-failure"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
				account.Credentials["sub"] = "captured-grok-user"
				account.Credentials["_token_version"] = int64(1)
				if phase == "refresh-reread" || phase == "refresh-response" || phase == "refresh-persisted" || phase == "lock-wait" {
					account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
				}
				repo := &grokTokenOwnerRepo{current: account, storageFailure: change == "storage-failure"}
				mutate := func() {
					switch change {
					case "relinked":
						account.Credentials["access_token"] = "replacement-owner-access"
						account.Credentials["refresh_token"] = "replacement-owner-refresh"
						account.Credentials["sub"] = "replacement-grok-user"
					case "proxy-edited":
						account.Proxy.Password = "replacement-owner-route"
					case "identity-edited":
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "9.9.9"}
					}
					if change != "unchanged" && change != "storage-failure" && phase != "refresh-response" {
						account.Credentials["access_token"] = "replacement-owner-access"
						account.Credentials["_token_version"] = int64(2)
					}
					if phase == "lock-wait" {
						account.Credentials["access_token"] = "same-owner-background-access"
						account.Credentials["_token_version"] = int64(2)
						account.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
					}
				}
				cache := &grokOwnerAcquisitionTokenCache{onRead: mutate, lockHeld: phase == "lock-wait"}
				if phase == "cache-hit" {
					cache.token = "private-grok-token"
					if change != "unchanged" && change != "storage-failure" {
						cache.token = "replacement-owner-access"
					}
				}
				if phase == "refresh-response" || phase == "refresh-persisted" {
					cache.onRead = nil
				}
				refresh := &grokTokenOwnerRefresh{}
				if phase == "refresh-response" {
					refresh.onRefresh = mutate
				}
				if phase == "refresh-persisted" {
					repo.afterWrite = mutate
				}
				tokens := NewGrokTokenProvider(repo, cache)
				tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), refresh)
				calls := 0
				upstream := &grokOutcomeHTTPUpstream{respond: func(request *http.Request, proxy string, _ int64) (*http.Response, error) {
					calls++
					require.Equal(t, "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080", proxy)
					require.Equal(t, "grok-shell/1.0.46 (linux; x86_64)", request.UserAgent())
					wantToken := "private-grok-token"
					if phase == "refresh-reread" || phase == "refresh-response" || phase == "refresh-persisted" {
						wantToken = "same-owner-rotated-access"
					}
					if phase == "lock-wait" {
						wantToken = "same-owner-background-access"
					}
					require.Equal(t, "Bearer "+wantToken, request.Header.Get("Authorization"))
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"text":"hello"}`))}, nil
				}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: repo, grokTokenProvider: tokens, httpUpstream: upstream}
				_, err := svc.ForwardGrokVoice(context.Background(), grokAuxiliaryInbound(http.MethodPost, "/v1/tts"), account, "tts", []byte(`{"text":"hello"}`), "application/json")
				positive := change == "unchanged" || change == "storage-failure" && (phase == "cache-hit" || phase == "version-adoption" || phase == "lock-wait")
				if positive {
					require.NoError(t, err)
					require.Equal(t, 1, calls)
					return
				}
				grokAuxiliaryAssertSafeError(t, err)
				for _, secret := range []string{"replacement-owner-access", "replacement-owner-refresh", "replacement-owner-route"} {
					require.NotContains(t, err.Error(), secret)
				}
				require.Zero(t, calls, "no request can mix the acquired route with a later owner's bearer")
				if phase == "refresh-persisted" && change != "storage-failure" {
					require.Equal(t, 1, repo.writes, "the original rotation committed before the independent owner change")
				} else {
					require.Zero(t, repo.writes, "a late refresh cannot replace another owner or route")
				}
				require.Zero(t, cache.writes, "a rejected result cannot populate the token cache")
				if phase == "refresh-response" || phase == "refresh-persisted" || change == "storage-failure" {
					require.Equal(t, 1, refresh.calls)
				} else {
					require.Zero(t, refresh.calls, "reject replacement state before issuer I/O")
				}
			})
		}
	}
}

func TestGrokManualTokenRefreshRetainsCapturedOwner(t *testing.T) {
	for _, change := range []string{"unchanged", "relinked", "proxy-edited", "identity-edited", "storage-failure"} {
		t.Run(change, func(t *testing.T) {
			account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
			account.Schedulable = false
			account.Credentials["sub"] = "captured-grok-user"
			account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
			repo := &grokTokenOwnerRepo{current: account, storageFailure: change == "storage-failure"}
			refresh := &grokTokenOwnerRefresh{onRefresh: func() {
				switch change {
				case "relinked":
					account.Credentials["access_token"], account.Credentials["refresh_token"], account.Credentials["sub"] = "replacement-owner-access", "replacement-owner-refresh", "replacement-grok-user"
					account.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
				case "proxy-edited":
					account.Proxy.Password = "replacement-owner-route"
				case "identity-edited":
					account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "9.9.9"}
				}
			}}
			tokens := NewGrokTokenProvider(repo, nil)
			tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), refresh)
			token, err := tokens.GetAccessTokenForManualTest(context.Background(), account)
			require.Equal(t, 1, refresh.calls)
			require.False(t, account.Schedulable, "manual token refresh must not clear an administrative pause")
			if change == "unchanged" {
				require.NoError(t, err)
				require.Equal(t, "same-owner-rotated-access", token)
				require.Equal(t, 1, repo.writes)
				return
			}
			require.Error(t, err)
			require.Empty(t, token, "a manual test cannot adopt a replacement login after its issuer call")
			require.Zero(t, repo.writes)
		})
	}
}

func TestGrokOwnedTokenRotationPreservesOfficialProfileMetadata(t *testing.T) {
	for _, mode := range []string{"request", "manual"} {
		for _, change := range []string{"profile-metadata", "changed-subject", "changed-team", "changed-client", "changed-identity", "changed-base"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
				account.Credentials["sub"], account.Credentials["team_id"], account.Credentials["client_id"] = "captured-user", "captured-team", "captured-client"
				account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
				repo := &grokTokenOwnerRepo{current: account}
				refresh := &grokTokenOwnerRefresh{issued: map[string]any{"scope": "openid email profile offline_access", "email": "updated-profile@example.test", "subscription_tier": "pro", "entitlement_status": "active"}}
				switch change {
				case "changed-subject":
					refresh.issued["sub"] = "different-user"
				case "changed-team":
					refresh.issued["team_id"] = "different-team"
				case "changed-client":
					refresh.issued["client_id"] = "different-client"
				case "changed-identity":
					refresh.issued[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "9.9.9"}
				case "changed-base":
					refresh.issued["base_url"] = "https://different-relay.example/v1"
				}
				tokens := NewGrokTokenProvider(repo, nil)
				tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), refresh)
				var token string
				var err error
				if mode == "manual" {
					account.Schedulable = false
					token, err = tokens.GetAccessTokenForManualTest(context.Background(), account)
				} else {
					token, err = tokens.GetAccessToken(context.Background(), account)
				}
				require.Equal(t, 1, refresh.calls)
				if change != "profile-metadata" {
					require.Error(t, err)
					require.Empty(t, token)
					require.Zero(t, repo.writes, "an issuer response cannot change the captured principal, client fingerprint or destination")
					return
				}
				require.NoError(t, err, "official refresh profile fields may change within the same principal and client")
				require.Equal(t, "same-owner-rotated-access", token)
				require.Equal(t, "same-owner-rotated-refresh", account.GetCredential("refresh_token"))
				require.Equal(t, "updated-profile@example.test", account.GetCredential("email"))
				require.Equal(t, "openid email profile offline_access", account.GetCredential("scope"))
				require.Equal(t, 1, repo.writes)
			})
		}
	}
}

type grokOwnerAcquisitionTokenCache struct {
	GeminiTokenCache
	token    string
	onRead   func()
	writes   int
	lockHeld bool
}

func (c *grokOwnerAcquisitionTokenCache) GetAccessToken(context.Context, string) (string, error) {
	if c.onRead != nil {
		hook := c.onRead
		c.onRead = nil
		hook()
	}
	if c.token == "" {
		return "", errors.New("not cached")
	}
	return c.token, nil
}
func (c *grokOwnerAcquisitionTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	c.writes++
	return nil
}
func (c *grokOwnerAcquisitionTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return !c.lockHeld, nil
}
func (*grokOwnerAcquisitionTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }
func (*grokOwnerAcquisitionTokenCache) DeleteAccessToken(context.Context, string) error  { return nil }
