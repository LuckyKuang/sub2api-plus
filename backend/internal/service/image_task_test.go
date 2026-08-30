package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskMemoryStore struct {
	task      *ImageTaskRecord
	ttl       time.Duration
	saveErr   error
	getErr    error
	deleteErr error
}

type imageTaskMemoryHistory struct {
	tasks     map[string]*ImageTaskRecord
	order     []string
	getErr    error
	deleteErr error
}

func (h *imageTaskMemoryHistory) Save(_ context.Context, task *ImageTaskRecord) error {
	if h.tasks == nil {
		h.tasks = make(map[string]*ImageTaskRecord)
	}
	if _, exists := h.tasks[task.ID]; !exists {
		h.order = append(h.order, task.ID)
	}
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	copy.StorageKeys = append([]string(nil), task.StorageKeys...)
	h.tasks[task.ID] = &copy
	return nil
}

func (h *imageTaskMemoryHistory) List(_ context.Context, owner ImageTaskOwner, filter ImageTaskHistoryFilter) ([]*ImageTaskRecord, bool, error) {
	filter = normalizeImageTaskHistoryFilter(filter)
	matched := make([]*ImageTaskRecord, 0, len(h.order))
	for i := len(h.order) - 1; i >= 0; i-- {
		task := h.tasks[h.order[i]]
		if task == nil {
			continue
		}
		if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID || (filter.Status != "" && task.Status != filter.Status) {
			continue
		}
		copy := *task
		copy.Result = append(json.RawMessage(nil), task.Result...)
		copy.Error = append(json.RawMessage(nil), task.Error...)
		matched = append(matched, &copy)
	}
	if filter.Offset >= len(matched) {
		return nil, false, nil
	}
	matched = matched[filter.Offset:]
	hasMore := len(matched) > filter.Limit
	if hasMore {
		matched = matched[:filter.Limit]
	}
	return matched, hasMore, nil
}

func (h *imageTaskMemoryHistory) Get(_ context.Context, owner ImageTaskOwner, id string) (*ImageTaskRecord, error) {
	if h.getErr != nil {
		return nil, h.getErr
	}
	task := h.tasks[id]
	if task == nil || task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		return nil, ErrImageTaskNotFound
	}
	copy := *task
	return &copy, nil
}

func (h *imageTaskMemoryHistory) ListByUser(_ context.Context, userID int64, filter ImageTaskHistoryFilter) ([]*ImageTaskRecord, bool, error) {
	filter = normalizeImageTaskHistoryFilter(filter)
	matched := make([]*ImageTaskRecord, 0, len(h.order))
	for i := len(h.order) - 1; i >= 0; i-- {
		task := h.tasks[h.order[i]]
		if task == nil || task.UserID != userID || (filter.Status != "" && task.Status != filter.Status) {
			continue
		}
		copy := *task
		matched = append(matched, &copy)
	}
	if filter.Offset >= len(matched) {
		return nil, false, nil
	}
	matched = matched[filter.Offset:]
	hasMore := len(matched) > filter.Limit
	if hasMore {
		matched = matched[:filter.Limit]
	}
	return matched, hasMore, nil
}

func (h *imageTaskMemoryHistory) GetByUser(_ context.Context, userID int64, id string) (*ImageTaskRecord, error) {
	task := h.tasks[id]
	if task == nil || task.UserID != userID {
		return nil, ErrImageTaskNotFound
	}
	copy := *task
	return &copy, nil
}

func (h *imageTaskMemoryHistory) DeleteFailed(_ context.Context, owner ImageTaskOwner, id string) (bool, error) {
	if h.deleteErr != nil {
		return false, h.deleteErr
	}
	task := h.tasks[id]
	if task == nil || task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID || task.Status != ImageTaskStatusFailed {
		return false, nil
	}
	delete(h.tasks, id)
	return true, nil
}

func (s *imageTaskMemoryStore) Save(_ context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *task
	s.task = &copy
	s.ttl = ttl
	return nil
}

