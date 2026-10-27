package service

import (
	"context"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
)

type rpmAdmissionScopeKey struct{}

type rpmAdmissionCounterKey struct {
	service *BillingCacheService
	userID  int64
	groupID int64
	group   bool
}

type rpmAdmissionCount struct {
	count int
	err   error
}

// rpmAdmissionScope belongs to one ingress request or one WebSocket turn. It
// retains the actual counter result, so later billing checks and cross-group
// fallbacks can enforce their current limits without counting the request twice.
type rpmAdmissionScope struct {
	mu     sync.Mutex
	counts map[rpmAdmissionCounterKey]rpmAdmissionCount
}

// WithRPMAdmissionScope preserves the current request's RPM admission scope.
// Only trusted ingress code installs this state; client headers cannot supply it.
func WithRPMAdmissionScope(ctx context.Context) context.Context {
	if _, ok := ctx.Value(rpmAdmissionScopeKey{}).(*rpmAdmissionScope); ok {
		return ctx
	}
	return WithNewRPMAdmissionScope(ctx)
}

// WithNewRPMAdmissionScope starts an independent admission for a later
// WebSocket turn even though it shares the connection context.
func WithNewRPMAdmissionScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, rpmAdmissionScopeKey{}, &rpmAdmissionScope{})
}

func incrementRequestRPM(ctx context.Context, key rpmAdmissionCounterKey, increment func() (int, error)) (int, error) {
	scope, _ := ctx.Value(rpmAdmissionScopeKey{}).(*rpmAdmissionScope)
	if scope == nil {
		return increment()
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if result, ok := scope.counts[key]; ok {
		return result.count, result.err
	}
	count, err := increment()
	if scope.counts == nil {
		scope.counts = make(map[rpmAdmissionCounterKey]rpmAdmissionCount, 2)
	}
	scope.counts[key] = rpmAdmissionCount{count: count, err: err}
	return count, err
}

func (s *BillingCacheService) incrementRequestUserRPM(ctx context.Context, userID int64) (int, error) {
	return incrementRequestRPM(ctx, rpmAdmissionCounterKey{service: s, userID: userID}, func() (int, error) {
		return s.userRPMCache.IncrementUserRPM(ctx, userID)
	})
}

func (s *BillingCacheService) incrementRequestUserGroupRPM(ctx context.Context, userID, groupID int64) (int, error) {
	return incrementRequestRPM(ctx, rpmAdmissionCounterKey{service: s, userID: userID, groupID: groupID, group: true}, func() (int, error) {
		return s.userRPMCache.IncrementUserGroupRPM(ctx, userID, groupID)
	})
}

// CheckRequestRPM enforces the existing user and user-group RPM policies for an
// accepted ingress request. Repeated checks in the same scope share counter
// results. Simple mode retains its explicit RPM bypass.
func (s *BillingCacheService) CheckRequestRPM(ctx context.Context, user *User, group *Group) error {
	if s == nil || (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) {
		return nil
	}
	return s.checkRPM(ctx, user, group)
}
