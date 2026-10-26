package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"golang.org/x/sync/singleflight"
)

type projectTokenState struct {
	mu        sync.Mutex
	value     *zcode.CodingPlanCredential
	refreshAt time.Time
	pending   singleflight.Group
}

func (s *projectTokenState) reject(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value != nil && s.value.APIKey == token {
		s.value = nil
		s.refreshAt = time.Time{}
	}
}

type projectTokenRequestOwnerKey struct{}
type ZhipuProjectTokenManager struct {
	client      ZhipuOAuthClient
	accounts    AccountRepository
	mu          sync.Mutex
	states      map[string]*projectTokenState
	generations map[outboundIdentityOwner]string
	now         func() time.Time
	jitter      func() time.Duration
}

func NewZhipuProjectTokenManager(client ZhipuOAuthClient, accounts AccountRepository) *ZhipuProjectTokenManager {
	return &ZhipuProjectTokenManager{client: client, accounts: accounts, states: map[string]*projectTokenState{}, generations: map[outboundIdentityOwner]string{}, now: time.Now, jitter: func() time.Duration { return time.Duration(rand.IntN(61)) * time.Second }}
}
func usesZhipuProjectToken(a *Account) bool {
	if a == nil || a.Platform != PlatformZhipu || a.Type != AccountTypeOAuth {
		return false
	}
	switch a.GetCredential("plan_kind") {
	case ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan, ZhipuPlanOffPeak:
		return true
	}
	return false
}
func projectTokenScope(a *Account) string {
	proxyID := ""
	if a.ProxyID != nil {
		proxyID = strconv.FormatInt(*a.ProxyID, 10)
	}
	values := []string{strconv.FormatInt(a.ID, 10), a.Platform, a.Type, a.Status, proxyID, a.GetCredential(domesticOAuthLoginIDCredential), a.GetCredential("oauth_access_token"), a.GetCredential("zhipu_project_kind"), a.GetCredential("zhipu_project_key_name"), a.GetCredential("zcode_jwt_token"), a.GetCredential("oauth_provider"), a.GetCredential("plan_kind"), a.GetCredential("access_token"), a.GetCredential("zhipu_project_key_id"), a.GetCredential("zhipu_organization"), a.GetCredential("zhipu_project"), a.proxyURLOrEmpty()}
	raw, _ := json.Marshal(values)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func projectTokenIdentityScope(ctx context.Context, a *Account) (context.Context, string) {
	ctx = WithAccountOutboundIdentity(ctx, a)
	identity, _ := outboundidentity.FromContext(ctx)
	raw, _ := json.Marshal(identity)
	return ctx, projectTokenScope(a) + string(raw)
}
func (m *ZhipuProjectTokenManager) state(ctx context.Context, a *Account, ticket bool) *projectTokenState {
	_, scope := projectTokenIdentityScope(ctx, a)
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.states[scope]
	if state == nil {
		state = &projectTokenState{}
		m.states[scope] = state
	}
	if !ticket {
		m.generations[outboundIdentityOwnerKey(a)] = scope
	}
	return state
}
func (m *ZhipuProjectTokenManager) assertOwner(ctx context.Context, a *Account) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.accounts == nil {
		return nil
	}
	current, err := m.accounts.GetByID(ctx, a.ID)
	if err != nil || current == nil || !usesZhipuProjectToken(current) || projectTokenScope(current) != projectTokenScope(a) {
		return errors.New("project token owner changed")
	}
	return nil
}
func (m *ZhipuProjectTokenManager) Token(ctx context.Context, a *Account) (string, error) {
	value, err := m.credential(ctx, a, false)
	if err != nil {
		return "", err
	}
	return value.APIKey, nil
}
func (m *ZhipuProjectTokenManager) tokenForTicket(ctx context.Context, a *Account) (string, error) {
	ctx = context.WithValue(ctx, outboundIdentityScopeKey{}, &outboundIdentityScope{})
	ctx = context.WithValue(ctx, requestCredentialOwnerContextKey{}, requestCredentialOwnerEvidence{})
	ctx = context.WithValue(ctx, requestCredentialTransportKey{}, false)
	value, err := m.credential(ctx, a, true)
	if err != nil {
		return "", err
	}
	return value.APIKey, nil
}
func (m *ZhipuProjectTokenManager) forgetTicket(ctx context.Context, a *Account) {
	if m == nil || a == nil {
		return
	}
	_, scope := projectTokenIdentityScope(ctx, a)
	m.mu.Lock()
	delete(m.states, scope)
	m.mu.Unlock()
}
func (m *ZhipuProjectTokenManager) reject(ctx context.Context, a *Account, token string) {
	if m == nil || a == nil || token == "" {
		return
	}
	_, scope := projectTokenIdentityScope(ctx, a)
	m.mu.Lock()
	state := m.states[scope]
	m.mu.Unlock()
	if state != nil {
		state.reject(token)
	}
}
func (m *ZhipuProjectTokenManager) observeTicketRejection(ctx context.Context, a *Account, token string, err error) {
	var rejection *zcode.OffPeakError
	if !errors.As(err, &rejection) || (rejection.HTTPStatus != 401 && rejection.HTTPStatus != 403) {
		return
	}
	m.reject(ctx, a, token)
}
func (m *ZhipuProjectTokenManager) credential(ctx context.Context, a *Account, ticket bool) (*zcode.CodingPlanCredential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m == nil || m.client == nil || !usesZhipuProjectToken(a) || a.ID <= 0 {
		return nil, NewProviderCredentialFailure("project_token_service_unavailable", nil)
	}
	owner, err := requestCredentialAccount(ctx, a)
	if err != nil || owner == nil {
		return nil, NewProviderCredentialFailure("project_token_owner_unavailable", err)
	}
	a, err = snapshotCNControlAccount(owner)
	if err != nil {
		return nil, err
	}
	proxy, err := a.StrictProxyURL()
	if err != nil {
		return nil, NewProviderCredentialFailure("project_token_proxy_unavailable", err)
	}
	if !ticket {
		if err = m.assertOwner(ctx, a); err != nil {
			return nil, err
		}
	}
	selectedCtx, scope := projectTokenIdentityScope(ctx, a)
	entry := m.state(selectedCtx, a, ticket)
	current := func() bool {
		if ticket {
			return true
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.generations[outboundIdentityOwnerKey(a)] == scope && m.states[scope] == entry
	}
	ch := entry.pending.DoChan("issue", func() (result any, issueErr error) {
		defer func() {
			if issueErr != nil {
				entry.mu.Lock()
				entry.value = nil
				entry.refreshAt = time.Time{}
				entry.mu.Unlock()
			}
		}()
		entry.mu.Lock()
		cached := entry.value
		if cached != nil && m.now().Before(entry.refreshAt) && m.now().Before(cached.ExpiresAt) {
			entry.mu.Unlock()
			return cached, nil
		}
		entry.mu.Unlock()
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(selectedCtx), 30*time.Second)
		defer cancel()
		provider := zcode.NormalizeProvider(a.GetCredential("oauth_provider"))
		if provider == "" {
			return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("project_token_provider_invalid", nil), a)
		}
		if a.GetCredential("plan_kind") == ZhipuPlanOffPeak && a.GetCredential("zhipu_project_kind") == "" && a.GetCredential("zhipu_project_key_name") == "" {
			return nil, errors.New("off-peak project scope requires reauthorization")
		}
		org, project, key := a.GetCredential("zhipu_organization"), a.GetCredential("zhipu_project"), a.GetCredential("zhipu_project_key_id")
		providerLogin := strings.TrimSpace(a.GetCredential("oauth_access_token"))
		var value *zcode.CodingPlanCredential
		var err error
		for attempt := 0; attempt < 2; attempt++ {
			login := providerLogin
			if login != "" && provider == zcode.ProviderZai {
				login, err = m.client.ExchangeZaiBusinessToken(callCtx, login, proxy)
			} else if login == "" {
				login = strings.TrimSpace(a.GetCredential("access_token"))
			}
			if err != nil {
				break
			}
			if login == "" {
				return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("project_token_login_required", nil), a)
			}
			if key != "" && org != "" && project != "" {
				var issued *zcode.ProjectAccessToken
				issued, err = m.client.IssueProjectAccessToken(callCtx, provider, login, org, project, key, proxy)
				if err == nil && issued != nil {
					value = &zcode.CodingPlanCredential{APIKey: issued.Token, ExpiresAt: issued.ExpiresAt, KeyID: key, OrganizationID: org, ProjectID: project}
				}
				var missing *zcode.BusinessHTTPError
				if errors.As(err, &missing) && missing.Status == 404 {
					key = ""
				}
			}
			if key == "" || org == "" || project == "" {
				if zhipuTeamProject(a) {
					value, err = m.client.ResolveTeamPlanKey(callCtx, provider, login, org, project, proxy)
				} else {
					value, err = m.client.ResolveIndividualCodingPlanKey(callCtx, provider, login, proxy)
				}
			}
			var status *zcode.BusinessHTTPError
			if err == nil || provider != zcode.ProviderZai || providerLogin == "" || !errors.As(err, &status) || status.Status != 401 {
				break
			}
		}
		if !ticket {
			if ownerErr := m.assertOwner(callCtx, a); ownerErr != nil {
				return nil, ownerErr
			}
		}
		if !current() {
			return nil, NewProviderCredentialFailure("project_token_scope_invalidated", nil)
		}
		if err != nil {
			if callCtx.Err() != nil {
				return nil, callCtx.Err()
			}
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			if zcode.IsTransientProjectTokenError(err) {
				entry.mu.Lock()
				cached = entry.value
				if cached != nil && m.now().Before(cached.ExpiresAt) {
					entry.refreshAt = minTime(m.now().Add(5*time.Second), cached.ExpiresAt)
					entry.mu.Unlock()
					return cached, nil
				}
				entry.mu.Unlock()
			}
			return nil, zhipuProjectTokenFailure(callCtx, a, err)
		}
		if value == nil || value.APIKey == "" || !m.now().Before(value.ExpiresAt) {
			return nil, NewProviderCredentialFailure("project_token_invalid_response", nil)
		}
		copy := *value
		entry.mu.Lock()
		entry.value = &copy
		entry.refreshAt = copy.ExpiresAt.Add(-120*time.Second - m.jitter())
		entry.mu.Unlock()
		return &copy, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if result.Err != nil {
			return nil, result.Err
		}
		if !current() {
			return nil, NewProviderCredentialFailure("project_token_scope_invalidated", nil)
		}
		if !ticket {
			if err = m.assertOwner(ctx, a); err != nil {
				return nil, err
			}
		}
		value, ok := result.Val.(*zcode.CodingPlanCredential)
		if !ok || value == nil {
			return nil, NewProviderCredentialFailure("project_token_invalid_response", nil)
		}
		return value, nil
	}
}
func zhipuTeamProject(a *Account) bool {
	if a.GetCredential("plan_kind") == ZhipuPlanTeamCodingPlan {
		return true
	}
	if a.GetCredential("plan_kind") != ZhipuPlanOffPeak {
		return false
	}
	if kind := a.GetCredential("zhipu_project_kind"); kind != "" {
		return kind == "team"
	}
	if name := a.GetCredential("zhipu_project_key_name"); name != "" {
		return name == zcode.TeamAPIKeyName
	}
	headers, _ := a.Credentials[credKeyHeaderOverrides].(map[string]any)
	org, project := false, false
	for name, value := range headers {
		text, _ := value.(string)
		if text == "" {
			continue
		}
		if strings.EqualFold(name, "bigmodel-organization") {
			org = true
		}
		if strings.EqualFold(name, "bigmodel-project") {
			project = true
		}
	}
	return org && project
}

