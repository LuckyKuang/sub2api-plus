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

func (s *ZhipuOAuthService) projectToken(ctx context.Context, account *Account, rejected string) (*zcode.CodingPlanCredential, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.client == nil {
		return nil, errors.New("project_token_service_unavailable")
	}
	selectedCtx := WithAccountOutboundIdentity(ctx, account)
	identity, _ := outboundidentity.FromContext(selectedCtx)
	fingerprint, _ := json.Marshal(identity)
	generation := zhipuProjectTokenGeneration(account) + string(fingerprint)
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
	entry.mu.Lock()
	if rejected != "" && entry.value != nil && entry.value.APIKey == rejected {
		entry.value = nil
		entry.refreshAt = time.Time{}
	}
	entry.mu.Unlock()
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
			return nil, errors.New("project_token_provider_invalid")
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
				return nil, errors.New("project_token_login_required")
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
			if transient && old != nil && time.Now().Before(old.ExpiresAt) {
				entry.mu.Lock()
				entry.refreshAt = minTime(time.Now().Add(5*time.Second), old.ExpiresAt)
				entry.mu.Unlock()
				return old, nil
			}
			return nil, errors.New("project_token_resolution_failed")
		}
		if value == nil || value.APIKey == "" || !value.ExpiresAt.After(time.Now()) {
			return nil, errors.New("project_token_invalid_response")
		}
		s.projectTokenMu.Lock()
		current := s.projectTokens[owner] == entry && s.projectTokenGenerations[owner] == generation
		s.projectTokenMu.Unlock()
		if !current {
			return nil, errors.New("project_token_scope_invalidated")
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
			return nil, errors.New("project_token_scope_invalidated")
		}
		value, valid := result.Val.(*zcode.CodingPlanCredential)
		if !valid || value == nil {
			return nil, errors.New("project_token_invalid_response")
		}
		return value, nil
	}
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

func (s *CNOAuthService) prepareZhipuLinkedRequest(req *http.Request, a *Account, rejected string) error {
	if !usesZhipuProjectToken(a) {
		return nil
	}
	if !zhipuProjectTokenTargetAllowed(req, a) {
		return errors.New("project_token_destination_rejected")
	}
	if s == nil || s.zhipu == nil {
		return errors.New("project_token_service_unavailable")
	}
	value, err := s.zhipu.projectToken(req.Context(), a, rejected)
	if err != nil {
		return err
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
			return errors.New("off_peak_model_credential_missing")
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
	if err := s.prepareZhipuLinkedRequest(req, a, ""); err != nil {
		return nil, err
	}
	rejected := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	if a.GetCredential("plan_kind") == ZhipuPlanOffPeak {
		rejected = req.Header.Get("X-Coding-Plan-Api-Key")
	}
	resp, err := send(req)
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
	if err = s.prepareZhipuLinkedRequest(retry, a, rejected); err != nil {
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
	return send(retry)
}