func (s *imageTaskMemoryStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func (s *imageTaskMemoryStore) DeleteIfStatus(_ context.Context, _ string, status string) (bool, bool, error) {
	if s.deleteErr != nil {
		return false, false, s.deleteErr
	}
	if s.task == nil {
		return false, false, nil
	}
	if s.task.Status != status {
		return false, true, nil
	}
	s.task = nil
	return true, true, nil
}

func TestImageTaskServiceLifecycleAndOwnership(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, created.Status)
	require.Equal(t, created.ID, created.TaskID)
	require.Equal(t, "image.generation.task", created.Object)
	require.Equal(t, time.Hour, store.ttl)
	require.Equal(t, owner.UserID, store.task.UserID)
	require.Equal(t, owner.APIKeyID, store.task.APIKeyID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://example.test/image.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	completed, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Equal(t, http.StatusOK, completed.HTTPStatus)
	require.Equal(t, "https://example.test/image.png", completed.ImageURL)
	require.JSONEq(t, string(result), string(completed.Result))
	require.NotNil(t, completed.CompletedAt)
}

func TestImageTaskServiceInvalidResultBecomesFailed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)

	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, json.RawMessage(`not-json`)))
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Equal(t, http.StatusBadGateway, got.HTTPStatus)
	require.Contains(t, string(got.Error), "non-JSON")
}

func TestImageTaskServiceMapsStoreFailures(t *testing.T) {
	store := &imageTaskMemoryStore{saveErr: errors.New("redis down")}
	svc := NewImageTaskService(store)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

func TestImageTaskServiceHistoryListKeepsMetadataAndScopesByAPIKey(t *testing.T) {
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	svc.SetHistoryRepository(history)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.CreateWithMetadata(context.Background(), owner, ImageTaskMetadata{
		RequestType:     "generation",
		Model:           "gpt-image-1",
		PromptPreview:   "Draw a glass greenhouse under a bright sky.",
		RequestedImages: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "generation", created.RequestType)
	require.Equal(t, "gpt-image-1", created.Model)
	require.Equal(t, "Draw a glass greenhouse under a bright sky.", created.PromptPreview)
	require.Equal(t, 2, created.RequestedImages)

	processing, err := svc.List(context.Background(), owner, ImageTaskHistoryFilter{})
	require.NoError(t, err)
	require.Len(t, processing.Data, 1)
	require.Equal(t, created.TaskID, processing.Data[0].TaskID)
	require.Equal(t, ImageTaskStatusProcessing, processing.Data[0].Status)
	require.Equal(t, created.PromptPreview, processing.Data[0].PromptPreview)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://storage.example.test/images/task.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.TaskID, http.StatusOK, result))

	completed, err := svc.List(context.Background(), owner, ImageTaskHistoryFilter{Status: ImageTaskStatusCompleted})
	require.NoError(t, err)
	require.Len(t, completed.Data, 1)
	require.Equal(t, ImageTaskStatusCompleted, completed.Data[0].Status)
	require.Equal(t, "https://storage.example.test/images/task.png", completed.Data[0].ImageURL)
	require.Equal(t, 2, completed.Data[0].RequestedImages)
	require.Equal(t, 1, completed.Data[0].ActualImages)
	require.NotNil(t, completed.Data[0].CompletedAt)

	otherKey, err := svc.List(context.Background(), ImageTaskOwner{UserID: owner.UserID, APIKeyID: owner.APIKeyID + 1}, ImageTaskHistoryFilter{})
	require.NoError(t, err)
	require.Empty(t, otherKey.Data)
}

func TestImageTaskPublicViewDerivesLegacyCompletedImageCount(t *testing.T) {
	task := imageTaskToPublic(&ImageTaskRecord{
		ID:              "imgtask_legacy",
		Status:          ImageTaskStatusCompleted,
		RequestedImages: 0,
		ActualImages:    0,
		Result:          json.RawMessage(`{"data":[{"url":"https://example.test/one.png"},{"url":"https://example.test/two.png"}]}`),
	})
	require.Equal(t, 1, task.RequestedImages)
	require.Equal(t, 2, task.ActualImages)
}

func TestImageTaskServiceAdminListByUserReadsAcrossKeysWithoutStoreMutation(t *testing.T) {
	history := &imageTaskMemoryHistory{
		tasks: map[string]*ImageTaskRecord{
			"target-a": {ID: "target-a", UserID: 7, APIKeyID: 9, Status: ImageTaskStatusCompleted, CreatedAt: 10},
			"target-b": {ID: "target-b", UserID: 7, APIKeyID: 10, Status: ImageTaskStatusFailed, CreatedAt: 20},
			"other":    {ID: "other", UserID: 8, APIKeyID: 11, Status: ImageTaskStatusCompleted, CreatedAt: 30},
		},
		order: []string{"target-a", "target-b", "other"},
	}
	svc := NewImageTaskService(&imageTaskMemoryStore{})
	svc.SetHistoryRepository(history)

	list, err := svc.ListByUser(context.Background(), 7, ImageTaskHistoryFilter{Limit: 20})
	require.NoError(t, err)
	require.Len(t, list.Items, 2)
	require.Equal(t, "target-b", list.Items[0].TaskID)
	require.Equal(t, int64(10), list.Items[0].APIKeyID)
	require.Equal(t, "target-a", list.Items[1].TaskID)
	require.NotContains(t, string(mustImageTaskJSON(t, list)), "storage_keys")

	_, err = svc.GetByUser(context.Background(), 8, "target-a")
	require.ErrorIs(t, err, ErrImageTaskNotFound)
	require.Len(t, history.tasks, 3)
}

func mustImageTaskJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func TestImageTaskServiceDeleteRemovesOwnedFailedTask(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task := &ImageTaskRecord{ID: "imgtask_failed", UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusFailed}
	store := &imageTaskMemoryStore{task: task}
	history := &imageTaskMemoryHistory{tasks: map[string]*ImageTaskRecord{task.ID: task}, order: []string{task.ID}}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)

	require.NoError(t, svc.Delete(context.Background(), owner, task.ID))
	require.Nil(t, store.task)
	require.NotContains(t, history.tasks, task.ID)
}

func TestImageTaskServiceDeleteSucceedsWhenRedisTaskExpired(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task := &ImageTaskRecord{ID: "imgtask_expired", UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusFailed}
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{tasks: map[string]*ImageTaskRecord{task.ID: task}}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)

	require.NoError(t, svc.Delete(context.Background(), owner, task.ID))
	require.NotContains(t, history.tasks, task.ID)
}

