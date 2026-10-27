package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
)

// ConcurrencyLeaseCache extends the ordinary request pool with owner-preserving
// renewal. The production Redis cache implements it; renewal cannot recreate a
// missing/expired owner. The original cache interface remains usable by tests.
type ConcurrencyLeaseCache interface {
	ConcurrencySlotTTL() time.Duration
	RefreshUserSlot(context.Context, int64, string) (bool, error)
	RefreshAccountSlot(context.Context, int64, string) (bool, error)
}

// APIKeyConcurrencyLeaseCache keeps the statistics for long-running requests
// visible without turning a statistics failure into an admission failure.
// Refresh may renew only an existing owner, never recreate a missing one.
type APIKeyConcurrencyLeaseCache interface {
	ConcurrencySlotTTL() time.Duration
	RefreshAPIKeySlot(context.Context, int64, string) (bool, error)
}

type concurrencyExecutionKey struct{}
type workerUserLeaseKey struct{}

// Only a worker that owns an actual user lease may delegate admission to its
// synchronous gateway call. This marker is never derived from request headers.
type workerUserLease struct {
	service   *ConcurrencyService
	userID    int64
	execution *concurrencyExecution
	active    atomic.Bool
	statsKey  atomic.Int64
}
type concurrencyExecution struct {
	cancel       context.CancelCauseFunc
	capacity     context.Context
	stopCapacity context.CancelCauseFunc
	parent       context.Context
	mu           sync.Mutex
	slots        int
}

// capacityExecutionContext retains request metadata while deriving cancellation
// from capacity ownership. WithoutCancel masks the request's cancelCtx value,
// so context.Cause resolves the capacity owner's cause instead of disconnect.
type capacityExecutionContext struct {
	context.Context
	values context.Context
}

func (c capacityExecutionContext) Value(key any) any {
	if value := c.values.Value(key); value != nil {
		return value
	}
	return c.Context.Value(key)
}

// ConcurrencyCapacityContext is for work that deliberately drains upstream
// usage after a downstream disconnect. It remains cancellable on lost capacity,
// retaining all accepted request metadata. Without an admitted domain, the
// caller's existing cancellation behavior remains unchanged.
func ConcurrencyCapacityContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	if execution, ok := ctx.Value(concurrencyExecutionKey{}).(*concurrencyExecution); ok && execution != nil {
		return capacityExecutionContext{Context: execution.capacity, values: context.WithoutCancel(ctx)}
	}
	return ctx
}

func (e *concurrencyExecution) retainSlot() { e.mu.Lock(); e.slots++; e.mu.Unlock() }
func (e *concurrencyExecution) releaseSlot() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.slots--
	if e.slots == 0 && e.parent.Err() != nil {
		e.stopCapacity(context.Cause(e.parent))
	}
}

var ErrConcurrencyLeaseLost = errors.New("concurrency execution lease lost")

// WithConcurrencyLeaseContext creates the trusted cancellation domain shared by
// a request's user and account slots. Callers with no Gin request must retain
// this context for execution, and cancel it when the whole operation finishes.
// A normal account release during failover does not cancel that domain.
func WithConcurrencyLeaseContext(ctx context.Context) (context.Context, context.CancelCauseFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if execution, ok := ctx.Value(concurrencyExecutionKey{}).(*concurrencyExecution); ok && execution != nil {
		return ctx, execution.cancel
	}
	executionCtx, cancelExecution := context.WithCancelCause(ctx)
	capacityCtx, cancelCapacity := context.WithCancelCause(context.WithoutCancel(ctx))
	execution := &concurrencyExecution{capacity: capacityCtx, stopCapacity: cancelCapacity, parent: ctx}
	execution.cancel = func(cause error) { cancelExecution(cause); cancelCapacity(cause) }
	if ctx.Done() != nil {
		context.AfterFunc(ctx, func() {
			execution.mu.Lock()
			defer execution.mu.Unlock()
			if execution.slots == 0 {
				cancelCapacity(context.Cause(ctx))
			}
		})
	}
	return context.WithValue(executionCtx, concurrencyExecutionKey{}, execution), execution.cancel
}

