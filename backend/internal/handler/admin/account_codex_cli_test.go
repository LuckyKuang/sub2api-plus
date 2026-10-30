//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexCLIAdminStub struct {
	*stubAdminService
	commits      int
	beforeCommit func()
}

func (s *codexCLIAdminStub) CommitOAuthCredentials(_ context.Context, expected *service.Account, accountType string, credentials, _ map[string]any, recover bool) (*service.Account, error) {
	if s.beforeCommit != nil {
		s.beforeCommit()
	}
	if s.getAccountResult.ID != expected.ID || s.getAccountResult.Type != expected.Type ||
		!reflect.DeepEqual(s.getAccountResult.Credentials, expected.Credentials) ||
		!reflect.DeepEqual(s.getAccountResult.ProxyID, expected.ProxyID) {
		return nil, infraerrors.New(http.StatusConflict, "OAUTH_OWNER_CHANGED", "account credentials changed")
	}
	s.commits++
	updated := *expected
	updated.Type, updated.Credentials = accountType, credentials
	s.getAccountResult = &updated
	return &updated, nil
}

func (s *codexCLIAdminStub) CommitCodexCLIAPIKeyConfiguration(ctx context.Context, expected *service.Account, credentials map[string]any) (*service.Account, error) {
	return s.CommitOAuthCredentials(ctx, expected, expected.Type, credentials, nil, false)
}

func TestCodexCLIAPIKeyAccountHandlerConfigOnly(t *testing.T) {
	stub, router := newCodexCLIHandlerFixture()
	stub.getAccountResult.Type = service.AccountTypeAPIKey
	stub.getAccountResult.Credentials = map[string]any{"api_key": "private-api-key", "base_url": "https://compatible.example/v1"}
	get := codexCLIRequest(t, router, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, get.Code)
	require.NotContains(t, get.Body.String(), "private-api-key")
	require.Contains(t, get.Body.String(), "http_bridge_only")
	owner := service.CodexCLIAccountOwner(stub.getAccountResult)
	put := codexCLIRequest(t, router, http.MethodPut, map[string]any{"expected_owner": owner, "config_toml": ""})
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, 1, stub.commits)
	require.Equal(t, "private-api-key", stub.getAccountResult.GetCredential("api_key"))
	require.Equal(t, "https://compatible.example/v1", stub.getAccountResult.GetCredential("base_url"))
	owner = service.CodexCLIAccountOwner(stub.getAccountResult)
	bad := codexCLIRequest(t, router, http.MethodPut, map[string]any{"expected_owner": owner, "config_toml": "", "auth": map[string]any{"tokens": map[string]any{"access_token": "forbidden"}}})
	require.Equal(t, http.StatusBadRequest, bad.Code)
	require.Equal(t, 1, stub.commits)
}

func newCodexCLIHandlerFixture() (*codexCLIAdminStub, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	stub := &codexCLIAdminStub{stubAdminService: newStubAdminService()}
	stub.getAccountResult = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{
		"access_token": "private-handler-access", "refresh_token": "private-handler-refresh", "id_token": "private-handler-id", "chatgpt_account_id": "workspace-one", "chatgpt_user_id": "user-one",
	}}
	h := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/accounts/:id/codex-cli", h.GetCodexCLIAccount)
	router.PUT("/accounts/:id/codex-cli", h.UpdateCodexCLIAccount)
	return stub, router
}

func codexCLIRequest(t *testing.T, router *gin.Engine, method string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, "/accounts/1/codex-cli", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

// User contract: configuration/auth edits have one database commit, no token
// readback, and no writes for malformed config, a stale owner or mixed accounts.
func TestCodexCLIAccountHandlerSecretAwareRoundTrip(t *testing.T) {
	stub, router := newCodexCLIHandlerFixture()
	get := codexCLIRequest(t, router, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, get.Code)
	require.Equal(t, "no-store", get.Header().Get("Cache-Control"))
	require.NotContains(t, get.Body.String(), "private-handler")
	owner := service.CodexCLIAccountOwner(stub.getAccountResult)
	put := codexCLIRequest(t, router, http.MethodPut, map[string]any{
		"expected_owner": owner, "config_toml": "sandbox_mode = \"read-only\"\napproval_policy = \"never\"\n", "auth": map[string]any{"tokens": map[string]any{"refresh_token": nil}},
	})
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, 1, stub.commits)
	require.NotContains(t, put.Body.String(), "private-handler")
	require.Equal(t, "", stub.getAccountResult.GetCredential("refresh_token"))
	require.Equal(t, "private-handler-access", stub.getAccountResult.GetCredential("access_token"))
}

func TestCodexCLIAccountHandlerValidationPrecedesEveryWrite(t *testing.T) {
	for _, scenario := range []string{"bad-config", "unsupported-approval", "profile-reference", "profile-definitions", "unknown-field", "nested-secret", "stale-owner", "foreign-workspace", "commit-race", "unsupported-account"} {
		t.Run(scenario, func(t *testing.T) {
			stub, router := newCodexCLIHandlerFixture()
			body := map[string]any{"expected_owner": service.CodexCLIAccountOwner(stub.getAccountResult), "config_toml": ""}
			expectedStatus := http.StatusBadRequest
			switch scenario {
			case "bad-config":
				body["config_toml"] = "access_token = \"private-invalid-token\""
			case "unsupported-approval":
				body["config_toml"] = "approval_policy = \"on-request\"\n"
			case "profile-reference":
				body["config_toml"] = "profile = \"private-invalid-token\""
			case "profile-definitions":
				body["config_toml"] = "[profiles.private-invalid-token]\nmodel_reasoning_effort = \"high\"\n"
			case "unknown-field":
				body["auth_json"] = "private-invalid-token"
			case "nested-secret":
				body["auth"] = map[string]any{"tokens": map[string]any{"private_key": "private-invalid-token"}}
			case "stale-owner":
				stub.getAccountResult.Credentials["refresh_token"] = "replacement-grant"
				expectedStatus = http.StatusConflict
			case "foreign-workspace":
				body["auth"] = map[string]any{"tokens": map[string]any{"account_id": "workspace-two"}}
			case "commit-race":
				stub.beforeCommit = func() { stub.getAccountResult.Credentials["refresh_token"] = "replacement-grant" }
				expectedStatus = http.StatusConflict
			case "unsupported-account":
				stub.getAccountResult.Type = service.AccountTypeSetupToken
			}
			response := codexCLIRequest(t, router, http.MethodPut, body)
			require.Equal(t, expectedStatus, response.Code, response.Body.String())
			require.Zero(t, stub.commits)
			require.Zero(t, stub.updateAccountCalls)
			require.NotContains(t, response.Body.String(), "private-invalid-token")
			require.NotContains(t, response.Body.String(), "private-handler")
		})
	}
}
