package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
)

type ipAccessMiddlewareSettingsStub struct {
	values map[string]string
	err    error
}

func (s *ipAccessMiddlewareSettingsStub) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (s *ipAccessMiddlewareSettingsStub) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}
func (s *ipAccessMiddlewareSettingsStub) Set(context.Context, string, string) error { return nil }
func (s *ipAccessMiddlewareSettingsStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}
func (s *ipAccessMiddlewareSettingsStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (s *ipAccessMiddlewareSettingsStub) GetAll(context.Context) (map[string]string, error) {
	return s.values, nil
}
func (s *ipAccessMiddlewareSettingsStub) Delete(context.Context, string) error { return nil }

type ipAccessMiddlewareRuleStub struct {
	rules []*service.IPAccessRule
	err   error
}

func (s *ipAccessMiddlewareRuleStub) ListIPAccessRules(context.Context, service.IPAccessRuleFilter) (*service.IPAccessRuleList, error) {
	return &service.IPAccessRuleList{}, nil
}
func (s *ipAccessMiddlewareRuleStub) ListActiveIPAccessRules(context.Context) ([]*service.IPAccessRule, error) {
	return s.rules, s.err
}
func (s *ipAccessMiddlewareRuleStub) CreateManualIPAccessRule(context.Context, *service.IPAccessRule) (*service.IPAccessRule, error) {
	return nil, nil
}
func (s *ipAccessMiddlewareRuleStub) CreateManualIPBlockForFailureState(context.Context, string, string, int64) (*service.IPFailureStateBlockRepositoryResult, error) {
	return nil, nil
}
func (s *ipAccessMiddlewareRuleStub) ReleaseIPAccessRuleAndReset(context.Context, int64, int64) (*service.IPAccessRule, error) {
	return nil, service.ErrIPAccessRuleNotFound
}
func (s *ipAccessMiddlewareRuleStub) ListIPLoginFailureStates(context.Context, service.IPLoginFailureStateFilter, time.Duration) (*service.IPLoginFailureStateList, error) {
	return &service.IPLoginFailureStateList{}, nil
}
func (s *ipAccessMiddlewareRuleStub) ResetIPLoginFailureState(context.Context, string) error {
	return nil
}
func (s *ipAccessMiddlewareRuleStub) RecordFailedLogin(context.Context, string, int, time.Duration, time.Duration) (*service.LoginFailureRecordResult, error) {
	return nil, nil
}
func (s *ipAccessMiddlewareRuleStub) RecordIPAccessRuleHit(context.Context, string) error {
	return nil
}

func newBlockedIPRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	access := service.NewIPAccessControlService(
		&ipAccessMiddlewareSettingsStub{values: map[string]string{
			service.SettingKeyGlobalIPAccessControlEnabled: "true",
			service.SettingKeyIPAccessControlEnabled:       "true",
		}},
		&ipAccessMiddlewareRuleStub{rules: []*service.IPAccessRule{
			{
				IPOrCIDR: "203.0.113.8",
				RuleKind: service.IPAccessRuleKindManualBlock,
				Status:   service.IPAccessRuleStatusActive,
			},
		}},
	)
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("set trusted proxies: %v", err)
	}
	router.Use(gin.HandlerFunc(NewIPAccessControlMiddleware(access)))
	router.Any("/*path", func(c *gin.Context) { c.String(http.StatusOK, "next") })
	return router
}

