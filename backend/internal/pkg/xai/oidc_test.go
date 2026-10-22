//go:build unit

package xai

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGrokAuthorizationIDTokenValidation(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encode := base64.RawURLEncoding.EncodeToString
	keys, err := json.Marshal(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "official-key", "alg": "RS256", "n": encode(key.N.Bytes()), "e": encode(big.NewInt(int64(key.E)).Bytes())}}})
	require.NoError(t, err)
	login := OIDCLoginContext{Issuer: OAuthIssuer, ClientID: "public-client", Nonce: "login-nonce"}
	discovery := OIDCDiscovery{Issuer: OAuthIssuer, Algorithms: []string{"RS256"}}
	for _, tc := range []struct {
		name   string
		mutate func(jwt.MapClaims, *jwt.Token)
		valid  bool
	}{
		{"valid", func(jwt.MapClaims, *jwt.Token) {}, true},
		{"wrong nonce", func(c jwt.MapClaims, _ *jwt.Token) { c["nonce"] = "other-login" }, false},
		{"missing nonce", func(c jwt.MapClaims, _ *jwt.Token) { delete(c, "nonce") }, false},
		{"wrong issuer", func(c jwt.MapClaims, _ *jwt.Token) { c["iss"] = "https://other.example" }, false},
		{"wrong audience", func(c jwt.MapClaims, _ *jwt.Token) { c["aud"] = "other-client" }, false},
		{"missing audience", func(c jwt.MapClaims, _ *jwt.Token) { delete(c, "aud") }, false},
		{"missing subject", func(c jwt.MapClaims, _ *jwt.Token) { delete(c, "sub") }, false},
		{"missing expiry", func(c jwt.MapClaims, _ *jwt.Token) { delete(c, "exp") }, false},
		{"expired", func(c jwt.MapClaims, _ *jwt.Token) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, false},
		{"unknown key", func(_ jwt.MapClaims, t *jwt.Token) { t.Header["kid"] = "unknown" }, false},
		{"missing key", func(_ jwt.MapClaims, t *jwt.Token) { delete(t.Header, "kid") }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claims := jwt.MapClaims{"iss": OAuthIssuer, "aud": "public-client", "sub": "verified-person", "nonce": "login-nonce", "exp": time.Now().Add(time.Hour).Unix(), "email": "person@example.test"}
			token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
			token.Header["kid"] = "official-key"
			tc.mutate(claims, token)
			raw, err := token.SignedString(key)
			require.NoError(t, err)
			identity, err := VerifyAuthorizationIDToken(raw, login, discovery, keys)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "verified-person", identity.Subject)
			} else {
				require.Error(t, err)
				require.Nil(t, identity)
			}
		})
	}
	claims := jwt.MapClaims{"iss": OAuthIssuer, "aud": "public-client", "sub": "person", "nonce": "login-nonce", "exp": time.Now().Add(time.Hour).Unix()}
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "official-key"
	raw, err := token.SignedString(wrongKey)
	require.NoError(t, err)
	identity, err := VerifyAuthorizationIDToken(raw, login, discovery, keys)
	require.Error(t, err)
	require.Nil(t, identity)
	token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = "official-key"
	raw, err = token.SignedString([]byte("forbidden-shared-key"))
	require.NoError(t, err)
	identity, err = VerifyAuthorizationIDToken(raw, login, discovery, keys)
	require.Error(t, err)
	require.Nil(t, identity)
}

func TestGrokTeamExceptionRequiresAccessTokenPrincipal(t *testing.T) {
	encode := func(claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		raw, err := token.SignedString([]byte("trusted-exchange-fixture"))
		require.NoError(t, err)
		return raw
	}
	team := encode(jwt.MapClaims{"principal_type": "Team", "principal_id": "team-123"})
	identity, ok := AuthorizationTeamIdentity(&TokenResponse{AccessToken: team})
	require.True(t, ok)
	require.Equal(t, "team-123", identity.Subject)
	_, ok = AuthorizationTeamIdentity(&TokenResponse{AccessToken: "opaque-user-token", IDToken: team})
	require.False(t, ok)
	_, ok = AuthorizationTeamIdentity(&TokenResponse{AccessToken: encode(jwt.MapClaims{"principal_type": "Team"})})
	require.False(t, ok)
}

func TestGrokOAuthStateAndNonceUseV7(t *testing.T) {
	for _, generate := range []func() (string, error){GenerateState, GenerateNonce} {
		first, err := generate()
		require.NoError(t, err)
		second, err := generate()
		require.NoError(t, err)
		require.NotEqual(t, first, second)
		parsed, err := uuid.Parse(first)
		require.NoError(t, err)
		require.Equal(t, uuid.Version(7), parsed.Version())
		require.Equal(t, uuid.RFC4122, parsed.Variant())
	}
}

func TestGrokOIDCOfficialAsymmetricKeyFamilies(t *testing.T) {
	encode := base64.RawURLEncoding.EncodeToString
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	edPublic, edPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	login := OIDCLoginContext{Issuer: OAuthIssuer, ClientID: "client", Nonce: "nonce"}
	for _, tc := range []struct {
		method jwt.SigningMethod
		key    any
		jwk    map[string]string
	}{
		{jwt.SigningMethodPS256, rsaKey, map[string]string{"kty": "RSA", "n": encode(rsaKey.N.Bytes()), "e": encode(big.NewInt(int64(rsaKey.E)).Bytes())}},
		{jwt.SigningMethodES256, p256, map[string]string{"kty": "EC", "crv": "P-256", "x": encode(p256.X.FillBytes(make([]byte, 32))), "y": encode(p256.Y.FillBytes(make([]byte, 32)))}},
		{jwt.SigningMethodES384, p384, map[string]string{"kty": "EC", "crv": "P-384", "x": encode(p384.X.FillBytes(make([]byte, 48))), "y": encode(p384.Y.FillBytes(make([]byte, 48)))}},
		{jwt.SigningMethodEdDSA, edPrivate, map[string]string{"kty": "OKP", "crv": "Ed25519", "x": encode(edPublic)}},
	} {
		t.Run(tc.method.Alg(), func(t *testing.T) {
			token := jwt.NewWithClaims(tc.method, jwt.MapClaims{"iss": login.Issuer, "aud": login.ClientID, "nonce": login.Nonce, "sub": "verified", "exp": time.Now().Add(time.Hour).Unix()})
			token.Header["kid"] = "family-key"
			raw, err := token.SignedString(tc.key)
			require.NoError(t, err)
			tc.jwk["kid"], tc.jwk["use"], tc.jwk["alg"] = "family-key", "sig", tc.method.Alg()
			keys, err := json.Marshal(map[string]any{"keys": []any{tc.jwk}})
			require.NoError(t, err)
			identity, err := VerifyAuthorizationIDToken(raw, login, OIDCDiscovery{Algorithms: []string{tc.method.Alg()}}, keys)
			require.NoError(t, err)
			require.Equal(t, "verified", identity.Subject)
			_, err = VerifyAuthorizationIDToken(raw, login, OIDCDiscovery{Algorithms: []string{"unsupported"}}, keys)
			require.Error(t, err, "the issuer's declared algorithm list is mandatory when present")
			tc.jwk["use"] = "enc"
			keys, err = json.Marshal(map[string]any{"keys": []any{tc.jwk}})
			require.NoError(t, err)
			_, err = VerifyAuthorizationIDToken(raw, login, OIDCDiscovery{}, keys)
			require.Error(t, err, "an encryption-only key cannot authorize login")
		})
	}
}
