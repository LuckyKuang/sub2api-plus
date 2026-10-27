//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type inflightPoolIdentityCache struct {
	SchedulerCache
	reads atomic.Int64
}

func (c *inflightPoolIdentityCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return []*Account{{ID: c.reads.Add(1)}}, true, nil
}

func TestInflightSnapshotLookupSeparatesPoolsAndRequests(t *testing.T) {
	cache := &inflightPoolIdentityCache{}
	snapshot := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	ctx, cleanup := withInflightSnapshotLookup(context.Background())
	defer cleanup()
	groupA, groupB, zeroGroup := int64(11), int64(22), int64(0)
	for _, tc := range []struct {
		group    *int64
		platform string
		forced   bool
		owner    int64
	}{
		{&groupA, PlatformOpenAI, false, 1},
		{&groupA, PlatformGrok, false, 2},
		{&groupB, PlatformOpenAI, false, 3},
		{&groupA, PlatformOpenAI, true, 4},
		{nil, PlatformOpenAI, false, 5},
		{&zeroGroup, PlatformOpenAI, false, 6},
	} {
		for repeat := 0; repeat < 2; repeat++ {
			accounts, err := listInflightSnapshotAccounts(ctx, snapshot, tc.group, tc.platform, tc.forced)
			require.NoError(t, err)
			require.Len(t, accounts, 1)
			require.Equal(t, tc.owner, accounts[0].ID, "only the identical authorization pool may reuse an account snapshot")
		}
	}
	require.EqualValues(t, 6, cache.reads.Load())
	next, nextCleanup := withInflightSnapshotLookup(context.Background())
	defer nextCleanup()
	accounts, err := listInflightSnapshotAccounts(next, snapshot, &groupA, PlatformOpenAI, false)
	require.NoError(t, err)
	require.EqualValues(t, 7, accounts[0].ID, "the next request must observe fresh scheduler evidence")
	require.EqualValues(t, 7, cache.reads.Load())
}

type inflightLookupBudgetCache struct {
	SchedulerCache
	contexts []context.Context
}

func (c *inflightLookupBudgetCache) GetSnapshot(ctx context.Context, _ SchedulerBucket) ([]*Account, bool, error) {
	c.contexts = append(c.contexts, ctx)
	return []*Account{{ID: int64(len(c.contexts))}}, true, nil
}

func TestInflightSnapshotLookupSharesDeadlineAcrossPoolsWithoutCancellingParent(t *testing.T) {
	parent := context.Background()
	cache := &inflightLookupBudgetCache{}
	snapshot := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	ctx, cleanup := withInflightSnapshotLookup(parent)
	defer cleanup()
	groupA, groupB := int64(11), int64(22)
	for _, pool := range []struct {
		group    *int64
		platform string
	}{
		{&groupA, PlatformOpenAI},
		{&groupA, PlatformGrok},
		{&groupB, PlatformOpenAI},
	} {
		accounts, err := listInflightSnapshotAccounts(ctx, snapshot, pool.group, pool.platform, false)
		require.NoError(t, err)
		require.Len(t, accounts, 1)
	}
	require.Len(t, cache.contexts, 3, "distinct pools must each retain their own account evidence")
	deadline, ok := cache.contexts[0].Deadline()
	require.True(t, ok, "metadata cannot wait without a finite budget")
	require.Positive(t, time.Until(deadline))
	require.LessOrEqual(t, time.Until(deadline), 2*time.Second)
	for _, lookupCtx := range cache.contexts[1:] {
		nextDeadline, hasDeadline := lookupCtx.Deadline()
		require.True(t, hasDeadline)
		require.Equal(t, deadline, nextDeadline, "a different pool must not restart the same admission's two-second budget")
	}
	callbackDeadline, ok := inflightSnapshotLookupContext(ctx).Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, callbackDeadline, "other estimate metadata callbacks share the same remaining budget")
	cleanup()
	for _, lookupCtx := range cache.contexts {
		require.ErrorIs(t, lookupCtx.Err(), context.Canceled, "completed admission must release every dependency context")
	}
	require.NoError(t, parent.Err(), "releasing metadata resources must not cancel the parent request")

	next, nextCleanup := withInflightSnapshotLookup(parent)
	defer nextCleanup()
	_, err := listInflightSnapshotAccounts(next, snapshot, &groupA, PlatformOpenAI, false)
	require.NoError(t, err)
	require.Len(t, cache.contexts, 4)
	fresh := cache.contexts[3]
	freshDeadline, ok := fresh.Deadline()
	require.True(t, ok)
	require.True(t, freshDeadline.After(deadline), "an independent admission gets its own budget")
	require.NoError(t, fresh.Err(), "the previous admission's cleanup cannot poison a later request")
}

func TestInflightSnapshotLookupRetainsShorterParentDeadline(t *testing.T) {
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	cache := &inflightLookupBudgetCache{}
	snapshot := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	ctx, cleanup := withInflightSnapshotLookup(parent)
	defer cleanup()
	_, err := listInflightSnapshotAccounts(ctx, snapshot, nil, PlatformOpenAI, false)
	require.NoError(t, err)
	require.Len(t, cache.contexts, 1)
	deadline, ok := cache.contexts[0].Deadline()
	require.True(t, ok)
	require.Equal(t, parentDeadline, deadline, "the metadata budget cannot extend a shorter request deadline")
	cancel()
	require.ErrorIs(t, cache.contexts[0].Err(), context.Canceled)
}
