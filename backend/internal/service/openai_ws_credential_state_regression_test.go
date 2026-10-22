//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexWSProvenanceHTTPUpstream struct{ client *http.Client }

func (u *codexWSProvenanceHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.client.Do(req)
}

func (u *codexWSProvenanceHTTPUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

// OFFICIAL_AGENT_REQUEST_STATE.md requires provenance from the acquired grant,
// including when a selected row has been relinked before forwarding. Capture
// real native handshakes and HTTP bridge requests with session routing absent:
// the old grant may resume its state, while the replacement may never echo it.
func TestCodexWSIngressProvenanceUsesAcquiredCredentialOwner(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			captured := make(chan http.Header, 3)
			capturedBodies := make(chan []byte, 3)
			completed := []byte(`{"type":"response.completed","response":{"id":"resp_owner_state","status":"completed","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				if mode == OpenAIWSIngressModeHTTPBridge {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Error(err)
						return
					}
					capturedBodies <- body
					w.Header().Set("Content-Type", "text/event-stream")
					w.Header().Set(openAIWSTurnStateHeader, "acquired-owner-state")
					_, _ = w.Write(append(append([]byte("data: "), completed...), []byte("\n\n")...))
					return
				}
				header := http.Header{}
				header.Set(openAIWSTurnStateHeader, "acquired-owner-state")
				conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, header)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				if _, body, err := conn.ReadMessage(); err != nil {
					t.Error(err)
					return
				} else {
					capturedBodies <- body
				}
				if err := conn.WriteMessage(websocket.TextMessage, completed); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()

			cfg := newOpenAIWSExecutionScopeTestConfig()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = mode
			stateStore := NewOpenAIWSStateStore(nil)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &codexWSProvenanceHTTPUpstream{client: upstream.Client()},
				cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector: NewCodexToolCorrector(), openaiWSStateStore: stateStore}
			defer svc.CloseOpenAIWSPool()
			oldOwner := &Account{ID: 501, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "old-acquired-key", "base_url": upstream.URL},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": mode}}
			replacement := snapshotRequestCredentialAccount(oldOwner)
			replacement.Credentials["api_key"] = "replacement-key"
			type attempt struct {
				selected *Account
				owner    *Account
				token    string
			}
			attempts := make(chan attempt, 1)
			results := make(chan error, 1)
			ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current := <-attempts
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					results <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_, first, err := conn.Read(ctx)
				if err != nil {
					results <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r.Clone(ctx)
				c.Set("api_key", &APIKey{ID: 77501})
				forwardCtx := WithOutboundIdentityScope(ctx, c)
				recordRequestCredentialOwner(forwardCtx, current.owner, current.token)
				results <- svc.ProxyResponsesWebSocketFromClient(forwardCtx, c, conn, current.selected, current.token, first, nil)
			}))
			defer ingress.Close()
			oldHeaderThread, oldBodyThread := "", ""
			for index, current := range []attempt{
				{selected: replacement, owner: oldOwner, token: "old-acquired-key"},
				{selected: oldOwner, owner: oldOwner, token: "old-acquired-key"},
				{selected: replacement, owner: replacement, token: "replacement-key"},
			} {
				attempts <- current
				header := http.Header{}
				header.Set("session-id", "credential-owner-session")
				header.Set("thread-id", "credential-owner-thread")
				if index == 2 || (index == 1 && mode == OpenAIWSIngressModeHTTPBridge) {
					header.Set(openAIWSTurnStateHeader, "acquired-owner-state")
				}
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(ingress.URL, "http"), &coderws.DialOptions{HTTPHeader: header})
				require.NoError(t, err)
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","stream":true,"input":"hello","client_metadata":{"thread_id":"legacy-body-thread"}}`)))
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				require.Contains(t, string(event), `"type":"response.completed"`)
				require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
				select {
				case err := <-results:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case sent := <-captured:
					require.Equal(t, "Bearer "+current.token, sent.Get("Authorization"))
					body := <-capturedBodies
					bodyThread := gjson.GetBytes(body, "client_metadata.thread_id").String()
					for _, raw := range []string{sent.Get("thread-id"), bodyThread} {
						id, err := uuid.Parse(raw)
						require.NoError(t, err)
						require.Equal(t, uuid.Version(7), id.Version())
					}
					switch index {
					case 0:
						oldHeaderThread, oldBodyThread = sent.Get("thread-id"), bodyThread
					case 1:
						require.Equal(t, oldHeaderThread, sent.Get("thread-id"), "header IDs follow the acquired old grant")
						require.Equal(t, oldBodyThread, bodyThread, "body IDs follow the same acquired grant")
					default:
						require.NotEqual(t, oldHeaderThread, sent.Get("thread-id"))
						require.NotEqual(t, oldBodyThread, bodyThread)
					}
					if index == 1 {
						require.Equal(t, "acquired-owner-state", sent.Get(openAIWSTurnStateHeader), "same transmitted grant must resume its observed state")
					} else {
						require.Empty(t, sent.Get(openAIWSTurnStateHeader), "a replacement grant cannot inherit old opaque state")
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if mode == OpenAIWSIngressModeCtxPool {
					connID, ok := stateStore.GetResponseConn(oldOwner.ID, "resp_owner_state")
					require.True(t, ok)
					svc.getOpenAIWSConnPool().evictConn(oldOwner.ID, connID)
				}
			}
		})
	}
}

