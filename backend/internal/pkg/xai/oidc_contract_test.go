//go:build unit

package xai

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Authority: grok-build 2bdd1d6a, oidc/protocol.rs validate_and_extract_user_info
// and OIDC Core 1.0 sections 2/3.1.3.7. Claims/signatures are independently built.
func TestOIDCIDTokenCryptographicAndClaimBoundaries(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, mode := range []string{"valid", "wrong-issuer", "wrong-audience", "expired", "missing-expiry", "future-issued-at", "missing-issued-at", "missing-subject", "missing-nonce", "wrong-nonce", "missing-kid", "unknown-kid", "wrong-signature", "symmetric-algorithm", "none-algorithm", "unadvertised-algorithm", "wrong-jwk-algorithm", "encryption-jwk", "empty-jwks", "bad-discovery-issuer", "untrusted-jwks", "multiple-audience-no-azp", "multiple-audience-valid-azp", "wrong-azp"} {
		t.Run(mode, func(t *testing.T) {
			var issuer string
			requests := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/.well-known/openid-configuration" {
					algs := []string{"RS256"}
					if mode == "unadvertised-algorithm" {
						algs = []string{"ES256"}
					}
					discoveryIssuer := issuer
					if mode == "bad-discovery-issuer" {
						discoveryIssuer = "https://other.example"
					}
					jwks := issuer + "/jwks"
					if mode == "untrusted-jwks" {
						jwks = "https://attacker.example/jwks"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": discoveryIssuer, "jwks_uri": jwks, "id_token_signing_alg_values_supported": algs})
					return
				}
				if r.URL.Path != "/jwks" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				jwk := map[string]any{"kty": "RSA", "kid": "official-key", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}
				if mode == "wrong-jwk-algorithm" {
					jwk["alg"] = "RS512"
				}
				if mode == "encryption-jwk" {
					jwk["use"] = "enc"
				}
				keys := []any{jwk}
				if mode == "empty-jwks" {
					keys = nil
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
			}))
			t.Cleanup(server.Close)
			issuer = server.URL
			now := time.Now().Unix()
			claims := jwt.MapClaims{"iss": issuer, "aud": "browser-client", "sub": "user-42", "nonce": "login-nonce", "iat": now - 10, "exp": now + 300, "email": "verified@example.com"}
			method := jwt.SigningMethodRS256
			signer := any(key)
			var signingMethod jwt.SigningMethod = method
			switch mode {
			case "wrong-issuer":
				claims["iss"] = "https://other.example"
			case "wrong-audience":
				claims["aud"] = "other-client"
			case "expired":
				claims["exp"] = now - 120
			case "missing-expiry":
				delete(claims, "exp")
			case "future-issued-at":
				claims["iat"] = now + 300
			case "missing-issued-at":
				delete(claims, "iat")
			case "missing-subject":
				delete(claims, "sub")
			case "missing-nonce":
				delete(claims, "nonce")
			case "wrong-nonce":
				claims["nonce"] = "other-login-nonce"
			case "wrong-signature":
				signer = otherKey
			case "symmetric-algorithm":
				signingMethod = jwt.SigningMethodHS256
				signer = []byte("public-rsa-key-must-not-be-a-hmac-secret")
			case "none-algorithm":
				signingMethod = jwt.SigningMethodNone
				signer = jwt.UnsafeAllowNoneSignatureType
			case "multiple-audience-no-azp":
				claims["aud"] = []string{"browser-client", "other-client"}
			case "multiple-audience-valid-azp":
				claims["aud"] = []string{"browser-client", "other-client"}
				claims["azp"] = "browser-client"
			case "wrong-azp":
				claims["azp"] = "other-client"
			}
			token := jwt.NewWithClaims(signingMethod, claims)
			token.Header["kid"] = "official-key"
			if mode == "missing-kid" {
				delete(token.Header, "kid")
			}
			if mode == "unknown-kid" {
				token.Header["kid"] = "unknown-key"
			}
			signed, err := token.SignedString(signer)
			require.NoError(t, err)
			verified, err := VerifyOIDCIDToken(context.Background(), server.Client(), issuer, signed, "browser-client", "login-nonce")
			if mode == "valid" || mode == "multiple-audience-valid-azp" {
				require.NoError(t, err)
				require.Equal(t, "user-42", verified["sub"])
				require.Equal(t, "verified@example.com", verified["email"])
				require.Equal(t, []string{"/.well-known/openid-configuration", "/jwks"}, requests)
			} else {
				require.Error(t, err)
				require.Nil(t, verified)
				require.NotContains(t, err.Error(), signed)
				require.NotContains(t, err.Error(), "login-nonce")
				if mode == "bad-discovery-issuer" || mode == "untrusted-jwks" {
					require.Equal(t, []string{"/.well-known/openid-configuration"}, requests)
				}
			}
		})
	}
}

