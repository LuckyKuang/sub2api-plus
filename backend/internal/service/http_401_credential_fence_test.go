//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type http401CredentialFenceRepo struct {
	AccountRepository
	current                      *Account
	attempted                    *Account
	casCalls, unconditionalCalls int
	writeErr                     error
}

func (r *http401CredentialFenceRepo) GetByID(context.Context, int64) (*Account, error) {
	return snapshotOAuthRefreshAccount(r.current), nil
}

func (r *http401CredentialFenceRepo) SetError(context.Context, int64, string) error {
	r.unconditionalCalls++
	return errors.New("unconditional credential mutation is forbidden")
}

func (r *http401CredentialFenceRepo) SetCredentialErrorIfUnchanged(_ context.Context, expectedOwner *Account, message string) (bool, error) {
	id := expectedOwner.ID
	platform := expectedOwner.Platform
	kind := expectedOwner.Type
	credentials := expectedOwner.Credentials
	proxyID := expectedOwner.ProxyID
	r.casCalls++
	r.attempted = snapshotOAuthRefreshAccount(expectedOwner)
	if r.writeErr != nil {
		return false, r.writeErr
	}
	current := r.current
	if current == nil || current.Status != StatusActive || current.ID != id || current.Platform != platform || current.Type != kind || !reflect.DeepEqual(current.Credentials, credentials) || (!reflect.DeepEqual(current.ProxyID, proxyID) || !reflect.DeepEqual(current.Proxy, expectedOwner.Proxy)) {
		return false, nil
	}
	current.Status, current.ErrorMessage, current.Schedulable = StatusError, message, false
	return true, nil
}

// HTTP failures belong to the sent grant, across both credential types and all
// seven official provider mappings. A later login/key replacement must retain
// its durable and process-local scheduling state; an unchanged failed grant
// must still be quarantined and authorize failover.
func TestHTTP401PermanentCredentialQuarantineUsesExactGrant(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, kind := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			for _, rotated := range []bool{false, true} {
				name := platform + "/" + kind + "/unchanged"
				if rotated {
					name = platform + "/" + kind + "/reauthorized"
				}
				t.Run(name, func(t *testing.T) {
					selected := &Account{ID: 41, Platform: platform, Type: kind, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "sent-access", "api_key": "sent-key"}}
					repo := &http401CredentialFenceRepo{current: snapshotOAuthRefreshAccount(selected)}
					if rotated {
						repo.current.Credentials["access_token"], repo.current.Credentials["api_key"] = "new-access", "new-key"
					}
					blocker := &runtimeBlockRecorder{}
					svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
					svc.SetAccountRuntimeBlocker(blocker)
					require.True(t, svc.HandleUpstreamError(context.Background(), selected, http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"message":"invalid credential"}}`)))
					require.Equal(t, 1, repo.casCalls)
					require.Zero(t, repo.unconditionalCalls)
					require.Equal(t, selected.Credentials, repo.attempted.Credentials, "CAS must carry the rejected grant, never a reread authorization")
					if rotated {
						require.Equal(t, StatusActive, repo.current.Status)
						require.True(t, repo.current.Schedulable)
						require.Empty(t, blocker.accounts)
					} else {
						require.Equal(t, StatusError, repo.current.Status)
						require.False(t, repo.current.Schedulable)
						require.Len(t, blocker.accounts, 1)
					}
				})
			}
		}
	}
}

func TestHTTP401OpenAIPermanentOAuthFormsFenceReauthorization(t *testing.T) {
	for _, body := range []string{`{"error":{"code":"token_revoked"}}`, `{"detail":"Unauthorized"}`} {
		selected := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "sent-token", "refresh_token": "sent-refresh"}}
		repo := &http401CredentialFenceRepo{current: snapshotOAuthRefreshAccount(selected)}
		repo.current.Credentials["refresh_token"] = "new-login-refresh"
		blocker := &runtimeBlockRecorder{}
		svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		svc.SetAccountRuntimeBlocker(blocker)
		require.True(t, svc.HandleUpstreamError(context.Background(), selected, http.StatusUnauthorized, http.Header{}, []byte(body)))
		require.Equal(t, 1, repo.casCalls)
		require.Zero(t, repo.unconditionalCalls)
		require.Equal(t, StatusActive, repo.current.Status)
		require.Empty(t, blocker.accounts)
	}
}

func TestHTTP401ForwardKeepsFailoverWithoutBlockingNewGrant(t *testing.T) {
	for _, scenario := range []string{"unchanged", "reauthorized", "write-failure"} {
		t.Run(scenario, func(t *testing.T) {
			account := rawChatCompletionsTestAccount()
			account.Status, account.Schedulable = StatusActive, true
			repo := &http401CredentialFenceRepo{current: snapshotOAuthRefreshAccount(account)}
			if scenario == "reauthorized" {
				repo.current.Credentials["api_key"] = "new-login-key"
			}
			if scenario == "write-failure" {
				repo.writeErr = errors.New("durable state unavailable")
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_api_key","message":"invalid credential"}}`))}}
			rl := NewRateLimitService(repo, nil, rawChatCompletionsTestConfig(), nil, nil)
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), accountRepo: repo, rateLimitService: rl, httpUpstream: upstream}
			blocker := &runtimeBlockRecorder{}
			rl.SetAccountRuntimeBlocker(blocker)
			body := []byte(`{"model":"gpt-5.6-sol","input":"hello","stream":false}`)
			c := newOpenAIRejectedFieldTestContext(body)
			_, err := gateway.Forward(context.Background(), c, account, body)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.True(t, failover.ShouldRetryNextAccount())
			require.True(t, failover.AllowsOpenAIAccountSwitch())
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "Bearer sk-test", upstream.lastReq.Header.Get("Authorization"))
			require.False(t, c.Writer.Written())
			require.Equal(t, 1, repo.casCalls)
			require.Zero(t, repo.unconditionalCalls)
			if scenario == "unchanged" {
				require.Equal(t, StatusError, repo.current.Status)
				require.Len(t, blocker.accounts, 1)
			} else {
				require.Equal(t, StatusActive, repo.current.Status)
				require.Empty(t, blocker.accounts)
				require.False(t, gateway.isOpenAIAccountRuntimeBlocked(account), "the gateway wrapper must not bypass the failed CAS")
			}
		})
	}
}

