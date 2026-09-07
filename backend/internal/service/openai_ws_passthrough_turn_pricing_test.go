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
	expectedTurnStartedAt := time.Date(2026, time.August, 17, 9, 59, 59, 0, time.UTC)
	type hookEvent struct {
		name      string
		turn      int
		startedAt time.Time
	}
	var hookEvents []hookEvent
	hooks := &OpenAIWSIngressHooks{
		InitialTurnStartedAt: expectedTurnStartedAt,
		TurnStarted: func(turn int, startedAt time.Time) {
			hooksMu.Lock()
			hookEvents = append(hookEvents, hookEvent{name: "TurnStarted", turn: turn, startedAt: startedAt})
			hooksMu.Unlock()
		},
		BeforeTurn: func(int) error {
			hooksMu.Lock()
			beforeTurnCalls++
			hooksMu.Unlock()
			return nil
		},
		AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
			hooksMu.Lock()
			hookEvents = append(hookEvents, hookEvent{name: "AfterTurn", turn: turn})
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
	gotBefore := beforeTurnCalls
	gotEvents := append([]hookEvent(nil), hookEvents...)
	hooksMu.Unlock()

	require.Equal(t, 1, gotBefore, "透传 ingress 必须在首个 response.create 前调用 BeforeTurn")
	require.GreaterOrEqual(t, len(gotEvents), 2, "透传 ingress 应报告 TurnStarted 和 AfterTurn")
	require.Equal(t, "TurnStarted", gotEvents[0].name)
	require.Equal(t, expectedTurnStartedAt, gotEvents[0].startedAt, "TurnStarted 必须携带入口冻结的首轮开始时刻")
	require.Equal(t, "AfterTurn", gotEvents[1].name)
	require.Equal(t, gotEvents[0].turn, gotEvents[1].turn, "TurnStarted 后应提交同一 turn 的 AfterTurn")
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

func TestPassthroughIngressAfterTurnSettlesWhenFirstWriteNeverReachesUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)

	upstream := newStagedPassthroughConn()
	upstream.failNextWrite.Store(true)
	var hooksMu sync.Mutex
	afterTurns := 0
	var afterTurnErr error
	hooks := &OpenAIWSIngressHooks{
		BeforeTurn: func(int) error { return nil },
		AfterTurn: func(turn int, result *OpenAIForwardResult, err error) {
			hooksMu.Lock()
			afterTurns++
			afterTurnErr = err
			hooksMu.Unlock()
			require.Equal(t, 1, turn)
			require.Nil(t, result)
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

	select {
	case err := <-serverErr:
		require.Error(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough ingress did not exit after first upstream write failed")
	}

	hooksMu.Lock()
	gotAfter := afterTurns
	gotErr := afterTurnErr
	hooksMu.Unlock()
	require.Equal(t, 1, gotAfter, "BeforeTurn success must still settle AfterTurn when the first write never reaches upstream")
	require.Error(t, gotErr)
}
