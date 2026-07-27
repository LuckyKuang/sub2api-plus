//go:build unit

package ip

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetTrustedClientIPUsesGinClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))

	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetTrustedClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestGetClientIPPreservesLegacyDockerForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "192.168.32.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.2, 203.0.113.42")
	req.Header.Set("X-Real-IP", "192.168.32.1")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "203.0.113.42", w.Body.String())
}

func TestGetClientIPSkipsInvalidLegacyXFFCandidates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "192.168.32.1:12345"
	req.Header.Set("X-Forwarded-For", "invalid-ip, 203.0.113.42")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "203.0.113.42", w.Body.String())
}

func TestCheckIPRestrictionWithCompiledRules(t *testing.T) {
	whitelist := CompileIPRules([]string{"10.0.0.0/8", "192.168.1.2"})
	blacklist := CompileIPRules([]string{"10.1.1.1"})

	allowed, reason := CheckIPRestrictionWithCompiledRules("10.2.3.4", whitelist, blacklist)
	require.True(t, allowed)
	require.Equal(t, "", reason)

	allowed, reason = CheckIPRestrictionWithCompiledRules("10.1.1.1", whitelist, blacklist)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestCheckIPRestrictionWithCompiledRules_InvalidWhitelistStillDenies(t *testing.T) {
	// 与旧实现保持一致：白名单有配置但全无效时，最终应拒绝访问。
	invalidWhitelist := CompileIPRules([]string{"not-a-valid-pattern"})
	allowed, reason := CheckIPRestrictionWithCompiledRules("8.8.8.8", invalidWhitelist, nil)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestNormalizeIPOrCIDRCanonicalizesNetworks(t *testing.T) {
	require.Equal(t, "192.0.2.0/24", NormalizeIPOrCIDR("192.0.2.99/24"))
	require.Equal(t, "2001:db8::/64", NormalizeIPOrCIDR("2001:db8::1234/64"))
	require.Equal(t, "192.0.2.1", NormalizeIPOrCIDR("::ffff:192.0.2.1"))
	require.Equal(t, "", NormalizeIPOrCIDR("not-an-ip"))
}

func TestNormalizeNonGlobalIPOrCIDRRejectsBroadRecoveryRanges(t *testing.T) {
	require.Equal(t, "203.0.113.0/24", NormalizeNonGlobalIPOrCIDR("203.0.113.9/24"))
	require.Equal(t, "2001:db8::99", NormalizeNonGlobalIPOrCIDR("2001:db8::99"))
	require.Empty(t, NormalizeNonGlobalIPOrCIDR("0.0.0.0/0"))
	require.Empty(t, NormalizeNonGlobalIPOrCIDR("::/0"))
	require.Empty(t, NormalizeNonGlobalIPOrCIDR("::ffff:192.0.2.0/120"))
}

func TestInspectTrustedProxyConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		configured bool
		values     []string
		state      TrustedProxyConfigurationState
		trusted    bool
	}{
		{name: "not configured", state: TrustedProxyStateNotConfigured},
		{name: "explicit empty", configured: true, state: TrustedProxyStateEmpty},
		{name: "ipv4 cidr", configured: true, values: []string{"10.0.0.0/8"}, state: TrustedProxyStateConfigured, trusted: true},
		{name: "ipv6 address", configured: true, values: []string{"2001:db8::1"}, state: TrustedProxyStateConfigured},
		{name: "invalid", configured: true, values: []string{"not-an-ip"}, state: TrustedProxyStateInvalid},
		{name: "wildcard is unsafe", configured: true, values: []string{"*"}, state: TrustedProxyStateInvalid},
		{name: "ipv4 global cidr is unsafe", configured: true, values: []string{"0.0.0.0/0"}, state: TrustedProxyStateInvalid},
		{name: "ipv6 global cidr is unsafe", configured: true, values: []string{"::/0"}, state: TrustedProxyStateInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configuration := InspectTrustedProxyConfiguration(tc.configured, tc.values)
			require.Equal(t, tc.state, configuration.State)
			require.Equal(t, tc.trusted, configuration.DirectPeerTrusted("10.1.2.3"))
		})
	}
}

func TestClientIdentityResolverRequiresCompleteTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	policy := InspectTrustedProxyConfiguration(true, []string{"10.0.0.0/8"})
	resolver := NewClientIdentityResolver(policy)

	t.Run("trusted peer with downstream source", func(t *testing.T) {
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(policy.Values))
		var identity ClientIdentity
		router.GET("/t", func(c *gin.Context) { identity = resolver.Resolve(c) })
		request := httptest.NewRequest("GET", "/t", nil)
		request.RemoteAddr = "10.1.2.3:12345"
		request.Header.Set("X-Forwarded-For", "203.0.113.8")
		router.ServeHTTP(httptest.NewRecorder(), request)
		require.True(t, identity.SafeForEnforcement)
		require.Equal(t, "203.0.113.8", identity.EffectiveIP)
		require.Equal(t, ClientIdentitySourceTrustedForwarded, identity.Source)
	})

	t.Run("trusted peer without forwarded source is unsafe", func(t *testing.T) {
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(policy.Values))
		var identity ClientIdentity
		router.GET("/t", func(c *gin.Context) { identity = resolver.Resolve(c) })
		request := httptest.NewRequest("GET", "/t", nil)
		request.RemoteAddr = "10.1.2.3:12345"
		router.ServeHTTP(httptest.NewRecorder(), request)
		require.False(t, identity.SafeForEnforcement)
		require.Equal(t, "unsafe_proxy_chain", identity.FailureReason)
	})

	t.Run("multi-hop forwarding is unsafe until the final proxy rewrites it", func(t *testing.T) {
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(policy.Values))
		var identity ClientIdentity
		router.GET("/t", func(c *gin.Context) { identity = resolver.Resolve(c) })
		request := httptest.NewRequest("GET", "/t", nil)
		request.RemoteAddr = "10.1.2.3:12345"
		// Gin would return the right-most untrusted address (the CDN hop), not
		// the original client. The global policy must refuse this ambiguity.
		request.Header.Set("X-Forwarded-For", "198.51.100.8, 203.0.113.8")
		router.ServeHTTP(httptest.NewRecorder(), request)
		require.False(t, identity.SafeForEnforcement)
		require.Equal(t, "forwarded_chain_not_sanitized", identity.FailureReason)
	})

	t.Run("a single rewritten X-Real-IP is accepted when XFF is absent", func(t *testing.T) {
		router := gin.New()
		require.NoError(t, router.SetTrustedProxies(policy.Values))
		var identity ClientIdentity
		router.GET("/t", func(c *gin.Context) { identity = resolver.Resolve(c) })
		request := httptest.NewRequest("GET", "/t", nil)
		request.RemoteAddr = "10.1.2.3:12345"
		request.Header.Set("X-Real-IP", "203.0.113.8")
		router.ServeHTTP(httptest.NewRecorder(), request)
		require.True(t, identity.SafeForEnforcement)
		require.Equal(t, "203.0.113.8", identity.EffectiveIP)
	})
}

func TestGetSecurityClientIPSwitchEnabledUsesLegacyHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPCustomHeaderPrecedenceAndFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		trustForward   bool
		headers        []string
		requestHeaders map[string]string
		want           string
	}{
		{
			name:         "configured order precedes built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":      "198.51.100.10",
				"X-CDN-Second":     "203.0.113.20",
				"CF-Connecting-IP": "8.8.8.8",
			},
			want: "198.51.100.10",
		},
		{
			name:         "comma candidates skip invalid and private values",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":  "not-an-ip, 10.0.0.8",
				"X-CDN-Second": "also-bad, 203.0.113.9",
			},
			want: "203.0.113.9",
		},
		{
			name:         "legacy public header wins over custom private fallback",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "1.2.3.4",
			},
			want: "1.2.3.4",
		},
		{
			name:         "custom private fallback retains configured precedence",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "192.168.1.4",
			},
			want: "10.0.0.8",
		},
		{
			name:         "invalid custom value continues to built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":         "1.2.3.4:443",
				"CF-Connecting-IP": "4.4.4.4",
			},
			want: "4.4.4.4",
		},
		{
			name:         "disabled mode ignores custom and legacy headers",
			trustForward: false,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "1.2.3.4",
				"X-Real-IP": "4.4.4.4",
			},
			want: "9.9.9.9",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettings(c, test.trustForward, test.headers)
				c.String(200, GetSecurityClientIP(c, !test.trustForward))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "9.9.9.9:12345"
			for name, value := range test.requestHeaders {
				req.Header.Set(name, value)
			}
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

func TestGetSecurityClientIPSwitchDisabledUsesConfiguredTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"9.9.9.9"}))
	r.GET("/t", func(c *gin.Context) { c.String(200, GetSecurityClientIP(c, false)) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetClientIPSwitchDisabledUsesTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetLegacyForwardedIPTrust(c, false)
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotCopiesCustomHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		headers := []string{"X-Original-IP"}
		SetForwardedIPSettings(c, true, headers)
		headers[0] = "X-Mutated-IP"
		c.String(200, GetSecurityClientIP(c, false))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Original-IP", "1.2.3.4")
	req.Header.Set("X-Mutated-IP", "4.4.4.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotOverridesLiveFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		requestTrust  bool
		fallbackTrust bool
		want          string
	}{
		{name: "captured secure mode wins", requestTrust: false, fallbackTrust: true, want: "9.9.9.9"},
		{name: "captured compatibility mode wins", requestTrust: true, fallbackTrust: false, want: "1.2.3.4"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetLegacyForwardedIPTrust(c, test.requestTrust)
				c.String(200, GetSecurityClientIP(c, test.fallbackTrust))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "9.9.9.9:12345"
			req.Header.Set("X-Real-IP", "1.2.3.4")
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}
