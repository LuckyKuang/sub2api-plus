package admin

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	servermiddleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
)

type IPAccessControlHandler struct {
	service *service.IPAccessControlService
	cfg     *config.Config
}

func NewIPAccessControlHandler(access *service.IPAccessControlService, cfg *config.Config) *IPAccessControlHandler {
	return &IPAccessControlHandler{service: access, cfg: cfg}
}

type trustedProxyStatusResponse struct {
	ConfigurationState           string   `json:"configuration_state"`
	TrustedProxies               []string `json:"trusted_proxies"`
	ClientIP                     string   `json:"client_ip"`
	DirectPeerIP                 string   `json:"direct_peer_ip"`
	DirectPeerTrusted            bool     `json:"direct_peer_trusted"`
	TrustedProxyApplied          bool     `json:"trusted_proxy_applied"`
	ForwardedHeaders             []string `json:"forwarded_headers"`
	IdentitySource               string   `json:"identity_source"`
	SafeForEnforcement           bool     `json:"safe_for_enforcement"`
	FailureReason                string   `json:"failure_reason,omitempty"`
	LegacyForwardedMode          bool     `json:"legacy_forwarded_mode"`
	EmergencyAllowlistConfigured bool     `json:"emergency_allowlist_configured"`
	EmergencyAllowlistCount      int      `json:"emergency_allowlist_count"`
	AutomaticBlockingReady       bool     `json:"automatic_blocking_ready"`
	ManualBlockingReady          bool     `json:"manual_blocking_ready"`
}

type ipAccessControlSettingsRequest struct {
	EnforcementEnabled     bool `json:"enforcement_enabled"`
	LoginFailureAutoBlock  bool `json:"login_failure_auto_block_enabled"`
	LoginFailureThreshold  int  `json:"login_failure_threshold"`
	LoginFailureWindowMins int  `json:"login_failure_window_minutes"`
	LoginFailureBlockMins  int  `json:"login_failure_block_minutes"`
}

type createIPAccessRuleRequest struct {
	IPOrCIDR  string  `json:"ip_or_cidr" binding:"required"`
	RuleKind  string  `json:"rule_kind" binding:"required"`
	Reason    string  `json:"reason"`
	ExpiresAt *string `json:"expires_at"`
}

type resetIPFailureRequest struct {
	IP string `json:"ip" binding:"required"`
}

func (h *IPAccessControlHandler) GetTrustedProxyStatus(c *gin.Context) {
	policy := h.trustedProxyPolicy()
	identity := h.identityResolver().Resolve(c)
	blockingReady := h.automaticBlockingCanBeEnabled(c)
	trustedProxies := policy.Values
	if trustedProxies == nil {
		// Keep the API contract stable for an unconfigured proxy policy. A nil
		// slice is encoded as JSON null, while the UI consumes this field as a
		// collection.
		trustedProxies = []string{}
	}

	forwardedHeaders := make([]string, 0, 3)
	for _, header := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
		if strings.TrimSpace(c.GetHeader(header)) != "" {
			forwardedHeaders = append(forwardedHeaders, header)
		}
	}

	emergencyConfigured, emergencyCount := false, 0
	if h.service != nil {
		emergencyConfigured, emergencyCount = h.service.EmergencyAllowlistStatus()
	}
	response.Success(c, trustedProxyStatusResponse{
		ConfigurationState:           string(policy.State),
		TrustedProxies:               trustedProxies,
		ClientIP:                     identity.EffectiveIP,
		DirectPeerIP:                 identity.DirectPeerIP,
		DirectPeerTrusted:            policy.DirectPeerTrusted(identity.DirectPeerIP),
		TrustedProxyApplied:          identity.Source == ip.ClientIdentitySourceTrustedForwarded,
		ForwardedHeaders:             forwardedHeaders,
		IdentitySource:               string(identity.Source),
		SafeForEnforcement:           identity.SafeForEnforcement,
		FailureReason:                identity.FailureReason,
		LegacyForwardedMode:          h.cfg != nil && h.cfg.ForwardedClientIPTrustEnabled(),
		EmergencyAllowlistConfigured: emergencyConfigured,
		EmergencyAllowlistCount:      emergencyCount,
		AutomaticBlockingReady:       blockingReady,
		ManualBlockingReady:          blockingReady,
	})
}

func (h *IPAccessControlHandler) trustedProxyPolicy() ip.TrustedProxyConfiguration {
	if h == nil || h.cfg == nil {
		return ip.InspectTrustedProxyConfiguration(false, nil)
	}
	return ip.InspectTrustedProxyConfiguration(h.cfg.Server.TrustedProxiesConfigured, h.cfg.Server.TrustedProxies)
}

func (h *IPAccessControlHandler) identityResolver() ip.ClientIdentityResolver {
	return ip.NewClientIdentityResolver(h.trustedProxyPolicy())
}

