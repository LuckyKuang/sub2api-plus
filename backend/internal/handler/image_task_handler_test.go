package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type asyncImageMemoryStore struct {
	mu    sync.RWMutex
	tasks map[string]*service.ImageTaskRecord
}

type asyncImageDownloadStorage struct {
	objects map[string][]byte
	openErr error
}

type asyncImageOpsCaptureRepo struct {
	service.OpsRepository
	entry *service.OpsInsertErrorLogInput
}

func (r *asyncImageOpsCaptureRepo) InsertErrorLog(_ context.Context, entry *service.OpsInsertErrorLogInput) (int64, error) {
	r.entry = entry
	return 1, nil
}

func (r *asyncImageOpsCaptureRepo) BatchInsertErrorLogs(ctx context.Context, entries []*service.OpsInsertErrorLogInput) (int64, error) {
	var inserted int64
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		if _, err := r.InsertErrorLog(ctx, entry); err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, nil
}

func (s *asyncImageDownloadStorage) Save(_ context.Context, key, _ string, data []byte) (string, error) {
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = append([]byte(nil), data...)
	return "https://cdn.example.test/" + key, nil
}

func (s *asyncImageDownloadStorage) Open(_ context.Context, key string) (*service.ImageStorageObject, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, service.ErrImageTaskNotFound
	}
	return &service.ImageStorageObject{
		Body:        io.NopCloser(bytes.NewReader(data)),
		ContentType: "image/png",
		Size:        int64(len(data)),
	}, nil
}

var asyncImageTestPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0xf0,
	0x1f, 0x00, 0x05, 0x80, 0x02, 0x3f, 0x49, 0xc2,
	0xfb, 0x58, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

type asyncImageMemoryHistory struct {
	mu    sync.RWMutex
	tasks map[string]*service.ImageTaskRecord
	order []string
}

func (h *asyncImageMemoryHistory) Save(_ context.Context, task *service.ImageTaskRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tasks == nil {
		h.tasks = make(map[string]*service.ImageTaskRecord)
	}
	if _, exists := h.tasks[task.ID]; !exists {
		h.order = append(h.order, task.ID)
	}
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	h.tasks[task.ID] = &copy
	return nil
}