type codexWSAuditIDStore struct {
	mu     sync.Mutex
	values map[string]string
	calls  atomic.Int64
}

func (s *codexWSAuditIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	s.calls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := namespace + ":" + digest
	if value := s.values[key]; value != "" {
		return value, nil
	}
	s.values[key] = candidate
	return candidate, nil
}

// Security audit must precede mapping-table writes on every accepted turn,
// including a follow-up whose legacy metadata needs durable UUID conversion.
// A rejected second turn must not prepare IDs, acquire billing/concurrency, or
// write another HTTP request/WS payload; its original unknown siblings still
// reach the hook alongside recognized user content.
func TestCodexWSFollowupAuditPrecedesPersistentIdentityMapping(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var upstreamWrites atomic.Int64
			completed := []byte(`{"type":"response.completed","response":{"id":"resp_audited_first","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == OpenAIWSIngressModeHTTPBridge {
					_, _ = io.Copy(io.Discard, r.Body)
					upstreamWrites.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write(append(append([]byte("data: "), completed...), []byte("\n\n")...))
					return
				}
				conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				for {
					if _, _, err := conn.ReadMessage(); err != nil {
						return
					}
					upstreamWrites.Add(1)
					if err := conn.WriteMessage(websocket.TextMessage, completed); err != nil {
						return
					}
				}
			}))
			defer upstream.Close()
			cfg := newOpenAIWSExecutionScopeTestConfig()
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.IngressModeDefault = mode
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &codexWSProvenanceHTTPUpstream{client: upstream.Client()},
				cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
			defer svc.CloseOpenAIWSPool()
			account := &Account{ID: 502, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "audit-order-key", "base_url": upstream.URL},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": mode}}
			store := &codexWSAuditIDStore{values: map[string]string{}}
			auditDenied := errors.New("second turn rejected by security audit")
			audited := make(chan []byte, 1)
			firstFinished := make(chan struct{}, 1)
			results := make(chan error, 1)
			var beforeTurns atomic.Int64
			ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					results <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_, first, err := conn.Read(ctx)
				if err != nil {
					results <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r.Clone(ctx)
				c.Set("api_key", &APIKey{ID: 77502})
				hooks := &OpenAIWSIngressHooks{
					BeforeRequest: func(turn int, payload []byte, _ string) error {
						if turn == 2 {
							audited <- append([]byte(nil), payload...)
							return auditDenied
						}
						return nil
					},
					BeforeTurn: func(int) error { beforeTurns.Add(1); return nil },
					AfterTurn: func(turn int, _ *OpenAIForwardResult, err error) {
						if turn == 1 && err == nil {
							firstFinished <- struct{}{}
						}
					},
				}
				results <- svc.ProxyResponsesWebSocketFromClient(WithOutboundIDStore(ctx, store), c, conn, account, "audit-order-key", first, hooks)
			}))
			defer ingress.Close()
			client := dialOpenAIWSExecutionScopeClient(t, ingress.URL, "audit-order-thread")
			defer func() { _ = client.CloseNow() }()
			writeOpenAIWSExecutionScopeRequest(t, client, `{"type":"response.create","model":"gpt-5.1","input":"allowed first"}`)
			_, event, err := client.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
			select {
			case <-firstFinished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			baselineCalls := store.calls.Load()
			require.Positive(t, baselineCalls, "the allowed first turn exercises durable identity preparation")
			denied := []byte(`{"type":"response.create","model":"gpt-5.1","client_metadata":{"thread_id":"new-legacy-thread","turn_id":"new-legacy-turn","x-codex-turn-metadata":"{\"parent_thread_id\":\"new-legacy-parent\"}"},"input":[{"type":"future_item","opaque":true},{"role":"user","content":[{"type":"input_text","text":"deny this recognized sibling"}]}],"future_options":{"opaque":true}}`)
			require.NoError(t, client.Write(ctx, coderws.MessageText, denied))
			select {
			case err := <-results:
				require.ErrorIs(t, err, auditDenied)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.JSONEq(t, string(denied), string(<-audited), "audit must receive the original recognized and unknown fields")
			require.Equal(t, baselineCalls, store.calls.Load(), "a blocked follow-up cannot access or write persistent ID mappings")
			require.EqualValues(t, 1, beforeTurns.Load(), "blocked content cannot enter the next turn's billing/concurrency acquisition")
			require.EqualValues(t, 1, upstreamWrites.Load(), "blocked content cannot trigger a second upstream request or payload")
		})
	}
}

// V18/V19: isolate all execution resources, including deletion. Legacy entries
// without an owner cannot satisfy the new lookup contract.
func TestCodexWSOwnedStateRegression(t *testing.T) {
	s, ok := NewOpenAIWSStateStore(nil).(*defaultOpenAIWSStateStore)
	require.True(t, ok)
	s.BindSessionTurnState(101, 7, "thread", "a", time.Hour)
	s.BindSessionConn(101, 7, "thread", "conn-a", time.Hour)
	s.BindResponseConn(101, "resp_a", "conn-a", time.Hour)
	_, ok = s.GetSessionTurnState(102, 7, "thread")
	require.False(t, ok)
	_, ok = s.GetSessionConn(102, 7, "thread")
	require.False(t, ok)
	_, ok = s.GetResponseConn(102, "resp_a")
	require.False(t, ok)
	s.DeleteSessionTurnState(102, 7, "thread")
	s.DeleteSessionConn(102, 7, "thread")
	s.DeleteResponseConn(102, "resp_a")
	state, ok := s.GetSessionTurnState(101, 7, "thread")
	require.True(t, ok)
	require.Equal(t, "a", state)
	s.sessionToTurnState["7:legacy"] = openAIWSTurnStateBinding{turnState: "legacy", expiresAt: time.Now().Add(time.Hour)}
	_, ok = s.GetSessionTurnState(101, 7, "legacy")
	require.False(t, ok)
}

// Capture the real HTTP -> WS handshake. Both an internal backfill and a
// client echo minted by A must be absent when B opens its authenticated socket.
func TestCodexWSStateWireIsolationRegression(t *testing.T) {
	for _, echo := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached", true: "client-echo"}[echo], func(t *testing.T) {
			captured := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() { _ = conn.Close() }()
				if _, _, err = conn.ReadMessage(); err != nil {
					t.Error(err)
					return
				}
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_b","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`))
			}))
			defer server.Close()
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.APIKeyEnabled, cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true, true, true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			store := NewOpenAIWSStateStore(nil)
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSStateStore: store}
			a := &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			b := &Account{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "key-b", "base_url": server.URL}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
			body := []byte(`{"model":"gpt-5.1","stream":false,"input":"hello"}`)
			c := newOpenAIRejectedFieldTestContext(body)
			c.Set("api_key", &APIKey{ID: 77})
			c.Request.Header.Set("session-id", "session")
			c.Request.Header.Set("thread-id", "thread")
			scope, _ := resolveOpenAIWSExecutionScope(c, body, 77)
			store.BindSessionTurnState(101, 0, scope, "state-a", time.Hour)
			store.BindSessionConn(101, 0, scope, "conn-a", time.Hour)
			if echo {
				c.Request.Header.Set(openAIWSTurnStateHeader, "state-a")
				svc.noteOpenAICodexTurnStateProvenance(c, a, "state-a")
			}
			_, err := svc.Forward(context.Background(), c, b, body)
			require.NoError(t, err)
			h := <-captured
			require.Empty(t, h.Get(openAIWSTurnStateHeader))
			require.Equal(t, "Bearer key-b", h.Get("Authorization"))
			svc.getOpenAIWSConnPool().Close()
		})
	}
}

