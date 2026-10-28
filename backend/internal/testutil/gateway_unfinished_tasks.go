//go:build !e2e

package testutil

import (
	"context"
	"sync"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// StubGatewayUnfinishedTaskRepo is an explicit isolated-fixture admission
// authority. Real serialization and persistence are covered by PostgreSQL tests.
type StubGatewayUnfinishedTaskRepo struct {
	mu                                    sync.Mutex
	Active                                map[string]*service.GatewayTaskReservation
	Bindings                              map[string]string
	Uncertain                             map[string]bool
	ReserveCalls, BindCalls, ReleaseCalls int
	Err                                   error
}

func (r *StubGatewayUnfinishedTaskRepo) Reserve(_ context.Context, reservation *service.GatewayTaskReservation, limit int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ReserveCalls++
	if r.Err != nil {
		return r.Err
	}
	if r.Active == nil {
		r.Active = make(map[string]*service.GatewayTaskReservation)
		r.Bindings = make(map[string]string)
		r.Uncertain = make(map[string]bool)
	}
	count := 0
	for _, active := range r.Active {
		if active.Owner.UserID == reservation.Owner.UserID {
			count++
		}
	}
	if count >= limit {
		return service.ErrGatewayUnfinishedTaskLimit
	}
	copy := *reservation
	r.Active[reservation.ID] = &copy
	return nil
}

func (r *StubGatewayUnfinishedTaskRepo) Bind(_ context.Context, reservation *service.GatewayTaskReservation, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.BindCalls++
	if r.Err != nil {
		return r.Err
	}
	if active := r.Active[reservation.ID]; active == nil || *active != *reservation {
		return service.ErrGatewayTaskReservationConflict
	}
	if previous := r.Bindings[reservation.ID]; previous != "" && previous != taskID {
		return service.ErrGatewayTaskReservationConflict
	}
	r.Bindings[reservation.ID] = taskID
	delete(r.Uncertain, reservation.ID)
	return nil
}

func (r *StubGatewayUnfinishedTaskRepo) MarkUncertain(_ context.Context, reservation *service.GatewayTaskReservation, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	if active := r.Active[reservation.ID]; active == nil || *active != *reservation {
		return service.ErrGatewayTaskReservationConflict
	}
	r.Uncertain[reservation.ID] = true
	return nil
}

func (r *StubGatewayUnfinishedTaskRepo) Release(_ context.Context, reservation *service.GatewayTaskReservation, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ReleaseCalls++
	if r.Err != nil {
		return r.Err
	}
	if active := r.Active[reservation.ID]; active != nil && *active != *reservation {
		return service.ErrGatewayTaskReservationConflict
	}
	delete(r.Active, reservation.ID)
	return nil
}

func (r *StubGatewayUnfinishedTaskRepo) ReleaseTask(_ context.Context, owner service.GatewayTaskOwner, kind, taskID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Err != nil {
		return r.Err
	}
	for id, active := range r.Active {
		if active.Owner == owner && active.Kind == kind && r.Bindings[id] == taskID && (!r.Uncertain[id] || reason == "provider_terminal" || reason == "task_completed" || reason == "task_cancelled_confirmed") {
			delete(r.Active, id)
			r.ReleaseCalls++
		}
	}
	return nil
}
