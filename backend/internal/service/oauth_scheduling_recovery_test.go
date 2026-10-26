//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type oauthSchedulingRecoveryRepo struct {
	AccountRepository
	account       *Account
	afterCommit   func()
	fail          bool
	miss          bool
	capturedExtra map[string]any
}

func (r *oauthSchedulingRecoveryRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *oauthSchedulingRecoveryRepo) UpdateOAuthCredentialsIfUnchanged(_ context.Context, expected *Account, accountType string, credentials, extra map[string]any, recover bool) (bool, error) {
	r.capturedExtra = extra
	if r.fail {
		return false, errors.New("injected credential storage failure")
	}
	if r.miss || !testGrokOutcomeOwnersEqual(r.account, expected) {
		return false, nil
	}
	r.account.Type, r.account.Credentials = accountType, shallowCopyMap(credentials)
	if recover {
		r.account.Status, r.account.ErrorMessage = StatusActive, ""
		r.account.TempUnschedulableUntil, r.account.TempUnschedulableReason = nil, ""
	}
	if r.afterCommit != nil {
		r.afterCommit()
	}
	return true, nil
}

func TestOAuthCredentialRecoveryOnlyClearsCapturedRuntimeGeneration(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok} {
		for _, change := range []string{"unchanged", "same-owner-new-failure", "relinked", "proxy-edited", "no-prior-block", "storage-failure", "owner-cas-miss"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
				account.ID, account.Platform = time.Now().UnixNano(), platform
				if platform == PlatformOpenAI {
					delete(account.Credentials, outboundIdentityCredential)
					account.Credentials["user_agent"] = DefaultOpenAICodexUserAgent
				}
				account.Credentials["team_id"] = "recovery-team-" + time.Now().Format("150405.000000000")
				oldOwner, err := CaptureOAuthCredentialOwner(account)
				require.NoError(t, err)
				gateway := &OpenAIGatewayService{}
				until := time.Now().Add(time.Minute)
				if change != "no-prior-block" {
					gateway.BlockAccountScheduling(oldOwner, until, "old credential failure")
					if platform == PlatformGrok {
						markGrokModelTransientBlock(oldOwner, "grok-4.5", until)
					}
				}
				if platform == PlatformGrok {
					markGrokTeamModelRateLimit(oldOwner, "grok-4.5", until)
				}
				repo := &oauthSchedulingRecoveryRepo{account: account, fail: change == "storage-failure", miss: change == "owner-cas-miss"}
				repo.afterCommit = func() {
					switch change {
					case "same-owner-new-failure":
						gateway.BlockAccountScheduling(oldOwner, until, "new same-deadline credential failure")
						if platform == PlatformGrok {
							markGrokModelTransientBlock(oldOwner, "grok-4.5", until)
							markGrokModelTransientBlock(oldOwner, "grok-4.7", until)
						}
					case "relinked", "proxy-edited", "no-prior-block":
						if change == "relinked" {
							account.Credentials["access_token"] = "post-commit-replacement-token"
						}
						if change == "proxy-edited" {
							account.Proxy.Password = "post-commit-replacement-route"
						}
						gateway.BlockAccountScheduling(account, until, "replacement owner failure")
						if platform == PlatformGrok {
							markGrokModelTransientBlock(account, "grok-4.5", until)
						}
					}
				}
				admin := &adminServiceImpl{accountRepo: repo, runtimeBlocker: gateway}
				_, err = admin.CommitOAuthCredentials(context.Background(), oldOwner, AccountTypeOAuth,
					map[string]any{"access_token": "reauthorized-owner-token"}, nil, true)
				if change == "storage-failure" || change == "owner-cas-miss" {
					require.Error(t, err)
					require.True(t, gateway.isOpenAIAccountRuntimeBlocked(oldOwner), "a failed credential commit cannot authorize runtime recovery")
				} else {
					require.NoError(t, err)
					keepOld := change == "same-owner-new-failure"
					require.Equal(t, keepOld, gateway.isOpenAIAccountRuntimeBlocked(oldOwner), "recovery clears the captured generation and preserves every later failure, even at the same deadline")
					if change == "relinked" || change == "proxy-edited" || change == "no-prior-block" {
						require.True(t, gateway.isOpenAIAccountRuntimeBlocked(account), "post-commit replacement failure must survive old recovery")
					}
				}
				if platform == PlatformGrok {
					keepModel := change == "same-owner-new-failure" || change == "storage-failure" || change == "owner-cas-miss"
					require.Equal(t, keepModel, isGrokModelQuotaBlocked(oldOwner, "grok-4.5", time.Now()))
					if change == "same-owner-new-failure" {
						require.True(t, isGrokModelQuotaBlocked(oldOwner, "grok-4.7", time.Now()))
					}
					if change == "relinked" || change == "proxy-edited" || change == "no-prior-block" {
						require.True(t, isGrokModelQuotaBlocked(account, "grok-4.5", time.Now()))
					}
					require.True(t, isGrokTeamModelRateLimited(oldOwner, "grok-4.5", time.Now()), "a login recovery cannot erase true shared team quota")
				}
			})
		}
	}
}

func TestOAuthCredentialOwnerIncludesSavedRecoveryState(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
	account.Extra = map[string]any{RuntimeRecoveryGenerationExtraKey: "saved-recovery", "retained": "original"}
	old := OAuthCredentialOwner(account)
	require.NotEmpty(t, old)
	account.Extra["retained"] = "unrelated-observation"
	require.Equal(t, old, OAuthCredentialOwner(account), "observations do not replace the login owner")
	account.Extra[RuntimeRecoveryGenerationExtraKey] = "new-recovery"
	require.NotEqual(t, old, OAuthCredentialOwner(account), "an older authorization cannot clear recovery state established by a later administrator operation")
}

func TestOAuthCredentialCommitRejectsCallerRecoveryGeneration(t *testing.T) {
	account := grokStrictProxyAccount(AccountTypeOAuth, "valid-bound")
	account.Extra = map[string]any{RuntimeRecoveryGenerationExtraKey: "saved-generation"}
	owner, err := CaptureOAuthCredentialOwner(account)
	require.NoError(t, err)
	repo := &oauthSchedulingRecoveryRepo{account: account}
	admin := &adminServiceImpl{accountRepo: repo}
	_, err = admin.CommitOAuthCredentials(context.Background(), owner, AccountTypeOAuth, map[string]any{"access_token": "authorized-rotation"}, map[string]any{RuntimeRecoveryGenerationExtraKey: "caller-forged-generation", "retained": "allowed"}, false)
	require.NoError(t, err)
	require.NotContains(t, repo.capturedExtra, RuntimeRecoveryGenerationExtraKey, "only the atomic recovery writer owns the generation")
	require.Equal(t, "allowed", repo.capturedExtra["retained"])
}
