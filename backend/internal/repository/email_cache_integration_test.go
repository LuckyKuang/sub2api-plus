//go:build integration

package repository

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type EmailCacheSuite struct {
	IntegrationRedisSuite
	cache service.EmailCache
}

func (s *EmailCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewEmailCache(s.rdb)
}

func (s *EmailCacheSuite) TestGetVerificationCode_Missing() {
	_, err := s.cache.GetVerificationCode(s.ctx, "nonexistent@example.com")
	require.True(s.T(), errors.Is(err, redis.Nil), "expected redis.Nil for missing verification code")
}

func (s *EmailCacheSuite) TestSetAndGetVerificationCode() {
	email := "a@example.com"
	emailTTL := 2 * time.Minute
	data := &service.VerificationCodeData{Code: "123456", Attempts: 1, CreatedAt: time.Now()}

	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, data, emailTTL), "SetVerificationCode")

	got, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err, "GetVerificationCode")
	require.Equal(s.T(), "123456", got.Code)
	require.Equal(s.T(), 1, got.Attempts)
	require.NotEmpty(s.T(), got.Generation, "set must install a generation")
}

func (s *EmailCacheSuite) TestVerificationCode_TTL() {
	email := "ttl@example.com"
	emailTTL := 2 * time.Minute
	data := &service.VerificationCodeData{Code: "654321", Attempts: 0, CreatedAt: time.Now()}

	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, data, emailTTL), "SetVerificationCode")

	emailKey := verifyCodeKeyPrefix + email
	ttl, err := s.rdb.TTL(s.ctx, emailKey).Result()
	require.NoError(s.T(), err, "TTL emailKey")
	s.AssertTTLWithin(ttl, 1*time.Second, emailTTL)
}

func (s *EmailCacheSuite) TestReserveDoesNotExtendTTL() {
	email := "ttl-reserve@example.com"
	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, &service.VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(),
	}, 2*time.Minute))

	snapshot, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err)
	before, err := s.rdb.TTL(s.ctx, verifyCodeKeyPrefix+email).Result()
	require.NoError(s.T(), err)

	attempts, err := s.cache.ReserveVerificationCodeAttempt(s.ctx, email, snapshot.Generation)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 1, attempts)

	after, err := s.rdb.TTL(s.ctx, verifyCodeKeyPrefix+email).Result()
	require.NoError(s.T(), err)
	require.LessOrEqual(s.T(), after, before+time.Millisecond, "reservation must not extend the code lifetime")

	counterTTL, err := s.rdb.TTL(s.ctx, verifyCodeKeyPrefix+email+attemptsKeySuffix).Result()
	require.NoError(s.T(), err)
	require.Greater(s.T(), counterTTL, time.Duration(0))
	require.LessOrEqual(s.T(), counterTTL, after+time.Second)
}

func (s *EmailCacheSuite) TestConsumeIsGenerationBoundAndSingleUse() {
	email := "consume@example.com"
	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, &service.VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(),
	}, 2*time.Minute))

	stale, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err)

	// A concurrent resend replaces the generation.
	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, &service.VerificationCodeData{
		Code: "654321", CreatedAt: time.Now(),
	}, 2*time.Minute))

	_, err = s.cache.ReserveVerificationCodeAttempt(s.ctx, email, stale.Generation)
	require.ErrorIs(s.T(), err, service.ErrVerifyCodeMissing)
	consumed, err := s.cache.ConsumeVerificationCode(s.ctx, email, stale.Generation)
	require.NoError(s.T(), err)
	require.False(s.T(), consumed, "stale generation must not consume the replacement")
	require.True(s.T(), s.rdb.Exists(s.ctx, verifyCodeKeyPrefix+email).Val() > 0)

	current, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err)
	require.NotEqual(s.T(), stale.Generation, current.Generation)
	consumed, err = s.cache.ConsumeVerificationCode(s.ctx, email, current.Generation)
	require.NoError(s.T(), err)
	require.True(s.T(), consumed)
	require.Equal(s.T(), int64(0), s.rdb.Exists(s.ctx, verifyCodeKeyPrefix+email).Val())
}

func (s *EmailCacheSuite) TestLegacyAttemptsCarryOver() {
	email := "legacy@example.com"
	require.NoError(s.T(), s.rdb.Set(s.ctx, verifyCodeKeyPrefix+email,
		`{"Code":"123456","Attempts":4,"CreatedAt":"2026-01-01T00:00:00Z"}`, 2*time.Minute).Err())

	snapshot, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 4, snapshot.Attempts)

	attempts, err := s.cache.ReserveVerificationCodeAttempt(s.ctx, email, snapshot.Generation)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 5, attempts)

	_, err = s.cache.ReserveVerificationCodeAttempt(s.ctx, email, snapshot.Generation)
	require.ErrorIs(s.T(), err, service.ErrVerifyCodeExhausted)
}

func (s *EmailCacheSuite) TestConcurrentReservationHonorsCap() {
	email := "concurrent@example.com"
	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, &service.VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(),
	}, 2*time.Minute))
	snapshot, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err)

	const workers = 50
	var reserved, exhausted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.cache.ReserveVerificationCodeAttempt(s.ctx, email, snapshot.Generation)
			switch {
			case err == nil:
				reserved.Add(1)
			case errors.Is(err, service.ErrVerifyCodeExhausted):
				exhausted.Add(1)
			default:
				s.T().Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	require.Equal(s.T(), int32(service.MaxVerificationCodeAttempts), reserved.Load())
	require.Equal(s.T(), int32(workers-service.MaxVerificationCodeAttempts), exhausted.Load())
}

func (s *EmailCacheSuite) TestConcurrentConsumeWinsOnce() {
	email := "winner@example.com"
	require.NoError(s.T(), s.cache.SetVerificationCode(s.ctx, email, &service.VerificationCodeData{
		Code: "123456", CreatedAt: time.Now(),
	}, 2*time.Minute))
	snapshot, err := s.cache.GetVerificationCode(s.ctx, email)
	require.NoError(s.T(), err)

	const workers = 30
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumed, err := s.cache.ConsumeVerificationCode(s.ctx, email, snapshot.Generation)
			if err == nil && consumed {
				won.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(s.T(), int32(1), won.Load())
}

func (s *EmailCacheSuite) TestGetVerificationCode_JSONCorruption() {
	emailKey := verifyCodeKeyPrefix + "corrupted@example.com"

	require.NoError(s.T(), s.rdb.Set(s.ctx, emailKey, "not-json", 1*time.Minute).Err(), "Set invalid JSON")

	_, err := s.cache.GetVerificationCode(s.ctx, "corrupted@example.com")
	require.Error(s.T(), err, "expected error for corrupted JSON")
	require.False(s.T(), errors.Is(err, redis.Nil), "expected decoding error, not redis.Nil")

	_, err = s.cache.ReserveVerificationCodeAttempt(s.ctx, "corrupted@example.com", "")
	require.ErrorIs(s.T(), err, service.ErrVerifyCodeMissing)
}

func TestEmailCacheSuite(t *testing.T) {
	suite.Run(t, new(EmailCacheSuite))
}
