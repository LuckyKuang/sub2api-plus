//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type grokQuotaHandlerAccountRepo struct {
	service.AccountRepository
	mu                   sync.Mutex
	account              *service.Account
	updates              map[int64]map[string]any
	unconditionalUpdates int
	observedModelUpdates int
}

var _ service.CNControlSnapshotRepository = (*grokQuotaHandlerAccountRepo)(nil)

func (r *grokQuotaHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account != nil && r.account.ID == id {
		encoded, err := json.Marshal(r.account)
		if err != nil {
			return nil, err
		}
		var snapshot service.Account
		if err := json.Unmarshal(encoded, &snapshot); err != nil {
			return nil, err
		}
		return &snapshot, nil
	}
	return nil, service.ErrAccountNotFound
}

func (r *grokQuotaHandlerAccountRepo) CompareAndSwapGrokObservedModels(_ context.Context, owner *service.Account, expected, next json.RawMessage) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner == nil || r.account == nil || owner.ID != r.account.ID || owner.Platform != r.account.Platform || owner.Type != r.account.Type || owner.Status != r.account.Status || !reflect.DeepEqual(owner.Credentials, r.account.Credentials) || !reflect.DeepEqual(owner.ProxyID, r.account.ProxyID) || !reflect.DeepEqual(owner.Proxy, r.account.Proxy) {
		return false, nil
	}
	var current any
	if r.account.Extra != nil {
		current = r.account.Extra["grok_observed_models"]
	}
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return false, err
	}
	if string(currentJSON) != string(expected) {
		return false, nil
	}
	var value any
	if err := json.Unmarshal(next, &value); err != nil {
		return false, err
	}
	if r.account.Extra == nil {
		r.account.Extra = make(map[string]any)
	}
	r.account.Extra["grok_observed_models"] = value
	r.recordExtraUpdateLocked(owner.ID, map[string]any{"grok_observed_models": value})
	return true, nil
}

func (r *grokQuotaHandlerAccountRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unconditionalUpdates++
	r.recordExtraUpdateLocked(id, updates)
	return nil
}

func (r *grokQuotaHandlerAccountRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, expected *service.Account, updates map[string]any) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.account
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) {
		return false, nil
	}
	currentCredentials, err := json.Marshal(current.Credentials)
	if err != nil {
		return false, err
	}
	expectedCredentials, err := json.Marshal(expected.Credentials)
	if err != nil {
		return false, err
	}
	if string(currentCredentials) != string(expectedCredentials) {
		return false, nil
	}
	currentRoute, currentRouteErr := current.StrictProxyURL()
	expectedRoute, expectedRouteErr := expected.StrictProxyURL()
	if currentRouteErr != nil || expectedRouteErr != nil || currentRoute != expectedRoute {
		return false, nil
	}
	if current.ProxyID != nil {
		currentProxy, expectedProxy := current.Proxy, expected.Proxy
		if currentProxy == nil || expectedProxy == nil || currentProxy.ID != expectedProxy.ID || currentProxy.Protocol != expectedProxy.Protocol || currentProxy.Host != expectedProxy.Host || currentProxy.Port != expectedProxy.Port || currentProxy.Username != expectedProxy.Username || currentProxy.Password != expectedProxy.Password || currentProxy.Status != expectedProxy.Status || !reflect.DeepEqual(currentProxy.ExpiresAt, expectedProxy.ExpiresAt) {
			return false, nil
		}
	}
	r.recordExtraUpdateLocked(expected.ID, updates)
	return true, nil
}

func (r *grokQuotaHandlerAccountRepo) recordExtraUpdateLocked(id int64, updates map[string]any) {
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	if r.updates[id] == nil {
		r.updates[id] = make(map[string]any)
	}
	// PostgreSQL merges Extra keys, so billing and detached model discovery
	// must both remain recorded regardless of which update finishes first.
	for key, value := range updates {
		r.updates[id][key] = value
	}
	if updates["grok_observed_models"] != nil {
		r.observedModelUpdates++
	}
}

