//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	pkghttputil "github.com/LuckyKuang/sub2api-plus/internal/pkg/httputil"
	"github.com/stretchr/testify/require"
)

// EDGE_SECURITY.md: a client disconnect starts a finite per-attempt drain;
// healthy streams have no whole-response deadline. This exercises the actual
// Plus context composition, not only the upstream utility in isolation.
func TestPlusDetachedAttemptsKeepFiniteClientDrain(t *testing.T) {
	for _, path := range []string{"stream", "nonstream", "explicit"} {
		t.Run(path, func(t *testing.T) {
			client, disconnect := context.WithCancel(context.Background())
			defer disconnect()
			var detached context.Context
			var release context.CancelFunc
			if path == "explicit" {
				detached, release = detachUpstreamContext(client, 20*time.Millisecond)
			} else {
				detached, release = detachStreamUpstreamContext(client, path == "stream")
			}
			defer release()
			attempt, closeAttempt := pkghttputil.NewUpstreamAttemptContext(detached, 20*time.Millisecond)
			defer closeAttempt()
			require.NoError(t, attempt.Err())
			if path == "stream" {
				_, hasDeadline := attempt.Deadline()
				require.False(t, hasDeadline, "healthy streams cannot get a total lifetime deadline")
			}
			disconnect()
			select {
			case <-attempt.Done():
				require.ErrorIs(t, attempt.Err(), context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("Plus context composition lost the original client drain signal")
			}
		})
	}
}
