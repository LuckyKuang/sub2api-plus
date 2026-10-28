package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	// Reserve recovery space before writing a Redis owner. Active requests keep
	// their reservation until a confirmed release, so an outage cannot overflow
	// a queue by making every admitted execution finish at the same instant.
	// This is a per-instance memory safety bound, not a user concurrency limit;
	// exhausting it must reject admission before issuing a Redis acquisition.
	concurrencyOwnerRecoveryCapacity    = 65536
	concurrencyOwnerRecoveryBatch       = 64
	concurrencyOwnerRecoveryFallbackTTL = 30 * time.Minute
)

var errConcurrencyOwnerRecoveryUnavailable = errors.New("concurrency owner recovery unavailable")

type concurrencyOwner struct {
	scope     string
	id        int64
	requestID string
}

type concurrencyOwnerGroup struct {
	scope string
	id    int64
}

type concurrencyOwnerState struct {
	busy      bool
	expiresAt time.Time
}

type concurrencyOwnerRecovery struct {
	mu       sync.Mutex
	capacity int
	owners   map[concurrencyOwner]*concurrencyOwnerState
	pending  map[concurrencyOwnerGroup]map[concurrencyOwner]*concurrencyOwnerState
}

func (s *ConcurrencyService) reserveConcurrencyOwner(owner concurrencyOwner) error {
	r := &s.ownerRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	capacity := r.capacity
	if capacity <= 0 {
		capacity = concurrencyOwnerRecoveryCapacity
	}
	if len(r.owners) >= capacity {
		// The optional cleanup worker may be disabled, and these completed
		// users may never return. Reclaim only proven-expired pending records
		// locally before refusing unrelated admissions. Active executions and
		// in-flight recovery operations must retain their reservations.
		now := time.Now()
		for _, pending := range r.pending {
			for expired, state := range pending {
				if !state.busy && !now.Before(state.expiresAt) {
					r.forgetLocked(expired)
				}
			}
		}
	}
	if len(r.owners) >= capacity {
		return errConcurrencyOwnerRecoveryUnavailable
	}
	if r.owners == nil {
		r.owners = make(map[concurrencyOwner]*concurrencyOwnerState)
	}
	r.owners[owner] = &concurrencyOwnerState{}
	return nil
}

func (s *ConcurrencyService) forgetConcurrencyOwner(owner concurrencyOwner) {
	r := &s.ownerRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgetLocked(owner)
}

func (r *concurrencyOwnerRecovery) forgetLocked(owner concurrencyOwner) {
	delete(r.owners, owner)
	group := concurrencyOwnerGroup{scope: owner.scope, id: owner.id}
	delete(r.pending[group], owner)
	if len(r.pending[group]) == 0 {
		delete(r.pending, group)
	}
}

func (s *ConcurrencyService) markConcurrencyOwnerPending(owner concurrencyOwner) {
	ttl := concurrencyOwnerRecoveryFallbackTTL
	if cache, ok := s.cache.(ConcurrencyLeaseCache); ok && cache.ConcurrencySlotTTL() > 0 {
		ttl = cache.ConcurrencySlotTTL()
	}
	r := &s.ownerRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.owners[owner]
	if state == nil {
		return
	}
	// No renewer remains when a released/uncertain owner is registered here.
	// Starting at registration is conservative relative to its last Redis write.
	state.expiresAt = time.Now().Add(ttl)
	group := concurrencyOwnerGroup{scope: owner.scope, id: owner.id}
	if r.pending == nil {
		r.pending = make(map[concurrencyOwnerGroup]map[concurrencyOwner]*concurrencyOwnerState)
	}
	if r.pending[group] == nil {
		r.pending[group] = make(map[concurrencyOwner]*concurrencyOwnerState)
	}
	r.pending[group][owner] = state
}

