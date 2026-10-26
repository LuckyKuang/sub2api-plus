//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The authoritative store must provide compare-and-set semantics on the
// generation and the migration lease: a late attempt can never overwrite a
// newer owner.
func TestOpenAISessionRouteRepositoryCASLifecycle(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	repo := NewOpenAISessionRouteRepository(tx, integrationRedis, time.Minute)
	domain := fmt.Sprintf("test:%s:%d", t.Name(), time.Now().UnixNano())
	digest := service.OpenAISessionRouteDigest("logical-session")

	created, err := repo.CreateIfAbsent(ctx, service.OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 100,
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, created.OwnerAccountID)
	require.EqualValues(t, 1, created.Generation)

	// A concurrent first bind must not steal the route.
	again, err := repo.CreateIfAbsent(ctx, service.OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 999,
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, again.OwnerAccountID)

	// A stale generation cannot start a migration.
	_, err = repo.BeginMigration(ctx, domain, digest, 7, 101, "lease-a", "quota_exhausted", time.Minute)
	require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost)

	migrating, err := repo.BeginMigration(ctx, domain, digest, 1, 101, "lease-a", "quota_exhausted", time.Minute)
	require.NoError(t, err)
	require.Equal(t, service.OpenAISessionRouteMigrating, migrating.Status)
	require.EqualValues(t, 100, migrating.OwnerAccountID, "the owner stays canonical until the commit")
	require.EqualValues(t, 101, migrating.CandidateAccountID)

	// A second attempt cannot take a held lease.
	_, err = repo.BeginMigration(ctx, domain, digest, 1, 102, "lease-b", "quota_exhausted", time.Minute)
	require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost)

	// Only the lease holder may commit.
	_, err = repo.CommitOwner(ctx, domain, digest, "lease-b", 101)
	require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost)

	committed, err := repo.CommitOwner(ctx, domain, digest, "lease-a", 101)
	require.NoError(t, err)
	require.EqualValues(t, 101, committed.OwnerAccountID)
	require.EqualValues(t, 2, committed.Generation)
	require.Equal(t, service.OpenAISessionRouteActive, committed.Status)

	// A late attempt with the consumed lease must not overwrite the new owner.
	_, err = repo.CommitOwner(ctx, domain, digest, "lease-a", 101)
	require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost)

	// The committed SQL owner is canonical across repeated reads.
	reloaded, err := repo.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 101, reloaded.OwnerAccountID)
	require.EqualValues(t, 2, reloaded.Generation)

	cached, err := repo.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 101, cached.OwnerAccountID)
	require.EqualValues(t, 2, cached.Generation)

	// Abandon releases a lease without moving the owner.
	leased, err := repo.BeginMigration(ctx, domain, digest, 2, 103, "lease-c", "probe", time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 101, leased.OwnerAccountID)
	require.NoError(t, repo.AbandonMigration(ctx, domain, digest, "lease-c"))
	abandoned, err := repo.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.Equal(t, service.OpenAISessionRouteActive, abandoned.Status)
	require.EqualValues(t, 101, abandoned.OwnerAccountID)
	require.EqualValues(t, 2, abandoned.Generation)
}

// V04/V14/V15: stale Redis fills cannot roll back SQL ownership, and renewing
// an expired lease cannot resurrect a request that has lost acceptance rights.
func TestOpenAISessionRouteRepositoryStaleCacheAndExpiredLease(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	repo := NewOpenAISessionRouteRepository(tx, integrationRedis, time.Minute)
	domain := fmt.Sprintf("test:%s:%d", t.Name(), time.Now().UnixNano())
	digest := service.OpenAISessionRouteDigest("session")
	_, err = repo.CreateIfAbsent(ctx, service.OpenAISessionRoute{AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 101})
	require.NoError(t, err)
	_, err = repo.BeginMigration(ctx, domain, digest, 1, 102, "expired", "credential_invalid", time.Minute)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE openai_session_routes SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE authz_domain=$1`, domain)
	require.NoError(t, err)
	_, err = repo.BeginMigration(ctx, domain, digest, 1, 102, "expired", "credential_invalid", time.Minute)
	require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost)
	_, err = repo.CommitOwner(ctx, domain, digest, "expired", 102)
	require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost)
	_, err = repo.BeginMigration(ctx, domain, digest, 1, 103, "new-attempt", "credential_invalid", time.Minute)
	require.NoError(t, err)
	_, err = repo.CommitOwner(ctx, domain, digest, "new-attempt", 103)
	require.NoError(t, err)
	key := repo.cacheKey(domain, digest)
	require.NoError(t, integrationRedis.Set(ctx, key, `{"authz_domain":"old","owner_account_id":101,"generation":1}`, time.Minute).Err())
	defer integrationRedis.Del(ctx, key)
	got, err := repo.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 103, got.OwnerAccountID)
	require.EqualValues(t, 2, got.Generation)
}

// Concurrent attempts must produce exactly one migration lease: the rest lose the
// generation race and can never commit. This closes the audit's "CAS reviewed but
// never raced" evidence gap with the real database and connection pool.
func TestOpenAISessionRouteRepositoryConcurrentMigration(t *testing.T) {
	ctx := context.Background()
	domain := fmt.Sprintf("test:%s:%d", t.Name(), time.Now().UnixNano())
	digest := service.OpenAISessionRouteDigest("logical-session")
	repo := NewOpenAISessionRouteRepository(integrationDB, integrationRedis, time.Minute)
	defer func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM openai_session_routes WHERE authz_domain = $1`, domain)
	}()

	created, err := repo.CreateIfAbsent(ctx, service.OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 100,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, created.Generation)

	const attempts = 8
	type leaseResult struct {
		index   int
		leaseID string
		ok      bool
	}
	results := make(chan leaseResult, attempts)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			leaseID := fmt.Sprintf("lease-%d", index)
			<-start
			_, err := repo.BeginMigration(ctx, domain, digest, 1, int64(200+index), leaseID, "quota_exhausted", time.Minute)
			results <- leaseResult{index: index, leaseID: leaseID, ok: err == nil}
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	var winner leaseResult
	for result := range results {
		if result.ok {
			winners++
			winner = result
		}
	}
	require.Equal(t, 1, winners, "exactly one concurrent attempt may hold the migration lease")

	// The canonical owner is unchanged while the lease is held.
	leased, err := repo.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 100, leased.OwnerAccountID)
	require.Equal(t, service.OpenAISessionRouteMigrating, leased.Status)
	require.EqualValues(t, 200+winner.index, leased.CandidateAccountID)

	// Only the lease winner commits, and it bumps the generation.
	committed, err := repo.CommitOwner(ctx, domain, digest, winner.leaseID, int64(200+winner.index))
	require.NoError(t, err)
	require.EqualValues(t, 200+winner.index, committed.OwnerAccountID)
	require.EqualValues(t, 2, committed.Generation)

	// Every losing attempt can never overwrite the committed owner.
	for i := 0; i < attempts; i++ {
		if i == winner.index {
			continue
		}
		_, err := repo.CommitOwner(ctx, domain, digest, fmt.Sprintf("lease-%d", i), int64(200+i))
		require.ErrorIs(t, err, service.ErrOpenAISessionRouteLeaseLost, "loser %d must not commit", i)
	}
	final, err := repo.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 200+winner.index, final.OwnerAccountID)
	require.EqualValues(t, 2, final.Generation)
}
