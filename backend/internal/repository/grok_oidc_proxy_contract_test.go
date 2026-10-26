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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

type grokOIDCProxyRepo struct {
	service.ProxyRepository
	proxy *service.Proxy
}

func (r grokOIDCProxyRepo) GetByID(context.Context, int64) (*service.Proxy, error) {
	return r.proxy, nil
}

type grokOIDCWireRequest struct{ path, authorization, proxyAuthorization, userAgent, identifier, version, mode, body string }

// Drive the actual browser service and constructor-created repository client.
// The destination is reachable: its TCP count detects fallback even if TLS or
// HTTP would fail. Each discovery/JWKS fault occurs after real earlier stages.
func TestGrokOIDCProductionBrowserProxyFailuresNeverDialDirect(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, stage := range []string{"discovery", "jwks"} {
		for _, failure := range []string{"timeout", "refused", "407", "success"} {
			t.Run(stage+"/"+failure, func(t *testing.T) {
				var directTCP atomic.Int32
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directTCP.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				t.Setenv(xai.EnvAllowUnsafeURLOverrides, "1")
				t.Setenv(xai.EnvTokenURL, destination.URL+"/token")
				t.Setenv("NO_PROXY", "*")
				t.Setenv("no_proxy", "*")
				var mu sync.Mutex
				var sent []grokOIDCWireRequest
				release := make(chan struct{})
				var signed string
				var proxy *httptest.Server
				failedPath := "/.well-known/openid-configuration"
				if stage == "jwks" {
					failedPath = "/jwks"
				}
				proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, readErr := io.ReadAll(r.Body)
					if readErr != nil {
						t.Errorf("read proxy request: %v", readErr)
					}
					mu.Lock()
					sent = append(sent, grokOIDCWireRequest{path: r.URL.Path, body: string(body), authorization: r.Header.Get("Authorization"), proxyAuthorization: r.Header.Get("Proxy-Authorization"), userAgent: r.UserAgent(), identifier: r.Header.Get("x-grok-client-identifier"), version: r.Header.Get("x-grok-client-version"), mode: r.Header.Get("x-grok-client-mode")})
					mu.Unlock()
					if r.URL.Path == failedPath {
						if failure == "timeout" {
							<-release
							return
						}
						if failure == "407" {
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
					}
					previousPath := "/token"
					if stage == "jwks" {
						previousPath = "/.well-known/openid-configuration"
					}
					if failure == "refused" && r.URL.Path == previousPath {
						w.Header().Set("Connection", "close")
						_ = proxy.Listener.Close()
					}
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/token":
						_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "private-browser-access", "refresh_token": "private-browser-refresh", "id_token": signed})
					case "/.well-known/openid-configuration":
						_ = json.NewEncoder(w).Encode(map[string]any{"issuer": destination.URL, "jwks_uri": destination.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
					case "/jwks":
						_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kid": "browser-key", "kty": "RSA", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
					default:
						t.Errorf("unexpected OIDC path: %s", r.URL.Path)
						w.WriteHeader(404)
					}
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { close(release) })
				proxyURL, err := url.Parse(proxy.URL)
				require.NoError(t, err)
				port, err := strconv.Atoi(proxyURL.Port())
				require.NoError(t, err)
				id := int64(981)
				bound := &service.Proxy{ID: id, Protocol: "http", Host: proxyURL.Hostname(), Port: port, Username: "proxy-owner", Password: "private-proxy-password"}
				client := NewGrokOAuthClient().(*grokOAuthClient)
				require.Equal(t, xai.OAuthIssuer, client.oidcIssuer, "runtime issuer is pinned independently of arbitrary token URL overrides")
				client.oidcIssuer = destination.URL // Local issuer seam; transport remains production.
				svc := service.NewGrokOAuthService(grokOIDCProxyRepo{proxy: bound}, client)
				t.Cleanup(svc.Stop)
				start, err := svc.GenerateAuthURL(context.Background(), &id, "")
				require.NoError(t, err)
				authorization, err := url.Parse(start.AuthURL)
				require.NoError(t, err)
				now := time.Now().Unix()
				token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": destination.URL, "aud": authorization.Query().Get("client_id"), "sub": "verified-browser-user", "nonce": authorization.Query().Get("nonce"), "iat": now - 10, "exp": now + 300})
				token.Header["kid"] = "browser-key"
				signed, err = token.SignedString(key)
				require.NoError(t, err)
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				info, err := svc.ExchangeCode(ctx, &service.GrokExchangeCodeInput{SessionID: start.SessionID, State: start.State, Code: "private-browser-code"})
				if failure == "success" {
					require.NoError(t, err)
					require.Equal(t, "verified-browser-user", info.Subject)
				} else {
					require.Error(t, err)
					require.Nil(t, info)
					for _, secret := range []string{"private-browser-code", "private-browser-access", "private-browser-refresh", "private-proxy-password", signed} {
						require.NotContains(t, err.Error(), secret)
					}
				}
				require.Zero(t, directTCP.Load(), "discovery and JWKS cannot fall back to direct TCP after a saved proxy failure")
				mu.Lock()
				observed := append([]grokOIDCWireRequest(nil), sent...)
				mu.Unlock()
				expected := 3
				if failure != "success" {
					expected = 2
					if stage == "jwks" {
						expected = 3
					}
					if failure == "refused" {
						expected--
					}
				}
				require.Len(t, observed, expected, "failure cannot retry via another route or continue to a later OIDC stage")
				for _, request := range observed {
					require.Empty(t, request.authorization, "ID/access/refresh tokens are never sent to metadata endpoints")
					require.Equal(t, "Basic cHJveHktb3duZXI6cHJpdmF0ZS1wcm94eS1wYXNzd29yZA==", request.proxyAuthorization)
					require.Equal(t, "grok-shell/1.0.45 (linux; x86_64)", request.userAgent)
					require.Equal(t, "grok-shell", request.identifier)
					require.Equal(t, "1.0.45", request.version)
					require.Empty(t, request.mode, "the selected CLI identity omits native-host-only mode on every local token and metadata destination")
					if request.path != "/token" {
						require.Empty(t, request.body)
					}
				}
			})
		}
	}
}
