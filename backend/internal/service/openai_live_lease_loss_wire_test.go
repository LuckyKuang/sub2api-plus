//go:build unit || !integration

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

type liveLeaseLossWireCache struct {
	*liveTestConcurrencyCache
	refreshErr error
	refreshes  atomic.Int32
}

func (c *liveLeaseLossWireCache) RefreshLiveLease(context.Context, int64, int64, int64, string) (bool, error) {
	c.refreshes.Add(1)
	return false, c.refreshErr
}

func TestLiveConcurrencyLeaseLossStopsActualProviderSessionAndSocket(t *testing.T) {
	// The connected Live session has two minutes left. Both controllers must
	// stop provider execution after a missing owner or Redis renewal failure.
	// Natural expiry and a test-side socket close cannot satisfy these assertions.
	for _, controller := range []string{"client", "observer"} {
		for _, failure := range []string{"missing-owner", "redis-error"} {
			t.Run(controller+"/"+failure, func(t *testing.T) {
				t.Parallel()
				providerFrames := make(chan []byte, 2)
				providerStopped := make(chan error, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						providerStopped <- err
						return
					}
					defer func() { _ = conn.CloseNow() }()
					for {
						kind, payload, readErr := conn.Read(r.Context())
						if readErr != nil {
							providerStopped <- readErr
							return
						}
						if kind == coderws.MessageText {
							providerFrames <- payload
						}
					}
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
				require.NoError(t, err)
				defer func() { _ = conn.CloseNow() }()
				upstream := &coderOpenAIWSClientConn{conn: conn}
				cache := &liveLeaseLossWireCache{liveTestConcurrencyCache: &liveTestConcurrencyCache{}}
				if failure == "redis-error" {
					cache.refreshErr = errors.New("isolated Redis renewal unavailable")
				}
				record := &LiveCallRecord{CallID: "call_lease_loss", CallHash: hashLiveCallID("call_lease_loss"), AccountID: 11, UserID: 33, APIKeyID: 22,
					LeaseID: "lost-live-owner", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(2 * time.Minute), Controller: LiveControllerObserver}
				store := &liveTestStore{}
				require.NoError(t, store.SaveLiveCall(ctx, record, time.Hour))
				svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(cache)}
				result := make(chan error, 1)
				go func() {
					if controller == "client" {
						result <- svc.runLiveController(ctx, record, upstream, make(chan error))
						return
					}
					result <- svc.runLiveObserverConnection(record, upstream)
				}()
				select {
				case payload := <-providerFrames:
					require.JSONEq(t, `{"type":"session.close"}`, string(payload), "capacity loss must stop the remote media session")
				case <-ctx.Done():
					t.Fatal("lost Live lease did not send session.close to the actual provider socket")
				}
				select {
				case stopErr := <-providerStopped:
					require.Error(t, stopErr)
				case <-time.After(time.Second):
					t.Fatal("provider control socket survived the lost Live execution lease")
				}
				select {
				case runErr := <-result:
					require.ErrorIs(t, runErr, ErrLiveUnavailable)
				case <-time.After(time.Second):
					t.Fatal("Live controller survived the lost execution lease")
				}
				require.EqualValues(t, 1, cache.refreshes.Load(), "lost capacity must not reconnect or renew again")
				require.True(t, time.Now().Before(record.ExpiresAt), "natural Live expiry cannot prove lease-loss cancellation")
			})
		}
	}
}