func (h *asyncImageMemoryHistory) List(_ context.Context, owner service.ImageTaskOwner, filter service.ImageTaskHistoryFilter) ([]*service.ImageTaskRecord, bool, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if filter.Limit <= 0 {
		filter.Limit = 20
	}
	matched := make([]*service.ImageTaskRecord, 0, len(h.order))
	for i := len(h.order) - 1; i >= 0; i-- {
		task := h.tasks[h.order[i]]
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

func (s *asyncImageMemoryStore) Save(_ context.Context, task *service.ImageTaskRecord, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	s.tasks[task.ID] = &copy
	return nil
}

func (s *asyncImageMemoryStore) Get(_ context.Context, id string) (*service.ImageTaskRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task := s.tasks[id]
	if task == nil {
		return nil, service.ErrImageTaskNotFound
	}
	copy := *task
	copy.Result = append(json.RawMessage(nil), task.Result...)
	copy.Error = append(json.RawMessage(nil), task.Error...)
	return &copy, nil
}

func TestAsyncImageHandlerSubmitAndPoll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	tasks.SetHistoryRepository(&asyncImageMemoryHistory{})
	release := make(chan struct{})
	h := &AsyncImageHandler{tasks: tasks}
	h.execute = func(_ string, c *gin.Context) {
		<-release
		c.JSON(http.StatusOK, gin.H{"created": 123, "data": []gin.H{{"url": "https://example.test/image.png"}}})
	}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:      9,
			UserID:  7,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks", h.List)
	router.GET("/v1/images/tasks/:task_id", h.Get)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`)).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.Equal(t, "3", w.Header().Get("Retry-After"))

	var accepted struct {
		TaskID  string `json:"task_id"`
		Status  string `json:"status"`
		PollURL string `json:"poll_url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	require.Equal(t, service.ImageTaskStatusProcessing, accepted.Status)
	require.Equal(t, "/v1/images/tasks/"+accepted.TaskID, accepted.PollURL)
	require.Equal(t, accepted.PollURL, w.Header().Get("Location"))

	// The detached background request must survive completion of/cancellation
	// from the short submission request.
	cancelRequest()
	close(release)
	require.Eventually(t, func() bool {
		got, err := tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, accepted.TaskID)
		return err == nil && got.Status == service.ImageTaskStatusCompleted
	}, time.Second, 10*time.Millisecond)

	pollReq := httptest.NewRequest(http.MethodGet, accepted.PollURL, nil)
	pollWriter := httptest.NewRecorder()
	router.ServeHTTP(pollWriter, pollReq)
	require.Equal(t, http.StatusOK, pollWriter.Code)
	require.Equal(t, "no-store", pollWriter.Header().Get("Cache-Control"))
	require.Empty(t, pollWriter.Header().Get("Retry-After"))
	require.Contains(t, pollWriter.Body.String(), "https://example.test/image.png")

	listReq := httptest.NewRequest(http.MethodGet, "/v1/images/tasks?status=completed&limit=20", nil)
	listWriter := httptest.NewRecorder()
	router.ServeHTTP(listWriter, listReq)
	require.Equal(t, http.StatusOK, listWriter.Code)
	require.Equal(t, "no-store", listWriter.Header().Get("Cache-Control"))
	var list struct {
		Object  string              `json:"object"`
		Data    []service.ImageTask `json:"data"`
		HasMore bool                `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal(listWriter.Body.Bytes(), &list))
	require.Equal(t, "list", list.Object)
	require.False(t, list.HasMore)
	require.Len(t, list.Data, 1)
	require.Equal(t, accepted.TaskID, list.Data[0].TaskID)
	require.Equal(t, service.ImageTaskStatusCompleted, list.Data[0].Status)
}

func TestAsyncImageHandlerSubmitEditPreservesMultipartRequestAndTaskType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	var executedPath string
	var executedContentType string
	var executedBody []byte
	h := &AsyncImageHandler{tasks: tasks}
	h.execute = func(_ string, c *gin.Context) {
		executedPath = c.Request.URL.Path
		executedContentType = c.GetHeader("Content-Type")
		executedBody, _ = io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"created": 123, "data": []gin.H{{"url": "https://example.test/edited.png"}}})
	}

	router := gin.New()
	router.Use(asyncImageTestAPIKeyMiddleware)
	router.POST("/v1/images/edits/async", h.Submit)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "replace the background"))
	require.NoError(t, writer.WriteField("n", "2"))
	image, err := writer.CreateFormFile("image[]", "source.png")
	require.NoError(t, err)
	_, err = image.Write(asyncImageTestPNG)
	require.NoError(t, err)
	mask, err := writer.CreateFormFile("mask", "mask.png")
	require.NoError(t, err)
	_, err = mask.Write(asyncImageTestPNG)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits/async", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusAccepted, recorder.Code)

	var accepted struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &accepted))
	require.Eventually(t, func() bool {
		task, getErr := tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, accepted.TaskID)
		return getErr == nil && task.Status == service.ImageTaskStatusCompleted
	}, time.Second, 10*time.Millisecond)

	task, err := tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, accepted.TaskID)
	require.NoError(t, err)
	require.Equal(t, "edit", task.RequestType)
	require.Equal(t, "/v1/images/edits", executedPath)
	require.Contains(t, executedContentType, "multipart/form-data")
	require.Contains(t, string(executedBody), `name="image[]"`)
	require.Contains(t, string(executedBody), `name="mask"`)
}

func TestAsyncImageHandlerFailedTaskEnqueuesOneSanitizedCorrelatedOpsError(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 2)
	t.Cleanup(func() { resetOpsErrorLoggerStateForTest(t) })
	gin.SetMode(gin.TestMode)

	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	opsRepo := &asyncImageOpsCaptureRepo{}
	h := &AsyncImageHandler{
		tasks: tasks,
		ops:   service.NewOpsService(opsRepo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}
	h.execute = func(_ string, c *gin.Context) {
		upstreamStatus := http.StatusServiceUnavailable
		upstreamMessage := "upstream rejected request: https://provider.example/v1/images?access_token=secret-value"
		upstreamDetail := `{"authorization":"Bearer top-secret","message":"provider unavailable"}`
		c.Set(service.OpsUpstreamStatusCodeKey, upstreamStatus)
		c.Set(service.OpsUpstreamErrorMessageKey, upstreamMessage)
		c.Set(service.OpsUpstreamErrorDetailKey, upstreamDetail)
		c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{
			Platform:           service.PlatformOpenAI,
			AccountID:          22,
			UpstreamStatusCode: upstreamStatus,
			Message:            upstreamMessage,
			Detail:             upstreamDetail,
		}})
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{
			"type":    "upstream_error",
			"message": upstreamMessage,
		}})
	}

	router := gin.New()
	router.Use(asyncImageTestAPIKeyMiddleware)
	router.POST("/v1/images/edits/async", h.Submit)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits/async", strings.NewReader(`{"model":"gpt-image-2","prompt":"replace background"}`))
	req.Header.Set("Content-Type", "application/json")
	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, req)
	require.Equal(t, http.StatusAccepted, writer.Code)

	var accepted struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(writer.Body.Bytes(), &accepted))

	var job opsErrorLogJob
	select {
	case job = <-opsErrorLogQueue:
	case <-time.After(time.Second):
		t.Fatal("expected failed async image task to enqueue an Ops error")
	}
	require.NotNil(t, job.entry)
	entry := job.entry
	require.Equal(t, accepted.TaskID, entry.AsyncTaskID)
	require.Equal(t, "upstream", entry.ErrorPhase)
	require.Equal(t, "upstream_error", entry.ErrorType)
	require.Equal(t, http.StatusBadGateway, entry.StatusCode)
	require.NotNil(t, entry.AccountID)
	require.EqualValues(t, 22, *entry.AccountID)
	require.Equal(t, EndpointImagesEdits, entry.InboundEndpoint)
	require.NotContains(t, entry.ErrorBody, "secret-value")
	require.NotContains(t, entry.ErrorBody, "top-secret")
	require.NotNil(t, entry.UpstreamErrorsJSON)
	require.NotContains(t, *entry.UpstreamErrorsJSON, "secret-value")
	require.NotContains(t, *entry.UpstreamErrorsJSON, "top-secret")
	require.NoError(t, job.ops.RecordError(context.Background(), entry))
	require.NotNil(t, opsRepo.entry)
	require.Equal(t, accepted.TaskID, opsRepo.entry.AsyncTaskID)
	require.NotNil(t, opsRepo.entry.UpstreamErrorMessage)
	require.NotContains(t, *opsRepo.entry.UpstreamErrorMessage, "secret-value")
	require.NotNil(t, opsRepo.entry.UpstreamErrorDetail)
	require.NotContains(t, *opsRepo.entry.UpstreamErrorDetail, "top-secret")

	task, err := tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, accepted.TaskID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, task.Status)
}

func TestAsyncImageHandlerRoutingFailureDoesNotInventUpstreamStatus(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 1)
	t.Cleanup(func() { resetOpsErrorLoggerStateForTest(t) })
	gin.SetMode(gin.TestMode)

	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	h := &AsyncImageHandler{
		tasks: tasks,
		ops:   service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}
	h.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{
			"type":    "api_error",
			"message": "No available compatible accounts",
		}})
	}

	router := gin.New()
	router.Use(asyncImageTestAPIKeyMiddleware)
	router.POST("/v1/images/generations/async", h.Submit)
	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-2","prompt":"cat"}`)))
	require.Equal(t, http.StatusAccepted, writer.Code)

	select {
	case job := <-opsErrorLogQueue:
		require.Equal(t, "routing", job.entry.ErrorPhase)
		require.Nil(t, job.entry.UpstreamStatusCode)
		require.Equal(t, "No available compatible accounts", job.entry.ErrorMessage)
	case <-time.After(time.Second):
		t.Fatal("expected routing failure to enqueue an Ops error")
	}
}

