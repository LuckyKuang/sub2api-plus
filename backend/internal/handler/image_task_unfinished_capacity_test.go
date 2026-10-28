//go:build unit || !integration

package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A mutex-protected observer makes the terminal-release assertions safe while
// the production detached worker is still completing its deferred cleanup.
type asyncImageCapacityObserver struct {
	testutil.StubGatewayUnfinishedTaskRepo
	observedMu sync.Mutex
	releases   int
}

func (r *asyncImageCapacityObserver) ReleaseTask(ctx context.Context, owner service.GatewayTaskOwner, kind, taskID, reason string) error {
	err := r.StubGatewayUnfinishedTaskRepo.ReleaseTask(ctx, owner, kind, taskID, reason)
	if err == nil {
		r.observedMu.Lock()
		r.releases++
		r.observedMu.Unlock()
	}
	return err
}
func (r *asyncImageCapacityObserver) releaseCount() int {
	r.observedMu.Lock()
	defer r.observedMu.Unlock()
	return r.releases
}

func TestAsyncImageUnfinishedCapacityRejectsBeforeTaskCreationOrWorker(t *testing.T) {
	for _, capacityErr := range []error{service.ErrGatewayUnfinishedTaskLimit, service.ErrGatewayUnfinishedTaskUnavailable} {
		t.Run(capacityErr.Error(), func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			h := &AsyncImageHandler{tasks: service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute), openAI: newGatewayAdmissionForTest()}
			repo := &testutil.StubGatewayUnfinishedTaskRepo{Err: capacityErr}
			h.openAI.unfinishedTasks = service.NewGatewayUnfinishedTaskService(repo, nil)
			var executed atomic.Int32
			h.execute = func(string, *gin.Context) { executed.Add(1) }
			c, response := asyncImageCapacityContext(7, 9)
			h.Submit(c)
			expected := http.StatusTooManyRequests
			if capacityErr == service.ErrGatewayUnfinishedTaskUnavailable {
				expected = http.StatusServiceUnavailable
			}
			require.Equal(t, expected, response.Code, response.Body.String())
			store.mu.RLock()
			require.Empty(t, store.tasks)
			store.mu.RUnlock()
			require.Zero(t, executed.Load(), "a rejected durable capacity check cannot start a detached worker")
			require.Equal(t, 1, repo.ReserveCalls)
			require.Zero(t, repo.BindCalls)
			require.Zero(t, repo.ReleaseCalls)
		})
	}
}

type asyncImageWorkerCapacityRepo struct {
	testutil.StubGatewayUnfinishedTaskRepo
	markErr        error
	markCalls      int
	releaseReasons []string
}

func (r *asyncImageWorkerCapacityRepo) MarkUncertain(ctx context.Context, reservation *service.GatewayTaskReservation, reason string) error {
	r.markCalls++
	if r.markErr != nil {
		return r.markErr
	}
	return r.StubGatewayUnfinishedTaskRepo.MarkUncertain(ctx, reservation, reason)
}

func (r *asyncImageWorkerCapacityRepo) Release(ctx context.Context, reservation *service.GatewayTaskReservation, reason string) error {
	r.releaseReasons = append(r.releaseReasons, reason)
	return r.StubGatewayUnfinishedTaskRepo.Release(ctx, reservation, reason)
}