// automaticBlockingCanBeEnabled requires an explicit client-IP trust mode so
// an unnoticed reverse proxy cannot turn its shared transport peer into a
// global ban target. Direct deployments must opt in with trusted_proxies: [].
func (h *IPAccessControlHandler) automaticBlockingCanBeEnabled(c *gin.Context) bool {
	policy := h.trustedProxyPolicy()
	if policy.State == ip.TrustedProxyStateNotConfigured {
		return false
	}
	identity := ip.NewClientIdentityResolver(policy).Resolve(c)
	if !identity.SafeForEnforcement {
		return false
	}
	// An explicit direct deployment can safely ignore a spoofed forwarding
	// header at runtime, but requiring a clean settings request prevents an
	// operator from enabling automatic bans while an unconfigured proxy is in
	// front of the application.
	if identity.Source == ip.ClientIdentitySourceDirect {
		for _, header := range []string{"CF-Connecting-IP", "X-Forwarded-For", "X-Real-IP"} {
			if strings.TrimSpace(c.GetHeader(header)) != "" {
				return false
			}
		}
	}
	return true
}

func (h *IPAccessControlHandler) GetSettings(c *gin.Context) {
	settings, err := h.service.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *IPAccessControlHandler) UpdateSettings(c *gin.Context) {
	var req ipAccessControlSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if req.EnforcementEnabled && !h.automaticBlockingCanBeEnabled(c) {
		response.ErrorWithDetails(c, http.StatusConflict,
			"configure a safe client-IP proxy chain before enabling global IP enforcement",
			"IP_ACCESS_TRUSTED_PROXY_REQUIRED", nil)
		return
	}
	settings, err := h.service.UpdateSettings(c.Request.Context(), service.IPAccessControlSettings{
		EnforcementEnabled:     req.EnforcementEnabled,
		LoginFailureAutoBlock:  req.LoginFailureAutoBlock,
		LoginFailureThreshold:  req.LoginFailureThreshold,
		LoginFailureWindowMins: req.LoginFailureWindowMins,
		LoginFailureBlockMins:  req.LoginFailureBlockMins,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	servermiddleware.SetAuditExtra(c, map[string]any{
		"enabled":          settings.EnforcementEnabled,
		"blocking_enabled": settings.LoginFailureAutoBlock,
	})
	response.Success(c, settings)
}

func (h *IPAccessControlHandler) ListRules(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	list, err := h.service.ListRules(c.Request.Context(), service.IPAccessRuleFilter{
		Page: page, PageSize: pageSize,
		Status: service.IPAccessRuleStatus(strings.TrimSpace(c.Query("status"))),
		Query:  c.Query("query"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, list.Items, list.Total, list.Page, list.PageSize)
}

func (h *IPAccessControlHandler) ListFailureStates(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	list, err := h.service.ListFailureStates(c.Request.Context(), service.IPLoginFailureStateFilter{
		Page: page, PageSize: pageSize, Query: c.Query("query"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, list.Items, list.Total, list.Page, list.PageSize)
}

func (h *IPAccessControlHandler) CreateRule(c *gin.Context) {
	var req createIPAccessRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && strings.TrimSpace(*req.ExpiresAt) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.ExpiresAt))
		if err != nil {
			response.BadRequest(c, "expires_at must use RFC3339 format")
			return
		}
		expiresAt = &parsed
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, http.StatusUnauthorized, "Authorization required")
		return
	}
	if service.IPAccessRuleKind(req.RuleKind) == service.IPAccessRuleKindManualBlock && !h.automaticBlockingCanBeEnabled(c) {
		response.ErrorWithDetails(c, http.StatusConflict,
			"configure a safe client-IP proxy chain before creating a block rule",
			"IP_ACCESS_TRUSTED_PROXY_REQUIRED", nil)
		return
	}
	rule, err := h.service.AddManualRule(c.Request.Context(), req.IPOrCIDR, service.IPAccessRuleKind(req.RuleKind), req.Reason, expiresAt, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	servermiddleware.SetAuditExtra(c, map[string]any{"result": string(rule.RuleKind)})
	response.Created(c, rule)
}

func (h *IPAccessControlHandler) ReleaseRuleAndReset(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid rule id")
		return
	}
	subject, ok := servermiddleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, http.StatusUnauthorized, "Authorization required")
		return
	}
	rule, err := h.service.ReleaseRuleAndReset(c.Request.Context(), id, subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	result := "released_and_reset"
	if rule.RuleKind == service.IPAccessRuleKindAllow {
		result = "allow_rule_released"
	}
	servermiddleware.SetAuditExtra(c, map[string]any{"result": result})
	response.Success(c, rule)
}

func (h *IPAccessControlHandler) ResetFailureState(c *gin.Context) {
	var req resetIPFailureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.service.ResetFailureState(c.Request.Context(), req.IP); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	servermiddleware.SetAuditExtra(c, map[string]any{"result": "failure_counter_reset"})
	response.Success(c, gin.H{"ip": strings.TrimSpace(req.IP)})
}
