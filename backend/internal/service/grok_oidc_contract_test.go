//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Source: xai-org/grok-build 2bdd1d6a, xai-grok-login/src/oidc/
// {login,protocol}.rs: personal browser login requires a validated ID token and
// the original nonce; only explicit Team principals omit user-only OIDC scopes.
type grokOIDCContractClient struct {
	GrokOAuthClient
	response                                    *xai.TokenResponse
	verifyError                                 error
	onVerify                                    func()
	verified                                    map[string]any
	verificationCalls                           int
	exchangeCalls                               int
	exchangeProxy                               string
	seenToken, seenClient, seenNonce, seenProxy string
}

type grokOIDCWithoutVerifier struct{ GrokOAuthClient }

func (c *grokOIDCContractClient) ExchangeCode(_ context.Context, _ string, _ string, _ string, proxyURL string, _ string) (*xai.TokenResponse, error) {
	c.exchangeCalls++
	c.exchangeProxy = proxyURL
	return c.response, nil
}
func (c *grokOIDCContractClient) VerifyIDToken(_ context.Context, token, clientID, nonce, proxyURL string) (map[string]any, error) {
	c.verificationCalls++
	c.seenToken, c.seenClient, c.seenNonce, c.seenProxy = token, clientID, nonce, proxyURL
	if c.onVerify != nil {
		c.onVerify()
	}
	return c.verified, c.verifyError
}

func TestGrokOIDCCanceledVerificationCannotPublishLateCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &grokOIDCContractClient{response: &xai.TokenResponse{AccessToken: "late-access", IDToken: "signed-id-token"}, verified: map[string]any{"sub": "verified-user"}, onVerify: cancel}
	svc := NewGrokOAuthService(nil, client)
	t.Cleanup(svc.Stop)
	start, err := svc.GenerateAuthURL(ctx, nil, "")
	require.NoError(t, err)
	info, err := svc.ExchangeCode(ctx, &GrokExchangeCodeInput{SessionID: start.SessionID, State: start.State, Code: "code"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, info)
	require.Nil(t, svc.BuildAccountCredentials(info))
	require.Equal(t, 1, client.verificationCalls)
}

type grokOIDCSessionProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	calls int
}

func (r *grokOIDCSessionProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	r.calls++
	return r.proxy, nil
}

func TestGrokOIDCCallbackRetainsOriginalRoute(t *testing.T) {
	for _, mode := range []string{"no-override", "same-id-edited-row", "different-id", "bind-mid-login"} {
		t.Run(mode, func(t *testing.T) {
			id := int64(51)
			repo := &grokOIDCSessionProxyRepo{proxy: &Proxy{ID: id, Protocol: "http", Host: "original.example", Port: 8080}}
			client := &grokOIDCContractClient{response: &xai.TokenResponse{AccessToken: "access", IDToken: "signed-id-token"}, verified: map[string]any{"sub": "verified-user"}}
			svc := NewGrokOAuthService(repo, client)
			t.Cleanup(svc.Stop)
			var selected *int64 = &id
			if mode == "bind-mid-login" {
				selected = nil
			}
			start, err := svc.GenerateAuthURL(context.Background(), selected, "")
			require.NoError(t, err)
			input := &GrokExchangeCodeInput{SessionID: start.SessionID, State: start.State, Code: "code"}
			switch mode {
			case "same-id-edited-row":
				repo.proxy.Host = "replacement.example"
				input.ProxyID = &id
			case "different-id":
				other := int64(52)
				repo.proxy.ID = other
				input.ProxyID = &other
			case "bind-mid-login":
				input.ProxyID = &id
			}
			info, err := svc.ExchangeCode(context.Background(), input)
			if mode == "different-id" || mode == "bind-mid-login" {
				require.Error(t, err)
				require.Nil(t, info)
				require.Zero(t, client.exchangeCalls)
				require.Zero(t, client.verificationCalls)
				_, ok := svc.sessionStore.Get(start.SessionID)
				require.True(t, ok, "invalid route override cannot consume the original flow")
			} else {
				require.NoError(t, err)
				require.Equal(t, "http://original.example:8080", client.exchangeProxy)
				require.Equal(t, client.exchangeProxy, client.seenProxy)
			}
			expectedLookups := 1
			if selected == nil {
				expectedLookups = 0
			}
			require.Equal(t, expectedLookups, repo.calls, "callback cannot re-resolve a mutable route")
		})
	}
}

