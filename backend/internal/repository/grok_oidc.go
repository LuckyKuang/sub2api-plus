package repository

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	sharedhttp "github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

// ValidateAuthorizationTokens is called only after this client's authenticated
// TLS token exchange. Team selection comes from that response's access token,
// not from the callback or a management-provided principal flag.
func (c *grokOAuthClient) ValidateAuthorizationTokens(ctx context.Context, tokens *xai.TokenResponse, login xai.OIDCLoginContext, proxyURL string) (*xai.OIDCIdentity, error) {
	if login.Issuer != xai.OAuthIssuer || login.Nonce == "" || login.ClientID == "" {
		return nil, errors.New("invalid Grok OIDC login context")
	}
	if identity, team := xai.AuthorizationTeamIdentity(tokens); team {
		return identity, nil
	}
	if tokens == nil || tokens.IDToken == "" {
		return nil, errors.New("missing Grok OIDC ID token")
	}
	ctx = grokOAuthIdentityContext(ctx)
	client, err := sharedhttp.GetClient(sharedhttp.Options{ProxyURL: proxyURL, Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	return validateGrokOIDCWithClient(ctx, client, tokens.IDToken, login)
}

func validateGrokOIDCWithClient(ctx context.Context, client *http.Client, raw string, login xai.OIDCLoginContext) (*xai.OIDCIdentity, error) {
	discoveryBytes, err := fetchGrokOIDCDocument(ctx, client, xai.DiscoveryURL)
	if err != nil {
		return nil, err
	}
	var discovery xai.OIDCDiscovery
	if err := json.Unmarshal(discoveryBytes, &discovery); err != nil {
		return nil, errors.New("invalid Grok discovery")
	}
	if discovery.Issuer != "" && discovery.Issuer != login.Issuer {
		return nil, errors.New("grok discovery issuer mismatch")
	}
	keys, err := fetchGrokOIDCDocument(ctx, client, discovery.JWKSURI)
	if err != nil {
		return nil, err
	}
	return xai.VerifyAuthorizationIDToken(raw, login, discovery, keys)
}

func fetchGrokOIDCDocument(ctx context.Context, client *http.Client, endpoint string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	// Discovery cannot expand the network trust boundary or carry credentials to
	// another origin. Do not apply the general unsafe OAuth override to JWKS.
	if err != nil || u.Scheme != "https" || u.Host != "auth.x.ai" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("untrusted Grok OIDC document URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	outboundidentity.ApplyContext(req)
	isolated := *client
	isolated.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := isolated.Do(req)
	if err != nil {
		return nil, errors.New("grok OIDC document request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("grok OIDC document unavailable")
	}
	const limit = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) > limit {
		return nil, errors.New("invalid Grok OIDC document size")
	}
	return data, nil
}
