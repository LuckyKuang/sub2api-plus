//go:build unit

package service

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	pluginv1 "github.com/LuckyKuang/sub2api-plus/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type diagnosticBudgetPlugin struct {
	pluginv1.UnimplementedTransportPluginServer
	heldCanceled chan struct{}
}

func (server *diagnosticBudgetPlugin) Forward(stream pluginv1.TransportPlugin_ForwardServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if frame.GetBodyEnd() {
			break
		}
	}
	requestURL := first.GetStart().Url
	statusCode := int32(http.StatusOK)
	if strings.HasSuffix(requestURL, "/held") || strings.HasSuffix(requestURL, "/complete-error") {
		statusCode = http.StatusTooManyRequests
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: statusCode}}}); err != nil {
		return err
	}
	if strings.HasSuffix(requestURL, "/held") {
		if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: []byte(`{"error":"rate limited"}`)}}); err != nil {
			return err
		}
		<-stream.Context().Done()
		close(server.heldCanceled)
		return stream.Context().Err()
	}
	if strings.HasSuffix(requestURL, "/slow-success") {
		select {
		case <-time.After(5500 * time.Millisecond):
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: []byte("complete")}}); err != nil {
		return err
	}
	return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{}}})
}

func newDiagnosticBudgetPlugin(t *testing.T) (*pluginRuntime, *diagnosticBudgetPlugin) {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })
	server := grpc.NewServer()
	fixture := &diagnosticBudgetPlugin{heldCanceled: make(chan struct{})}
	pluginv1.RegisterTransportPluginServer(server, fixture)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	connection, err := grpc.NewClient("passthrough:///plugin-diagnostic-budget", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	return &pluginRuntime{api: pluginv1.NewTransportPluginClient(connection), done: make(chan struct{})}, fixture
}

func pluginDiagnosticRequest(t *testing.T, runtime *pluginRuntime, ctx context.Context, path string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/"+path, nil)
	require.NoError(t, err)
	require.True(t, runtime.beginRequest())
	response, err := runtime.roundTrip(ctx, request, "", &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	if err != nil {
		runtime.finishRequest()
	}
	require.NoError(t, err)
	return response
}

// Count-token and Live callers consume the returned body directly. A bounded
// service error helper cannot protect those plugin requests, so the runtime
// must terminate rejected bodies independently of which consumer reads them.
func TestPluginRejectedBodyBudgetStopsRPCAndReleasesRuntimeWithoutCallerClose(t *testing.T) {
	t.Parallel()
	runtime, fixture := newDiagnosticBudgetPlugin(t)
	ctx := context.WithoutCancel(t.Context())
	response := pluginDiagnosticRequest(t, runtime, ctx, "held")
	defer response.Body.Close()
	type readResult struct {
		body []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() { body, err := io.ReadAll(response.Body); done <- readResult{body, err} }()
	select {
	case result := <-done:
		require.Error(t, result.err, "a non-EOF rejected body cannot become a completed response")
		require.Equal(t, `{"error":"rate limited"}`, string(result.body))
	case <-time.After(7 * time.Second):
		_ = response.Body.Close()
		t.Fatal("the plugin retained a rejected body beyond its independent diagnostic budget")
	}
	require.Zero(t, runtime.inFlight.Load(), "timeout must finish the runtime request without waiting for caller Close")
	select {
	case <-fixture.heldCanceled:
	case <-time.After(time.Second):
		t.Fatal("the plugin RPC remained active after its rejected body timed out")
	}
	require.NoError(t, ctx.Err(), "terminating one plugin attempt must not cancel its caller")
	require.NoError(t, response.Body.Close())
	require.NoError(t, response.Body.Close())
	require.Zero(t, runtime.inFlight.Load(), "later Close calls cannot finish twice")
	next := pluginDiagnosticRequest(t, runtime, ctx, "next")
	body, err := io.ReadAll(next.Body)
	require.NoError(t, err)
	require.Equal(t, "complete", string(body))
	require.NoError(t, next.Body.Close())
	require.Zero(t, runtime.inFlight.Load(), "the runtime must admit and finish the next request")
}

func TestPluginSuccessfulBodySurvivesRejectedResponseBudget(t *testing.T) {
	t.Parallel()
	runtime, _ := newDiagnosticBudgetPlugin(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	response := pluginDiagnosticRequest(t, runtime, ctx, "slow-success")
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err, "an accepted stream must survive longer than the diagnostic budget")
	require.Equal(t, "complete", string(body))
	require.NoError(t, response.Body.Close())
	require.Zero(t, runtime.inFlight.Load())
}

func TestPluginCompletedErrorBodyFinishesBeforeCallerClose(t *testing.T) {
	runtime, _ := newDiagnosticBudgetPlugin(t)
	response := pluginDiagnosticRequest(t, runtime, t.Context(), "complete-error")
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "complete", string(body))
	require.Zero(t, runtime.inFlight.Load(), "EOF must stop the timer and finish exactly once")
	require.NoError(t, response.Body.Close())
	require.Zero(t, runtime.inFlight.Load())
}
