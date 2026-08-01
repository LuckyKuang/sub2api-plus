package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type passkeySwitchSettingRepo struct {
	value string
	err   error
}

func (r *passkeySwitchSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (r *passkeySwitchSettingRepo) GetValue(context.Context, string) (string, error) {
	return r.value, r.err
}
func (r *passkeySwitchSettingRepo) Set(context.Context, string, string) error { return nil }
func (r *passkeySwitchSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *passkeySwitchSettingRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (r *passkeySwitchSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *passkeySwitchSettingRepo) Delete(context.Context, string) error { return nil }

func TestBindPasskeyFinishRequestRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/passkey/login/finish",
		strings.NewReader(`{"credential":"`+strings.Repeat("x", passkeyFinishBodyMaxBytes)+`"}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	_, ok := bindPasskeyFinishRequest(context)
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPasskeyBeginLoginRejectsDisabledAdminSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &passkeySwitchSettingRepo{value: "false"}
	settings := service.NewSettingService(repo, &config.Config{
		WebAuthn: config.WebAuthnConfig{Enabled: true},
	})
	handler := NewPasskeyHandler(nil, nil, settings)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/passkey/login/begin", nil)

	handler.BeginLogin(ginContext)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "PASSKEY_DISABLED")
}

func TestPasskeyBeginLoginReportsSettingStoreFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := service.NewSettingService(
		&passkeySwitchSettingRepo{err: errors.New("database unavailable")},
		&config.Config{WebAuthn: config.WebAuthnConfig{Enabled: true}},
	)
	handler := NewPasskeyHandler(nil, nil, settings)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/passkey/login/begin", nil)

	handler.BeginLogin(ginContext)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "PASSKEY_DISABLED")
}

func TestPasskeyCredentialListRemainsAvailableWhenSignInDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewPasskeyHandler(nil, nil, nil)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/api/v1/user/passkeys", nil)

	handler.List(ginContext)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "PASSKEY_DISABLED")
}

func TestPasskeySuccessfulLoginClearsIPFailureState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access, repo := newAuthIPAccessControlForTest(2)
	handler := &PasskeyHandler{ipAccessControl: access}

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/passkey/login/finish", nil)
	request.RemoteAddr = "203.0.113.24:43123"
	ginContext.Request = request

	require.True(t, handler.prepareSuccessfulLogin(ginContext, &service.User{ID: 42, Email: "passkey@example.test"}))
	require.Equal(t, []string{"203.0.113.24"}, repo.resetIPs)
	require.Equal(t, service.AuditAuthMethodPasskey, ginContext.GetString("auth_method"))
}

func TestPasskeySuccessfulLoginFailsClosedWhenIPStateCannotBeCleared(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access, repo := newAuthIPAccessControlForTest(2)
	repo.resetErr = errors.New("failure state database unavailable")
	handler := &PasskeyHandler{ipAccessControl: access}

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/passkey/login/finish", nil)
	request.RemoteAddr = "203.0.113.24:43123"
	ginContext.Request = request

	require.False(t, handler.prepareSuccessfulLogin(ginContext, &service.User{ID: 42, Email: "passkey@example.test"}))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "5", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), "IP_ACCESS_CONTROL_UNAVAILABLE")
}
