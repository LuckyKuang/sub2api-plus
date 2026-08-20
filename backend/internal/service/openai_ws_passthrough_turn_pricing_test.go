package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// startPassthroughHookRecordingServer 与 startPassthroughLifecycleServer 相同，
// 但把一组会记录调用的 hooks 传给 ingress，用于观察透传路径的 turn 回调。
func startPassthroughHookRecordingServer(
	t *testing.T,
	controlCtx context.Context,
	svc *OpenAIGatewayService,
	account *Account,
	hooks *OpenAIWSIngressHooks,
) (*httptest.Server, <-chan error) {
	t.Helper()
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		msgType, firstMessage, err := ReadOpenAIWSClientMessage(
			controlCtx,
			conn,
			3*time.Second,
			coderws.StatusPolicyViolation,
			"missing first response.create message",
		)
		if err != nil {
			serverErr <- err
			return
		}
		if msgType != coderws.MessageText {
			serverErr <- errors.New("first message was not text")
			return
		}

		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		req := r.Clone(controlCtx)
		req.Header = req.Header.Clone()
		ginCtx.Request = req
		serverErr <- svc.ProxyResponsesWebSocketFromClient(controlCtx, ginCtx, conn, account, "sk-test", firstMessage, hooks)
	}))
	return server, serverErr
}

// TestPassthroughIngressCallsBeforeTurn pins the lifecycle contract shared by
// every ingress mode: BeforeTurn runs before the first response.create reaches
// upstream, and AfterTurn runs after its terminal event.
func TestPassthroughIngressCallsBeforeTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)

	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_pricing","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)

	var hooksMu sync.Mutex
	beforeTurnCalls := 0
	afterTurnCalls := 0
	hooks := &OpenAIWSIngressHooks{
		BeforeTurn: func(int) error {
			hooksMu.Lock()
			beforeTurnCalls++
			hooksMu.Unlock()
			return nil
		},
		AfterTurn: func(int, *OpenAIForwardResult, error) {
			hooksMu.Lock()
			afterTurnCalls++
			hooksMu.Unlock()
		},
	}

	server, serverErr := startPassthroughHookRecordingServer(
		t,
		controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(),
		hooks,
	)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())

	// 等待连接自然结束（inter-turn idle 超时），确保 AfterTurn 已提交。
	_, _ = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough ingress did not exit")
	}

	hooksMu.Lock()
	gotBefore, gotAfter := beforeTurnCalls, afterTurnCalls
	hooksMu.Unlock()

	require.Equal(t, 1, gotBefore, "透传 ingress 必须在首个 response.create 前调用 BeforeTurn")
	require.Positive(t, gotAfter, "透传 ingress 仍应回调 AfterTurn 提交用量")
}

func TestPassthroughIngressBeforeTurnRejectsNextTurnBeforeUpstreamWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)

	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	hookErr := errors.New("account removed from group")
	var hookMu sync.Mutex
	hookOrder := make([]string, 0, 4)
	hooks := &OpenAIWSIngressHooks{
		MapRequestModel: func(turn int, model string) (string, error) {
			hookMu.Lock()
			hookOrder = append(hookOrder, fmt.Sprintf("map:%d", turn))
			hookMu.Unlock()
			return model, nil
		},
		BeforeTurn: func(turn int) error {
			hookMu.Lock()
			hookOrder = append(hookOrder, fmt.Sprintf("before:%d", turn))
			hookMu.Unlock()
			if turn > 1 {
				return hookErr
			}
			return nil
		},
	}

	server, serverErr := startPassthroughHookRecordingServer(
		t,
		controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(),
		hooks,
	)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	require.NotEmpty(t, requirePassthroughUpstreamWrite(t, upstream, 3*time.Second))
	completed, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_first"}`))
	cancelWrite()
	require.NoError(t, err)

	select {
	case err = <-serverErr:
		require.ErrorIs(t, err, hookErr)
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough ingress did not stop after BeforeTurn rejected the next turn")
	}
	select {
	case payload := <-upstream.writes:
		t.Fatalf("rejected turn reached upstream: %s", payload)
	case <-time.After(200 * time.Millisecond):
	}
	hookMu.Lock()
	gotHookOrder := append([]string(nil), hookOrder...)
	hookMu.Unlock()
	require.Equal(t, []string{"map:1", "before:1", "map:2", "before:2"}, gotHookOrder,
		"passthrough must resolve the current turn model before durable turn eligibility runs")
}
