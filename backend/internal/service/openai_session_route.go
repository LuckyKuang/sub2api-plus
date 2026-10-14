package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// OpenAISessionRouteStatus describes the canonical routing state of one logical
// OpenAI session inside one authorization domain.
type OpenAISessionRouteStatus string

const (
	// OpenAISessionRouteActive means the recorded owner is canonical.
	OpenAISessionRouteActive OpenAISessionRouteStatus = "active"
	// OpenAISessionRouteMigrating means a candidate holds a lease but the owner
	// is still canonical until the migration commits.
	OpenAISessionRouteMigrating OpenAISessionRouteStatus = "migrating"
	// OpenAISessionRouteSuspended means there is no usable owner and the session
	// must not silently fall back to random selection.
	OpenAISessionRouteSuspended OpenAISessionRouteStatus = "suspended"
)

// OpenAISessionRoute is the authoritative route record for one logical session.
// It deliberately carries no prompt text, credentials, or conversation history.
type OpenAISessionRoute struct {
	AuthzDomain        string
	SessionDigest      string
	OwnerAccountID     int64
	Generation         int64
	Status             OpenAISessionRouteStatus
	MigrationReason    string
	CandidateAccountID int64
	LeaseID            string
	LeaseExpiresAt     time.Time
}

var (
	// ErrOpenAISessionRouteNotFound means the domain/session has no route yet.
	ErrOpenAISessionRouteNotFound = errors.New("openai session route not found")
	// ErrOpenAISessionRouteLeaseLost means the CAS expected generation or lease
	// no longer matches, so the caller must not treat its attempt as canonical.
	ErrOpenAISessionRouteLeaseLost = errors.New("openai session route lease lost")
	// ErrOpenAISessionRouteStoreUnavailable means the authoritative store could
	// not be read or written. Callers must surface a recoverable routing error
	// instead of silently selecting another account.
	ErrOpenAISessionRouteStoreUnavailable = errors.New("openai session route store unavailable")
)

// OpenAISessionRouteStore is the authoritative store for logical session routes.
// Implementations must provide compare-and-set semantics on generation/lease.
type OpenAISessionRouteStore interface {
	// Get returns the current route or ErrOpenAISessionRouteNotFound.
	Get(ctx context.Context, authzDomain, sessionDigest string) (OpenAISessionRoute, error)
	// CreateIfAbsent inserts the first route for a session and returns the
	// authoritative record (which may be a concurrent winner).
	CreateIfAbsent(ctx context.Context, route OpenAISessionRoute) (OpenAISessionRoute, error)
	// BeginMigration records a candidate lease when expectedGeneration still
	// matches. It never changes the canonical owner.
	BeginMigration(ctx context.Context, authzDomain, sessionDigest string, expectedGeneration, candidateAccountID int64, leaseID, reason string, leaseTTL time.Duration) (OpenAISessionRoute, error)
	// CommitOwner promotes the leased candidate to the canonical owner and bumps
	// the generation. Late attempts with a stale lease or generation fail with
	// ErrOpenAISessionRouteLeaseLost.
	CommitOwner(ctx context.Context, authzDomain, sessionDigest, leaseID string, candidateAccountID int64) (OpenAISessionRoute, error)
	// AbandonMigration releases a lease without changing the owner.
	AbandonMigration(ctx context.Context, authzDomain, sessionDigest, leaseID string) error
	// MarkSuspended records that no candidate could take over.
	MarkSuspended(ctx context.Context, authzDomain, sessionDigest string, expectedGeneration int64, reason string) error
}

// OpenAISessionRouteDigest derives the irreversible logical-session digest. The
// digest is the only session-shaped value persisted by the authoritative store.
func OpenAISessionRouteDigest(logicalSeed string) string {
	seed := strings.TrimSpace(logicalSeed)
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("sub2api:openai-session-route:v1:" + seed))
	return hex.EncodeToString(sum[:])
}

// openAISessionRouteLeaseActive reports whether a migration lease is still held.
func openAISessionRouteLeaseActive(route OpenAISessionRoute, now time.Time) bool {
	if strings.TrimSpace(route.LeaseID) == "" {
		return false
	}
	return route.LeaseExpiresAt.IsZero() || route.LeaseExpiresAt.After(now)
}

// openAISessionRouteDecision is the canonical-owner decision for one request.
type openAISessionRouteDecision struct {
	// OwnerAccountID is the account the request must use. Zero means the caller
	// must fail with a recoverable routing error instead of selecting randomly.
	OwnerAccountID int64
	// StartMigration asks the caller to acquire a candidate lease for Candidate.
	StartMigration bool
	// Candidate is the desired new owner when StartMigration is true.
	Candidate int64
}

