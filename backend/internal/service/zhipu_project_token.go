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

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"golang.org/x/sync/semaphore"
)

type projectTokenState struct {
	gate         *semaphore.Weighted
	scope        string
	token        string
	expires      time.Time
	refresh      time.Time
	backoff      time.Time
	rejected     bool
	keyID        string
	organization string
	project      string
}

func (s *projectTokenState) discardToken() {
	s.token = ""
	s.expires = time.Time{}
	s.refresh = time.Time{}
	s.backoff = time.Time{}
	s.rejected = true
}

type projectTokenRequestOwnerKey struct{}

type ZhipuProjectTokenManager struct {
	client       ZhipuOAuthClient
	accounts     AccountRepository
	mu           sync.Mutex
	states       map[int64]*projectTokenState
	ticketStates map[string]*projectTokenState
	now          func() time.Time
	jitter       func() time.Duration
}

func NewZhipuProjectTokenManager(client ZhipuOAuthClient, accounts AccountRepository) *ZhipuProjectTokenManager {
	return &ZhipuProjectTokenManager{client: client, accounts: accounts, states: map[int64]*projectTokenState{}, ticketStates: map[string]*projectTokenState{}, now: time.Now, jitter: func() time.Duration { return time.Duration(rand.IntN(61)) * time.Second }}
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
	values := []string{a.Platform, a.Type, a.Status, proxyID, a.GetCredential("zhipu_project_kind"), a.GetCredential("zcode_jwt_token"), a.GetCredential("oauth_provider"), a.GetCredential("plan_kind"), a.GetCredential("access_token"), a.GetCredential("zhipu_project_key_id"), a.GetCredential("zhipu_organization"), a.GetCredential("zhipu_project"), a.proxyURLOrEmpty()}
	raw, _ := json.Marshal(values)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func (m *ZhipuProjectTokenManager) state(id int64) *projectTokenState {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.states[id]
	if s == nil {
		s = &projectTokenState{gate: semaphore.NewWeighted(1)}
		m.states[id] = s
	}
	return s
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
	return m.token(ctx, a, false)
}

// tokenForTicket is restricted to a ticket's captured owner. Settlement may
// finish after relinking; it must use that old login and never the replacement.
func (m *ZhipuProjectTokenManager) tokenForTicket(ctx context.Context, a *Account) (string, error) {
	// A ticket can outlive relinking under the same account ID. Keep its frozen
	// identity, but do not inherit the replacement login's request-scope cache.
	ctx = context.WithValue(ctx, outboundIdentityScopeKey{}, &outboundIdentityScope{})
	return m.token(ctx, a, true)
}
func (m *ZhipuProjectTokenManager) ticketState(a *Account) *projectTokenState {
	key := projectTokenScope(a) + ":" + strconv.FormatInt(a.ID, 10)
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ticketStates[key]
	if state == nil {
		state = &projectTokenState{gate: semaphore.NewWeighted(1)}
		m.ticketStates[key] = state
	}
	return state
}
func (m *ZhipuProjectTokenManager) forgetTicket(a *Account) {
	if m == nil || a == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ticketStates, projectTokenScope(a)+":"+strconv.FormatInt(a.ID, 10))
}