func zhipuProjectTokenFailure(ctx context.Context, account *Account, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var status *zcode.BusinessHTTPError
	if errors.As(err, &status) && (status.Status == http.StatusUnauthorized || status.Status == http.StatusForbidden) {
		return WithCredentialFailureAccount(NewAccountCredentialFailure("project_token_login_rejected", err), account)
	}
	return NewProviderCredentialFailure("project_token_resolution_failed", err)
}

// Linked personal/team discovery is a gateway extension under the same fixed
// estate as inference. Never attach the business login to the model endpoint;
// the final sending wrapper supplies the current owner-scoped PAT.
func (s *AccountTestService) buildZhipuLinkedModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	provider := zcode.NormalizeProvider(account.GetCredential("oauth_provider"))
	if provider == "" {
		return nil, newUpstreamModelSyncConfigError("Linked ZCode provider is invalid", nil)
	}
	target := buildOpenAIModelsURL(zhipuCodingBaseURL(provider))
	if _, err := s.validateUpstreamBaseURL(target); err != nil {
		return nil, newUpstreamModelSyncConfigError("Linked ZCode model destination is not allowed", err)
	}
	ctx = WithHTTPUpstreamRedirectsDisabled(WithAccountOutboundIdentity(ctx, account))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	account.applyOpenAIHeaderOverrides(req.Header)
	return req, nil
}

