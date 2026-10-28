//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A provider may send a complete 429 diagnostic and keep the HTTP body open.
// Byte limits alone cannot let failover and concurrency release make progress.
func TestUpstreamErrorBodyDeadlineClosesNetworkReadAndReleasesConnection(t *testing.T) {
	const prefix = `{"error":{"type":"rate_limit_error"}}`
	heldCanceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/held" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, prefix)
			_ = http.NewResponseController(w).Flush()
			<-r.Context().Done()
			close(heldCanceled)
			return
		}
		_, _ = io.WriteString(w, "next request completed")
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 1
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	callerCtx, cancelCaller := context.WithCancel(t.Context())
	defer cancelCaller()
	req, err := http.NewRequestWithContext(context.WithoutCancel(callerCtx), http.MethodGet, srv.URL+"/held", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// The detached upstream must survive disconnect for accepted usage, but an
	// unsuccessful response must still have an independent, finite read budget.
	cancelCaller()
	readDone := make(chan struct{})
	var body []byte
	var readErr error
	go func() {
		body, readErr = readUpstreamErrorResponseBody(resp, 8192, 30*time.Millisecond)
		close(readDone)
	}()
	select {
	case <-readDone:
	case <-time.After(time.Second):
		_ = resp.Body.Close()
		t.Fatal("a non-EOF 429 retained the upstream read after its budget")
	}
	require.ErrorIs(t, readErr, context.DeadlineExceeded)
	require.Equal(t, prefix, string(body), "partial diagnostics survive the timeout")
	require.NoError(t, req.Context().Err(), "closing this attempt cannot cancel the caller or its retries")
	select {
	case <-heldCanceled:
	case <-time.After(time.Second):
		t.Fatal("the timed-out error body retained its network connection")
	}
	nextCtx, cancelNext := context.WithTimeout(t.Context(), time.Second)
	defer cancelNext()
	next, err := http.NewRequestWithContext(nextCtx, http.MethodGet, srv.URL+"/next", nil)
	require.NoError(t, err)
	nextResp, err := client.Do(next)
	require.NoError(t, err, "the one-connection pool must admit a subsequent request")
	defer nextResp.Body.Close()
	nextBody, err := io.ReadAll(nextResp.Body)
	require.NoError(t, err)
	require.Equal(t, "next request completed", string(nextBody))
}

func TestUpstreamErrorBodyCancellationReturnsBeforeReadBudget(t *testing.T) {
	canceled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
		close(canceled)
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	cancel()
	body, err := readUpstreamErrorResponseBody(resp, 8192, time.Second)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, body)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not release the held response")
	}
}

type observedErrorBody struct {
	io.Reader
	closed atomic.Bool
}

func (body *observedErrorBody) Close() error {
	body.closed.Store(true)
	return nil
}

func TestUpstreamErrorBodyRetainsByteLimitAndNormalCloseOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, body, expected string
		limit                int64
	}{
		{"complete", "limited", "limited", 64},
		{"explicit_limit", "limited: diagnostic details", "limited", 7},
		{"empty", "", "", 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underlying := &observedErrorBody{Reader: strings.NewReader(tc.body)}
			body, err := readUpstreamErrorResponseBody(&http.Response{Body: underlying}, tc.limit, time.Second)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(body))
			require.False(t, underlying.closed.Load(), "normal response ownership stays with the existing caller")
		})
	}
}

func TestGatewayErrorBodyReadersPreserveDiagnosticPrefix(t *testing.T) {
	for _, read := range []struct {
		name string
		body func(*http.Response) []byte
	}{
		{"gateway", func(resp *http.Response) []byte {
			body, _ := (&GatewayService{}).readUpstreamErrorBody(resp)
			return body
		}},
		{"openai", (&OpenAIGatewayService{}).readUpstreamErrorBody},
		{"gemini_messages", (&GeminiMessagesCompatService{}).readUpstreamErrorBody},
		{"antigravity", (&AntigravityGatewayService{}).readUpstreamErrorBody},
	} {
		t.Run(read.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(`{"error":"limited"}`))}
			defer resp.Body.Close()
			require.Equal(t, `{"error":"limited"}`, string(read.body(resp)))
		})
	}
}

