package service

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

// Credential keys and ZCode platform endpoints owned by this flow.
const (
	// credentialAPIBaseURLs is the per-protocol base URL map an adaptive account
	// resolves before falling back to a platform default. Adaptive is required for
	// the Z.ai estate and for the ZCode plan gateway, because neither matches the
	// compiled BigModel defaults.
	credentialAPIBaseURLs = "api_base_urls"

	// zcodePlanAnthropicBaseURL is the start-plan inference gateway. The native
	// Anthropic adapter appends /v1/messages, matching the official client's
	// model endpoint.
	zcodePlanAnthropicBaseURL = "https://zcode.z.ai/api/v1/zcode-plan/anthropic"

	// zcodeOffPeakAnthropicBaseURL is the off-peak inference gateway.
	zcodeOffPeakAnthropicBaseURL = "https://zcode.z.ai/api/v1/off-peak/anthropic"
)

// Zhipu / GLM account-link plan kinds. ZCode derives a different credential for
// each plan, so the admin must state which plan the linked account owns.
const (
	ZhipuPlanIndividualCodingPlan = "individual-coding-plan"
	ZhipuPlanTeamCodingPlan       = "team-coding-plan"
	ZhipuPlanStartPlan            = "start-plan"
	ZhipuPlanOffPeak              = "off-peak"
)

// zhipuPlanKinds lists the supported plan kinds in display order.
var zhipuPlanKinds = []string{
	ZhipuPlanIndividualCodingPlan,
	ZhipuPlanTeamCodingPlan,
	ZhipuPlanStartPlan,
	ZhipuPlanOffPeak,
}

// ZhipuPlanKindSupported reports whether the plan kind can be mapped onto an
// account. Off-peak is supported: its account material mirrors start-plan
// (the ZCode token as bearer) plus the derived coding-plan key, and the
// per-request admission ticket is supplied separately by the ticket manager.
func ZhipuPlanKindSupported(planKind string) bool {
	switch strings.TrimSpace(planKind) {
	case ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan, ZhipuPlanStartPlan, ZhipuPlanOffPeak:
		return true
	default:
		return false
	}
}

// ZhipuOAuthClient is the egress port for the ZCode handshake and the GLM
// business credential derivation. Failover, retries and identity resolution stay
// with the transport implementation.
type ZhipuOAuthClient interface {
	StartFlow(ctx context.Context, provider, pollToken, proxyURL string) (*zcode.FlowInit, error)
	PollFlow(ctx context.Context, provider, flowID, pollToken, proxyURL string) (*zcode.FlowPoll, error)
	ExchangeCode(ctx context.Context, provider, code, redirectURI, state, proxyURL string) (*zcode.FlowReady, error)
	ExchangeZaiBusinessToken(ctx context.Context, oauthAccessToken, proxyURL string) (string, error)
	ResolveIndividualCodingPlanKey(ctx context.Context, provider, accessToken, proxyURL string) (*zcode.CodingPlanCredential, error)
	ResolveTeamPlanKey(ctx context.Context, provider, accessToken, organizationID, projectID, proxyURL string) (*zcode.CodingPlanCredential, error)
}

// ZhipuOAuthService owns the ZCode platform account-link flow.
//
// The service is deliberately transport-agnostic: it resolves a trusted identity
// for every outbound call through the native Zhipu preset and then delegates the
// HTTP work to ZhipuOAuthClient.
type ZhipuOAuthService struct {
	client       ZhipuOAuthClient
	sessionStore *zcode.SessionStore
	proxyRepo    ProxyRepository
}

// NewZhipuOAuthService creates the service with a process-local session store.
func NewZhipuOAuthService(client ZhipuOAuthClient, proxyRepo ProxyRepository) *ZhipuOAuthService {
	return &ZhipuOAuthService{
		client:       client,
		sessionStore: zcode.NewSessionStore(),
		proxyRepo:    proxyRepo,
	}
}

// WithSessionStore replaces the session store, used to inject the Redis-backed
// store when a Redis client is configured.
func (s *ZhipuOAuthService) WithSessionStore(store *zcode.SessionStore) *ZhipuOAuthService {
	if s != nil && store != nil {
		s.sessionStore = store
	}
	return s
}

// Stop releases the session store's cleanup goroutine.
func (s *ZhipuOAuthService) Stop() {
	if s != nil && s.sessionStore != nil {
		s.sessionStore.Stop()
	}
}

