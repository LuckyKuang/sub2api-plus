package service

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ImageTaskStatusProcessing = "processing"
	ImageTaskStatusCompleted  = "completed"
	ImageTaskStatusFailed     = "failed"

	defaultImageTaskTTL                    = 24 * time.Hour
	defaultImageTaskExecutionTimeout       = 30 * time.Minute
	maxImageTaskZipImages                  = 16
	maxImageTaskZipBytes             int64 = 128 << 20 // 128 MiB
	maxImageTaskZipDuration                = 2 * time.Minute
)

var (
	ErrImageTaskNotFound         = infraerrors.New(http.StatusNotFound, "IMAGE_TASK_NOT_FOUND", "image task not found")
	ErrImageTaskForbidden        = infraerrors.New(http.StatusForbidden, "IMAGE_TASK_FORBIDDEN", "image task does not belong to this API key")
	ErrImageTaskUnavailable      = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_UNAVAILABLE", "image task storage is unavailable")
	ErrImageTaskDeleteNotAllowed = infraerrors.New(http.StatusConflict, "IMAGE_TASK_DELETE_NOT_ALLOWED", "only failed image tasks can be deleted")
	ErrImageTaskNoImages         = infraerrors.New(http.StatusConflict, "IMAGE_TASK_NO_IMAGES", "image task has no generated images to download")
	ErrImageTaskDownload         = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_DOWNLOAD_UNAVAILABLE", "generated image download is unavailable")
	ErrImageTaskZipTooLarge      = infraerrors.New(http.StatusRequestEntityTooLarge, "IMAGE_TASK_DOWNLOAD_TOO_LARGE", "generated images exceed the download size limit")
)

// ImageTaskRecord is the private Redis representation of an asynchronous image
// request. Ownership fields are intentionally omitted from the public view.
type ImageTaskRecord struct {
	ID              string          `json:"id"`
	UserID          int64           `json:"user_id"`
	APIKeyID        int64           `json:"api_key_id"`
	RequestType     string          `json:"request_type,omitempty"`
	Model           string          `json:"model,omitempty"`
	PromptPreview   string          `json:"prompt_preview,omitempty"`
	RequestedImages int             `json:"requested_images"`
	ActualImages    int             `json:"actual_images"`
	Status          string          `json:"status"`
	HTTPStatus      int             `json:"http_status,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	StorageKeys     []string        `json:"storage_keys,omitempty"`
	Error           json.RawMessage `json:"error,omitempty"`
	CreatedAt       int64           `json:"created_at"`
	CompletedAt     *int64          `json:"completed_at,omitempty"`
	ExpiresAt       int64           `json:"expires_at"`
}

// ImageTask is the API-safe task representation returned to callers.
type ImageTask struct {
	ID              string          `json:"id"`
	TaskID          string          `json:"task_id"`
	Object          string          `json:"object"`
	RequestType     string          `json:"request_type,omitempty"`
	Model           string          `json:"model,omitempty"`
	PromptPreview   string          `json:"prompt_preview,omitempty"`
	RequestedImages int             `json:"requested_images"`
	ActualImages    int             `json:"actual_images"`
	Status          string          `json:"status"`
	HTTPStatus      int             `json:"http_status,omitempty"`
	ImageURL        string          `json:"image_url,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           json.RawMessage `json:"error,omitempty"`
	CreatedAt       int64           `json:"created_at"`
	CompletedAt     *int64          `json:"completed_at,omitempty"`
	ExpiresAt       int64           `json:"expires_at"`
}

