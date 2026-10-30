//go:build unit

package codexcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The acceptance contract is a transparent official HTTP hop: exact arbitrary
// Responses bytes, trusted owner identity, one provider write, no agent tools,
// no credential export, cancellation cleanup and no client-controlled target.
// The independent fixture models rust-v0.165.0 responses-api-proxy/src/lib.rs,
// including its restricted stdin token format and startup file.
const bridgeFixtureProgram = `#!/usr/bin/env python3
import http.server,json,os,re,sys,urllib.request,urllib.error
assert sys.argv[1]=='responses-api-proxy'
args=dict(zip(sys.argv[2::2],sys.argv[3::2]))
token=sys.stdin.readline().strip()
assert re.fullmatch('[A-Za-z0-9_-]{1,1000}',token)
assert all(k not in os.environ for k in ['HTTP_PROXY','HTTPS_PROXY','ALL_PROXY','OPENAI_API_KEY'])
assert token not in ' '.join(sys.argv)
target=args['--upstream-url']
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
class Handler(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args): pass
 def do_POST(self):
  body=self.rfile.read(int(self.headers['Content-Length']))
  headers=dict(self.headers)
  headers['Authorization']='Bearer '+token
  for k in ['Host','Connection','Content-Length']: headers.pop(k,None)
  try:
   req=urllib.request.Request(target,body,headers,method='POST')
   response=opener.open(req)
  except urllib.error.HTTPError as err: response=err
  self.send_response(response.status)
  for k,v in response.headers.items():
   if k.lower() not in ['connection','transfer-encoding','content-length','server','date']: self.send_header(k,v)
  self.send_header('Connection','close')
  self.end_headers()
  while True:
   chunk=response.read(1)
   if not chunk: break
   self.wfile.write(chunk)
   self.wfile.flush()
server=http.server.HTTPServer(('127.0.0.1',0),Handler)
with open(args['--server-info'],'w') as f: json.dump({'port':server.server_port,'pid':os.getpid()},f)
server.serve_forever()
`

func bridgeFixture(t *testing.T) Status {
	return bridgeProgramFixture(t, bridgeFixtureProgram)
}

func bridgeProgramFixture(t *testing.T, program string) Status {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("bridge process groups support Linux/macOS")
	}
	path := filepath.Join(t.TempDir(), "official-proxy-fixture")
	content := []byte(program)
	if err := os.WriteFile(path, content, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	return Status{Path: path, Installed: true, Source: "managed", SourceVerified: true, VersionMatch: true, ExecutionAvailable: true, proof: &runtimeProof{path: path, executableSHA256: digest[:]}}
}

// A close-delimited local response can end cleanly when the child exits even
// though the provider is still streaming. That EOF must never imply a completed
// provider request or authorize a replay against another owner.
func TestCodexBridgeProxyExitBeforeProviderEOFIsTerminal(t *testing.T) {
	for _, afterByte := range []bool{false, true} {
		t.Run(map[bool]string{false: "before first client byte", true: "after first client byte"}[afterByte], func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			loop := "  while True:\n   chunk=response.read(1)\n   if not chunk: break\n   self.wfile.write(chunk)\n   self.wfile.flush()"
			fault := "  chunk=response.read(1)\n"
			if afterByte {
				fault += "  self.wfile.write(chunk)\n  self.wfile.flush()\n"
			}
			fault += "  os._exit(0)"
			program := strings.Replace(bridgeFixtureProgram, loop, fault, 1)
			if program == bridgeFixtureProgram {
				t.Fatal("proxy fault fixture was not installed")
			}
			fixture := bridgeProgramFixture(t, program)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.invalid/v1/responses", strings.NewReader(`{"input":"hello"}`))
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			providerStopped := make(chan struct{})
			res, err := bridgeWithHostCheck(ctx, fixture, req, func(ctx context.Context, _ *http.Request) (*http.Response, error) {
				calls.Add(1)
				reader, writer := io.Pipe()
				go func() {
					defer close(providerStopped)
					_, _ = writer.Write([]byte("x"))
					<-ctx.Done()
					_ = writer.CloseWithError(ctx.Err())
				}()
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
			}, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			got, readErr := io.ReadAll(res.Body)
			var terminal *BridgeError
			if !errors.As(readErr, &terminal) || !terminal.RequestSent || terminal.Code != "bridge_response_read_failed" {
				t.Fatalf("premature local EOF must retain terminal sent state: %v", readErr)
			}
			want := ""
			if afterByte {
				want = "x"
			}
			if string(got) != want {
				t.Fatalf("response bytes = %q, want %q", got, want)
			}
			if err := res.Body.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-providerStopped:
			case <-ctx.Done():
				t.Fatal("provider stream did not stop after bridge cleanup")
			}
			if calls.Load() != 1 {
				t.Fatalf("provider request replayed: %d calls", calls.Load())
			}
			entries, err := os.ReadDir(temporary)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("bridge temporary resources leaked: %v", entries)
			}
		})
	}
}

