//go:build unit || !integration

package repository

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type codeDeviceProxyRepository struct {
	service.ProxyRepository
	proxy *service.Proxy
}

func (r codeDeviceProxyRepository) GetByID(context.Context, int64) (*service.Proxy, error) {
	return r.proxy, nil
}

type codeDevicePhysicalRequest struct {
	path, contentType, body, userAgent, originator, version, proxyAuth string
	readErr                                                            error
}

// Official Codex login/src/device_code_auth.rs performs usercode -> token poll
// -> authorization-code exchange. Every physical stage must retain the chosen
// proxy; a failed configured route never authorizes a direct TCP connection.
// The raw client contract comes from Codex login/src/server.rs/token exchange:
// ordered form fields for exchange, JSON device requests and no identity headers.
func TestOpenAICodeAndDeviceProxyStagesNeverDialDirect(t *testing.T) {
	for _, stage := range []string{"authorization-code", "device-start", "device-poll", "device-exchange"} {
		for _, route := range []string{"missing", "mismatched", "invalid", "timeout", "refused", "407", "proxy", "unbound"} {
			t.Run(stage+"/"+route, func(t *testing.T) {
				var directConnections, directRequests atomic.Int32
				var mu sync.Mutex
				var requests []codeDevicePhysicalRequest
				respond := func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/deviceauth/usercode":
						_, _ = io.WriteString(w, `{"device_auth_id":"device-private-id","user_code":"USER-CODE","interval":1}`)
					case "/deviceauth/token":
						_, _ = io.WriteString(w, `{"authorization_code":"device-code-secret","code_verifier":"device-verifier-secret"}`)
					default:
						_, _ = io.WriteString(w, `{"access_token":"issued-access","refresh_token":"issued-refresh","token_type":"Bearer","expires_in":3600}`)
					}
				}
				destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					directRequests.Add(1)
					respond(w, r)
				}))
				destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateNew {
						directConnections.Add(1)
					}
				}
				destination.Start()
				t.Cleanup(destination.Close)
				// Explicit routing must win even when process defaults request bypass.
				t.Setenv("NO_PROXY", "*")
				t.Setenv("no_proxy", "*")
				failedPath := "/oauth/token"
				if stage == "device-start" {
					failedPath = "/deviceauth/usercode"
				}
				if stage == "device-poll" {
					failedPath = "/deviceauth/token"
				}
				release := make(chan struct{})
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					mu.Lock()
					requests = append(requests, codeDevicePhysicalRequest{
						path: r.URL.Path, contentType: r.Header.Get("Content-Type"), body: string(body), readErr: err,
						userAgent: r.UserAgent(), originator: r.Header.Get("Originator"), version: r.Header.Get("Version"), proxyAuth: r.Header.Get("Proxy-Authorization"),
					})
					mu.Unlock()
					if r.URL.Path == failedPath {
						if route == "timeout" {
							<-release
							return
						}
						if route == "407" {
							w.WriteHeader(http.StatusProxyAuthRequired)
							return
						}
					}
					respond(w, r)
				}))
				t.Cleanup(proxy.Close)
				t.Cleanup(func() { close(release) })
				proxyURL, err := url.Parse(proxy.URL)
				require.NoError(t, err)
				port, err := strconv.Atoi(proxyURL.Port())
				require.NoError(t, err)
				proxyID := int64(961)
				bound := &service.Proxy{ID: proxyID, Protocol: "http", Host: proxyURL.Hostname(), Port: port, Status: service.StatusActive, Username: "proxy-user", Password: "proxy-password"}
				switch route {
				case "missing":
					bound = nil
				case "mismatched":
					bound.ID++
				case "invalid":
					bound.Protocol = "file"
				case "refused":
					listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, listenErr)
					refusedURL, parseErr := url.Parse("http://" + listener.Addr().String())
					require.NoError(t, parseErr)
					bound.Port, err = strconv.Atoi(refusedURL.Port())
					require.NoError(t, err)
					require.NoError(t, listener.Close())
				}
				client := &openaiOAuthService{tokenURL: destination.URL + "/oauth/token", deviceAuthAPIBase: destination.URL}
				svc := service.NewOpenAIOAuthService(codeDeviceProxyRepository{proxy: bound}, client)
				t.Cleanup(svc.Stop)
				svc.SetPrivacyClientFactory(func(string) (*req.Client, error) { return nil, errors.New("no enrichment in login routing contract") })
				selection := &proxyID
				if route == "unbound" {
					selection = nil
				}
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				var token *service.OpenAITokenInfo
				var pending bool
				// A refused listener cannot first supply a device authorization.
				// Exercise each independent raw client entry directly for this case;
				// timeout/407 below run the complete preceding device stages.
				if route == "refused" && stage == "device-poll" {
					_, pending, err = client.PollDeviceCode(ctx, bound.URL(), "device-private-id", "USER-CODE")
				} else if route == "refused" && stage == "device-exchange" {
					_, err = client.ExchangeCode(ctx, "device-code-secret", "device-verifier-secret", openai.DeviceCodeRedirectURI, bound.URL(), openai.ClientID)
				} else if stage == "authorization-code" {
					started, startErr := svc.GenerateAuthURL(ctx, selection, "", service.PlatformOpenAI)
					err = startErr
					if err == nil {
						authorizationURL, parseErr := url.Parse(started.AuthURL)
						require.NoError(t, parseErr)
						token, err = svc.ExchangeCode(ctx, &service.OpenAIExchangeCodeInput{SessionID: started.SessionID, State: authorizationURL.Query().Get("state"), Code: "authorization-code-secret"})
					}
				} else {
					started, startErr := svc.StartDeviceCode(ctx, selection, service.PlatformOpenAI)
					err = startErr
					if err == nil && stage != "device-start" {
						token, pending, err = svc.PollDeviceCode(ctx, started.SessionID)
					}
				}
				if route == "proxy" || route == "unbound" {
					require.NoError(t, err)
					if stage != "device-start" {
						require.NotNil(t, token)
						require.Equal(t, "issued-access", token.AccessToken)
						require.False(t, pending)
					}
				} else {
					require.Error(t, err, "a failed or unresolvable configured proxy cannot complete login")
					require.Nil(t, token)
					require.False(t, pending, "proxy failures are not authorization-pending responses")
					for _, secret := range []string{"authorization-code-secret", "device-code-secret", "device-verifier-secret", "device-private-id", "proxy-password"} {
						require.NotContains(t, err.Error(), secret)
					}
				}
				if route == "unbound" {
					require.Positive(t, directRequests.Load())
				} else {
					require.Zero(t, directRequests.Load(), "all device stages retain the selected route")
					require.Zero(t, directConnections.Load(), "no direct TCP connection is permitted after configured-proxy failure")
				}
				mu.Lock()
				observed := append([]codeDevicePhysicalRequest(nil), requests...)
				mu.Unlock()
				if route == "missing" || route == "mismatched" || route == "invalid" || route == "refused" || route == "unbound" {
					require.Empty(t, observed)
				} else {
					expected := 1
					if stage == "device-poll" {
						expected = 2
						if route == "proxy" {
							expected = 3
						}
					}
					if stage == "device-exchange" {
						expected = 3
					}
					require.Len(t, observed, expected, "later stages cannot run after a failed send, and no stage retries through another route")
				}
				for _, sent := range observed {
					require.NoError(t, sent.readErr)
					require.Empty(t, sent.userAgent)
					require.Empty(t, sent.originator)
					require.Empty(t, sent.version)
					require.Equal(t, "Basic cHJveHktdXNlcjpwcm94eS1wYXNzd29yZA==", sent.proxyAuth)
					switch sent.path {
					case "/deviceauth/usercode":
						require.Contains(t, sent.contentType, "application/json")
						require.JSONEq(t, `{"client_id":"app_EMoamEEZ73f0CkXaXp7hrann"}`, sent.body)
					case "/deviceauth/token":
						require.JSONEq(t, `{"device_auth_id":"device-private-id","user_code":"USER-CODE"}`, sent.body)
					case "/oauth/token":
						require.Contains(t, sent.contentType, "application/x-www-form-urlencoded")
						form, parseErr := url.ParseQuery(sent.body)
						require.NoError(t, parseErr)
						require.Len(t, form, 5)
						require.Equal(t, "authorization_code", form.Get("grant_type"))
						require.Equal(t, "app_EMoamEEZ73f0CkXaXp7hrann", form.Get("client_id"))
						if stage == "authorization-code" {
							require.Equal(t, "authorization-code-secret", form.Get("code"))
							require.Equal(t, "http://localhost:1455/auth/callback", form.Get("redirect_uri"))
							require.NotEmpty(t, form.Get("code_verifier"))
						} else {
							require.Equal(t, "device-code-secret", form.Get("code"))
							require.Equal(t, "device-verifier-secret", form.Get("code_verifier"))
							require.Equal(t, "https://auth.openai.com/deviceauth/callback", form.Get("redirect_uri"))
						}
					default:
						t.Fatalf("unexpected login path: %s", sent.path)
					}
				}
			})
		}
	}
}