// ZhipuOAuthCapabilities describes what this deployment can do.
type ZhipuOAuthCapabilities struct {
	Enabled   bool     `json:"enabled"`
	Providers []string `json:"providers"`
	// PlanKinds lists every plan kind the API accepts, and SupportedPlanKinds the
	// subset that can be mapped onto an account today.
	PlanKinds          []string `json:"plan_kinds"`
	SupportedPlanKinds []string `json:"supported_plan_kinds"`
	// HandshakeURL is the effective ZCode platform API root.
	HandshakeURL string `json:"handshake_url"`
}

// Capabilities reports the supported link surface.
func (s *ZhipuOAuthService) Capabilities() ZhipuOAuthCapabilities {
	supported := make([]string, 0, len(zhipuPlanKinds))
	for _, planKind := range zhipuPlanKinds {
		if ZhipuPlanKindSupported(planKind) {
			supported = append(supported, planKind)
		}
	}
	return ZhipuOAuthCapabilities{
		Enabled:            s != nil && s.client != nil,
		Providers:          append([]string{}, zcode.Providers...),
		PlanKinds:          append([]string{}, zhipuPlanKinds...),
		SupportedPlanKinds: supported,
		HandshakeURL:       zcode.ResolveHandshakeBaseURL(),
	}
}

// StartZhipuLinkInput starts a link or re-link flow.
type StartZhipuLinkInput struct {
	Provider  string
	ProxyID   *int64
	AccountID *int64
}

// ZhipuLinkSession is returned to the admin panel. The poll credential stays
// server-side; the panel only receives the opaque session handle.
type ZhipuLinkSession struct {
	SessionID       string `json:"session_id"`
	Provider        string `json:"provider"`
	AuthorizeURL    string `json:"authorize_url"`
	ExpiresAt       string `json:"expires_at"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

// StartLink opens a handshake flow. The authorization URL is opened by the
// operator in a browser; nothing about this flow requires an inbound callback,
// which is what makes it usable from a server-hosted deployment.
func (s *ZhipuOAuthService) StartLink(ctx context.Context, input StartZhipuLinkInput) (*ZhipuLinkSession, error) {
	if s == nil || s.client == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OAUTH_UNAVAILABLE", "zhipu oauth client is not configured")
	}
	provider := zcode.NormalizeProvider(input.Provider)
	if provider == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_PROVIDER_INVALID", "unsupported zhipu oauth provider")
	}
	pollToken, err := zcode.GeneratePollToken()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "ZHIPU_OAUTH_TOKEN_FAILED", "failed to generate poll token: %v", err)
	}
	sessionID, err := zcode.GenerateSessionID()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "ZHIPU_OAUTH_SESSION_FAILED", "failed to generate session id: %v", err)
	}
	proxyURL, err := s.resolveProxyURL(ctx, input.ProxyID)
	if err != nil {
		return nil, err
	}
	// The handshake is a pre-account authorization operation, so it advertises the
	// platform's native preset rather than any account snapshot.
	flowCtx := withNativeOAuthOutboundIdentity(ctx, PlatformZhipu)
	init, err := s.client.StartFlow(flowCtx, provider, pollToken, proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OAUTH_START_FAILED", "failed to start zhipu oauth flow: %v", err)
	}
	expiresAt := init.ExpiresAt
	if expiresAt.IsZero() || time.Until(expiresAt) > zcode.SessionTTL {
		expiresAt = time.Now().Add(zcode.SessionTTL)
	}
	session := &zcode.OAuthSession{
		State:           zcode.AuthorizeState(init.AuthorizeURL),
		Provider:        provider,
		PollToken:       pollToken,
		FlowID:          init.FlowID,
		AuthorizeURL:    init.AuthorizeURL,
		ExpiresAt:       expiresAt,
		PollIntervalSec: init.PollIntervalSec,
		ProxyURL:        proxyURL,
		AccountID:       input.AccountID,
		CreatedAt:       time.Now(),
	}
	s.sessionStore.Set(sessionID, session)
	return &ZhipuLinkSession{
		SessionID:       sessionID,
		Provider:        provider,
		AuthorizeURL:    init.AuthorizeURL,
		ExpiresAt:       expiresAt.UTC().Format(time.RFC3339),
		IntervalSeconds: init.PollIntervalSec,
	}, nil
}

// ZhipuLinkUser is the linked account identity reported to the panel.
type ZhipuLinkUser struct {
	ID     string `json:"user_id"`
	Name   string `json:"user_name,omitempty"`
	Email  string `json:"user_email,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// ZhipuLinkToken is the credential payload the panel echoes back when creating
// the account, mirroring the existing device-code contract where the panel
// performs the final create call.
type ZhipuLinkToken struct {
	Provider     string        `json:"provider"`
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token,omitempty"`
	ZCodeJWT     string        `json:"zcode_jwt_token,omitempty"`
	User         ZhipuLinkUser `json:"user"`
}

// ZhipuLinkPoll is one poll result.
type ZhipuLinkPoll struct {
	Pending bool            `json:"pending"`
	Ready   *ZhipuLinkToken `json:"ready,omitempty"`
}

// PollLink advances the handshake.
func (s *ZhipuOAuthService) PollLink(ctx context.Context, sessionID string) (*ZhipuLinkPoll, error) {
	session, err := s.loadSession(sessionID)
	if err != nil {
		return nil, err
	}
	flowCtx := withNativeOAuthOutboundIdentity(ctx, PlatformZhipu)
	poll, err := s.client.PollFlow(flowCtx, session.Provider, session.FlowID, session.PollToken, session.ProxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OAUTH_POLL_FAILED", "failed to poll zhipu oauth flow: %v", err)
	}
	switch poll.Status {
	case zcode.FlowStatusPending:
		return &ZhipuLinkPoll{Pending: true}, nil
	case zcode.FlowStatusFailed:
		// A failed flow is terminal: drop the session so the operator must start a
		// new authorization instead of polling a dead flow forever.
		s.sessionStore.Delete(sessionID)
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_FLOW_FAILED", "zhipu authorization failed or was denied")
	default:
		return &ZhipuLinkPoll{Pending: false, Ready: linkTokenFromReady(session.Provider, poll.Ready)}, nil
	}
}

// ExchangeZhipuLinkInput redeems a pasted authorization code. It is the fallback
// for deployments where the polling handshake is unavailable: the operator
// pastes either the bare code or the whole callback URL.
func (s *ZhipuOAuthService) ExchangeLink(ctx context.Context, sessionID, callback string, proxyID *int64) (*ZhipuLinkPoll, error) {
	session, err := s.loadSession(sessionID)
	if err != nil {
		return nil, err
	}
	code, state := zcode.ParseCallback(callback)
	if code == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_CODE_REQUIRED", "authorization code is required")
	}
	if state == "" {
		state = session.State
	}
	proxyURL := session.ProxyURL
	if proxyID != nil {
		resolved, err := s.resolveProxyURL(ctx, proxyID)
		if err != nil {
			return nil, err
		}
		proxyURL = resolved
	}
	redirectURI := zhipuDesktopRedirectURI()
	flowCtx := withNativeOAuthOutboundIdentity(ctx, PlatformZhipu)
	ready, err := s.client.ExchangeCode(flowCtx, session.Provider, code, redirectURI, state, proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OAUTH_EXCHANGE_FAILED", "failed to exchange zhipu authorization code: %v", err)
	}
	return &ZhipuLinkPoll{Pending: false, Ready: linkTokenFromReady(session.Provider, ready)}, nil
}

