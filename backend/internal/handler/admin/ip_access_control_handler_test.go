package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	servermiddleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIPAccessControlTrustedProxyStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("configured proxy resolves forwarded client", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				TrustedProxiesConfigured: true,
				TrustedProxies:           []string{"10.0.0.0/8"},
			},
		}
		handler := NewIPAccessControlHandler(nil, cfg)
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(cfg.Server.TrustedProxies))
		router.GET("/status", handler.GetTrustedProxyStatus)

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/status", nil)
		request.RemoteAddr = "10.1.2.3:43123"
		request.Header.Set("X-Forwarded-For", "203.0.113.8")
		request.Header.Set("CF-Connecting-IP", "203.0.113.8")
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		status := decodeTrustedProxyStatus(t, recorder)
		require.Equal(t, string(ip.TrustedProxyStateConfigured), status.ConfigurationState)
		require.Equal(t, []string{"10.0.0.0/8"}, status.TrustedProxies)
		require.Equal(t, "203.0.113.8", status.ClientIP)
		require.Equal(t, "10.1.2.3", status.DirectPeerIP)
		require.True(t, status.DirectPeerTrusted)
		require.True(t, status.TrustedProxyApplied)
		require.True(t, status.AutomaticBlockingReady)
		require.True(t, status.ManualBlockingReady)
		require.Equal(t, []string{"CF-Connecting-IP", "X-Forwarded-For"}, status.ForwardedHeaders)
	})

	t.Run("unconfigured proxy does not trust spoofed header", func(t *testing.T) {
		handler := NewIPAccessControlHandler(nil, &config.Config{})
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(nil))
		router.GET("/status", handler.GetTrustedProxyStatus)

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/status", nil)
		request.RemoteAddr = "198.51.100.20:43123"
		request.Header.Set("X-Forwarded-For", "203.0.113.9")
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		status := decodeTrustedProxyStatus(t, recorder)
		require.Equal(t, string(ip.TrustedProxyStateNotConfigured), status.ConfigurationState)
		require.NotNil(t, status.TrustedProxies)
		require.Empty(t, status.TrustedProxies)
		require.Equal(t, "198.51.100.20", status.ClientIP)
		require.Equal(t, "198.51.100.20", status.DirectPeerIP)
		require.False(t, status.DirectPeerTrusted)
		require.False(t, status.TrustedProxyApplied)
		require.False(t, status.AutomaticBlockingReady)
		require.False(t, status.ManualBlockingReady)
		require.Equal(t, []string{"X-Forwarded-For"}, status.ForwardedHeaders)
	})
}

func TestBlockFailureStateHandlerRequiresSafeIdentityAndReturnsConfirmedResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("unsafe client identity is rejected with stable conflict code", func(t *testing.T) {
		handler := NewIPAccessControlHandler(nil, &config.Config{})
		router := gin.New()
		router.POST("/block", func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
			handler.BlockFailureState(c)
		})

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/block", strings.NewReader(`{"ip":"203.0.113.8"}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "198.51.100.9:43123"
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusConflict, recorder.Code)
		var envelope struct {
			Reason string `json:"reason"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		require.Equal(t, "IP_ACCESS_IDENTITY_UNSAFE", envelope.Reason)
	})

	t.Run("safe direct deployment returns confirmed manual block", func(t *testing.T) {
		settings := &handlerIPAccessSettingStub{values: map[string]string{
			service.SettingKeyGlobalIPAccessControlEnabled: "true",
			service.SettingKeyIPAccessControlEnabled:       "true",
			service.SettingKeyLoginFailureBlockMinutes:     "60",
		}}
		repo := &handlerIPAccessRepositoryStub{}
		access := service.NewIPAccessControlService(settings, repo)
		handler := NewIPAccessControlHandler(access, &config.Config{Server: config.ServerConfig{
			TrustedProxiesConfigured: true,
			TrustedProxies:           []string{},
		}})
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(nil))
		router.POST("/block", func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 7})
			handler.BlockFailureState(c)
		})

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/block", strings.NewReader(`{"ip":"203.0.113.8"}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "198.51.100.9:43123"
		router.ServeHTTP(recorder, request)

		require.Equal(t, http.StatusOK, recorder.Code)
		var envelope struct {
			Data service.IPFailureStateBlockResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
		require.True(t, envelope.Data.EffectivelyBlocked)
		require.False(t, envelope.Data.AlreadyBlocked)
		require.NotNil(t, envelope.Data.Rule)
		require.Equal(t, service.IPAccessRuleKindManualBlock, envelope.Data.Rule.RuleKind)
		require.Equal(t, "203.0.113.8", envelope.Data.Rule.IPOrCIDR)
	})
}