// An accepted local task that cannot start a worker has created no provider
// work. Its durable capacity must be available to another key. After execution
// begins, a local transport failure cannot establish upstream termination.
func TestAsyncImageWorkerPreExecutionFailuresReleaseUnfinishedCapacity(t *testing.T) {
	for _, scenario := range []string{"missing_user_context", "missing_concurrency_helper", "worker_acquire_error", "uncertain_write_error", "execution_unknown_failure"} {
		t.Run(scenario, func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
			owner := service.GatewayTaskOwner{UserID: 7, APIKeyID: 9}
			repo := &asyncImageWorkerCapacityRepo{}
			cfg := &config.Config{}
			cfg.Gateway.MaxUnfinishedTasksPerUser = 1
			unfinished := service.NewGatewayUnfinishedTaskService(repo, cfg)
			reservation, err := unfinished.Reserve(context.Background(), owner, service.GatewayTaskKindAsyncImage)
			require.NoError(t, err)
			task, err := tasks.Create(context.Background(), service.ImageTaskOwner(owner))
			require.NoError(t, err)
			require.NoError(t, unfinished.Bind(context.Background(), reservation, task.ID))
			var attemptedOwner string
			slots := &concurrencyCacheMock{
				acquireUserSlotFn: func(_ context.Context, _ int64, _ int, requestID string) (bool, error) {
					attemptedOwner = requestID
					if scenario == "worker_acquire_error" {
						return false, errors.New("isolated Redis admission failure")
					}
					return true, nil
				},
				releaseUserSlotFn: func(_ context.Context, userID int64, requestID string) error {
					require.Equal(t, owner.UserID, userID)
					require.NotEmpty(t, attemptedOwner)
					require.Equal(t, attemptedOwner, requestID, "cleanup must never release another execution's owner")
					return nil
				},
			}
			h := &AsyncImageHandler{tasks: tasks, openAI: &OpenAIGatewayHandler{
				unfinishedTasks:   unfinished,
				concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(slots), SSEPingFormatNone, 0),
			}}
			var executed int
			h.execute = func(_ string, c *gin.Context) {
				executed++
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "provider create result is unknown"}})
			}
			worker, response := asyncImageCapacityContext(owner.UserID, owner.APIKeyID)
			worker.Set("gateway.unfinished_task_reservation", reservation)
			if scenario == "missing_user_context" {
				delete(worker.Keys, string(middleware.ContextKeyUser))
			}
			if scenario == "missing_concurrency_helper" {
				h.openAI.concurrencyHelper = nil
			}
			if scenario == "uncertain_write_error" {
				repo.markErr = errors.New("isolated durable uncertainty failure")
			}
			workerCtx, cancel := context.WithCancel(worker.Request.Context())
			worker.Request = worker.Request.WithContext(workerCtx)
			h.run(task.ID, service.PlatformOpenAI, worker, response, cancel, asyncImageOpsSnapshot{})
			record, err := tasks.Get(context.Background(), service.ImageTaskOwner(owner), task.ID)
			require.NoError(t, err)
			require.Equal(t, service.ImageTaskStatusFailed, record.Status)
			require.ErrorIs(t, workerCtx.Err(), context.Canceled, "all worker exit paths finish their execution context")
			_, nextErr := unfinished.Reserve(context.Background(), service.GatewayTaskOwner{UserID: owner.UserID, APIKeyID: 10}, service.GatewayTaskKindBatchImage)
			if scenario == "execution_unknown_failure" {
				require.Equal(t, 1, executed)
				require.Equal(t, 1, repo.markCalls)
				require.Empty(t, repo.releaseReasons)
				require.ErrorIs(t, nextErr, service.ErrGatewayUnfinishedTaskLimit, "a local execution failure cannot free provider uncertainty")
				require.Equal(t, http.StatusBadGateway, record.HTTPStatus)
			} else {
				require.Zero(t, executed, "pre-execution rejection must not contact the provider")
				require.Equal(t, []string{"worker_not_started"}, repo.releaseReasons)
				require.NoError(t, nextErr, "another key can use capacity once the worker is proved not started")
				require.Equal(t, http.StatusServiceUnavailable, record.HTTPStatus)
			}
			if scenario == "uncertain_write_error" || scenario == "execution_unknown_failure" || scenario == "worker_acquire_error" {
				require.Equal(t, int32(1), atomic.LoadInt32(&slots.releaseUserCalled), "an acquired or uncertain attempted worker owner must be reclaimed")
			} else {
				require.Zero(t, atomic.LoadInt32(&slots.releaseUserCalled), "a rejected worker cannot release another execution's lease")
			}
		})
	}
}