// WithoutConcurrencyLeaseContext detaches a durable worker from the submitting
// request's cancellation and capacity domain while retaining tracing/pricing
// values. WithConcurrencyLeaseContext must then create a fresh worker domain.
// context.WithoutCancel alone retains values and would reuse the old cancel
// function, allowing a later worker lease loss to cancel the finished ingress.
func WithoutConcurrencyLeaseContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(context.WithoutCancel(ctx), concurrencyExecutionKey{}, (*concurrencyExecution)(nil))
	return context.WithValue(ctx, workerUserLeaseKey{}, (*workerUserLease)(nil))
}

// AcquireWorkerUserSlot holds user capacity for an entire background operation,
// including result offloading. The nested gateway call shares this one lease;
// another user, request, or detached worker must acquire separate capacity.
func (s *ConcurrencyService) AcquireWorkerUserSlot(ctx context.Context, userID int64, limit int) (*AcquireResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, workerUserLeaseKey{}, (*workerUserLease)(nil))
	result, err := s.acquireRequestSlot(ctx, "user", userID, limit)
	if err != nil || !result.Acquired {
		return result, err
	}
	execution, ok := result.Context.Value(concurrencyExecutionKey{}).(*concurrencyExecution)
	if !ok || execution == nil {
		result.ReleaseFunc()
		return nil, ErrConcurrencyLeaseLost
	}
	owner := &workerUserLease{service: s, userID: userID, execution: execution}
	owner.active.Store(true)
	result.Context = context.WithValue(result.Context, workerUserLeaseKey{}, owner)
	release := result.ReleaseFunc
	result.ReleaseFunc = func() {
		if owner.active.CompareAndSwap(true, false) {
			release()
		}
	}
	return result, nil
}

// SetUserRepository injects the authoritative per-admission user configuration.
// Production wiring always supplies it. Request/auth-cache limits are retained
// as the fallback only for isolated service fixtures without a repository.
func (s *ConcurrencyService) SetUserRepository(repo UserRepository) {
	if s != nil {
		s.userRepo = repo
	}
}

func (s *ConcurrencyService) ResolveUserConcurrency(ctx context.Context, userID int64, fallback int) (int, error) {
	if s == nil {
		return 0, errors.New("concurrency service unavailable")
	}
	if s.userRepo == nil {
		return fallback, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, regularLeaseOperationTimeout)
	defer cancel()
	user, err := s.userRepo.GetByID(readCtx, userID)
	if err != nil {
		return 0, fmt.Errorf("current user concurrency unavailable: %w", err)
	}
	if user == nil || user.ID != userID {
		return 0, errors.New("current user concurrency unavailable")
	}
	return user.Concurrency, nil
}

const (
	regularLeaseOperationTimeout = 2 * time.Second
	// WorkerUserConcurrencyWaitTimeout bounds the private handoff queue of a
	// durable image worker. External requests never enter this user queue.
	WorkerUserConcurrencyWaitTimeout = 30 * time.Second
)