func TestAsyncImageHandlerSuccessfulTaskDoesNotEnqueueOpsError(t *testing.T) {
	setupOpsErrorLogTestQueue(t, 1)
	t.Cleanup(func() { resetOpsErrorLoggerStateForTest(t) })
	gin.SetMode(gin.TestMode)

	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	h := &AsyncImageHandler{
		tasks: tasks,
		ops:   service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}
	h.execute = func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"created": 123, "data": []gin.H{{"url": "https://example.test/image.png"}}})
	}

	router := gin.New()
	router.Use(asyncImageTestAPIKeyMiddleware)
	router.POST("/v1/images/generations/async", h.Submit)
	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-2","prompt":"cat"}`)))
	require.Equal(t, http.StatusAccepted, writer.Code)

	var accepted struct {
		TaskID string `json:"task_id"`
	}
	require.NoError(t, json.Unmarshal(writer.Body.Bytes(), &accepted))
	require.Eventually(t, func() bool {
		task, err := tasks.Get(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9}, accepted.TaskID)
		return err == nil && task.Status == service.ImageTaskStatusCompleted
	}, time.Second, 10*time.Millisecond)

	select {
	case job := <-opsErrorLogQueue:
		t.Fatalf("successful async image task unexpectedly enqueued Ops error: %+v", job.entry)
	default:
	}
}