func TestAutomaticBlockingRequiresSafeProxyConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(headers map[string]string) *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		request := httptest.NewRequest(http.MethodPut, "/settings", nil)
		request.RemoteAddr = "198.51.100.9:43123"
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		ctx.Request = request
		return ctx
	}

	t.Run("absent client IP trust mode is rejected", func(t *testing.T) {
		h := NewIPAccessControlHandler(nil, &config.Config{})
		require.False(t, h.automaticBlockingCanBeEnabled(newContext(nil)))
	})

	t.Run("explicit direct deployment is allowed without forwarded headers", func(t *testing.T) {
		h := NewIPAccessControlHandler(nil, &config.Config{Server: config.ServerConfig{
			TrustedProxiesConfigured: true,
			TrustedProxies:           []string{},
		}})
		require.True(t, h.automaticBlockingCanBeEnabled(newContext(nil)))
	})

	t.Run("explicit direct deployment rejects forwarded headers", func(t *testing.T) {
		h := NewIPAccessControlHandler(nil, &config.Config{Server: config.ServerConfig{
			TrustedProxiesConfigured: true,
			TrustedProxies:           []string{},
		}})
		require.False(t, h.automaticBlockingCanBeEnabled(newContext(map[string]string{"CF-Connecting-IP": "203.0.113.8"})))
	})

	t.Run("valid trusted proxy is allowed", func(t *testing.T) {
		h := NewIPAccessControlHandler(nil, &config.Config{Server: config.ServerConfig{
			TrustedProxiesConfigured: true,
			TrustedProxies:           []string{"10.0.0.0/8"},
		}})
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies([]string{"10.0.0.0/8"}))
		allowed := false
		router.PUT("/settings", func(c *gin.Context) { allowed = h.automaticBlockingCanBeEnabled(c) })
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/settings", nil)
		request.RemoteAddr = "10.1.2.3:43123"
		request.Header.Set("X-Forwarded-For", "203.0.113.8")
		router.ServeHTTP(recorder, request)
		require.True(t, allowed)
	})

	t.Run("invalid and wildcard trusted proxy settings are rejected", func(t *testing.T) {
		for _, trustedProxies := range [][]string{{"not-a-cidr"}, {"*"}} {
			h := NewIPAccessControlHandler(nil, &config.Config{Server: config.ServerConfig{
				TrustedProxiesConfigured: true,
				TrustedProxies:           trustedProxies,
			}})
			require.False(t, h.automaticBlockingCanBeEnabled(newContext(nil)))
		}
	})
}

func decodeTrustedProxyStatus(t *testing.T, recorder *httptest.ResponseRecorder) trustedProxyStatusResponse {
	t.Helper()
	var envelope struct {
		Data trustedProxyStatusResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return envelope.Data
}

type handlerIPAccessSettingStub struct {
	values map[string]string
}

func (s *handlerIPAccessSettingStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (s *handlerIPAccessSettingStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}
func (s *handlerIPAccessSettingStub) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}
func (s *handlerIPAccessSettingStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}
func (s *handlerIPAccessSettingStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}
func (s *handlerIPAccessSettingStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}
func (s *handlerIPAccessSettingStub) Delete(_ context.Context, key string) error {
	delete(s.values, key)
	return nil
}

type handlerIPAccessRepositoryStub struct {
	rules []*service.IPAccessRule
}

func (s *handlerIPAccessRepositoryStub) ListIPAccessRules(context.Context, service.IPAccessRuleFilter) (*service.IPAccessRuleList, error) {
	return &service.IPAccessRuleList{}, nil
}
func (s *handlerIPAccessRepositoryStub) ListActiveIPAccessRules(context.Context) ([]*service.IPAccessRule, error) {
	return s.rules, nil
}
func (s *handlerIPAccessRepositoryStub) CreateManualIPAccessRule(context.Context, *service.IPAccessRule) (*service.IPAccessRule, error) {
	return nil, nil
}
func (s *handlerIPAccessRepositoryStub) CreateManualIPBlockForFailureState(_ context.Context, normalizedIP, reason string, expiresAt time.Time, actorUserID int64) (*service.IPFailureStateBlockRepositoryResult, error) {
	actor := actorUserID
	rule := &service.IPAccessRule{
		ID: 42, IPOrCIDR: normalizedIP, RuleKind: service.IPAccessRuleKindManualBlock,
		Status: service.IPAccessRuleStatusActive, Reason: reason, ExpiresAt: &expiresAt,
		CreatedByUserID: &actor,
	}
	s.rules = append(s.rules, rule)
	return &service.IPFailureStateBlockRepositoryResult{Rule: rule}, nil
}
func (s *handlerIPAccessRepositoryStub) ReleaseIPAccessRuleAndReset(context.Context, int64, int64) (*service.IPAccessRule, error) {
	return nil, service.ErrIPAccessRuleNotFound
}
func (s *handlerIPAccessRepositoryStub) ListIPLoginFailureStates(context.Context, service.IPLoginFailureStateFilter, time.Duration) (*service.IPLoginFailureStateList, error) {
	return &service.IPLoginFailureStateList{}, nil
}
func (s *handlerIPAccessRepositoryStub) ResetIPLoginFailureState(context.Context, string) error {
	return nil
}
func (s *handlerIPAccessRepositoryStub) RecordFailedLogin(context.Context, string, int, time.Duration, time.Duration) (*service.LoginFailureRecordResult, error) {
	return &service.LoginFailureRecordResult{}, nil
}
func (s *handlerIPAccessRepositoryStub) RecordIPAccessRuleHit(context.Context, string) error {
	return nil
}
