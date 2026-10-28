//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This fake models a lost Redis response after the owner was committed, rather
// than reporting an error before writing. Foreign owners must remain executing.
type ownerRecoveryCache struct {
	stubConcurrencyCacheForTest
	mu                 sync.Mutex
	owners             map[string]map[string]bool
	uncertain          bool
	releaseFailures    int
	releaseUnavailable bool
	releases           map[string]int
	acquisitions       int
}

func newOwnerRecoveryCache() *ownerRecoveryCache {
	return &ownerRecoveryCache{owners: make(map[string]map[string]bool), releases: make(map[string]int)}
}

func recoveryScopeID(scope string, id int64) string { return fmt.Sprintf("%s:%d", scope, id) }

func (c *ownerRecoveryCache) seed(scope string, id int64, owner string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := recoveryScopeID(scope, id)
	if c.owners[key] == nil {
		c.owners[key] = make(map[string]bool)
	}
	c.owners[key][owner] = true
}

func (c *ownerRecoveryCache) acquire(scope string, id int64, limit int, owner string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acquisitions++
	key := recoveryScopeID(scope, id)
	if c.owners[key] == nil {
		c.owners[key] = make(map[string]bool)
	}
	if limit > 0 && len(c.owners[key]) >= limit {
		return false, nil
	}
	c.owners[key][owner] = true
	if c.uncertain {
		c.uncertain = false
		return false, errors.New("reply lost after owner committed")
	}
	return true, nil
}

func (c *ownerRecoveryCache) AcquireUserSlot(_ context.Context, id int64, limit int, owner string) (bool, error) {
	return c.acquire("user", id, limit, owner)
}
func (c *ownerRecoveryCache) AcquireAccountSlot(_ context.Context, id int64, limit int, owner string) (bool, error) {
	return c.acquire("account", id, limit, owner)
}
func (c *ownerRecoveryCache) release(scope string, id int64, owner string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releases[owner]++
	if c.releaseUnavailable {
		return errors.New("redis unavailable")
	}
	if c.releaseFailures > 0 {
		c.releaseFailures--
		return errors.New("redis release reply lost")
	}
	delete(c.owners[recoveryScopeID(scope, id)], owner)
	return nil
}
func (c *ownerRecoveryCache) ReleaseUserSlot(_ context.Context, id int64, owner string) error {
	return c.release("user", id, owner)
}
func (c *ownerRecoveryCache) ReleaseAccountSlot(_ context.Context, id int64, owner string) error {
	return c.release("account", id, owner)
}
func (c *ownerRecoveryCache) snapshot(scope string, id int64) map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]bool)
	for owner := range c.owners[recoveryScopeID(scope, id)] {
		out[owner] = true
	}
	return out
}

func acquireRecoveryScope(s *ConcurrencyService, scope string, id int64, limit int) (*AcquireResult, error) {
	if scope == "account" {
		return s.AcquireAccountSlot(context.Background(), id, limit)
	}
	return s.AcquireUserSlot(context.Background(), id, limit)
}

func TestUncertainConcurrencyAdmissionReclaimsOnlyItsOwner(t *testing.T) {
	for _, scope := range []string{"user", "account"} {
		t.Run(scope, func(t *testing.T) {
			cache := newOwnerRecoveryCache()
			cache.seed(scope, 42, "another-instance-still-executing")
			cache.uncertain = true
			svc := NewConcurrencyService(cache)
			result, err := acquireRecoveryScope(svc, scope, 42, 2)
			require.ErrorContains(t, err, "reply lost")
			require.Nil(t, result)
			require.Equal(t, map[string]bool{"another-instance-still-executing": true}, cache.snapshot(scope, 42), "failed admission cannot leave its committed owner consuming capacity or remove a foreign owner")
			next, err := acquireRecoveryScope(svc, scope, 42, 2)
			require.NoError(t, err)
			require.True(t, next.Acquired, "one free execution slot must be usable without waiting for the old 30-minute TTL")
			next.ReleaseFunc()
		})
	}
}