// decideOpenAISessionRoute encodes the migration policy:
//   - a healthy canonical owner always wins, regardless of scoring preferences;
//   - only a permanent failure may start a migration;
//   - transient unavailability (profit gate, 429, short concurrency pressure)
//     keeps the canonical owner and lets the caller wait/retry;
//   - a suspended session needs an explicit candidate before it can migrate.
func decideOpenAISessionRoute(route OpenAISessionRoute, candidate int64, ownerAvailable bool, permanentFailure bool) openAISessionRouteDecision {
	if route.OwnerAccountID <= 0 {
		// No usable owner recorded: establishing the first route is not a
		// cross-owner migration.
		if candidate > 0 {
			return openAISessionRouteDecision{OwnerAccountID: candidate}
		}
		return openAISessionRouteDecision{}
	}
	if ownerAvailable {
		return openAISessionRouteDecision{OwnerAccountID: route.OwnerAccountID}
	}
	if !permanentFailure || candidate <= 0 || candidate == route.OwnerAccountID {
		// Temporary unavailability or no better candidate: keep the canonical
		// owner so a recoverable condition cannot silently move the session.
		if route.Status == OpenAISessionRouteSuspended && candidate > 0 && candidate != route.OwnerAccountID {
			return openAISessionRouteDecision{OwnerAccountID: route.OwnerAccountID, StartMigration: true, Candidate: candidate}
		}
		return openAISessionRouteDecision{OwnerAccountID: route.OwnerAccountID}
	}
	return openAISessionRouteDecision{OwnerAccountID: route.OwnerAccountID, StartMigration: true, Candidate: candidate}
}

// openAISessionRouteStore returns the configured authoritative store, if any.
// A nil store keeps the legacy Redis-only behaviour so existing deployments and
// tests are unaffected until the store is wired in.
func (s *OpenAIGatewayService) openAISessionRouteStore() OpenAISessionRouteStore {
	if s == nil {
		return nil
	}
	return s.sessionRouteStore
}

// openAISessionRoutePreference reports whether this logical session already has
// an authoritative route, so scoring must consult the canonical owner before it
// may pick another account. A store failure also reports true: the authoritative
// read that follows then surfaces the recoverable error instead of silently
// re-scoring the session onto a different account.
func (s *OpenAIGatewayService) openAISessionRoutePreference(ctx context.Context, groupID *int64, sessionHash string) bool {
	store := s.openAISessionRouteStore()
	if store == nil {
		return false
	}
	digest := OpenAISessionRouteDigest(sessionHash)
	if digest == "" {
		return false
	}
	_, err := store.Get(ctx, openAISessionRouteDomain(derefGroupID(groupID)), digest)
	return !errors.Is(err, ErrOpenAISessionRouteNotFound)
}

// openAISessionRouteKeepsCanonicalOwner reports whether the authoritative route
// for this logical session already names accountID as the committed owner.
// Transient quality signals (TTFT, error rate) must not move a committed owner:
// only a permanent unavailability may migrate it, and that path commits a new
// owner explicitly.
func (s *OpenAIGatewayService) openAISessionRouteKeepsCanonicalOwner(ctx context.Context, groupID *int64, sessionHash string, accountID int64) bool {
	store := s.openAISessionRouteStore()
	if store == nil || accountID <= 0 {
		return false
	}
	digest := OpenAISessionRouteDigest(sessionHash)
	if digest == "" {
		return false
	}
	route, err := store.Get(ctx, openAISessionRouteDomain(derefGroupID(groupID)), digest)
	if errors.Is(err, ErrOpenAISessionRouteNotFound) {
		return false
	}
	if err != nil {
		// A store failure must not be read as "no canonical owner": keep the
		// established binding rather than escaping on a transient signal.
		return true
	}
	if route.Status == OpenAISessionRouteSuspended {
		return false
	}
	return route.OwnerAccountID == accountID
}

// openAISessionRouteDomain scopes one authoritative route to a routing group.
// The session value passed alongside it is already tenant scoped by the
// scheduling seed (see scopeOpenAISchedulingSeed), so the pair identifies one
// tenant's logical session.
func openAISessionRouteDomain(groupID int64) string {
	return "group:" + strconv.FormatInt(groupID, 10)
}

// openAISessionRouteLeaseID returns an unguessable migration lease identifier.
func openAISessionRouteLeaseID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "lease-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(buf[:])
}

// upsertOpenAISessionRoute creates the first route for a logical session or
// commits an owner change with a lease-protected CAS. The route is
// authoritative, so a store failure must fail the bind instead of falling back
// to a cache write that would only survive until the next TTL expiry.
func (s *OpenAIGatewayService) upsertOpenAISessionRoute(ctx context.Context, store OpenAISessionRouteStore, groupID int64, sessionHash string, accountID int64, leaseTTL time.Duration) error {
	if store == nil || accountID <= 0 {
		return nil
	}
	domain := openAISessionRouteDomain(groupID)
	digest := OpenAISessionRouteDigest(sessionHash)
	if digest == "" {
		return nil
	}
	current, err := store.Get(ctx, domain, digest)
	if errors.Is(err, ErrOpenAISessionRouteNotFound) {
		_, err = store.CreateIfAbsent(ctx, OpenAISessionRoute{
			AuthzDomain:    domain,
			SessionDigest:  digest,
			OwnerAccountID: accountID,
			Status:         OpenAISessionRouteActive,
		})
		return err
	}
	if err != nil {
		return err
	}
	if current.OwnerAccountID == accountID {
		return nil
	}
	// A different account is taking over. Callers only reach this point for an
	// admitted bind, so hold a lease on the current generation and commit with
	// CAS; a stale or concurrent attempt then fails instead of overwriting.
	leaseID := openAISessionRouteLeaseID()
	reason := strings.TrimSpace(current.MigrationReason)
	if reason == "" {
		reason = "admitted_owner_change"
	}
	if _, err := store.BeginMigration(ctx, domain, digest, current.Generation, accountID, leaseID, reason, leaseTTL); err != nil {
		return err
	}
	if _, err := store.CommitOwner(ctx, domain, digest, leaseID, accountID); err != nil {
		_ = store.AbandonMigration(ctx, domain, digest, leaseID)
		return err
	}
	return nil
}
