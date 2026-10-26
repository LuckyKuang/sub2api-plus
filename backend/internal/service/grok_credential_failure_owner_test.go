//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type grokCredentialOwnerFailureRefresh struct {
	onRefresh func()
	failure   error
	calls     int
}

func (*grokCredentialOwnerFailureRefresh) CanRefresh(*Account) bool                  { return true }
func (*grokCredentialOwnerFailureRefresh) NeedsRefresh(*Account, time.Duration) bool { return true }
func (*grokCredentialOwnerFailureRefresh) CacheKey(account *Account) string {
	return GrokTokenCacheKey(account)
}
func (r *grokCredentialOwnerFailureRefresh) Refresh(context.Context, *Account) (map[string]any, error) {
	r.calls++
	if r.onRefresh != nil {
		r.onRefresh()
	}
	return nil, r.failure
}

type grokCredentialOwnerFailureRepo struct {
	*tokenRefreshAccountRepo
	conditionalCalls int
}

func (r *grokCredentialOwnerFailureRepo) SetGrokCredentialErrorIfMatch(ctx context.Context, id int64, snapshot GrokCredentialMutationSnapshot, reason string) (bool, error) {
	r.conditionalCalls++
	return r.tokenRefreshAccountRepo.SetGrokCredentialErrorIfMatch(ctx, id, snapshot, reason)
}

func (r *grokCredentialOwnerFailureRepo) SetGrokCredentialTempUnschedulableIfMatch(ctx context.Context, id int64, snapshot GrokCredentialMutationSnapshot, until time.Time, reason string) (bool, error) {
	r.conditionalCalls++
	return r.tokenRefreshAccountRepo.SetGrokCredentialTempUnschedulableIfMatch(ctx, id, snapshot, until, reason)
}

func grokCredentialOwnerFailureAccount() *Account {
	account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
	account.Credentials["sub"], account.Credentials["team_id"], account.Credentials["client_id"] = "captured-user", "captured-team", "captured-client"
	account.Credentials["_token_version"] = int64(1)
	account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	return account
}

// A failed issuer request belongs to the route actually sent. Editing that
// proxy row while the issuer is in flight must not quarantine the new route.
// Name/latency edits and unchanged routes retain normal failure handling.
func TestGrokCredentialIssuerFailureRetainsCapturedProxyRow(t *testing.T) {
	for _, failure := range []struct {
		name       string
		err        error
		wantReason GatewayFailureReason
	}{
		{name: "permanent", err: infraerrors.New(http.StatusForbidden, "GROK_OAUTH_ENTITLEMENT_DENIED", "access_denied"), wantReason: GrokCredentialReasonEntitlement},
		{name: "transient", err: errors.New("temporary refresh transport failure"), wantReason: GrokCredentialReasonRefreshTransient},
	} {
		for _, change := range []string{"unchanged", "display-metadata", "protocol", "host", "port", "username", "password", "status", "expiry"} {
			t.Run(failure.name+"/"+change, func(t *testing.T) {
				account := grokCredentialOwnerFailureAccount()
				base := &tokenRefreshAccountRepo{}
				base.accountsByID = map[int64]*Account{account.ID: account}
				repo := &grokCredentialOwnerFailureRepo{tokenRefreshAccountRepo: base}
				refresh := &grokCredentialOwnerFailureRefresh{failure: failure.err, onRefresh: func() {
					switch change {
					case "display-metadata":
						account.Proxy.Name = "renamed proxy"
					case "protocol":
						account.Proxy.Protocol = "socks5"
					case "host":
						account.Proxy.Host = "replacement.proxy.example"
					case "port":
						account.Proxy.Port = 9090
					case "username":
						account.Proxy.Username = "replacement-proxy-user"
					case "password":
						account.Proxy.Password = "replacement-proxy-password"
					case "status":
						account.Proxy.Status = StatusDisabled
					case "expiry":
						until := time.Now().Add(time.Hour)
						account.Proxy.ExpiresAt = &until
					}
				}}
				cache := &grokTokenCacheForProviderTest{lockResult: true}
				tokens := NewGrokTokenProvider(repo, cache)
				tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), refresh)
				svc := &OpenAIGatewayService{accountRepo: repo, grokTokenProvider: tokens}

				token, kind, err := svc.GetRequestCredential(context.Background(), nil, account)

				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Empty(t, token)
				require.Empty(t, kind)
				require.Equal(t, 1, refresh.calls, "the failed issuer attempt must actually execute")
				if change == "unchanged" || change == "display-metadata" {
					require.Equal(t, failure.wantReason, failover.Reason)
					require.Equal(t, 1, repo.conditionalCalls)
					require.Equal(t, 1, base.setErrorCalls+base.setTempUnschedCalls)
					return
				}
				require.Equal(t, GrokCredentialReasonAccountChanged, failover.Reason)
				require.Equal(t, NextAccountRetry, failover.NextAccountAction)
				require.Zero(t, repo.conditionalCalls, "reject a changed proxy row before any quarantine write")
				require.Zero(t, base.setErrorCalls+base.setTempUnschedCalls)
				require.Empty(t, cache.deletedKeys)
				require.Equal(t, StatusActive, account.Status)
				require.True(t, account.Schedulable)
				require.Nil(t, account.TempUnschedulableUntil)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				grokAuxiliaryAssertSafeError(t, err)
			})
		}
	}
}