func TestCodexBridgePreservesResponsesBytesOwnerAndStatusWithoutReplay(t *testing.T) {
	for _, status := range []int{200, 302, 307, 401, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			runtime := bridgeFixture(t)
			t.Setenv("HTTP_PROXY", "http://host-proxy.invalid")
			t.Setenv("OPENAI_API_KEY", "host-secret-must-not-inherit")
			body := `{"model":"gpt-6.1-sol","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}],"tools":[{"type":"function","name":"caller_tool","parameters":{}}],"previous_response_id":"resp_caller","future":{"preserve":true}}`
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses?trusted=1", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer owner.jwt.with.dots")
			req.Header.Set("User-Agent", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color")
			req.Header.Set("Originator", "codex_cli_rs")
			req.Header.Set("Version", "0.165.0")
			req.Header.Set("Chatgpt-Account-Id", "owner-A")
			var calls atomic.Int32
			responseBody := "event: response.completed\ndata: {\"type\":\"response.completed\",\"future\":true}\n\n"
			res, err := bridgeWithHostCheck(t.Context(), runtime, req, func(ctx context.Context, actual *http.Request) (*http.Response, error) {
				calls.Add(1)
				if actual.URL.String() != req.URL.String() || actual.Header.Get("Authorization") != "Bearer owner.jwt.with.dots" || actual.Header.Get("Chatgpt-Account-Id") != "owner-A" || actual.Header.Get("User-Agent") != req.Header.Get("User-Agent") || actual.Header.Get("Originator") != "codex_cli_rs" || actual.Header.Get("Version") != "0.165.0" {
					t.Error("trusted owner or destination changed")
				}
				got, _ := io.ReadAll(actual.Body)
				if string(got) != body {
					t.Error("Responses payload changed")
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"provider-1"}, "Location": {"http://must-never-connect.invalid/steal-nonce"}}, Body: io.NopCloser(strings.NewReader(responseBody))}, nil
			}, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := res.Body.Close(); err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != status || string(got) != responseBody || res.Header.Get("X-Request-Id") != "provider-1" || res.Header.Get("Location") != "http://must-never-connect.invalid/steal-nonce" || calls.Load() != 1 {
				t.Fatalf("wire contract failed: status=%d calls=%d", res.StatusCode, calls.Load())
			}
			left, err := filepath.Glob(filepath.Join(temporary, "sub2api-codex-bridge-*"))
			if err != nil || len(left) != 0 {
				t.Fatal("response close left private bridge state")
			}
		})
	}
}

func TestCodexBridgeRejectsUnverifiedRuntimeBeforeReadingRequest(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://provider.invalid/responses", strings.NewReader("original"))
	_, err := Bridge(t.Context(), Status{ExecutionAvailable: true, SourceVerified: true, VersionMatch: true, Path: "/untrusted", Source: "system"}, req, func(context.Context, *http.Request) (*http.Response, error) {
		t.Fatal("provider side effect")
		return nil, nil
	})
	assertTaskCode(t, err, "bridge_runtime_unavailable")
	var bridgeErr *BridgeError
	if !errors.As(err, &bridgeErr) || bridgeErr.RequestSent {
		t.Fatal("local runtime failure misclassified as provider write")
	}
	got, _ := io.ReadAll(req.Body)
	if string(got) != "original" {
		t.Fatal("invalid runtime consumed request")
	}
}

