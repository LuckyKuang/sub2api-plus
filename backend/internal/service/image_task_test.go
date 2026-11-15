//go:build unit || !integration

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
		if !owner.Matches(task) || (filter.Status != "" && task.Status != filter.Status) ||
			(filter.Platform != "" && task.Platform != filter.Platform) ||
			(filter.Model != "" && task.Model != filter.Model) {
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
	if task == nil || !owner.Matches(task) {
		return nil, ErrImageTaskNotFound
	}
	copy := *task
	return &copy, nil
}

func (h *imageTaskMemoryHistory) HasAnyTaskForUser(_ context.Context, userID int64) (bool, error) {
	for _, task := range h.tasks {
		if task != nil && task.UserID == userID {
			return true, nil
		}
	}
	return false, nil
}

func (h *imageTaskMemoryHistory) DeleteFailed(_ context.Context, owner ImageTaskOwner, id string) (bool, error) {
	if h.deleteErr != nil {
		return false, h.deleteErr
	}
	task := h.tasks[id]
	if task == nil || !owner.Matches(task) || task.Status != ImageTaskStatusFailed {
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
	// R6: an unarchived upstream URL is not a durable image reference.
	require.Empty(t, completed.ImageURL)
	require.Equal(t, "unarchived", completed.ReadStatus)
	require.JSONEq(t, `{"created":123,"data":[{"url":""}]}`, string(completed.Result))
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
	require.Empty(t, completed.Data[0].ImageURL)
	require.Equal(t, "unarchived", completed.Data[0].ArchiveStatus)
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

// The console must keep a task readable after the Redis execution record has
// expired: the durable history row is the only remaining copy.
func TestImageTaskServiceGetFallsBackToDurableHistory(t *testing.T) {
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	svc.SetHistoryRepository(history)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK,
		json.RawMessage(`{"created":1,"data":[{"url":"https://example.test/image.png"}]}`)))
	store.task = nil // simulate Redis TTL expiry

	got, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, got.Status)
	require.Empty(t, got.ImageURL)
	require.Equal(t, "unarchived", got.ReadStatus)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound, "another key of the same user stays hidden on the gateway contract")
}

// The console reads by user only, which must include a task created by a key the
// user has since deleted, and must still reject another user.
func TestImageTaskServiceUserScopeCoversEveryKeyOfTheOwner(t *testing.T) {
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	svc.SetHistoryRepository(history)

	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)

	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 7}, created.ID)
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 8}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

type signingImageStorage struct {
	signed []string
}

func (s *signingImageStorage) Save(context.Context, string, string, []byte) (string, error) {
	return "https://stale.example.test/expired", nil
}

func (s *signingImageStorage) Sign(_ context.Context, key string) (string, error) {
	s.signed = append(s.signed, key)
	return "https://fresh.example.test/" + key, nil
}

// Stored result URLs are minted at upload time and a presigned one expires long
// before the history row. Every read must re-sign the recorded object keys.
func TestImageTaskServiceReSignsStoredImageURLs(t *testing.T) {
	storage := &signingImageStorage{}
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithUploader(store, NewImageResultUploader(storage, "images/", false, 0, nil), time.Hour, time.Minute)

	record := &ImageTaskRecord{
		ID:           "imgtask_signed",
		UserID:       7,
		APIKeyID:     9,
		RequestType:  "generation",
		Model:        "gpt-image-2",
		Platform:     "openai",
		Status:       ImageTaskStatusCompleted,
		Result:       json.RawMessage(`{"created":1,"data":[{"url":"https://stale.example.test/a"},{"url":"https://stale.example.test/b"}]}`),
		StorageKeys:  []string{"images/task-0.png", "images/task-1.png"},
		ActualImages: 2,
		CreatedAt:    time.Now().Unix(),
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
	store.task = record

	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9}, record.ID)
	require.NoError(t, err)
	require.Equal(t, "https://fresh.example.test/images/task-0.png", got.ImageURL)
	require.NotContains(t, string(got.Result), "stale.example.test")
	require.Equal(t, []string{"images/task-0.png", "images/task-1.png"}, storage.signed)
}