func TestImageTaskServiceDeleteRejectsNonFailedAndOtherOwner(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	for _, status := range []string{ImageTaskStatusProcessing, ImageTaskStatusCompleted} {
		task := &ImageTaskRecord{ID: "imgtask_" + status, UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: status}
		store := &imageTaskMemoryStore{task: task}
		history := &imageTaskMemoryHistory{tasks: map[string]*ImageTaskRecord{task.ID: task}}
		svc := NewImageTaskService(store)
		svc.SetHistoryRepository(history)

		err := svc.Delete(context.Background(), owner, task.ID)
		require.ErrorIs(t, err, ErrImageTaskDeleteNotAllowed)
		require.NotNil(t, store.task)
		require.Contains(t, history.tasks, task.ID)
	}

	task := &ImageTaskRecord{ID: "imgtask_other_owner", UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusFailed}
	store := &imageTaskMemoryStore{task: task}
	history := &imageTaskMemoryHistory{tasks: map[string]*ImageTaskRecord{task.ID: task}}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)
	err := svc.Delete(context.Background(), ImageTaskOwner{UserID: owner.UserID, APIKeyID: owner.APIKeyID + 1}, task.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
	require.NotNil(t, store.task)
	require.Contains(t, history.tasks, task.ID)
}

func TestImageTaskServiceDeleteRedisFailurePreservesHistory(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task := &ImageTaskRecord{ID: "imgtask_failed", UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusFailed}
	store := &imageTaskMemoryStore{task: task, deleteErr: errors.New("redis unavailable")}
	history := &imageTaskMemoryHistory{tasks: map[string]*ImageTaskRecord{task.ID: task}}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)

	err := svc.Delete(context.Background(), owner, task.ID)
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
	require.NotNil(t, store.task)
	require.Contains(t, history.tasks, task.ID)
}

func TestImageTaskServiceDeleteDoesNotRemoveRedisTaskWhoseStatusChanged(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	historyTask := &ImageTaskRecord{ID: "imgtask_raced", UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusFailed}
	redisTask := &ImageTaskRecord{ID: historyTask.ID, UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusCompleted}
	store := &imageTaskMemoryStore{task: redisTask}
	history := &imageTaskMemoryHistory{tasks: map[string]*ImageTaskRecord{historyTask.ID: historyTask}}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)

	err := svc.Delete(context.Background(), owner, historyTask.ID)
	require.ErrorIs(t, err, ErrImageTaskDeleteNotAllowed)
	require.Equal(t, ImageTaskStatusCompleted, store.task.Status)
	require.Contains(t, history.tasks, historyTask.ID)
}

func TestImageTaskServiceDeleteHistoryFailureRemainsRetryable(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task := &ImageTaskRecord{ID: "imgtask_failed", UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusFailed}
	store := &imageTaskMemoryStore{task: task}
	history := &imageTaskMemoryHistory{
		tasks:     map[string]*ImageTaskRecord{task.ID: task},
		deleteErr: errors.New("postgres unavailable"),
	}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)

	err := svc.Delete(context.Background(), owner, task.ID)
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
	require.Nil(t, store.task)
	require.Contains(t, history.tasks, task.ID)

	history.deleteErr = nil
	require.NoError(t, svc.Delete(context.Background(), owner, task.ID))
	require.NotContains(t, history.tasks, task.ID)
}
