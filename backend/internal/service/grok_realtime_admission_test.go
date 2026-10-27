//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type grokAdmissionRelayResult struct {
	audio bool
	err   error
}

// Both ingress and upstream use real WebSocket framing. The upstream counter
// observes actual wire messages rather than calls to a forwarding stub.
func grokAdmissionRelay(t *testing.T, ctx context.Context, observer *GrokRealtimeTurnObserver) (*coderws.Conn, <-chan grokAdmissionRelayResult, *atomic.Int64) {
	t.Helper()
	var writes atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		for {
			kind, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			writes.Add(1)
			if conn.Write(r.Context(), kind, body) != nil {
				return
			}
		}
	}))
	t.Cleanup(origin.Close)
	done := make(chan grokAdmissionRelayResult, 1)
	ingress := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(origin.URL, "http"), nil)
		if err != nil {
			done <- grokAdmissionRelayResult{err: err}
			return
		}
		client, err := coderws.Accept(w, r, nil)
		if err != nil {
			_ = remote.CloseNow()
			done <- grokAdmissionRelayResult{err: err}
			return
		}
		audio, err := (&OpenAIGatewayService{}).ProxyGrokRealtimeConnWithObserver(ctx, nil, client, &GrokRealtimeUpstream{conn: &coderOpenAIWSClientConn{conn: remote}}, observer)
		_ = remote.CloseNow()
		_ = client.CloseNow()
		done <- grokAdmissionRelayResult{audio: audio, err: err}
	}))
	t.Cleanup(ingress.Close)
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(ingress.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })
	return client, done, &writes
}

func TestGrokRealtimeAdmissionBlocksBeforeWireWriteAndAudioAccounting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	denied := errors.New("audit denied")
	var calls atomic.Int64
	client, done, writes := grokAdmissionRelay(t, ctx, &GrokRealtimeTurnObserver{BeforeClient: func(context.Context, []byte) error {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return denied
		case <-ctx.Done():
			return ctx.Err()
		}
	}})
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"input_audio_buffer.append","audio":"YXVkaW8="}`)))
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("admission hook did not run")
	}
	require.Zero(t, writes.Load(), "no upstream frame may be sent while audit is pending")
	close(release)
	select {
	case result := <-done:
		var admission *GrokRealtimeAdmissionError
		require.ErrorAs(t, result.err, &admission)
		require.ErrorIs(t, result.err, denied)
		require.False(t, result.audio, "a rejected audio frame must not create usage")
	case <-ctx.Done():
		t.Fatal("blocked relay did not stop")
	}
	require.Equal(t, int64(1), calls.Load())
	require.Zero(t, writes.Load())
}

func TestGrokRealtimeAdmissionAuditsEachValidFrameAndPreservesAcceptedAudio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	denied := errors.New("second turn denied")
	var calls atomic.Int64
	client, done, writes := grokAdmissionRelay(t, ctx, &GrokRealtimeTurnObserver{BeforeClient: func(context.Context, []byte) error {
		if calls.Add(1) == 3 {
			return denied
		}
		return nil
	}})
	for _, frame := range []string{`{"type":"future.client.event","unknown":{"text":"forward compatible"}}`, `{"type":"input_audio_buffer.append","audio":"YXVkaW8="}`} {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(frame)))
		_, echo, err := client.Read(ctx)
		require.NoError(t, err)
		require.JSONEq(t, frame, string(echo))
	}
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"input_audio_buffer.append","audio":"YmxvY2tlZA=="}`)))
	select {
	case result := <-done:
		require.ErrorIs(t, result.err, denied)
		require.True(t, result.audio, "earlier accepted audio must keep its already incurred usage")
	case <-ctx.Done():
		t.Fatal("rejected subsequent turn did not stop")
	}
	require.Equal(t, int64(3), calls.Load())
	require.Equal(t, int64(2), writes.Load(), "only the first two admitted frames may reach upstream")
}

func TestGrokRealtimeInvalidSyntaxDoesNotEnterAdmissionOrUpstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int64
	client, done, writes := grokAdmissionRelay(t, ctx, &GrokRealtimeTurnObserver{BeforeClient: func(context.Context, []byte) error { calls.Add(1); return nil }})
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`invalid JSON`)))
	select {
	case result := <-done:
		require.ErrorContains(t, result.err, "invalid realtime event")
	case <-ctx.Done():
		t.Fatal("invalid frame did not close relay")
	}
	require.Zero(t, calls.Load())
	require.Zero(t, writes.Load())
}