// observeTicketRejection invalidates only the PAT used on that physical ticket
// call. A delayed failure for an older token cannot evict a rotated credential,
// and quota/server errors do not reject an otherwise valid PAT.
func (m *ZhipuProjectTokenManager) observeTicketRejection(a *Account, usedToken string, requestErr error) {
	var rejection *zcode.OffPeakError
	if m == nil || a == nil || usedToken == "" || !errors.As(requestErr, &rejection) ||
		(rejection.HTTPStatus != http.StatusUnauthorized && rejection.HTTPStatus != http.StatusForbidden) {
		return
	}
	scope := projectTokenScope(a)
	m.mu.Lock()
	state := m.ticketStates[scope+":"+strconv.FormatInt(a.ID, 10)]
	m.mu.Unlock()
	if state == nil {
		return
	}
	if err := state.gate.Acquire(context.Background(), 1); err != nil {
		return
	}
	defer state.gate.Release(1)
	if state.scope == scope && state.token == usedToken {
		state.rejected = true
	}
}
func (m *ZhipuProjectTokenManager) token(ctx context.Context, a *Account, ticket bool) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if m == nil || m.client == nil || !usesZhipuProjectToken(a) || a.ID <= 0 {
		return "", errors.New("project token service unavailable")
	}
	snapshot, snapshotErr := snapshotCNControlAccount(a)
	if snapshotErr != nil {
		return "", snapshotErr
	}
	a = snapshot
	proxyURL, routeErr := a.StrictProxyURL()
	if routeErr != nil {
		return "", routeErr
	}
	state := m.state(a.ID)
	if ticket {
		state = m.ticketState(a)
	}
	if err := state.gate.Acquire(ctx, 1); err != nil {
		return "", err
	}
	defer state.gate.Release(1)
	if !ticket {
		if err := m.assertOwner(ctx, a); err != nil {
			return "", err
		}
	}
	scope := projectTokenScope(a)
	if state.scope != scope {
		state.scope = scope
		state.token = ""
		state.rejected = false
		state.backoff = time.Time{}
		state.keyID = a.GetCredential("zhipu_project_key_id")
		state.organization = a.GetCredential("zhipu_organization")
		state.project = a.GetCredential("zhipu_project")
	}
	now := m.now()
	valid := state.token != "" && !state.rejected && now.Before(state.expires)
	if valid && (now.Before(state.refresh) || now.Before(state.backoff)) {
		return state.token, nil
	}
	ctx = WithAccountOutboundIdentity(ctx, a)
	var token *zcode.ProjectAccessToken
	var err error
	provider := a.GetCredential("oauth_provider")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak && a.GetCredential("zhipu_project_kind") == "" {
		return "", errors.New("off-peak project scope requires reauthorization")
	}
	login := a.GetCredential("access_token")
	if zcode.NormalizeProvider(provider) == "" || strings.TrimSpace(login) == "" {
		return "", errors.New("project token login required")
	}
	if state.keyID == "" || state.organization == "" || state.project == "" {
		// Reconstruct public metadata for existing linked accounts. Copied secrets
		// and saved short-token snapshots are never an authentication fallback.
		var credential *zcode.CodingPlanCredential
		if a.GetCredential("plan_kind") == ZhipuPlanTeamCodingPlan || (a.GetCredential("plan_kind") == ZhipuPlanOffPeak && a.GetCredential("zhipu_project_kind") == "team") {
			credential, err = m.client.ResolveTeamPlanKey(ctx, provider, login, state.organization, state.project, proxyURL)
		} else {
			credential, err = m.client.ResolveIndividualCodingPlanKey(ctx, provider, login, proxyURL)
		}
		if err == nil && credential != nil && credential.KeyID != "" {
			state.keyID = credential.KeyID
			state.organization = credential.OrganizationID
			state.project = credential.ProjectID
			token = &zcode.ProjectAccessToken{Token: credential.APIKey, ExpiresAt: credential.ExpiresAt}
		} else if err == nil {
			err = errors.New("project token metadata unavailable")
		}
	} else {
		token, err = m.client.IssueProjectAccessToken(ctx, provider, login, state.organization, state.project, state.keyID, proxyURL)
	}
	if err := ctx.Err(); err != nil {
		state.discardToken()
		return "", err
	}
	if !ticket {
		if ownerErr := m.assertOwner(ctx, a); ownerErr != nil {
			return "", ownerErr
		}
	}
	if err != nil {
		if valid && m.now().Before(state.expires) && zcode.IsTransientProjectTokenError(err) {
			state.backoff = m.now().Add(5 * time.Second)
			return state.token, nil
		}
		state.discardToken()
		return "", errors.New("project token refresh failed")
	}
	if token == nil || token.Token == "" || !m.now().Before(token.ExpiresAt) {
		state.discardToken()
		return "", errors.New("invalid project token")
	}
	state.token = token.Token
	state.expires = token.ExpiresAt
	state.refresh = token.ExpiresAt.Add(-120*time.Second - m.jitter())
	state.rejected = false
	state.backoff = time.Time{}
	return state.token, nil
}
func (m *ZhipuProjectTokenManager) Prepare(req *http.Request, a *Account) error {
	if !usesZhipuProjectToken(a) {
		return nil
	}
	if m == nil {
		return errors.New("project token service unavailable")
	}
	if req.URL.User != nil || req.URL.Fragment != "" || req.URL.Scheme != "https" {
		return errors.New("project token destination rejected")
	}
	snapshot, snapshotErr := snapshotCNControlAccount(a)
	if snapshotErr != nil {
		return snapshotErr
	}
	a = snapshot
	provider := a.GetCredential("oauth_provider")
	target := req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	allowed := false
	if req.URL.RawPath != "" {
		return errors.New("project token destination rejected")
	}
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		allowed = req.Method == http.MethodPost && (target == zcodeOffPeakAnthropicBaseURL+"/v1/messages" || target == zcodeOffPeakAnthropicBaseURL+"/v1/messages/count_tokens")
	} else {
		chat := strings.TrimRight(zhipuCodingBaseURL(provider), "/")
		anthropic := strings.TrimRight(zhipuAnthropicBaseURL(provider), "/")
		allowed = (req.Method == http.MethodGet && target == chat+"/models") || (req.Method == http.MethodPost && (target == chat+"/chat/completions" || target == anthropic+"/v1/messages" || target == anthropic+"/v1/messages/count_tokens"))
	}
	if !allowed {
		return errors.New("project token destination rejected")
	}
	token, err := m.Token(req.Context(), a)
	if err != nil {
		return err
	}
	ctx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(req.Context()), a)
	*req = *req.WithContext(context.WithValue(ctx, projectTokenRequestOwnerKey{}, a))
	deleteHeaderAllForms(req.Header, "X-Api-Key")
	deleteHeaderAllForms(req.Header, "Authorization")
	deleteHeaderAllForms(req.Header, "X-Coding-Plan-Api-Key")
	deleteHeaderAllForms(req.Header, "bigmodel-organization")
	deleteHeaderAllForms(req.Header, "bigmodel-project")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		req.Header.Set("Authorization", "Bearer "+a.GetCredential("zcode_jwt_token"))
		req.Header.Set("X-Coding-Plan-Api-Key", token)
	} else {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if a.GetCredential("plan_kind") == ZhipuPlanTeamCodingPlan ||
		(a.GetCredential("plan_kind") == ZhipuPlanOffPeak && a.GetCredential("zhipu_project_kind") == "team") {
		organization, project := strings.TrimSpace(a.GetCredential("zhipu_organization")), strings.TrimSpace(a.GetCredential("zhipu_project"))
		if organization != "" && project != "" {
			req.Header.Set("bigmodel-organization", organization)
			req.Header.Set("bigmodel-project", project)
		}
	}
	return nil
}
func (m *ZhipuProjectTokenManager) Observe(req *http.Request, a *Account, resp *http.Response) {
	if req == nil {
		return
	}
	if owner, ok := req.Context().Value(projectTokenRequestOwnerKey{}).(*Account); ok {
		a = owner
	}
	if m == nil || !usesZhipuProjectToken(a) || resp == nil || (resp.StatusCode != 401 && resp.StatusCode != 403) {
		return
	}
	state := m.state(a.ID)
	if err := state.gate.Acquire(context.Background(), 1); err != nil {
		return
	}
	defer state.gate.Release(1)
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		if planToken := req.Header.Get("X-Coding-Plan-Api-Key"); planToken != "" {
			token = planToken
		}
	}
	if state.scope == projectTokenScope(a) && state.token == token {
		state.rejected = true
	}
}
