package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/deepseek"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/google/uuid"
)

// CNOAuthService owns browser login sessions and never echoes granted credentials.
type CNOAuthService struct {
	client      *cnoauth.Client
	store       *cnoauth.Store
	proxyRepo   ProxyRepository
	accountRepo AccountRepository
	refreshAPI  *OAuthRefreshAPI
	admin       AdminService
	modelTester *AccountTestService
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
		if s.accountRepo == nil {
			return nil, infraerrors.BadRequest("CN_OAUTH_ACCOUNT_INVALID", "account is unavailable")
		}
		var err error
		account, err = s.accountRepo.GetByID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		if account == nil || account.Platform != platform || account.Type != AccountTypeOAuth {
			return nil, infraerrors.BadRequest("CN_OAUTH_ACCOUNT_INVALID", "account platform or authentication type does not match")
		}
		account, err = snapshotCNControlAccount(account)
		if err != nil {
			return nil, cnOAuthError(err)
		}
		proxyID = account.ProxyID
		if platform == PlatformStepFun {
			region = account.GetCredential("oauth_region")
		}
	}
	proxyURL := ""
	if account != nil {
		var err error
		proxyURL, err = requestCredentialOwnerProxyURL(context.Background(), account)
		if err != nil {
			return nil, infraerrors.BadRequest("CN_OAUTH_PROXY_UNAVAILABLE", "configured account proxy is unavailable")
		}
	} else if proxyID != nil {
		if *proxyID <= 0 || s.proxyRepo == nil {
			return nil, infraerrors.BadRequest("CN_OAUTH_PROXY_UNAVAILABLE", "selected proxy is unavailable")
		}
		proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
		if err != nil || proxy == nil {
			return nil, infraerrors.BadRequest("CN_OAUTH_PROXY_UNAVAILABLE", "selected proxy is unavailable")
		}
		proxyURL, err = requestCredentialOwnerProxyURL(context.Background(), &Account{ProxyID: proxyID, Proxy: proxy})
		if err != nil {
			return nil, infraerrors.BadRequest("CN_OAUTH_PROXY_INVALID", "selected proxy configuration is invalid")
		}
		selectedID := *proxyID
		proxyID = &selectedID
	}
	var loginOwner json.RawMessage
	if account != nil {
		var err error
		loginOwner, err = json.Marshal(cnOAuthLoginOwnerFromAccount(account))
		if err != nil {
			return nil, cnOAuthError(err)
		}
	}
	flowCtx := withNativeOAuthOutboundIdentity(ctx, platform)
	if account != nil {
		flowCtx = WithAccountOutboundIdentity(ctx, account)
	}
	if platform == PlatformDeepseek {
		identity, _ := outboundidentity.FromContext(flowCtx)
		flowCtx = outboundidentity.WithIdentity(flowCtx, deepseek.CaptureIdentity(identity, time.Now()))
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
	session := &cnoauth.Session{Flow: flow, OwnerID: owner, AccountID: accountID, ProxyID: proxyID, ProxyURL: proxyURL, LoginOwner: loginOwner, Identity: identity, NextPoll: time.Now().Add(time.Duration(flow.Interval) * time.Second)}
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
			session.LoginOwner = nil
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
			session.LoginOwner = nil
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
	Name         string            `json:"name"`
	Concurrency  int               `json:"concurrency"`
	Priority     int               `json:"priority"`
	GroupIDs     []int64           `json:"group_ids"`
	ModelMapping map[string]string `json:"model_mapping"`
}