// ConsumeLinkSession consumes the session that authorized a create call, so one
// authorization cannot mint two accounts.
func (s *ZhipuOAuthService) ConsumeLinkSession(sessionID string) error {
	session, err := s.loadSession(sessionID)
	if err != nil {
		return err
	}
	if !session.TryConsume() {
		return infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_SESSION_CONSUMED", "this authorization was already used")
	}
	s.sessionStore.Delete(sessionID)
	return nil
}

// ZhipuAccountMaterialInput describes the plan an operator is linking.
type ZhipuAccountMaterialInput struct {
	Provider     string
	PlanKind     string
	TeamOrg      string
	TeamProject  string
	AccessToken  string
	RefreshToken string
	ZCodeJWT     string
	ProxyURL     string
	User         ZhipuLinkUser
	// HeaderOverrides carries the account's existing overrides so the team-plan
	// scope headers are added without discarding operator configuration.
	HeaderOverrides map[string]any
}

// ZhipuAccountMaterial is the account configuration for a linked plan.
type ZhipuAccountMaterial struct {
	Credentials map[string]any
	BaseURL     string
	PlanKind    string
}

// BuildAccountMaterial maps a linked plan onto the credential and endpoint shape
// the existing GLM data plane already understands. No gateway code changes are
// required for the supported plans: each one resolves to an API key, a base URL
// and (where the plan requires it) an authentication scheme or scope header.
func (s *ZhipuOAuthService) BuildAccountMaterial(ctx context.Context, input ZhipuAccountMaterialInput) (*ZhipuAccountMaterial, error) {
	if s == nil || s.client == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OAUTH_UNAVAILABLE", "zhipu oauth client is not configured")
	}
	provider := zcode.NormalizeProvider(input.Provider)
	if provider == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_PROVIDER_INVALID", "unsupported zhipu oauth provider")
	}
	planKind := strings.TrimSpace(input.PlanKind)
	if planKind == "" {
		planKind = ZhipuPlanIndividualCodingPlan
	}
	if !ZhipuPlanKindSupported(planKind) {
		return nil, infraerrors.Newf(http.StatusBadRequest, "ZHIPU_OAUTH_PLAN_UNSUPPORTED", "plan %q cannot be linked yet", planKind)
	}
	accessToken := strings.TrimSpace(input.AccessToken)
	if accessToken == "" && planKind != ZhipuPlanStartPlan {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_ACCESS_TOKEN_REQUIRED", "the linked token is required")
	}
	flowCtx := withNativeOAuthOutboundIdentity(ctx, PlatformZhipu)

	credentials := map[string]any{
		"account_mode":   AccountModeCoding,
		"plan_kind":      planKind,
		"oauth_provider": provider,
		// Every linked plan is served through explicit per-protocol endpoints, so
		// the account is adaptive and never falls back to a platform default.
		"api_protocol": APIProtocolAdaptive,
	}
	if id := strings.TrimSpace(input.User.ID); id != "" {
		credentials["zhipu_account_id"] = id
	}
	if name := strings.TrimSpace(input.User.Name); name != "" {
		credentials["oauth_account_name"] = name
	}
	if email := strings.TrimSpace(input.User.Email); email != "" {
		credentials["email"] = email
	}
	if refresh := strings.TrimSpace(input.RefreshToken); refresh != "" {
		// Refresh material is retained for diagnostics only: the official client
		// implements no refresh exchange, so nothing here rotates it.
		credentials["refresh_token"] = refresh
	}

	switch planKind {
	case ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan:
		businessToken, err := s.businessAccessToken(flowCtx, provider, accessToken, input.ProxyURL)
		if err != nil {
			return nil, err
		}
		var credential *zcode.CodingPlanCredential
		if planKind == ZhipuPlanTeamCodingPlan {
			organizationID := strings.TrimSpace(input.TeamOrg)
			projectID := strings.TrimSpace(input.TeamProject)
			if organizationID == "" || projectID == "" {
				return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_TEAM_SCOPE_REQUIRED", "a team plan requires its organization and project")
			}
			credential, err = s.client.ResolveTeamPlanKey(flowCtx, provider, businessToken, organizationID, projectID, input.ProxyURL)
		} else {
			credential, err = s.client.ResolveIndividualCodingPlanKey(flowCtx, provider, businessToken, input.ProxyURL)
		}
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OAUTH_KEY_RESOLUTION_FAILED", "failed to resolve the coding-plan api key: %v", err)
		}
		if credential == nil || strings.TrimSpace(credential.APIKey) == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "ZHIPU_OAUTH_KEY_RESOLUTION_FAILED", "the coding-plan api key could not be derived for this account")
		}
		credentials["api_key"] = credential.APIKey
		credentials["access_token"] = businessToken
		if credential.OrganizationID != "" {
			credentials["zhipu_organization"] = credential.OrganizationID
		}
		if credential.ProjectID != "" {
			credentials["zhipu_project"] = credential.ProjectID
		}
		credentials[credentialAPIBaseURLs] = map[string]any{
			APIProtocolChatCompletions: zhipuCodingBaseURL(provider),
			APIProtocolAnthropic:       zhipuAnthropicBaseURL(provider),
		}
		if planKind == ZhipuPlanTeamCodingPlan {
			// Team-plan traffic authenticates with the project key but is scoped by
			// the organization/project headers, which is exactly what the account
			// header-override map owns.
			credentials[credKeyHeaderOverrides] = mergeTeamScopeOverrides(input.HeaderOverrides, credential)
			credentials[credKeyHeaderOverrideEnabled] = true
		}
		if jwt := strings.TrimSpace(input.ZCodeJWT); jwt != "" {
			credentials["zcode_jwt_token"] = jwt
		}
		return &ZhipuAccountMaterial{
			Credentials: credentials,
			BaseURL:     zhipuCodingBaseURL(provider),
			PlanKind:    planKind,
		}, nil

	case ZhipuPlanStartPlan:
		jwt := strings.TrimSpace(input.ZCodeJWT)
		if jwt == "" {
			return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_JWT_REQUIRED", "a start-plan account requires the zcode token")
		}
		// The start plan authenticates model traffic with the ZCode token as a
		// bearer credential against the plan's Anthropic endpoint.
		credentials["api_key"] = jwt
		credentials["zcode_jwt_token"] = jwt
		credentials[credentialAPIBaseURLs] = map[string]any{APIProtocolAnthropic: zhipuPlanAnthropicBaseURL()}
		// The plan gateway authenticates with `Authorization: Bearer <zcode
		// token>`; the account-level scheme switches the Anthropic adapter away
		// from the default x-api-key header.
		credentials[anthropicAPIKeyAuthSchemeExtraKey] = AnthropicAPIKeyAuthSchemeAuthorizationBearer
		return &ZhipuAccountMaterial{
			Credentials: credentials,
			BaseURL:     zhipuPlanAnthropicBaseURL(),
			PlanKind:    planKind,
		}, nil

	case ZhipuPlanOffPeak:
		jwt := strings.TrimSpace(input.ZCodeJWT)
		if jwt == "" {
			return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_JWT_REQUIRED", "an off-peak account requires the zcode token")
		}
		businessToken, err := s.businessAccessToken(flowCtx, provider, accessToken, input.ProxyURL)
		if err != nil {
			return nil, err
		}
		organizationID, projectID := strings.TrimSpace(input.TeamOrg), strings.TrimSpace(input.TeamProject)
		teamPlan := organizationID != "" || projectID != ""
		if teamPlan && (organizationID == "" || projectID == "") {
			return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_TEAM_SCOPE_REQUIRED", "a team plan requires its organization and project")
		}
		// The ticket server settles the idle queue against a coding-plan key, so an
		// off-peak account owns both the plan key and the ZCode token.
		var credential *zcode.CodingPlanCredential
		if teamPlan {
			credential, err = s.client.ResolveTeamPlanKey(flowCtx, provider, businessToken, organizationID, projectID, input.ProxyURL)
		} else {
			credential, err = s.client.ResolveIndividualCodingPlanKey(flowCtx, provider, businessToken, input.ProxyURL)
		}
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OAUTH_KEY_RESOLUTION_FAILED", "failed to resolve the coding-plan api key: %v", err)
		}
		if credential == nil || strings.TrimSpace(credential.APIKey) == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "ZHIPU_OAUTH_KEY_RESOLUTION_FAILED", "the coding-plan api key could not be derived for this account")
		}
		credentials["api_key"] = jwt
		credentials["zcode_jwt_token"] = jwt
		credentials["access_token"] = businessToken
		credentials["off_peak_plan_key"] = credential.APIKey
		if credential.OrganizationID != "" {
			credentials["zhipu_organization"] = credential.OrganizationID
		}
		if credential.ProjectID != "" {
			credentials["zhipu_project"] = credential.ProjectID
		}
		credentials[credentialAPIBaseURLs] = map[string]any{APIProtocolAnthropic: zcodeOffPeakAnthropicBaseURL}
		credentials[anthropicAPIKeyAuthSchemeExtraKey] = AnthropicAPIKeyAuthSchemeAuthorizationBearer
		// The plan key and the team scope are static request declarations, so they
		// live in the account header overrides; only the admission ticket id is
		// request-scoped and is injected by the ticket manager.
		offPeakHeaders := map[string]string{"x-coding-plan-api-key": credential.APIKey}
		if teamPlan {
			offPeakHeaders["bigmodel-organization"] = credential.OrganizationID
			offPeakHeaders["bigmodel-project"] = credential.ProjectID
		}
		credentials[credKeyHeaderOverrides] = mergeHeaderOverrides(input.HeaderOverrides, offPeakHeaders)
		credentials[credKeyHeaderOverrideEnabled] = true
		return &ZhipuAccountMaterial{
			Credentials: credentials,
			BaseURL:     zcodeOffPeakAnthropicBaseURL,
			PlanKind:    planKind,
		}, nil
	}
	return nil, infraerrors.Newf(http.StatusBadRequest, "ZHIPU_OAUTH_PLAN_UNSUPPORTED", "plan %q cannot be linked yet", planKind)
}

