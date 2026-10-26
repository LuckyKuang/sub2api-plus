//go:build unit

package repository

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type grokOIDCRoundTrip func(*http.Request) (*http.Response, error)

func (f grokOIDCRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGrokOIDCDiscoveryAndKeysRetainLoginIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encode := base64.RawURLEncoding.EncodeToString
	keys, err := json.Marshal(map[string]any{"keys": []any{map[string]string{
		"kty": "RSA", "kid": "key-one", "alg": "RS256", "n": encode(key.N.Bytes()), "e": encode(big.NewInt(int64(key.E)).Bytes()),
	}}})
	require.NoError(t, err)
	login := xai.OIDCLoginContext{Issuer: xai.OAuthIssuer, ClientID: "public-client", Nonce: "bound-login"}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss": login.Issuer, "aud": login.ClientID, "nonce": login.Nonce, "sub": "verified-owner", "exp": time.Now().Add(time.Hour).Unix(),
	})
	token.Header["kid"] = "key-one"
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	identity := outboundidentity.Identity{Preset: "grok", UserAgent: "grok-shell/1.0.45 (linux; x86_64)", Headers: map[string]string{
		"X-Grok-Client-Version": "1.0.45", "X-Grok-Client-Identifier": "grok-cli", "X-Grok-Client-Mode": "build",
	}}
	ctx := outboundidentity.WithIdentity(context.Background(), identity)
	ctx = outboundidentity.WithResolver(ctx, func(context.Context, string) outboundidentity.Identity {
		t.Fatal("an existing login must not reselect settings")
		return outboundidentity.Identity{}
	})
	calls := 0
	client := &http.Client{Transport: grokOIDCRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, identity.UserAgent, req.UserAgent())
		require.Equal(t, "1.0.45", req.Header.Get("X-Grok-Client-Version"))
		require.Equal(t, "grok-cli", req.Header.Get("X-Grok-Client-Identifier"))
		require.Equal(t, "build", req.Header.Get("X-Grok-Client-Mode"))
		require.Empty(t, req.Header.Get("Authorization"))
		body := string(keys)
		if calls == 1 {
			require.Equal(t, xai.DiscoveryURL, req.URL.String())
			body = `{"issuer":"` + login.Issuer + `","jwks_uri":"https://auth.x.ai/keys","id_token_signing_alg_values_supported":["RS256"]}`
		} else {
			require.Equal(t, "https://auth.x.ai/keys", req.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	verified, err := validateGrokOIDCWithClient(grokOAuthIdentityContext(ctx), client, raw, login)
	require.NoError(t, err)
	require.Equal(t, "verified-owner", verified.Subject)
	require.Equal(t, 2, calls)
}

func TestGrokOIDCDocumentsCannotExpandNetworkTrust(t *testing.T) {
	for _, endpoint := range []string{"http://auth.x.ai/keys", "https://auth.x.ai.evil.test/keys", "https://auth.x.ai:444/keys", "https://user@auth.x.ai/keys", "https://auth.x.ai/keys#fragment", "file:///keys"} {
		t.Run(endpoint, func(t *testing.T) {
			client := &http.Client{Transport: grokOIDCRoundTrip(func(*http.Request) (*http.Response, error) {
				t.Fatal("untrusted endpoint must not reach transport")
				return nil, nil
			})}
			_, err := fetchGrokOIDCDocument(context.Background(), client, endpoint)
			require.Error(t, err)
		})
	}
	calls := 0
	client := &http.Client{Transport: grokOIDCRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://untrusted.test/keys"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})}
	_, err := fetchGrokOIDCDocument(context.Background(), client, "https://auth.x.ai/keys")
	require.Error(t, err)
	require.Equal(t, 1, calls, "JWKS redirects must never be followed")
}