func (r *grokQuotaHandlerAccountRepo) ApplyCredentialStateMutation(_ context.Context, owner *service.Account, mutation service.CredentialStateMutation) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner == nil || r.account == nil || owner.ID != r.account.ID || owner.Platform != r.account.Platform || owner.Type != r.account.Type || owner.Status != r.account.Status || !reflect.DeepEqual(owner.ProxyID, r.account.ProxyID) || !reflect.DeepEqual(owner.Proxy, r.account.Proxy) {
		return false, nil
	}
	// Account snapshots cross the same JSON boundary as stored credentials.
	currentCredentials, err := json.Marshal(r.account.Credentials)
	if err != nil {
		return false, err
	}
	ownerCredentials, err := json.Marshal(owner.Credentials)
	if err != nil {
		return false, err
	}
	if string(currentCredentials) != string(ownerCredentials) {
		return false, nil
	}
	if r.updates == nil {
		r.updates = make(map[int64]map[string]any)
	}
	if r.updates[owner.ID] == nil {
		r.updates[owner.ID] = make(map[string]any)
	}
	for key, value := range mutation.Extra {
		r.updates[owner.ID][key] = value
	}
	if mutation.Extra["grok_observed_models"] != nil {
		r.observedModelUpdates++
	}
	return true, nil
}

type grokQuotaHandlerUpstream struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
}

type grokOAuthReconcilerStub struct {
	input  service.GrokOAuthReconcileInput
	calls  int
	result *service.GrokOAuthReconcileResult
	err    error
}

func (s *grokOAuthReconcilerStub) ReconcileGrokOAuth(_ context.Context, input service.GrokOAuthReconcileInput) (*service.GrokOAuthReconcileResult, error) {
	s.calls++
	s.input = input
	return s.result, s.err
}

func (u *grokQuotaHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	u.mu.Lock()
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, body)
	u.mu.Unlock()
	if req.URL.Path == "/v1/responses" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"X-Ratelimit-Limit-Requests":     []string{"10"},
				"X-Ratelimit-Remaining-Requests": []string{"8"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"resp_probe"}`)),
		}, nil
	}
	if req.URL.Path == "/v1/models" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"grok-4.5"}]}`)),
		}, nil
	}
	payload := `{"config":{"billingPeriodStart":"2026-07-01T00:00:00Z","billingPeriodEnd":"2026-08-01T00:00:00Z"}}`
	if req.URL.RawQuery == "format=credits" {
		payload = `{"config":{"currentPeriod":{"type":"WEEKLY","start":"2026-07-09T03:25:00Z","end":"2026-07-16T03:25:00Z"}}}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
}

func (u *grokQuotaHandlerUpstream) DoWithTLS(
	req *http.Request,
	proxyURL string,
	accountID int64,
	accountConcurrency int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestGrokOAuthHandlerQueryQuotaProbesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &grokQuotaHandlerAccountRepo{account: &service.Account{
		ID:          42,
		Platform:    service.PlatformGrok,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_at":    time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
		},
	}}
	upstream := &grokQuotaHandlerUpstream{}
	quotaService := service.NewGrokQuotaService(repo, nil, service.NewGrokTokenProvider(repo, nil), upstream, nil)
	handler := NewGrokOAuthHandler(nil, nil, quotaService, nil)

	router := gin.New()
	router.GET("/api/v1/admin/grok/accounts/:id/quota", handler.QueryQuota)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/grok/accounts/42/quota", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"source":"hybrid_probe"`)
	require.Contains(t, rec.Body.String(), `"billing":`)
	require.Contains(t, rec.Body.String(), `"snapshot":`)
	require.Contains(t, rec.Body.String(), `"headers_observed":true`)
	require.NotContains(t, rec.Body.String(), "access-token")
	require.Eventually(t, func() bool {
		upstream.mu.Lock()
		defer upstream.mu.Unlock()
		return len(upstream.requests) == 4
	}, time.Second, 10*time.Millisecond)
	upstream.mu.Lock()
	requests := append([]*http.Request(nil), upstream.requests...)
	bodies := append([][]byte(nil), upstream.bodies...)
	upstream.mu.Unlock()
	require.Len(t, requests, 4)
	responsesProbeSeen := false
	modelsSyncSeen := false
	for i, upstreamReq := range requests {
		require.Equal(t, "Bearer access-token", upstreamReq.Header.Get("Authorization"))
		if upstreamReq.URL.String() == xai.DefaultCLIBaseURL+"/responses" {
			responsesProbeSeen = true
			require.Equal(t, "text/event-stream", upstreamReq.Header.Get("Accept"))
			require.Contains(t, string(bodies[i]), `"model":"grok-4.5"`)
			require.Contains(t, string(bodies[i]), `"input":"hi"`)
			require.Contains(t, string(bodies[i]), `"stream":true`)
			require.NotContains(t, string(bodies[i]), `"max_output_tokens"`)
			require.NotContains(t, string(bodies[i]), `"store"`)
		}
		if upstreamReq.URL.String() == xai.DefaultCLIBaseURL+"/models" {
			modelsSyncSeen = true
		}
	}
	require.True(t, responsesProbeSeen)
	require.True(t, modelsSyncSeen)
	// Recording the /models request precedes its asynchronous persistence. Wait
	// for that side effect and synchronize every access to the repository fake.
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.updates[42]["grok_observed_models"] != nil
	}, time.Second, 10*time.Millisecond)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.observedModelUpdates)
	require.Zero(t, repo.unconditionalUpdates, "quota observations must retain the captured credential owner")
}