// businessAccessToken normalizes the handshake token into the business token the
// GLM business APIs expect. Z.ai issues an OAuth token that must be exchanged;
// BigModel already returns a business token.
func (s *ZhipuOAuthService) businessAccessToken(ctx context.Context, provider, accessToken, proxyURL string) (string, error) {
	if provider != zcode.ProviderZai {
		return accessToken, nil
	}
	businessToken, err := s.client.ExchangeZaiBusinessToken(ctx, accessToken, proxyURL)
	if err != nil {
		return "", infraerrors.Newf(http.StatusBadGateway, "ZHIPU_OAUTH_BUSINESS_TOKEN_FAILED", "failed to exchange the zai business token: %v", err)
	}
	return businessToken, nil
}

func (s *ZhipuOAuthService) loadSession(sessionID string) (*zcode.OAuthSession, error) {
	if s == nil || s.client == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "ZHIPU_OAUTH_UNAVAILABLE", "zhipu oauth client is not configured")
	}
	session, ok := s.sessionStore.Get(strings.TrimSpace(sessionID))
	if !ok || session == nil {
		return nil, infraerrors.New(http.StatusBadRequest, "ZHIPU_OAUTH_SESSION_INVALID", "the link session is missing or expired")
	}
	return session, nil
}

// ResolveProxyURL reports the proxy URL an egress call should use. The handler
// resolves it once so the account material and the create call agree.
func (s *ZhipuOAuthService) ResolveProxyURL(ctx context.Context, proxyID *int64) string {
	proxyURL, err := s.resolveProxyURL(ctx, proxyID)
	if err != nil {
		return ""
	}
	return proxyURL
}

