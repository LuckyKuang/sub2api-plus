package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type authIPAccessSettingStub struct {
	values map[string]string
}

func (s *authIPAccessSettingStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (s *authIPAccessSettingStub) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func (s *authIPAccessSettingStub) Set(context.Context, string, string) error {
	return nil
}

func (s *authIPAccessSettingStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *authIPAccessSettingStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *authIPAccessSettingStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}

func (s *authIPAccessSettingStub) Delete(context.Context, string) error {
	return nil
}

type authIPAccessRepositorySpy struct {
	failureCount int
	recordedIPs  []string
	thresholds   []int
	recordErr    error
	resetIPs     []string
	resetErr     error
	rules        []*service.IPAccessRule
}

func (s *authIPAccessRepositorySpy) ListIPAccessRules(context.Context, service.IPAccessRuleFilter) (*service.IPAccessRuleList, error) {
	return &service.IPAccessRuleList{}, nil
}

func (s *authIPAccessRepositorySpy) ListActiveIPAccessRules(context.Context) ([]*service.IPAccessRule, error) {
	return s.rules, nil
}

func (s *authIPAccessRepositorySpy) CreateManualIPAccessRule(context.Context, *service.IPAccessRule) (*service.IPAccessRule, error) {
	return nil, nil
}

func (s *authIPAccessRepositorySpy) CreateManualIPBlockForFailureState(context.Context, string, string, int64) (*service.IPFailureStateBlockRepositoryResult, error) {
	return nil, nil
}

func (s *authIPAccessRepositorySpy) ReleaseIPAccessRuleAndReset(context.Context, int64, int64) (*service.IPAccessRule, error) {
	return nil, service.ErrIPAccessRuleNotFound
}

func (s *authIPAccessRepositorySpy) ListIPLoginFailureStates(context.Context, service.IPLoginFailureStateFilter, time.Duration) (*service.IPLoginFailureStateList, error) {
	return &service.IPLoginFailureStateList{}, nil
}

func (s *authIPAccessRepositorySpy) ResetIPLoginFailureState(_ context.Context, normalizedIP string) error {
	if s.resetErr != nil {
		return s.resetErr
	}
	s.resetIPs = append(s.resetIPs, normalizedIP)
	return nil
}

func (s *authIPAccessRepositorySpy) RecordFailedLogin(_ context.Context, normalizedIP string, threshold int, _ time.Duration, _ time.Duration) (*service.LoginFailureRecordResult, error) {
	if s.recordErr != nil {
		return nil, s.recordErr
	}
	s.failureCount++
	s.recordedIPs = append(s.recordedIPs, normalizedIP)
	s.thresholds = append(s.thresholds, threshold)
	result := &service.LoginFailureRecordResult{
		FailureCount: s.failureCount,
		Blocked:      s.failureCount >= threshold,
	}
	if result.Blocked {
		result.Rule = &service.IPAccessRule{
			ID: 1, IPOrCIDR: normalizedIP, RuleKind: service.IPAccessRuleKindAutoBlock,
			Status: service.IPAccessRuleStatusActive,
		}
	}
	return result, nil
}

func (s *authIPAccessRepositorySpy) RecordIPAccessRuleHit(context.Context, string) error { return nil }

func newAuthIPAccessControlForTest(threshold int) (*service.IPAccessControlService, *authIPAccessRepositorySpy) {
	settings := &authIPAccessSettingStub{values: map[string]string{
		service.SettingKeyGlobalIPAccessControlEnabled: "true",
		service.SettingKeyIPAccessControlEnabled:       "true",
		service.SettingKeyLoginFailureAutoBlockEnabled: "true",
		service.SettingKeyLoginFailureIPThreshold:      strconv.Itoa(threshold),
		service.SettingKeyLoginFailureWindowMinutes:    "15",
		service.SettingKeyLoginFailureBlockMinutes:     "60",
	}}
	repo := &authIPAccessRepositorySpy{}
	return service.NewIPAccessControlService(settings, repo), repo
}

// attachDisabledIPAccessControlForWebSocketTest keeps WebSocket tests focused
// on their target behavior. Production wiring always injects the global policy
// and remains fail-closed when that dependency is unexpectedly unavailable.
func attachDisabledIPAccessControlForWebSocketTest(handler *OpenAIGatewayHandler) {
	if handler == nil || handler.ipAccessControl != nil {
		return
	}
	handler.SetIPAccessControlService(service.NewIPAccessControlService(
		&authIPAccessSettingStub{values: map[string]string{
			service.SettingKeyIPAccessControlEnabled: "false",
		}},
		&authIPAccessRepositorySpy{},
	))
}

func TestRecordFailedLocalLoginFailsClosedWhenCounterPersistenceFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access, repo := newAuthIPAccessControlForTest(2)
	repo.recordErr = errors.New("failure state database unavailable")
	handler := &AuthHandler{ipAccessControl: access}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	request.RemoteAddr = "203.0.113.24:43123"
	ctx.Request = request

	require.True(t, handler.recordFailedLocalLogin(ctx))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "5", recorder.Header().Get("Retry-After"))
	require.Contains(t, recorder.Body.String(), "IP_ACCESS_CONTROL_UNAVAILABLE")
}

