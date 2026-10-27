//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type unfinishedTaskRepoForTest struct {
	mu           sync.Mutex
	reservations map[string]*GatewayTaskReservation
	bound        map[string]string
	uncertain    map[string]bool
}

func (r *unfinishedTaskRepoForTest) Reserve(_ context.Context, slot *GatewayTaskReservation, limit int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reservations == nil {
		r.reservations = map[string]*GatewayTaskReservation{}
		r.bound = map[string]string{}
		r.uncertain = map[string]bool{}
	}
	count := 0
	for _, old := range r.reservations {
		if old.Owner.UserID == slot.Owner.UserID {
			count++
		}
	}
	if count >= limit {
		return ErrGatewayUnfinishedTaskLimit
	}
	copy := *slot
	r.reservations[slot.ID] = &copy
	return nil
}
func (r *unfinishedTaskRepoForTest) Bind(_ context.Context, slot *GatewayTaskReservation, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reservations[slot.ID] == nil {
		return ErrGatewayTaskReservationConflict
	}
	r.bound[slot.ID] = taskID
	delete(r.uncertain, slot.ID)
	return nil
}
func (r *unfinishedTaskRepoForTest) MarkUncertain(_ context.Context, slot *GatewayTaskReservation, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uncertain[slot.ID] = true
	return nil
}
func (r *unfinishedTaskRepoForTest) Release(_ context.Context, slot *GatewayTaskReservation, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.reservations, slot.ID)
	return nil
}
func (r *unfinishedTaskRepoForTest) ReleaseTask(_ context.Context, owner GatewayTaskOwner, kind, taskID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, slot := range r.reservations {
		if slot.Owner == owner && slot.Kind == kind && r.bound[id] == taskID && (!r.uncertain[id] || reason == "provider_terminal" || reason == "task_completed" || reason == "task_cancelled_confirmed") {
			delete(r.reservations, id)
		}
	}
	return nil
}

func TestBatchImageUnfinishedCapBlocksOtherKeyAndSettlementRestoresCapacity(t *testing.T) {
	svc, repo, _, provider, _ := newTestBatchImagePublicService(true)
	tasks := &unfinishedTaskRepoForTest{}
	svc.UnfinishedTasks = NewGatewayUnfinishedTaskService(tasks, &config.Config{Gateway: config.GatewayConfig{MaxUnfinishedTasksPerUser: 1}})
	owner := testBatchImageOwner()
	first, err := svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
	require.NoError(t, err)
	otherKey := owner
	otherKey.APIKeyID++
	_, err = svc.Submit(context.Background(), otherKey, validBatchImageSubmitRequest(), "")
	require.ErrorIs(t, err, ErrGatewayUnfinishedTaskLimit)
	require.Len(t, provider.submits, 1)
	require.Len(t, tasks.reservations, 1)
	job := repo.jobs[first.ID]
	job.Status, job.SuccessCount, job.FailCount = BatchImageJobStatusSettling, 1, 1
	settlement := &BatchImageSettlementService{Repo: repo, BillingRepo: svc.BillingRepo, Pricing: svc.Pricing, UnfinishedTasks: svc.UnfinishedTasks}
	_, err = settlement.Settle(context.Background(), first.ID)
	require.NoError(t, err)
	require.Empty(t, tasks.reservations)
	_, err = svc.Submit(context.Background(), otherKey, validBatchImageSubmitRequest(), "")
	require.NoError(t, err)
	require.Len(t, provider.submits, 2)
}

func TestBatchImageUnknownCreateRetainsPersistentCapacityAfterLocalFailure(t *testing.T) {
	svc, repo, _, provider, _ := newTestBatchImagePublicService(true)
	tasks := &unfinishedTaskRepoForTest{}
	svc.UnfinishedTasks = NewGatewayUnfinishedTaskService(tasks, &config.Config{Gateway: config.GatewayConfig{MaxUnfinishedTasksPerUser: 1}})
	provider.submitErr = errors.New("response lost after provider accepted")
	owner := testBatchImageOwner()
	_, err := svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
	require.Error(t, err)
	require.Len(t, tasks.reservations, 1)
	require.Len(t, tasks.uncertain, 1)
	for _, job := range repo.jobs {
		_, err = svc.Cancel(context.Background(), owner, job.BatchID)
		require.NoError(t, err)
	}
	require.Len(t, tasks.reservations, 1, "local failed/cancelled cannot prove an unknown provider create stopped")
	_, err = svc.Submit(context.Background(), owner, validBatchImageSubmitRequest(), "")
	require.ErrorIs(t, err, ErrGatewayUnfinishedTaskLimit)
	require.Len(t, provider.submits, 1)
}

func TestGatewayUnfinishedTaskMissingAuthorityFailsClosed(t *testing.T) {
	var service *GatewayUnfinishedTaskService
	_, err := service.Reserve(context.Background(), GatewayTaskOwner{UserID: 1, APIKeyID: 2}, GatewayTaskKindBatchImage)
	require.ErrorIs(t, err, ErrGatewayUnfinishedTaskUnavailable)
	svc, _, _, provider, _ := newTestBatchImagePublicService(true)
	svc.UnfinishedTasks = nil
	_, err = svc.Submit(context.Background(), testBatchImageOwner(), validBatchImageSubmitRequest(), "")
	require.ErrorIs(t, err, ErrGatewayUnfinishedTaskUnavailable)
	require.Empty(t, provider.submits)
}