func (s *ZhipuOAuthService) resolveProxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil || s.proxyRepo == nil {
		return "", nil
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		return "", infraerrors.Newf(http.StatusBadRequest, "ZHIPU_OAUTH_PROXY_NOT_FOUND", "proxy not found: %v", err)
	}
	if proxy == nil {
		return "", nil
	}
	return proxy.URL(), nil
}

func linkTokenFromReady(provider string, ready *zcode.FlowReady) *ZhipuLinkToken {
	if ready == nil {
		return nil
	}
	return &ZhipuLinkToken{
		Provider:     provider,
		AccessToken:  ready.Provider.AccessToken,
		RefreshToken: ready.Provider.RefreshToken,
		ZCodeJWT:     ready.ZCodeJWT,
		User: ZhipuLinkUser{
			ID:     ready.User.ID,
			Name:   ready.User.Name,
			Email:  ready.User.Email,
			Avatar: ready.User.Avatar,
		},
	}
}

// zhipuCodingBaseURL returns the coding-plan inference root for the estate.
func zhipuCodingBaseURL(provider string) string {
	if zcode.NormalizeProvider(provider) == zcode.ProviderZai {
		return DefaultZaiCodingBaseURL
	}
	return DefaultZhipuCodingBaseURL
}

// zhipuAnthropicBaseURL returns the Anthropic-protocol inference root.
func zhipuAnthropicBaseURL(provider string) string {
	if zcode.NormalizeProvider(provider) == zcode.ProviderZai {
		return DefaultZaiAnthropicBaseURL
	}
	return DefaultZhipuAnthropicBaseURL
}

