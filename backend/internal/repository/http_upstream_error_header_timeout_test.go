//go:build unit || !integration

package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A rejected request must release the transport and admission slots even when
// an upstream/proxy flushes 429 headers but never sends its compressed body.
// ResponseHeaderTimeout cannot protect this phase, and streaming callers may
// intentionally detach their upstream context from the downstream connection.
func TestHTTPUpstreamThrottledResponseCannotHoldAttempt(t *testing.T) {
	for _, encoding := range []string{"", "gzip", "zstd"} {
		for _, withTLS := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/tls=%v", encoding, withTLS), func(t *testing.T) {
				t.Parallel()
				srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/healthy" {
						_, _ = io.WriteString(w, "ready")
						return
					}
					w.Header().Set("Content-Encoding", encoding)
					w.WriteHeader(http.StatusTooManyRequests)
					_ = http.NewResponseController(w).Flush()
					<-r.Context().Done()
				}))
				if withTLS {
					srv.StartTLS()
				} else {
					srv.Start()
				}
				defer srv.Close()
				defer srv.CloseClientConnections()
				s, do := lifecycleClient(t, srv, withTLS)
				req, err := http.NewRequestWithContext(context.WithoutCancel(t.Context()), http.MethodGet, srv.URL, nil)
				require.NoError(t, err)
				req.Header.Set("Accept-Encoding", encoding)
				type result struct {
					resp *http.Response
					err  error
				}
				done := make(chan result, 1)
				go func() { resp, err := do(req); done <- result{resp, err} }()
				select {
				case got := <-done:
					require.NoError(t, got.err)
					require.Equal(t, http.StatusTooManyRequests, got.resp.StatusCode)
					readDone := make(chan error, 1)
					go func() { _, err := io.ReadAll(got.resp.Body); readDone <- err }()
					select {
					case err := <-readDone:
						require.Error(t, err, "an unfinished error body must be interrupted")
					case <-time.After(7 * time.Second):
						_ = got.resp.Body.Close()
						t.Fatal("rejected response held its transport after the diagnostic budget")
					}
					require.NoError(t, got.resp.Body.Close())
				case <-time.After(7 * time.Second):
					t.Fatal("compressed error headers blocked before the error-body timeout could run")
				}
				require.NoError(t, req.Context().Err(), "one attempt must not cancel future retries")
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				next, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/healthy", nil)
				require.NoError(t, err)
				resp, err := do(next)
				require.NoError(t, err, "the single-connection pool must recover after rejection")
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.Equal(t, "ready", string(body))
				require.NoError(t, resp.Body.Close())
				requireNoUpstreamInFlight(t, s)
			})
		}
	}
}

func TestHTTPUpstreamSuccessfulStreamSurvivesDiagnosticBudget(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: begin\n\n")
		_ = http.NewResponseController(w).Flush()
		select {
		case <-time.After(5500 * time.Millisecond):
			_, _ = io.WriteString(w, "data: usage and completion\n\n")
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	s, do := lifecycleClient(t, srv, false)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "diagnostic timeouts must not truncate accepted streaming work")
	require.Equal(t, "data: begin\n\ndata: usage and completion\n\n", string(body))
	require.NoError(t, resp.Body.Close())
	requireNoUpstreamInFlight(t, s)
}
