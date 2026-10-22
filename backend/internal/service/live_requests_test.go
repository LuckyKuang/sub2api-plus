//go:build unit || !integration

package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/liverequestcache"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil/redisclient"
	"github.com/stretchr/testify/require"
)

func liveTestService(t *testing.T) *LiveRequestService {
	t.Helper()
	return NewLiveRequestService(liverequestcache.New(redisclient.New(t)))
}

func liveTestBinding(t *testing.T, s *LiveRequestService, id string) string {
	t.Helper()
	values, err := s.cache.UsageBindings(context.Background(), []string{id})
	require.NoError(t, err)
	require.Len(t, values, 1)
	return values[0]
}

func TestLiveRequestsOwnerIsolationAndMonotonicUpdates(t *testing.T) {
	s := liveTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	r := LiveRequest{ID: "request-a", Version: 2, UserID: 7, AccountID: 99, Status: "finished", StartedAt: now, UpdatedAt: now, FinishedAt: &now}
	require.NoError(t, s.store(ctx, r))
	old := r
	old.Version = 1
	old.Status = "processing"
	old.FinishedAt = nil
	require.NoError(t, s.store(ctx, old))
	other := r
	other.ID = "request-b"
	other.UserID = 8
	require.NoError(t, s.store(ctx, other))
	user, err := s.List(ctx, 7)
	require.NoError(t, err)
	require.Len(t, user.Items, 1)
	require.Equal(t, "finished", user.Items[0].Status)
	require.Zero(t, user.Items[0].AccountID, "user payload must not expose the selected upstream account")
	admin, err := s.List(ctx, 0)
	require.NoError(t, err)
	require.Len(t, admin.Items, 2)
	require.Equal(t, int64(99), admin.Items[0].AccountID)
}

func TestLiveRequestsRetentionBoundsAndStaleState(t *testing.T) {
	s := liveTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 205; i++ {
		r := LiveRequest{ID: strconv.Itoa(i), Version: 1, UserID: 7, Status: "finished", StartedAt: now, UpdatedAt: now}
		require.NoError(t, s.store(ctx, r))
	}
	data, err := s.List(ctx, 7)
	require.NoError(t, err)
	require.Len(t, data.Items, 200)
	require.True(t, data.Truncated)
	stale := LiveRequest{ID: "stale", Version: 1, UserID: 8, Status: "processing", StartedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
	require.NoError(t, s.store(ctx, stale))
	data, err = s.List(ctx, 8)
	require.NoError(t, err)
	require.Equal(t, "unknown", data.Items[0].Status, "loss of observation must not become a failed request")
	stale.ID = "expired"
	stale.UpdatedAt = now.Add(-6 * time.Minute)
	require.NoError(t, s.store(ctx, stale))
	data, err = s.List(ctx, 8)
	require.NoError(t, err)
	require.Len(t, data.Items, 1)
}

func TestLiveRequestsUnavailableDoesNotInventEmptySuccess(t *testing.T) {
	var s *LiveRequestService
	_, err := s.List(context.Background(), 7)
	require.Error(t, err)
	s.Publish(LiveRequest{}) // Observer absence cannot block a gateway request.
	s = NewLiveRequestService(liverequestcache.New(nil))
	_, err = s.List(context.Background(), 7)
	require.Error(t, err, "an unconfigured cache must remain unavailable")
}
