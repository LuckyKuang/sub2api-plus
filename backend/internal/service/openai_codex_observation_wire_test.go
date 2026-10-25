//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexWireObservationAttempt struct {
	owner         *Account
	accepted      bool
	unconditional bool
	updates       map[string]any
}

// This test oracle compares the independently stored live account and exact
// captured grant. It deliberately does not call the production digest/CAS code.
type codexWireObservationRepo struct {
	AccountRepository
	mu       sync.Mutex
	current  *Account
	attempts chan codexWireObservationAttempt
}

func (r *codexWireObservationRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return snapshotRequestCredentialAccount(r.current), nil
}

func (r *codexWireObservationRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.attempts <- codexWireObservationAttempt{unconditional: true, updates: updates}
	return errors.New("quota evidence must not bypass the transmitted-owner guard")
}

func (r *codexWireObservationRepo) UpdateOpenAICodexUsageIfUnchanged(_ context.Context, id int64, owner *Account, updates map[string]any) (bool, error) {
	r.mu.Lock()
	current := r.current
	accepted := current != nil && owner != nil && id == current.ID && owner.ID == current.ID &&
		owner.Platform == current.Platform && owner.Type == current.Type && owner.Status == current.Status &&
		reflect.DeepEqual(owner.Credentials, current.Credentials) && reflect.DeepEqual(owner.ProxyID, current.ProxyID) && reflect.DeepEqual(owner.Proxy, current.Proxy)
	r.mu.Unlock()
	r.attempts <- codexWireObservationAttempt{owner: snapshotRequestCredentialAccount(owner), accepted: accepted, updates: updates}
	return accepted, nil
}

func (r *codexWireObservationRepo) replaceWhileRequestIsInFlight(change string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	replacement := snapshotRequestCredentialAccount(r.current)
	switch change {
	case "new login":
		replacement.Credentials["access_token"] = "new-token"
		replacement.Credentials["chatgpt_account_id"] = "new-principal"
	case "proxy edited":
		replacement.Proxy.Host = "replacement.invalid"
	case "proxy removed":
		replacement.ProxyID, replacement.Proxy = nil, nil
	case "account disabled":
		replacement.Status = StatusDisabled
	}
	r.current = replacement
}

func codexWireObservationAccount(t *testing.T, proxyURL string, passthrough bool) *Account {
	t.Helper()
	parsed, err := url.Parse(proxyURL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)
	account := codexMetadataOwnerAccount("old", passthrough)
	account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
	account.Proxy = &Proxy{ID: 912, Protocol: "http", Host: parsed.Hostname(), Port: port, Status: StatusActive}
	account.ProxyID = &account.Proxy.ID
	return account
}

func codexWireObservationUpstream() *codexModelsHTTPUpstreamStub {
	return &codexModelsHTTPUpstreamStub{do: func(request *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = "http", "codex-wire.invalid", "codex-wire.invalid"
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxy)}
		defer transport.CloseIdleConnections()
		return (&http.Client{Transport: transport}).Do(redirected)
	}}
}

func requireCodexWireObservation(t *testing.T, repo *codexWireObservationRepo, change string, old *Account) {
	t.Helper()
	select {
	case attempt := <-repo.attempts:
		require.False(t, attempt.unconditional, "a late response must not invoke unconditional UpdateExtra")
		require.NotNil(t, attempt.owner)
		require.Equal(t, old.Credentials, attempt.owner.Credentials, "persist the grant actually authenticated on the wire")
		require.Equal(t, old.Proxy, attempt.owner.Proxy, "persist the exact proxy used on the wire")
		require.Equal(t, 77.0, attempt.updates["codex_5h_used_percent"])
		require.Equal(t, change == "unchanged", attempt.accepted, "replaced accounts retain their existing quota state")
	case <-time.After(3 * time.Second):
		t.Fatal("the actual upstream response never reached the captured-owner persistence guard")
	}
}