func TestGrokOIDCBrowserLoginRequiresValidatedPersonalIdentity(t *testing.T) {
	for _, mode := range []string{"missing-id-token", "verification-failed", "validated", "team-without-id-token", "team-missing-principal", "lowercase-team", "team-invalid-header", "team-two-segments", "no-verifier"} {
		t.Run(mode, func(t *testing.T) {
			client := &grokOIDCContractClient{response: &xai.TokenResponse{AccessToken: makeGrokOAuthJWT(map[string]any{"sub": "unverified-user", "email": "unverified@example.com"}), IDToken: "private-id-token"}, verified: map[string]any{"sub": "verified-user", "email": "verified@example.com"}}
			switch mode {
			case "missing-id-token":
				client.response.IDToken = ""
			case "verification-failed":
				client.verifyError = errors.New("signature or nonce invalid")
			case "team-without-id-token":
				client.response.IDToken = ""
				client.response.AccessToken = makeGrokOIDCAccessJWT(t, map[string]any{"principal_type": "Team", "principal_id": "team-123", "email": "must-not-be-user@example.com", "sub": "must-not-be-user"})
			case "team-invalid-header":
				client.response.IDToken = ""
				client.response.AccessToken = makeGrokOAuthJWT(map[string]any{"principal_type": "Team", "principal_id": "team-123"})
			case "team-two-segments":
				client.response.IDToken = ""
				access := makeGrokOIDCAccessJWT(t, map[string]any{"principal_type": "Team", "principal_id": "team-123"})
				client.response.AccessToken = access[:strings.LastIndex(access, ".")]
			case "team-missing-principal":
				client.response.IDToken = ""
				client.response.AccessToken = makeGrokOAuthJWT(map[string]any{"principal_type": "Team", "team_id": "team-123"})
			case "lowercase-team":
				client.response.IDToken = ""
				client.response.AccessToken = makeGrokOAuthJWT(map[string]any{"principal_type": "team", "principal_id": "team-123"})
			}
			var configured GrokOAuthClient = client
			if mode == "no-verifier" {
				configured = &grokOIDCWithoutVerifier{GrokOAuthClient: client}
			}
			svc := NewGrokOAuthService(nil, configured)
			t.Cleanup(svc.Stop)
			start, err := svc.GenerateAuthURL(context.Background(), nil, "")
			require.NoError(t, err)
			authorization, err := url.Parse(start.AuthURL)
			require.NoError(t, err)
			nonce := authorization.Query().Get("nonce")
			require.NotEmpty(t, nonce)
			session, ok := svc.sessionStore.Get(start.SessionID)
			require.True(t, ok)
			serialized, err := json.Marshal(session)
			require.NoError(t, err)
			var stored map[string]any
			require.NoError(t, json.Unmarshal(serialized, &stored))
			assert.Equal(t, nonce, stored["nonce"], "nonce must survive both process-local and Redis session storage")
			info, err := svc.ExchangeCode(context.Background(), &GrokExchangeCodeInput{SessionID: start.SessionID, State: start.State, Code: "private-code"})
			switch mode {
			case "validated":
				require.NoError(t, err)
				require.NotNil(t, info)
				require.Equal(t, "verified-user", info.Subject)
				require.Equal(t, "verified@example.com", info.Email)
				require.Equal(t, 1, client.verificationCalls)
				require.Equal(t, "private-id-token", client.seenToken)
				require.Equal(t, nonce, client.seenNonce)
				require.Equal(t, authorization.Query().Get("client_id"), client.seenClient)
				require.Empty(t, client.seenProxy)
			case "team-without-id-token":
				require.NoError(t, err)
				require.NotNil(t, info)
				require.Equal(t, "team-123", info.Subject)
				require.Equal(t, "team-123", info.TeamID)
				require.Empty(t, info.Email)
				require.Zero(t, client.verificationCalls)
			default:
				require.Error(t, err, "an unvalidated personal identity cannot become account material")
				require.Nil(t, info)
				require.Nil(t, svc.BuildAccountCredentials(info))
				if mode == "no-verifier" {
					require.Contains(t, err.Error(), "GROK_OIDC_VERIFIER_NOT_CONFIGURED")
				}
				if mode == "verification-failed" {
					require.Equal(t, 1, client.verificationCalls)
				} else {
					require.Zero(t, client.verificationCalls)
				}
			}
		})
	}
}

func makeGrokOIDCAccessJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims)).SignedString([]byte("test-only-access-token-signature"))
	require.NoError(t, err)
	return token
}
