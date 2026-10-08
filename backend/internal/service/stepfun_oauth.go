package service

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
)

func prepareStepFunOAuthRequest(req *http.Request, account *Account) error {
	expected, _ := url.Parse(cnoauth.ModelBase(account.Platform, account.GetCredential("oauth_region")))
	if req.URL.User != nil || req.URL.Fragment != "" || req.URL.RawQuery != "" || req.URL.Scheme != expected.Scheme || req.URL.Host != expected.Host {
		return errors.New("step OAuth destination is not supported")
	}
	chat := req.Method == http.MethodPost && req.URL.Path == expected.Path+"/chat/completions"
	models := req.Method == http.MethodGet && req.URL.Path == expected.Path+"/models"
	if !chat && !models {
		return errors.New("step OAuth operation is not supported")
	}
	if expiry := account.GetCredentialAsTime("expires_at"); expiry != nil && !time.Now().Before(*expiry) {
		return errors.New("step credential expired; reauthorize account")
	}
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token == "" {
		return errors.New("step OAuth credential is missing")
	}
	*req = *req.WithContext(WithAccountOutboundIdentity(WithHTTPUpstreamRedirectsDisabled(req.Context()), account))
	req.Header.Del("X-Api-Key")
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}