// Admin changes occur after the old authenticated request reaches a real HTTP
// proxy but before its response is returned. Each adapter must still present the
// old grant to persistence, while accepting unchanged responses normally.
func TestCodexHTTPQuotaObservationUsesActualWireOwner(t *testing.T) {
	for _, adapter := range []string{"responses", "passthrough", "chat-responses-bridge", "messages-bridge"} {
		for _, change := range []string{"unchanged", "new login", "proxy edited", "proxy removed", "account disabled"} {
			t.Run(adapter+"/"+change, func(t *testing.T) {
				var repo *codexWireObservationRepo
				wire := make(chan http.Header, 1)
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					wire <- r.Header.Clone()
					repo.replaceWhileRequestIsInFlight(change)
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("X-Codex-Primary-Used-Percent", "77")
					w.Header().Set("X-Codex-Primary-Window-Minutes", "300")
					_, _ = io.WriteString(w, "event: response.completed\ndata: "+`{"type":"response.completed","response":{"id":"resp_observation","object":"response","status":"completed","model":"gpt-5.4","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n\n")
				}))
				defer proxy.Close()
				account := codexWireObservationAccount(t, proxy.URL, adapter == "passthrough")
				repo = &codexWireObservationRepo{current: snapshotRequestCredentialAccount(account), attempts: make(chan codexWireObservationAttempt, 2)}
				svc := newOpenAIRejectedFieldTestService(nil)
				svc.accountRepo, svc.httpUpstream = repo, codexWireObservationUpstream()
				svc.codexSnapshotThrottle = newAccountWriteThrottle(0)
				svc.openAITokenProvider = NewOpenAITokenProvider(nil, &codexMetadataOwnerTokenCache{acquire: func(ctx context.Context) string {
					recordRequestCredentialOwner(ctx, account, "old-token")
					return "old-token"
				}}, nil)
				body := []byte(`{"model":"gpt-5.4","input":"hello","instructions":"be helpful","stream":false}`)
				if adapter == "chat-responses-bridge" {
					body = []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
				} else if adapter == "messages-bridge" {
					body = []byte(`{"model":"gpt-5.4","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				}
				c := newOpenAIRejectedFieldTestContext(body)
				ctx := WithOutboundIdentityScope(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()), c)
				var err error
				switch adapter {
				case "chat-responses-bridge":
					c.Request.URL.Path = "/v1/chat/completions"
					_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				case "messages-bridge":
					c.Request.URL.Path = "/v1/messages"
					_, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
				default:
					_, err = svc.Forward(ctx, c, account, body)
				}
				require.NoError(t, err)
				require.Equal(t, "Bearer old-token", (<-wire).Get("Authorization"))
				requireCodexWireObservation(t, repo, change, account)
			})
		}
	}
}

// Admin connection/compaction probes use the same observation contract as real
// inference. A successful old probe may report its result, but cannot update the
// replacement account or merge stale quota into the mutable selected row.
func TestCodexAdminProbeObservationUsesActualWireOwner(t *testing.T) {
	for _, mode := range []string{"connection", "compact"} {
		for _, change := range []string{"unchanged", "new login", "proxy edited", "proxy removed", "account disabled"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				var repo *codexWireObservationRepo
				wire := make(chan http.Header, 1)
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					wire <- r.Header.Clone()
					repo.replaceWhileRequestIsInFlight(change)
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set("X-Codex-Primary-Used-Percent", "77")
					w.Header().Set("X-Codex-Primary-Window-Minutes", "300")
					body := "data: {\"type\":\"response.completed\"}\n\n"
					if mode == "compact" {
						body = compactProbeSSESuccessBody
					}
					_, _ = io.WriteString(w, body)
				}))
				defer proxy.Close()
				account := codexWireObservationAccount(t, proxy.URL, false)
				repo = &codexWireObservationRepo{current: snapshotRequestCredentialAccount(account), attempts: make(chan codexWireObservationAttempt, 2)}
				svc := &AccountTestService{accountRepo: repo, httpUpstream: codexWireObservationUpstream(), cfg: codexTransportTestConfig()}
				c, recorder := newTestContext()
				c.Request = c.Request.WithContext(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()))
				var err error
				if mode == "compact" {
					err = svc.testOpenAICompactConnection(c, account, "gpt-5.4")
				} else {
					err = svc.testOpenAIAccountConnection(c, account, "gpt-5.4", "", "")
				}
				require.NoError(t, err)
				require.Contains(t, recorder.Body.String(), "test_complete")
				require.Equal(t, "Bearer old-token", (<-wire).Get("Authorization"))
				requireCodexWireObservation(t, repo, change, account)
				if change == "unchanged" {
					require.Equal(t, 77.0, account.Extra["codex_5h_used_percent"])
				} else {
					require.NotContains(t, account.Extra, "codex_5h_used_percent", "rejected observations must not mutate the selected account in memory")
				}
			})
		}
	}
}

