package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProxyResponsesWebSocketFromClient_ClientProfileRejectionPrecedesUpstreamWork(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serviceCtx, cancelService := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelService()

	// There is intentionally no configured upstream dialer. The typed policy
	// close below proves the ingress gate returns before protocol resolution or
	// any upstream connection work.
	svc := &OpenAIGatewayService{codexDetector: &stubCodexRestrictionDetector{result: CodexClientRestrictionDetectionResult{
		Enabled: true,
		Matched: false,
		Reason:  CodexClientRestrictionReasonNotMatchedProfile,
	}}}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_cli_only": true},
	}
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		conn, err := coderws.Accept(w, request, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		messageType, firstMessage, err := ReadOpenAIWSClientMessage(
			serviceCtx,
			conn,
			2*time.Second,
			coderws.StatusPolicyViolation,
			"missing first response.create message",
		)
		if err != nil {
			serverErr <- err
			return
		}
		if messageType != coderws.MessageText {
			serverErr <- errors.New("first message was not text")
			return
		}

		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		ginCtx.Request = request.Clone(serviceCtx)
		ginCtx.Request.Header = request.Header.Clone()

		proxyErr := svc.ProxyResponsesWebSocketFromClient(serviceCtx, ginCtx, conn, account, "access-token", firstMessage, nil)
		var closeErr *OpenAIWSClientCloseError
		if !errors.As(proxyErr, &closeErr) {
			serverErr <- proxyErr
			return
		}
		if closeErr.StatusCode() != coderws.StatusPolicyViolation {
			serverErr <- errors.New("unexpected client policy close status")
			return
		}
		if err := conn.Close(closeErr.StatusCode(), closeErr.Reason()); err != nil {
			serverErr <- err
			return
		}
		serverErr <- proxyErr
	}))
	defer server.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 2*time.Second)
	err = clientConn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.2"}`))
	cancelWrite()
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(context.Background(), 2*time.Second)
	_, _, err = clientConn.Read(readCtx)
	cancelRead()
	var closeErr coderws.CloseError
	require.True(t, errors.As(err, &closeErr), "expected client policy close, got %T: %v", err, err)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	require.Contains(t, closeErr.Reason, CodexOfficialClientsOnlyMessage)

	select {
	case proxyErr := <-serverErr:
		var policyErr *OpenAIWSClientCloseError
		require.True(t, errors.As(proxyErr, &policyErr))
		require.Equal(t, coderws.StatusPolicyViolation, policyErr.StatusCode())
	case <-time.After(2 * time.Second):
		t.Fatal("client profile policy gate did not complete")
	}
}