func TestAsyncImageUnfinishedCapacityRequiresProviderCompletionAcrossKeys(t *testing.T) {
	for _, scenario := range []string{"success", "transport502", "timeout", "panic", "missing_output", "storage_failure"} {
		t.Run(scenario, func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			h := &AsyncImageHandler{tasks: service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute), openAI: newGatewayAdmissionForTest()}
			storage := &asyncImageCapacityFailStorage{}
			if scenario == "storage_failure" {
				h.tasks = service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(storage, "test/", false, 0, nil), time.Hour, time.Minute)
			}
			history := &asyncImageMemoryHistory{}
			h.tasks.SetHistoryRepository(history)
			repo := &asyncImageCapacityObserver{}
			cfg := &config.Config{}
			cfg.Gateway.MaxUnfinishedTasksPerUser = 1
			h.openAI.unfinishedTasks = service.NewGatewayUnfinishedTaskService(repo, cfg)
			started := make(chan struct{}, 2)
			var executed atomic.Int32
			finish := make(chan struct{})
			var finishOnce sync.Once
			complete := func() { finishOnce.Do(func() { close(finish) }) }
			t.Cleanup(complete)
			h.execute = func(_ string, c *gin.Context) {
				executed.Add(1)
				started <- struct{}{}
				<-finish
				switch scenario {
				case "transport502":
					c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "isolated failure"}})
				case "timeout":
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				case "panic":
					panic("isolated worker panic")
				case "missing_output":
					c.JSON(http.StatusOK, gin.H{"data": []gin.H{}})
				case "storage_failure":
					c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"b64_json": base64.StdEncoding.EncodeToString(asyncImageTestPNG)}}})
				default:
					c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://example.test/image.png"}}})
				}
			}
			first, accepted := asyncImageCapacityContext(7, 9)
			h.Submit(first)
			require.Equal(t, http.StatusAccepted, accepted.Code, accepted.Body.String())
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("accepted task worker did not start")
			}
			otherKey, denied := asyncImageCapacityContext(7, 10)
			h.Submit(otherKey)
			require.Equal(t, http.StatusTooManyRequests, denied.Code, "the same user cannot evade the unfinished task cap with another key")
			require.Contains(t, denied.Body.String(), "UNFINISHED_TASK_LIMIT")
			require.Equal(t, int32(1), executed.Load(), "capacity denial cannot start another worker")
			store.mu.RLock()
			require.Len(t, store.tasks, 1)
			store.mu.RUnlock()
			require.Zero(t, repo.releaseCount(), "HTTP 202 must not release a still-running background task")
			complete()
			var created struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &created))
			require.Eventually(t, func() bool {
				task, err := h.tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, created.ID)
				persisted, historyErr := history.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, created.ID)
				return err == nil && historyErr == nil && task.Status != service.ImageTaskStatusProcessing && persisted.Status == task.Status
			}, time.Second, time.Millisecond)
			record, err := h.tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, created.ID)
			require.NoError(t, err)
			if scenario == "success" {
				require.Equal(t, service.ImageTaskStatusCompleted, record.Status)
			} else {
				require.Equal(t, service.ImageTaskStatusFailed, record.Status)
			}
			providerCompleted := scenario == "success" || scenario == "storage_failure"
			if !providerCompleted {
				// A local failure cannot establish whether an accepted provider
				// operation stopped. Neither polling nor deleting local history
				// may turn that uncertainty into new upstream capacity.
				require.Zero(t, repo.releaseCount())
				poll, polled := asyncImageCapacityContext(7, 9)
				poll.Request = httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+created.ID, nil)
				poll.Params = gin.Params{{Key: "task_id", Value: created.ID}}
				h.Get(poll)
				require.Equal(t, http.StatusOK, polled.Code, polled.Body.String())
				require.Contains(t, polled.Body.String(), `"status":"failed"`)
				deletedContext, deleted := asyncImageCapacityContext(7, 9)
				deletedContext.Request = httptest.NewRequest(http.MethodDelete, "/v1/images/tasks/"+created.ID, nil)
				deletedContext.Params = gin.Params{{Key: "task_id", Value: created.ID}}
				h.Delete(deletedContext)
				// Gin normally flushes a status-only response when ServeHTTP
				// completes. This direct handler call must perform that step.
				deletedContext.Writer.WriteHeaderNow()
				require.Equal(t, http.StatusNoContent, deleted.Code, deleted.Body.String())
				_, removedErr := h.tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, created.ID)
				require.ErrorIs(t, removedErr, service.ErrImageTaskNotFound)
				_, historyErr := history.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, created.ID)
				require.ErrorIs(t, historyErr, service.ErrImageTaskNotFound)
				require.Zero(t, repo.releaseCount(), "local failed task management cannot release provider uncertainty")
				next, stillDenied := asyncImageCapacityContext(7, 10)
				h.Submit(next)
				require.Equal(t, http.StatusTooManyRequests, stillDenied.Code)
				require.Contains(t, stillDenied.Body.String(), "UNFINISHED_TASK_LIMIT")
				require.Equal(t, int32(1), executed.Load())
				return
			}
			require.Eventually(t, func() bool { return repo.releaseCount() == 1 }, time.Second, time.Millisecond)
			if scenario == "storage_failure" {
				require.Equal(t, int32(1), storage.calls.Load(), "the provider returned a real image and object storage failed afterwards")
				require.Contains(t, string(record.Error), "failed to store generated image")
			}
			next, resumed := asyncImageCapacityContext(7, 10)
			h.Submit(next)
			require.Equal(t, http.StatusAccepted, resumed.Code, "observed image output frees provider capacity even if local result storage failed")
			require.Eventually(t, func() bool { return repo.releaseCount() == 2 }, time.Second, time.Millisecond)
			require.Equal(t, int32(2), executed.Load())
			require.NoError(t, json.Unmarshal(resumed.Body.Bytes(), &created))
			require.Eventually(t, func() bool {
				task, err := h.tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
				return err == nil && task.Status != service.ImageTaskStatusProcessing
			}, time.Second, time.Millisecond)
		})
	}
}

type asyncImageCapacityFailStorage struct{ calls atomic.Int32 }

func (s *asyncImageCapacityFailStorage) Save(context.Context, string, string, []byte) (string, error) {
	s.calls.Add(1)
	return "", errors.New("isolated object storage failure")
}

func asyncImageCapacityContext(userID, keyID int64) (*gin.Context, *httptest.ResponseRecorder) {
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"safe"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	groupID := int64(3)
	c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: keyID, UserID: userID, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true}})
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID, Concurrency: 3})
	return c, response
}
