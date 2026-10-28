//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type UserRPMAdmissionSuite struct {
	IntegrationRedisSuite
	peers  []*service.BillingCacheService
	caches []service.UserRPMCache
}

func TestUserRPMAdmissionSuite(t *testing.T) { suite.Run(t, new(UserRPMAdmissionSuite)) }

func (s *UserRPMAdmissionSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.peers = nil
	s.caches = nil
	// Independent Redis connections model separate gateway instances. Both use
	// one test-only namespace so increments must contend on the same real keys.
	prefix := fmt.Sprintf("it:%s:%d:shared-rpm:", sanitizeRedisNamespace(s.T().Name()), time.Now().UnixNano())
	s.T().Cleanup(func() {
		var cursor uint64
		for {
			keys, next, err := integrationRedis.Scan(context.Background(), cursor, prefix+"*", 100).Result()
			require.NoError(s.T(), err)
			if len(keys) > 0 {
				require.NoError(s.T(), integrationRedis.Unlink(context.Background(), keys...).Err())
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	})
	for i := 0; i < 2; i++ {
		opts := *integrationRedis.Options()
		client := redis.NewClient(&opts)
		client.AddHook(prefixHook{prefix: prefix})
		s.T().Cleanup(func() { _ = client.Close() })
		cache := NewUserRPMCache(client)
		svc := service.NewBillingCacheService(nil, nil, nil, nil, cache, nil, &config.Config{}, nil)
		s.T().Cleanup(svc.Stop)
		s.caches = append(s.caches, cache)
		s.peers = append(s.peers, svc)
	}
}

// Fixed-minute limits intentionally reset at Redis minute boundaries. Start
// close to a boundary only after it advances, rather than making the concurrent
// acceptance assertions intermittently span two valid quota windows.
func (s *UserRPMAdmissionSuite) startMinute() int64 {
	now, err := s.rdb.Time(s.ctx).Result()
	require.NoError(s.T(), err)
	if remaining := time.Duration(60-now.Second())*time.Second - time.Duration(now.Nanosecond()); remaining < 5*time.Second {
		time.Sleep(remaining + 20*time.Millisecond)
		now, err = s.rdb.Time(s.ctx).Result()
		require.NoError(s.T(), err)
	}
	return now.Unix() / 60
}

func (s *UserRPMAdmissionSuite) requireSameMinute(minute int64) {
	now, err := s.rdb.Time(s.ctx).Result()
	require.NoError(s.T(), err)
	require.Equal(s.T(), minute, now.Unix()/60, "the scenario must run within one Redis quota minute")
}

func (s *UserRPMAdmissionSuite) TestGlobalFiveRPMSharedAcrossKeysAndInstances() {
	minute := s.startMinute()
	user := &service.User{ID: 721, RPMLimit: 5}
	const requests = 20
	results := make(chan error, requests)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		key := &service.APIKey{ID: int64(i + 1), UserID: user.ID, User: user}
		peer := s.peers[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- peer.CheckRequestRPM(service.WithNewRPMAdmissionScope(s.ctx), key.User, nil)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(s.T(), err, service.ErrUserRPMExceeded)
			rejected++
		}
	}
	s.requireSameMinute(minute)
	require.Equal(s.T(), 5, accepted, "creating more API keys or changing instances must not enlarge the user limit")
	require.Equal(s.T(), 15, rejected)
	for _, cache := range s.caches {
		used, err := cache.GetUserRPM(s.ctx, user.ID)
		require.NoError(s.T(), err)
		require.Equal(s.T(), 20, used, "both independent clients must observe the same authoritative counter")
	}
}

func (s *UserRPMAdmissionSuite) TestUserGroupRPMIsolatesBothUserAndGroup() {
	minute := s.startMinute()
	first, second := &service.User{ID: 722}, &service.User{ID: 723}
	groupA, groupB := &service.Group{ID: 801, RPMLimit: 2}, &service.Group{ID: 802, RPMLimit: 2}
	for _, target := range []struct {
		user  *service.User
		group *service.Group
	}{{first, groupA}, {first, groupB}, {second, groupA}} {
		for i := 0; i < 2; i++ {
			require.NoError(s.T(), s.peers[i].CheckRequestRPM(service.WithNewRPMAdmissionScope(s.ctx), target.user, target.group))
		}
		require.ErrorIs(s.T(), s.peers[1].CheckRequestRPM(service.WithNewRPMAdmissionScope(s.ctx), target.user, target.group), service.ErrGroupRPMExceeded)
		used, err := s.caches[0].GetUserGroupRPM(s.ctx, target.user.ID, target.group.ID)
		require.NoError(s.T(), err)
		require.Equal(s.T(), 3, used)
	}
	s.requireSameMinute(minute)
	used, err := s.caches[0].GetUserRPM(s.ctx, first.ID)
	require.NoError(s.T(), err)
	require.Zero(s.T(), used, "group limits alone must not invent a user-level cap")
}

func (s *UserRPMAdmissionSuite) TestUserHardCapAndGroupCapBothRemainEffective() {
	minute := s.startMinute()
	user := &service.User{ID: 724, RPMLimit: 5}
	groupA, groupB := &service.Group{ID: 803, RPMLimit: 2}, &service.Group{ID: 804, RPMLimit: 100}
	for i := 0; i < 2; i++ {
		require.NoError(s.T(), s.peers[i].CheckRequestRPM(service.WithNewRPMAdmissionScope(s.ctx), user, groupA))
	}
	const requests = 8
	results := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		peer := s.peers[i%2]
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- peer.CheckRequestRPM(service.WithNewRPMAdmissionScope(s.ctx), user, groupB)
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(s.T(), err, service.ErrUserRPMExceeded)
		}
	}
	s.requireSameMinute(minute)
	require.Equal(s.T(), 3, accepted, "a larger group cap must not bypass the user's remaining three requests")
	used, err := s.caches[0].GetUserRPM(s.ctx, user.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 10, used, "the two group-A requests and eight group-B attempts must share one global counter")
	for _, target := range []struct {
		groupID int64
		count   int
	}{{groupA.ID, 2}, {groupB.ID, 8}} {
		used, err = s.caches[1].GetUserGroupRPM(s.ctx, user.ID, target.groupID)
		require.NoError(s.T(), err)
		require.Equal(s.T(), target.count, used)
	}
}

