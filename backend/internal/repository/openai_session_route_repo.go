package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
)

// openAIRouteSQL is the narrow SQL surface the route repository needs so callers
// and tests can inject either the pool or a transaction.
type openAIRouteSQL interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// OpenAISessionRouteRepository stores authoritative logical-session routes in
// PostgreSQL and accelerates reads through Redis. Redis is never authoritative:
// a miss falls back to SQL, and every write invalidates the cached record.
type OpenAISessionRouteRepository struct {
	db       openAIRouteSQL
	rdb      *redis.Client
	cacheTTL time.Duration
}

const openAISessionRouteCachePrefix = "openai_session_route:"

const openAISessionRouteColumns = `authz_domain, session_digest, owner_account_id, generation,
	route_status, migration_reason, COALESCE(candidate_account_id, 0), lease_id, lease_expires_at`

// NewOpenAISessionRouteRepository builds the store. A nil Redis client disables
// caching and reads PostgreSQL directly.
func NewOpenAISessionRouteRepository(db openAIRouteSQL, rdb *redis.Client, cacheTTL time.Duration) *OpenAISessionRouteRepository {
	if cacheTTL <= 0 {
		cacheTTL = time.Hour
	}
	return &OpenAISessionRouteRepository{db: db, rdb: rdb, cacheTTL: cacheTTL}
}

func (r *OpenAISessionRouteRepository) cacheKey(authzDomain, sessionDigest string) string {
	return openAISessionRouteCachePrefix + authzDomain + ":" + sessionDigest
}

type openAIRouteScanner interface {
	Scan(dest ...any) error
}

func scanOpenAISessionRoute(scanner openAIRouteScanner) (service.OpenAISessionRoute, error) {
	var (
		route     service.OpenAISessionRoute
		status    string
		leaseExp  sql.NullTime
		reason    string
		candidate int64
	)
	if err := scanner.Scan(
		&route.AuthzDomain,
		&route.SessionDigest,
		&route.OwnerAccountID,
		&route.Generation,
		&status,
		&reason,
		&candidate,
		&route.LeaseID,
		&leaseExp,
	); err != nil {
		return service.OpenAISessionRoute{}, err
	}
	route.Status = service.OpenAISessionRouteStatus(status)
	route.MigrationReason = reason
	route.CandidateAccountID = candidate
	if leaseExp.Valid {
		route.LeaseExpiresAt = leaseExp.Time
	}
	return route, nil
}

// Get returns the authoritative route, preferring the Redis cache when present.
// The cached record intentionally omits the migration lease: a lease is only
// valid on the value returned by BeginMigration, never on a later read.
func (r *OpenAISessionRouteRepository) Get(ctx context.Context, authzDomain, sessionDigest string) (service.OpenAISessionRoute, error) {
	authzDomain = strings.TrimSpace(authzDomain)
	sessionDigest = strings.TrimSpace(sessionDigest)
	if authzDomain == "" || sessionDigest == "" {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteNotFound
	}
	if route, ok := r.getCached(ctx, authzDomain, sessionDigest); ok {
		return route, nil
	}
	route, err := r.getSQL(ctx, authzDomain, sessionDigest)
	if errors.Is(err, service.ErrOpenAISessionRouteNotFound) {
		return service.OpenAISessionRoute{}, err
	}
	if err != nil {
		return service.OpenAISessionRoute{}, fmt.Errorf("%w: %v", service.ErrOpenAISessionRouteStoreUnavailable, err)
	}
	r.setCached(ctx, route)
	return route, nil
}

func (r *OpenAISessionRouteRepository) getSQL(ctx context.Context, authzDomain, sessionDigest string) (service.OpenAISessionRoute, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+openAISessionRouteColumns+` FROM openai_session_routes WHERE authz_domain = $1 AND session_digest = $2`,
		authzDomain, sessionDigest)
	route, err := scanOpenAISessionRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteNotFound
	}
	if err != nil {
		return service.OpenAISessionRoute{}, err
	}
	return route, nil
}