func TestAsyncImageHandlerDownloadReturnsCompletedZIP(t *testing.T) {
	storage := &asyncImageDownloadStorage{}
	tasks, task := completedAsyncImageDownloadTask(t, storage)
	h := &AsyncImageHandler{tasks: tasks}
	router := gin.New()
	router.Use(asyncImageTestAPIKeyMiddleware)
	router.GET("/v1/images/tasks/:task_id/download", h.Download)

	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+task.TaskID+"/download", nil))

	require.Equal(t, http.StatusOK, writer.Code)
	require.Equal(t, "application/zip", writer.Header().Get("Content-Type"))
	require.Contains(t, writer.Header().Get("Content-Disposition"), task.TaskID+".zip")
	reader, err := zip.NewReader(bytes.NewReader(writer.Body.Bytes()), int64(writer.Body.Len()))
	require.NoError(t, err)
	require.Len(t, reader.File, 1)
	require.Equal(t, "image-1.png", reader.File[0].Name)
}

func TestAsyncImageHandlerDownloadReturnsJSONWhenObjectStorageReadFails(t *testing.T) {
	storage := &asyncImageDownloadStorage{}
	tasks, task := completedAsyncImageDownloadTask(t, storage)
	storage.openErr = errors.New("object storage read denied")
	h := &AsyncImageHandler{tasks: tasks}
	router := gin.New()
	router.Use(asyncImageTestAPIKeyMiddleware)
	router.GET("/v1/images/tasks/:task_id/download", h.Download)

	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+task.TaskID+"/download", nil))

	require.Equal(t, http.StatusServiceUnavailable, writer.Code)
	require.Contains(t, writer.Header().Get("Content-Type"), "application/json")
	require.Empty(t, writer.Header().Get("Content-Disposition"))
	require.Contains(t, writer.Body.String(), "IMAGE_TASK_DOWNLOAD_UNAVAILABLE")
	require.NotEqual(t, "PK", writer.Body.String()[:2])
}