func TestCodexBridgeRejectsTamperedProxyPayloadBeforeProviderWrite(t *testing.T) {
	program := strings.Replace(bridgeFixtureProgram, "body=self.rfile.read(int(self.headers['Content-Length']))", "body=self.rfile.read(int(self.headers['Content-Length']))+b'changed'", 1)
	status := bridgeProgramFixture(t, program)
	req, _ := http.NewRequest(http.MethodPost, "https://provider.invalid/responses", strings.NewReader(`{"input":"original"}`))
	_, err := bridgeWithHostCheck(t.Context(), status, req, func(context.Context, *http.Request) (*http.Response, error) {
		t.Error("untrusted bytes reached provider")
		return nil, nil
	}, func(context.Context) error { return nil })
	var bridgeErr *BridgeError
	if !errors.As(err, &bridgeErr) || bridgeErr.RequestSent || bridgeErr.Code != "bridge_relay_not_accepted" {
		t.Fatalf("wrong tamper failure: %v", err)
	}
}

func TestCodexBridgeCancellationClosesProviderContextAndDoesNotReplay(t *testing.T) {
	runtime := bridgeFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.invalid/responses", strings.NewReader(`{"input":"one"}`))
	started := make(chan struct{})
	stopped := make(chan struct{})
	finished := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		_, err := bridgeWithHostCheck(ctx, runtime, req, func(ctx context.Context, _ *http.Request) (*http.Response, error) {
			calls.Add(1)
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		}, func(context.Context) error { return nil })
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("provider context survived cancellation")
	}
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge process survived cancellation")
	}
	if calls.Load() != 1 {
		t.Fatal("cancellation replayed request")
	}
}

func TestCodexBridgeTransportErrorRetainsCauseWithoutDiagnostics(t *testing.T) {
	runtime := bridgeFixture(t)
	req, _ := http.NewRequest(http.MethodPost, "https://provider.invalid/responses", strings.NewReader(`{"input":"one"}`))
	secret := errors.New("secret provider token and content")
	_, err := bridgeWithHostCheck(t.Context(), runtime, req, func(context.Context, *http.Request) (*http.Response, error) { return nil, secret }, func(context.Context) error { return nil })
	assertTaskCode(t, err, "bridge_upstream_transport_failed")
	if !errors.Is(err, secret) || strings.Contains(err.Error(), "secret") {
		t.Fatal("transport cause lost or exposed")
	}
	var bridgeErr *BridgeError
	if !errors.As(err, &bridgeErr) || !bridgeErr.RequestSent {
		t.Fatal("real provider write was not marked terminal")
	}
}

func TestCodexBridgePreservesOtherTrustedMethodsAndDestinations(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			status := bridgeFixture(t)
			req, _ := http.NewRequest(method, "https://provider.invalid/backend-api/models?scope=trusted", nil)
			res, err := bridgeWithHostCheck(t.Context(), status, req, func(_ context.Context, actual *http.Request) (*http.Response, error) {
				if actual.Method != method || actual.URL.String() != req.URL.String() {
					t.Fatal("trusted method or endpoint changed")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok"))}, nil
			}, func(context.Context) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(res.Body)
			_ = res.Body.Close()
		})
	}
}