func (s *ConcurrencyService) acquireRequestSlot(ctx context.Context, scope string, id int64, limit int) (*AcquireResult, error) {
	executionCtx, cancelExecution := WithConcurrencyLeaseContext(ctx)
	if err := executionCtx.Err(); err != nil {
		return nil, err
	}
	execution, ok := executionCtx.Value(concurrencyExecutionKey{}).(*concurrencyExecution)
	if !ok || execution == nil {
		cancelExecution(ErrConcurrencyLeaseLost)
		return nil, ErrConcurrencyLeaseLost
	}
	if scope == "user" {
		if owner, ok := executionCtx.Value(workerUserLeaseKey{}).(*workerUserLease); ok && owner != nil && owner.service == s && owner.userID == id && owner.execution == executionCtx.Value(concurrencyExecutionKey{}) {
			if !owner.active.Load() {
				return nil, ErrConcurrencyLeaseLost
			}
			return &AcquireResult{Acquired: true, Context: executionCtx, ReleaseFunc: func() {}}, nil
		}
	}
	if scope == "user" {
		current, err := s.ResolveUserConcurrency(executionCtx, id, limit)
		if err != nil {
			return nil, err
		}
		limit = current
	}
	// Unlimited users still occupy a renewable record; lowering their cap
	// must see work accepted before the edit. Unlimited accounts retain their
	// historical no-op behavior.
	if scope == "account" && limit <= 0 {
		return &AcquireResult{Acquired: true, Context: executionCtx, ReleaseFunc: func() {}}, nil
	}
	if s == nil || s.cache == nil {
		return nil, errors.New("concurrency cache unavailable")
	}
	if err := s.recoverConcurrencyOwners(executionCtx, scope, id); err != nil {
		return nil, err
	}
	requestID := generateRequestID()
	owner := concurrencyOwner{scope: scope, id: id, requestID: requestID}
	if err := s.reserveConcurrencyOwner(owner); err != nil {
		return nil, err
	}
	acquireCtx, acquireCancel := context.WithTimeout(executionCtx, regularLeaseOperationTimeout)
	var acquired bool
	var err error
	if scope == "user" {
		acquired, err = s.cache.AcquireUserSlot(acquireCtx, id, limit, requestID)
	} else {
		acquired, err = s.cache.AcquireAccountSlot(acquireCtx, id, limit, requestID)
	}
	acquireCancel()
	if err != nil {
		// The Lua command may have committed before its reply was lost. No
		// execution was accepted, so reclaim exactly this request's owner.
		s.reclaimConcurrencyOwner(owner)
		return nil, err
	}
	result := &AcquireResult{Acquired: acquired, Context: executionCtx}
	if !acquired {
		s.forgetConcurrencyOwner(owner)
		return result, nil
	}
	execution.retainSlot()
	var stopOnce sync.Once
	stop := make(chan struct{})
	done := make(chan struct{})
	renewer, renewable := s.cache.(ConcurrencyLeaseCache)
	var ttl time.Duration
	if renewable {
		ttl = renewer.ConcurrencySlotTTL()
		if ttl <= 0 {
			cancelExecution(ErrConcurrencyLeaseLost)
			close(done)
		} else {
			go s.refreshRequestLease(ConcurrencyCapacityContext(executionCtx), cancelExecution, renewer, scope, id, requestID, ttl, stop, done)
		}
	} else {
		close(done)
	}
	result.ReleaseFunc = func() {
		stopOnce.Do(func() {
			close(stop)
			<-done
			defer execution.releaseSlot()
			s.reclaimConcurrencyOwner(owner)
		})
	}
	if renewable && ttl <= 0 {
		result.ReleaseFunc()
		return nil, ErrConcurrencyLeaseLost
	}
	return result, nil
}

func (s *ConcurrencyService) refreshRequestLease(ctx context.Context, cancel context.CancelCauseFunc, cache ConcurrencyLeaseCache, scope string, id int64, requestID string, ttl time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	interval := ttl / 3
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	timeout := regularLeaseOperationTimeout
	if timeout > ttl/6 {
		timeout = ttl / 6
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, refreshCancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
			var ok bool
			var err error
			if scope == "user" {
				ok, err = cache.RefreshUserSlot(refreshCtx, id, requestID)
			} else {
				ok, err = cache.RefreshAccountSlot(refreshCtx, id, requestID)
			}
			refreshCancel()
			if err != nil || !ok {
				logger.L().Warn("concurrency_slot_lease_lost", zap.String("scope", scope), zap.Int64("owner_id", id), zap.Error(err))
				cancel(ErrConcurrencyLeaseLost)
				return
			}
		}
	}
}

func refreshAPIKeyStatsLease(ctx context.Context, cache APIKeyConcurrencyLeaseCache, apiKeyID int64, requestID string, ttl time.Duration, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	interval := ttl / 3
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	if interval <= 0 {
		return
	}
	timeout := apiKeySlotTrackTimeout
	if timeout > ttl/6 {
		timeout = ttl / 6
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, timeout)
			ok, err := cache.RefreshAPIKeySlot(refreshCtx, apiKeyID, requestID)
			cancel()
			if err != nil || !ok {
				logger.L().Warn("api_key_concurrency_stats_lease_lost", zap.Int64("api_key_id", apiKeyID), zap.Error(err))
				return
			}
		}
	}
}
