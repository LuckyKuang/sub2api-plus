package codexcli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// BridgeError distinguishes a local bridge failure from provider HTTP results.
// RequestSent means the trusted provider callback has been entered: callers
// must not automatically replay that attempt on another account or transport.
// Error deliberately contains only a stable code; cause remains unwrap-only.
type BridgeError struct {
	Code        string
	RequestSent bool
	cause       error
}

func (e *BridgeError) Error() string { return "Codex CLI bridge failed: " + e.Code }
func (e *BridgeError) Unwrap() error { return e.cause }

// Bridge preserves the already-admitted HTTP Responses request and response
// through the pinned official responses-api-proxy. It does not execute an agent
// task or own account selection, identity, retries, auditing, or conversation state.
// The official proxy accepts only short API-key-shaped stdin credentials; a
// one-use local nonce therefore authenticates its relay, never the OAuth JWT.
// The relay's destination and credentials come exclusively from request/forward.
func Bridge(ctx context.Context, runtime Status, request *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
	return bridgeWithHostCheck(ctx, runtime, request, forward, checkHostConfiguration)
}

func bridgeWithHostCheck(ctx context.Context, runtime Status, request *http.Request, forward func(context.Context, *http.Request) (*http.Response, error), hostCheck func(context.Context) error) (result *http.Response, returnedErr error) {
	var requestSent atomic.Bool
	defer func() {
		if returnedErr == nil {
			return
		}
		code := "bridge_failed"
		var typed *TaskError
		if errors.As(returnedErr, &typed) {
			code = typed.Code
		}
		returnedErr = &BridgeError{Code: code, RequestSent: requestSent.Load(), cause: returnedErr}
	}()
	if !runtime.ExecutionAvailable || !runtime.SourceVerified || !runtime.VersionMatch || (runtime.Source != "managed" && runtime.Source != "system") || runtime.proof == nil || runtime.Path != runtime.proof.path || !filepath.IsAbs(runtime.Path) {
		return nil, taskError("bridge_runtime_unavailable")
	}
	if request == nil || request.URL == nil || request.Method == "" || forward == nil {
		return nil, taskError("bridge_invalid_request")
	}
	if err := ctx.Err(); err != nil {
		return nil, taskError("bridge_cancelled")
	}
	if err := hostCheck(ctx); err != nil {
		return nil, taskError(hostConfigurationErrorCode(err))
	}
	if err := verifyTaskExecutable(ctx, runtime); err != nil {
		return nil, err
	}
	var body []byte
	var err error
	if request.Body != nil {
		body, err = io.ReadAll(request.Body)
		_ = request.Body.Close()
	}
	if err != nil {
		return nil, taskError("bridge_request_read_failed")
	}
	// Leave a reusable body on the original trusted request for caller diagnostics
	// and its existing same-account recovery paths.
	request.Body = io.NopCloser(bytes.NewReader(body))
	wantDigest := sha256.Sum256(body)
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return nil, taskError("bridge_state_unavailable")
	}
	nonce := hex.EncodeToString(secret[:])
	var relayID [16]byte
	if _, err := rand.Read(relayID[:]); err != nil {
		return nil, taskError("bridge_state_unavailable")
	}
	root, err := os.MkdirTemp("", "sub2api-codex-bridge-")
	if err != nil {
		return nil, taskError("bridge_state_unavailable")
	}
	processCtx, cancel := context.WithCancel(ctx)
	var cleanupOnce sync.Once
	var cmd *exec.Cmd
	var processDone <-chan struct{}
	var server *http.Server
	var localTransport *http.Transport
	cleanup := func() {
		cleanupOnce.Do(func() {
			cancel()
			if server != nil {
				_ = server.Close()
			}
			if localTransport != nil {
				localTransport.CloseIdleConnections()
			}
			if processDone != nil {
				<-processDone
			}
			_ = os.RemoveAll(root) //nolint:gosec // root belongs to this invocation's MkdirTemp, never a caller path.
		})
	}
	failed := true
	defer func() {
		if failed {
			cleanup()
		}
	}()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, taskError("bridge_state_unavailable")
	}
	path := "/" + hex.EncodeToString(relayID[:])
	var claimed atomic.Bool
	var relayMu sync.Mutex
	var relayErr error
	relayFinished := make(chan struct{})
	var providerStatus int
	var providerHeader http.Header
	setRelayError := func(err error) { relayMu.Lock(); relayErr = err; relayMu.Unlock() }
	server = &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, incoming *http.Request) {
		// An arbitrary local request must neither choose a provider nor consume
		// this invocation's only admission. Check nonce, exact bytes and path first.
		if incoming.Method != http.MethodPost || incoming.URL.RequestURI() != path || subtle.ConstantTimeCompare([]byte(incoming.Header.Get("Authorization")), []byte("Bearer "+nonce)) != 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		got, readErr := io.ReadAll(io.LimitReader(incoming.Body, int64(len(body))+1))
		if readErr != nil || len(got) != len(body) || sha256.Sum256(got) != wantDigest || !claimed.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		out := request.Clone(processCtx)
		defer close(relayFinished)
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		out.ContentLength = int64(len(body))
		// Retain the original owner snapshot's headers rather than trusting even
		// local proxy header changes, default fingerprints or forwarded values.
		out.Header = request.Header.Clone()
		requestSent.Store(true)
		response, forwardErr := forward(processCtx, out)
		if forwardErr != nil || response == nil || response.Body == nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if forwardErr == nil {
				forwardErr = errors.New("empty trusted bridge response")
			}
			setRelayError(forwardErr)
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		relayMu.Lock()
		providerStatus, providerHeader = response.StatusCode, response.Header.Clone()
		relayMu.Unlock()
		// Provider metadata stays in the trusted side channel. In particular do
		// not expose Content-Encoding to the proxy's HTTP client: its compiled
		// decompression features must never transform a provider response body.
		w.Header().Set("Content-Type", "application/octet-stream")
		// API-key mode of the reviewed proxy follows redirects. Keep its local
		// envelope at 200 so a provider Location cannot make it send the nonce
		// outside this relay. Restore the trusted status/headers below; provider
		// response bytes remain untouched, including errors and redirects.
		w.WriteHeader(http.StatusOK)
		buffer := make([]byte, 32<<10)
		for {
			n, readErr := response.Body.Read(buffer)
			if n > 0 {
				if _, err := w.Write(buffer[:n]); err != nil {
					setRelayError(err)
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			if readErr != nil {
				if !errors.Is(readErr, io.EOF) {
					setRelayError(readErr)
				}
				return
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	infoPath := filepath.Join(root, "server.json")
	cmd = exec.CommandContext(processCtx, runtime.Path, "responses-api-proxy", "--server-info", infoPath, "--upstream-url", "http://"+listener.Addr().String()+path)
	cmd.Dir = root
	cmd.Env = []string{"PATH=" + taskPath, "HOME=" + root, "CODEX_HOME=" + root, "TMPDIR=" + root, "LANG=C.UTF-8", "TERM=dumb", "NO_PROXY=*", "no_proxy=*"}
	cmd.Stdin = strings.NewReader(nonce + "\n")
	cmd.Stdout = io.Discard
	cmd.Stderr = &taskStderr{cancel: cancel}
	if err := configureTaskProcess(cmd); err != nil {
		return nil, taskError("bridge_unsupported_platform")
	}
	if err := cmd.Start(); err != nil {
		return nil, taskError("bridge_process_start_failed")
	}
	done := make(chan struct{})
	processDone = done
	go func() { _ = cmd.Wait(); close(done) }()
	var port int
	readyCtx, readyCancel := context.WithTimeout(processCtx, 5*time.Second)
	defer readyCancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for port == 0 {
		if data, err := os.ReadFile(infoPath); err == nil && len(data) <= 1024 { //nolint:gosec // infoPath is in our fresh private directory and is never caller-selected.
			var info struct {
				Port int `json:"port"`
				PID  int `json:"pid"`
			}
			if json.Unmarshal(data, &info) == nil && info.Port > 0 && info.Port <= 65535 && info.PID == cmd.Process.Pid {
				if err := os.Chmod(infoPath, 0600); err != nil {
					return nil, taskError("bridge_state_unavailable")
				}
				port = info.Port
				break
			}
		}
		select {
		case <-readyCtx.Done():
			return nil, taskError("bridge_startup_timeout")
		case <-done:
			return nil, taskError("bridge_process_unavailable")
		case <-ticker.C:
		}
	}
	local, err := http.NewRequestWithContext(processCtx, http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(port)+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, taskError("bridge_invalid_request")
	}
	local.Header = request.Header.Clone()
	local.Header.Del("Authorization")
	for key := range local.Header {
		if bridgeHopHeader(key) {
			local.Header.Del(key)
		}
	}
	localTransport = &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true}
	client := &http.Client{Transport: localTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(local)
	if err != nil {
		return nil, taskError("bridge_transport_failed")
	}
	relayMu.Lock()
	err = relayErr
	trustedStatus, trustedHeader := providerStatus, providerHeader
	relayMu.Unlock()
	if err != nil {
		_ = response.Body.Close()
		return nil, &TaskError{Code: "bridge_upstream_transport_failed", cause: err}
	}
	if !claimed.Load() {
		_ = response.Body.Close()
		return nil, taskError("bridge_relay_not_accepted")
	}
	if trustedStatus == 0 {
		_ = response.Body.Close()
		return nil, taskError("bridge_relay_response_unavailable")
	}
	response.StatusCode = trustedStatus
	response.Status = strconv.Itoa(trustedStatus) + " " + http.StatusText(trustedStatus)
	response.Header = trustedHeader
	response.ContentLength = -1
	if length, err := strconv.ParseInt(trustedHeader.Get("Content-Length"), 10, 64); err == nil && length >= 0 {
		response.ContentLength = length
	}
	for key := range response.Header {
		if bridgeHopHeader(key) {
			response.Header.Del(key)
		}
	}
	response.Body = &bridgeResponseBody{ReadCloser: response.Body, cleanup: cleanup, terminalError: func() error {
		select {
		case <-relayFinished:
			relayMu.Lock()
			defer relayMu.Unlock()
			return relayErr
		default:
			return errors.New("trusted bridge relay has not completed")
		}
	}}
	failed = false
	go func() { <-processCtx.Done(); cleanup() }()
	return response, nil
}

func bridgeHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

type bridgeResponseBody struct {
	io.ReadCloser
	cleanup       func()
	terminalError func() error
	once          sync.Once
}

func (b *bridgeResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) && b.terminalError != nil {
		if terminal := b.terminalError(); terminal != nil {
			return n, &BridgeError{Code: "bridge_response_read_failed", RequestSent: true, cause: terminal}
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, &BridgeError{Code: "bridge_response_read_failed", RequestSent: true, cause: err}
	}
	return n, err
}
func (b *bridgeResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cleanup)
	return err
}
