package repository

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	verifyCodeKeyPrefix          = "verify_code:"
	notifyVerifyKeyPrefix        = "notify_verify:"
	passwordResetKeyPrefix       = "password_reset:"
	passwordResetSentAtKeyPrefix = "password_reset_sent:"
	notifyCodeUserRateKeyPrefix  = "notify_code_user_rate:"

	// attemptsKeySuffix stores the admitted-attempt counter for the *current*
	// generation of a verification code. Kept in a separate key so reservation can
	// be atomic; it is reset (deleted) whenever a new generation is installed.
	attemptsKeySuffix = ":attempts"
)

// reserveCodeAttemptScript atomically reserves one attempt against the current
// code generation.
//
// KEYS[1] = code key, KEYS[2] = attempts key.
// ARGV[1] = expected generation, ARGV[2] = attempt cap.
//
// Returns the reserved attempt count (> 0) on success, -1 when the code is
// missing, undecodable or a different generation, and -2 when the budget is spent.
//
// The counter is derived from max(stored JSON Attempts, counter) so a legacy
// payload written with Attempts=4 admits exactly one further comparison instead of
// restarting at one. The counter TTL always follows the code's remaining TTL, so
// reservations never extend the code's validity.
var reserveCodeAttemptScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then
  return -1
end
local decoded, d = pcall(cjson.decode, raw)
if not decoded or type(d) ~= 'table' then
  return -1
end
local generation = ''
if d['Generation'] ~= nil then
  generation = tostring(d['Generation'])
end
if generation ~= ARGV[1] then
  return -1
end
local used = tonumber(d['Attempts']) or 0
local counter = tonumber(redis.call('GET', KEYS[2])) or 0
if counter > used then
  used = counter
end
local maxAttempts = tonumber(ARGV[2])
if used >= maxAttempts then
  return -2
end
local nextAttempts = used + 1
redis.call('SET', KEYS[2], nextAttempts)
local ttl = redis.call('PTTL', KEYS[1])
if ttl > 0 then
  redis.call('PEXPIRE', KEYS[2], ttl)
end
return nextAttempts
`)

// consumeCodeScript atomically deletes the code and its attempt counter only when
// the stored generation still matches.
//
// KEYS[1] = code key, KEYS[2] = attempts key, ARGV[1] = expected generation.
// Returns 1 for the single winning consumer, 0 otherwise.
var consumeCodeScript = redis.NewScript(`
local raw = redis.call('GET', KEYS[1])
if not raw then
  return 0
end
local decoded, d = pcall(cjson.decode, raw)
if not decoded or type(d) ~= 'table' then
  return 0
end
local generation = ''
if d['Generation'] ~= nil then
  generation = tostring(d['Generation'])
end
if generation ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1], KEYS[2])
return 1
`)

// consumeResetTokenScript atomically compares the stored token hash and deletes it.
// KEYS[1] = reset key, ARGV[1] = expected token hash. Returns 1 on success, 0 otherwise.
var consumeResetTokenScript = redis.NewScript(`
local v = redis.call('GET', KEYS[1])
if not v then
  return 0
end
local ok, d = pcall(cjson.decode, v)
if not ok or type(d) ~= 'table' or d['Token'] ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

// verifyCodeKey generates the Redis key for email verification code.
// Email is lowercased for case-insensitive consistency.
func verifyCodeKey(email string) string {
	return verifyCodeKeyPrefix + strings.ToLower(email)
}

// notifyVerifyKey generates the Redis key for notify email verification code.
// Email is lowercased to prevent case-sensitive key mismatch (the business layer
// uses strings.EqualFold for comparison).
func notifyVerifyKey(email string) string {
	return notifyVerifyKeyPrefix + strings.ToLower(email)
}

// passwordResetKey generates the Redis key for password reset token.
func passwordResetKey(email string) string {
	return passwordResetKeyPrefix + strings.ToLower(email)
}

// passwordResetSentAtKey generates the Redis key for password reset email sent timestamp.
func passwordResetSentAtKey(email string) string {
	return passwordResetSentAtKeyPrefix + strings.ToLower(email)
}

type emailCache struct {
	rdb *redis.Client
}

func NewEmailCache(rdb *redis.Client) service.EmailCache {
	return &emailCache{rdb: rdb}
}

// newVerificationCodeGeneration returns an opaque random handle identifying one
// code issuance. It carries no information about the code itself.
func newVerificationCodeGeneration() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate verification code generation: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func (c *emailCache) getCode(ctx context.Context, key string) (*service.VerificationCodeData, error) {
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var data service.VerificationCodeData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	if n, err := c.rdb.Get(ctx, key+attemptsKeySuffix).Int(); err == nil && n > data.Attempts {
		data.Attempts = n
	}
	return &data, nil
}

