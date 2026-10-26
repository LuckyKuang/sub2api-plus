//go:build integration

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pluginv1 "github.com/LuckyKuang/sub2api-plus/pkg/pluginapi/v1"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The installed executable is this test binary. Only the exact production SDK
// magic cookie enters the helper process; ordinary integration runs execute tests.
func TestMain(m *testing.M) {
	if os.Getenv(pluginv1.HandshakeConfig.MagicCookieKey) == pluginv1.HandshakeConfig.MagicCookieValue {
		pluginv1.Serve(&localProcessPluginServer{})
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type localProcessPluginConfig struct {
	HeaderValue string `json:"header_value"`
}
type localProcessPluginStatus struct {
	PID           int    `json:"pid"`
	ConfigApplies int    `json:"config_applies"`
	Forwards      int64  `json:"forwards"`
	AccountID     int64  `json:"account_id"`
	Concurrency   int32  `json:"concurrency"`
	Platform      string `json:"platform"`
	AccountType   string `json:"account_type"`
}
type localProcessPluginServer struct {
	pluginv1.UnimplementedTransportPluginServer
	mu     sync.Mutex
	config localProcessPluginConfig
	state  localProcessPluginStatus
}

func (*localProcessPluginServer) GetInfo(context.Context, *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{PluginId: "example.fixture.transport", PluginVersion: "0.1.0", ProtocolVersion: 1, TransportApiVersion: 1, Capabilities: []string{"openai.oauth.outbound_transport.v1"}}, nil
}
func (s *localProcessPluginServer) Health(context.Context, *pluginv1.HealthRequest) (*pluginv1.HealthResponse, error) {
	s.mu.Lock()
	state := s.state
	s.mu.Unlock()
	state.PID = os.Getpid()
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	return &pluginv1.HealthResponse{Healthy: true, StatusJson: string(data)}, nil
}
func localProcessPluginDecodeConfig(data []byte) (localProcessPluginConfig, error) {
	var cfg localProcessPluginConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return cfg, errors.New("exactly one config object is required")
	}
	if cfg.HeaderValue == "" {
		return cfg, errors.New("header_value is required")
	}
	return cfg, nil
}
func (*localProcessPluginServer) ValidateConfig(_ context.Context, request *pluginv1.ValidateConfigRequest) (*pluginv1.ValidateConfigResponse, error) {
	cfg, err := localProcessPluginDecodeConfig(request.ConfigJson)
	if err != nil {
		return &pluginv1.ValidateConfigResponse{Valid: false, Message: err.Error()}, nil
	}
	normalized, err := json.Marshal(cfg)
	return &pluginv1.ValidateConfigResponse{Valid: true, NormalizedConfigJson: normalized}, err
}
func (s *localProcessPluginServer) ApplyConfig(_ context.Context, request *pluginv1.ApplyConfigRequest) (*pluginv1.ApplyConfigResponse, error) {
	cfg, err := localProcessPluginDecodeConfig(request.ConfigJson)
	if err != nil {
		return &pluginv1.ApplyConfigResponse{Applied: false, Message: err.Error()}, nil
	}
	s.mu.Lock()
	s.config = cfg
	s.state.ConfigApplies++
	s.mu.Unlock()
	return &pluginv1.ApplyConfigResponse{Applied: true}, nil
}
func (s *localProcessPluginServer) Forward(stream pluginv1.TransportPlugin_ForwardServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	start := first.GetStart()
	if start == nil {
		return errors.New("request must start with metadata")
	}
	s.mu.Lock()
	s.state.Forwards++
	s.state.AccountID = start.AccountId
	s.state.Concurrency = start.AccountConcurrency
	s.state.Platform = start.Platform
	s.state.AccountType = start.AccountType
	cfg := s.config
	s.mu.Unlock()
	var body bytes.Buffer
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if chunk := frame.GetBodyChunk(); len(chunk) > 0 {
			_, _ = body.Write(chunk)
			continue
		}
		if frame.GetBodyEnd() {
			break
		}
		return errors.New("unexpected request frame")
	}
	if start.Headers["X-Fixture-Reject"].GetValues() != nil {
		return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "FIXTURE_REJECTED", Message: "fixture rejected before transport", RequestSent: false}}})
	}
	request, err := http.NewRequestWithContext(stream.Context(), start.Method, start.Url, bytes.NewReader(body.Bytes()))
	if err != nil {
		return err
	}
	request.Host = start.Host
	request.ContentLength = start.ContentLength
	for key, values := range start.Headers {
		if values != nil {
			request.Header[key] = append([]string(nil), values.Values...)
		}
	}
	request.Header.Set("X-Fixture-Config", cfg.HeaderValue)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	response, err := (&http.Client{Transport: transport, Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "FIXTURE_UPSTREAM", Message: "local upstream transport failed", RequestSent: true}}})
	}
	defer func() { _ = response.Body.Close() }()
	headers := make(map[string]*pluginv1.HeaderValues, len(response.Header))
	for key, values := range response.Header {
		headers[key] = &pluginv1.HeaderValues{Values: append([]string(nil), values...)}
	}
	if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: int32(response.StatusCode), Status: response.Status, Protocol: response.Proto, ProtocolMajor: int32(response.ProtoMajor), ProtocolMinor: int32(response.ProtoMinor), Headers: headers, ContentLength: response.ContentLength}}}); err != nil {
		return err
	}
	var received int64
	buffer := make([]byte, 8192)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			received += int64(n)
			if err := stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: append([]byte(nil), buffer[:n]...)}}); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return stream.Send(&pluginv1.ForwardResponse{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{BytesReceived: received}}})
		}
		if readErr != nil {
			return readErr
		}
	}
}