func completedAsyncImageDownloadTask(t *testing.T, storage *asyncImageDownloadStorage) (*service.ImageTaskService, *service.ImageTask) {
	t.Helper()
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithUploader(store, service.NewImageResultUploader(storage, "images/", 0, nil), time.Hour, time.Minute)
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task, err := tasks.Create(context.Background(), owner)
	require.NoError(t, err)
	result := []byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(asyncImageTestPNG) + `"}]}`)
	require.NoError(t, tasks.Complete(context.Background(), task.ID, http.StatusOK, result))
	return tasks, task
}

func TestValidateAsyncImageEditUploadLimits(t *testing.T) {
	makeMultipart := func(t *testing.T, files int, fileData []byte) (string, []byte) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for index := 0; index < files; index++ {
			part, err := writer.CreateFormFile("image[]", "source.png")
			require.NoError(t, err)
			_, err = part.Write(fileData)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		return writer.FormDataContentType(), body.Bytes()
	}

	contentType, body := makeMultipart(t, asyncImageEditMaxInputImages+1, asyncImageTestPNG)
	err := validateAsyncImageEditUploadLimits("/v1/images/edits/async", contentType, body)
	require.ErrorIs(t, err, errAsyncImageEditTooManyInputImages)

	contentType, body = makeMultipart(t, 1, bytes.Repeat([]byte("a"), int(service.OpenAIImageMaxUploadPartBytes)+1))
	err = validateAsyncImageEditUploadLimits("/v1/images/edits/async", contentType, body)
	var tooLarge *service.OpenAIImageUploadTooLargeError
	require.True(t, errors.As(err, &tooLarge))
	require.Equal(t, service.OpenAIImageMaxUploadPartBytes, tooLarge.Limit)

	var mismatchedMask bytes.Buffer
	writer := multipart.NewWriter(&mismatchedMask)
	input, createErr := writer.CreateFormFile("image[]", "source.png")
	require.NoError(t, createErr)
	_, createErr = input.Write(asyncImageTestPNG)
	require.NoError(t, createErr)
	mask, createErr := writer.CreateFormFile("mask", "mask.png")
	require.NoError(t, createErr)
	_, createErr = mask.Write(asyncImageTestPNGWithDimensions(t, 2, 1))
	require.NoError(t, createErr)
	require.NoError(t, writer.Close())
	err = validateAsyncImageEditUploadLimits("/v1/images/edits/async", writer.FormDataContentType(), mismatchedMask.Bytes())
	require.ErrorIs(t, err, errAsyncImageEditMaskSizeMismatch)
}

func asyncImageTestPNGWithDimensions(t *testing.T, width, height int) []byte {
	t.Helper()
	var body bytes.Buffer
	require.NoError(t, png.Encode(&body, image.NewRGBA(image.Rect(0, 0, width, height))))
	return body.Bytes()
}

func asyncImageTestAPIKeyMiddleware(c *gin.Context) {
	groupID := int64(3)
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      9,
		UserID:  7,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
	})
	c.Next()
}

// When object storage is not configured the feature is fully disabled: the
// endpoints must return 404 without creating a task or writing to Redis.
func TestAsyncImageHandlerDisabledReturns404(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	tasks := service.NewImageTaskServiceWithOptions(store, time.Hour, time.Minute) // enabled == false
	h := &AsyncImageHandler{tasks: tasks}

	router := gin.New()
	router.Use(func(c *gin.Context) {
		groupID := int64(3)
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:      9,
			UserID:  7,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
		})
		c.Next()
	})
	router.POST("/v1/images/generations/async", h.Submit)
	router.GET("/v1/images/tasks/:task_id", h.Get)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"gpt-image-1","prompt":"cat"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "not enabled")

	pollReq := httptest.NewRequest(http.MethodGet, "/v1/images/tasks/imgtask_missing", nil)
	pollWriter := httptest.NewRecorder()
	router.ServeHTTP(pollWriter, pollReq)
	require.Equal(t, http.StatusNotFound, pollWriter.Code)

	// No task was created / persisted.
	require.Empty(t, store.tasks)
}
