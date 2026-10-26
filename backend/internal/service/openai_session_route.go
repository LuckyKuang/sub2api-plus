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
	ErrOpenAISessionRouteNotAccepted      = errors.New("openai session candidate has not accepted the request")
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

// openAISessionRouteStore returns the configured authoritative store, if any.
// A nil store keeps the legacy Redis-only behaviour so existing deployments and
// tests are unaffected until the store is wired in.
func (s *OpenAIGatewayService) openAISessionRouteStore() OpenAISessionRouteStore {
	if s == nil {
		return nil
	}
	return s.sessionRouteStore
}

type openAISessionRouteDisabledKey struct{}

// WithoutOpenAISessionRoute keeps excluded requests on ordinary TTL affinity.
// They must not read, create or migrate a text/WS conversation's durable owner.
func WithoutOpenAISessionRoute(ctx context.Context) context.Context {
	if openAISessionRouteDisabled(ctx) {
		return ctx
	}
	return context.WithValue(ctx, openAISessionRouteDisabledKey{}, true)
}

func openAISessionRouteDisabled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	disabled, _ := ctx.Value(openAISessionRouteDisabledKey{}).(bool)
	return disabled
}

func (s *OpenAIGatewayService) openAISessionRouteStoreForRequest(ctx context.Context) OpenAISessionRouteStore {
	if openAISessionRouteDisabled(ctx) {
		return nil
	}
	return s.openAISessionRouteStore()
}

// Only generation paths with admission, renewal, semantic acceptance and
// fencing participate. Platform is the resolved target (including Composite).
func openAISessionRouteApplies(req OpenAIAccountScheduleRequest) bool {
	if (req.Platform != "" && req.Platform != PlatformOpenAI) || req.RequiredImageCapability != "" || req.RequireCompact {
		return false
	}
	switch req.RequiredCapability {
	case "", OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityChatCompletions:
		return true
	default:
		return false
	}
}

// openAISessionRouteCanonicalOwner returns the committed owner of the session's
// authoritative route, or 0 when the session has no route yet (or the store is
// unusable). Callers use it to decide whether a later signal may establish the
// route or must leave an existing owner alone.
func (s *OpenAIGatewayService) openAISessionRouteCanonicalOwner(ctx context.Context, groupID *int64, sessionHash string) int64 {
	store := s.openAISessionRouteStoreForRequest(ctx)
	if store == nil {
		return 0
	}
	digest := OpenAISessionRouteDigest(sessionHash)
	if digest == "" {
		return 0
	}
	route, err := store.Get(ctx, openAISessionRouteDomain(derefGroupID(groupID)), digest)
	if err != nil {
		return 0
	}
	if route.Status == OpenAISessionRouteSuspended {
		return 0
	}
	return route.OwnerAccountID
}

// openAISessionRouteKeepsCanonicalOwner reports whether the authoritative route
// for this logical session already names accountID as the committed owner.
// Transient quality signals (TTFT, error rate) must not move a committed owner:
// only a permanent unavailability may migrate it, and that path commits a new
// owner explicitly.
func (s *OpenAIGatewayService) openAISessionRouteKeepsCanonicalOwner(ctx context.Context, groupID *int64, sessionHash string, accountID int64) bool {
	store := s.openAISessionRouteStoreForRequest(ctx)
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
// verifies an existing owner. Owner changes belong to a verified attempt. The route is
// authoritative, so a store failure must fail the bind instead of falling back
// to a cache write that would only survive until the next TTL expiry.
func (s *OpenAIGatewayService) upsertOpenAISessionRoute(ctx context.Context, store OpenAISessionRouteStore, groupID int64, sessionHash string, accountID int64) error {
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
		winner, createErr := store.CreateIfAbsent(ctx, OpenAISessionRoute{
			AuthzDomain:    domain,
			SessionDigest:  digest,
			OwnerAccountID: accountID,
			Status:         OpenAISessionRouteActive,
		})
		if createErr != nil {
			return createErr
		}
		if winner.OwnerAccountID != accountID {
			return ErrOpenAISessionRouteLeaseLost
		}
		return nil
	}
	if err != nil {
		return err
	}
	if current.OwnerAccountID == accountID {
		return nil
	}
	// Only a selected, admitted and verified attempt can change an owner.
	return ErrOpenAISessionRouteLeaseLost
}
