//go:build unit

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestQPSWebSocketClosesWhenIPPolicyBecomesBlocked(t *testing.T) {
	completed := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { completed <- struct{}{} }()
		handleQPSWebSocketWithIPAccessInterval(
			request.Context(),
			conn,
			func(context.Context) (bool, error) { return true, nil },
			10*time.Millisecond,
		)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = client.ReadMessage()
	var closeErr *websocket.CloseError
	require.True(t, errors.As(err, &closeErr), "expected policy close, got %v", err)
	require.Equal(t, websocket.ClosePolicyViolation, closeErr.Code)
	require.Contains(t, closeErr.Text, "prohibited")

	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("QPS websocket handler did not exit after policy closure")
	}
}
