//go:build unit || !integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newMiniredisEmailCache(t *testing.T) (service.EmailCache, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewEmailCache(rdb), mr, rdb
}

// seedLegacyCode writes a pre-upgrade payload: no Generation field and no
// separate attempt counter key.
func seedLegacyCode(t *testing.T, rdb *redis.Client, key string, attempts int, ttl time.Duration) {
	t.Helper()
	payload := `{"Code":"123456","Attempts":` + strconv.Itoa(attempts) + `,"CreatedAt":"2026-01-01T00:00:00Z"}`
	require.NoError(t, rdb.Set(context.Background(), key, payload, ttl).Err())
}

func TestEmailCache_ConcurrentWrongCodesCannotExceedAttemptCap(t *testing.T) {
	cache, _, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "user@example.com"

	svc := service.NewEmailService(nil, cache)
	require.NoError(t, cache.SetVerificationCode(ctx, email, &service.VerificationCodeData{
		Code:      "123456",
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(15 * time.Minute),
	}, 15*time.Minute))

	const workers = 50
	var invalid, maxed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.VerifyCode(ctx, email, "000000")
			switch {
			case errors.Is(err, service.ErrInvalidVerifyCode):
				invalid.Add(1)
			case errors.Is(err, service.ErrVerifyCodeMaxAttempts):
				maxed.Add(1)
			default:
				t.Errorf("unexpected result: %v", err)
			}
		}()
	}
	wg.Wait()

	// Only attempts 1..4 may return "invalid"; every other guess is rejected by the cap.
	require.LessOrEqual(t, int(invalid.Load()), 4)
	require.Equal(t, workers, int(invalid.Load()+maxed.Load()))

	// Even the correct code is now rejected.
	require.ErrorIs(t, svc.VerifyCode(ctx, email, "123456"), service.ErrVerifyCodeMaxAttempts)

	data, err := cache.GetVerificationCode(ctx, email)
	require.NoError(t, err)
	require.GreaterOrEqual(t, data.Attempts, service.MaxVerificationCodeAttempts)
}

func TestEmailCache_ConcurrentCorrectCodeIsConsumedOnce(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "winner@example.com"
	svc := service.NewEmailService(nil, cache)

	require.NoError(t, cache.SetVerificationCode(ctx, email, &service.VerificationCodeData{
		Code: "654321", CreatedAt: time.Now(),
	}, 5*time.Minute))

	const workers = 30
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.VerifyCode(ctx, email, "654321") == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int32(1), ok.Load(), "only the single CAS winner may authorize success")
	require.False(t, mr.Exists(verifyCodeKey(email)), "consumed code is deleted")
	require.False(t, mr.Exists(verifyCodeKey(email)+attemptsKeySuffix))
}

func TestEmailCache_LegacyAttemptsCarryOver(t *testing.T) {
	ctx := context.Background()

	t.Run("four legacy attempts admit exactly one comparison", func(t *testing.T) {
		cache, _, rdb := newMiniredisEmailCache(t)
		email := "legacy4@example.com"
		seedLegacyCode(t, rdb, verifyCodeKey(email), 4, 5*time.Minute)
		svc := service.NewEmailService(nil, cache)

		// The single admitted comparison is spent by a wrong candidate and the
		// counter continues from 5, it does not restart at one.
		require.ErrorIs(t, svc.VerifyCode(ctx, email, "000000"), service.ErrVerifyCodeMaxAttempts)
		n, err := rdb.Get(ctx, verifyCodeKey(email)+attemptsKeySuffix).Int()
		require.NoError(t, err)
		require.Equal(t, 5, n)

		// No further comparison is admitted, even for the correct code.
		require.ErrorIs(t, svc.VerifyCode(ctx, email, "123456"), service.ErrVerifyCodeMaxAttempts)
	})

	t.Run("four legacy attempts still authorize the correct code", func(t *testing.T) {
		cache, mr, rdb := newMiniredisEmailCache(t)
		email := "legacy4ok@example.com"
		seedLegacyCode(t, rdb, verifyCodeKey(email), 4, 5*time.Minute)
		svc := service.NewEmailService(nil, cache)

		require.NoError(t, svc.VerifyCode(ctx, email, "123456"))
		require.False(t, mr.Exists(verifyCodeKey(email)))
	})

	t.Run("exhausted legacy attempts compare nothing", func(t *testing.T) {
		cache, _, rdb := newMiniredisEmailCache(t)
		email := "legacy5@example.com"
		seedLegacyCode(t, rdb, verifyCodeKey(email), 5, 5*time.Minute)
		svc := service.NewEmailService(nil, cache)

		require.ErrorIs(t, svc.VerifyCode(ctx, email, "123456"), service.ErrVerifyCodeMaxAttempts)
		// The payload is left intact (no budget, no deletion).
		require.True(t, rdb.Exists(ctx, verifyCodeKey(email)).Val() > 0)
	})
}

