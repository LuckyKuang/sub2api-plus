package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	accountExportConfirmationFailureLimit  = 5
	accountExportConfirmationFailureWindow = 15 * time.Minute
	accountExportConfirmationKeyPrefix     = "security:account-export-confirmation:"
)

// AccountExportConfirmationLimiter is a Redis-backed failure-only limiter.
// The IP is hashed before it is used in a cache key to avoid retaining a raw
// client address outside the audit record that already needs it.
type AccountExportConfirmationLimiter struct {
	rdb *redis.Client
}

func NewAccountExportConfirmationLimiter(rdb *redis.Client) service.AccountExportConfirmationLimiter {
	return &AccountExportConfirmationLimiter{rdb: rdb}
}

func (l *AccountExportConfirmationLimiter) Allowed(ctx context.Context, userID int64, clientIP string) (bool, error) {
	if l == nil || l.rdb == nil || userID <= 0 {
		return false, fmt.Errorf("account export confirmation limiter unavailable")
	}
	count, err := l.rdb.Get(ctx, l.key(userID, clientIP)).Int()
	if err != nil {
		if err == redis.Nil {
			return true, nil
		}
		return false, fmt.Errorf("get account export confirmation attempts: %w", err)
	}
	return count < accountExportConfirmationFailureLimit, nil
}

func (l *AccountExportConfirmationLimiter) RecordFailure(ctx context.Context, userID int64, clientIP string) error {
	if l == nil || l.rdb == nil || userID <= 0 {
		return fmt.Errorf("account export confirmation limiter unavailable")
	}
	key := l.key(userID, clientIP)
	pipe := l.rdb.TxPipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, accountExportConfirmationFailureWindow)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("record account export confirmation failure: %w", err)
	}
	return nil
}

func (l *AccountExportConfirmationLimiter) Reset(ctx context.Context, userID int64, clientIP string) error {
	if l == nil || l.rdb == nil || userID <= 0 {
		return fmt.Errorf("account export confirmation limiter unavailable")
	}
	if err := l.rdb.Del(ctx, l.key(userID, clientIP)).Err(); err != nil {
		return fmt.Errorf("clear account export confirmation failures: %w", err)
	}
	return nil
}

func (l *AccountExportConfirmationLimiter) key(userID int64, clientIP string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(clientIP)))
	return fmt.Sprintf("%s%d:%s", accountExportConfirmationKeyPrefix, userID, hex.EncodeToString(sum[:8]))
}
