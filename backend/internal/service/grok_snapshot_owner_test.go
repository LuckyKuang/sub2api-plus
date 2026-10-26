//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestGrokBillingLateResponseCannotAuthorizeNewCredential(t *testing.T) {
	old := healthyGrokQuotaOAuthAccount(90601)
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{old.ID: old}}}
	started, release := make(chan struct{}), make(chan struct{})
	usage := 12.0
	upstream := &grokHybridUpstream{weeklyUsagePercent: &usage, billingStarted: started, billingRelease: release}
	svc := NewGrokQuotaService(repo, nil, NewGrokTokenProvider(repo, nil), upstream, nil)
	done := make(chan *GrokQuotaProbeResult, 1)
	errs := make(chan error, 1)
	go func() { result, err := svc.ProbeBilling(context.Background(), old.ID); done <- result; errs <- err }()
	<-started
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new-login-token"
	current.Credentials["refresh_token"] = "new-login-refresh"
	repo.accountsByID[old.ID] = current
	close(release)
	result := <-done
	require.NoError(t, <-errs)
	require.False(t, result.Persisted)
	require.Zero(t, repo.updateCalls)
	eligible, reason := current.GrokMediaGenerationEligibility()
	require.False(t, eligible)
	require.Equal(t, "billing_unobserved", reason)
}

func TestGrokLateResponseCannotPublishNewCredentialState(t *testing.T) {
	old := healthyGrokOAuthGatewayTestAccount(90602, "old-token")
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new-token"
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{old.ID: current}}}
	svc := &OpenAIGatewayService{accountRepo: repo}
	ctx := withRequestCredentialOwner(context.Background(), old, "old-token")
	svc.handleGrokAccountUpstreamError(ctx, old, http.StatusTooManyRequests, http.Header{"Retry-After": []string{"120"}}, nil)
	require.Zero(t, repo.updateCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(current))
	svc.tempUnscheduleGrok(ctx, old, time.Minute, "old quota error")
	require.Zero(t, repo.tempUnschedCalls)
	markGrokModelQuotaBlock(old, "grok-4.5", time.Now().Add(time.Hour))
	require.True(t, isGrokModelQuotaBlocked(old, "grok-4.5", time.Now()))
	require.False(t, isGrokModelQuotaBlocked(current, "grok-4.5", time.Now()))
}

func TestGrokSnapshotOwnershipReadAndExplicitOverride(t *testing.T) {
	account := healthyGrokQuotaOAuthAccount(90603)
	usage := 12.0
	billing := &xai.BillingSummary{StatusCode: http.StatusOK, UsagePercent: &usage}
	account.Extra = map[string]any{grokBillingExtraKey: billing}
	eligible, reason := account.GrokMediaGenerationEligibility()
	require.False(t, eligible)
	require.Equal(t, "billing_unobserved", reason)
	billing.CredentialGeneration = grokCredentialGeneration(account)
	eligible, reason = account.GrokMediaGenerationEligibility()
	require.True(t, eligible)
	require.Equal(t, "eligible", reason)
	account.Credentials["access_token"] = "replacement"
	eligible, reason = account.GrokMediaGenerationEligibility()
	require.False(t, eligible)
	require.Equal(t, "billing_unobserved", reason)
	account.Extra[GrokMediaEligibleExtraKey] = true
	eligible, reason = account.GrokMediaGenerationEligibility()
	require.True(t, eligible)
	require.Equal(t, "override_enabled", reason)
}

func TestCredentialProjectionReadsRuntimeStateButCannotMutate(t *testing.T) {
	account := healthyGrokOAuthGatewayTestAccount(90604, "current-token")
	projection := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type, CredentialGeneration: CredentialStateGeneration(account)}
	require.Nil(t, projection.Credentials, "slim OAuth candidates omit the complete grant")
	svc := &OpenAIGatewayService{}
	svc.BlockAccountScheduling(account, time.Now().Add(time.Minute), "observed current grant")
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(projection))
	account.Credentials["access_token"] = "replacement-token"
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	owner, known := credentialStateMutationOwner(context.Background(), projection)
	require.False(t, known)
	require.Nil(t, owner)
	require.Nil(t, grokResponseOwner(context.Background(), projection))
}

func TestGrokQuotaThresholdCannotCrossCredentialGeneration(t *testing.T) {
	account := healthyGrokOAuthGatewayTestAccount(90605, "old-token")
	account.Extra = map[string]any{
		"grok_sched_utilization":           99.0,
		"grok_sched_reset_at":              time.Now().Add(time.Hour).Format(time.RFC3339),
		"grok_sched_credential_generation": grokCredentialGeneration(account),
	}
	require.True(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformGrok: 90}, time.Now()).ShouldPause)
	account.Credentials["access_token"] = "replacement-token"
	require.False(t, EvaluateAccountSchedulingThreshold(account, map[string]int{PlatformGrok: 90}, time.Now()).ShouldPause)
}

func TestGrokManualProbeLateResponseCannotPublishState(t *testing.T) {
	old := healthyGrokOAuthGatewayTestAccount(90606, "old-token")
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new-token"
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{old.ID: current}}}
	svc := &AccountTestService{accountRepo: repo}
	ctx := withRequestCredentialOwner(context.Background(), old, "old-token")
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusPaymentRequired} {
		resp := &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"60"}}, Body: io.NopCloser(strings.NewReader(`{"error":"old grant failed"}`))}
		svc.observeGrokTestResponse(ctx, old, resp)
	}
	require.Zero(t, repo.updateCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.Zero(t, repo.tempUnschedCalls)
}

func TestGrokLateRefreshCleanupCannotClearNewGrantReauthFlag(t *testing.T) {
	old := healthyGrokOAuthGatewayTestAccount(90607, "old-token")
	current := snapshotRequestCredentialAccount(old)
	current.Credentials["access_token"] = "new-token"
	current.Extra = map[string]any{"grok_needs_reauth": true}
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{old.ID: current}}}
	clearGrokNeedsReauthExtra(context.Background(), repo, old)
	require.Zero(t, repo.updateCalls)
	require.True(t, accountGrokNeedsReauth(current))
	clearGrokNeedsReauthExtra(context.Background(), repo, current)
	require.Equal(t, 1, repo.updateCalls)
	require.False(t, accountGrokNeedsReauth(current))
}
