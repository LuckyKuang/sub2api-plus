//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type retryHistoryRepo struct {
	failures int
	saved    []*ImageTaskRecord
}

func (r *retryHistoryRepo) Save(_ context.Context, task *ImageTaskRecord) error {
	if r.failures > 0 {
		r.failures--
		return errors.New("database unavailable")
	}
	r.saved = append(r.saved, task)
	return nil
}

func (r *retryHistoryRepo) List(context.Context, ImageTaskOwner, ImageTaskHistoryFilter) ([]*ImageTaskRecord, bool, error) {
	return nil, false, nil
}
func (r *retryHistoryRepo) Get(_ context.Context, owner ImageTaskOwner, id string) (*ImageTaskRecord, error) {
	for _, record := range r.saved {
		if record != nil && record.ID == id && owner.Matches(record) {
			return record, nil
		}
	}
	return nil, ErrImageTaskNotFound
}
func (r *retryHistoryRepo) DeleteFailed(context.Context, ImageTaskOwner, string) (bool, error) {
	return false, nil
}
func (r *retryHistoryRepo) HasAnyTaskForUser(context.Context, int64) (bool, error) {
	return false, nil
}

type retryQueueRepo struct {
	pending []ImageTaskHistoryRetry
}

func (r *retryQueueRepo) Enqueue(_ context.Context, record *ImageTaskRecord) error {
	r.pending = append(r.pending, ImageTaskHistoryRetry{Record: record})
	return nil
}
func (r *retryQueueRepo) Due(context.Context, int) ([]ImageTaskHistoryRetry, error) {
	return r.pending, nil
}
func (r *retryQueueRepo) Delete(_ context.Context, taskID string) error {
	for i, item := range r.pending {
		if item.Record != nil && item.Record.ID == taskID {
			r.pending = append(r.pending[:i], r.pending[i+1:]...)
			return nil
		}
	}
	return nil
}
func (r *retryQueueRepo) Reschedule(_ context.Context, taskID string, next time.Time) error {
	for i, item := range r.pending {
		if item.Record != nil && item.Record.ID == taskID {
			r.pending[i].Attempts++
			return nil
		}
	}
	return nil
}

// A failed terminal write must be retried from the database, not lost with the
// Redis execution record. This is the acceptance scenario for R6.
func TestImageTaskHistoryRetryReappliesFailedTerminalWrite(t *testing.T) {
	store := &imageTaskMemoryStore{}
	history := &retryHistoryRepo{}
	tasks := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	tasks.SetHistoryRepository(history)
	queue := &retryQueueRepo{}
	svc := NewImageTaskHistoryRetryService(history, queue)
	tasks.SetHistoryRetryQueue(svc)

	created, err := tasks.Create(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	history.failures = 1 // the terminal write is the one that fails
	require.NoError(t, tasks.Complete(context.Background(), created.ID, 200, []byte(`{"data":[{"url":"https://example.test/i.png"}]}`)))
	require.Len(t, queue.pending, 1, "the failed write must be queued")

	// Redis now loses the record; only the queued payload can restore history.
	store.task = nil
	history.saved = nil // ignore the record written when the task was created
	saved, err := svc.RunOnce(context.Background(), time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, saved)
	require.Empty(t, queue.pending)
	require.Len(t, history.saved, 1)
	require.Equal(t, created.ID, history.saved[0].ID)

	got, err := tasks.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, got.Status)
}

// A still-failing write stays queued with an increased attempt counter and a
// later retry time instead of being dropped.
func TestImageTaskHistoryRetryKeepsFailingRowScheduled(t *testing.T) {
	history := &retryHistoryRepo{failures: 100}
	queue := &retryQueueRepo{pending: []ImageTaskHistoryRetry{{Record: &ImageTaskRecord{ID: "imgtask_retry", UserID: 7, APIKeyID: 9}}}}
	svc := NewImageTaskHistoryRetryService(history, queue)

	saved, err := svc.RunOnce(context.Background(), time.Now())
	require.NoError(t, err)
	require.Zero(t, saved)
	require.Len(t, queue.pending, 1)
	require.Equal(t, 1, queue.pending[0].Attempts)
}