func TestRecordFailedLocalLoginBlocksAtConfiguredThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access, repo := newAuthIPAccessControlForTest(2)
	handler := &AuthHandler{ipAccessControl: access}

	attempt := func() (*httptest.ResponseRecorder, bool) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		request.RemoteAddr = "203.0.113.24:43123"
		ctx.Request = request
		return recorder, handler.recordFailedLocalLogin(ctx)
	}

	first, aborted := attempt()
	require.False(t, aborted)
	require.Equal(t, 1, repo.failureCount)
	require.NotEqual(t, http.StatusForbidden, first.Code)

	second, aborted := attempt()
	require.True(t, aborted)
	require.Equal(t, 2, repo.failureCount)
	require.Equal(t, []string{"203.0.113.24", "203.0.113.24"}, repo.recordedIPs)
	require.Equal(t, http.StatusForbidden, second.Code)
	require.Contains(t, second.Body.String(), "IP_BANNED")
}

type missingLoginSessionTotpCache struct {
	lookupErr error
}

func (c *missingLoginSessionTotpCache) GetSetupSession(context.Context, int64) (*service.TotpSetupSession, error) {
	return nil, nil
}

func (c *missingLoginSessionTotpCache) SetSetupSession(context.Context, int64, *service.TotpSetupSession, time.Duration) error {
	return nil
}

func (c *missingLoginSessionTotpCache) DeleteSetupSession(context.Context, int64) error {
	return nil
}

func (c *missingLoginSessionTotpCache) GetLoginSession(context.Context, string) (*service.TotpLoginSession, error) {
	return nil, c.lookupErr
}

func (c *missingLoginSessionTotpCache) SetLoginSession(context.Context, string, *service.TotpLoginSession, time.Duration) error {
	return nil
}

func (c *missingLoginSessionTotpCache) DeleteLoginSession(context.Context, string) error {
	return nil
}

func (c *missingLoginSessionTotpCache) IncrementVerifyAttempts(context.Context, int64) (int, error) {
	return 0, nil
}

func (c *missingLoginSessionTotpCache) GetVerifyAttempts(context.Context, int64) (int, error) {
	return 0, nil
}

func (c *missingLoginSessionTotpCache) ClearVerifyAttempts(context.Context, int64) error {
	return nil
}

func (c *missingLoginSessionTotpCache) SetStepUpGrant(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *missingLoginSessionTotpCache) HasStepUpGrant(context.Context, int64, string) (bool, error) {
	return false, nil
}

