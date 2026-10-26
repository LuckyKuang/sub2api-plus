//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	adminhandler "github.com/LuckyKuang/sub2api-plus/internal/handler/admin"
	"github.com/LuckyKuang/sub2api-plus/internal/handler/dto"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminOAuthCredentialWriter interface {
	UpdateOAuthCredentialsIfUnchanged(context.Context, *service.Account, string, map[string]any, map[string]any, bool) (bool, error)
}

// Both the old Update boundary and the conditional replacement use this same
// observer. The real PostgreSQL repository performs every actual mutation.
type adminOAuthHeldRepository struct {
	service.AdminAccountRepository
	before      func()
	once        sync.Once
	observed    atomic.Int32
	finalWrites atomic.Int32
}

func (r *adminOAuthHeldRepository) beforeWrite() {
	r.once.Do(func() {
		r.observed.Add(1)
		if r.before != nil {
			r.before()
		}
	})
}

func (r *adminOAuthHeldRepository) Update(ctx context.Context, account *service.Account) error {
	r.finalWrites.Add(1)
	r.beforeWrite()
	return r.AdminAccountRepository.Update(ctx, account)
}

func (r *adminOAuthHeldRepository) RunOpenAIOAuthPolicyTransaction(ctx context.Context, fn func(context.Context, service.AccountRepository) error) error {
	writer, ok := r.AdminAccountRepository.(service.OpenAIOAuthPolicyTransactionRepository)
	if !ok {
		return errors.New("account transaction writer is unavailable")
	}
	return writer.RunOpenAIOAuthPolicyTransaction(ctx, func(writeCtx context.Context, repo service.AccountRepository) error {
		adminRepo, ok := repo.(service.AdminAccountRepository)
		if !ok {
			return errors.New("transaction account repository is incomplete")
		}
		return fn(writeCtx, &adminOAuthHeldRepository{AdminAccountRepository: adminRepo, before: r.beforeWrite})
	})
}

func (r *adminOAuthHeldRepository) ClearAccountRecoveryState(ctx context.Context, accountID int64) error {
	writer, ok := r.AdminAccountRepository.(service.AccountRecoveryRepository)
	if !ok {
		return errors.New("atomic recovery writer is unavailable")
	}
	return writer.ClearAccountRecoveryState(ctx, accountID)
}

func (r *adminOAuthHeldRepository) UpdateOAuthCredentialsIfUnchanged(ctx context.Context, expected *service.Account, accountType string, credentials, extra map[string]any, recover bool) (bool, error) {
	r.finalWrites.Add(1)
	r.beforeWrite()
	writer, ok := r.AdminAccountRepository.(adminOAuthCredentialWriter)
	if !ok {
		return false, errors.New("atomic admin OAuth credential writer is unavailable")
	}
	return writer.UpdateOAuthCredentialsIfUnchanged(ctx, expected, accountType, credentials, extra, recover)
}

type adminOAuthTokenInvalidator struct{ accounts []*service.Account }

func (r *adminOAuthTokenInvalidator) InvalidateToken(_ context.Context, account *service.Account) error {
	r.accounts = append(r.accounts, account)
	return nil
}

type adminOAuthOpenAIIssuer struct {
	service.OpenAIOAuthClient
	t      *testing.T
	before func()
	proxy  string
	calls  atomic.Int32
}

func (s *adminOAuthOpenAIIssuer) RefreshTokenWithClientIDAndIdentity(_ context.Context, refresh, proxy, _ string, userAgent, originator, version string) (*openai.TokenResponse, error) {
	s.calls.Add(1)
	require.Equal(s.t, "original-refresh", refresh)
	require.Equal(s.t, s.proxy, proxy)
	require.NotEmpty(s.t, userAgent)
	require.NotEmpty(s.t, originator)
	require.NotEmpty(s.t, version)
	s.before()
	return &openai.TokenResponse{AccessToken: "issued-access", RefreshToken: "issued-refresh", ExpiresIn: 3600, TokenType: "Bearer"}, nil
}

type adminOAuthGrokIssuer struct {
	t      *testing.T
	before func()
	proxy  string
	calls  atomic.Int32
}

func (s *adminOAuthGrokIssuer) RefreshAccountToken(_ context.Context, account *service.Account) (*service.GrokTokenInfo, error) {
	s.calls.Add(1)
	require.Equal(s.t, "original-refresh", account.GetCredential("refresh_token"))
	proxy, err := account.StrictProxyURL()
	require.NoError(s.t, err)
	require.Equal(s.t, s.proxy, proxy)
	s.before()
	return &service.GrokTokenInfo{AccessToken: "issued-access", RefreshToken: "issued-refresh", ExpiresIn: 3600, ExpiresAt: time.Now().Add(time.Hour).Unix()}, nil
}