func TestConcurrencyReleaseRecoveryReadmitsWithoutDeletingLiveOwners(t *testing.T) {
	for _, scope := range []string{"user", "account"} {
		t.Run(scope, func(t *testing.T) {
			cache := newOwnerRecoveryCache()
			cache.seed(scope, 42, "another-instance-still-executing")
			svc := NewConcurrencyService(cache)
			first, err := acquireRecoveryScope(svc, scope, 42, 2)
			require.NoError(t, err)
			require.True(t, first.Acquired)
			cache.releaseFailures = 1
			first.ReleaseFunc()
			first.ReleaseFunc()
			next, err := acquireRecoveryScope(svc, scope, 42, 2)
			require.NoError(t, err)
			require.True(t, next.Acquired, "a recovered store must reclaim the completed owner before deciding that the user/account is still full")
			require.True(t, cache.snapshot(scope, 42)["another-instance-still-executing"])
			next.ReleaseFunc()
			require.Equal(t, map[string]bool{"another-instance-still-executing": true}, cache.snapshot(scope, 42))
		})
	}
}

func TestConcurrencyOwnerRecoveryRemainsBoundedDuringPersistentFailure(t *testing.T) {
	cache := newOwnerRecoveryCache()
	svc := NewConcurrencyService(cache)
	first, err := svc.AcquireUserSlot(context.Background(), 42, 1)
	require.NoError(t, err)
	cache.releaseUnavailable = true
	first.ReleaseFunc()
	for i := 0; i < 100; i++ {
		result, nextErr := svc.AcquireUserSlot(context.Background(), 42, 1)
		require.Error(t, nextErr, "unconfirmed owner recovery must fail closed instead of treating the retained owner as active user load")
		require.Nil(t, result)
	}
	require.Len(t, cache.snapshot("user", 42), 1, "repeated retries cannot write more uncertain owners")
	cache.releaseUnavailable = false
	next, err := svc.AcquireUserSlot(context.Background(), 42, 1)
	require.NoError(t, err)
	require.True(t, next.Acquired)
	next.ReleaseFunc()
	require.Empty(t, cache.snapshot("user", 42))
}

func TestConcurrencyOwnerRecoveryCapacityReservedBeforeEveryAdmission(t *testing.T) {
	cache := newOwnerRecoveryCache()
	svc := NewConcurrencyService(cache)
	svc.ownerRecovery.capacity = 2
	user, err := svc.AcquireUserSlot(context.Background(), 42, 3)
	require.NoError(t, err)
	account, err := svc.AcquireAccountSlot(context.Background(), 19, 3)
	require.NoError(t, err)
	defer account.ReleaseFunc()

	for _, scope := range []string{"user", "account"} {
		blocked, admissionErr := acquireRecoveryScope(svc, scope, 99, 3)
		require.ErrorIs(t, admissionErr, errConcurrencyOwnerRecoveryUnavailable)
		require.Nil(t, blocked)
		require.Empty(t, cache.snapshot(scope, 99))
	}
	require.Equal(t, 2, cache.acquisitions, "a full recovery registry must reject before issuing any new Redis write")
	require.Len(t, svc.ownerRecovery.owners, 2)

	user.ReleaseFunc()
	user.ReleaseFunc()
	require.Len(t, svc.ownerRecovery.owners, 1, "a confirmed, idempotent release returns its recovery reservation")
	next, err := svc.AcquireUserSlot(context.Background(), 99, 3)
	require.NoError(t, err)
	require.True(t, next.Acquired)
	next.ReleaseFunc()
	require.Len(t, svc.ownerRecovery.owners, 1)
}