// PreviewModels uses the server-held grant without creating an account or
// exposing credentials. Only StepFun currently publishes this login-time catalog.
func (s *CNOAuthService) PreviewModels(ctx context.Context, owner int64, platform, id string) ([]string, error) {
	var account *Account
	validate := func(_ context.Context, session *cnoauth.Session) error {
		if owner <= 0 || platform != PlatformStepFun || session.OwnerID != owner || session.Flow.Platform != platform || session.Cancelled || session.CompletedID > 0 || session.Grant == nil {
			return cnoauth.ErrSession
		}
		if session.Committing {
			return cnoauth.ErrBusy
		}
		if !session.Grant.ExpiresAt.IsZero() && time.Now().After(session.Grant.ExpiresAt) {
			return cnoauth.ErrExpired
		}
		credentials := cnOAuthCredentials(session.Flow, session.Grant)
		credentials[outboundIdentityCredential] = selectionFromIdentity(session.Identity)
		proxy, err := cnOAuthSessionProxy(session.ProxyID, session.ProxyURL)
		if err != nil {
			return err
		}
		account = &Account{Platform: platform, Type: AccountTypeOAuth, Credentials: credentials, ProxyID: session.ProxyID, Proxy: proxy}
		return nil
	}
	if err := s.store.Update(ctx, id, validate); err != nil {
		return nil, cnOAuthError(err)
	}
	models, err := s.modelTester.FetchUpstreamSupportedModels(ctx, account)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadGateway, "CN_OAUTH_MODELS_FAILED", "failed to fetch authorized models; retry or enter model IDs manually")
	}
	// A cancelled, expired or consumed session cannot return a late result.
	if err := s.store.Update(ctx, id, validate); err != nil {
		return nil, cnOAuthError(err)
	}
	return models, nil
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
	credentials[outboundIdentityCredential] = selectionFromIdentity(session.Identity)
	var account *Account
	if session.AccountID > 0 {
		var savedOwner cnOAuthLoginOwner
		if len(session.LoginOwner) == 0 || json.Unmarshal(session.LoginOwner, &savedOwner) != nil {
			return nil, cnOAuthError(cnoauth.ErrSession)
		}
		expected := savedOwner.account()
		if expected.ID != session.AccountID || expected.Platform != platform || expected.Type != AccountTypeOAuth {
			return nil, cnOAuthError(cnoauth.ErrSession)
		}
		writer, ok := s.accountRepo.(CNOAuthLoginSnapshotRepository)
		if !ok {
			return nil, cnOAuthError(errors.New("atomic native OAuth login persistence is unavailable"))
		}
		existing, e := s.accountRepo.GetByID(ctx, session.AccountID)
		if e != nil {
			return nil, cnOAuthError(e)
		}
		expectedOwner, expectedErr := cnControlOwnerFingerprint(expected)
		currentOwner, currentErr := cnControlOwnerFingerprint(existing)
		if expectedErr != nil || currentErr != nil || expectedOwner != currentOwner {
			return nil, cnOAuthError(cnoauth.ErrSession)
		}
		previousCredentials := existing.Credentials
		if platform == PlatformStepFun {
			previousCredentials = shallowCopyMap(previousCredentials)
			for key := range previousCredentials {
				if strings.HasPrefix(key, "step_account_") {
					delete(previousCredentials, key)
				}
			}
		}
		credentials = MergeCredentials(previousCredentials, credentials)
		delete(credentials, "api_key")
		if platform == PlatformDeepseek || platform == PlatformStepFun {
			delete(credentials, "refresh_token")
			delete(credentials, "expires_at")
			delete(credentials, "expires_in")
			if !session.Grant.ExpiresAt.IsZero() {
				credentials["expires_at"] = session.Grant.ExpiresAt.UTC().Format(time.RFC3339)
			}
		}
		applied, persistErr := writer.CompleteCNOAuthLoginIfUnchanged(ctx, expected, credentials)
		if persistErr != nil {
			return nil, cnOAuthError(persistErr)
		}
		if !applied {
			return nil, cnOAuthError(cnoauth.ErrSession)
		}
		account, err = s.accountRepo.GetByID(ctx, session.AccountID)
		if err == nil {
			expected.Credentials = credentials
			expectedOwner, expectedErr = cnControlOwnerFingerprint(expected)
			currentOwner, currentErr = cnControlOwnerFingerprint(account)
			if expectedErr != nil || currentErr != nil || expectedOwner != currentOwner {
				return nil, cnOAuthError(cnoauth.ErrSession)
			}
		}
	} else {
		if len(input.ModelMapping) > 0 {
			mapping := make(map[string]any, len(input.ModelMapping))
			for from, to := range input.ModelMapping {
				mapping[from] = to
			}
			credentials["model_mapping"] = mapping
		}
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
	// A concurrent status/completion request may hold the short Redis lease.
	// Retry only the receipt, never the account write that already succeeded.
	receiptCtx, cancelReceipt := context.WithTimeout(ctx, 2*time.Second)
	defer cancelReceipt()
	for {
		err = s.store.Update(receiptCtx, id, func(_ context.Context, current *cnoauth.Session) error {
			current.CompletedID = account.ID
			current.Committing = false
			current.Grant = nil
			current.LoginOwner = nil
			current.Flow.Verifier = ""
			current.Flow.DeviceCode = ""
			view = cnOAuthView(id, current)
			return nil
		})
		if !errors.Is(err, cnoauth.ErrBusy) {
			break
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-receiptCtx.Done():
			timer.Stop()
			return nil, cnOAuthError(receiptCtx.Err())
		case <-timer.C:
		}
	}
	if err != nil {
		return nil, cnOAuthError(err)
	}
	return view, nil
}

