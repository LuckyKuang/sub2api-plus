//go:build unit

package handler

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type workerLeaseHTTPStorage struct {
	client *http.Client
	url    string
}
type workerLeaseContextStore struct {
	*asyncImageMemoryStore
	terminalStarted chan struct{}
	finishTerminal  <-chan struct{}
}

func (s workerLeaseContextStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.asyncImageMemoryStore.Get(ctx, id)
}
func (s workerLeaseContextStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.Status == service.ImageTaskStatusCompleted && s.terminalStarted != nil {
		close(s.terminalStarted)
		select {
		case <-s.finishTerminal:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.asyncImageMemoryStore.Save(ctx, task, ttl)
}
func (s workerLeaseHTTPStorage) Save(ctx context.Context, key, _ string, _ []byte) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, s.url, nil)
	if err != nil {
		return "", err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	_, err = io.Copy(io.Discard, response.Body)
	if err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK {
		return "", service.ErrImageTaskUnavailable
	}
	return "https://cdn.example.test/" + key, nil
}

func TestAsyncImageWorkerUserLeaseIncludesResultStorage(t *testing.T) {
	// Returning from Images must leave one global user slot occupied until the
	// actual object-storage HTTP call finishes. A lost lease must cancel that
	// call, and both success and failure release the one owner exactly once.
	for _, scenario := range []string{"success", "storage failure", "lease loss"} {
		t.Run(scenario, func(t *testing.T) {
			providerStarted, finishProvider := make(chan struct{}), make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(providerStarted)
				select {
				case <-finishProvider:
					_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+base64.StdEncoding.EncodeToString(asyncImageTestPNG)+`"}]}`)
				case <-r.Context().Done():
				}
			}))
			defer provider.Close()
			defer func() {
				select {
				case <-finishProvider:
				default:
					close(finishProvider)
				}
			}()
			storageStarted := make(chan struct{})
			storageStopped := make(chan struct{})
			finishStorage := make(chan struct{})
			storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(storageStarted)
				defer close(storageStopped)
				select {
				case <-finishStorage:
				case <-r.Context().Done():
					return
				}
				if scenario == "storage failure" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer storage.Close()
			defer func() {
				select {
				case <-finishStorage:
				default:
					close(finishStorage)
				}
			}()
			cache := &renewableHelperConcurrencyCache{ttl: 90 * time.Millisecond, helperConcurrencyCacheStub: helperConcurrencyCacheStub{userSeq: []bool{true}}}
			h := &AsyncImageHandler{openAI: newGatewayAdmissionForTest()}
			h.openAI.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			terminalStarted, finishTerminal := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-finishTerminal:
				default:
					close(finishTerminal)
				}
			}()
			store := workerLeaseContextStore{asyncImageMemoryStore: &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}, terminalStarted: terminalStarted, finishTerminal: finishTerminal}
			h.tasks = service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(workerLeaseHTTPStorage{storage.Client(), storage.URL}, "images/", false, 0, nil), time.Hour, time.Minute)
			owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
			task, err := h.tasks.Create(context.Background(), owner)
			require.NoError(t, err)
			c, recorder := asyncImageCapacityContext(owner.UserID, owner.APIKeyID)
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			gatewayReturned := make(chan struct{})
			h.execute = func(_ string, c *gin.Context) {
				release, admitted := acquireGatewayUserRequest(c, h.openAI.concurrencyHelper, h.openAI.errorResponse)
				if !admitted {
					return
				}
				defer release()
				defer close(gatewayReturned)
				request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, provider.URL, nil)
				if err != nil {
					c.Status(http.StatusBadGateway)
					return
				}
				response, err := provider.Client().Do(request)
				if err != nil {
					c.Status(http.StatusBadGateway)
					return
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					c.Status(http.StatusBadGateway)
					return
				}
				c.Data(response.StatusCode, "application/json", body)
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				h.run(task.ID, service.PlatformOpenAI, c, recorder, cancel, asyncImageOpsSnapshot{})
			}()
			select {
			case <-providerStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("nested gateway never sent its actual provider request")
			}
			cache.mu.Lock()
			require.Equal(t, 1, cache.userAcquireCalls)
			require.Equal(t, 1, cache.apiKeyTrackCalls, "nested gateway must reuse the worker's key-statistics owner")
			require.Zero(t, cache.apiKeyReleaseCalls)
			cache.mu.Unlock()
			close(finishProvider)
			select {
			case <-storageStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("image result never reached object storage")
			}
			select {
			case <-gatewayReturned:
			default:
				t.Fatal("storage must follow the gateway return")
			}
			require.Eventually(t, func() bool { cache.mu.Lock(); defer cache.mu.Unlock(); return cache.refreshCalls >= 4 }, time.Second, 5*time.Millisecond)
			cache.mu.Lock()
			acquired, released := cache.userAcquireCalls, cache.userReleaseCalls
			cache.mu.Unlock()
			require.Equal(t, 1, acquired, "worker and nested gateway must share one user owner")
			require.Zero(t, released, "uploading results remains executing user work")
			cache.mu.Lock()
			require.Equal(t, 1, cache.apiKeyTrackCalls)
			require.Zero(t, cache.apiKeyReleaseCalls, "gateway return cannot hide the worker's ongoing storage work")
			cache.mu.Unlock()
			if scenario == "lease loss" {
				cache.mu.Lock()
				cache.leaseLost = true
				cache.mu.Unlock()
			} else {
				close(finishStorage)
			}
			if scenario == "success" {
				select {
				case <-terminalStarted:
				case <-time.After(time.Second):
					t.Fatal("worker never reached terminal persistence")
				}
				cache.mu.Lock()
				require.Zero(t, cache.userReleaseCalls)
				require.Zero(t, cache.apiKeyReleaseCalls, "terminal persistence remains executing user/key work")
				cache.mu.Unlock()
				close(finishTerminal)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("worker did not finish or cancel after storage outcome")
			}
			select {
			case <-storageStopped:
			case <-time.After(time.Second):
				t.Fatal("actual storage HTTP request survived lost capacity")
			}
			cache.mu.Lock()
			released = cache.userReleaseCalls
			require.Equal(t, 1, cache.apiKeyTrackCalls)
			require.Equal(t, 1, cache.apiKeyReleaseCalls)
			cache.mu.Unlock()
			require.Equal(t, 1, released)
			result, err := h.tasks.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			if scenario == "success" {
				require.Equal(t, service.ImageTaskStatusCompleted, result.Status)
			} else {
				require.Equal(t, service.ImageTaskStatusFailed, result.Status)
			}
		})
	}
}