func TestCodexBridgeOfficialBinaryWire(t *testing.T) {
	path := os.Getenv("SUB2API_CODEX_BRIDGE_BINARY")
	if path == "" {
		t.Skip("dedicated official wire gate supplies independently verified binary")
	}
	// Independent expected executable digests from rust-v0.165.0 release assets.
	// Neither production manifests nor the executable under test produce these.
	expected := map[string]string{"linux/amd64": "c06a67fd8ac76508dc78bd99ce20c9f317b8d568beda59485f3f4134df9962d5", "linux/arm64": "b32ece31af06440da980b733fb8fefdd1f193439595fda84096f1caaceec543f"}[runtime.GOOS+"/"+runtime.GOARCH]
	if expected == "" || !filepath.IsAbs(path) {
		t.Fatal("unsupported official wire environment")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal("official bridge executable missing")
	}
	digest := sha256.New()
	_, err = io.Copy(digest, f)
	_ = f.Close()
	if err != nil || hex.EncodeToString(digest.Sum(nil)) != expected {
		t.Fatal("official bridge executable digest mismatch")
	}
	status := Status{Path: path, Installed: true, Source: "managed", SourceVerified: true, VersionMatch: true, ExecutionAvailable: true, proof: &runtimeProof{path: path, executableSHA256: digest.Sum(nil)}}
	for _, code := range []int{200, 302, 401} {
		body := `{"model":"fixture-model","input":[{"role":"user","content":"retain"}],"tools":[{"type":"function","name":"external","parameters":{}}],"unknown_sibling":{"retain":true}}`
		req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer synthetic.oauth.jwt.with.dots")
		req.Header.Set("User-Agent", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color")
		req.Header.Set("Originator", "codex_cli_rs")
		req.Header.Set("Version", "0.165.0")
		var calls atomic.Int32
		wire := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"unknown\":true}}\n\n"
		res, err := Bridge(t.Context(), status, req, func(ctx context.Context, out *http.Request) (*http.Response, error) {
			calls.Add(1)
			got, _ := io.ReadAll(out.Body)
			if out.URL.String() != req.URL.String() || string(got) != body || out.Header.Get("Authorization") != "Bearer synthetic.oauth.jwt.with.dots" || out.Header.Get("User-Agent") != req.Header.Get("User-Agent") || out.Header.Get("Version") != "0.165.0" || out.Header.Get("Originator") != "codex_cli_rs" {
				t.Error("real CLI relay changed trusted provider request")
			}
			return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"real-cli-fixture"}, "Location": {"http://127.0.0.1:1/must-not-follow"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(res.Body)
		closeErr := res.Body.Close()
		if readErr != nil || closeErr != nil || string(got) != wire || res.StatusCode != code || res.Header.Get("X-Request-Id") != "real-cli-fixture" || calls.Load() != 1 {
			t.Fatal("official CLI transparent bridge wire or no-replay contract failed")
		}
	}
	// API Key accounts retain their own authentication and endpoint, including
	// metadata GETs and image payloads. The official proxy carries only the
	// private relay envelope; it must not reinterpret the trusted provider wire.
	for _, fixture := range []struct {
		name, method, endpoint, contentType, body string
	}{
		{"models", http.MethodGet, "https://api.openai.com/v1/models", "", ""},
		{"compatible-responses", http.MethodPost, "https://compatible.example/v1/responses", "application/json", `{"model":"fixture-model","input":"retain"}`},
		{"image-generation", http.MethodPost, "https://api.openai.com/v1/images/generations", "application/json", `{"model":"gpt-image-1","prompt":"fixture"}`},
		{"image-edit", http.MethodPost, "https://compatible.example/v1/images/edits", "multipart/form-data; boundary=fixture", "--fixture\r\nContent-Disposition: form-data; name=\"image\"; filename=\"fixture.png\"\r\nContent-Type: image/png\r\n\r\n\x89PNG\x00\xff\r\n--fixture--\r\n"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(t.Context(), fixture.method, fixture.endpoint, strings.NewReader(fixture.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer synthetic-api-key")
			request.Header.Set("User-Agent", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color")
			request.Header.Set("Originator", "codex_cli_rs")
			request.Header.Set("Version", "0.165.0")
			if fixture.contentType != "" {
				request.Header.Set("Content-Type", fixture.contentType)
			}
			var calls atomic.Int32
			response, err := Bridge(t.Context(), status, request, func(_ context.Context, out *http.Request) (*http.Response, error) {
				calls.Add(1)
				body, readErr := io.ReadAll(out.Body)
				if readErr != nil || out.Method != fixture.method || out.URL.String() != fixture.endpoint || string(body) != fixture.body ||
					out.Header.Get("Content-Type") != fixture.contentType || out.Header.Get("Authorization") != "Bearer synthetic-api-key" ||
					out.Header.Get("Chatgpt-Account-Id") != "" || out.Header.Get("User-Agent") != "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color" ||
					out.Header.Get("Originator") != "codex_cli_rs" || out.Header.Get("Version") != "0.165.0" {
					t.Error("official CLI changed the prepared API Key request")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"retained":true}`))}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil || string(body) != `{"retained":true}` || response.StatusCode != http.StatusOK || calls.Load() != 1 {
				t.Fatal("official CLI API Key response or no-replay contract failed")
			}
		})
	}
}