// A reference plus a text delta has no complete-history proof, even without
// function_call_output. Conversely an observed full root turn seeds replay.
func TestCodexOpaqueContinuationReplayRegression(t *testing.T) {
	_, complete, err := buildOpenAIWSReplayInputSequence(nil, false, []byte(`{"previous_response_id":"resp_a","input":"next"}`), true)
	require.NoError(t, err)
	require.False(t, complete)
	_, complete, err = buildOpenAIWSReplayInputSequence(nil, false, []byte(`{"input":"root"}`), false)
	require.NoError(t, err)
	require.True(t, complete)
}

// V19: sibling threads sharing one session and independent guardian lanes must
// not overwrite each other's provenance; body-only Desktop metadata also works.
func TestCodexTurnStateSiblingProvenanceRegression(t *testing.T) {
	svc := &OpenAIGatewayService{}
	parent, _ := newTurnStateTestContext(t, 7, "shared")
	parent.Request.Header.Set("thread-id", "parent")
	child, _ := newTurnStateTestContext(t, 7, "shared")
	stageOpenAICodexTurnStateScope(child, []byte(`{"client_metadata":{"thread_id":"child"}}`))
	svc.noteOpenAICodexTurnStateProvenance(parent, turnStateAccount(101, "parent"), "private-state")
	svc.noteOpenAICodexTurnStateProvenance(child, turnStateAccount(102, "child"), "private-state")
	for _, test := range []struct {
		c     *gin.Context
		owner int64
	}{{parent, 101}, {child, 102}} {
		h := http.Header{}
		h.Set(openAIWSTurnStateHeader, "private-state")
		ownerName := "parent"
		if test.owner == 102 {
			ownerName = "child"
		}
		svc.guardOpenAICodexTurnStateEcho(test.c, turnStateAccount(test.owner, ownerName), h)
		require.Equal(t, "private-state", h.Get(openAIWSTurnStateHeader))
		svc.guardOpenAICodexTurnStateEcho(test.c, &Account{ID: 103}, h)
		require.Empty(t, h.Get(openAIWSTurnStateHeader))
	}
	bodyOnly, _ := newTurnStateTestContext(t, 7, "")
	stageOpenAICodexTurnStateScope(bodyOnly, []byte(`{"client_metadata":{"thread_id":"body-only"}}`))
	require.NotEmpty(t, openAICodexTurnStateSeed(bodyOnly))
}
