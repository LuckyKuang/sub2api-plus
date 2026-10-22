package xai

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// OIDCLoginContext is captured before authorization, never supplied by callback
// fields. Refresh grants have no fresh login nonce and do not use this contract.
type OIDCLoginContext struct {
	Issuer   string
	ClientID string
	Nonce    string
}

type OIDCIdentity struct {
	Subject string
	Email   string
	TeamID  string
}

type OIDCDiscovery struct {
	Issuer     string   `json:"issuer"`
	JWKSURI    string   `json:"jwks_uri"`
	Algorithms []string `json:"id_token_signing_alg_values_supported"`
}

var grokOIDCAlgorithms = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "EdDSA"}

// AuthorizationTeamIdentity implements grok-build's explicit team-principal
// exception. Call only for the access token returned by the trusted token
// exchange, never for a caller-supplied ID token, header or account metadata.
func AuthorizationTeamIdentity(tokens *TokenResponse) (*OIDCIdentity, bool) {
	if tokens == nil {
		return nil, false
	}
	claims := DecodeJWTClaims(tokens.AccessToken)
	principalType := JWTClaimString(claims, "principal_type")
	if principalType == "" {
		principalType = JWTClaimString(claims, "principalType")
	}
	principalID := JWTClaimString(claims, "principal_id")
	if principalID == "" {
		principalID = JWTClaimString(claims, "principalId")
	}
	if principalType != "Team" || strings.TrimSpace(principalID) == "" {
		return nil, false
	}
	return &OIDCIdentity{Subject: principalID, TeamID: principalID}, true
}

type oidcClaims struct {
	jwt.RegisteredClaims
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

type oidcJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// VerifyAuthorizationIDToken verifies only asymmetric algorithms supported by
// grok-build. No unverified identity claim escapes this function on failure.
func VerifyAuthorizationIDToken(raw string, login OIDCLoginContext, discovery OIDCDiscovery, jwks []byte) (*OIDCIdentity, error) {
	if raw == "" || login.Issuer == "" || login.ClientID == "" || login.Nonce == "" {
		return nil, errors.New("incomplete OIDC authorization")
	}
	if discovery.Issuer != "" && discovery.Issuer != login.Issuer {
		return nil, errors.New("OIDC discovery issuer mismatch")
	}
	var keySet struct {
		Keys []oidcJWK `json:"keys"`
	}
	if err := json.Unmarshal(jwks, &keySet); err != nil {
		return nil, errors.New("invalid OIDC key set")
	}
	claims := &oidcClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		kid, ok := token.Header["kid"].(string)
		if !ok || kid == "" {
			return nil, errors.New("missing OIDC key id")
		}
		alg := token.Method.Alg()
		if discovery.Algorithms != nil && !slices.Contains(discovery.Algorithms, alg) {
			return nil, errors.New("OIDC algorithm not declared")
		}
		for _, key := range keySet.Keys {
			if key.Kid == kid && (key.Use == "" || key.Use == "sig") && (key.Alg == "" || key.Alg == alg) {
				return key.publicKey(alg)
			}
		}
		return nil, errors.New("OIDC signing key unavailable")
	}, jwt.WithValidMethods(grokOIDCAlgorithms), jwt.WithIssuer(login.Issuer), jwt.WithAudience(login.ClientID), jwt.WithExpirationRequired())
	if err != nil {
		return nil, errors.New("OIDC signature or claims validation failed")
	}
	if claims.Subject == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(login.Nonce)) != 1 {
		return nil, errors.New("OIDC subject or nonce invalid")
	}
	return &OIDCIdentity{Subject: claims.Subject, Email: claims.Email}, nil
}

func (k oidcJWK) publicKey(alg string) (any, error) {
	decode := func(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }
	switch {
	case k.Kty == "RSA" && (strings.HasPrefix(alg, "RS") || strings.HasPrefix(alg, "PS")):
		n, err := decode(k.N)
		if err != nil || len(n) == 0 {
			return nil, errors.New("invalid RSA modulus")
		}
		e, err := decode(k.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return nil, errors.New("invalid RSA exponent")
		}
		exponent := new(big.Int).SetBytes(e).Int64()
		if exponent < 3 || exponent > 2147483647 || exponent%2 == 0 {
			return nil, errors.New("invalid RSA exponent")
		}
		modulus := new(big.Int).SetBytes(n)
		if modulus.Sign() <= 0 {
			return nil, errors.New("invalid RSA modulus")
		}
		return &rsa.PublicKey{N: modulus, E: int(exponent)}, nil
	case k.Kty == "EC" && (alg == "ES256" || alg == "ES384"):
		var curve elliptic.Curve
		if alg == "ES256" && k.Crv == "P-256" {
			curve = elliptic.P256()
		}
		if alg == "ES384" && k.Crv == "P-384" {
			curve = elliptic.P384()
		}
		if curve == nil {
			return nil, errors.New("invalid EC curve")
		}
		x, ex := decode(k.X)
		y, ey := decode(k.Y)
		width := (curve.Params().BitSize + 7) / 8
		if ex != nil || ey != nil || len(x) != width || len(y) != width {
			return nil, errors.New("invalid EC coordinates")
		}
		encoded := append([]byte{4}, x...)
		encoded = append(encoded, y...)
		return ecdsa.ParseUncompressedPublicKey(curve, encoded)
	case k.Kty == "OKP" && k.Crv == "Ed25519" && alg == "EdDSA":
		x, err := decode(k.X)
		if err != nil || len(x) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 key")
		}
		return ed25519.PublicKey(x), nil
	default:
		return nil, errors.New("OIDC key type does not match algorithm")
	}
}