func (s *ConcurrencyService) releaseConcurrencyOwner(ctx context.Context, owner concurrencyOwner) error {
	if owner.scope == "user" {
		return s.cache.ReleaseUserSlot(ctx, owner.id, owner.requestID)
	}
	return s.cache.ReleaseAccountSlot(ctx, owner.id, owner.requestID)
}

// reclaimConcurrencyOwner closes an uncertain admission or completed execution.
// It never changes another owner, and a failed delete retains its reserved
// recovery record instead of spawning one goroutine per failed Redis command.
func (s *ConcurrencyService) reclaimConcurrencyOwner(owner concurrencyOwner) {
	ctx, cancel := context.WithTimeout(context.Background(), regularLeaseOperationTimeout)
	err := s.releaseConcurrencyOwner(ctx, owner)
	cancel()
	if err == nil {
		s.forgetConcurrencyOwner(owner)
		return
	}
	s.markConcurrencyOwnerPending(owner)
	logger.L().Warn("concurrency_slot_release_pending", zap.String("scope", owner.scope), zap.Int64("owner_id", owner.id), zap.Error(err))
}

func (s *ConcurrencyService) pendingConcurrencyOwners(group *concurrencyOwnerGroup, maximum int) ([]concurrencyOwner, bool) {
	r := &s.ownerRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	owners := make([]concurrencyOwner, 0)
	busy := false
	now := time.Now()
	collect := func(pending map[concurrencyOwner]*concurrencyOwnerState) {
		for owner, state := range pending {
			if !now.Before(state.expiresAt) {
				r.forgetLocked(owner)
				continue
			}
			if state.busy {
				busy = true
				continue
			}
			if len(owners) >= maximum {
				busy = true
				continue
			}
			state.busy = true
			owners = append(owners, owner)
		}
	}
	if group != nil {
		collect(r.pending[*group])
	} else {
		for _, pending := range r.pending {
			collect(pending)
		}
	}
	return owners, busy
}

func (s *ConcurrencyService) finishConcurrencyOwnerRecovery(owner concurrencyOwner, success bool) {
	r := &s.ownerRecovery
	r.mu.Lock()
	defer r.mu.Unlock()
	if success {
		r.forgetLocked(owner)
		return
	}
	if state := r.owners[owner]; state != nil {
		state.busy = false
	}
}

// New admission tries only this user's/account's pending releases. A store
// failure is dependency unavailability, not an assertion that this user is full.
func (s *ConcurrencyService) recoverConcurrencyOwners(ctx context.Context, scope string, id int64) error {
	group := concurrencyOwnerGroup{scope: scope, id: id}
	owners, busy := s.pendingConcurrencyOwners(&group, concurrencyOwnerRecoveryBatch)
	if len(owners) == 0 {
		if busy {
			return errConcurrencyOwnerRecoveryUnavailable
		}
		return nil
	}
	recoveryCtx, cancel := context.WithTimeout(ctx, regularLeaseOperationTimeout)
	defer cancel()
	failed := busy
	for _, owner := range owners {
		err := recoveryCtx.Err()
		if err == nil {
			err = s.releaseConcurrencyOwner(recoveryCtx, owner)
		}
		s.finishConcurrencyOwnerRecovery(owner, err == nil)
		failed = failed || err != nil
	}
	if failed {
		return errConcurrencyOwnerRecoveryUnavailable
	}
	return nil
}

// The existing cache-wide slot cleanup worker also retries completed owners.
// One finite batch shares its caller's total deadline, even throughout outages.
func (s *ConcurrencyService) recoverPendingConcurrencyOwners(ctx context.Context) {
	owners, _ := s.pendingConcurrencyOwners(nil, concurrencyOwnerRecoveryBatch)
	for _, owner := range owners {
		err := ctx.Err()
		if err == nil {
			err = s.releaseConcurrencyOwner(ctx, owner)
		}
		s.finishConcurrencyOwnerRecovery(owner, err == nil)
	}
}