// setCode atomically installs a new generation, the payload and the initial
// attempt counter. Rotating the generation here guarantees that a caller holding
// an old snapshot can neither reserve nor consume this replacement.
func (c *emailCache) setCode(ctx context.Context, key string, data *service.VerificationCodeData, ttl time.Duration) error {
	if data == nil {
		return fmt.Errorf("verification code data is nil")
	}
	generation, err := newVerificationCodeGeneration()
	if err != nil {
		return err
	}
	data.Generation = generation
	val, err := json.Marshal(data)
	if err != nil {
		return err
	}
	pipe := c.rdb.TxPipeline()
	pipe.Set(ctx, key, val, ttl)
	pipe.Del(ctx, key+attemptsKeySuffix)
	if data.Attempts > 0 {
		pipe.Set(ctx, key+attemptsKeySuffix, data.Attempts, ttl)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (c *emailCache) reserveCodeAttempt(ctx context.Context, key, generation string) (int, error) {
	n, err := reserveCodeAttemptScript.Run(ctx, c.rdb,
		[]string{key, key + attemptsKeySuffix},
		generation, service.MaxVerificationCodeAttempts).Int()
	if err != nil {
		return 0, err
	}
	switch {
	case n == -2:
		return 0, service.ErrVerifyCodeExhausted
	case n < 0:
		return 0, service.ErrVerifyCodeMissing
	default:
		return n, nil
	}
}

func (c *emailCache) consumeCode(ctx context.Context, key, generation string) (bool, error) {
	n, err := consumeCodeScript.Run(ctx, c.rdb,
		[]string{key, key + attemptsKeySuffix}, generation).Int()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (c *emailCache) GetVerificationCode(ctx context.Context, email string) (*service.VerificationCodeData, error) {
	return c.getCode(ctx, verifyCodeKey(email))
}

func (c *emailCache) SetVerificationCode(ctx context.Context, email string, data *service.VerificationCodeData, ttl time.Duration) error {
	return c.setCode(ctx, verifyCodeKey(email), data, ttl)
}

func (c *emailCache) ReserveVerificationCodeAttempt(ctx context.Context, email, generation string) (int, error) {
	return c.reserveCodeAttempt(ctx, verifyCodeKey(email), generation)
}

func (c *emailCache) ConsumeVerificationCode(ctx context.Context, email, generation string) (bool, error) {
	return c.consumeCode(ctx, verifyCodeKey(email), generation)
}

// Password reset token methods

func (c *emailCache) GetPasswordResetToken(ctx context.Context, email string) (*service.PasswordResetTokenData, error) {
	key := passwordResetKey(email)
	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	var data service.PasswordResetTokenData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *emailCache) SetPasswordResetToken(ctx context.Context, email string, data *service.PasswordResetTokenData, ttl time.Duration) error {
	key := passwordResetKey(email)
	val, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, key, val, ttl).Err()
}

// ConsumePasswordResetToken atomically deletes the stored reset token when its
// stored hash equals tokenHash. Returns true only for the single winning caller.
func (c *emailCache) ConsumePasswordResetToken(ctx context.Context, email, tokenHash string) (bool, error) {
	n, err := consumeResetTokenScript.Run(ctx, c.rdb, []string{passwordResetKey(email)}, tokenHash).Int()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

func (c *emailCache) DeletePasswordResetToken(ctx context.Context, email string) error {
	key := passwordResetKey(email)
	return c.rdb.Del(ctx, key).Err()
}

// Password reset email cooldown methods

func (c *emailCache) IsPasswordResetEmailInCooldown(ctx context.Context, email string) bool {
	key := passwordResetSentAtKey(email)
	exists, err := c.rdb.Exists(ctx, key).Result()
	return err == nil && exists > 0
}

func (c *emailCache) SetPasswordResetEmailCooldown(ctx context.Context, email string, ttl time.Duration) error {
	key := passwordResetSentAtKey(email)
	return c.rdb.Set(ctx, key, "1", ttl).Err()
}

// Notify email verification code methods

func (c *emailCache) GetNotifyVerifyCode(ctx context.Context, email string) (*service.VerificationCodeData, error) {
	return c.getCode(ctx, notifyVerifyKey(email))
}

func (c *emailCache) SetNotifyVerifyCode(ctx context.Context, email string, data *service.VerificationCodeData, ttl time.Duration) error {
	return c.setCode(ctx, notifyVerifyKey(email), data, ttl)
}

func (c *emailCache) ReserveNotifyVerifyCodeAttempt(ctx context.Context, email, generation string) (int, error) {
	return c.reserveCodeAttempt(ctx, notifyVerifyKey(email), generation)
}

func (c *emailCache) ConsumeNotifyVerifyCode(ctx context.Context, email, generation string) (bool, error) {
	return c.consumeCode(ctx, notifyVerifyKey(email), generation)
}

// User-level rate limiting for notify email verification codes

func notifyCodeUserRateKey(userID int64) string {
	return notifyCodeUserRateKeyPrefix + fmt.Sprintf("%d", userID)
}

func (c *emailCache) IncrNotifyCodeUserRate(ctx context.Context, userID int64, window time.Duration) (int64, error) {
	key := notifyCodeUserRateKey(userID)
	count, err := c.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// Always set TTL (idempotent) to avoid orphan keys if process crashes between INCR and EXPIRE.
	if err := c.rdb.Expire(ctx, key, window).Err(); err != nil {
		return count, fmt.Errorf("expire notify code rate key: %w", err)
	}
	return count, nil
}

func (c *emailCache) GetNotifyCodeUserRate(ctx context.Context, userID int64) (int64, error) {
	key := notifyCodeUserRateKey(userID)
	count, err := c.rdb.Get(ctx, key).Int64()
	if err != nil {
		return 0, err
	}
	return count, nil
}
