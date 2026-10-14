//go:build integration

package repository

import (
	"context"
	"fmt"
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

	// The committed owner is canonical, and the Redis cache carries it.
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
