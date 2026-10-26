//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

func projectTokenAccount() *Account {
	return &Account{ID: 94, Platform: PlatformZhipu, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"oauth_provider": "bigmodel", "plan_kind": ZhipuPlanIndividualCodingPlan, "access_token": "owner-login", "api_key": "public-id", "zhipu_project_key_id": "public-id", "zhipu_organization": "org", "zhipu_project": "project"}}
}

func TestProjectTokenConcurrentSendAndRejectedCredential(t *testing.T) {
	a := projectTokenAccount()
	issuer := &projectIssuerStub{issue: func(ctx context.Context, login string) (*zcode.ProjectAccessToken, error) {
		identity, ok := outboundidentity.FromContext(ctx)
		require.True(t, ok)
		require.Equal(t, "zcode", identity.Preset)
		require.Equal(t, "owner-login", login)
		return &zcode.ProjectAccessToken{Token: "ephemeral", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	m := NewZhipuProjectTokenManager(issuer, nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := m.Token(context.Background(), a)
			require.NoError(t, err)
			require.Equal(t, "ephemeral", token)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, issuer.calls.Load())
	req, _ := http.NewRequest(http.MethodPost, "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer stale-secret")
	require.NoError(t, m.Prepare(req, a))
	require.Equal(t, "Bearer ephemeral", req.Header.Get("Authorization"))
	m.Observe(req, a, &http.Response{StatusCode: 401})
	issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return nil, &zcode.ProjectTokenHTTPError{Status: 503}
	}
	_, err := m.Token(context.Background(), a)
	require.Error(t, err, "explicitly rejected PAT cannot survive refresh failure")
	require.NotContains(t, err.Error(), "owner-login")
}

func TestProjectTokenCacheSeparatesIdenticalGrantsAcrossAccounts(t *testing.T) {
	first := projectTokenAccount()
	second := snapshotRequestCredentialAccount(first)
	second.ID = 95
	issuer := &projectIssuerStub{}
	issuer.issue = func(ctx context.Context, login string) (*zcode.ProjectAccessToken, error) {
		require.Equal(t, "owner-login", login)
		identity, ok := outboundidentity.FromContext(ctx)
		require.True(t, ok)
		return &zcode.ProjectAccessToken{Token: fmt.Sprintf("account-%d-pat-%d", identity.AccountID, issuer.calls.Load()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	manager := NewZhipuProjectTokenManager(issuer, nil)
	firstRequest, err := http.NewRequest(http.MethodPost, "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions", nil)
	require.NoError(t, err)
	require.NoError(t, manager.Prepare(firstRequest, first))
	require.Equal(t, "Bearer account-94-pat-1", firstRequest.Header.Get("Authorization"))
	secondToken, err := manager.Token(context.Background(), second)
	require.NoError(t, err)
	require.Equal(t, "account-95-pat-2", secondToken, "different persisted owners cannot share a PAT even when their grants and identity declarations match")
	require.EqualValues(t, 2, issuer.calls.Load())
	manager.Observe(firstRequest, first, &http.Response{StatusCode: http.StatusUnauthorized})
	secondToken, err = manager.Token(context.Background(), second)
	require.NoError(t, err)
	require.Equal(t, "account-95-pat-2", secondToken, "another account's rejection cannot evict this owner's PAT")
	require.EqualValues(t, 2, issuer.calls.Load())
	firstToken, err := manager.Token(context.Background(), first)
	require.NoError(t, err)
	require.Equal(t, "account-94-pat-3", firstToken)
	require.EqualValues(t, 3, issuer.calls.Load())
}

func TestProjectTokenRefreshBoundaryAndTransientRetention(t *testing.T) {
	now := time.Now()
	a := projectTokenAccount()
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: "valid-old", ExpiresAt: now.Add(4 * time.Minute)}, nil
	}}
	m := NewZhipuProjectTokenManager(issuer, nil)
	m.now = func() time.Time { return now }
	m.jitter = func() time.Duration { return 0 }
	token, err := m.Token(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "valid-old", token)
	now = now.Add(121 * time.Second)
	issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return nil, &zcode.ProjectTokenHTTPError{Status: 429}
	}
	token, err = m.Token(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "valid-old", token)
	require.EqualValues(t, 2, issuer.calls.Load())
	_, err = m.Token(context.Background(), a)
	require.NoError(t, err)
	require.EqualValues(t, 2, issuer.calls.Load(), "transient refresh backoff")
	now = now.Add(2 * time.Minute)
	_, err = m.Token(context.Background(), a)
	require.Error(t, err, "expired credentials must never be reused")
}

func TestProjectTokenOwnerChangeCancellationAndManualKeyIsolation(t *testing.T) {
	a := projectTokenAccount()
	changed := *a
	changed.Credentials = map[string]any{"access_token": "new-owner"}
	repo := &refreshAPIAccountRepo{account: a}
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		repo.account = &changed
		return &zcode.ProjectAccessToken{Token: "late", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	m := NewZhipuProjectTokenManager(issuer, repo)
	_, err := m.Token(context.Background(), a)
	require.Error(t, err, "late results cannot cross a login generation")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = m.Token(ctx, a)
	require.ErrorIs(t, err, context.Canceled)
	manual := *a
	manual.Type = AccountTypeAPIKey
	req, _ := http.NewRequest(http.MethodPost, "https://custom.example/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer manual")
	require.NoError(t, m.Prepare(req, &manual))
	require.Equal(t, "Bearer manual", req.Header.Get("Authorization"))
	req.URL.Host = "attacker.example"
	require.Error(t, m.Prepare(req, a))
	require.EqualValues(t, 1, issuer.calls.Load())
	issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) { return nil, errors.New("malformed") }
}

func TestProjectTokenFreezesMutableCredentialOwner(t *testing.T) {
	a := projectTokenAccount()
	repo := &refreshAPIAccountRepo{account: a}
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		a.Credentials["access_token"] = "new-login"
		return &zcode.ProjectAccessToken{Token: "late-old-owner", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	manager := NewZhipuProjectTokenManager(issuer, repo)
	_, err := manager.Token(context.Background(), a)
	require.Error(t, err, "in-place relink must invalidate the captured old credential owner")
}

func TestProjectTokenOnlyAuthenticatesSupportedPhysicalEndpoints(t *testing.T) {
	a := projectTokenAccount()
	issuer := &projectIssuerStub{issue: func(context.Context, string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: "pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
	}}
	manager := NewZhipuProjectTokenManager(issuer, nil)
	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "https://open.bigmodel.cn/api/coding/paas/v4/../outside"},
		{http.MethodDelete, "https://open.bigmodel.cn/api/coding/paas/v4/models"},
		{http.MethodPost, "https://open.bigmodel.cn/api/coding/paas/v4/future-operation"},
	} {
		req, _ := http.NewRequest(tc.method, tc.target, nil)
		require.Error(t, manager.Prepare(req, a))
		require.Empty(t, req.Header.Get("Authorization"))
	}
	require.Zero(t, issuer.calls.Load(), "forbidden operation must fail before credential issuance")
}