func TestConcurrencyOwnerRecoveryReclaimsExpiredForeignRecordsWithoutCleanupWorker(t *testing.T) {
	cache := newOwnerRecoveryCache()
	svc := NewConcurrencyService(cache)
	svc.ownerRecovery.capacity = 4
	active, err := svc.AcquireUserSlot(context.Background(), 42, 1)
	require.NoError(t, err)
	defer active.ReleaseFunc()
	completed, err := svc.AcquireUserSlot(context.Background(), 43, 1)
	require.NoError(t, err)
	cache.releaseFailures = 1
	completed.ReleaseFunc()
	var activeOwner, completedOwner concurrencyOwner
	for owner := range svc.ownerRecovery.owners {
		if owner.id == 42 {
			activeOwner = owner
		}
		if owner.id == 43 {
			completedOwner = owner
		}
	}
	busy := concurrencyOwner{scope: "user", id: 44, requestID: "busy-recovery"}
	future := concurrencyOwner{scope: "user", id: 45, requestID: "pending-unexpired"}
	for _, owner := range []concurrencyOwner{busy, future} {
		require.NoError(t, svc.reserveConcurrencyOwner(owner))
		svc.markConcurrencyOwnerPending(owner)
	}
	old := time.Now().Add(-time.Hour)
	svc.ownerRecovery.owners[activeOwner].expiresAt = old
	svc.ownerRecovery.owners[busy].expiresAt = old
	svc.ownerRecovery.owners[busy].busy = true
	blocked, err := svc.AcquireUserSlot(context.Background(), 99, 1)
	require.ErrorIs(t, err, errConcurrencyOwnerRecoveryUnavailable)
	require.Nil(t, blocked)
	require.Equal(t, 2, cache.acquisitions, "active, busy or unexpired records must not be evicted to admit new work")

	// No cleanup worker runs, and user 43 never sends another request. Its
	// completed owner's conservative TTL has now elapsed; a different user's
	// admission must recover the local reservation without a Redis delete.
	svc.ownerRecovery.owners[completedOwner].expiresAt = old
	next, err := svc.AcquireUserSlot(context.Background(), 99, 1)
	require.NoError(t, err)
	require.True(t, next.Acquired)
	defer next.ReleaseFunc()
	require.NotContains(t, svc.ownerRecovery.owners, completedOwner)
	for _, owner := range []concurrencyOwner{activeOwner, busy, future} {
		require.Contains(t, svc.ownerRecovery.owners, owner)
	}
	require.Len(t, svc.ownerRecovery.owners, 4)
	require.Equal(t, 1, cache.releases[completedOwner.requestID], "local TTL eviction must not retry a Redis release for another user")
	require.Zero(t, cache.releases[activeOwner.requestID], "old local timestamps cannot release a live execution")
	require.Zero(t, cache.releases[busy.requestID])
	require.Zero(t, cache.releases[future.requestID])
	require.Len(t, cache.snapshot("user", 42), 1, "admission must preserve the active Redis owner")
}

func TestConcurrencyRejectedAdmissionReturnsRecoveryReservation(t *testing.T) {
	cache := newOwnerRecoveryCache()
	cache.seed("user", 42, "still-running")
	svc := NewConcurrencyService(cache)
	svc.ownerRecovery.capacity = 1
	for i := 0; i < 3; i++ {
		full, err := svc.AcquireUserSlot(context.Background(), 42, 1)
		require.NoError(t, err)
		require.False(t, full.Acquired)
		require.Nil(t, full.ReleaseFunc)
		require.Empty(t, svc.ownerRecovery.owners, "a confirmed capacity rejection must not consume recovery space")
	}
	other, err := svc.AcquireAccountSlot(context.Background(), 19, 1)
	require.NoError(t, err)
	require.True(t, other.Acquired)
	other.ReleaseFunc()
	require.Equal(t, map[string]bool{"still-running": true}, cache.snapshot("user", 42))
}

func TestConcurrencyOwnerRecoveryWorkerReclaimsUncertainAndCompletedOwners(t *testing.T) {
	cache := newOwnerRecoveryCache()
	cache.seed("user", 42, "foreign-user")
	cache.seed("account", 19, "foreign-account")
	svc := NewConcurrencyService(cache)
	user, err := svc.AcquireUserSlot(context.Background(), 42, 2)
	require.NoError(t, err)
	cache.releaseUnavailable = true
	user.ReleaseFunc()
	cache.uncertain = true
	uncertain, err := svc.AcquireAccountSlot(context.Background(), 19, 2)
	require.ErrorContains(t, err, "reply lost")
	require.Nil(t, uncertain)
	require.Len(t, svc.ownerRecovery.owners, 2, "both completed and uncertain owners retain their pre-reserved recovery record")

	cache.releaseUnavailable = false
	svc.recoverPendingConcurrencyOwners(context.Background())
	require.Empty(t, svc.ownerRecovery.owners)
	require.Empty(t, svc.ownerRecovery.pending)
	require.Equal(t, map[string]bool{"foreign-user": true}, cache.snapshot("user", 42))
	require.Equal(t, map[string]bool{"foreign-account": true}, cache.snapshot("account", 19))
	for owner, attempts := range cache.releases {
		require.Equal(t, 2, attempts, "owner %s must have exactly one failed delete and one confirmed recovery", owner)
	}
	svc.recoverPendingConcurrencyOwners(context.Background())
	for _, attempts := range cache.releases {
		require.Equal(t, 2, attempts, "confirmed recoveries must never be retried")
	}
}

