package middleware

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
)

const ipBannedMessage = "Access from this IP address has been prohibited."
const ipAccessUnavailableMessage = "IP access control is temporarily unavailable."

// IPAccessControlMiddleware is intentionally placed before all authentication
// and gateway handlers. It always uses Gin's trusted-proxy chain instead of
// the legacy raw-forwarded-header compatibility switch, so a direct client
// cannot spoof CF-Connecting-IP/X-Forwarded-For to evade or poison a ban.
type IPAccessControlMiddleware gin.HandlerFunc

// ProvideIPAccessControlMiddleware is the production Wire provider. It makes
// startup fail closed if the first complete security snapshot cannot be read.
func ProvideIPAccessControlMiddleware(access *service.IPAccessControlService, cfg *config.Config) (IPAccessControlMiddleware, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := access.Warmup(ctx); err != nil {
		return nil, fmt.Errorf("warm up IP access control security snapshot: %w", err)
	}
	policy := ip.InspectTrustedProxyConfiguration(false, nil)
	if cfg != nil {
		policy = ip.InspectTrustedProxyConfiguration(cfg.Server.TrustedProxiesConfigured, cfg.Server.TrustedProxies)
		if err := access.ConfigureEmergencyAllowlist(cfg.Server.IPAccessEmergencyAllowlist); err != nil {
			return nil, fmt.Errorf("configure IP access emergency allowlist: %w", err)
		}
	}
	return NewIPAccessControlMiddlewareWithResolver(access, ip.NewClientIdentityResolver(policy)), nil
}

func NewIPAccessControlMiddleware(access *service.IPAccessControlService) IPAccessControlMiddleware {
	return NewIPAccessControlMiddlewareWithResolver(access, ip.NewClientIdentityResolver(ip.InspectTrustedProxyConfiguration(false, nil)))
}

func NewIPAccessControlMiddlewareWithResolver(access *service.IPAccessControlService, resolver ip.ClientIdentityResolver) IPAccessControlMiddleware {
	return IPAccessControlMiddleware(func(c *gin.Context) {
		// Keep container and load-balancer health probes independent from access
		// rules; otherwise an accidental rule can turn a healthy deployment down.
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		if access == nil {
			writeIPAccessUnavailable(c)
			return
		}

		identity := resolver.Resolve(c)
		ip.SetClientIdentity(c, identity)
		decision, err := access.Evaluate(c.Request.Context(), identity)
		if err != nil {
			writeIPAccessUnavailable(c)
			return
		}
		if decision.Allowed {
			c.Next()
			return
		}
		if !decision.Blocked {
			writeIPAccessUnavailable(c)
			return
		}

		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		path := c.Request.URL.Path
		if isGoogleGatewayPath(path) {
			GoogleErrorWriter(c, http.StatusForbidden, ipBannedMessage)
			c.Abort()
			return
		}
		if isAnthropicGatewayPath(path) {
			AnthropicErrorWriter(c, http.StatusForbidden, ipBannedMessage)
			c.Abort()
			return
		}
		if isOpenAIGatewayPath(path) {
			c.JSON(http.StatusForbidden, gin.H{
				"error": gin.H{
					"message": ipBannedMessage,
					"type":    "access_denied",
					"param":   nil,
					"code":    "IP_BANNED",
				},
			})
			c.Abort()
			return
		}
		if path == "/api" || strings.HasPrefix(path, "/api/") {
			AbortWithError(c, http.StatusForbidden, "IP_BANNED", ipBannedMessage)
			return
		}
		if acceptsHTMLNavigation(c) {
			c.Header("Content-Type", "text/html; charset=utf-8")
			c.Status(http.StatusForbidden)
			_, _ = c.Writer.WriteString(ipBannedPage())
			c.Abort()
			return
		}
		AbortWithError(c, http.StatusForbidden, "IP_BANNED", ipBannedMessage)
	})
}

func writeIPAccessUnavailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Retry-After", "5")
	path := c.Request.URL.Path
	if isGoogleGatewayPath(path) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"code":    http.StatusServiceUnavailable,
				"message": ipAccessUnavailableMessage,
				"status":  "UNAVAILABLE",
			},
		})
		c.Abort()
		return
	}
	if isAnthropicGatewayPath(path) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": ipAccessUnavailableMessage,
			},
		})
		c.Abort()
		return
	}
	if isOpenAIGatewayPath(path) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"message": ipAccessUnavailableMessage,
				"type":    "server_error",
				"param":   nil,
				"code":    "IP_ACCESS_CONTROL_UNAVAILABLE",
			},
		})
		c.Abort()
		return
	}
	AbortWithError(c, http.StatusServiceUnavailable, "IP_ACCESS_CONTROL_UNAVAILABLE", ipAccessUnavailableMessage)
}

func hasIPAccessPathPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func isGoogleGatewayPath(path string) bool {
	return hasIPAccessPathPrefix(path, "/v1beta") ||
		hasIPAccessPathPrefix(path, "/antigravity/v1beta")
}

func isAnthropicGatewayPath(path string) bool {
	return hasIPAccessPathPrefix(path, "/v1/messages") ||
		hasIPAccessPathPrefix(path, "/messages/count_tokens") ||
		hasIPAccessPathPrefix(path, "/antigravity/v1") ||
		hasIPAccessPathPrefix(path, "/antigravity/models")
}

func isOpenAIGatewayPath(path string) bool {
	for _, prefix := range []string{
		"/v1",
		"/responses",
		"/models",
		"/chat",
		"/embeddings",
		"/images",
		"/videos",
		"/alpha",
		"/backend-api/codex",
	} {
		if hasIPAccessPathPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func TrustedClientIP(c *gin.Context) string {
	if identity, ok := ip.ClientIdentityFromContext(c); ok {
		return identity.EffectiveIP
	}
	return ip.GetTrustedClientIP(c)
}

// TrustedClientIdentity returns the request identity established by the global
// policy middleware. The fallback is suitable only for focused tests that do
// not install the production router middleware.
func TrustedClientIdentity(c *gin.Context) ip.ClientIdentity {
	if identity, ok := ip.ClientIdentityFromContext(c); ok {
		return identity
	}
	clientIP := ip.GetTrustedClientIP(c)
	return ip.ClientIdentity{EffectiveIP: clientIP, DirectPeerIP: clientIP, Source: ip.ClientIdentitySourceDirect, SafeForEnforcement: clientIP != ""}
}

func acceptsHTMLNavigation(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	return strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/html")
}

func ipBannedPage() string {
	// Inline and dependency-free so it remains comprehensible even when all SPA
	// assets are blocked by the same global rule.
	message := html.EscapeString(ipBannedMessage)
	return "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Access denied</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f8fafc;color:#172033;font-family:-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif}.box{max-width:30rem;padding:2rem;text-align:center}.code{font-size:4rem;font-weight:700;color:#b42318;margin:0}.title{font-size:1.4rem;font-weight:650;margin:1rem 0 .5rem}.detail{margin:0;color:#536174;line-height:1.6}</style></head><body><main class=\"box\"><p class=\"code\">403</p><h1 class=\"title\">Access denied</h1><p class=\"detail\">" + message + "</p></main></body></html>"
}