func (m *ZhipuProjectTokenManager) Prepare(req *http.Request, a *Account) error {
	if !usesZhipuProjectToken(a) {
		return nil
	}
	if m == nil {
		return errors.New("project token service unavailable")
	}
	if req.URL.User != nil || req.URL.Fragment != "" || req.URL.Scheme != "https" {
		return NewProviderCredentialFailure("project_token_destination_rejected", nil)
	}
	// Keep the acquired grant's value representation as well as its contents.
	// JSON normalization would turn a typed account identity into a map and
	// make the same grant look like conflicting acquisition evidence.
	a = snapshotRequestCredentialAccount(a)
	provider := a.GetCredential("oauth_provider")
	target := req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	allowed := false
	if req.URL.RawPath != "" {
		return NewProviderCredentialFailure("project_token_destination_rejected", nil)
	}
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		allowed = req.Method == http.MethodPost && (target == zcodeOffPeakAnthropicBaseURL+"/v1/messages" || target == zcodeOffPeakAnthropicBaseURL+"/v1/messages/count_tokens")
	} else {
		chat := strings.TrimRight(zhipuCodingBaseURL(provider), "/")
		anthropic := strings.TrimRight(zhipuAnthropicBaseURL(provider), "/")
		allowed = (req.Method == http.MethodGet && target == chat+"/models") || (req.Method == http.MethodPost && (target == chat+"/chat/completions" || target == anthropic+"/v1/messages" || target == anthropic+"/v1/messages/count_tokens"))
	}
	if !allowed {
		return NewProviderCredentialFailure("project_token_destination_rejected", nil)
	}
	ctx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(req.Context()), a)
	token, err := m.Token(ctx, a)
	if err != nil {
		return err
	}
	*req = *req.WithContext(context.WithValue(ctx, projectTokenRequestOwnerKey{}, a))
	deleteHeaderAllForms(req.Header, "X-Api-Key")
	deleteHeaderAllForms(req.Header, "Authorization")
	deleteHeaderAllForms(req.Header, "X-Coding-Plan-Api-Key")
	deleteHeaderAllForms(req.Header, "bigmodel-organization")
	deleteHeaderAllForms(req.Header, "bigmodel-project")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		jwt := strings.TrimSpace(a.GetCredential("zcode_jwt_token"))
		if jwt == "" {
			jwt = strings.TrimSpace(a.GetCredential("api_key"))
		}
		if jwt == "" {
			return WithCredentialFailureAccount(NewAccountCredentialFailure("off_peak_model_credential_missing", nil), a)
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("X-Coding-Plan-Api-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if zhipuTeamProject(a) {
		organization, project := strings.TrimSpace(a.GetCredential("zhipu_organization")), strings.TrimSpace(a.GetCredential("zhipu_project"))
		if organization == "" || project == "" {
			return NewProviderCredentialFailure("project_token_team_scope_missing", nil)
		}
		if organization != "" && project != "" {
			req.Header.Set("bigmodel-organization", organization)
			req.Header.Set("bigmodel-project", project)
		}
	}
	recordTransmittedRequestCredentialOwner(req, a, a.GetOpenAIProtocolAPIKey())
	return nil
}
func (m *ZhipuProjectTokenManager) Observe(req *http.Request, a *Account, resp *http.Response) {
	if req == nil {
		return
	}
	if owner, ok := req.Context().Value(projectTokenRequestOwnerKey{}).(*Account); ok {
		a = owner
	}
	if m == nil || !usesZhipuProjectToken(a) || resp == nil || resp.StatusCode != 401 {
		return
	}
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		token = req.Header.Get("X-Coding-Plan-Api-Key")
	}
	m.reject(req.Context(), a, token)
}

// RoundTrip retains one credential owner across a bounded explicit-401 replay.
func (m *ZhipuProjectTokenManager) RoundTrip(req *http.Request, a *Account, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	owner, err := requestCredentialAccount(req.Context(), a)
	if err != nil {
		return nil, err
	}
	a = snapshotRequestCredentialAccount(owner)
	if !usesZhipuProjectToken(a) {
		return send(req)
	}
	if err = m.Prepare(req, a); err != nil {
		return nil, err
	}
	sendAttempt := func(attempt *http.Request) (*http.Response, error) {
		resp, err := send(attempt)
		m.Observe(attempt, a, resp)
		return resp, err
	}
	resp, err := sendAttempt(req)
	if err != nil || resp == nil || resp.StatusCode != 401 || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, err
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return resp, nil
		}
	}
	if err = m.Prepare(retry, a); err != nil {
		if retry.Body != nil {
			_ = retry.Body.Close()
		}
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	return sendAttempt(retry)
}
