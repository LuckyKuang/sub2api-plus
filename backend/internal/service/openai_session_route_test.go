//go:build unit || !integration

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAISessionRouteDigestIsStableAndOpaque(t *testing.T) {
	first := OpenAISessionRouteDigest("session-alpha")
	require.NotEmpty(t, first)
	require.Equal(t, first, OpenAISessionRouteDigest("  session-alpha  "), "the digest must ignore surrounding whitespace")
	require.NotEqual(t, first, OpenAISessionRouteDigest("session-beta"))
	require.NotContains(t, first, "session-alpha", "the persisted digest must not carry the raw session value")
	require.Equal(t, 64, len(first), "sha256 hex digest")
	require.Empty(t, OpenAISessionRouteDigest("   "))
}

// fakeOpenAISessionRouteStore implements the store contract with the same
// compare-and-set rules as the PostgreSQL repository.
type fakeOpenAISessionRouteStore struct {
	mu     sync.Mutex
	now    time.Time
	routes map[string]OpenAISessionRoute
	fail   error
}

func newFakeOpenAISessionRouteStore() *fakeOpenAISessionRouteStore {
	return &fakeOpenAISessionRouteStore{now: time.Now(), routes: map[string]OpenAISessionRoute{}}
}

func fakeRouteKey(domain, digest string) string { return domain + "\x00" + digest }

func (f *fakeOpenAISessionRouteStore) Get(_ context.Context, domain, digest string) (OpenAISessionRoute, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return OpenAISessionRoute{}, f.fail
	}
	route, ok := f.routes[fakeRouteKey(domain, digest)]
	if !ok {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteNotFound
	}
	return route, nil
}

func (f *fakeOpenAISessionRouteStore) CreateIfAbsent(_ context.Context, route OpenAISessionRoute) (OpenAISessionRoute, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeRouteKey(route.AuthzDomain, route.SessionDigest)
	if existing, ok := f.routes[key]; ok {
		return existing, nil
	}
	if route.Generation <= 0 {
		route.Generation = 1
	}
	if route.Status == "" {
		route.Status = OpenAISessionRouteActive
	}
	f.routes[key] = route
	return route, nil
}

func (f *fakeOpenAISessionRouteStore) BeginMigration(_ context.Context, domain, digest string, expectedGeneration, candidate int64, leaseID, reason string, leaseTTL time.Duration) (OpenAISessionRoute, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeRouteKey(domain, digest)
	route, ok := f.routes[key]
	if !ok {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteNotFound
	}
	if route.Generation != expectedGeneration {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteLeaseLost
	}
	if (openAISessionRouteLeaseActive(route, f.now) && (route.LeaseID != leaseID || route.CandidateAccountID != candidate)) ||
		(route.LeaseID == leaseID && !openAISessionRouteLeaseActive(route, f.now)) {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteLeaseLost
	}
	route.Status = OpenAISessionRouteMigrating
	route.CandidateAccountID = candidate
	route.LeaseID = leaseID
	route.MigrationReason = reason
	route.LeaseExpiresAt = f.now.Add(leaseTTL)
	f.routes[key] = route
	return route, nil
}

func (f *fakeOpenAISessionRouteStore) CommitOwner(_ context.Context, domain, digest, leaseID string, candidate int64) (OpenAISessionRoute, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeRouteKey(domain, digest)
	route, ok := f.routes[key]
	if !ok {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteNotFound
	}
	if route.LeaseID == "" || route.LeaseID != leaseID || route.CandidateAccountID != candidate {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteLeaseLost
	}
	if !route.LeaseExpiresAt.IsZero() && !route.LeaseExpiresAt.After(f.now) {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteLeaseLost
	}
	route.OwnerAccountID = candidate
	route.Generation++
	route.Status = OpenAISessionRouteActive
	route.CandidateAccountID = 0
	route.LeaseID = ""
	route.LeaseExpiresAt = time.Time{}
	route.MigrationReason = ""
	f.routes[key] = route
	return route, nil
}

func (f *fakeOpenAISessionRouteStore) AbandonMigration(_ context.Context, domain, digest, leaseID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeRouteKey(domain, digest)
	route, ok := f.routes[key]
	if !ok || route.LeaseID != leaseID {
		return nil
	}
	route.Status = OpenAISessionRouteActive
	route.CandidateAccountID = 0
	route.LeaseID = ""
	route.LeaseExpiresAt = time.Time{}
	f.routes[key] = route
	return nil
}

func (f *fakeOpenAISessionRouteStore) MarkSuspended(_ context.Context, domain, digest string, expectedGeneration int64, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeRouteKey(domain, digest)
	route, ok := f.routes[key]
	if !ok || route.Generation != expectedGeneration {
		return ErrOpenAISessionRouteLeaseLost
	}
	route.Status = OpenAISessionRouteSuspended
	route.MigrationReason = reason
	route.CandidateAccountID = 0
	route.LeaseID = ""
	route.LeaseExpiresAt = time.Time{}
	f.routes[key] = route
	return nil
}