func TestGrokOAuthHandlerResetQuotaReturnsUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &grokQuotaHandlerAccountRepo{account: &service.Account{
		ID:       43,
		Platform: service.PlatformGrok,
		Type:     service.AccountTypeOAuth,
	}}
	quotaService := service.NewGrokQuotaService(repo, nil, nil, nil, nil)
	handler := NewGrokOAuthHandler(nil, nil, quotaService, nil)

	router := gin.New()
	router.POST("/api/v1/admin/grok/accounts/:id/reset-quota", handler.ResetQuota)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/accounts/43/reset-quota", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotImplemented, rec.Code)
	require.Contains(t, rec.Body.String(), `"reason":"GROK_QUOTA_RESET_UNSUPPORTED"`)
	require.NotContains(t, rec.Body.String(), "access-token")
}

func TestGrokOAuthHandlerRuntimeSanityDoesNotExposeSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(xai.EnvBaseURL, "http://127.0.0.1:8080/v1?access_token=secret")
	t.Setenv(xai.EnvClientID, "client-secret-like-value")

	handler := NewGrokOAuthHandler(nil, nil, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/grok/runtime-sanity", handler.RuntimeSanity)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/grok/runtime-sanity", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"public_gateway_scope":"responses_only"`)
	require.Contains(t, rec.Body.String(), `"valid":false`)
	require.NotContains(t, rec.Body.String(), "access_token")
	require.NotContains(t, rec.Body.String(), "secret")
	require.NotContains(t, rec.Body.String(), "client-secret-like-value")
}

type grokOAuthHandlerClient struct{}

func (c *grokOAuthHandlerClient) ValidateAuthorizationTokens(context.Context, *xai.TokenResponse, xai.OIDCLoginContext, string) (*xai.OIDCIdentity, error) {
	return nil, errors.New("unexpected authorization validation")
}

func (c *grokOAuthHandlerClient) ExchangeCode(context.Context, string, string, string, string, string) (*xai.TokenResponse, error) {
	return nil, errors.New("unexpected exchange")
}

func (c *grokOAuthHandlerClient) RefreshToken(context.Context, string, string, string) (*xai.TokenResponse, error) {
	return &xai.TokenResponse{AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresIn: 3600}, nil
}

func (c *grokOAuthHandlerClient) ConvertSSOToBuild(context.Context, string, string) (*xai.TokenResponse, error) {
	return &xai.TokenResponse{AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresIn: 3600}, nil
}

func TestGrokOAuthHandlerValidateSSOTokenReturnsTokenInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oauthClient := &grokOAuthHandlerClient{}
	oauthService := service.NewGrokOAuthService(nil, oauthClient)
	defer oauthService.Stop()
	handler := NewGrokOAuthHandler(oauthService, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/sso-token", handler.ValidateSSOToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/oauth/sso-token", strings.NewReader(`{"sso_token":"sso-token"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"access_token":"access-token"`)
	require.NotContains(t, rec.Body.String(), `"sso_token"`)
}

func TestGrokOAuthHandlerAuthorizePasswordRemainsDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oauthClient := &grokOAuthHandlerClient{}
	oauthService := service.NewGrokOAuthService(nil, oauthClient)
	defer oauthService.Stop()
	handler := NewGrokOAuthHandler(oauthService, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/password", handler.AuthorizePassword)
	for _, body := range []string{
		`{"email":"user@example.com","password":"super-secret"}`,
		`{`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/oauth/password", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Contains(t, rec.Body.String(), "GROK_OAUTH_PASSWORD_AUTH_DISABLED")
		require.NotContains(t, rec.Body.String(), "super-secret")
	}
}

func TestGrokOAuthHandlerPasswordCapabilityDefaultsToDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oauthService := service.NewGrokOAuthService(nil, &grokOAuthHandlerClient{})
	defer oauthService.Stop()
	handler := NewGrokOAuthHandler(oauthService, nil, nil, nil)

	router := gin.New()
	router.GET("/api/v1/admin/grok/oauth/capabilities", handler.GetCapabilities)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/grok/oauth/capabilities", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"password_auth_enabled":false`)
}

func TestGrokSSOImportExpiryUsesTokenExpiryWithoutRefreshToken(t *testing.T) {
	tokenExpiry := time.Now().Add(6 * time.Hour).Unix()
	expiresAt, autoPause := grokSSOImportExpiry(nil, nil, &service.GrokTokenInfo{
		ExpiresAt: tokenExpiry,
	})

	require.NotNil(t, expiresAt)
	require.Equal(t, tokenExpiry, *expiresAt)
	require.NotNil(t, autoPause)
	require.True(t, *autoPause)
}

func TestGrokSSOImportExpiryUsesEarlierRequestedExpiryWithoutRefreshToken(t *testing.T) {
	requestedExpiry := time.Now().Add(2 * time.Hour).Unix()
	tokenExpiry := time.Now().Add(6 * time.Hour).Unix()
	requestedAutoPause := false
	expiresAt, autoPause := grokSSOImportExpiry(&requestedExpiry, &requestedAutoPause, &service.GrokTokenInfo{
		ExpiresAt: tokenExpiry,
	})

	require.NotNil(t, expiresAt)
	require.Equal(t, requestedExpiry, *expiresAt)
	require.NotNil(t, autoPause)
	require.True(t, *autoPause)
}

func TestGrokSSOImportExpiryPreservesRequestSettingsWithRefreshToken(t *testing.T) {
	requestedExpiry := time.Now().Add(2 * time.Hour).Unix()
	requestedAutoPause := false
	expiresAt, autoPause := grokSSOImportExpiry(&requestedExpiry, &requestedAutoPause, &service.GrokTokenInfo{
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
	})

	require.Same(t, &requestedExpiry, expiresAt)
	require.Same(t, &requestedAutoPause, autoPause)
}

func TestGrokSSOImportCredentialsPreservesRequestedBaseURL(t *testing.T) {
	built := map[string]any{
		"access_token": "at-1",
		"base_url":     xai.DefaultCLIBaseURL,
	}
	reqCredentials := map[string]any{
		"base_url":                "https://relay.example.com/v1",
		"header_override_enabled": true,
		"header_overrides":        map[string]any{"x-relay-key": "k"},
	}

	credentials := grokSSOImportCredentials(built, reqCredentials)

	// token 字段以兑换结果为准；base_url 是运营侧配置，必须保留请求里的自定义地址
	require.Equal(t, "at-1", credentials["access_token"])
	require.Equal(t, "https://relay.example.com/v1", credentials["base_url"])
	require.Equal(t, true, credentials["header_override_enabled"])
	require.Equal(t, map[string]any{"x-relay-key": "k"}, credentials["header_overrides"])
	// 入参不被污染（req.Credentials 会被多个 worker 并发读取）
	require.Equal(t, "https://relay.example.com/v1", reqCredentials["base_url"])
}

func TestGrokSSOImportCredentialsDefaultsToOfficialBaseURL(t *testing.T) {
	built := map[string]any{
		"access_token": "at-1",
		"base_url":     xai.DefaultCLIBaseURL,
	}

	credentials := grokSSOImportCredentials(built, nil)
	require.Equal(t, xai.DefaultCLIBaseURL, credentials["base_url"])

	credentials = grokSSOImportCredentials(map[string]any{
		"access_token": "at-2",
		"base_url":     xai.DefaultCLIBaseURL,
	}, map[string]any{"base_url": "   "})
	require.Equal(t, xai.DefaultCLIBaseURL, credentials["base_url"])
	require.Equal(t, "at-2", credentials["access_token"])
}

func TestGrokSSOImportWorkerHandlesMissingOAuthService(t *testing.T) {
	h := &GrokOAuthHandler{}
	result := h.safeCreateAccountFromSSOToken(context.Background(), GrokSSOToOAuthRequest{}, "token", 2, 3)
	require.False(t, result.created)
	require.Equal(t, 2, result.item.Index)
	require.Contains(t, result.item.Error, "GROK_OAUTH_CLIENT_NOT_CONFIGURED")
}

func TestGrokOAuthHandlerReconcileDefaultsToDryRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reconciler := &grokOAuthReconcilerStub{result: &service.GrokOAuthReconcileResult{
		DryRun:      true,
		Scanned:     2,
		Actionable:  1,
		WouldBlock:  1,
		Items:       []service.GrokOAuthReconcileItem{{AccountID: 42, Reason: service.GrokOAuthReconcileReasonMissingRefreshToken, Action: service.GrokOAuthReconcileActionBlock, Outcome: service.GrokOAuthReconcileOutcomePlanned}},
		NextAfterID: 0,
	}}
	handler := NewGrokOAuthHandler(nil, nil, nil, reconciler)
	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/reconcile", handler.ReconcileOAuthAccounts)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/oauth/reconcile", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, reconciler.calls)
	require.True(t, reconciler.input.DryRun)
	require.False(t, reconciler.input.Apply)
	require.Contains(t, rec.Body.String(), `"reason":"missing_refresh_token"`)
	require.NotContains(t, rec.Body.String(), `"refresh_token":`)
	require.NotContains(t, rec.Body.String(), `"access_token":`)
}