// AdminImageTask is the credential-free durable task read model used by the
// administrator support view. APIKeyID is included for diagnosis, but no API
// key value or mutable execution-store field is exposed.
type AdminImageTask struct {
	ID              string          `json:"id"`
	TaskID          string          `json:"task_id"`
	Object          string          `json:"object"`
	APIKeyID        int64           `json:"api_key_id"`
	RequestType     string          `json:"request_type,omitempty"`
	Model           string          `json:"model,omitempty"`
	PromptPreview   string          `json:"prompt_preview,omitempty"`
	RequestedImages int             `json:"requested_images"`
	ActualImages    int             `json:"actual_images"`
	Status          string          `json:"status"`
	HTTPStatus      int             `json:"http_status,omitempty"`
	ImageURL        string          `json:"image_url,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Error           json.RawMessage `json:"error,omitempty"`
	CreatedAt       int64           `json:"created_at"`
	CompletedAt     *int64          `json:"completed_at,omitempty"`
	ExpiresAt       int64           `json:"expires_at"`
}

type AdminImageTaskListResponse struct {
	Items   []*AdminImageTask `json:"items"`
	HasMore bool              `json:"has_more"`
}

type ImageTaskOwner struct {
	UserID   int64
	APIKeyID int64
}

type ImageTaskStore interface {
	Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error
	Get(ctx context.Context, id string) (*ImageTaskRecord, error)
	DeleteIfStatus(ctx context.Context, id, status string) (deleted bool, exists bool, err error)
}

// ImageStorageResolver reports the currently effective object-storage binding
// and whether it accepts new submissions. A non-nil uploader can remain usable
// for existing tasks while enabled is false.
type ImageStorageResolver func() (uploader *ImageResultUploader, enabled bool)

type ImageTaskService struct {
	store            ImageTaskStore
	uploader         *ImageResultUploader
	enabled          bool
	resolve          ImageStorageResolver
	history          ImageTaskHistoryRepository
	ttl              time.Duration
	executionTimeout time.Duration
}

// SetHistoryRepository enables durable list records. It is set once during
// application wiring; the Redis task store remains the execution-state source.
func (s *ImageTaskService) SetHistoryRepository(history ImageTaskHistoryRepository) {
	if s == nil {
		return
	}
	s.history = history
}

func NewImageTaskService(store ImageTaskStore) *ImageTaskService {
	return NewImageTaskServiceWithOptions(store, defaultImageTaskTTL, defaultImageTaskExecutionTimeout)
}

func NewImageTaskServiceWithOptions(store ImageTaskStore, ttl, executionTimeout time.Duration) *ImageTaskService {
	if ttl <= 0 {
		ttl = defaultImageTaskTTL
	}
	if executionTimeout <= 0 {
		executionTimeout = defaultImageTaskExecutionTimeout
	}
	return &ImageTaskService{store: store, ttl: ttl, executionTimeout: executionTimeout}
}

// NewImageTaskServiceWithUploader 构造一个已启用的图片任务服务：结果会先经 uploader
// 转存到对象存储再落 Redis。uploader 为 nil 时不做转存（仅用于测试）。
func NewImageTaskServiceWithUploader(store ImageTaskStore, uploader *ImageResultUploader, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.uploader = uploader
	s.enabled = true
	return s
}

// NewImageTaskServiceWithResolver 构造一个由 resolver 决定启用状态的服务：
// 开关与凭证来自后台设置，保存后立即生效，无需重启。
func NewImageTaskServiceWithResolver(store ImageTaskStore, resolve ImageStorageResolver, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.resolve = resolve
	return s
}

// current 返回当前生效的 uploader 与启用状态。
// 注入了 resolver 时以 resolver 为准（后台设置可热切换），否则回落到构造时固定的值。
func (s *ImageTaskService) current() (*ImageResultUploader, bool) {
	if s == nil {
		return nil, false
	}
	if s.resolve != nil {
		return s.resolve()
	}
	return s.uploader, s.enabled
}

// Enabled 表示异步图片任务功能是否可用（总开关 + 凭证齐全）。
// 关闭时 handler 直接返回 404，不创建任务、不写 Redis。
func (s *ImageTaskService) Enabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	_, enabled := s.current()
	return enabled
}

// Pollable 表示已创建的任务能否被查询。
// 比 Enabled 弱：只要 store 可用即可，从而在功能被关掉后仍能取回进行中的任务结果。
func (s *ImageTaskService) Pollable() bool {
	return s != nil && s.store != nil
}

func (s *ImageTaskService) ExecutionTimeout() time.Duration {
	if s == nil || s.executionTimeout <= 0 {
		return defaultImageTaskExecutionTimeout
	}
	return s.executionTimeout
}

func (s *ImageTaskService) Create(ctx context.Context, owner ImageTaskOwner) (*ImageTask, error) {
	return s.CreateWithMetadata(ctx, owner, ImageTaskMetadata{})
}

// CreateWithMetadata persists a compact, user-visible task index before the
// caller starts upstream work. The raw request body is intentionally excluded.
func (s *ImageTaskService) CreateWithMetadata(ctx context.Context, owner ImageTaskOwner, metadata ImageTaskMetadata) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	metadata = normalizeImageTaskMetadata(metadata)
	now := time.Now().UTC()
	task := &ImageTaskRecord{
		ID:              "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:          owner.UserID,
		APIKeyID:        owner.APIKeyID,
		RequestType:     metadata.RequestType,
		Model:           metadata.Model,
		PromptPreview:   metadata.PromptPreview,
		RequestedImages: metadata.RequestedImages,
		Status:          ImageTaskStatusProcessing,
		CreatedAt:       now.Unix(),
		ExpiresAt:       now.Add(s.ttl).Unix(),
	}
	if s.history != nil {
		if err := s.history.Save(ctx, task); err != nil {
			logger.L().Error("image_task.history_create_failed", zap.String("task_id", task.ID), zap.Error(err))
			return nil, ErrImageTaskUnavailable.WithCause(err)
		}
	}
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		if s.history != nil {
			task.Status = ImageTaskStatusFailed
			task.HTTPStatus = http.StatusServiceUnavailable
			task.Error = imageTaskErrorJSON("api_error", "image task storage is unavailable")
			completedAt := time.Now().UTC().Unix()
			task.CompletedAt = &completedAt
			if historyErr := s.history.Save(context.Background(), task); historyErr != nil {
				logger.L().Error("image_task.history_mark_storage_failure_failed", zap.String("task_id", task.ID), zap.Error(historyErr))
			}
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) List(ctx context.Context, owner ImageTaskOwner, filter ImageTaskHistoryFilter) (*ImageTaskListResponse, error) {
	if s == nil || s.history == nil {
		return nil, ErrImageTaskUnavailable
	}
	filter = normalizeImageTaskHistoryFilter(filter)
	records, hasMore, err := s.history.List(ctx, owner, filter)
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	data := make([]*ImageTask, 0, len(records))
	for _, record := range records {
		data = append(data, imageTaskToPublic(record))
	}
	return &ImageTaskListResponse{Object: "list", Data: data, HasMore: hasMore}, nil
}

func (s *ImageTaskService) Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		// Do not reveal whether a random task ID exists for another caller.
		return nil, ErrImageTaskNotFound
	}
	return imageTaskToPublic(task), nil
}

// ListByUser reads durable history for an administrator support view. It does
// not consult Redis and therefore cannot repair, refresh, or otherwise mutate
// target task state.
func (s *ImageTaskService) ListByUser(ctx context.Context, userID int64, filter ImageTaskHistoryFilter) (*AdminImageTaskListResponse, error) {
	if s == nil || s.history == nil {
		return nil, ErrImageTaskUnavailable
	}
	filter = normalizeImageTaskHistoryFilter(filter)
	records, hasMore, err := s.history.ListByUser(ctx, userID, filter)
	if err != nil {
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	items := make([]*AdminImageTask, 0, len(records))
	for _, record := range records {
		items = append(items, imageTaskToAdmin(record))
	}
	return &AdminImageTaskListResponse{Items: items, HasMore: hasMore}, nil
}

// GetByUser reads one durable task scoped to the selected support target.
func (s *ImageTaskService) GetByUser(ctx context.Context, userID int64, id string) (*AdminImageTask, error) {
	if s == nil || s.history == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.history.GetByUser(ctx, userID, strings.TrimSpace(id))
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	return imageTaskToAdmin(task), nil
}

// Delete removes a failed task's execution state and durable history. The
// Redis key is deleted first so an infrastructure failure leaves the history
// row visible and the operation retryable.
func (s *ImageTaskService) Delete(ctx context.Context, owner ImageTaskOwner, id string) error {
	if s == nil || s.store == nil || s.history == nil {
		return ErrImageTaskUnavailable
	}
	taskID := strings.TrimSpace(id)
	task, err := s.history.Get(ctx, owner, taskID)
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return ErrImageTaskNotFound
		}
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if task.Status != ImageTaskStatusFailed {
		return ErrImageTaskDeleteNotAllowed
	}
	deleted, exists, err := s.store.DeleteIfStatus(ctx, taskID, ImageTaskStatusFailed)
	if err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if exists && !deleted {
		return ErrImageTaskDeleteNotAllowed
	}
	deleted, err = s.history.DeleteFailed(ctx, owner, taskID)
	if err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if deleted {
		return nil
	}

	// A concurrent identical deletion is already in the desired state. If the
	// row still exists, its status changed and the failed-only rule wins.
	current, err := s.history.Get(ctx, owner, taskID)
	if errors.Is(err, ErrImageTaskNotFound) {
		return nil
	}
	if err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if current.Status != ImageTaskStatusFailed {
		return ErrImageTaskDeleteNotAllowed
	}
	return ErrImageTaskUnavailable
}

// StreamDownloadZip creates one ZIP archive for every image associated with a
// completed task. Files are opened through the configured object storage using
// exact keys recorded at upload time, never by fetching the URLs stored in the
// task response.
func (s *ImageTaskService) StreamDownloadZip(ctx context.Context, owner ImageTaskOwner, id string, writer io.Writer) (int, error) {
	if writer == nil {
		return 0, ErrImageTaskDownload
	}
	if s == nil || s.store == nil {
		return 0, ErrImageTaskUnavailable
	}
	taskID := strings.TrimSpace(id)
	task, err := s.store.Get(ctx, taskID)
	if err != nil {
		if !errors.Is(err, ErrImageTaskNotFound) {
			return 0, ErrImageTaskUnavailable.WithCause(err)
		}
		if s.history == nil {
			return 0, ErrImageTaskNotFound
		}
		task, err = s.history.Get(ctx, owner, taskID)
		if err != nil {
			if errors.Is(err, ErrImageTaskNotFound) {
				return 0, ErrImageTaskNotFound
			}
			return 0, ErrImageTaskUnavailable.WithCause(err)
		}
	}
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		return 0, ErrImageTaskNotFound
	}
	urls := imageTaskURLs(task.Result)
	if len(urls) == 0 {
		return 0, ErrImageTaskNoImages
	}
	if len(urls) > maxImageTaskZipImages {
		return 0, ErrImageTaskZipTooLarge
	}
	if len(task.StorageKeys) != len(urls) {
		return 0, ErrImageTaskDownload
	}
	uploader, _ := s.current()
	if uploader == nil {
		return 0, ErrImageTaskDownload
	}

	streamCtx, cancel := context.WithTimeout(ctx, maxImageTaskZipDuration)
	defer cancel()
	limited := &imageTaskZipLimitWriter{writer: writer, limit: maxImageTaskZipBytes}
	archive := zip.NewWriter(limited)
	var sourceBytes int64
	for index := range urls {
		object, extension, openErr := uploader.OpenStoredTaskImage(streamCtx, task.StorageKeys[index])
		if openErr != nil {
			_ = archive.Close()
			return index, ErrImageTaskDownload.WithCause(openErr)
		}
		entry, createErr := archive.Create(fmt.Sprintf("image-%d%s", index+1, extension))
		if createErr != nil {
			_ = object.Body.Close()
			_ = archive.Close()
			return index, imageTaskZipError(createErr)
		}
		remaining := maxImageTaskZipBytes - sourceBytes
		if object.Size > remaining || remaining <= 0 {
			_ = object.Body.Close()
			_ = archive.Close()
			return index, ErrImageTaskZipTooLarge
		}
		copied, copyErr := io.Copy(entry, io.LimitReader(object.Body, remaining+1))
		closeErr := object.Body.Close()
		if copyErr != nil {
			_ = archive.Close()
			return index, imageTaskZipError(copyErr)
		}
		sourceBytes += copied
		if copied > remaining {
			_ = archive.Close()
			return index, ErrImageTaskZipTooLarge
		}
		if closeErr != nil {
			_ = archive.Close()
			return index, ErrImageTaskDownload.WithCause(closeErr)
		}
		if limited.exceeded {
			_ = archive.Close()
			return index, ErrImageTaskZipTooLarge
		}
	}
	if err := archive.Close(); err != nil {
		return len(urls), imageTaskZipError(err)
	}
	return len(urls), nil
}

type imageTaskZipLimitWriter struct {
	writer   io.Writer
	limit    int64
	written  int64
	exceeded bool
}

func (w *imageTaskZipLimitWriter) Write(data []byte) (int, error) {
	if w == nil || w.writer == nil {
		return 0, io.ErrClosedPipe
	}
	if w.limit > 0 && w.written+int64(len(data)) > w.limit {
		w.exceeded = true
		return 0, ErrImageTaskZipTooLarge
	}
	n, err := w.writer.Write(data)
	w.written += int64(n)
	return n, err
}

func imageTaskZipError(err error) error {
	if errors.Is(err, ErrImageTaskZipTooLarge) {
		return ErrImageTaskZipTooLarge.WithCause(err)
	}
	return ErrImageTaskDownload.WithCause(err)
}

func (s *ImageTaskService) Complete(ctx context.Context, id string, statusCode int, result json.RawMessage) error {
	if !json.Valid(result) {
		return s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("api_error", "upstream returned a non-JSON image response"))
	}
	var storageKeys []string
	if uploader, _ := s.current(); uploader != nil {
		rewritten, keys, err := uploader.rewriteWithKeys(ctx, id, result)
		if err != nil {
			// 转存失败不回退存 base64，避免大 blob 撑爆 Redis：直接把任务标记为失败。
			logger.L().Error("image_task.offload_failed", zap.String("task_id", id), zap.Error(err))
			return s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("api_error", "failed to store generated image to object storage"))
		}
		result = rewritten
		storageKeys = keys
	}
	return s.finish(ctx, id, ImageTaskStatusCompleted, statusCode, result, nil, storageKeys)
}

func (s *ImageTaskService) Fail(ctx context.Context, id string, statusCode int, taskErr json.RawMessage) error {
	if !json.Valid(taskErr) {
		taskErr = imageTaskErrorJSON("api_error", "image generation failed")
	}
	return s.finish(ctx, id, ImageTaskStatusFailed, statusCode, nil, taskErr, nil)
}

func (s *ImageTaskService) finish(ctx context.Context, id, status string, statusCode int, result, taskErr json.RawMessage, storageKeys []string) error {
	if s == nil || s.store == nil {
		return ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return ErrImageTaskNotFound
		}
		return ErrImageTaskUnavailable.WithCause(err)
	}
	now := time.Now().UTC()
	completedAt := now.Unix()
	task.Status = status
	task.HTTPStatus = statusCode
	task.Result = result
	task.StorageKeys = append([]string(nil), storageKeys...)
	task.ActualImages = imageTaskResultCount(result)
	task.Error = taskErr
	task.CompletedAt = &completedAt
	task.ExpiresAt = now.Add(s.ttl).Unix()
	if err := s.store.Save(ctx, task, s.ttl); err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if s.history != nil {
		if historyErr := s.history.Save(ctx, task); historyErr != nil {
			logger.L().Error("image_task.history_update_failed", zap.String("task_id", id), zap.Error(historyErr))
		}
	}
	return nil
}

func imageTaskToPublic(task *ImageTaskRecord) *ImageTask {
	if task == nil {
		return nil
	}
	requestedImages := task.RequestedImages
	if requestedImages <= 0 {
		requestedImages = 1
	}
	actualImages := normalizedImageTaskActualImages(task)
	return &ImageTask{
		ID:              task.ID,
		TaskID:          task.ID,
		Object:          "image.generation.task",
		RequestType:     task.RequestType,
		Model:           task.Model,
		PromptPreview:   task.PromptPreview,
		RequestedImages: requestedImages,
		ActualImages:    actualImages,
		Status:          task.Status,
		HTTPStatus:      task.HTTPStatus,
		ImageURL:        firstImageTaskURL(task.Result),
		Result:          task.Result,
		Error:           task.Error,
		CreatedAt:       task.CreatedAt,
		CompletedAt:     task.CompletedAt,
		ExpiresAt:       task.ExpiresAt,
	}
}

func imageTaskToAdmin(task *ImageTaskRecord) *AdminImageTask {
	if task == nil {
		return nil
	}
	requestedImages := task.RequestedImages
	if requestedImages <= 0 {
		requestedImages = 1
	}
	actualImages := normalizedImageTaskActualImages(task)
	return &AdminImageTask{
		ID:              task.ID,
		TaskID:          task.ID,
		Object:          "image.generation.task",
		APIKeyID:        task.APIKeyID,
		RequestType:     task.RequestType,
		Model:           task.Model,
		PromptPreview:   task.PromptPreview,
		RequestedImages: requestedImages,
		ActualImages:    actualImages,
		Status:          task.Status,
		HTTPStatus:      task.HTTPStatus,
		ImageURL:        firstImageTaskURL(task.Result),
		Result:          task.Result,
		Error:           task.Error,
		CreatedAt:       task.CreatedAt,
		CompletedAt:     task.CompletedAt,
		ExpiresAt:       task.ExpiresAt,
	}
}

func firstImageTaskURL(result json.RawMessage) string {
	urls := imageTaskURLs(result)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

func imageTaskURLs(result json.RawMessage) []string {
	if len(result) == 0 || !json.Valid(result) {
		return nil
	}
	var response struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(result, &response) != nil || len(response.Data) == 0 {
		return nil
	}
	urls := make([]string, 0, len(response.Data))
	for _, data := range response.Data {
		if url := strings.TrimSpace(data.URL); url != "" {
			urls = append(urls, url)
		}
	}
	return urls
}

func imageTaskResultCount(result json.RawMessage) int {
	if len(result) == 0 || !json.Valid(result) {
		return 0
	}
	var response struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(result, &response) != nil {
		return 0
	}
	return len(response.Data)
}

func normalizedImageTaskActualImages(task *ImageTaskRecord) int {
	if task == nil {
		return 0
	}
	if task.ActualImages > 0 || task.Status != ImageTaskStatusCompleted {
		return task.ActualImages
	}
	return imageTaskResultCount(task.Result)
}

func imageTaskErrorJSON(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"type": errorType, "message": message})
	return data
}
