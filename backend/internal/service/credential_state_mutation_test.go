//go:build unit

package service

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type credentialMutationFenceRepo struct {
	rateLimitAccountRepoStub
	current *Account
}

func (r *credentialMutationFenceRepo) ApplyCredentialStateMutation(ctx context.Context, owner *Account, mutation CredentialStateMutation) (bool, error) {
	if !sameRequestCredentialGrant(owner, r.current) {
		return false, nil
	}
	return r.rateLimitAccountRepoStub.ApplyCredentialStateMutation(ctx, owner, mutation)
}
func (r *credentialMutationFenceRepo) SetCredentialErrorIfUnchanged(ctx context.Context, expectedOwner *Account, message string) (bool, error) {
	id := expectedOwner.ID
	platform := expectedOwner.Platform
	kind := expectedOwner.Type
	creds := expectedOwner.Credentials
	proxy := expectedOwner.ProxyID
	if r.current.ID != id || r.current.Platform != platform || r.current.Type != kind || !reflect.DeepEqual(creds, r.current.Credentials) || (!reflect.DeepEqual(proxy, r.current.ProxyID) || !reflect.DeepEqual(expectedOwner.Proxy, r.current.Proxy)) {
		return false, nil
	}
	return true, r.SetError(ctx, id, message)
}

func TestCNReactiveErrorsFenceTransmittedCredentials(t *testing.T) {
	for _, quota := range []bool{false, true} {
		for _, cause := range []string{"quota403", "balance", "429", "concurrency403"} {
			t.Run(cause+map[bool]string{true: "_quota", false: "_no_quota"}[quota], func(t *testing.T) {
				old := &Account{ID: 91, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "old", "account_mode": AccountModeCoding}, Extra: map[string]any{}}
				resetAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
				if quota {
					old.Extra[cnExtraKey(PlatformKimi, cnExtraSuffix5hReset)] = resetAt.Format(time.RFC3339)
					bindCNQuotaTestSnapshot(old)
				}
				current := snapshotRequestCredentialAccount(old)
				current.Extra = cloneRequestCredentialMap(old.Extra)
				current.Credentials["api_key"] = "new"
				if quota {
					bindCNQuotaTestSnapshot(current)
					require.Equal(t, &resetAt, cnProviderQuotaSnapshotReset(old, time.Now()))
					require.Equal(t, &resetAt, cnProviderQuotaSnapshotReset(current, time.Now()))
				}
				repo := &credentialMutationFenceRepo{current: current}
				blocker := &runtimeBlockRecorder{}
				svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
				svc.SetAccountRuntimeBlocker(blocker)
				apply := func(a *Account) {
					switch cause {
					case "quota403":
						svc.handleCNProviderQuotaExhausted403(context.Background(), a, "quota will reset")
					case "balance":
						svc.handleCNProviderInsufficientBalance(context.Background(), a, "insufficient balance")
					case "429":
						svc.applyCNProviderReactive429(context.Background(), a, http.Header{}, []byte(`{"error":"rate_limit"}`))
					case "concurrency403":
						svc.handleCNProviderConcurrencyLimit403(context.Background(), a)
					}
				}
				apply(old)
				require.Zero(t, repo.tempCalls+repo.rateLimitedCalls+repo.updateExtraCalls)
				require.Empty(t, blocker.accounts)
				apply(current)
				require.Positive(t, repo.tempCalls+repo.rateLimitedCalls)
				require.Len(t, blocker.accounts, 1)
				if quota && (cause == "quota403" || cause == "429") {
					require.Equal(t, 1, repo.rateLimitedCalls, "must consume the matching quota snapshot")
					require.True(t, resetAt.Equal(repo.lastRateLimitedAt), "cooldown must end at the observed reset, not fallback duration")
					require.Zero(t, repo.tempCalls)
				}
			})
		}
	}
}

func TestProbeLate401CannotDisableReauthorizedAccount(t *testing.T) {
	old := &Account{ID: 101, Platform: PlatformKimi, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "old"}}
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["api_key"] = "new"
	repo := &credentialMutationFenceRepo{current: current}
	svc := &AccountTestService{accountRepo: repo}
	svc.setProbeCredentialError(context.Background(), old, nil, "old401")
	require.Zero(t, repo.setErrorCalls)
	svc.setProbeCredentialError(context.Background(), current, nil, "new401")
	require.Equal(t, 1, repo.setErrorCalls)
}

func TestRuntimeCredentialBlocksCannotCrossReauthorization(t *testing.T) {
	svc := &OpenAIGatewayService{}
	old := &Account{ID: 111, Platform: PlatformGrok, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "old"}}
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new"
	svc.BlockAccountScheduling(old, time.Now().Add(time.Minute), "old")
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(current))
	svc.BlockAccountScheduling(current, time.Now().Add(time.Minute), "new")
	currentBlock := svc.peekOpenAIAccountRuntimeBlock(current)
	svc.BlockAccountScheduling(old, time.Now().Add(time.Hour), "late_old")
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(current))
	require.Equal(t, currentBlock.generation, svc.peekOpenAIAccountRuntimeBlock(current).generation)
	svc.ClearAccountSchedulingBlock(old.ID)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(old))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(current))
}

func TestAgentIdentityFreezesSigningOwnerWithoutBearerToken(t *testing.T) {
	_, privateKey := newTestAgentIdentityKey(t)
	account := &Account{ID: 119, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime-original", "agent_private_key": privateKey,
	}}
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, account, "")
	attempt := withRequestCredentialOwner(ctx, account, "")
	account.Credentials["agent_runtime_id"] = "runtime-relinked"
	owner, ok := requestCredentialOwner(attempt, account)
	require.True(t, ok)
	require.Equal(t, "runtime-original", owner.GetCredential("agent_runtime_id"))

	for _, credentials := range []map[string]any{
		{"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "runtime", "agent_private_key": "invalid"},
		{"auth_mode": OpenAIAuthModeAgentIdentity, "agent_private_key": privateKey},
		{"access_token": ""},
	} {
		invalid := &Account{ID: 120, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: credentials}
		invalidCtx := withRequestCredentialOwner(WithOutboundIdentityScope(context.Background(), nil), invalid, "")
		_, known := requestCredentialOwner(invalidCtx, invalid)
		require.False(t, known)
	}
}