func TestProjectTokenRotationRetainsOffPeakTicketAndOriginalSettlementOwner(t *testing.T) {
	a := projectTokenAccount()
	a.Credentials["plan_kind"] = ZhipuPlanOffPeak
	a.Credentials["zhipu_project_kind"] = "personal"
	a.Credentials["zcode_jwt_token"] = "old-jwt"
	now := time.Now()
	repo := &refreshAPIAccountRepo{account: a}
	issuer := &projectIssuerStub{issue: func(_ context.Context, login string) (*zcode.ProjectAccessToken, error) {
		return &zcode.ProjectAccessToken{Token: login + "-pat-" + now.Format("150405"), ExpiresAt: now.Add(4 * time.Minute)}, nil
	}}
	tokens := NewZhipuProjectTokenManager(issuer, repo)
	tokens.now = func() time.Time { return now }
	tokens.jitter = func() time.Duration { return 0 }
	tickets := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "retained", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, tickets)
	manager.projectTokens = tokens
	manager.now = func() time.Time { return now }
	ticket, err := manager.TicketForRequest(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "retained", ticket.TicketID)
	ticket.Release()
	initialPAT := tickets.lastAuth.PlanKey
	now = now.Add(121 * time.Second)
	ticket, err = manager.TicketForRequest(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "retained", ticket.TicketID)
	ticket.Release()
	require.Equal(t, 1, tickets.takeCalls, "PAT rotation is not a new ticket owner")
	require.Empty(t, tickets.settled)
	// Logout/relink must never make settlement use the new login's credential.
	a.Credentials["access_token"] = "replacement-login"
	a.Credentials["zcode_jwt_token"] = "replacement-jwt"
	now = now.Add(DefaultOffPeakSettleIdle + time.Second)
	manager.sweepOnce()
	require.Equal(t, []string{"retained"}, tickets.settled)
	require.Equal(t, "old-jwt", tickets.settleAuths[0].JWT)
	require.Contains(t, tickets.settleAuths[0].PlanKey, "owner-login-pat-")
	require.NotEqual(t, initialPAT, tickets.settleAuths[0].PlanKey, "settlement refreshes expired PAT in the frozen original scope")
}