func TestGatewayErrorBodyReadersBoundNonEOFResponse(t *testing.T) {
	var ccIncomplete atomic.Bool
	for _, read := range []struct {
		name string
		body func(*http.Response) []byte
	}{
		{"gateway", func(resp *http.Response) []byte {
			body, _ := (&GatewayService{}).readUpstreamErrorBody(resp)
			return body
		}},
		{"openai", (&OpenAIGatewayService{}).readUpstreamErrorBody},
		{"openai_cc", func(resp *http.Response) []byte {
			body, _, complete := (&OpenAIGatewayService{}).readOpenAIUpstreamErrorWithCompleteness(resp)
			ccIncomplete.Store(!complete)
			return body
		}},
		{"gemini_messages", (&GeminiMessagesCompatService{}).readUpstreamErrorBody},
		{"antigravity", (&AntigravityGatewayService{}).readUpstreamErrorBody},
	} {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()
			const prefix = `{"error":"limited"}`
			canceled := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/held" {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, prefix)
					_ = http.NewResponseController(w).Flush()
					<-r.Context().Done()
					close(canceled)
					return
				}
				_, _ = io.WriteString(w, "next request completed")
			}))
			defer srv.Close()
			defer srv.CloseClientConnections()
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.MaxConnsPerHost = 1
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			req, err := http.NewRequestWithContext(context.WithoutCancel(t.Context()), http.MethodGet, srv.URL+"/held", nil)
			require.NoError(t, err)
			resp, err := client.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			done := make(chan []byte, 1)
			go func() { done <- read.body(resp) }()
			select {
			case body := <-done:
				require.Equal(t, prefix, string(body))
				if read.name == "openai_cc" {
					require.True(t, ccIncomplete.Load(), "a timed-out diagnostic must never authorize a complete-body recovery")
					rewound, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Equal(t, prefix, string(rewound))
				}
			case <-time.After(7 * time.Second):
				_ = resp.Body.Close()
				t.Fatal("the production error reader retained a non-EOF 429 beyond its diagnostic budget")
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("the production reader did not terminate its network response")
			}
			nextCtx, cancelNext := context.WithTimeout(t.Context(), time.Second)
			defer cancelNext()
			next, err := http.NewRequestWithContext(nextCtx, http.MethodGet, srv.URL+"/next", nil)
			require.NoError(t, err)
			nextResp, err := client.Do(next)
			require.NoError(t, err, "a timed-out error cannot retain the only connection")
			defer nextResp.Body.Close()
			body, err := io.ReadAll(nextResp.Body)
			require.NoError(t, err)
			require.Equal(t, "next request completed", string(body))
		})
	}
}

func TestOpenAIUpstreamErrorCompletenessPreservesPartialAndRewinds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		fail     bool
		complete bool
	}{
		{"complete", `{"error":"limited"}`, false, true},
		{"interrupted", `{"error":"lim`, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := io.Reader(strings.NewReader(tc.body))
			if tc.fail {
				reader = io.MultiReader(reader, fixedReadError{err: io.ErrUnexpectedEOF})
			}
			resp := &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(reader)}
			defer resp.Body.Close()
			body, _, complete := (&OpenAIGatewayService{}).readOpenAIUpstreamErrorWithCompleteness(resp)
			require.Equal(t, tc.complete, complete)
			require.Equal(t, tc.body, string(body))
			rewound, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, body, rewound)
		})
	}
}

func TestOpenAIUpstreamErrorCompletenessRetainsConfiguredByteCap(t *testing.T) {
	limit := openAIUpstreamErrorBodyReadLimitForConfig(nil)
	resp := &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", int(limit)+1)))}
	defer resp.Body.Close()
	body, _, complete := (&OpenAIGatewayService{}).readOpenAIUpstreamErrorWithCompleteness(resp)
	require.False(t, complete)
	require.Len(t, body, int(limit))
	rewound, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, rewound)
}

type fixedReadError struct{ err error }

func (reader fixedReadError) Read([]byte) (int, error) { return 0, reader.err }