func serveBlockedIPRequest(t *testing.T, router *gin.Engine, method, path, accept string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = "203.0.113.8:43123"
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestIPAccessControlMiddlewareResponseContracts(t *testing.T) {
	router := newBlockedIPRouter(t)

	t.Run("html navigation", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/admin/settings", "text/html")
		if recorder.Code != http.StatusForbidden ||
			!strings.Contains(recorder.Header().Get("Content-Type"), "text/html") ||
			recorder.Header().Get("X-Frame-Options") != "DENY" ||
			!strings.Contains(recorder.Body.String(), "Access denied") {
			t.Fatalf("unexpected HTML response: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	})

	t.Run("openai endpoint", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodPost, "/v1/responses", "application/json")
		if recorder.Code != http.StatusForbidden ||
			!strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) ||
			!strings.Contains(recorder.Body.String(), `"type":"access_denied"`) {
			t.Fatalf("unexpected OpenAI response: code=%d body=%q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("openai endpoint ignores html accept header", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/v1/models", "text/html")
		if recorder.Code != http.StatusForbidden ||
			strings.Contains(recorder.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) ||
			!strings.Contains(recorder.Body.String(), `"type":"access_denied"`) {
			t.Fatalf("OpenAI endpoint must retain its JSON contract: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	})

	t.Run("openai alias ignores html accept header", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/models", "text/html")
		if recorder.Code != http.StatusForbidden ||
			strings.Contains(recorder.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) {
			t.Fatalf("OpenAI alias must retain its JSON contract: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	})

	t.Run("anthropic endpoint uses anthropic error", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodPost, "/v1/messages", "text/html")
		if recorder.Code != http.StatusForbidden ||
			strings.Contains(recorder.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(recorder.Body.String(), `"type":"error"`) ||
			!strings.Contains(recorder.Body.String(), `"type":"permission_error"`) ||
			!strings.Contains(recorder.Body.String(), ipBannedMessage) {
			t.Fatalf("Anthropic endpoint must retain its JSON contract: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	})

	t.Run("google endpoint uses google error", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/v1beta/models", "text/html")
		if recorder.Code != http.StatusForbidden ||
			strings.Contains(recorder.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(recorder.Body.String(), `"code":403`) ||
			!strings.Contains(recorder.Body.String(), `"status":"PERMISSION_DENIED"`) ||
			!strings.Contains(recorder.Body.String(), ipBannedMessage) {
			t.Fatalf("Google endpoint must retain its JSON contract: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	})

	t.Run("regular api", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodPost, "/api/v1/auth/login", "application/json")
		if recorder.Code != http.StatusForbidden ||
			!strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) {
			t.Fatalf("unexpected API response: code=%d body=%q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("passkey authentication endpoints", func(t *testing.T) {
		for _, path := range []string{
			"/api/v1/auth/passkey/login/begin",
			"/api/v1/auth/passkey/login/finish",
		} {
			recorder := serveBlockedIPRequest(t, router, http.MethodPost, path, "application/json")
			if recorder.Code != http.StatusForbidden ||
				!strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) {
				t.Fatalf("passkey endpoint %s must be globally blocked: code=%d body=%q", path, recorder.Code, recorder.Body.String())
			}
		}
	})

	t.Run("regular api ignores html accept header", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/api/v1/auth/me", "text/html")
		if recorder.Code != http.StatusForbidden ||
			strings.Contains(recorder.Header().Get("Content-Type"), "text/html") ||
			!strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) {
			t.Fatalf("API endpoint must retain its JSON contract: code=%d content-type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
		}
	})

	t.Run("health remains available", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/health", "application/json")
		if recorder.Code != http.StatusOK || recorder.Body.String() != "next" {
			t.Fatalf("health endpoint must be exempt: code=%d body=%q", recorder.Code, recorder.Body.String())
		}
	})

	t.Run("readiness is blocked", func(t *testing.T) {
		recorder := serveBlockedIPRequest(t, router, http.MethodGet, "/ready", "application/json")
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"IP_BANNED"`) {
			t.Fatalf("readiness must remain behind global IP enforcement: code=%d body=%q", recorder.Code, recorder.Body.String())
		}
	})
}

func TestProvideIPAccessControlMiddlewareRejectsUnsafeEmergencyAllowlist(t *testing.T) {
	access := service.NewIPAccessControlService(
		&ipAccessMiddlewareSettingsStub{values: map[string]string{}},
		&ipAccessMiddlewareRuleStub{},
	)
	_, err := ProvideIPAccessControlMiddleware(access, &config.Config{Server: config.ServerConfig{
		IPAccessEmergencyAllowlist: []string{"0.0.0.0/0"},
	}})
	if err == nil {
		t.Fatal("unsafe emergency allowlist must prevent middleware startup")
	}
}

func TestIPAccessControlMiddlewareEmergencyAllowlistAllowsDirectPeerRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access := service.NewIPAccessControlService(
		&ipAccessMiddlewareSettingsStub{values: map[string]string{
			service.SettingKeyGlobalIPAccessControlEnabled: "true",
			service.SettingKeyIPAccessControlEnabled:       "true",
		}},
		&ipAccessMiddlewareRuleStub{rules: []*service.IPAccessRule{{
			IPOrCIDR: "203.0.113.8", RuleKind: service.IPAccessRuleKindManualBlock, Status: service.IPAccessRuleStatusActive,
		}}},
	)
	if err := access.ConfigureEmergencyAllowlist([]string{"203.0.113.8"}); err != nil {
		t.Fatalf("configure emergency allowlist: %v", err)
	}
	router := gin.New()
	router.Use(gin.HandlerFunc(NewIPAccessControlMiddlewareWithResolver(
		access,
		ip.NewClientIdentityResolver(ip.InspectTrustedProxyConfiguration(false, nil)),
	)))
	router.GET("/ready", func(c *gin.Context) { c.String(http.StatusOK, "ready") })
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/ready", nil)
	request.RemoteAddr = "203.0.113.8:43123"
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ready" {
		t.Fatalf("emergency allowlist must preserve recovery access: code=%d body=%q", recorder.Code, recorder.Body.String())
	}
}

func TestIPAccessControlMiddlewareFailsClosedWithoutSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &ipAccessMiddlewareSettingsStub{
		values: map[string]string{service.SettingKeyGlobalIPAccessControlEnabled: "true", service.SettingKeyIPAccessControlEnabled: "true"},
		err:    errors.New("database unavailable"),
	}
	access := service.NewIPAccessControlService(settings, &ipAccessMiddlewareRuleStub{})
	router := gin.New()
	router.Use(gin.HandlerFunc(NewIPAccessControlMiddleware(access)))
	router.Any("/*path", func(c *gin.Context) { c.String(http.StatusOK, "next") })

	tests := []struct {
		name     string
		path     string
		contains []string
	}{
		{
			name:     "openai",
			path:     "/v1/responses",
			contains: []string{`"code":"IP_ACCESS_CONTROL_UNAVAILABLE"`, `"type":"server_error"`},
		},
		{
			name:     "anthropic",
			path:     "/v1/messages",
			contains: []string{`"type":"error"`, `"type":"api_error"`},
		},
		{
			name:     "google",
			path:     "/v1beta/models",
			contains: []string{`"code":503`, `"status":"UNAVAILABLE"`},
		},
		{
			name:     "regular api",
			path:     "/api/v1/auth/me",
			contains: []string{`"code":"IP_ACCESS_CONTROL_UNAVAILABLE"`},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d: %s", recorder.Code, recorder.Body.String())
			}
			for _, expected := range test.contains {
				if !strings.Contains(recorder.Body.String(), expected) {
					t.Fatalf("response %q does not contain %q", recorder.Body.String(), expected)
				}
			}
		})
	}

	t.Run("health remains live", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/health", nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("health must stay available, got %d", recorder.Code)
		}
	})
}

func TestIPAccessControlReadinessRecoversWhenDatabaseReturns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &ipAccessMiddlewareSettingsStub{
		values: map[string]string{service.SettingKeyGlobalIPAccessControlEnabled: "true", service.SettingKeyIPAccessControlEnabled: "true"},
		err:    errors.New("database unavailable"),
	}
	rules := &ipAccessMiddlewareRuleStub{}
	access := service.NewIPAccessControlService(settings, rules)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewIPAccessControlMiddleware(access)))
	router.GET("/ready", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ready"}) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("cold readiness must fail closed, got %d", recorder.Code)
	}

	settings.err = nil
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if recorder.Code != http.StatusOK || !access.SecuritySnapshotReady() {
		t.Fatalf("readiness must recover after warmup succeeds: code=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