func TestOpenAISessionRouteStoreAccessorIsNilSafe(t *testing.T) {
	var nilService *OpenAIGatewayService
	require.Nil(t, nilService.openAISessionRouteStore())

	store := newFakeOpenAISessionRouteStore()
	svc := &OpenAIGatewayService{sessionRouteStore: store}
	require.Same(t, store, svc.openAISessionRouteStore())
}

func TestOpenAISessionRouteIsAuthoritativeForStickyReads(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	sessionHash := "tenant-scoped-session"
	store := newFakeOpenAISessionRouteStore()
	cacheKey := (&OpenAIGatewayService{}).openAISessionCacheKey(sessionHash)
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{cacheKey: 100}}
	svc := &OpenAIGatewayService{cache: cache, sessionRouteStore: store}

	// A committed route wins over a stale cache binding.
	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{
		AuthzDomain:    openAISessionRouteDomain(groupID),
		SessionDigest:  OpenAISessionRouteDigest(sessionHash),
		OwnerAccountID: 200,
		Status:         OpenAISessionRouteActive,
	})
	require.NoError(t, err)
	owner, err := svc.getStickySessionAccountID(ctx, &groupID, sessionHash)
	require.NoError(t, err)
	require.EqualValues(t, 200, owner, "the authoritative owner must survive a stale or expired cache entry")

	// Without a route the legacy cache path still answers.
	svc = &OpenAIGatewayService{cache: cache, sessionRouteStore: newFakeOpenAISessionRouteStore()}
	owner, err = svc.getStickySessionAccountID(ctx, &groupID, sessionHash)
	require.NoError(t, err)
	require.EqualValues(t, 100, owner)

	// An unreachable authoritative store must surface a recoverable error
	// instead of silently falling back to the cache.
	failing := newFakeOpenAISessionRouteStore()
	failing.fail = ErrOpenAISessionRouteStoreUnavailable
	svc = &OpenAIGatewayService{cache: cache, sessionRouteStore: failing}
	_, err = svc.getStickySessionAccountID(ctx, &groupID, sessionHash)
	require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
}

func TestBindStickySessionCannotMigrateWithoutAcceptedAttempt(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	sessionHash := "tenant-scoped-session"
	store := newFakeOpenAISessionRouteStore()
	svc := &OpenAIGatewayService{sessionRouteStore: store}
	domain := openAISessionRouteDomain(groupID)
	digest := OpenAISessionRouteDigest(sessionHash)

	require.NoError(t, svc.BindStickySession(ctx, &groupID, sessionHash, 100))
	route, err := store.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 100, route.OwnerAccountID)
	require.EqualValues(t, 1, route.Generation)

	// Re-binding the same owner keeps the generation: it is not a migration.
	require.NoError(t, svc.BindStickySession(ctx, &groupID, sessionHash, 100))
	route, err = store.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 1, route.Generation)

	// Selection/binding alone is not evidence of candidate acceptance.
	require.ErrorIs(t, svc.BindStickySession(ctx, &groupID, sessionHash, 101), ErrOpenAISessionRouteLeaseLost)
	route, err = store.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 100, route.OwnerAccountID)
	require.EqualValues(t, 1, route.Generation)
	require.Equal(t, OpenAISessionRouteActive, route.Status)
}

// A03: profit and cache changes cannot authorize a migration. Only accepted
// attempts carrying the original generation may commit it.
func TestProfitGateKeepsCanonicalOwnerUntilAdmittedOwnerChange(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	sessionHash := "tenant-scoped-session"
	store := newFakeOpenAISessionRouteStore()
	cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
	svc := &OpenAIGatewayService{cache: cache, sessionRouteStore: store}
	domain := openAISessionRouteDomain(groupID)
	digest := OpenAISessionRouteDigest(sessionHash)

	require.NoError(t, svc.BindStickySession(ctx, &groupID, sessionHash, 100))

	gateCtx := context.WithValue(ctx, openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{threshold: 1, pricingAt: time.Now()})
	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(gateCtx, &groupID, sessionHash, 101))
	route, err := store.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 100, route.OwnerAccountID, "a transient profit veto must not move the canonical owner")
	require.EqualValues(t, 1, route.Generation)

	// Deleting a cache entry cannot authorize replacing the SQL owner.
	cache.sessionBindings = map[string]int64{}
	require.ErrorIs(t, svc.BindStickySessionAfterProfitAdmission(ctx, &groupID, sessionHash, 101), ErrOpenAISessionRouteLeaseLost)
	route, err = store.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 100, route.OwnerAccountID)
	require.EqualValues(t, 1, route.Generation)
}