// CreateIfAbsent inserts the first route for a logical session. A concurrent
// winner is returned unchanged instead of overwriting it.
func (r *OpenAISessionRouteRepository) CreateIfAbsent(ctx context.Context, route service.OpenAISessionRoute) (service.OpenAISessionRoute, error) {
	if route.OwnerAccountID <= 0 || strings.TrimSpace(route.AuthzDomain) == "" || strings.TrimSpace(route.SessionDigest) == "" {
		return service.OpenAISessionRoute{}, fmt.Errorf("%w: incomplete route", service.ErrOpenAISessionRouteStoreUnavailable)
	}
	if route.Generation <= 0 {
		route.Generation = 1
	}
	if route.Status == "" {
		route.Status = service.OpenAISessionRouteActive
	}
	created, err := scanOpenAISessionRoute(r.db.QueryRowContext(ctx,
		`INSERT INTO openai_session_routes (authz_domain, session_digest, owner_account_id, generation, route_status, migration_reason)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (authz_domain, session_digest) DO NOTHING
		 RETURNING `+openAISessionRouteColumns,
		route.AuthzDomain, route.SessionDigest, route.OwnerAccountID, route.Generation, string(route.Status), route.MigrationReason))
	if err == nil {
		r.invalidate(ctx, route.AuthzDomain, route.SessionDigest)
		return created, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return service.OpenAISessionRoute{}, fmt.Errorf("%w: %v", service.ErrOpenAISessionRouteStoreUnavailable, err)
	}
	// A concurrent request created the route first: return the winner.
	return r.Get(ctx, route.AuthzDomain, route.SessionDigest)
}

// BeginMigration records a candidate lease without changing the canonical owner.
// The generation must still match, and an unexpired lease owned by someone else
// blocks the attempt.
func (r *OpenAISessionRouteRepository) BeginMigration(
	ctx context.Context,
	authzDomain, sessionDigest string,
	expectedGeneration, candidateAccountID int64,
	leaseID, reason string,
	leaseTTL time.Duration,
) (service.OpenAISessionRoute, error) {
	if expectedGeneration <= 0 || candidateAccountID <= 0 || strings.TrimSpace(leaseID) == "" {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
	}
	ttlSeconds := int64(leaseTTL / time.Second)
	if ttlSeconds <= 0 {
		ttlSeconds = 60
	}
	route, err := scanOpenAISessionRoute(r.db.QueryRowContext(ctx,
		`UPDATE openai_session_routes
		 SET route_status = 'migrating', candidate_account_id = $4, lease_id = $5,
		     lease_expires_at = NOW() + ($6 * INTERVAL '1 second'), migration_reason = $7, updated_at = NOW()
		 WHERE authz_domain = $1 AND session_digest = $2 AND generation = $3
		   AND (lease_id = '' OR lease_expires_at IS NULL OR lease_expires_at < NOW() OR lease_id = $5)
		 RETURNING `+openAISessionRouteColumns,
		authzDomain, sessionDigest, expectedGeneration, candidateAccountID, leaseID, ttlSeconds, reason))
	if errors.Is(err, sql.ErrNoRows) {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
	}
	if err != nil {
		return service.OpenAISessionRoute{}, fmt.Errorf("%w: %v", service.ErrOpenAISessionRouteStoreUnavailable, err)
	}
	r.invalidate(ctx, authzDomain, sessionDigest)
	return route, nil
}

// CommitOwner promotes the leased candidate and bumps the generation. A stale
// lease or generation fails with ErrOpenAISessionRouteLeaseLost, so a late
// attempt can never overwrite a newer owner.
func (r *OpenAISessionRouteRepository) CommitOwner(ctx context.Context, authzDomain, sessionDigest, leaseID string, candidateAccountID int64) (service.OpenAISessionRoute, error) {
	route, err := scanOpenAISessionRoute(r.db.QueryRowContext(ctx,
		`UPDATE openai_session_routes
		 SET owner_account_id = $4, generation = generation + 1, route_status = 'active',
		     candidate_account_id = NULL, lease_id = '', lease_expires_at = NULL,
		     migration_reason = '', updated_at = NOW()
		 WHERE authz_domain = $1 AND session_digest = $2 AND lease_id = $3 AND candidate_account_id = $4
		   AND lease_expires_at IS NOT NULL AND lease_expires_at >= NOW()
		 RETURNING `+openAISessionRouteColumns,
		authzDomain, sessionDigest, leaseID, candidateAccountID))
	if errors.Is(err, sql.ErrNoRows) {
		return service.OpenAISessionRoute{}, service.ErrOpenAISessionRouteLeaseLost
	}
	if err != nil {
		return service.OpenAISessionRoute{}, fmt.Errorf("%w: %v", service.ErrOpenAISessionRouteStoreUnavailable, err)
	}
	r.invalidate(ctx, authzDomain, sessionDigest)
	return route, nil
}

