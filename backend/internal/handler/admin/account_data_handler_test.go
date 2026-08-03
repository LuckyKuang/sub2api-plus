package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	servermiddleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dataResponse struct {
	Code int         `json:"code"`
	Data dataPayload `json:"data"`
}

type exportSettingsStub struct {
	enabled bool
	err     error
}

func (s exportSettingsStub) GetStepUpEnabledStrict(context.Context) (bool, error) {
	return s.enabled, s.err
}

type exportTotpStub struct{ err error }

func (s exportTotpStub) VerifyCodeStrict(context.Context, int64, string) error { return s.err }

type exportLimiterStub struct {
	allowed  bool
	err      error
	failures int
	resets   int
}

func (s *exportLimiterStub) Allowed(context.Context, int64, string) (bool, error) {
	return s.allowed, s.err
}
func (s *exportLimiterStub) RecordFailure(context.Context, int64, string) error {
	s.failures++
	return s.err
}
func (s *exportLimiterStub) Reset(context.Context, int64, string) error {
	s.resets++
	return s.err
}

type exportAuditStub struct {
	entries []*service.AuditLog
	err     error
}

func (s *exportAuditStub) RecordCritical(_ context.Context, entry *service.AuditLog) error {
	s.entries = append(s.entries, entry)
	return s.err
}

type dataPayload struct {
	Type           string        `json:"type"`
	Version        int           `json:"version"`
	Proxies        []dataProxy   `json:"proxies"`
	Accounts       []dataAccount `json:"accounts"`
	SkippedShadows int           `json:"skipped_shadows"`
}

type dataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type dataAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
	ProxyKey    *string        `json:"proxy_key"`
	Concurrency int            `json:"concurrency"`
	Priority    int            `json:"priority"`
}

func setupAccountDataRouter(t *testing.T) (*gin.Engine, *stubAdminService, *exportAuditStub) {
	audit := &exportAuditStub{}
	return setupAccountDataRouterWithSecurity(t, exportSettingsStub{}, exportTotpStub{}, &exportLimiterStub{allowed: true}, audit)
}

func setupAccountDataRouterWithSecurity(
	t *testing.T,
	settings exportSettingsStub,
	totp exportTotpStub,
	limiter *exportLimiterStub,
	audit *exportAuditStub,
) (*gin.Engine, *stubAdminService, *exportAuditStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.users[0].Role = service.RoleAdmin
	require.NoError(t, adminSvc.users[0].SetPassword("admin-password"))

	h := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	h.configureExportSecurity(totp, settings, audit, limiter)

	router.Use(func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1})
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
		c.Set(servermiddleware.ContextKeyAuthEmail, "admin@example.com")
		authMethod := c.GetHeader("X-Test-Auth-Method")
		if authMethod == "" {
			authMethod = service.AuditAuthMethodJWT
		}
		c.Set("auth_method", authMethod)
		c.Next()
	})
	router.POST("/api/v1/admin/accounts/export", h.ExportData)
	router.POST("/api/v1/admin/accounts/data", h.ImportData)
	return router, adminSvc, audit
}

func newExportRequest(t *testing.T, scope map[string]any) *http.Request {
	t.Helper()
	payload := map[string]any{"password": "admin-password"}
	for key, value := range scope {
		payload[key] = value
	}
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/export", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestExportDataIncludesSecrets(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       12,
			Name:     "orphan",
			Protocol: "https",
			Host:     "10.0.0.1",
			Port:     443,
			Username: "o",
			Password: "p",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Extra:       map[string]any{"note": "x"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := newExportRequest(t, nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, dataType, resp.Data.Type)
	require.Equal(t, dataVersion, resp.Data.Version)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "pass", resp.Data.Proxies[0].Password)
	require.Len(t, resp.Data.Accounts, 1)
	require.Equal(t, "secret", resp.Data.Accounts[0].Credentials["token"])
}

func TestExportDataWithoutProxies(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := newExportRequest(t, map[string]any{"include_proxies": false})
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 0)
	require.Len(t, resp.Data.Accounts, 1)
	require.Nil(t, resp.Data.Accounts[0].ProxyKey)
}

// TestExportDataExcludesSparkShadow 验证外审第5轮 P1/P2:导出时排除 spark 影子账号
// (影子无凭据、导入侧强制 credentials 非空,混入会产出无法还原的坏备份),并透出跳过计数。
func TestExportDataExcludesSparkShadow(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)

	parentID := int64(21)
	adminSvc.accounts = []service.Account{
		{
			ID:          parentID,
			Name:        "mother",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Status:      service.StatusActive,
		},
		{
			ID:              22,
			Name:            "mother (Spark)",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Credentials:     map[string]any{}, // 影子恒空凭据
			ParentAccountID: &parentID,        // 影子标记
			QuotaDimension:  service.QuotaDimensionSpark,
			Status:          service.StatusActive,
		},
	}

	rec := httptest.NewRecorder()
	req := newExportRequest(t, map[string]any{"include_proxies": false})
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 1, "影子应被排除,仅导出母账号")
	require.Equal(t, "mother", resp.Data.Accounts[0].Name)
	require.Equal(t, 1, resp.Data.SkippedShadows, "跳过的影子数量应透出")
}