func TestEmailCache_ResendIsolatesGenerations(t *testing.T) {
	cache, mr, rdb := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "resend@example.com"

	require.NoError(t, cache.SetVerificationCode(ctx, email, &service.VerificationCodeData{
		Code: "111111", CreatedAt: time.Now(),
	}, 5*time.Minute))
	stale, err := cache.GetVerificationCode(ctx, email)
	require.NoError(t, err)
	require.NotEmpty(t, stale.Generation)

	// Resend installs a new generation and resets the counter.
	require.NoError(t, cache.SetVerificationCode(ctx, email, &service.VerificationCodeData{
		Code: "222222", CreatedAt: time.Now(),
	}, 5*time.Minute))

	// A stale snapshot can neither reserve budget nor consume the replacement.
	_, err = cache.ReserveVerificationCodeAttempt(ctx, email, stale.Generation)
	require.ErrorIs(t, err, service.ErrVerifyCodeMissing)
	consumed, err := cache.ConsumeVerificationCode(ctx, email, stale.Generation)
	require.NoError(t, err)
	require.False(t, consumed)
	require.True(t, mr.Exists(verifyCodeKey(email)))
	require.False(t, mr.Exists(verifyCodeKey(email)+attemptsKeySuffix), "stale reservation must not create the new counter")

	// The new generation keeps its own full budget.
	current, err := cache.GetVerificationCode(ctx, email)
	require.NoError(t, err)
	require.Equal(t, "222222", current.Code)
	require.Equal(t, 0, current.Attempts)
	svc := service.NewEmailService(nil, cache)
	require.NoError(t, svc.VerifyCode(ctx, email, "222222"))

	// A stale wrong guess follows existing invalid-code semantics.
	require.ErrorIs(t, svc.VerifyCode(ctx, email, "111111"), service.ErrInvalidVerifyCode)
	require.False(t, rdb.Exists(ctx, verifyCodeKey(email)).Val() > 0)
}

func TestEmailCache_AttemptsDoNotExtendCodeTTL(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "ttl@example.com"

	require.NoError(t, cache.SetVerificationCode(ctx, email, &service.VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(),
	}, 2*time.Minute))
	codeKey := verifyCodeKey(email)
	before := mr.TTL(codeKey)
	require.Greater(t, before, time.Duration(0))

	data, err := cache.GetVerificationCode(ctx, email)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		_, err := cache.ReserveVerificationCodeAttempt(ctx, email, data.Generation)
		require.NoError(t, err)
	}

	require.Equal(t, before, mr.TTL(codeKey), "reservations must not extend the code lifetime")
	counterTTL := mr.TTL(codeKey + attemptsKeySuffix)
	require.Greater(t, counterTTL, time.Duration(0))
	require.LessOrEqual(t, counterTTL, before)

	// Expiry keeps the code unusable.
	mr.FastForward(3 * time.Minute)
	svc := service.NewEmailService(nil, cache)
	require.ErrorIs(t, svc.VerifyCode(ctx, email, "123456"), service.ErrInvalidVerifyCode)
}

func TestEmailCache_RedisErrorsNeverAuthorizeSuccess(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "redisfail@example.com"

	require.NoError(t, cache.SetVerificationCode(ctx, email, &service.VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(),
	}, 5*time.Minute))
	mr.Close()

	svc := service.NewEmailService(nil, cache)
	require.ErrorIs(t, svc.VerifyCode(ctx, email, "123456"), service.ErrInvalidVerifyCode)
}

func TestEmailCache_MalformedPayloadNeverAuthorizesSuccess(t *testing.T) {
	cache, _, rdb := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "corrupt@example.com"
	require.NoError(t, rdb.Set(ctx, verifyCodeKey(email), "not-json", time.Minute).Err())

	svc := service.NewEmailService(nil, cache)
	require.ErrorIs(t, svc.VerifyCode(ctx, email, "123456"), service.ErrInvalidVerifyCode)

	_, err := cache.ReserveVerificationCodeAttempt(ctx, email, "")
	require.ErrorIs(t, err, service.ErrVerifyCodeMissing)
}