// A failure CAS miss may recover a concurrent access rotation of the captured refresh grant,
// but must never lend a request a replacement login, identity, or proxy token.
func TestGrokCredentialFailureCASMissRetainsCapturedGrant(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{name: "permanent", err: infraerrors.New(http.StatusForbidden, "GROK_OAUTH_ENTITLEMENT_DENIED", "access_denied")},
		{name: "transient", err: errors.New("temporary refresh transport failure")},
	} {
		for _, change := range []string{"same-grant-access-rotation", "replacement-grant", "subject", "team", "client", "identity", "base", "proxy-auth", "proxy-lifecycle"} {
			t.Run(failure.name+"/"+change, func(t *testing.T) {
				account := grokCredentialOwnerFailureAccount()
				base := &tokenRefreshAccountRepo{}
				base.accountsByID = map[int64]*Account{account.ID: account}
				repo := &grokCredentialOwnerFailureRepo{tokenRefreshAccountRepo: base}
				var latest *Account
				base.beforeConditionalState = func() {
					replacement := *account
					replacement.Credentials = shallowCopyMap(account.Credentials)
					proxy := *account.Proxy
					replacement.Proxy = &proxy
					replacement.Credentials["access_token"] = "concurrent-rotated-access"
					replacement.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
					replacement.Credentials["_token_version"] = int64(2)
					switch change {
					case "replacement-grant":
						replacement.Credentials["refresh_token"] = "replacement-grant-refresh"
					case "subject":
						replacement.Credentials["sub"] = "replacement-user"
					case "team":
						replacement.Credentials["team_id"] = "replacement-team"
					case "client":
						replacement.Credentials["client_id"] = "replacement-client"
					case "identity":
						replacement.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "9.9.9"}
					case "base":
						replacement.Credentials["base_url"] = "https://replacement-relay.example/v1"
					case "proxy-auth":
						replacement.Proxy.Password = "replacement-proxy-password"
					case "proxy-lifecycle":
						replacement.Proxy.Status = StatusDisabled
					}
					latest = &replacement
					base.accountsByID[account.ID] = latest
				}
				cache := &grokTokenCacheForProviderTest{lockResult: true}
				refresh := &grokCredentialOwnerFailureRefresh{failure: failure.err}
				tokens := NewGrokTokenProvider(repo, cache)
				tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), refresh)
				svc := &OpenAIGatewayService{accountRepo: repo, grokTokenProvider: tokens}

				token, kind, err := svc.GetRequestCredential(context.Background(), nil, account)

				require.Equal(t, 1, refresh.calls)
				require.Equal(t, 1, repo.conditionalCalls, "the independent change must occur at the storage boundary")
				require.NotNil(t, latest)
				require.Zero(t, base.setErrorCalls+base.setTempUnschedCalls)
				require.Empty(t, cache.deletedKeys)
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "roll back only the failed attempt's speculative block")
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(latest))
				if change == "same-grant-access-rotation" {
					require.NoError(t, err)
					require.Equal(t, "concurrent-rotated-access", token)
					require.Equal(t, "oauth", kind)
					return
				}
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, GrokCredentialReasonAccountChanged, failover.Reason)
				require.Equal(t, NextAccountRetry, failover.NextAccountAction)
				require.Empty(t, token, "replacement credentials must not escape through failure recovery")
				require.Empty(t, kind)
				grokAuxiliaryAssertSafeError(t, err)
			})
		}
	}
}
