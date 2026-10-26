package xai

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
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

// AuthorizationTeamIdentity implements grok-build's explicit team-principal
// exception. Call only for the access token returned by the trusted token
// exchange, never for a caller-supplied ID token, header or account metadata.
func AuthorizationTeamIdentity(tokens *TokenResponse) (*OIDCIdentity, bool) {
	if tokens == nil {
		return nil, false
	}
	principalType, principalID := PeekAccessTokenPrincipal(tokens.AccessToken)
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

type OIDCHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// These asymmetric algorithms match the official Grok Build OIDC allowlist.
var oidcSigningAlgorithms = []string{"RS256", "RS384", "RS512", "PS256", "PS384", "PS512", "ES256", "ES384", "EdDSA"}

// PeekAccessTokenPrincipal mirrors the official unverified principal hint. It
// requires a parseable JWT and both fields; it does not verify an API access
// token locally or replace the upstream server's token authorization.
func PeekAccessTokenPrincipal(rawToken string) (principalType, principalID string) {
	parsed, _, err := jwt.NewParser().ParseUnverified(rawToken, jwt.MapClaims{})
	if err != nil || parsed.Method.Alg() == "none" {
		return "", ""
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return "", ""
	}
	principalType = JWTClaimString(claims, "principal_type")
	if principalType == "" {
		principalType = JWTClaimString(claims, "principalType")
	}
	principalID = JWTClaimString(claims, "principal_id")
	if principalID == "" {
		principalID = JWTClaimString(claims, "principalId")
	}
	if principalType == "" || principalID == "" {
		return "", ""
	}
	return principalType, principalID
}

type oidcDiscovery struct {
	Issuer     string   `json:"issuer"`
	JWKSURI    string   `json:"jwks_uri"`
	Algorithms []string `json:"id_token_signing_alg_values_supported"`
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
	}, jwt.WithValidMethods(oidcSigningAlgorithms), jwt.WithIssuer(login.Issuer), jwt.WithAudience(login.ClientID), jwt.WithExpirationRequired())
	if err != nil {
		return nil, errors.New("OIDC signature or claims validation failed")
	}
	if claims.Subject == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(login.Nonce)) != 1 {
		return nil, errors.New("OIDC subject or nonce invalid")
	}
	return &OIDCIdentity{Subject: claims.Subject, Email: claims.Email}, nil
}

// VerifyOIDCIDToken validates a browser identity against discovery/JWKS fetched
// through the caller's selected transport. No token is sent to either endpoint.
// Runtime callers pin expectedIssuer to OAuthIssuer. Explicit loopback issuers
// support isolated contract tests without changing process URL trust policy.
func VerifyOIDCIDToken(ctx context.Context, client OIDCHTTPClient, expectedIssuer, rawToken, clientID, nonce string) (map[string]any, error) {
	if client == nil || strings.TrimSpace(rawToken) == "" || strings.TrimSpace(clientID) == "" || strings.TrimSpace(nonce) == "" {
		return nil, errors.New("OIDC verification requires client, token, audience and nonce")
	}
	issuer, err := url.Parse(expectedIssuer)
	if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return nil, errors.New("invalid OIDC issuer")
	}
	if issuer.Scheme != "https" {
		ip := net.ParseIP(issuer.Hostname())
		if issuer.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("OIDC issuer requires HTTPS")
		}
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(rawToken, jwt.MapClaims{})
	if err != nil {
		return nil, errors.New("invalid OIDC ID token")
	}
	algorithm := parsed.Method.Alg()
	if !slices.Contains(oidcSigningAlgorithms, algorithm) {
		return nil, errors.New("unsupported OIDC ID token algorithm")
	}
	kid, _ := parsed.Header["kid"].(string)
	if strings.TrimSpace(kid) == "" {
		return nil, errors.New("OIDC ID token is missing key identifier")
	}
	var discovery oidcDiscovery
	if err := oidcFetchJSON(ctx, client, strings.TrimRight(expectedIssuer, "/")+"/.well-known/openid-configuration", &discovery); err != nil {
		return nil, err
	}
	if discovery.Issuer != expectedIssuer {
		return nil, errors.New("OIDC discovery issuer mismatch")
	}
	jwksURL, err := url.Parse(discovery.JWKSURI)
	if err != nil || jwksURL.Scheme != issuer.Scheme || jwksURL.Host != issuer.Host || jwksURL.User != nil || jwksURL.Fragment != "" {
		return nil, errors.New("OIDC discovery contains an untrusted JWKS endpoint")
	}
	if discovery.Algorithms != nil && !slices.Contains(discovery.Algorithms, algorithm) {
		return nil, errors.New("OIDC ID token algorithm is absent from discovery")
	}
	var jwks struct {
		Keys []oidcJWK `json:"keys"`
	}
	if err := oidcFetchJSON(ctx, client, discovery.JWKSURI, &jwks); err != nil {
		return nil, err
	}
	var selected *oidcJWK
	for i := range jwks.Keys {
		candidate := &jwks.Keys[i]
		if candidate.Kid != kid || (candidate.Use != "" && candidate.Use != "sig") || (candidate.Alg != "" && candidate.Alg != algorithm) {
			continue
		}
		if selected != nil {
			return nil, errors.New("OIDC JWKS has ambiguous signing keys")
		}
		selected = candidate
	}
	if selected == nil {
		return nil, errors.New("OIDC signing key not found")
	}
	publicKey, err := selected.publicKey(algorithm)
	if err != nil {
		return nil, err
	}
	verified, err := jwt.Parse(rawToken, func(*jwt.Token) (any, error) { return publicKey, nil }, jwt.WithValidMethods([]string{algorithm}), jwt.WithIssuer(expectedIssuer), jwt.WithAudience(clientID), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || verified == nil || !verified.Valid {
		return nil, errors.New("OIDC ID token signature or claims are invalid")
	}
	claims, ok := verified.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("OIDC ID token claims are invalid")
	}
	subject, _ := claims["sub"].(string)
	issuedAt, err := claims.GetIssuedAt()
	if strings.TrimSpace(subject) == "" || err != nil || issuedAt == nil {
		return nil, errors.New("OIDC ID token is missing required identity claims")
	}
	actualNonce, _ := claims["nonce"].(string)
	if subtle.ConstantTimeCompare([]byte(actualNonce), []byte(nonce)) != 1 {
		return nil, errors.New("OIDC ID token nonce mismatch")
	}
	audience, err := claims.GetAudience()
	if err != nil {
		return nil, errors.New("OIDC ID token audience is invalid")
	}
	azp, _ := claims["azp"].(string)
	if (len(audience) > 1 && azp != clientID) || (azp != "" && azp != clientID) {
		return nil, errors.New("OIDC ID token authorized party mismatch")
	}
	return map[string]any(claims), nil
}