func TestGrokOAuthHandlerReconcileRequiresExplicitApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reconciler := &grokOAuthReconcilerStub{}
	handler := NewGrokOAuthHandler(nil, nil, nil, reconciler)
	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/reconcile", handler.ReconcileOAuthAccounts)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/oauth/reconcile", strings.NewReader(`{"dry_run":false}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Zero(t, reconciler.calls)
	require.NotContains(t, rec.Body.String(), "credentials")
}

func TestGrokOAuthHandlerReconcileExplicitApply(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reconciler := &grokOAuthReconcilerStub{result: &service.GrokOAuthReconcileResult{DryRun: false, Refreshed: 1}}
	handler := NewGrokOAuthHandler(nil, nil, nil, reconciler)
	router := gin.New()
	router.POST("/api/v1/admin/grok/oauth/reconcile", handler.ReconcileOAuthAccounts)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/grok/oauth/reconcile", strings.NewReader(`{"apply":true,"dry_run":false,"after_id":10,"limit":25,"refresh_window_seconds":3600}`))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, reconciler.calls)
	require.True(t, reconciler.input.Apply)
	require.False(t, reconciler.input.DryRun)
	require.Equal(t, int64(10), reconciler.input.AfterID)
	require.Equal(t, 25, reconciler.input.Limit)
	require.Equal(t, time.Hour, reconciler.input.RefreshWindow)
}