func cnOAuthCredentials(flow *cnoauth.Flow, grant *cnoauth.Grant) map[string]any {
	protocol := APIProtocolAnthropic
	if flow.Platform == PlatformStepFun {
		protocol = APIProtocolChatCompletions
	}
	creds := map[string]any{"oauth_provider": flow.Platform, "oauth_region": flow.Region, "access_token": grant.AccessToken, "account_mode": AccountModeCoding, "api_protocol": protocol, "base_url": cnoauth.ModelBase(flow.Platform, flow.Region), "api_base_urls": map[string]any{protocol: cnoauth.ModelBase(flow.Platform, flow.Region)}}
	creds[domesticOAuthLoginIDCredential] = uuid.NewString()
	if flow.Platform == PlatformDeepseek {
		creds["oauth_device_id"] = flow.DeviceID
		creds["account_mode"] = AccountModePayG
		return creds
	}
	if flow.Platform == PlatformStepFun && grant.StepLogin != nil {
		session := grant.StepLogin
		creds["step_account_access_token"] = session.AccessToken
		creds["step_account_refresh_token"] = session.RefreshToken
		creds["step_account_device_id"] = session.DeviceID
		creds["step_account_app_id"] = session.AppID
		creds["step_account_expires_at"] = session.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if grant.RefreshToken != "" && flow.Platform != PlatformStepFun {
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
	client              *cnoauth.Client
	platform            string
	owner               *Account
	force               bool
	rejectedAccessToken string
	expectedOwner       string
}

func NewCNTokenRefresher(platform string) *CNTokenRefresher {
	return &CNTokenRefresher{client: &cnoauth.Client{}, platform: platform}
}
func (r *CNTokenRefresher) CacheKey(a *Account) string {
	return "cn-oauth:" + a.Platform + ":" + strconv.FormatInt(a.ID, 10)
}
func (r *CNTokenRefresher) CanRefresh(a *Account) bool {
	if a == nil || a.Platform != r.platform || !a.IsDomesticOAuth() || (r.owner != nil && !sameCNOAuthOwner(r.owner, a)) {
		return false
	}
	if r.expectedOwner != "" {
		owner, err := cnControlOwnerFingerprint(a)
		if err != nil || owner != r.expectedOwner {
			return false
		}
	}
	if a.Platform == PlatformStepFun {
		for _, key := range []string{"step_account_access_token", "step_account_refresh_token", "step_account_device_id", "step_account_app_id"} {
			if strings.TrimSpace(a.GetCredential(key)) == "" {
				return false
			}
		}
		return true
	}
	return (a.Platform == PlatformKimi || a.Platform == PlatformMiniMax) && strings.TrimSpace(a.GetCredential("refresh_token")) != ""
}
func (r *CNTokenRefresher) NeedsRefresh(a *Account, window time.Duration) bool {
	if !r.CanRefresh(a) {
		return false
	}
	if r.rejectedAccessToken != "" {
		return a.GetCredential("access_token") == r.rejectedAccessToken
	}
	if r.force {
		return true
	}
	expiry := a.GetCredentialAsTime("expires_at")
	if a.Platform == PlatformStepFun {
		expiry = a.GetCredentialAsTime("step_account_expires_at")
	}
	return expiry == nil || time.Until(*expiry) < window
}
func (r *CNTokenRefresher) Refresh(ctx context.Context, a *Account) (map[string]any, error) {
	if !r.CanRefresh(a) {
		return nil, errors.New("reauthorization required")
	}
	a, err := snapshotCNControlAccount(a)
	if err != nil {
		return nil, NewProviderCredentialFailure("native_oauth_owner_invalid", err)
	}
	proxyURL, err := requestCredentialOwnerProxyURL(context.Background(), a)
	if err != nil {
		return nil, NewProviderCredentialFailure("native_oauth_proxy_unavailable", err)
	}
	// The coordinator acquired this exact refresh owner under its locks.
	// Keep the inference attempt's older access-token evidence unchanged;
	// the issuer call resolves identity from its own authoritative snapshot.
	ctx = context.WithValue(ctx, requestCredentialOwnerContextKey{}, requestCredentialOwnerEvidence{
		selected: outboundIdentityOwnerKey(a), credentialOwner: credentialOwnerKey(a), owner: a,
	})
	ctx = context.WithValue(ctx, requestCredentialTransportKey{}, false)
	ctx = WithAccountOutboundIdentity(ctx, a)
	if a.Platform == PlatformStepFun {
		session, err := r.client.RefreshStepLogin(ctx, a.GetCredential("oauth_region"), proxyURL, &cnoauth.StepLogin{AccessToken: a.GetCredential("step_account_access_token"), RefreshToken: a.GetCredential("step_account_refresh_token"), DeviceID: a.GetCredential("step_account_device_id"), AppID: a.GetCredential("step_account_app_id")})
		if err != nil {
			return nil, err
		}
		return MergeCredentials(a.Credentials, map[string]any{"step_account_access_token": session.AccessToken, "step_account_refresh_token": session.RefreshToken, "step_account_expires_at": session.ExpiresAt.UTC().Format(time.RFC3339)}), nil
	}
	grant, err := r.client.Refresh(ctx, a.Platform, a.GetCredential("oauth_region"), a.GetCredential("refresh_token"), proxyURL)
	if err != nil {
		return nil, err
	}
	updated := map[string]any{"access_token": grant.AccessToken, "refresh_token": grant.RefreshToken, "expires_at": grant.ExpiresAt.UTC().Format(time.RFC3339)}
	return MergeCredentials(a.Credentials, updated), nil
}

// RefreshAccount shares the background coordinator and never routes native
// credentials through the OpenAI/Claude OAuth clients.
func (s *CNOAuthService) RefreshAccount(ctx context.Context, a *Account, force bool) (*Account, error) {
	fresh, err := s.refreshAccount(ctx, a, force, "")
	if err != nil && force && !isOAuthRefreshRequestPath(ctx) {
		return nil, cnOAuthError(err)
	}
	return fresh, err
}

func sameCNOAuthOwner(selected, current *Account) bool {
	return selected != nil && current != nil && selected.ID == current.ID &&
		selected.Platform == current.Platform && selected.Type == current.Type && current.IsDomesticOAuth() &&
		selected.GetCredential("oauth_provider") == current.GetCredential("oauth_provider") &&
		selected.GetCredential("oauth_region") == current.GetCredential("oauth_region") &&
		selected.GetCredential(domesticOAuthLoginIDCredential) == current.GetCredential(domesticOAuthLoginIDCredential) &&
		cnOAuthSameProxyID(selected.ProxyID, current.ProxyID) &&
		selected.proxyURLOrEmpty() == current.proxyURLOrEmpty()
}

func cnOAuthSameProxyID(selected, current *int64) bool {
	return (selected == nil && current == nil) || (selected != nil && current != nil && *selected == *current)
}

func (s *CNOAuthService) refreshAccount(ctx context.Context, a *Account, force bool, rejected string) (*Account, error) {
	if a == nil || !a.IsDomesticOAuth() || (a.Platform != PlatformKimi && a.Platform != PlatformMiniMax && a.Platform != PlatformStepFun) {
		return nil, NewAccountCredentialFailure("native_oauth_reauthorization_required", nil)
	}
	if s == nil || s.refreshAPI == nil || s.client == nil {
		return nil, NewProviderCredentialFailure("native_oauth_refresh_unavailable", nil)
	}
	executor := &CNTokenRefresher{client: s.client, platform: a.Platform, owner: snapshotOAuthRefreshAccount(a), force: force, rejectedAccessToken: rejected}
	if a.Platform == PlatformStepFun && !executor.CanRefresh(a) {
		return nil, NewAccountCredentialFailure("native_oauth_reauthorization_required", nil)
	}
	result, err := s.refreshAPI.RefreshIfNeeded(ctx, a, executor, 2*time.Minute)
	if err != nil && rejected != "" && oauthRefreshContextError(ctx) == nil && errors.Is(err, errOAuthRefreshAccountStateChanged) && s.accountRepo != nil {
		// A concurrent refresh may win the full-grant CAS after this attempt
		// received its response. Only an explicit 401 retry can reuse that
		// rotation, and only within the same known authorization lifetime.
		current, readErr := s.accountRepo.GetByID(ctx, a.ID)
		if ctxErr := oauthRefreshContextError(ctx); ctxErr != nil {
			return nil, ctxErr
		}
		if readErr == nil && current != nil && current.IsActive() && sameCNOAuthOwner(a, current) && strings.TrimSpace(a.GetCredential(domesticOAuthLoginIDCredential)) != "" {
			if current.GetCredential("access_token") == rejected {
				return nil, NewProviderCredentialFailure("native_oauth_rejected_token_unchanged", nil)
			}
			if sameCNOAuthRotationContext(a, current) && executor.CanRefresh(current) {
				result, err = &OAuthRefreshResult{Account: current}, nil
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		var typed *CredentialFailureError
		if errors.As(err, &typed) {
			return nil, err
		}
		failure := NewProviderCredentialFailure("native_oauth_refresh_unavailable", err)
		if errors.Is(err, cnoauth.ErrInvalidGrant) || errors.Is(err, cnoauth.ErrDenied) || errors.Is(err, cnoauth.ErrExpired) {
			failure = NewAccountCredentialFailure("native_oauth_grant_invalid", err)
		}
		if result != nil {
			failure = WithCredentialFailureAccount(failure, result.Account)
		}
		return nil, failure
	}
	if result == nil || result.LockHeld {
		return nil, NewProviderCredentialFailure("native_oauth_refresh_busy", nil)
	}
	fresh := result.Account
	if !sameCNOAuthOwner(a, fresh) || !fresh.IsActive() {
		return nil, NewProviderCredentialFailure("native_oauth_owner_changed", nil)
	}
	if a.Platform == PlatformStepFun && !executor.CanRefresh(fresh) {
		return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("native_oauth_reauthorization_required", nil), fresh)
	}
	if strings.TrimSpace(fresh.GetCredential("access_token")) == "" {
		return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("native_oauth_access_token_missing", nil), fresh)
	}
	if rejected != "" && fresh.GetCredential("access_token") == rejected {
		return nil, NewProviderCredentialFailure("native_oauth_rejected_token_unchanged", nil)
	}
	if rejected != "" && !result.Refreshed && strings.TrimSpace(a.GetCredential(domesticOAuthLoginIDCredential)) == "" {
		return nil, NewProviderCredentialFailure("native_oauth_owner_changed", nil)
	}
	if rejected != "" && !result.Refreshed && !sameCNOAuthRotationContext(a, fresh) {
		return nil, NewProviderCredentialFailure("native_oauth_owner_changed", nil)
	}
	return fresh, nil
}

func sameCNOAuthRotationContext(previous, current *Account) bool {
	previous = snapshotOAuthRefreshAccount(previous)
	current = snapshotOAuthRefreshAccount(current)
	for _, key := range []string{"access_token", "refresh_token", "expires_at", "expires_in", "_token_version"} {
		delete(previous.Credentials, key)
		delete(current.Credentials, key)
	}
	previousOwner, previousErr := cnControlOwnerFingerprint(previous)
	currentOwner, currentErr := cnControlOwnerFingerprint(current)
	return previousErr == nil && currentErr == nil && previousOwner == currentOwner
}

// One explicit HTTP 401 can recover the selected account before failover. The
// rejected token, rather than its expiry, fences concurrent refresh attempts.
func (s *CNOAuthService) roundTripNativeOAuth(req *http.Request, a *Account, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	return s.roundTripNativeOAuthWithPrepare(req, a, s.prepareRequest, send)
}

func (s *CNOAuthService) roundTripNativeOAuthWithPrepare(req *http.Request, a *Account, prepare func(*http.Request, *Account) error, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	owner, err := requestCredentialAccount(req.Context(), a)
	if err != nil {
		return nil, err
	}
	a = snapshotRequestCredentialAccount(owner)
	if a == nil || !a.IsDomesticOAuth() {
		return send(req)
	}
	if s == nil {
		return nil, NewProviderCredentialFailure("native_oauth_service_unavailable", nil)
	}
	if err := prepare(req, a); err != nil {
		return nil, err
	}
	resp, err := send(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusUnauthorized ||
		(a.Platform != PlatformKimi && a.Platform != PlatformMiniMax) ||
		(req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, err
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return resp, nil
		}
	}
	fresh, err := s.refreshAccount(req.Context(), a, true, strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "))
	if err == nil {
		err = prepare(retry, fresh)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		if retry.Body != nil {
			_ = retry.Body.Close()
		}
		return nil, err
	}
	resp, err = send(retry)
	if err == nil && resp != nil && resp.StatusCode == http.StatusUnauthorized {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("native_oauth_access_token_rejected", nil), fresh)
	}
	return resp, err
}
func (s *CNOAuthService) prepareRequest(req *http.Request, a *Account) error {
	if !a.IsDomesticOAuth() {
		return nil
	}
	if _, err := a.StrictProxyURL(); err != nil {
		return err
	}
	if _, err := cnoauth.NormalizeRegion(a.Platform, a.GetCredential("oauth_region")); err != nil {
		return NewAccountCredentialFailure("native_oauth_region_invalid", err)
	}
	if a.Platform == PlatformStepFun {
		if err := prepareStepFunOAuthRequest(req, a); err != nil {
			if a.GetCredential("access_token") == "" {
				return NewAccountCredentialFailure("native_oauth_access_token_missing", err)
			}
			if expiry := a.GetCredentialAsTime("expires_at"); expiry != nil && !time.Now().Before(*expiry) {
				return NewAccountCredentialFailure("native_oauth_grant_expired", err)
			}
			return NewProviderCredentialFailure("native_oauth_destination_rejected", err)
		}
		recordTransmittedRequestCredentialOwner(req, a, a.GetCredential("access_token"))
		return nil
	}
	if req.Method == http.MethodGet {
		return s.prepareControlRequest(req, a)
	}
	expected, _ := url.Parse(cnoauth.ModelBase(a.Platform, a.GetCredential("oauth_region")))
	if req.URL.User != nil || req.URL.Fragment != "" || req.URL.Scheme != expected.Scheme || req.URL.Host != expected.Host || req.URL.Path != strings.TrimRight(expected.Path, "/")+"/v1/messages" {
		return NewProviderCredentialFailure("native_oauth_destination_rejected", nil)
	}
	ctx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(withOAuthRefreshRequestPath(req.Context())), a)
	*req = *req.WithContext(ctx)
	if a.Platform != PlatformDeepseek && !(&CNTokenRefresher{platform: a.Platform}).CanRefresh(a) {
		return NewAccountCredentialFailure("native_oauth_refresh_token_missing", nil)
	}
	if a.Platform != PlatformDeepseek && (&CNTokenRefresher{platform: a.Platform}).NeedsRefresh(a, 2*time.Minute) {
		fresh, err := s.RefreshAccount(ctx, a, false)
		if err != nil {
			return err
		}
		if !sameCNOAuthOwner(a, fresh) {
			return NewProviderCredentialFailure("native_oauth_owner_changed", nil)
		}
		a = fresh
	}
	if a.Platform == PlatformKimi {
		q := req.URL.Query()
		q.Set("beta", "true")
		req.URL.RawQuery = q.Encode()
	}
	if strings.TrimSpace(a.GetCredential("access_token")) == "" {
		return NewAccountCredentialFailure("native_oauth_access_token_missing", nil)
	}
	setAnthropicAPIKeyAuthHeader(req.Header, a, a.GetCredential("access_token"), a.GetAnthropicProtocolBaseURL())
	recordTransmittedRequestCredentialOwner(req, a, a.GetCredential("access_token"))
	return nil
}
