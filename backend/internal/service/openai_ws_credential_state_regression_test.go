//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

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
				svc.noteOpenAICodexTurnStateProvenance(c, a)
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
	svc.noteOpenAICodexTurnStateProvenance(parent, &Account{ID: 101})
	svc.noteOpenAICodexTurnStateProvenance(child, &Account{ID: 102})
	for _, test := range []struct {
		c     *gin.Context
		owner int64
	}{{parent, 101}, {child, 102}} {
		h := http.Header{}
		h.Set(openAIWSTurnStateHeader, "private-state")
		svc.guardOpenAICodexTurnStateEcho(test.c, &Account{ID: test.owner}, h)
		require.Equal(t, "private-state", h.Get(openAIWSTurnStateHeader))
		svc.guardOpenAICodexTurnStateEcho(test.c, &Account{ID: 103}, h)
		require.Empty(t, h.Get(openAIWSTurnStateHeader))
	}
	bodyOnly, _ := newTurnStateTestContext(t, 7, "")
	stageOpenAICodexTurnStateScope(bodyOnly, []byte(`{"client_metadata":{"thread_id":"body-only"}}`))
	require.NotEmpty(t, openAICodexTurnStateSeed(bodyOnly))
}