func TestExportDataPassesAccountFiltersAndSort(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)
	adminSvc.accounts = []service.Account{
		{ID: 1, Name: "acc-1", Status: service.StatusActive},
	}

	rec := httptest.NewRecorder()
	req := newExportRequest(t, map[string]any{"filters": map[string]any{
		"platform": "openai", "type": "oauth", "status": "active", "group": "12", "privacy_mode": "blocked", "search": "keyword", "sort_by": "priority", "sort_order": "desc",
	}})
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListAccounts.calls)
	require.Equal(t, "openai", adminSvc.lastListAccounts.platform)
	require.Equal(t, "oauth", adminSvc.lastListAccounts.accountType)
	require.Equal(t, "active", adminSvc.lastListAccounts.status)
	require.Equal(t, int64(12), adminSvc.lastListAccounts.groupID)
	require.Equal(t, "blocked", adminSvc.lastListAccounts.privacyMode)
	require.Equal(t, "keyword", adminSvc.lastListAccounts.search)
	require.Equal(t, "priority", adminSvc.lastListAccounts.sortBy)
	require.Equal(t, "desc", adminSvc.lastListAccounts.sortOrder)
}

func TestExportDataSelectedIDsOverrideFilters(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)

	rec := httptest.NewRecorder()
	req := newExportRequest(t, map[string]any{"ids": []int64{1, 2}})
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 2)
	require.Equal(t, 0, adminSvc.lastListAccounts.calls)
}

func TestExportDataRequiresCurrentAdminPasswordAndWritesNoStoreAudit(t *testing.T) {
	router, adminSvc, audit := setupAccountDataRouter(t)
	adminSvc.accounts = []service.Account{{
		ID:          21,
		Name:        "account",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"token": "export-secret"},
		Status:      service.StatusActive,
	}}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newExportRequest(t, map[string]any{"password": "wrong-password"}))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "export-secret")
	require.Len(t, audit.entries, 1)
	require.Equal(t, "denied", audit.entries[0].Extra["result"])
	require.Equal(t, "password_invalid", audit.entries[0].Extra["error_code"])
	require.Equal(t, true, audit.entries[0].Extra["include_proxies"])
	require.Empty(t, audit.entries[0].RequestBody)
	require.Equal(t, service.AuditActionAccountsExport, audit.entries[0].Action)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, newExportRequest(t, nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "no-store, private, max-age=0", rec.Header().Get("Cache-Control"))
	require.Equal(t, "no-cache", rec.Header().Get("Pragma"))
	require.Equal(t, "0", rec.Header().Get("Expires"))
	require.Len(t, audit.entries, 2)
	require.Equal(t, "success", audit.entries[1].Extra["result"])
	require.Equal(t, 1, audit.entries[1].Extra["account_count"])
	require.Empty(t, audit.entries[1].RequestBody)
}

func TestExportDataRequiresTotpWhenStepUpIsEnabled(t *testing.T) {
	audit := &exportAuditStub{}
	router, _, _ := setupAccountDataRouterWithSecurity(
		t,
		exportSettingsStub{enabled: true},
		exportTotpStub{err: errors.New("invalid code")},
		&exportLimiterStub{allowed: true},
		audit,
	)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, newExportRequest(t, map[string]any{"totp_code": "123456"}))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Len(t, audit.entries, 1)
	require.Equal(t, "step_up_invalid", audit.entries[0].Extra["error_code"])
}

func TestExportDataRejectsAdministratorAPIKey(t *testing.T) {
	router, adminSvc, audit := setupAccountDataRouter(t)
	adminSvc.accounts = []service.Account{{
		ID:          21,
		Name:        "account",
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Credentials: map[string]any{"token": "export-secret"},
		Status:      service.StatusActive,
	}}

	rec := httptest.NewRecorder()
	req := newExportRequest(t, nil)
	req.Header.Set("X-Test-Auth-Method", service.AuditAuthMethodAdminAPIKey)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.NotContains(t, rec.Body.String(), "export-secret")
	require.Len(t, audit.entries, 1)
	require.Equal(t, "jwt_session_required", audit.entries[0].Extra["error_code"])
}

func TestLegacyAccountExportGetRouteCannotExport(t *testing.T) {
	router, _, _ := setupAccountDataRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestImportDataReusesProxyAndSkipsDefaultGroup(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy",
			Protocol: "socks5",
			Host:     "1.2.3.4",
			Port:     1080,
			Username: "u",
			Password: "p",
			Status:   service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "socks5|1.2.3.4|1080|u|p",
					"name":      "proxy",
					"protocol":  "socks5",
					"host":      "1.2.3.4",
					"port":      1080,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"token": "x"},
					"proxy_key":   "socks5|1.2.3.4|1080|u|p",
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdProxies, 0)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, adminSvc.createdAccounts[0].SkipDefaultGroupBind)
}

func TestImportDataRemovesInstanceLocalOpenAIOAuthSessionPolicy(t *testing.T) {
	router, adminSvc, _ := setupAccountDataRouter(t)

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{},
			"accounts": []map[string]any{
				{
					"name":        "oauth-backup",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"access_token": "secret"},
					"extra": map[string]any{
						"preserved": true,
						service.OpenAIOAuthSessionPolicyExtraKey: map[string]any{
							"enabled":           true,
							"allowed_group_ids": []int64{11, 12},
							"scope_version":     "source-instance-scope",
						},
					},
					"concurrency": 1,
					"priority":    1,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, err := json.Marshal(dataPayload)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdAccounts, 1)
	require.NotContains(t, adminSvc.createdAccounts[0].Extra, service.OpenAIOAuthSessionPolicyExtraKey)
	require.Equal(t, true, adminSvc.createdAccounts[0].Extra["preserved"])

	var response struct {
		Data DataImportResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, 1, response.Data.AccountCreated)
	require.Zero(t, response.Data.AccountFailed)
	require.Len(t, response.Data.Warnings, 1)
	require.Contains(t, response.Data.Warnings[0].Message, "session-sharing policy was removed")
}
