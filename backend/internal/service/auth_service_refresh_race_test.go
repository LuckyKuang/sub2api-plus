//go:build unit

package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// barrierRefreshTokenCacheStub 让所有并发请求都先读到旧 token，再统一放行，
// 从而确定性地复现 "Get 之后、消费之前" 的 double-spend 窗口。
type barrierRefreshTokenCacheStub struct {
	*emailBindRefreshTokenCacheStub

	target  int
	barrier chan struct{}
	mu      sync.Mutex
	reads   int
}

func newBarrierRefreshTokenCacheStub(target int) *barrierRefreshTokenCacheStub {
	return &barrierRefreshTokenCacheStub{
		emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub(),
		target:                         target,
		barrier:                        make(chan struct{}),
	}
}

func (s *barrierRefreshTokenCacheStub) GetRefreshToken(ctx context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	data, err := s.emailBindRefreshTokenCacheStub.GetRefreshToken(ctx, tokenHash)

	s.mu.Lock()
	s.reads++
	if s.reads == s.target {
		close(s.barrier)
	}
	s.mu.Unlock()
	<-s.barrier
	return data, err
}

// 同一个 refresh token 并发刷新时，只能有一个请求成功兑换，其余必须失败。
func TestAuthServiceRefreshTokenPairConsumesTokenOnce(t *testing.T) {
	ctx := context.Background()
	const workers = 8
	cache := newBarrierRefreshTokenCacheStub(workers)

	user := &service.User{
		ID:           41,
		Email:        "refresh-race@example.com",
		Username:     "refresh-race",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		TokenVersion: 1,
	}
	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:                   "test-refresh-race-secret",
			ExpireHour:               1,
			AccessTokenExpireMinutes: 60,
			RefreshTokenExpireDays:   7,
		},
	}
	svc := service.NewAuthService(nil, newEmailBindUserRepoStub(user), nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)

	pair, err := svc.GenerateTokenPair(ctx, user, "")
	require.NoError(t, err)

	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, refreshErr := svc.RefreshTokenPair(ctx, pair.RefreshToken)
			errs <- refreshErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	successes, invalid := 0, 0
	for refreshErr := range errs {
		switch {
		case refreshErr == nil:
			successes++
		case errors.Is(refreshErr, service.ErrRefreshTokenInvalid):
			invalid++
		default:
			t.Fatalf("unexpected refresh error: %v", refreshErr)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, workers-1, invalid)
}

type consumeFailureCacheStub struct {
	*emailBindRefreshTokenCacheStub
	consumeErr error
}

func (s *consumeFailureCacheStub) ConsumeRefreshToken(context.Context, string) (*service.RefreshTokenData, error) {
	return nil, s.consumeErr
}

// A replay or Redis failure cannot mint a new pair or add a family/user member.
func TestAuthServiceRefreshConsumeFailureHasNoIssuance(t *testing.T) {
	for _, tc := range []struct {
		name              string
		failure, expected error
	}{
		{"replay", service.ErrRefreshTokenNotFound, service.ErrRefreshTokenInvalid},
		{"redis unavailable", errors.New("synthetic redis outage"), service.ErrServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &consumeFailureCacheStub{emailBindRefreshTokenCacheStub: newEmailBindRefreshTokenCacheStub(), consumeErr: tc.failure}
			user := &service.User{ID: 41, Email: "consume@example.com", Role: service.RoleUser, Status: service.StatusActive, TokenVersion: 1}
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "synthetic-consume-secret", ExpireHour: 1, AccessTokenExpireMinutes: 60, RefreshTokenExpireDays: 7}}
			svc := service.NewAuthService(nil, newEmailBindUserRepoStub(user), nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
			original, err := svc.GenerateTokenPair(context.Background(), user, "family-consume")
			require.NoError(t, err)
			pair, err := svc.RefreshTokenPair(context.Background(), original.RefreshToken)
			require.ErrorIs(t, err, tc.expected)
			require.Nil(t, pair)
			require.Len(t, cache.tokens, 1)
			require.Len(t, cache.userSets[user.ID], 1)
			require.Len(t, cache.families["family-consume"], 1)
		})
	}
}