func TestEmailCache_NotifyVerifyUsesSamePrimitive(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "notify@example.com"
	require.NoError(t, cache.SetNotifyVerifyCode(ctx, email, &service.VerificationCodeData{
		Code: "999999", CreatedAt: time.Now(),
	}, 5*time.Minute))

	stale, err := cache.GetNotifyVerifyCode(ctx, email)
	require.NoError(t, err)
	require.NotEmpty(t, stale.Generation)

	// Wrong guesses are capped and never succeed.
	const workers = 20
	var invalid, maxed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cache.ReserveNotifyVerifyCodeAttempt(ctx, email, stale.Generation)
			switch {
			case err == nil:
				invalid.Add(1)
			case errors.Is(err, service.ErrVerifyCodeExhausted):
				maxed.Add(1)
			case errors.Is(err, service.ErrVerifyCodeMissing):
				t.Errorf("unexpected missing generation")
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(service.MaxVerificationCodeAttempts), invalid.Load())
	require.Equal(t, int32(workers-service.MaxVerificationCodeAttempts), maxed.Load())

	// A concurrent correct submission is consumed exactly once.
	require.NoError(t, cache.SetNotifyVerifyCode(ctx, email, &service.VerificationCodeData{
		Code: "888888", CreatedAt: time.Now(),
	}, 5*time.Minute))
	current, err := cache.GetNotifyVerifyCode(ctx, email)
	require.NoError(t, err)

	var ok atomic.Int32
	var wg2 sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			consumed, err := cache.ConsumeNotifyVerifyCode(ctx, email, current.Generation)
			if err == nil && consumed {
				ok.Add(1)
			}
		}()
	}
	wg2.Wait()
	require.Equal(t, int32(1), ok.Load())
	require.False(t, mr.Exists(notifyVerifyKey(email)))
}

func TestEmailCache_PasswordResetTokenHashedAndSingleUse(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "reset@example.com"

	svc := service.NewEmailService(nil, cache)

	// Seed the token the same way SendPasswordResetEmail does (hash only).
	token, err := svc.GeneratePasswordResetToken()
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(token))
	require.NoError(t, cache.SetPasswordResetToken(ctx, email, &service.PasswordResetTokenData{
		Token: hex.EncodeToString(sum[:]), CreatedAt: time.Now(),
	}, 30*time.Minute))

	raw, err := mr.Get(passwordResetKey(email))
	require.NoError(t, err)
	require.False(t, strings.Contains(raw, token), "plaintext token must not be stored")

	require.NoError(t, svc.VerifyPasswordResetToken(ctx, email, token))
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, email, "wrong"), service.ErrInvalidResetToken)

	const workers = 30
	var ok atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.ConsumePasswordResetToken(ctx, email, token) == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), ok.Load())
	require.False(t, mr.Exists(passwordResetKey(email)))
}

func TestEmailCache_LegacyPlaintextResetLinkFailsClosed(t *testing.T) {
	cache, mr, rdb := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "legacy-reset@example.com"
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// Pre-upgrade representation: the raw token was stored verbatim.
	require.NoError(t, rdb.Set(ctx, passwordResetKey(email), `{"Token":"`+token+`"}`, 30*time.Minute).Err())

	svc := service.NewEmailService(nil, cache)
	require.ErrorIs(t, svc.VerifyPasswordResetToken(ctx, email, token), service.ErrInvalidResetToken)
	require.ErrorIs(t, svc.ConsumePasswordResetToken(ctx, email, token), service.ErrInvalidResetToken)
	// Fail-closed must not delete the legacy entry either; the user requests a new link.
	require.True(t, mr.Exists(passwordResetKey(email)))
}

func TestEmailCache_ConsumePasswordResetTokenMismatchKeepsToken(t *testing.T) {
	cache, mr, _ := newMiniredisEmailCache(t)
	ctx := context.Background()
	email := "keep@example.com"
	require.NoError(t, cache.SetPasswordResetToken(ctx, email, &service.PasswordResetTokenData{Token: "abc"}, time.Minute))

	ok, err := cache.ConsumePasswordResetToken(ctx, email, "xyz")
	require.NoError(t, err)
	require.False(t, ok)
	require.True(t, mr.Exists(passwordResetKey(email)))

	ok, err = cache.ConsumePasswordResetToken(ctx, email, "abc")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = cache.ConsumePasswordResetToken(ctx, email, "abc")
	require.NoError(t, err)
	require.False(t, ok)
}