func TestConcurrencyOwnerRecoveryRunsInExistingCleanupWorker(t *testing.T) {
	cache := newOwnerRecoveryCache()
	cache.seed("user", 42, "foreign-owner")
	svc := NewConcurrencyService(cache)
	accepted, err := svc.AcquireUserSlot(context.Background(), 42, 2)
	require.NoError(t, err)
	cache.releaseFailures = 1
	accepted.ReleaseFunc()
	require.Len(t, cache.snapshot("user", 42), 2)
	// The existing worker performs one immediate cleanup, then sleeps. Recovery
	// must not depend on another request arriving from this user.
	svc.StartSlotCleanupWorker(nil, time.Hour)
	require.Eventually(t, func() bool {
		if len(cache.snapshot("user", 42)) != 1 {
			return false
		}
		svc.ownerRecovery.mu.Lock()
		defer svc.ownerRecovery.mu.Unlock()
		return len(svc.ownerRecovery.owners) == 0
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, map[string]bool{"foreign-owner": true}, cache.snapshot("user", 42))
	svc.ownerRecovery.mu.Lock()
	defer svc.ownerRecovery.mu.Unlock()
	require.Empty(t, svc.ownerRecovery.owners)
}

func TestConcurrencyOwnerRecoveryWorkerBoundsBatchAndHonorsCancellation(t *testing.T) {
	cache := newOwnerRecoveryCache()
	svc := NewConcurrencyService(cache)
	cache.releaseUnavailable = true
	for i := 0; i < 70; i++ {
		owner := concurrencyOwner{scope: "user", id: int64(i + 1), requestID: fmt.Sprintf("completed-%d", i)}
		require.NoError(t, svc.reserveConcurrencyOwner(owner))
		cache.seed(owner.scope, owner.id, owner.requestID)
		svc.markConcurrencyOwnerPending(owner)
	}
	cache.releaseUnavailable = false
	svc.recoverPendingConcurrencyOwners(context.Background())
	require.Len(t, cache.releases, 64, "one background batch must remain bounded during recovery storms")
	require.Len(t, svc.ownerRecovery.owners, 6)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	svc.recoverPendingConcurrencyOwners(canceled)
	require.Len(t, cache.releases, 64, "an expired cleanup budget cannot issue extra Redis operations")
	for _, state := range svc.ownerRecovery.owners {
		require.False(t, state.busy, "cancelled selections must be available to the next batch")
	}
	svc.recoverPendingConcurrencyOwners(context.Background())
	require.Len(t, cache.releases, 70)
	require.Empty(t, svc.ownerRecovery.owners)
}

func TestConcurrencyOwnerRecoveryAdmissionWaitsForAllPendingOwners(t *testing.T) {
	cache := newOwnerRecoveryCache()
	svc := NewConcurrencyService(cache)
	for i := 0; i < 65; i++ {
		owner := concurrencyOwner{scope: "user", id: 42, requestID: fmt.Sprintf("completed-%d", i)}
		require.NoError(t, svc.reserveConcurrencyOwner(owner))
		cache.seed(owner.scope, owner.id, owner.requestID)
		svc.markConcurrencyOwnerPending(owner)
	}
	blocked, err := svc.AcquireUserSlot(context.Background(), 42, 1)
	require.ErrorIs(t, err, errConcurrencyOwnerRecoveryUnavailable, "a bounded batch must not pretend that unprocessed owners have already been released")
	require.Nil(t, blocked)
	require.Zero(t, cache.acquisitions)
	require.Len(t, cache.snapshot("user", 42), 1)
	next, err := svc.AcquireUserSlot(context.Background(), 42, 1)
	require.NoError(t, err)
	require.True(t, next.Acquired)
	next.ReleaseFunc()
	require.Empty(t, cache.snapshot("user", 42))
	require.Empty(t, svc.ownerRecovery.owners)
}

func TestConcurrencyOwnerRecoveryExpiresOnlyCompletedOwnersAfterConservativeTTL(t *testing.T) {
	cache := newOwnerRecoveryCache()
	svc := NewConcurrencyService(cache)
	active := concurrencyOwner{scope: "user", id: 42, requestID: "active-owner"}
	completed := concurrencyOwner{scope: "account", id: 19, requestID: "completed-owner"}
	require.NoError(t, svc.reserveConcurrencyOwner(active))
	require.NoError(t, svc.reserveConcurrencyOwner(completed))
	before := time.Now()
	svc.markConcurrencyOwnerPending(completed)
	after := time.Now()
	pending := svc.ownerRecovery.owners[completed]
	require.False(t, pending.expiresAt.Before(before.Add(30*time.Minute)), "recovery must not forget an owner before the last possible Redis write has expired")
	require.False(t, pending.expiresAt.After(after.Add(30*time.Minute)))

	// Expired Redis owners no longer need a local recovery reservation. Active
	// executions must retain theirs, however long they have been executing.
	svc.ownerRecovery.owners[active].expiresAt = before.Add(-time.Hour)
	pending.expiresAt = before.Add(-time.Nanosecond)
	svc.recoverPendingConcurrencyOwners(context.Background())
	require.Contains(t, svc.ownerRecovery.owners, active)
	require.NotContains(t, svc.ownerRecovery.owners, completed)
	require.Empty(t, svc.ownerRecovery.pending)
	require.Empty(t, cache.releases, "a proven-expired owner needs no delete, and active owners must never be deleted by recovery")
}

type recoveryTTLCache struct {
	*ownerRecoveryCache
	ttl time.Duration
}

func (c *recoveryTTLCache) ConcurrencySlotTTL() time.Duration { return c.ttl }
func (c *recoveryTTLCache) RefreshUserSlot(context.Context, int64, string) (bool, error) {
	return true, nil
}
func (c *recoveryTTLCache) RefreshAccountSlot(context.Context, int64, string) (bool, error) {
	return true, nil
}

func TestConcurrencyOwnerRecoveryUsesConfiguredRedisExpiry(t *testing.T) {
	cache := &recoveryTTLCache{ownerRecoveryCache: newOwnerRecoveryCache(), ttl: 45 * time.Minute}
	svc := NewConcurrencyService(cache)
	owner := concurrencyOwner{scope: "user", id: 42, requestID: "completed-owner"}
	require.NoError(t, svc.reserveConcurrencyOwner(owner))
	before := time.Now()
	svc.markConcurrencyOwnerPending(owner)
	after := time.Now()
	expiry := svc.ownerRecovery.owners[owner].expiresAt
	require.False(t, expiry.Before(before.Add(45*time.Minute)), "a cache lifetime above the default must not be truncated")
	require.False(t, expiry.After(after.Add(45*time.Minute)))
}

type boundedConcurrencyLookupRepo struct {
	UserRepository
	deadline    time.Time
	hasDeadline bool
}

func (r *boundedConcurrencyLookupRepo) GetByID(ctx context.Context, id int64) (*User, error) {
	r.deadline, r.hasDeadline = ctx.Deadline()
	return &User{ID: id, Concurrency: 3}, nil
}

func TestResolveUserConcurrencyBoundsEveryDatabaseRead(t *testing.T) {
	for _, parentTimeout := range []time.Duration{0, 20 * time.Millisecond} {
		t.Run(parentTimeout.String(), func(t *testing.T) {
			repo := &boundedConcurrencyLookupRepo{}
			svc := NewConcurrencyService(newOwnerRecoveryCache())
			svc.SetUserRepository(repo)
			ctx := context.Background()
			if parentTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, parentTimeout)
				defer cancel()
			}
			started := time.Now()
			limit, err := svc.ResolveUserConcurrency(ctx, 42, 99)
			require.NoError(t, err)
			require.Equal(t, 3, limit)
			require.True(t, repo.hasDeadline, "Live and all direct callers must receive the same bounded authoritative lookup")
			maximum := 2 * time.Second
			if parentTimeout > 0 {
				maximum = parentTimeout
			}
			require.LessOrEqual(t, repo.deadline.Sub(started), maximum+20*time.Millisecond, "the concurrency lookup must neither omit its own budget nor extend a shorter caller deadline")
		})
	}
}