func TestOpenAISessionRouteLifecycleKeepsOwnerUntilCommit(t *testing.T) {
	ctx := context.Background()
	store := newFakeOpenAISessionRouteStore()
	domain := "user:7:key:42"
	digest := OpenAISessionRouteDigest("logical-session")

	created, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 100,
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, created.OwnerAccountID)
	require.EqualValues(t, 1, created.Generation)

	// A concurrent first bind must not steal the route.
	again, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 999,
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, again.OwnerAccountID)

	// A stale generation cannot start a migration.
	_, err = store.BeginMigration(ctx, domain, digest, 7, 101, "lease-a", "quota_exhausted", time.Minute)
	require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)

	migrating, err := store.BeginMigration(ctx, domain, digest, 1, 101, "lease-a", "quota_exhausted", time.Minute)
	require.NoError(t, err)
	require.Equal(t, OpenAISessionRouteMigrating, migrating.Status)
	require.EqualValues(t, 100, migrating.OwnerAccountID, "the owner stays canonical until the commit")
	require.EqualValues(t, 101, migrating.CandidateAccountID)

	// A second attempt cannot take the lease while it is held.
	_, err = store.BeginMigration(ctx, domain, digest, 1, 102, "lease-b", "quota_exhausted", time.Minute)
	require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)

	// Only the lease holder may commit.
	_, err = store.CommitOwner(ctx, domain, digest, "lease-b", 101)
	require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)

	committed, err := store.CommitOwner(ctx, domain, digest, "lease-a", 101)
	require.NoError(t, err)
	require.EqualValues(t, 101, committed.OwnerAccountID)
	require.EqualValues(t, 2, committed.Generation)
	require.Equal(t, OpenAISessionRouteActive, committed.Status)

	// A late attempt with the consumed lease must not overwrite the new owner.
	_, err = store.CommitOwner(ctx, domain, digest, "lease-a", 101)
	require.ErrorIs(t, err, ErrOpenAISessionRouteLeaseLost)

	// The migrated owner is canonical after commit.
	final, err := store.Get(ctx, domain, digest)
	require.NoError(t, err)
	require.EqualValues(t, 101, final.OwnerAccountID)
	require.EqualValues(t, 2, final.Generation)
}

// A committed owner must not be escaped on a transient quality signal (TTFT or
// error rate); only a different or suspended owner may move the session.
func TestOpenAISessionRouteKeepsCanonicalOwnerOnlyForItsCommittedOwner(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	sessionHash := "tenant-scoped-session"
	domain := openAISessionRouteDomain(groupID)
	digest := OpenAISessionRouteDigest(sessionHash)
	store := newFakeOpenAISessionRouteStore()
	svc := &OpenAIGatewayService{sessionRouteStore: store}

	require.False(t, svc.openAISessionRouteKeepsCanonicalOwner(ctx, &groupID, sessionHash, 100), "no route yet")

	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 100,
	})
	require.NoError(t, err)
	require.True(t, svc.openAISessionRouteKeepsCanonicalOwner(ctx, &groupID, sessionHash, 100))
	require.False(t, svc.openAISessionRouteKeepsCanonicalOwner(ctx, &groupID, sessionHash, 101), "another account is not the committed owner")

	// A suspended route must not pin the session to an unusable owner.
	require.NoError(t, store.MarkSuspended(ctx, domain, digest, 1, "no_candidate"))
	require.False(t, svc.openAISessionRouteKeepsCanonicalOwner(ctx, &groupID, sessionHash, 100))

	// A store failure must not be read as "no canonical owner": the established
	// binding is kept instead of escaping on a transient signal.
	failing := newFakeOpenAISessionRouteStore()
	failing.fail = ErrOpenAISessionRouteStoreUnavailable
	svc = &OpenAIGatewayService{sessionRouteStore: failing}
	require.True(t, svc.openAISessionRouteKeepsCanonicalOwner(ctx, &groupID, sessionHash, 100))
}

// A stale response binding may establish the first route or confirm the current
// owner, but it must never migrate a session whose committed owner is another
// account.
func TestOpenAISessionRouteCanonicalOwnerGatesStaleResponseBinding(t *testing.T) {
	ctx := context.Background()
	groupID := int64(7)
	sessionHash := "tenant-scoped-session"
	domain := openAISessionRouteDomain(groupID)
	digest := OpenAISessionRouteDigest(sessionHash)
	store := newFakeOpenAISessionRouteStore()
	svc := &OpenAIGatewayService{sessionRouteStore: store}

	// No route yet: the caller may establish it (0 means "no committed owner").
	require.EqualValues(t, 0, svc.openAISessionRouteCanonicalOwner(ctx, &groupID, sessionHash))

	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{
		AuthzDomain: domain, SessionDigest: digest, OwnerAccountID: 100,
	})
	require.NoError(t, err)
	require.EqualValues(t, 100, svc.openAISessionRouteCanonicalOwner(ctx, &groupID, sessionHash))

	// A suspended route has no usable owner, so a new binding may be established.
	require.NoError(t, store.MarkSuspended(ctx, domain, digest, 1, "no_candidate"))
	require.EqualValues(t, 0, svc.openAISessionRouteCanonicalOwner(ctx, &groupID, sessionHash))

	// A store failure must not be read as "no committed owner": it returns 0 only
	// because the bind that follows fails on the same store, so no owner moves.
	failing := newFakeOpenAISessionRouteStore()
	failing.fail = ErrOpenAISessionRouteStoreUnavailable
	svc = &OpenAIGatewayService{sessionRouteStore: failing}
	require.EqualValues(t, 0, svc.openAISessionRouteCanonicalOwner(ctx, &groupID, sessionHash))
}