func TestHTTP401ShadowQuarantineRequiresFrozenSentOwner(t *testing.T) {
	for _, scenario := range []string{"unchanged", "reauthorized", "no-request-snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			owner := &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "sent-parent-token"}}
			parentID := owner.ID
			shadow := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
			repo := &http401CredentialFenceRepo{current: snapshotOAuthRefreshAccount(owner)}
			ctx := context.Background()
			if scenario != "no-request-snapshot" {
				ctx = WithOutboundIdentityScope(ctx, nil)
				token, _, err := (&OpenAIGatewayService{accountRepo: repo}).GetAccessToken(ctx, shadow)
				require.NoError(t, err)
				require.Equal(t, "sent-parent-token", token)
				ctx = withRequestCredentialOwner(ctx, shadow, token)
			}
			if scenario == "reauthorized" {
				repo.current.Credentials["access_token"] = "new-parent-login"
			}
			blocker := &runtimeBlockRecorder{}
			rl := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			rl.SetAccountRuntimeBlocker(blocker)
			require.True(t, rl.HandleUpstreamError(ctx, shadow, http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"code":"token_revoked"}}`)))
			require.Zero(t, repo.unconditionalCalls)
			if scenario == "no-request-snapshot" {
				require.Zero(t, repo.casCalls)
			} else {
				require.Equal(t, 1, repo.casCalls)
				require.Equal(t, owner.ID, repo.attempted.ID)
				require.Equal(t, "sent-parent-token", repo.attempted.GetCredential("access_token"))
			}
			if scenario == "unchanged" {
				require.Equal(t, StatusError, repo.current.Status)
				require.Len(t, blocker.accounts, 1)
				require.Equal(t, owner.ID, blocker.accounts[0].ID)
			} else {
				require.Equal(t, StatusActive, repo.current.Status)
				require.Empty(t, blocker.accounts)
			}
		})
	}
}
