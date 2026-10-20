package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"golang.org/x/sync/singleflight"
)

type zhipuProjectTokenEntry struct {
	mu        sync.Mutex
	value     *zcode.CodingPlanCredential
	refreshAt time.Time
	pending   singleflight.Group
}

func (e *zhipuProjectTokenEntry) reject(token string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if token != "" && e.value != nil && e.value.APIKey == token {
		e.value = nil
		e.refreshAt = time.Time{}
	}
}

func zhipuProjectTokenScope(ctx context.Context, account *Account) (context.Context, string) {
	ctx = WithAccountOutboundIdentity(ctx, account)
	identity, _ := outboundidentity.FromContext(ctx)
	fingerprint, _ := json.Marshal(identity)
	return ctx, zhipuProjectTokenGeneration(account) + string(fingerprint)
}

// Rejection never issues a replacement itself, and cannot evict a newer PAT
// or another login/identity generation when a response arrives late.
func (s *ZhipuOAuthService) rejectProjectToken(ctx context.Context, account *Account, token string) {
	if s == nil || token == "" {
		return
	}
	_, generation := zhipuProjectTokenScope(ctx, account)
	owner := outboundIdentityOwnerKey(account)
	s.projectTokenMu.Lock()
	entry := s.projectTokens[owner]
	if s.projectTokenGenerations[owner] != generation {
		entry = nil
	}
	s.projectTokenMu.Unlock()
	if entry != nil {
		entry.reject(token)
	}
}

func usesZhipuProjectToken(a *Account) bool {
	if a == nil || a.Platform != PlatformZhipu || a.Type != AccountTypeOAuth {
		return false
	}
	switch a.GetCredential("plan_kind") {
	case ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan, ZhipuPlanOffPeak:
		return true
	default:
		return false
	}
}

func zhipuProjectTokenGeneration(a *Account) string {
	// The owner includes the original login, project, estate and proxy. The
	// digest is memory-local and never an outbound identifier or log field.
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", a.ID, a.GetCredential("oauth_provider"), a.GetCredential("oauth_access_token"), a.GetCredential("access_token"), a.GetCredential("plan_kind"), a.GetCredential("zhipu_organization"), a.GetCredential("zhipu_project"), a.proxyURLOrEmpty()+"\x00"+a.GetCredential("zhipu_project_key_id")+"\x00"+a.GetCredential("zhipu_project_key_name"))))
	return hex.EncodeToString(sum[:])
}