func (*adminOAuthGrokIssuer) BuildAccountCredentials(info *service.GrokTokenInfo) map[string]any {
	return map[string]any{"access_token": info.AccessToken, "refresh_token": info.RefreshToken, "expires_at": fmt.Sprint(info.ExpiresAt)}
}

func newAdminOAuthFixture(t *testing.T, platform, scenario string) (*accountRepository, *adminOAuthHeldRepository, *service.Account, func() *service.Account, *schedulerCacheRecorder, *adminRecoveryRuntimeRecorder) {
	t.Helper()
	ctx := context.Background()
	proxy := mustCreateProxy(t, integrationEntClient, &service.Proxy{Name: "admin-oauth-route", Protocol: "http", Host: "original.oauth.example", Port: 8080, Username: "original-user", Password: "original-password", Status: service.StatusActive})
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", proxy.ID) })
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("admin-oauth-%d", time.Now().UnixNano()), Platform: platform, Type: service.AccountTypeOAuth,
		Status: service.StatusError, ErrorMessage: "retained auth failure", ProxyID: &proxy.ID,
		Credentials: map[string]any{"access_token": "original-access", "refresh_token": "original-refresh", "custom_config": "retained"},
		Extra:       map[string]any{"retained": "base", "grok_needs_reauth": true, "model_rate_limits": map[string]any{"old": true}},
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
	})
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '1 hour',temp_unschedulable_reason='retained auth cooldown',rate_limited_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '2 hours' WHERE id=$1`, account.ID)
	require.NoError(t, err)
	base := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	before, err := base.GetByID(ctx, account.ID)
	require.NoError(t, err)
	cache := &schedulerCacheRecorder{accounts: map[int64]*service.Account{account.ID: before}}
	base.schedulerCache = cache
	expectedStored := before
	held := &adminOAuthHeldRepository{AdminAccountRepository: base}
	held.before = func() {
		var err error
		switch scenario {
		case "relinked":
			_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials || '{"access_token":"replacement-owner"}'::jsonb WHERE id=$1`, account.ID)
		case "proxy edited":
			_, err = integrationDB.ExecContext(ctx, "UPDATE proxies SET password='replacement-proxy-password' WHERE id=$1", proxy.ID)
		case "manual pause":
			_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET schedulable=FALSE WHERE id=$1", account.ID)
		case "newer cooldown":
			_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '4 hours',temp_unschedulable_reason='newer runtime failure' WHERE id=$1", account.ID)
		}
		require.NoError(t, err)
		expectedStored, err = base.GetByID(ctx, account.ID)
		require.NoError(t, err)
	}
	if scenario == "row failure" {
		installAdminLifecycleStorageFault(t, "accounts", fmt.Sprintf("NEW.id=%d AND NEW.credentials IS DISTINCT FROM OLD.credentials", account.ID))
	}
	if scenario == "outbox failure" {
		installAdminLifecycleStorageFault(t, "scheduler_outbox", fmt.Sprintf("NEW.account_id=%d", account.ID))
	}
	return base, held, before, func() *service.Account { return expectedStored }, cache, &adminRecoveryRuntimeRecorder{}
}

func newAdminOAuthHandler(repo service.AdminAccountRepository, runtime service.AccountRuntimeBlocker, invalidator service.TokenCacheInvalidator, openAI *service.OpenAIOAuthService, grok service.GrokOAuthTokenService) *adminhandler.AccountHandler {
	admin := service.NewAdminService(&config.Config{}, nil, nil, repo, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, integrationEntClient, nil, nil, nil, nil, runtime, nil, nil, nil, nil)
	return adminhandler.NewAccountHandler(admin, nil, openAI, nil, nil, grok, nil, nil, nil, nil, nil, nil, nil, invalidator)
}

func assertAdminOAuthStoredOwner(t *testing.T, expected, actual *service.Account) {
	t.Helper()
	require.Equal(t, expected.Type, actual.Type)
	require.Equal(t, expected.Credentials, actual.Credentials)
	require.Equal(t, expected.Extra, actual.Extra)
	require.Equal(t, expected.Status, actual.Status)
	require.Equal(t, expected.ErrorMessage, actual.ErrorMessage)
	require.Equal(t, expected.Schedulable, actual.Schedulable)
	require.Equal(t, expected.TempUnschedulableUntil, actual.TempUnschedulableUntil)
	require.Equal(t, expected.TempUnschedulableReason, actual.TempUnschedulableReason)
	require.Equal(t, expected.RateLimitedAt, actual.RateLimitedAt)
	require.Equal(t, expected.RateLimitResetAt, actual.RateLimitResetAt)
	require.Equal(t, expected.ProxyID, actual.ProxyID)
	require.Equal(t, expected.Proxy.Password, actual.Proxy.Password)
}