func TestOIDCIDTokenOfficialAsymmetricAlgorithms(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, algorithm := range []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "EdDSA"} {
		t.Run(algorithm, func(t *testing.T) {
			jwk := map[string]any{"kid": "algorithm-key", "use": "sig", "alg": algorithm}
			var signer any
			switch algorithm {
			case "ES256", "ES384":
				curve := elliptic.P256()
				curveName := "P-256"
				if algorithm == "ES384" {
					curve = elliptic.P384()
					curveName = "P-384"
				}
				key, err := ecdsa.GenerateKey(curve, rand.Reader)
				require.NoError(t, err)
				signer = key
				size := (curve.Params().BitSize + 7) / 8
				jwk["kty"], jwk["crv"] = "EC", curveName
				jwk["x"], jwk["y"] = base64.RawURLEncoding.EncodeToString(key.X.FillBytes(make([]byte, size))), base64.RawURLEncoding.EncodeToString(key.Y.FillBytes(make([]byte, size)))
			case "EdDSA":
				public, key, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				signer = key
				jwk["kty"], jwk["crv"], jwk["x"] = "OKP", "Ed25519", base64.RawURLEncoding.EncodeToString(public)
			default:
				signer = rsaKey
				jwk["kty"] = "RSA"
				jwk["n"] = base64.RawURLEncoding.EncodeToString(rsaKey.N.Bytes())
				jwk["e"] = base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaKey.E)).Bytes())
			}
			var issuer string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": issuer + "/jwks", "id_token_signing_alg_values_supported": []string{algorithm}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk}})
			}))
			t.Cleanup(server.Close)
			issuer = server.URL
			now := time.Now().Unix()
			token := jwt.NewWithClaims(jwt.GetSigningMethod(algorithm), jwt.MapClaims{"iss": issuer, "aud": "client", "sub": "user-42", "iat": now - 1, "exp": now + 300, "nonce": "nonce-42"})
			token.Header["kid"] = "algorithm-key"
			signed, err := token.SignedString(signer)
			require.NoError(t, err)
			claims, err := VerifyOIDCIDToken(context.Background(), server.Client(), issuer, signed, "client", "nonce-42")
			require.NoError(t, err)
			require.Equal(t, "user-42", claims["sub"])
		})
	}
}

func TestOIDCNonceSurvivesRedisCrossInstanceCallback(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	writer, reader := NewRedisSessionStore(rdb), NewRedisSessionStore(rdb)
	t.Cleanup(writer.Stop)
	t.Cleanup(reader.Stop)
	// JSON models persisted state without depending on any constructor defaults.
	var original OAuthSession
	require.NoError(t, json.Unmarshal([]byte(`{"nonce":"random-browser-nonce","state":"random-state","created_at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`), &original))
	writer.Set("flow", &original)
	restored, ok := reader.Get("flow")
	require.True(t, ok)
	serialized, err := json.Marshal(restored)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(serialized, &fields))
	require.Equal(t, "random-browser-nonce", fields["nonce"])
	require.True(t, reader.TryConsumeSession("flow"))
	require.False(t, writer.TryConsumeSession("flow"))
}