// The handler consumes each turn after the service restores Gin's prior
// request context. Captured result evidence must survive that boundary for all
// four native ingress modes, rather than borrowing the handler's new account.
func TestCodexWSQuotaResultUsesActualWireOwner(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeDedicated, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		for _, change := range []string{"unchanged", "new login", "proxy edited", "proxy removed", "account disabled"} {
			t.Run(mode+"/"+change, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				var repo *codexWireObservationRepo
				wire := make(chan http.Header, 1)
				completed := []byte(`{"type":"response.completed","response":{"id":"resp_observation","status":"completed","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`)
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					wire <- r.Header.Clone()
					w.Header().Set("X-Codex-Primary-Used-Percent", "77")
					w.Header().Set("X-Codex-Primary-Window-Minutes", "300")
					if mode == OpenAIWSIngressModeHTTPBridge {
						_, _ = io.Copy(io.Discard, r.Body)
						repo.replaceWhileRequestIsInFlight(change)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = w.Write(append(append([]byte("data: "), completed...), []byte("\n\n")...))
						return
					}
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer func() { _ = conn.CloseNow() }()
					if _, _, err := conn.Read(ctx); err != nil {
						t.Error(err)
						return
					}
					repo.replaceWhileRequestIsInFlight(change)
					if err := conn.Write(ctx, coderws.MessageText, completed); err != nil {
						t.Error(err)
						return
					}
					_, _, _ = conn.Read(ctx)
				}))
				defer proxy.Close()
				account := codexWireObservationAccount(t, proxy.URL, false)
				account.Extra["responses_websockets_v2_enabled"] = true
				account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
				repo = &codexWireObservationRepo{current: snapshotRequestCredentialAccount(account), attempts: make(chan codexWireObservationAttempt, 2)}
				cfg := newOpenAIWSExecutionScopeTestConfig()
				cfg.Gateway.OpenAIWS.ModeRouterV2Enabled, cfg.Gateway.OpenAIWS.IngressModeDefault = true, mode
				dialer := &acquiredOwnerRedirectingWSDialer{base: newDefaultOpenAIWSClientDialer()}
				svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, httpUpstream: codexWireObservationUpstream(),
					openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector(),
					openaiWSStateStore: NewOpenAIWSStateStore(nil), openaiWSPassthroughDialer: dialer, codexSnapshotThrottle: newAccountWriteThrottle(0)}
				svc.getOpenAIWSConnPool().setClientDialerForTest(dialer)
				defer svc.CloseOpenAIWSPool()
				turns := make(chan *OpenAIForwardResult, 1)
				finished := make(chan error, 1)
				ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						finished <- err
						return
					}
					defer func() { _ = conn.CloseNow() }()
					_, first, err := conn.Read(ctx)
					if err != nil {
						finished <- err
						return
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = r.Clone(ctx)
					c.Set("api_key", &APIKey{ID: 9912})
					forwardCtx := WithOutboundIdentityScope(WithOutboundIDStore(ctx, newTestOutboundIDStore()), c)
					recordRequestCredentialOwner(forwardCtx, account, "old-token")
					finished <- svc.ProxyResponsesWebSocketFromClient(forwardCtx, c, conn, account, "old-token", first, &OpenAIWSIngressHooks{
						AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
							if err != nil {
								t.Error(err)
							}
							turns <- result
						},
					})
				}))
				defer ingress.Close()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(ingress.URL, "http"), nil)
				require.NoError(t, err)
				defer func() { _ = client.CloseNow() }()
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4","stream":true,"input":"hello"}`)))
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				require.Contains(t, string(event), "response.completed")
				require.Equal(t, "Bearer old-token", (<-wire).Get("Authorization"))
				var result *OpenAIForwardResult
				select {
				case result = <-turns:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				require.NotNil(t, result)
				require.Equal(t, mode != OpenAIWSIngressModeHTTPBridge, result.ResponseHeadersFromHandshake)
				current, err := repo.GetByID(context.Background(), account.ID)
				require.NoError(t, err)
				svc.ApplyCodexUsageSnapshotFromResult(context.Background(), current, result)
				requireCodexWireObservation(t, repo, change, account)
				require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
				select {
				case err := <-finished:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			})
		}
	}
}
