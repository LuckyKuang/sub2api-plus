package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/google/uuid"
)

// CNOAuthService owns browser login sessions; credentials never transit the panel.
type CNOAuthService struct {
	client      *cnoauth.Client
	store       *cnoauth.Store
	proxyRepo   ProxyRepository
	accountRepo AccountRepository
	refreshAPI  *OAuthRefreshAPI
	admin       AdminService
}

func NewCNOAuthService(proxyRepo ProxyRepository, accountRepo AccountRepository, admin AdminService) *CNOAuthService {
	return &CNOAuthService{client: &cnoauth.Client{}, store: cnoauth.NewStore(nil), proxyRepo: proxyRepo, accountRepo: accountRepo, admin: admin}
}

type CNOAuthView struct {
	SessionID    string    `json:"session_id,omitempty"`
	AuthorizeURL string    `json:"authorize_url,omitempty"`
	UserCode     string    `json:"user_code,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Interval     int       `json:"interval_seconds"`
	Status       string    `json:"status"`
	AccountID    int64     `json:"account_id,omitempty"`
}

func cnOAuthView(id string, s *cnoauth.Session) *CNOAuthView {
	status := "pending"
	if s.Grant != nil {
		status = "ready"
	}
	if s.CompletedID > 0 {
		status = "completed"
	}
	if s.Cancelled {
		status = "cancelled"
	}
	return &CNOAuthView{SessionID: id, AuthorizeURL: s.Flow.AuthorizeURL, UserCode: s.Flow.UserCode, ExpiresAt: s.Flow.ExpiresAt, Interval: s.Flow.Interval, Status: status, AccountID: s.CompletedID}
}
func (s *CNOAuthService) Start(ctx context.Context, owner int64, platform, region string, proxyID *int64, accountID int64) (*CNOAuthView, error) {
	if owner <= 0 {
		return nil, infraerrors.BadRequest("CN_OAUTH_OWNER_REQUIRED", "authenticated administrator required")
	}
	var account *Account
	if accountID > 0 {
		var err error
		account, err = s.accountRepo.GetByID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if account.Platform != platform || account.Type != AccountTypeOAuth {
			return nil, infraerrors.BadRequest("CN_OAUTH_ACCOUNT_INVALID", "account platform or authentication type does not match")
		}
		proxyID = account.ProxyID
	}
	proxyURL := ""
	if proxyID != nil {
		proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
		if err != nil {
			return nil, err
		}
		proxyURL = proxy.URL()
	}
	flowCtx := withNativeOAuthOutboundIdentity(ctx, platform)
	if account != nil {
		flowCtx = WithAccountOutboundIdentity(ctx, account)
	}
	flow, err := s.client.Start(flowCtx, platform, region, proxyURL)
	if err != nil {
		return nil, cnOAuthError(err)
	}
	if account != nil && platform == PlatformDeepseek {
		if id := account.GetCredential("oauth_device_id"); id != "" {
			flow.DeviceID = id
		}
	}
	identity, _ := outboundidentity.FromContext(flowCtx)
	session := &cnoauth.Session{Flow: flow, OwnerID: owner, AccountID: accountID, ProxyID: proxyID, ProxyURL: proxyURL, Identity: identity, NextPoll: time.Now().Add(time.Duration(flow.Interval) * time.Second)}
	id, err := s.store.Create(ctx, session)
	if err != nil {
		return nil, cnOAuthError(err)
	}
	return cnOAuthView(id, session), nil
}
func (s *CNOAuthService) Advance(ctx context.Context, owner int64, platform, id, callback string, cancel bool) (*CNOAuthView, error) {
	var view *CNOAuthView
	err := s.store.Update(ctx, id, func(ctx context.Context, session *cnoauth.Session) error {
		if session.OwnerID != owner || session.Flow.Platform != platform {
			return cnoauth.ErrSession
		}
		if session.Committing {
			return cnoauth.ErrBusy
		}
		if cancel {
			session.Cancelled = true
			session.Grant = nil
			session.Flow.Verifier = ""
			session.Flow.DeviceCode = ""
		}
		if session.Cancelled || session.Grant != nil || session.CompletedID > 0 {
			view = cnOAuthView(id, session)
			return nil
		}
		if callback == "" && time.Now().Before(session.NextPoll) {
			view = cnOAuthView(id, session)
			return nil
		}
		flowCtx := outboundidentity.WithIdentity(ctx, session.Identity)
		var grant *cnoauth.Grant
		var err error
		if callback != "" {
			grant, err = s.client.Exchange(flowCtx, session.Flow, callback, session.ProxyURL)
		} else {
			grant, err = s.client.Poll(flowCtx, session.Flow, session.ProxyURL)
		}
		switch {
		case errors.Is(err, cnoauth.ErrPending):
		case errors.Is(err, cnoauth.ErrSlowDown):
			session.Flow.Interval = min(60, session.Flow.Interval+5)
		case errors.Is(err, cnoauth.ErrDenied), errors.Is(err, cnoauth.ErrExpired), errors.Is(err, cnoauth.ErrInvalidGrant):
			session.Cancelled = true
		case err != nil:
			return err
		default:
			session.Grant = grant
		}
		session.NextPoll = time.Now().Add(time.Duration(session.Flow.Interval) * time.Second)
		view = cnOAuthView(id, session)
		return nil
	})
	if err != nil {
		return nil, cnOAuthError(err)
	}
	return view, nil
}

type CNOAuthCompleteInput struct {
	Name        string  `json:"name"`
	Concurrency int     `json:"concurrency"`
	Priority    int     `json:"priority"`
	GroupIDs    []int64 `json:"group_ids"`
}

func (s *CNOAuthService) Complete(ctx context.Context, owner int64, platform, id string, input CNOAuthCompleteInput) (*CNOAuthView, error) {
	// Claim the session durably before creating/updating an account. A process
	// crash after the DB write cannot replay that write; operators check the list
	// and restart login if the final receipt could not be saved.
	var claimed *cnoauth.Session
	var view *CNOAuthView
	err := s.store.Update(ctx, id, func(_ context.Context, session *cnoauth.Session) error {
		if session.OwnerID != owner || session.Flow.Platform != platform || session.Cancelled {
			return cnoauth.ErrSession
		}
		if session.CompletedID > 0 {
			view = cnOAuthView(id, session)
			return nil
		}
		if session.Committing {
			return cnoauth.ErrBusy
		}
		if session.Grant == nil {
			return cnoauth.ErrPending
		}
		if !session.Grant.ExpiresAt.IsZero() && time.Now().After(session.Grant.ExpiresAt) {
			return cnoauth.ErrExpired
		}
		session.Committing = true
		copy := *session
		claimed = &copy
		return nil
	})
	if err != nil {
		return nil, cnOAuthError(err)
	}
	if view != nil {
		return view, nil
	}
	session := claimed
	credentials := cnOAuthCredentials(session.Flow, session.Grant)
	runtimeHeaders := map[string]string{}
	for _, h := range declaredOutboundHeaders(session.Identity.Preset) {
		if h.Class == outboundHeaderRuntime {
			runtimeHeaders[h.Name] = session.Identity.Headers[h.Name]
		}
	}
	credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: session.Identity.Preset, UserAgent: session.Identity.UserAgent, Version: session.Identity.Version, Headers: runtimeHeaders}
	var account *Account
	if session.AccountID > 0 {
		existing, e := s.accountRepo.GetByID(ctx, session.AccountID)
		if e != nil {
			return nil, cnOAuthError(e)
		}
		if existing.Platform != platform || existing.Type != AccountTypeOAuth {
			return nil, cnOAuthError(cnoauth.ErrSession)
		}
		credentials = MergeCredentials(existing.Credentials, credentials)
		delete(credentials, "api_key")
		if platform == PlatformDeepseek {
			delete(credentials, "refresh_token")
			delete(credentials, "expires_at")
			delete(credentials, "expires_in")
		}
		account, err = s.admin.UpdateAccount(ctx, session.AccountID, &UpdateAccountInput{Credentials: credentials})
		if err == nil {
			account, err = s.admin.ClearAccountError(ctx, session.AccountID)
		}
	} else {
		name := strings.TrimSpace(input.Name)
		if name == "" {
			name = platform + " OAuth"
		}
		concurrency := input.Concurrency
		if concurrency <= 0 {
			concurrency = 3
		}
		account, err = s.admin.CreateAccount(ctx, &CreateAccountInput{Name: name, Platform: platform, Type: AccountTypeOAuth, Credentials: credentials, ProxyID: session.ProxyID, Concurrency: concurrency, Priority: input.Priority, GroupIDs: input.GroupIDs})
	}
	if err != nil {
		return nil, cnOAuthError(err)
	}
	err = s.store.Update(ctx, id, func(_ context.Context, current *cnoauth.Session) error {
		current.CompletedID = account.ID
		current.Committing = false
		current.Grant = nil
		current.Flow.Verifier = ""
		current.Flow.DeviceCode = ""
		view = cnOAuthView(id, current)
		return nil
	})
	if err != nil {
		return nil, cnOAuthError(err)
	}
	return view, nil
}

func cnOAuthCredentials(flow *cnoauth.Flow, grant *cnoauth.Grant) map[string]any {
	creds := map[string]any{"oauth_provider": flow.Platform, "oauth_region": flow.Region, "access_token": grant.AccessToken, "account_mode": AccountModeCoding, "api_protocol": APIProtocolAnthropic, "base_url": cnoauth.ModelBase(flow.Platform, flow.Region), "api_base_urls": map[string]any{APIProtocolAnthropic: cnoauth.ModelBase(flow.Platform, flow.Region)}}
	if flow.Platform == PlatformDeepseek {
		creds["oauth_device_id"] = flow.DeviceID
		creds["account_mode"] = AccountModePayG
		return creds
	}
	if grant.RefreshToken != "" {
		creds["refresh_token"] = grant.RefreshToken
	}
	if !grant.ExpiresAt.IsZero() {
		creds["expires_at"] = grant.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return creds
}
func cnOAuthError(err error) error {
	switch {
	case errors.Is(err, cnoauth.ErrSession), errors.Is(err, cnoauth.ErrExpired):
		return infraerrors.BadRequest("CN_OAUTH_SESSION_EXPIRED", "authorization expired; start again")
	case errors.Is(err, cnoauth.ErrBusy):
		return infraerrors.New(409, "CN_OAUTH_BUSY", "authorization is being processed; retry shortly")
	default:
		return infraerrors.New(400, "CN_OAUTH_FAILED", "authorization failed; verify the callback or restart login")
	}
}

// CNTokenRefresher uses the same distributed refresh coordinator as other providers.
type CNTokenRefresher struct {
	client   *cnoauth.Client
	platform string
}

func NewCNTokenRefresher(platform string) *CNTokenRefresher {
	return &CNTokenRefresher{client: &cnoauth.Client{}, platform: platform}
}
func (r *CNTokenRefresher) CacheKey(a *Account) string {
	return "cn-oauth:" + a.Platform + ":" + strconv.FormatInt(a.ID, 10)
}
func (r *CNTokenRefresher) CanRefresh(a *Account) bool {
	return a != nil && a.Platform == r.platform && a.IsDomesticOAuth() && (a.Platform == PlatformKimi || a.Platform == PlatformMiniMax) && strings.TrimSpace(a.GetCredential("refresh_token")) != ""
}
func (r *CNTokenRefresher) NeedsRefresh(a *Account, window time.Duration) bool {
	if !r.CanRefresh(a) {
		return false
	}
	expiry := a.GetCredentialAsTime("expires_at")
	return expiry == nil || time.Until(*expiry) < window
}
func (r *CNTokenRefresher) Refresh(ctx context.Context, a *Account) (map[string]any, error) {
	if !r.CanRefresh(a) {
		return nil, errors.New("reauthorization required")
	}
	ctx = WithAccountOutboundIdentity(ctx, a)
	grant, err := r.client.Refresh(ctx, a.Platform, a.GetCredential("oauth_region"), a.GetCredential("refresh_token"), a.proxyURLOrEmpty())
	if err != nil {
		return nil, err
	}
	updated := map[string]any{"access_token": grant.AccessToken, "refresh_token": grant.RefreshToken, "expires_at": grant.ExpiresAt.UTC().Format(time.RFC3339)}
	return MergeCredentials(a.Credentials, updated), nil
}

// RefreshAccount shares the background coordinator and never routes native
// credentials through the OpenAI/Claude OAuth clients.
func (s *CNOAuthService) RefreshAccount(ctx context.Context, a *Account, force bool) (*Account, error) {
	if !a.IsDomesticOAuth() || a.Platform == PlatformDeepseek {
		return nil, infraerrors.BadRequest("CN_OAUTH_REAUTHORIZE", "this account requires browser reauthorization")
	}
	window := 2 * time.Minute
	if force {
		window = 366 * 24 * time.Hour
	}
	result, err := s.refreshAPI.RefreshIfNeeded(ctx, a, &CNTokenRefresher{client: s.client, platform: a.Platform}, window)
	if err != nil {
		return nil, cnOAuthError(err)
	}
	if result.LockHeld {
		return nil, cnOAuthError(cnoauth.ErrBusy)
	}
	if force && !result.Refreshed {
		return nil, infraerrors.BadRequest("CN_OAUTH_REAUTHORIZE", "account cannot refresh in its current state; reauthorize in the editor")
	}
	if result.Account != nil {
		return result.Account, nil
	}
	return a, nil
}
func (s *CNOAuthService) prepareRequest(req *http.Request, a *Account) error {
	if !a.IsDomesticOAuth() {
		return nil
	}
	if _, err := cnoauth.NormalizeRegion(a.Platform, a.GetCredential("oauth_region")); err != nil {
		return errors.New("native OAuth account region is invalid")
	}
	expected, _ := url.Parse(cnoauth.ModelBase(a.Platform, a.GetCredential("oauth_region")))
	if req.URL.User != nil || req.URL.Fragment != "" || req.URL.Scheme != expected.Scheme || req.URL.Host != expected.Host || req.URL.Path != strings.TrimRight(expected.Path, "/")+"/v1/messages" {
		return errors.New("native OAuth destination is not supported")
	}
	ctx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(withOAuthRefreshRequestPath(req.Context())), a)
	*req = *req.WithContext(ctx)
	if a.Platform != PlatformDeepseek && !(&CNTokenRefresher{platform: a.Platform}).CanRefresh(a) {
		return errors.New("native OAuth refresh token is missing; reauthorize account")
	}
	if a.Platform != PlatformDeepseek && (&CNTokenRefresher{platform: a.Platform}).NeedsRefresh(a, 2*time.Minute) {
		fresh, err := s.RefreshAccount(ctx, a, false)
		if err != nil {
			return err
		}
		if !fresh.IsDomesticOAuth() || fresh.Platform != a.Platform || fresh.GetCredential("oauth_region") != a.GetCredential("oauth_region") || fresh.proxyURLOrEmpty() != a.proxyURLOrEmpty() {
			return errors.New("OAuth account changed; retry request")
		}
		a = fresh
	}
	if a.Platform == PlatformKimi {
		q := req.URL.Query()
		q.Set("beta", "true")
		req.URL.RawQuery = q.Encode()
	}
	if a.Platform == PlatformMiniMax {
		state, ok := req.Context().Value(cnOAuthTurnKey{}).(cnOAuthTurn)
		if !ok {
			state = cnOAuthTurn{Session: uuid.NewString(), Agent: uuid.NewString()}
			*req = *req.WithContext(context.WithValue(req.Context(), cnOAuthTurnKey{}, state))
		}
		req.Header.Set("X-Mavis-Session-Id", state.Session)
		req.Header.Set("X-Mavis-Agent-Id", state.Agent)
		req.Header.Set("X-Mavis-Timezone-Offset", "0")
	}
	if strings.TrimSpace(a.GetCredential("access_token")) == "" {
		return errors.New("native OAuth access token is missing")
	}
	setAnthropicAPIKeyAuthHeader(req.Header, a, a.GetCredential("access_token"), a.GetAnthropicProtocolBaseURL())
	return nil
}

type cnOAuthTurnKey struct{}
type cnOAuthTurn struct{ Session, Agent string }
