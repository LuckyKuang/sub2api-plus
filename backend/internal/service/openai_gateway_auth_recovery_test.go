//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newAuthRecoveryTestProvider(t *testing.T, account *Account, executor *refreshAPIExecutorStub) (*OpenAITokenProvider, *refreshAPICacheStub) {
	t.Helper()
	repo := &refreshAPIAccountRepo{account: account}
	cache := &refreshAPICacheStub{lockResult: true}
	provider := NewOpenAITokenProvider(repo, cache, nil)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
	return provider, cache
}

func TestForceRefreshOpenAIToken_DeletesCacheAndRefreshes(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour)
	account := &Account{
		ID: 5, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "old-token", "refresh_token": "refresh-1"},
	}
	account.Credentials["expires_at"] = expiresAt.Format(time.RFC3339)
	executor := &refreshAPIExecutorStub{
		needsRefresh: true,
		credentials:  map[string]any{"access_token": "new-token", "refresh_token": "refresh-2"},
	}
	provider, cache := newAuthRecoveryTestProvider(t, account, executor)

	token, err := provider.ForceRefreshToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "new-token", token)
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, 1, cache.deleteCalls, "the rejected cached token must be dropped before refreshing")
	require.Equal(t, OpenAITokenCacheKey(account), cache.deleteKey)
}

// When the shared refresh path decides no refresh is necessary (an OAuth
// account without expires_at skips the window comparison entirely), the
// recovery must fail instead of replaying the credential the upstream just
// rejected. The caller then falls through to normal failover.
func TestForceRefreshOpenAIToken_FailsWhenNothingWasRefreshed(t *testing.T) {
	account := &Account{
		ID: 16, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "stale-token", "refresh_token": "refresh-1"},
	}
	// needsRefresh=false mirrors an account whose expires_at is nil: the shared
	// refresh path returns the unchanged account with Refreshed=false.
	executor := &refreshAPIExecutorStub{needsRefresh: false, credentials: map[string]any{"access_token": "stale-token"}}
	provider, _ := newAuthRecoveryTestProvider(t, account, executor)

	token, err := provider.ForceRefreshToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Contains(t, err.Error(), "was not refreshed")
	require.Equal(t, 0, executor.refreshCalls, "an un-refreshed account must not reach the refresh executor")
}

func TestForceRefreshOpenAIToken_RejectsUnrefreshableAccounts(t *testing.T) {
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new"}}
	provider, _ := newAuthRecoveryTestProvider(t, nil, executor)

	pat := &Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "pat", "auth_mode": OpenAIAuthModePersonalAccessToken}}
	if _, err := provider.ForceRefreshToken(context.Background(), pat); err == nil {
		t.Fatal("personal access tokens must be rejected")
	}

	noRefresh := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "at"}}
	if _, err := provider.ForceRefreshToken(context.Background(), noRefresh); err == nil {
		t.Fatal("accounts without a refresh token must be rejected")
	}

	apiKey := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive,
		Credentials: map[string]any{"api_key": "sk"}}
	if _, err := provider.ForceRefreshToken(context.Background(), apiKey); err == nil {
		t.Fatal("api-key accounts must be rejected")
	}
	require.Zero(t, executor.refreshCalls, "rejected accounts must not reach the refresh executor")
}

func TestCanForceRefreshOpenAIAuthOnUnauthorized(t *testing.T) {
	svc := &OpenAIGatewayService{}
	eligible := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"refresh_token": "r"}}
	require.True(t, svc.canForceRefreshOpenAIAuthOnUnauthorized(eligible))

	for _, account := range []*Account{
		nil,
		{ID: 10, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"refresh_token": "r"}},
		{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeSetupToken},
		{ID: 13, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModePersonalAccessToken}},
		{ID: 14, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity}},
	} {
		require.False(t, svc.canForceRefreshOpenAIAuthOnUnauthorized(account), "account %+v must not be eligible", account)
	}

	shadowParent := int64(9)
	shadow := &Account{ID: 15, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &shadowParent}
	require.True(t, svc.canForceRefreshOpenAIAuthOnUnauthorized(shadow), "a credential shadow recovers through its owner")
}

func newOpenAIAuthRecoveryTestAccount() *Account {
	return &Account{
		ID: 21, Name: "openai-oauth-auth-recovery", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "old-token",
			"refresh_token":      "refresh-1",
			"chatgpt_account_id": "chatgpt-acc",
			"expires_at":         time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
}

func TestOpenAIGatewayForward_401RecoversWithSameAccountRefresh(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"instructions":"test","input":"hello"}`)
	c := newOpenAIRejectedFieldTestContext(body)

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"rid_401"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_token","message":"token rejected"}}`))},
		openAIIdentityContractSuccessResponse(),
	}}
	account := newOpenAIAuthRecoveryTestAccount()
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new-token", "refresh_token": "refresh-2"}}
	provider, _ := newAuthRecoveryTestProvider(t, account, executor)
	svc := &OpenAIGatewayService{
		cfg:                 &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream:        upstream,
		openAITokenProvider: provider,
		accountRepo:         &refreshAPIAccountRepo{account: account},
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2, "the 401 must retry the same account once after the credential refresh")
	require.Equal(t, 1, executor.refreshCalls)

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "retry", events[0].Kind)
	require.Equal(t, openAIAuthRecoveryRetryReason, events[0].Reason)
	require.Equal(t, http.StatusUnauthorized, events[0].UpstreamStatusCode)
}

func TestOpenAIGatewayForward_401RefreshFailureFallsThroughToFailover(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"instructions":"test","input":"hello"}`)
	c := newOpenAIRejectedFieldTestContext(body)

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_token","message":"token rejected"}}`))},
	}}
	account := newOpenAIAuthRecoveryTestAccount()
	executor := &refreshAPIExecutorStub{needsRefresh: true, err: errors.New("refresh permanently failed")}
	provider, _ := newAuthRecoveryTestProvider(t, account, executor)
	svc := &OpenAIGatewayService{
		cfg:                 &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream:        upstream,
		openAITokenProvider: provider,
		accountRepo:         &refreshAPIAccountRepo{account: account},
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Len(t, upstream.bodies, 1, "a failed refresh must not retry the same account")
	require.Equal(t, 1, executor.refreshCalls)
}

// Batch 6: the HTTP passthrough path needs the same same-account 401 recovery as
// the non-passthrough forward path, and it must send the refreshed token.
func TestOpenAIGatewayPassthrough_401RecoversWithSameAccountRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6-sol","stream":false,"instructions":"test","input":"hello"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: http.StatusUnauthorized, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_token","message":"token rejected"}}`))},
		openAIIdentityContractSuccessResponse(),
	}}
	account := newOpenAIAuthRecoveryTestAccount()
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "new-token", "refresh_token": "refresh-2"}}
	provider, _ := newAuthRecoveryTestProvider(t, account, executor)
	svc := &OpenAIGatewayService{
		cfg:                 &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream:        upstream,
		openAITokenProvider: provider,
		accountRepo:         &refreshAPIAccountRepo{account: account},
	}

	result, err := svc.forwardOpenAIPassthrough(context.Background(), c, account, body, body, "gpt-5.6-sol", false, nil, false, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 2, "the 401 must retry the same account once after the credential refresh")
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, "Bearer new-token", upstream.requests[1].Header.Get("Authorization"),
		"the retry must use the refreshed credential, not the rejected one")
}