func (s *UserRPMAdmissionSuite) TestFallbackScopeIncrementsUserOnceAndEachGroupOnce() {
	minute := s.startMinute()
	user := &service.User{ID: 725, RPMLimit: 1}
	primary, fallback := &service.Group{ID: 805, RPMLimit: 1}, &service.Group{ID: 806, RPMLimit: 1}
	ctx := service.WithRPMAdmissionScope(s.ctx)
	for i := 0; i < 4; i++ {
		require.NoError(s.T(), s.peers[0].CheckRequestRPM(ctx, user, primary))
		require.NoError(s.T(), s.peers[0].CheckRequestRPM(ctx, user, fallback))
	}
	used, err := s.caches[1].GetUserRPM(s.ctx, user.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 1, used, "routing fallback and repeated billing checks belong to one external request")
	for _, group := range []*service.Group{primary, fallback} {
		used, err = s.caches[1].GetUserGroupRPM(s.ctx, user.ID, group.ID)
		require.NoError(s.T(), err)
		require.Equal(s.T(), 1, used)
	}
	require.ErrorIs(s.T(), s.peers[1].CheckRequestRPM(service.WithNewRPMAdmissionScope(ctx), user, nil), service.ErrUserRPMExceeded,
		"a new request or WebSocket turn must not inherit the previous request's exemption")
	used, err = s.caches[0].GetUserRPM(s.ctx, user.ID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), 2, used)
	s.requireSameMinute(minute)
}