func adminOAuthExpectedOwner(t *testing.T, account *service.Account) string {
	t.Helper()
	raw, err := json.Marshal(dto.AccountFromServiceShallow(account))
	require.NoError(t, err)
	var response struct {
		Owner string `json:"oauth_credential_owner"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Regexp(t, `^[0-9a-f]{64}$`, response.Owner)
	return response.Owner
}

func adminOAuthApplyPayload(t *testing.T, expectedOwner string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type": "oauth", "credentials": map[string]any{"access_token": "issued-access", "refresh_token": "issued-refresh", "sso_token": "never-persist"},
		"extra": map[string]any{"new_preference": "saved"}, "expected_owner": expectedOwner,
	})
	require.NoError(t, err)
	return string(payload)
}

func TestAdminApplyOAuthCredentialsKeepsFullOwnerAndAtomicRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		for _, scenario := range []string{"success", "relinked", "proxy edited", "manual pause", "newer cooldown", "row failure", "outbox failure"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				base, held, before, expectedStored, cache, runtime := newAdminOAuthFixture(t, platform, scenario)
				invalidator := &adminOAuthTokenInvalidator{}
				handler := newAdminOAuthHandler(held, runtime, invalidator, nil, nil)
				router := gin.New()
				router.POST("/accounts/:id/apply", handler.ApplyOAuthCredentials)
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/accounts/%d/apply", before.ID), strings.NewReader(adminOAuthApplyPayload(t, adminOAuthExpectedOwner(t, before))))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(response, request)
				require.Equal(t, int32(1), held.observed.Load(), "the request must reach the intended final-write race rather than fail basic validation")
				after, err := base.GetByID(context.Background(), before.ID)
				require.NoError(t, err)
				if scenario == "success" {
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Equal(t, "issued-access", after.GetCredential("access_token"))
					require.Equal(t, "issued-refresh", after.GetCredential("refresh_token"))
					require.Equal(t, "retained", after.GetCredential("custom_config"))
					require.NotContains(t, after.Credentials, "sso_token")
					require.Equal(t, "base", after.Extra["retained"])
					require.Equal(t, "saved", after.Extra["new_preference"])
					require.Equal(t, service.StatusActive, after.Status)
					require.Empty(t, after.ErrorMessage)
					require.Nil(t, after.TempUnschedulableUntil)
					require.Nil(t, after.RateLimitResetAt)
					require.NotContains(t, after.Extra, "model_rate_limits")
					if platform == service.PlatformGrok {
						require.Equal(t, false, after.Extra["grok_needs_reauth"])
					}
					require.Len(t, invalidator.accounts, 1)
					require.Equal(t, after.Credentials, invalidator.accounts[0].Credentials)
					require.Equal(t, []int64{before.ID}, runtime.cleared)
					cached := requireEvictedSchedulerAccount(t, base, cache, before.ID)
					require.Equal(t, after.Credentials, cached.Credentials)
					require.Equal(t, service.StatusActive, cached.Status)
					require.Equal(t, 1, cnControlOutboxCount(t, before.ID))
				} else {
					require.GreaterOrEqual(t, response.Code, http.StatusBadRequest, "a replaced owner or failed transaction must reject the old apply")
					assertAdminOAuthStoredOwner(t, expectedStored(), after)
					require.Empty(t, invalidator.accounts)
					require.Empty(t, runtime.cleared)
					require.Empty(t, cache.setAccounts)
					require.Zero(t, cnControlOutboxCount(t, before.ID))
				}
			})
		}
	}
}

func TestAdminSingleAndBatchRefreshCannotPublishLateOAuthOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		for _, operation := range []string{"single", "batch"} {
			for _, scenario := range []string{"success", "relinked", "proxy edited", "manual pause", "newer cooldown", "row failure", "outbox failure"} {
				t.Run(platform+"/"+operation+"/"+scenario, func(t *testing.T) {
					base, held, before, expectedStored, cache, runtime := newAdminOAuthFixture(t, platform, scenario)
					proxy, err := before.StrictProxyURL()
					require.NoError(t, err)
					openAIIssuer := &adminOAuthOpenAIIssuer{t: t, before: held.beforeWrite, proxy: proxy}
					grokIssuer := &adminOAuthGrokIssuer{t: t, before: held.beforeWrite, proxy: proxy}
					invalidator := &adminOAuthTokenInvalidator{}
					handler := newAdminOAuthHandler(held, runtime, invalidator, service.NewOpenAIOAuthService(nil, openAIIssuer), grokIssuer)
					router := gin.New()
					router.POST("/accounts/:id/refresh", handler.Refresh)
					router.POST("/batch-refresh", handler.BatchRefresh)
					path, body := fmt.Sprintf("/accounts/%d/refresh", before.ID), ""
					if operation == "batch" {
						path, body = "/batch-refresh", fmt.Sprintf(`{"account_ids":[%d]}`, before.ID)
					}
					response := httptest.NewRecorder()
					request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
					request.Header.Set("Content-Type", "application/json")
					router.ServeHTTP(response, request)
					require.Equal(t, int32(1), held.observed.Load())
					if platform == service.PlatformOpenAI {
						require.Equal(t, int32(1), openAIIssuer.calls.Load())
					} else {
						require.Equal(t, int32(1), grokIssuer.calls.Load())
					}
					after, err := base.GetByID(context.Background(), before.ID)
					require.NoError(t, err)
					if operation == "batch" {
						require.Equal(t, http.StatusOK, response.Code, response.Body.String())
						var envelope struct {
							Data struct {
								Success int `json:"success"`
								Failed  int `json:"failed"`
							} `json:"data"`
						}
						require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
						require.Equal(t, map[bool]int{false: 0, true: 1}[scenario == "success"], envelope.Data.Success)
						require.Equal(t, map[bool]int{false: 1, true: 0}[scenario == "success"], envelope.Data.Failed)
					} else if scenario == "success" {
						require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					} else {
						require.GreaterOrEqual(t, response.Code, http.StatusBadRequest)
					}
					if scenario == "success" {
						require.Equal(t, "issued-access", after.GetCredential("access_token"))
						require.Equal(t, "issued-refresh", after.GetCredential("refresh_token"))
						require.Equal(t, "retained", after.GetCredential("custom_config"))
						require.Equal(t, before.Status, after.Status, "refresh must retain the existing recovery contract")
						require.Equal(t, before.TempUnschedulableUntil, after.TempUnschedulableUntil)
						require.Equal(t, before.Extra, after.Extra)
						require.Len(t, invalidator.accounts, 1)
						require.Equal(t, after.Credentials, invalidator.accounts[0].Credentials)
						cached := requireEvictedSchedulerAccount(t, base, cache, before.ID)
						require.Equal(t, after.Credentials, cached.Credentials)
						require.Equal(t, 1, cnControlOutboxCount(t, before.ID))
					} else {
						assertAdminOAuthStoredOwner(t, expectedStored(), after)
						require.Empty(t, invalidator.accounts)
						require.Empty(t, cache.setAccounts)
						require.Zero(t, cnControlOutboxCount(t, before.ID))
					}
					require.Empty(t, runtime.cleared)
				})
			}
		}
	}
}

func TestAdminApplyOAuthCredentialsRejectsOwnerChangedBeforeHandlerRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		for _, scenario := range []string{"relinked", "proxy edited", "manual pause", "newer cooldown", "missing owner", "malformed owner"} {
			t.Run(platform+"/"+scenario, func(t *testing.T) {
				base, held, before, expectedStored, cache, runtime := newAdminOAuthFixture(t, platform, scenario)
				oldOwner := adminOAuthExpectedOwner(t, before)
				held.beforeWrite() // Another management operation completes before this request arrives.
				if scenario == "missing owner" {
					oldOwner = ""
				}
				if scenario == "malformed owner" {
					oldOwner = "untrusted-owner"
				}
				invalidator := &adminOAuthTokenInvalidator{}
				handler := newAdminOAuthHandler(held, runtime, invalidator, nil, nil)
				router := gin.New()
				router.POST("/accounts/:id/apply", handler.ApplyOAuthCredentials)
				response := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/accounts/%d/apply", before.ID), strings.NewReader(adminOAuthApplyPayload(t, oldOwner)))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(response, request)
				wantStatus := http.StatusConflict
				if scenario == "missing owner" || scenario == "malformed owner" {
					wantStatus = http.StatusBadRequest
				}
				require.Equal(t, wantStatus, response.Code, response.Body.String())
				require.Zero(t, held.finalWrites.Load(), "old authorization cannot adopt the replacement owner seen at handler admission")
				after, err := base.GetByID(context.Background(), before.ID)
				require.NoError(t, err)
				assertAdminOAuthStoredOwner(t, expectedStored(), after)
				require.Empty(t, runtime.cleared)
				require.Empty(t, invalidator.accounts)
				require.Empty(t, cache.setAccounts)
				require.Zero(t, cnControlOutboxCount(t, before.ID))
			})
		}
	}
}