// R6: durable terminal history remains readable during a Redis outage, with
// exactly the same user/key isolation as during normal cache operation.
func TestImageTaskServiceReadsTerminalHistoryDuringCacheOutage(t *testing.T) {
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	store := &imageTaskMemoryStore{getErr: errors.New("redis unavailable")}
	history := &imageTaskMemoryHistory{}
	require.NoError(t, history.Save(context.Background(), &ImageTaskRecord{ID: "imgtask_archived", UserID: 7, APIKeyID: 9, Status: ImageTaskStatusFailed}))
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)
	got, err := svc.Get(context.Background(), owner, "imgtask_archived")
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	for _, foreign := range []ImageTaskOwner{{UserID: 8}, {UserID: 7, APIKeyID: 10}} {
		_, err = svc.Get(context.Background(), foreign, "imgtask_archived")
		require.ErrorIs(t, err, ErrImageTaskNotFound)
	}
	history.getErr = errors.New("postgres unavailable")
	_, err = svc.Get(context.Background(), owner, "imgtask_archived")
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

// R6: the cache is not the durable completion boundary. A failed cache write
// cannot discard an already-observed provider result or its history index.
func TestImageTaskServicePersistsTerminalHistoryWhenCacheWriteFails(t *testing.T) {
	ctx := context.Background()
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)
	task, err := svc.Create(ctx, owner)
	require.NoError(t, err)
	store.saveErr = errors.New("redis write unavailable")
	require.NoError(t, svc.Fail(ctx, task.ID, 502, []byte(`{"message":"provider failed"}`)))
	stored, err := history.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, stored.Status)
}

func (h *imageTaskMemoryHistory) GetForCompletion(ctx context.Context, id string) (*ImageTaskRecord, error) {
	if h.getErr != nil {
		return nil, h.getErr
	}
	record := h.tasks[id]
	if record == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *record
	return &copy, nil
}

// R6: completing an accepted task during a cache outage still persists the
// observed result from its existing history identity, without reissuing work.
func TestImageTaskServiceFinishesDuringCacheOutage(t *testing.T) {
	ctx := context.Background()
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	store := &imageTaskMemoryStore{}
	history := &imageTaskMemoryHistory{}
	svc := NewImageTaskService(store)
	svc.SetHistoryRepository(history)
	task, err := svc.Create(ctx, owner)
	require.NoError(t, err)
	store.getErr = errors.New("redis unavailable")
	store.saveErr = store.getErr
	require.NoError(t, svc.Fail(ctx, task.ID, 502, []byte(`{"message":"provider failed"}`)))
	got, err := svc.Get(ctx, owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
}

// R6/R7: never replay old URLs when current storage cannot issue fresh ones;
// do not mark an incomplete object-key index as a readable archive.
func TestImageTaskServiceDoesNotReplayExpiredOrIncompleteImageURLs(t *testing.T) {
	for _, keys := range [][]string{nil, {"old-prefix/a.png"}, {"old-prefix/a.png", ""}} {
		t.Run(string(rune('0'+len(keys))), func(t *testing.T) {
			record := &ImageTaskRecord{ID: "imgtask_old", UserID: 7, APIKeyID: 9, Status: ImageTaskStatusCompleted, HistoryPersisted: true, StorageKeys: keys, Result: []byte(`{"data":[{"url":"https://old.test/expired-a"},{"url":"https://old.test/expired-b"}]}`)}
			svc := NewImageTaskService(&imageTaskMemoryStore{task: record})
			got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 7}, record.ID)
			require.NoError(t, err)
			require.Empty(t, got.ImageURL)
			require.NotContains(t, string(got.Result), "old.test")
			require.NotEqual(t, "readable", got.ReadStatus)
			require.NotEqual(t, "archived", got.ArchiveStatus)
		})
	}
}

// Malformed/incomplete historical results must remain unarchived instead of
// crashing a signed-in history read or guessing an object by its ordinal.
func TestImageTaskServiceNullHistoryImageDoesNotPanic(t *testing.T) {
	record := &ImageTaskRecord{ID: "imgtask_null", UserID: 7, APIKeyID: 9, Status: ImageTaskStatusCompleted, Result: []byte(`{"data":[null]}`)}
	svc := NewImageTaskService(&imageTaskMemoryStore{task: record})
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 7}, record.ID)
	require.NoError(t, err)
	require.Equal(t, "unarchived", got.ArchiveStatus)
	require.Empty(t, got.ImageURL)
}
