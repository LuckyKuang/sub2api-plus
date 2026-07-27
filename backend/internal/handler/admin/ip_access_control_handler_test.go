package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
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
