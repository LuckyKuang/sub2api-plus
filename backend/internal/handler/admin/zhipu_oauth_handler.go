package admin

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LuckyKuang/sub2api-plus/internal/handler/dto"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// ZhipuOAuthHandler exposes the ZCode platform account-link flow for Zhipu / GLM
// accounts.
//
// The flow is deliberately callback-free: the server opens a handshake session,
// the operator authorizes in a browser, and the server polls the platform until
// the authorization completes. That is what makes it usable from a
// server-hosted deployment, where a client-side redirect back into the app is
// impossible. A paste-the-callback-URL exchange is retained as a fallback.
type ZhipuOAuthHandler struct {
	zhipuOAuthService *service.ZhipuOAuthService
	adminService      service.AdminService
}

// NewZhipuOAuthHandler creates the handler.
func NewZhipuOAuthHandler(zhipuOAuthService *service.ZhipuOAuthService, adminService service.AdminService) *ZhipuOAuthHandler {
	return &ZhipuOAuthHandler{
		zhipuOAuthService: zhipuOAuthService,
		adminService:      adminService,
	}
}

// GetCapabilities reports the supported provider and plan surface.
// GET /api/v1/admin/zhipu/oauth/capabilities
func (h *ZhipuOAuthHandler) GetCapabilities(c *gin.Context) {
	response.Success(c, h.zhipuOAuthService.Capabilities())
}

// ZhipuStartLinkRequest starts a link flow.
type ZhipuStartLinkRequest struct {
	Provider string `json:"provider"`
	ProxyID  *int64 `json:"proxy_id"`
}

// StartLink opens a handshake session and returns the authorization URL.
// POST /api/v1/admin/zhipu/oauth/start
func (h *ZhipuOAuthHandler) StartLink(c *gin.Context) {
	var req ZhipuStartLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// An empty body selects the default provider.
		req = ZhipuStartLinkRequest{}
	}
	result, err := h.zhipuOAuthService.StartLink(c.Request.Context(), service.StartZhipuLinkInput{
		Provider: req.Provider,
		ProxyID:  req.ProxyID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ZhipuPollLinkRequest advances an existing session.
type ZhipuPollLinkRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// PollLink reports whether the authorization completed.
// POST /api/v1/admin/zhipu/oauth/poll
func (h *ZhipuOAuthHandler) PollLink(c *gin.Context) {
	var req ZhipuPollLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.zhipuOAuthService.PollLink(c.Request.Context(), req.SessionID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ZhipuExchangeLinkRequest redeems a pasted authorization code or callback URL.
type ZhipuExchangeLinkRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	// Callback accepts either the whole callback URL or the bare code.
	Callback string `json:"callback" binding:"required"`
	ProxyID  *int64 `json:"proxy_id"`
}

// ExchangeLink completes a session from a pasted callback.
// POST /api/v1/admin/zhipu/oauth/exchange-code
func (h *ZhipuOAuthHandler) ExchangeLink(c *gin.Context) {
	var req ZhipuExchangeLinkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.zhipuOAuthService.ExchangeLink(c.Request.Context(), req.SessionID, req.Callback, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ZhipuCreateAccountRequest creates the GLM account for a completed link.
type ZhipuCreateAccountRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	// PlanKind selects the linked subscription; empty selects the personal
	// coding plan.
	PlanKind    string `json:"plan_kind"`
	TeamOrg     string `json:"team_organization"`
	TeamProject string `json:"team_project"`

	// Provider and the token material are the values the panel received from the
	// poll or exchange call. They are echoed back so the create step never has to
	// re-run a single-use authorization.
	Provider     string `json:"provider" binding:"required"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ZCodeJWT     string `json:"zcode_jwt_token"`

	Name        string  `json:"name"`
	Concurrency int     `json:"concurrency"`
	Priority    int     `json:"priority"`
	ProxyID     *int64  `json:"proxy_id"`
	GroupIDs    []int64 `json:"group_ids"`
}

// CreateAccountFromLink resolves the plan credential and creates the account.
// POST /api/v1/admin/zhipu/oauth/create-from-oauth
func (h *ZhipuOAuthHandler) CreateAccountFromLink(c *gin.Context) {
	var req ZhipuCreateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	// The session is consumed only after the material resolves, so a rejected
	// plan can be retried with the same authorization.
	proxyURL := ""
	if req.ProxyID != nil {
		proxyURL = h.zhipuOAuthService.ResolveProxyURL(c.Request.Context(), req.ProxyID)
	}
	material, err := h.zhipuOAuthService.BuildAccountMaterial(c.Request.Context(), service.ZhipuAccountMaterialInput{
		SessionID:    req.SessionID,
		Provider:     req.Provider,
		PlanKind:     req.PlanKind,
		TeamOrg:      req.TeamOrg,
		TeamProject:  req.TeamProject,
		AccessToken:  req.AccessToken,
		RefreshToken: req.RefreshToken,
		ZCodeJWT:     req.ZCodeJWT,
		ProxyURL:     proxyURL,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := h.zhipuOAuthService.ConsumeLinkSession(req.SessionID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = zhipuDefaultAccountName(req.Provider, material.PlanKind)
	}
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:        name,
		Platform:    service.PlatformZhipu,
		Type:        service.AccountTypeOAuth,
		Credentials: material.Credentials,
		ProxyID:     req.ProxyID,
		Concurrency: req.Concurrency,
		Priority:    req.Priority,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

func zhipuDefaultAccountName(provider, planKind string) string {
	estate := "BigModel"
	if zcode.NormalizeProvider(provider) == zcode.ProviderZai {
		estate = "Z.ai"
	}
	plan := strings.TrimSpace(planKind)
	if plan == "" {
		plan = service.ZhipuPlanIndividualCodingPlan
	}
	return "GLM " + estate + " " + plan
}