// AbandonMigration releases a lease without touching the canonical owner.
func (r *OpenAISessionRouteRepository) AbandonMigration(ctx context.Context, authzDomain, sessionDigest, leaseID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE openai_session_routes
		 SET route_status = 'active', candidate_account_id = NULL, lease_id = '', lease_expires_at = NULL, updated_at = NOW()
		 WHERE authz_domain = $1 AND session_digest = $2 AND lease_id = $3`,
		authzDomain, sessionDigest, leaseID)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrOpenAISessionRouteStoreUnavailable, err)
	}
	r.invalidate(ctx, authzDomain, sessionDigest)
	return nil
}

// MarkSuspended records that no candidate could take over the session.
func (r *OpenAISessionRouteRepository) MarkSuspended(ctx context.Context, authzDomain, sessionDigest string, expectedGeneration int64, reason string) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE openai_session_routes
		 SET route_status = 'suspended', candidate_account_id = NULL, lease_id = '', lease_expires_at = NULL,
		     migration_reason = $4, updated_at = NOW()
		 WHERE authz_domain = $1 AND session_digest = $2 AND generation = $3`,
		authzDomain, sessionDigest, expectedGeneration, reason)
	if err != nil {
		return fmt.Errorf("%w: %v", service.ErrOpenAISessionRouteStoreUnavailable, err)
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr == nil && affected == 0 {
		return service.ErrOpenAISessionRouteLeaseLost
	}
	r.invalidate(ctx, authzDomain, sessionDigest)
	return nil
}

func (r *OpenAISessionRouteRepository) getCached(ctx context.Context, authzDomain, sessionDigest string) (service.OpenAISessionRoute, bool) {
	if r.rdb == nil {
		return service.OpenAISessionRoute{}, false
	}
	value, err := r.rdb.Get(ctx, r.cacheKey(authzDomain, sessionDigest)).Result()
	if err != nil {
		return service.OpenAISessionRoute{}, false
	}
	route, ok := decodeOpenAISessionRouteCache(value, authzDomain, sessionDigest)
	if !ok {
		// A malformed entry must never be authoritative.
		_ = r.rdb.Del(ctx, r.cacheKey(authzDomain, sessionDigest)).Err()
		return service.OpenAISessionRoute{}, false
	}
	return route, true
}

func (r *OpenAISessionRouteRepository) setCached(ctx context.Context, route service.OpenAISessionRoute) {
	if r.rdb == nil {
		return
	}
	_ = r.rdb.Set(ctx, r.cacheKey(route.AuthzDomain, route.SessionDigest), encodeOpenAISessionRouteCache(route), r.cacheTTL).Err()
}

func (r *OpenAISessionRouteRepository) invalidate(ctx context.Context, authzDomain, sessionDigest string) {
	if r.rdb == nil {
		return
	}
	if err := r.rdb.Del(ctx, r.cacheKey(authzDomain, sessionDigest)).Err(); err != nil {
		slog.Warn("openai_session_route_cache_invalidate_failed",
			"authz_domain", authzDomain, "error", err)
	}
}

// encodeOpenAISessionRouteCache keeps the cached generation next to the owner so
// a stale entry is detectable and can never hide a newer committed route.
func encodeOpenAISessionRouteCache(route service.OpenAISessionRoute) string {
	return strings.Join([]string{
		strconv.FormatInt(route.OwnerAccountID, 10),
		strconv.FormatInt(route.Generation, 10),
		string(route.Status),
		strconv.FormatInt(route.CandidateAccountID, 10),
		route.MigrationReason,
	}, "|")
}

func decodeOpenAISessionRouteCache(value, authzDomain, sessionDigest string) (service.OpenAISessionRoute, bool) {
	parts := strings.Split(value, "|")
	if len(parts) != 5 {
		return service.OpenAISessionRoute{}, false
	}
	owner, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || owner <= 0 {
		return service.OpenAISessionRoute{}, false
	}
	generation, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || generation <= 0 {
		return service.OpenAISessionRoute{}, false
	}
	candidate, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return service.OpenAISessionRoute{}, false
	}
	return service.OpenAISessionRoute{
		AuthzDomain:        authzDomain,
		SessionDigest:      sessionDigest,
		OwnerAccountID:     owner,
		Generation:         generation,
		Status:             service.OpenAISessionRouteStatus(parts[2]),
		CandidateAccountID: candidate,
		MigrationReason:    parts[4],
	}, true
}