func TestAsyncImageWorkerUserLeaseReleasesWhenGatewayFails(t *testing.T) {
	for _, panicGateway := range []bool{false, true} {
		t.Run(map[bool]string{false: "gateway error", true: "gateway panic"}[panicGateway], func(t *testing.T) {
			cache := &renewableHelperConcurrencyCache{ttl: time.Minute, helperConcurrencyCacheStub: helperConcurrencyCacheStub{userSeq: []bool{true}}}
			h := &AsyncImageHandler{openAI: newGatewayAdmissionForTest(), tasks: service.NewImageTaskServiceWithUploader(&asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}, nil, time.Hour, time.Minute)}
			h.openAI.concurrencyHelper = NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)
			owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
			task, err := h.tasks.Create(context.Background(), owner)
			require.NoError(t, err)
			c, recorder := asyncImageCapacityContext(owner.UserID, owner.APIKeyID)
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			h.execute = func(_ string, c *gin.Context) {
				if panicGateway {
					panic("isolated gateway panic")
				}
				c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": "isolated failure"}})
			}
			h.run(task.ID, service.PlatformOpenAI, c, recorder, cancel, asyncImageOpsSnapshot{})
			cache.mu.Lock()
			released := cache.userReleaseCalls
			require.Equal(t, 1, cache.apiKeyTrackCalls)
			require.Equal(t, 1, cache.apiKeyReleaseCalls)
			cache.mu.Unlock()
			require.Equal(t, 1, released)
			result, err := h.tasks.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			require.Equal(t, service.ImageTaskStatusFailed, result.Status)
		})
	}
}