func oidcFetchJSON(ctx context.Context, client OIDCHTTPClient, target string, result any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return errors.New("invalid OIDC metadata request")
	}
	outboundidentity.ApplyDefault(req, "grok")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return errors.New("OIDC metadata transport failed")
	}
	if resp == nil || resp.Body == nil {
		return errors.New("OIDC metadata response is empty")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OIDC metadata HTTP status %d", resp.StatusCode)
	}
	const maxMetadataBytes = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return errors.New("OIDC metadata read failed")
	}
	if len(body) > maxMetadataBytes {
		return errors.New("OIDC metadata response exceeds size limit")
	}
	if err := json.Unmarshal(body, result); err != nil {
		return errors.New("OIDC metadata JSON is invalid")
	}
	return nil
}

func (k oidcJWK) publicKey(algorithm string) (any, error) {
	decode := func(raw string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(raw) }
	switch k.Kty {
	case "RSA":
		if !strings.HasPrefix(algorithm, "RS") && !strings.HasPrefix(algorithm, "PS") {
			return nil, errors.New("OIDC RSA key algorithm mismatch")
		}
		nBytes, nErr := decode(k.N)
		eBytes, eErr := decode(k.E)
		if nErr != nil || eErr != nil || len(nBytes) == 0 || len(eBytes) == 0 || len(eBytes) > 4 {
			return nil, errors.New("invalid OIDC RSA key")
		}
		n := new(big.Int).SetBytes(nBytes)
		e := new(big.Int).SetBytes(eBytes).Int64()
		if n.Sign() <= 0 || e < 3 || e%2 == 0 || e > 1<<31-1 {
			return nil, errors.New("invalid OIDC RSA key")
		}
		return &rsa.PublicKey{N: n, E: int(e)}, nil
	case "EC":
		var curve elliptic.Curve
		if algorithm == "ES256" && k.Crv == "P-256" {
			curve = elliptic.P256()
		}
		if algorithm == "ES384" && k.Crv == "P-384" {
			curve = elliptic.P384()
		}
		if curve == nil {
			return nil, errors.New("OIDC elliptic key algorithm mismatch")
		}
		xBytes, xErr := decode(k.X)
		yBytes, yErr := decode(k.Y)
		size := (curve.Params().BitSize + 7) / 8
		if xErr != nil || yErr != nil || len(xBytes) != size || len(yBytes) != size {
			return nil, errors.New("invalid OIDC elliptic key")
		}
		encoded := append([]byte{4}, xBytes...)
		encoded = append(encoded, yBytes...)
		key, err := ecdsa.ParseUncompressedPublicKey(curve, encoded)
		if err != nil {
			return nil, errors.New("invalid OIDC elliptic key")
		}
		return key, nil
	case "OKP":
		xBytes, err := decode(k.X)
		if algorithm != "EdDSA" || k.Crv != "Ed25519" || err != nil || len(xBytes) != ed25519.PublicKeySize {
			return nil, errors.New("invalid OIDC Edwards key")
		}
		return ed25519.PublicKey(xBytes), nil
	default:
		return nil, errors.New("unsupported OIDC signing key")
	}
}