func TestLogin2FAInvalidSessionDoesNotRecordCredentialFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access, repo := newAuthIPAccessControlForTest(2)
	handler := &AuthHandler{
		ipAccessControl: access,
		totpService: service.NewTotpService(
			nil, nil, &missingLoginSessionTotpCache{}, nil, nil, nil,
		),
	}

	login := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/2fa", bytes.NewBufferString(`{"temp_token":"expired-session-token","totp_code":"123456"}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "203.0.113.24:43123"
		ctx.Request = request
		handler.Login2FA(ctx)
		return recorder
	}

	first := login()
	require.Equal(t, http.StatusBadRequest, first.Code)
	require.Contains(t, first.Body.String(), "Invalid or expired 2FA session")
	require.Empty(t, repo.recordedIPs)

	second := login()
	require.Equal(t, http.StatusBadRequest, second.Code)
	require.Contains(t, second.Body.String(), "Invalid or expired 2FA session")
	require.Empty(t, repo.recordedIPs)
}

func TestLogin2FASessionLookupFailureDoesNotRecordLoginFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access, repo := newAuthIPAccessControlForTest(2)
	handler := &AuthHandler{
		ipAccessControl: access,
		totpService: service.NewTotpService(
			nil, nil, &missingLoginSessionTotpCache{lookupErr: errors.New("cache unavailable")}, nil, nil, nil,
		),
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/2fa", bytes.NewBufferString(`{"temp_token":"lookup-error-token","totp_code":"123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "203.0.113.24:43123"
	ctx.Request = request
	handler.Login2FA(ctx)

	require.Empty(t, repo.recordedIPs)
}

func TestOpenAIWebSocketIPAccessCheckRejectsBlockedClient(t *testing.T) {
	access, repo := newAuthIPAccessControlForTest(2)
	repo.rules = []*service.IPAccessRule{{
		IPOrCIDR: "203.0.113.24",
		RuleKind: service.IPAccessRuleKindManualBlock,
		Status:   service.IPAccessRuleStatusActive,
	}}
	handler := &OpenAIGatewayHandler{}
	handler.SetIPAccessControlService(access)

	err := handler.enforceOpenAIWSIPAccess(context.Background(), ip.ClientIdentity{
		EffectiveIP:        "203.0.113.24",
		SafeForEnforcement: true,
	})
	var closeErr *service.OpenAIWSClientCloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
	require.Equal(t, openAIWSIPBannedMessage, closeErr.Reason())
}

func TestOpenAIWSClientPolicyCloseClassification(t *testing.T) {
	policyErr := service.NewOpenAIWSClientCloseError(
		coderws.StatusPolicyViolation,
		openAIWSIPBannedMessage,
		nil,
	)
	closeErr, ok := openAIWSClientPolicyClose(policyErr)
	require.True(t, ok)
	require.NotNil(t, closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())

	closeErr, ok = openAIWSClientPolicyClose(errors.New("upstream websocket failed"))
	require.False(t, ok)
	require.Nil(t, closeErr)
}

func TestOpenAIWebSocketIPAccessWatcherCancelsBlockedConnection(t *testing.T) {
	access, repo := newAuthIPAccessControlForTest(2)
	repo.rules = []*service.IPAccessRule{{
		IPOrCIDR: "203.0.113.24",
		RuleKind: service.IPAccessRuleKindManualBlock,
		Status:   service.IPAccessRuleStatusActive,
	}}
	handler := &OpenAIGatewayHandler{}
	handler.SetIPAccessControlService(access)

	ctx, stop := handler.watchOpenAIWSIPAccessWithInterval(context.Background(), ip.ClientIdentity{
		EffectiveIP: "203.0.113.24", SafeForEnforcement: true,
	}, 5*time.Millisecond)
	defer stop()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("IP policy watcher did not cancel blocked WebSocket context")
	}
	closeErr, ok := openAIWSClientPolicyClose(context.Cause(ctx))
	require.True(t, ok)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
	require.Equal(t, openAIWSIPBannedMessage, closeErr.Reason())
}