// zhipuPlanAnthropicBaseURL is the ZCode platform plan gateway. nativeAnthropic
// appends /v1/messages, matching the official client's model endpoint.
func zhipuPlanAnthropicBaseURL() string {
	return zcodePlanAnthropicBaseURL
}

// zhipuDesktopRedirectURI mirrors the official client's account-link redirect.
func zhipuDesktopRedirectURI() string {
	return zcode.DesktopRedirectURI(zcodeHandshakeOrigin(), zcode.DefaultVersion)
}

// zcodeHandshakeOrigin derives the ZCode website origin from the handshake root.
// The account-link redirect lives on the website origin, not on the API path.
func zcodeHandshakeOrigin() string {
	parsed, err := url.Parse(zcode.ResolveHandshakeBaseURL())
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

func mergeTeamScopeOverrides(existing map[string]any, credential *zcode.CodingPlanCredential) map[string]any {
	return mergeHeaderOverrides(existing, map[string]string{
		"bigmodel-organization": credential.OrganizationID,
		"bigmodel-project":      credential.ProjectID,
	})
}

// mergeHeaderOverrides folds plan-owned declarations into the account's existing
// header overrides so operator configuration is preserved. The override store is
// keyed by lower-case name.
func mergeHeaderOverrides(existing map[string]any, additions map[string]string) map[string]any {
	merged := map[string]any{}
	maps.Copy(merged, existing)
	for name, value := range additions {
		if strings.TrimSpace(value) == "" {
			continue
		}
		merged[strings.ToLower(name)] = value
	}
	return merged
}
