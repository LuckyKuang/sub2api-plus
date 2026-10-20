package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
)

func (s *CNOAuthService) prepareKimiQuotaRequest(req *http.Request, account *Account) error {
	return s.prepareKimiControlRequest(req, account, "usages")
}

func (s *CNOAuthService) prepareKimiModelsRequest(req *http.Request, account *Account) error {
	return s.prepareKimiControlRequest(req, account, "models")
}

func (s *CNOAuthService) prepareKimiControlRequest(req *http.Request, account *Account, operation string) error {
	if account == nil || !account.IsDomesticOAuth() || account.Platform != PlatformKimi {
		return NewProviderCredentialFailure("native_oauth_owner_changed", nil)
	}
	if _, err := cnoauth.NormalizeRegion(PlatformKimi, account.GetCredential("oauth_region")); err != nil {
		return NewAccountCredentialFailure("native_oauth_region_invalid", err)
	}
	expected := strings.TrimRight(cnoauth.ModelBase(PlatformKimi, account.GetCredential("oauth_region")), "/") + "/v1/" + operation
	if req.Method != http.MethodGet || req.URL.String() != expected {
		return NewProviderCredentialFailure("native_oauth_destination_rejected", nil)
	}
	ctx := WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(withOAuthRefreshRequestPath(req.Context())), account)
	*req = *req.WithContext(ctx)
	refresher := &CNTokenRefresher{platform: PlatformKimi}
	if !refresher.CanRefresh(account) {
		return NewAccountCredentialFailure("native_oauth_refresh_token_missing", nil)
	}
	if refresher.NeedsRefresh(account, 2*time.Minute) {
		fresh, err := s.RefreshAccount(ctx, account, false)
		if err != nil {
			return err
		}
		account = fresh
	}
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token == "" {
		return NewAccountCredentialFailure("native_oauth_access_token_missing", nil)
	}
	deleteHeaderAllForms(req.Header, "Authorization")
	deleteHeaderAllForms(req.Header, "x-api-key")
	req.Header.Set("Authorization", "Bearer "+token)
	if operation == "usages" {
		return withCNQuotaRequestOwner(req, account)
	}
	return nil
}

// Usage monitoring is on the fixed provider estate and authenticates with the
// raw project PAT. Team scope comes from that PAT, not generic account headers.
func (s *CNProviderQuotaService) roundTripZhipuQuota(req *http.Request, account *Account, send func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if s.cnOAuthService == nil || s.cnOAuthService.zhipu == nil {
		return nil, NewProviderCredentialFailure("project_token_service_unavailable", nil)
	}
	*req = *req.WithContext(WithHTTPUpstreamRedirectsDisabled(WithAccountOutboundIdentity(req.Context(), account)))
	manager := s.cnOAuthService.zhipu
	rejected := ""
	for attempt := 0; attempt < 2; attempt++ {
		value, err := manager.projectToken(req.Context(), account, rejected)
		if err != nil {
			return nil, classifyCredentialAcquisitionFailure(account, err)
		}
		org, project, err := zhipuProjectTokenTeamScope(account, value)
		if err != nil {
			return nil, NewProviderCredentialFailure("project_token_team_scope_missing", err)
		}
		deleteHeaderAllForms(req.Header, "Authorization")
		deleteHeaderAllForms(req.Header, "x-api-key")
		deleteHeaderAllForms(req.Header, "bigmodel-organization")
		deleteHeaderAllForms(req.Header, "bigmodel-project")
		req.Header.Set("Authorization", value.APIKey)
		if org != "" {
			req.Header.Set("bigmodel-organization", org)
			req.Header.Set("bigmodel-project", project)
		}
		resp, err := send(req)
		if err != nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			return resp, err
		}
		manager.rejectProjectToken(req.Context(), account, value.APIKey)
		if attempt == 1 {
			return resp, nil
		}
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		rejected = value.APIKey
	}
	return nil, NewProviderCredentialFailure("project_token_service_unavailable", nil)
}
