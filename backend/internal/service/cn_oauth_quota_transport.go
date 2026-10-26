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
	recordTransmittedRequestCredentialOwner(req, account, token)
	if operation == "usages" {
		return withCNQuotaRequestOwner(req, account)
	}
	return nil
}
