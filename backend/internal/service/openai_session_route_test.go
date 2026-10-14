//go:build unit || !integration

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestDecideOpenAISessionRoute(t *testing.T) {
	activeOwner := OpenAISessionRoute{OwnerAccountID: 100, Generation: 3, Status: OpenAISessionRouteActive}
	suspended := OpenAISessionRoute{OwnerAccountID: 100, Generation: 3, Status: OpenAISessionRouteSuspended}

	tests := []struct {
		name             string
		route            OpenAISessionRoute
		candidate        int64
		ownerAvailable   bool
		permanentFailure bool
		wantOwner        int64
		wantMigrate      bool
	}{
		{
			name:  "healthy canonical owner wins over a preferred candidate",
			route: activeOwner, candidate: 101, ownerAvailable: true,
			wantOwner: 100,
		},
		{
			name:  "permanent failure migrates to the candidate",
			route: activeOwner, candidate: 101, ownerAvailable: false, permanentFailure: true,
			wantOwner: 100, wantMigrate: true,
		},
		{
			name:  "transient unavailability keeps the canonical owner",
			route: activeOwner, candidate: 101, ownerAvailable: false, permanentFailure: false,
			wantOwner: 100,
		},
		{
			name:  "transient unavailability never silently uses another account",
			route: activeOwner, candidate: 0, ownerAvailable: false, permanentFailure: false,
			wantOwner: 100,
		},
		{
			name:  "permanent failure without a candidate keeps the owner and does not migrate",
			route: activeOwner, candidate: 0, ownerAvailable: false, permanentFailure: true,
			wantOwner: 100,
		},
		{
			name:  "permanent failure with the owner as candidate is not a migration",
			route: activeOwner, candidate: 100, ownerAvailable: false, permanentFailure: true,
			wantOwner: 100,
		},
		{
			name:  "suspended session may migrate once a candidate exists",
			route: suspended, candidate: 101, ownerAvailable: false, permanentFailure: false,
			wantOwner: 100, wantMigrate: true,
		},
		{
			name:  "suspended session without a candidate stays put",
			route: suspended, candidate: 0, ownerAvailable: false, permanentFailure: false,
			wantOwner: 100,
		},
		{
			name:  "missing owner adopts the candidate as the first route",
			route: OpenAISessionRoute{}, candidate: 100, ownerAvailable: false,
			wantOwner: 100,
		},
		{
			name:  "missing owner without candidate yields no owner",
			route: OpenAISessionRoute{}, candidate: 0, ownerAvailable: false,
			wantOwner: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideOpenAISessionRoute(tt.route, tt.candidate, tt.ownerAvailable, tt.permanentFailure)
			assert.Equal(t, tt.wantOwner, got.OwnerAccountID)
			assert.Equal(t, tt.wantMigrate, got.StartMigration)
			if tt.wantMigrate {
				assert.Equal(t, tt.candidate, got.Candidate)
			}
		})
	}
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
	if openAISessionRouteLeaseActive(route, f.now) && route.LeaseID != leaseID {
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
