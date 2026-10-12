//go:build unit || !integration

package firstbyte

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type steps struct {
	reads  int
	closed bool
}

func (s *steps) Read(p []byte) (int, error) {
	s.reads++
	if s.reads == 1 {
		return 0, nil
	}
	p[0] = ':'
	return 1, io.EOF
}
func (s *steps) Close() error { s.closed = true; return nil }

// docs/USAGE_TIMING.md: headers and zero-byte reads are not observations;
// positive data with EOF is an observation, and discarded attempts never leak.
func TestResponseBodyBoundaryAndAttemptIsolation(t *testing.T) {
	ctx, timing := WithStart(context.Background(), time.Now().Add(-time.Second))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	reader := &steps{}
	resp := &http.Response{Body: reader}
	Track(req, resp)
	require.Nil(t, timing.Milliseconds())
	data := make([]byte, 1)
	n, err := resp.Body.Read(data)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Nil(t, timing.Milliseconds())
	n, err = resp.Body.Read(data)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 1, n)
	require.GreaterOrEqual(t, *timing.Milliseconds(), 1000)
	require.NoError(t, resp.Body.Close())
	require.True(t, reader.closed)
	Track(req, &http.Response{Body: io.NopCloser(bytes.NewReader(nil))})
	require.Nil(t, timing.Milliseconds(), "a new empty attempt cannot borrow the old data")
	_, other := WithStart(ctx, time.Now())
	require.Nil(t, other.Milliseconds(), "a new account/forwarding call owns independent timing")
}

func TestZeroMillisecondsIsObserved(t *testing.T) {
	at := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	timing := &Timing{start: at, current: &observation{at: at}}
	require.NotNil(t, timing.Milliseconds())
	require.Zero(t, *timing.Milliseconds())
}

// A compressed header precedes application data. The consumer is still blocked
// decoding when the raw observer must already have recorded the first read.
func TestCompressedBodyObservedBeforeDecodedOutput(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte("data: token\n\n"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	data := compressed.Bytes()
	firstSent, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseServer := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Error("automatic encoding negotiation changed")
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(data[:10])
		require.NoError(t, http.NewResponseController(w).Flush())
		close(firstSent)
		<-release
		_, _ = w.Write(data[10:])
	}))
	t.Cleanup(server.Close)
	t.Cleanup(releaseServer)
	ctx, timing := WithStart(context.Background(), time.Now())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	rt := WrapTransport(&http.Transport{})
	client := &http.Client{Transport: rt}
	t.Cleanup(client.CloseIdleConnections)
	resp, err := client.Do(req)
	require.NoError(t, err)
	<-firstSent
	require.Nil(t, timing.Milliseconds(), "response headers cannot start the body clock")
	readDone := make(chan []byte, 1)
	go func() { result, _ := io.ReadAll(resp.Body); readDone <- result }()
	deadline := time.After(time.Second)
	for timing.Milliseconds() == nil {
		select {
		case <-deadline:
			t.Fatal("compressed header was not observed")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	select {
	case <-readDone:
		t.Fatal("decoder completed before application data")
	default:
	}
	releaseServer()
	require.Equal(t, "data: token\n\n", string(<-readDone))
	require.True(t, resp.Uncompressed)
	require.Empty(t, resp.Header.Get("Content-Encoding"))
	require.NoError(t, resp.Body.Close())
}

// Preserve net/http's encoding negotiation and signed request declarations.
func TestTransportPreservesExplicitEncodingRangeHeadAndIdentity(t *testing.T) {
	var zipped bytes.Buffer
	writer := gzip.NewWriter(&zipped)
	_, err := writer.Write([]byte("payload"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	for _, tc := range []struct {
		name, method, encoding, rangeHeader, wantEncoding string
		disable, decoded                                  bool
	}{
		{"automatic gzip", "GET", "", "", "gzip", false, true},
		{"explicit gzip", "GET", "gzip", "", "gzip", false, false},
		{"range", "GET", "", "bytes=0-2", "", false, false},
		{"head", "HEAD", "", "", "", false, false},
		{"compression disabled", "GET", "", "", "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tc.wantEncoding, r.Header.Get("Accept-Encoding"))
				require.Equal(t, tc.rangeHeader, r.Header.Get("Range"))
				require.Equal(t, "trusted-client/1", r.Header.Get("User-Agent"))
				require.Equal(t, "signed-declaration", r.Header.Get("Authorization"))
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(zipped.Bytes())
			}))
			defer server.Close()
			ctx, timing := WithStart(context.Background(), time.Now())
			req, err := http.NewRequestWithContext(ctx, tc.method, server.URL, nil)
			require.NoError(t, err)
			req.Header.Set("User-Agent", "trusted-client/1")
			req.Header.Set("Authorization", "signed-declaration")
			if tc.encoding != "" {
				req.Header.Set("Accept-Encoding", tc.encoding)
			}
			if tc.rangeHeader != "" {
				req.Header.Set("Range", tc.rangeHeader)
			}
			rt := WrapTransport(&http.Transport{DisableCompression: tc.disable})
			client := &http.Client{Transport: rt}
			defer client.CloseIdleConnections()
			resp, err := client.Do(req)
			require.NoError(t, err)
			data, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, tc.decoded, resp.Uncompressed)
			require.Equal(t, tc.encoding, req.Header.Get("Accept-Encoding"), "request must not be mutated")
			if tc.method == "HEAD" {
				require.Empty(t, data)
				require.Nil(t, timing.Milliseconds())
			} else if tc.decoded {
				require.Equal(t, "payload", string(data))
			} else {
				require.Equal(t, zipped.Bytes(), data)
			}
			bad := req.Clone(ctx)
			bad.URL.Scheme = "invalid"
			_, err = client.Do(bad)
			require.Error(t, err)
			require.Nil(t, timing.Milliseconds(), "a failed next attempt must not borrow the prior observation")
		})
	}
}
