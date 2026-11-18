//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// runCompatStreamToEnd drives the compat stream handler with an upstream that
// sends nothing and closes after closeAfter.
func runCompatStreamToEnd(t *testing.T, keepaliveInterval, maxWait, closeAfter time.Duration) error {
	t.Helper()
	svc := newAntigravityCompatService(config.GatewayConfig{
		MaxLineSize: defaultMaxLineSize, StreamDataIntervalTimeout: 30, StreamKeepaliveInterval: 0,
	}, nil)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1/responses", nil)
	reader, pipeWriter := io.Pipe()
	defer func() { _ = reader.Close() }()
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader}
	done := make(chan error, 1)
	go func() {
		_, err := svc.handleAntigravityCompatStreamWithKeepaliveInterval(
			c, resp, time.Now(), "gemini-4-hydrogen-high",
			newAntigravityResponsesStreamAdapter("gemini-4-hydrogen-high"), "test", keepaliveInterval, maxWait,
		)
		done <- err
	}()
	time.Sleep(closeAfter)
	_ = pipeWriter.Close()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("compat stream handler did not return")
		return nil
	}
}

func TestAntigravityCompatEmptyStreamAfterKeepaliveIsCommittedNoContent(t *testing.T) {
	err := runCompatStreamToEnd(t, 10*time.Millisecond, time.Second, 50*time.Millisecond)
	require.Error(t, err)
	require.True(t, IsAntigravityCompatCommittedNoContent(err), "got %v", err)
}

func TestAntigravityCompatPreContentTimeoutAfterKeepaliveIsCommittedNoContent(t *testing.T) {
	err := runCompatStreamToEnd(t, 10*time.Millisecond, 40*time.Millisecond, 200*time.Millisecond)
	require.Error(t, err)
	require.ErrorContains(t, err, "pre-content stream timeout")
	require.True(t, IsAntigravityCompatCommittedNoContent(err), "got %v", err)
}

func TestAntigravityCompatEmptyStreamBeforeKeepaliveStaysFailover(t *testing.T) {
	err := runCompatStreamToEnd(t, time.Second, 2*time.Second, 10*time.Millisecond)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "got %v", err)
	require.False(t, IsAntigravityCompatCommittedNoContent(err))
}

// Model failover replacing the sticky owner at the deletion boundary. The old
// read-then-delete path receives the stale owner after Get has moved the binding;
// an atomic compare/delete instead observes and preserves the replacement.
type committedNoContentReboundCache struct {
	stubGatewayCache
}

func (c *committedNoContentReboundCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	previous, err := c.stubGatewayCache.GetSessionAccountID(ctx, groupID, sessionHash)
	c.sessionBindings[sessionHash] = 6
	return previous, err
}

func (c *committedNoContentReboundCache) CompareAndDeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64) (bool, error) {
	c.sessionBindings[sessionHash] = 6
	return c.stubGatewayCache.CompareAndDeleteSessionAccountID(ctx, groupID, sessionHash, accountID)
}

type committedNoContentUnavailableCache struct {
	stubGatewayCache
}

func (c *committedNoContentUnavailableCache) CompareAndDeleteSessionAccountID(context.Context, int64, string, int64) (bool, error) {
	return false, errors.New("cache unavailable")
}

func TestReleaseStickySessionAfterCommittedNoContent(t *testing.T) {
	// docs/providers/ANTIGRAVITY.md requires a late failed stream to release
	// only its own sticky binding, preserving a concurrent successful failover.
	ctx := context.Background()
	groupID := int64(10)
	committed := &antigravityCompatCommittedNoContentError{msg: "empty Antigravity compatibility stream after keepalive"}

	t.Run("unbinds the session from the account that failed", func(t *testing.T) {
		cache := &stubGatewayCache{sessionBindings: map[string]int64{"sess": 5}}
		svc := &GatewayService{cache: cache}
		require.True(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "sess", 5, committed))
		_, bound := cache.sessionBindings["sess"]
		require.False(t, bound)
	})

	t.Run("keeps a binding that already moved to another account", func(t *testing.T) {
		cache := &stubGatewayCache{sessionBindings: map[string]int64{"sess": 6}}
		svc := &GatewayService{cache: cache}
		require.False(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "sess", 5, committed))
		require.Equal(t, int64(6), cache.sessionBindings["sess"])
	})

	t.Run("preserves failover between a stale observation and deletion", func(t *testing.T) {
		cache := &committedNoContentReboundCache{stubGatewayCache{sessionBindings: map[string]int64{"sess": 5}}}
		svc := &GatewayService{cache: cache}
		require.False(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "sess", 5, committed))
		require.Equal(t, int64(6), cache.sessionBindings["sess"])
		require.Empty(t, cache.deletedSessions, "the replacement binding must not be deleted")
	})

	t.Run("cache failure preserves the binding without an unconditional fallback", func(t *testing.T) {
		cache := &committedNoContentUnavailableCache{stubGatewayCache{sessionBindings: map[string]int64{"sess": 5}}}
		svc := &GatewayService{cache: cache}
		require.False(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "sess", 5, committed))
		require.Equal(t, int64(5), cache.sessionBindings["sess"])
		require.Empty(t, cache.deletedSessions)
	})

	t.Run("missing binding is a no-op", func(t *testing.T) {
		cache := &stubGatewayCache{}
		svc := &GatewayService{cache: cache}
		require.False(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "sess", 5, committed))
		require.Empty(t, cache.deletedSessions)
	})

	t.Run("ignores other errors", func(t *testing.T) {
		cache := &stubGatewayCache{sessionBindings: map[string]int64{"sess": 5}}
		svc := &GatewayService{cache: cache}
		require.False(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "sess", 5, errors.New("stream data interval timeout")))
		require.Equal(t, int64(5), cache.sessionBindings["sess"])
	})

	t.Run("no session hash is a no-op", func(t *testing.T) {
		cache := &stubGatewayCache{sessionBindings: map[string]int64{"sess": 5}}
		svc := &GatewayService{cache: cache}
		require.False(t, svc.ReleaseStickySessionAfterCommittedNoContent(ctx, &groupID, "", 5, committed))
		require.Equal(t, int64(5), cache.sessionBindings["sess"])
	})
}