type localProcessPluginUploadFailure struct{ closed atomic.Bool }

func (*localProcessPluginUploadFailure) Read([]byte) (int, error) {
	return 0, errors.New("injected local upload read failure")
}
func (b *localProcessPluginUploadFailure) Close() error { b.closed.Store(true); return nil }

func TestLocalSignedPluginRuntimeIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	archive, publicKey, uncompressedSize := localProcessPluginPackage(t)
	cfg := integrationPluginConfig(t.TempDir(), false)
	cfg.Plugins.MaxUploadBytes = int64(len(archive)) + 1024*1024
	cfg.Plugins.MaxUncompressedBytes = uncompressedSize + 1024*1024
	untrusted := NewPluginPackageInstaller(cfg, PluginHostInfo{Version: "0.1.179", BuildType: "release"})
	_, err := untrusted.Install(context.Background(), bytes.NewReader(archive), nil)
	require.ErrorContains(t, err, "不受信任", "the executable cannot be installed before explicitly trusting its ephemeral publisher")
	cfg.Plugins.TrustedPublishers["local-process-fixture-v1"] = publicKey
	installer := NewPluginPackageInstaller(cfg, PluginHostInfo{Version: "0.1.179", BuildType: "release"})
	installation, err := installer.Install(context.Background(), bytes.NewReader(archive), nil)
	require.NoError(t, err)
	require.Equal(t, PluginSignatureTrusted, installation.SignatureStatus)
	require.Equal(t, "example.fixture.transport", installation.PluginKey)
	for _, path := range []string{"ui/index.html", "ui/assets/styles.css", "ui/assets/bridge-v1.js", "ui/assets/app.js"} {
		require.FileExists(t, filepath.Join(installation.InstallPath, filepath.FromSlash(path)))
	}
	socketDir := filepath.Join(t.TempDir(), "rpc")
	require.NoError(t, os.MkdirAll(socketDir, 0700))
	runtime, err := startPluginRuntime(context.Background(), installation, 10*time.Second, socketDir, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		runtime.kill()
		require.Eventually(t, runtime.client.Exited, 3*time.Second, 10*time.Millisecond, "the actual exec child must exit during cleanup")
	})
	status := func(t *testing.T) localProcessPluginStatus {
		t.Helper()
		health, err := runtime.status(context.Background())
		require.NoError(t, err)
		require.True(t, health.Healthy)
		var state localProcessPluginStatus
		require.NoError(t, json.Unmarshal([]byte(health.StatusJson), &state))
		return state
	}
	state := status(t)
	require.Positive(t, state.PID)
	require.NotEqual(t, os.Getpid(), state.PID, "RPC must run in the installed executable's separate OS process")
	info, err := runtime.api.GetInfo(context.Background(), &pluginv1.GetInfoRequest{})
	require.NoError(t, err)
	require.Equal(t, "example.fixture.transport", info.PluginId)
	require.Equal(t, "0.1.0", info.PluginVersion)
	require.Equal(t, uint32(1), info.ProtocolVersion)
	require.Equal(t, uint32(1), info.TransportApiVersion)
	require.Equal(t, []string{"openai.oauth.outbound_transport.v1"}, info.Capabilities)
	require.NoError(t, runtime.checkHealth(context.Background()))
	require.NoError(t, runtime.validateAndApplyConfig(context.Background(), []byte(`{"header_value":"applied-config"}`)))
	require.Error(t, runtime.validateAndApplyConfig(context.Background(), []byte(`{"unknown":true}`)))
	require.Equal(t, 1, status(t).ConfigApplies, "invalid config cannot replace the applied configuration")

	type observedRequest struct {
		method, path, body string
		headers            http.Header
		length             int64
	}
	observed := make(chan observedRequest, 8)
	var upstreamCalls atomic.Int64
	cancelObserved := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		observed <- observedRequest{r.Method, r.URL.RequestURI(), string(body), r.Header.Clone(), r.ContentLength}
		switch r.URL.Path {
		case "/sent-error":
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("local HTTP server must support hijacking")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		case "/cancel":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "started\n")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("local HTTP server must support flushing")
				return
			}
			flusher.Flush()
			<-r.Context().Done()
			close(cancelObserved)
		default:
			w.Header().Set("X-Fixture-Upstream", "response-header")
			w.Header().Set("Content-Length", "22")
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "response-through-child")
		}
	}))
	t.Cleanup(upstream.Close)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2}
	newRequest := func(t *testing.T, ctx context.Context, path string, body io.Reader) *http.Request {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream.URL+path, body)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer local-fixture-token")
		req.Header.Set("Content-Type", "application/json")
		req.Header["X-Fixture-Multi"] = []string{"first", "second"}
		return req
	}
	forward := func(t *testing.T, ctx context.Context, req *http.Request) (*http.Response, error) {
		require.True(t, runtime.beginRequest())
		response, err := runtime.roundTrip(ctx, req, "", account)
		if err != nil {
			runtime.finishRequest()
		}
		return response, err
	}
	noReplay := func(t *testing.T, ctx context.Context, err error) {
		inbound, _ := gin.CreateTestContext(httptest.NewRecorder())
		inbound.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		classified := (&OpenAIGatewayService{}).handleOpenAIUpstreamTransportError(ctx, inbound, account, err, true)
		var failover *UpstreamFailoverError
		require.False(t, errors.As(classified, &failover), "delivered or uncertain requests cannot authorize account replay")
	}
	t.Run("request-response-config-and-passive-health", func(t *testing.T) {
		response, err := forward(t, context.Background(), newRequest(t, context.Background(), "/success?fixture=wire", bytes.NewBufferString("payload")))
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusCreated, response.StatusCode)
		require.Equal(t, "response-header", response.Header.Get("X-Fixture-Upstream"))
		require.Equal(t, "response-through-child", string(body))
		require.Equal(t, int64(22), response.ContentLength)
		request := <-observed
		require.Equal(t, "POST", request.method)
		require.Equal(t, "/success?fixture=wire", request.path)
		require.Equal(t, "payload", request.body)
		require.Equal(t, int64(7), request.length)
		require.Equal(t, []string{"first", "second"}, request.headers.Values("X-Fixture-Multi"))
		require.Equal(t, "Bearer local-fixture-token", request.headers.Get("Authorization"))
		require.Equal(t, "application/json", request.headers.Get("Content-Type"))
		require.Equal(t, "applied-config", request.headers.Get("X-Fixture-Config"))
		before := status(t)
		require.Equal(t, int64(42), before.AccountID)
		require.Equal(t, int32(2), before.Concurrency)
		require.Equal(t, "openai", before.Platform)
		require.Equal(t, "oauth", before.AccountType)
		calls := upstreamCalls.Load()
		require.Equal(t, before, status(t), "Health must not apply config or forward requests")
		require.Equal(t, calls, upstreamCalls.Load())
		require.Zero(t, runtime.inFlight.Load(), "closing a response twice releases its in-flight lease once")
	})
	t.Run("preflight-refusal-sends-no-http-request", func(t *testing.T) {
		before := upstreamCalls.Load()
		req := newRequest(t, context.Background(), "/reject", bytes.NewBufferString("payload"))
		req.Header.Set("X-Fixture-Reject", "true")
		response, err := forward(t, context.Background(), req)
		require.Nil(t, response)
		var transportErr *PluginTransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, "FIXTURE_REJECTED", transportErr.Code)
		require.False(t, transportErr.RequestSent)
		require.Equal(t, before, upstreamCalls.Load())
		require.Zero(t, runtime.inFlight.Load())
	})
	t.Run("delivered-transport-failure-forbids-account-replay", func(t *testing.T) {
		before := upstreamCalls.Load()
		response, err := forward(t, context.Background(), newRequest(t, context.Background(), "/sent-error", bytes.NewBufferString("payload")))
		require.Nil(t, response)
		var transportErr *PluginTransportError
		require.ErrorAs(t, err, &transportErr)
		require.Equal(t, "FIXTURE_UPSTREAM", transportErr.Code)
		require.True(t, transportErr.RequestSent)
		noReplay(t, context.Background(), err)
		require.Equal(t, before+1, upstreamCalls.Load())
		require.Equal(t, "/sent-error", (<-observed).path)
		require.Zero(t, runtime.inFlight.Load())
	})
	t.Run("upload-read-failure-closes-body-without-http-replay", func(t *testing.T) {
		before := upstreamCalls.Load()
		body := &localProcessPluginUploadFailure{}
		req := newRequest(t, context.Background(), "/body-error", body)
		req.ContentLength = -1
		response, err := forward(t, context.Background(), req)
		require.Nil(t, response)
		var transportErr *PluginTransportError
		require.ErrorAs(t, err, &transportErr)
		require.True(t, transportErr.RequestSent, "after metadata delivery the host conservatively forbids replay")
		noReplay(t, context.Background(), err)
		require.Eventually(t, body.closed.Load, time.Second, time.Millisecond)
		require.Equal(t, before, upstreamCalls.Load())
		require.Zero(t, runtime.inFlight.Load())
	})
	t.Run("cancellation-reaches-child-http-and-releases-lease", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		before := upstreamCalls.Load()
		response, err := forward(t, ctx, newRequest(t, ctx, "/cancel", bytes.NewBufferString("payload")))
		require.NoError(t, err)
		prefix := make([]byte, len("started\n"))
		_, err = io.ReadFull(response.Body, prefix)
		require.NoError(t, err)
		require.Equal(t, "started\n", string(prefix))
		cancel()
		_, err = io.ReadAll(response.Body)
		require.Error(t, err)
		noReplay(t, ctx, err)
		require.NoError(t, response.Body.Close())
		select {
		case <-cancelObserved:
		case <-time.After(3 * time.Second):
			t.Fatal("caller cancellation did not reach the child HTTP request")
		}
		require.Equal(t, before+1, upstreamCalls.Load())
		require.Equal(t, "/cancel", (<-observed).path)
		require.Zero(t, runtime.inFlight.Load())
		require.NoError(t, runtime.checkHealth(context.Background()))
	})
	runtime.drain(time.Second)
	require.True(t, runtime.client.Exited())
	require.False(t, runtime.beginRequest())
	require.Error(t, runtime.checkHealth(context.Background()))
}