// Old off-peak links encode team ownership in their scope headers. New links
// additionally persist the public key name, separately from the ephemeral PAT.
func zhipuTeamProject(a *Account) bool {
	if a.GetCredential("plan_kind") == ZhipuPlanTeamCodingPlan {
		return true
	}
	if a.GetCredential("plan_kind") != ZhipuPlanOffPeak {
		return false
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

func zhipuProjectTokenTeamScope(account *Account, value *zcode.CodingPlanCredential) (string, string, error) {
	if !zhipuTeamProject(account) {
		return "", "", nil
	}
	org, project := strings.TrimSpace(value.OrganizationID), strings.TrimSpace(value.ProjectID)
	if org == "" || project == "" {
		return "", "", errors.New("project_token_team_scope_missing")
	}
	return org, project, nil
}

func (s *ZhipuOAuthService) projectToken(ctx context.Context, account *Account, rejected string) (*zcode.CodingPlanCredential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.client == nil {
		return nil, NewProviderCredentialFailure("project_token_service_unavailable", nil)
	}
	selectedCtx, generation := zhipuProjectTokenScope(ctx, account)
	owner := outboundIdentityOwnerKey(account)
	s.projectTokenMu.Lock()
	if s.projectTokens == nil {
		s.projectTokens = make(map[outboundIdentityOwner]*zhipuProjectTokenEntry)
		s.projectTokenGenerations = make(map[outboundIdentityOwner]string)
	}
	entry := s.projectTokens[owner]
	if entry == nil || s.projectTokenGenerations[owner] != generation {
		entry = &zhipuProjectTokenEntry{}
		s.projectTokens[owner] = entry
		s.projectTokenGenerations[owner] = generation
	}
	s.projectTokenMu.Unlock()
	entry.reject(rejected)
	// Detach the shared issuance from any one subscriber, but keep the selected
	// credential owner's identity snapshot and a bounded operation deadline.
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
		old := entry.value
		if old != nil && time.Now().Before(entry.refreshAt) && time.Now().Before(old.ExpiresAt) {
			entry.mu.Unlock()
			return old, nil
		}
		entry.mu.Unlock()
		callCtx, cancel := context.WithTimeout(context.WithoutCancel(selectedCtx), 30*time.Second)
		defer cancel()
		provider := zcode.NormalizeProvider(account.GetCredential("oauth_provider"))
		if provider == "" {
			return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("project_token_provider_invalid", nil), account)
		}

		providerLogin := strings.TrimSpace(account.GetCredential("oauth_access_token"))
		var value *zcode.CodingPlanCredential
		var err error
		org, project := account.GetCredential("zhipu_organization"), account.GetCredential("zhipu_project")
		keyID := account.GetCredential("zhipu_project_key_id")
		for attempt := 0; attempt < 2; attempt++ {
			token := providerLogin
			if token != "" {
				if provider == zcode.ProviderZai {
					// Keep typed transport/status errors so a transient business
					// exchange can defer refresh of a still-valid PAT too.
					token, err = s.client.ExchangeZaiBusinessToken(callCtx, token, account.proxyURLOrEmpty())
				}
			} else {
				token = strings.TrimSpace(account.GetCredential("access_token"))
			}
			if err != nil {
				break
			}
			if token == "" {
				return nil, WithCredentialFailureAccount(NewAccountCredentialFailure("project_token_login_required", nil), account)
			}
			if keyID != "" && org != "" && project != "" {
				value, err = s.client.IssueProjectToken(callCtx, provider, token, org, project, keyID, account.proxyURLOrEmpty())
				var status *zcode.BusinessHTTPError
				if errors.As(err, &status) && status.Status == 404 {
					keyID = ""
				} // bounded location recovery
			}
			if keyID == "" {
				if zhipuTeamProject(account) {
					value, err = s.client.ResolveTeamPlanKey(callCtx, provider, token, org, project, account.proxyURLOrEmpty())
				} else {
					value, err = s.client.ResolveIndividualCodingPlanKey(callCtx, provider, token, account.proxyURLOrEmpty())
				}
			}
			var status *zcode.BusinessHTTPError
			if err == nil || provider != zcode.ProviderZai || providerLogin == "" || !errors.As(err, &status) || status.Status != 401 {
				break
			}
			// Only an explicit business 401 renews original Z.ai login, once.
		}
		if err != nil {
			var status *zcode.BusinessHTTPError
			var network net.Error
			transient := (errors.As(err, &status) && (status.Status == 429 || (status.Status >= 500 && status.Status < 600))) || errors.As(err, &network)
			if transient {
				entry.mu.Lock()
				// A concurrent model 401 may have rejected the old token while
				// issuance was pending. Only the current cache is a fallback.
				current := entry.value
				if current != nil && time.Now().Before(current.ExpiresAt) {
					entry.refreshAt = minTime(time.Now().Add(5*time.Second), current.ExpiresAt)
					entry.mu.Unlock()
					return current, nil
				}
				entry.mu.Unlock()
			}
			return nil, zhipuProjectTokenFailure(callCtx, account, err)
		}
		if value == nil || value.APIKey == "" || !value.ExpiresAt.After(time.Now()) {
			return nil, NewProviderCredentialFailure("project_token_invalid_response", nil)
		}
		s.projectTokenMu.Lock()
		current := s.projectTokens[owner] == entry && s.projectTokenGenerations[owner] == generation
		s.projectTokenMu.Unlock()
		if !current {
			return nil, NewProviderCredentialFailure("project_token_scope_invalidated", nil)
		}
		copy := *value
		entry.mu.Lock()
		entry.value = &copy
		entry.refreshAt = copy.ExpiresAt.Add(-2*time.Minute - time.Duration(rand.Float64()*float64(time.Minute)))
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
		s.projectTokenMu.Lock()
		current := s.projectTokens[owner] == entry
		s.projectTokenMu.Unlock()
		if !current {
			return nil, NewProviderCredentialFailure("project_token_scope_invalidated", nil)
		}
		value, valid := result.Val.(*zcode.CodingPlanCredential)
		if !valid || value == nil {
			return nil, NewProviderCredentialFailure("project_token_invalid_response", nil)
		}
		return value, nil
	}
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

func zhipuProjectTokenTargetAllowed(req *http.Request, a *Account) bool {
	if req == nil || req.URL == nil || req.URL.Scheme != "https" || req.URL.User != nil {
		return false
	}
	provider := zcode.NormalizeProvider(a.GetCredential("oauth_provider"))
	if provider == "" {
		return false
	}
	bases := []string{zhipuCodingBaseURL(provider), zhipuAnthropicBaseURL(provider)}
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		bases = []string{zcodeOffPeakAnthropicBaseURL}
	}
	for _, base := range bases {
		origin, _ := url.Parse(base)
		if strings.EqualFold(req.URL.Host, origin.Host) && (req.URL.Path == origin.Path || strings.HasPrefix(req.URL.Path, strings.TrimRight(origin.Path, "/")+"/")) {
			return true
		}
	}
	return false
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

func (s *CNOAuthService) prepareZhipuLinkedRequest(req *http.Request, a *Account) error {
	if !usesZhipuProjectToken(a) {
		return nil
	}
	if !zhipuProjectTokenTargetAllowed(req, a) {
		return NewProviderCredentialFailure("project_token_destination_rejected", nil)
	}
	*req = *req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context()))
	if s == nil || s.zhipu == nil {
		return NewProviderCredentialFailure("project_token_service_unavailable", nil)
	}
	value, err := s.zhipu.projectToken(req.Context(), a, "")
	if err != nil {
		return err
	}
	org, project, err := zhipuProjectTokenTeamScope(a, value)
	if err != nil {
		return NewProviderCredentialFailure("project_token_team_scope_missing", err)
	}
	// Scope is a declaration of this PAT's owner, not a generic header
	// override (linked OAuth accounts deliberately cannot use those overrides).
	deleteHeaderAllForms(req.Header, "bigmodel-organization")
	deleteHeaderAllForms(req.Header, "bigmodel-project")
	if org != "" {
		req.Header.Set("bigmodel-organization", org)
		req.Header.Set("bigmodel-project", project)
	}
	deleteHeaderAllForms(req.Header, "x-api-key")
	deleteHeaderAllForms(req.Header, "X-Coding-Plan-Api-Key")
	deleteHeaderAllForms(req.Header, "Authorization")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		jwt := strings.TrimSpace(a.GetCredential("zcode_jwt_token"))
		if jwt == "" {
			jwt = strings.TrimSpace(a.GetOpenAIProtocolAPIKey())
		}
		if jwt == "" {
			return WithCredentialFailureAccount(NewAccountCredentialFailure("off_peak_model_credential_missing", nil), a)
		}
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("X-Coding-Plan-Api-Key", value.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+value.APIKey)
	}
	return nil
}

// Each HTTP attempt resolves from the original account. Only an explicit 401
// rejects that attempt's PAT; 403/network/model failures never rotate it.
func (s *CNOAuthService) roundTripZhipuLinked(req *http.Request, a *Account, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if !usesZhipuProjectToken(a) {
		return send(req)
	}
	if err := s.prepareZhipuLinkedRequest(req, a); err != nil {
		return nil, err
	}
	sendAttempt := func(attempt *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(attempt.Header.Get("Authorization"), "Bearer ")
		if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
			token = attempt.Header.Get("X-Coding-Plan-Api-Key")
		}
		resp, err := send(attempt)
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			s.zhipu.rejectProjectToken(attempt.Context(), a, token)
		}
		return resp, err
	}
	resp, err := sendAttempt(req)
	if err != nil || resp == nil || resp.StatusCode != http.StatusUnauthorized || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, err
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return resp, nil
		}
	}
	if err = s.prepareZhipuLinkedRequest(retry, a); err != nil {
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
