package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	pkghttputil "github.com/LuckyKuang/sub2api-plus/internal/pkg/httputil"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ip"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	_ "golang.org/x/image/webp"
)

const (
	asyncImageEditMaxInputImages    = 4
	asyncImageEditMaxMultipartBytes = int64(40 << 20) // 40 MiB across images and mask
)

var (
	errAsyncImageEditTooManyInputImages = errors.New("async image edits support at most 4 input images")
	errAsyncImageEditTooManyMasks       = errors.New("async image edits support at most one mask image")
	errAsyncImageEditMultipartTooLarge  = errors.New("async image edit uploads exceed the allowed total size of 40 MiB")
	errAsyncImageEditInvalidImage       = errors.New("async image edits require PNG, JPEG, or WebP image files")
	errAsyncImageEditInvalidMask        = errors.New("async image edit masks must be PNG or WebP files")
	errAsyncImageEditMaskSizeMismatch   = errors.New("async image edit mask dimensions must match the first input image")
)

type AsyncImageHandler struct {
	tasks       *service.ImageTaskService
	openAI      *OpenAIGatewayHandler
	rateLimiter *service.AsyncImageRateLimiter
	ops         *service.OpsService
	execute     func(platform string, c *gin.Context)
}

// asyncImageOpsSnapshot contains only durable, non-sensitive submission
// metadata. The background execution context can outlive the HTTP response,
// so it must not depend on the original gin.Context to create a correlated
// Ops error record after a task reaches its terminal failed state.
type asyncImageOpsSnapshot struct {
	TaskID          string
	RequestID       string
	ClientRequestID string

	UserID   *int64
	APIKeyID *int64
	GroupID  *int64
	ClientIP *string

	Platform        string
	Model           string
	RequestPath     string
	InboundEndpoint string
	UserAgent       string
	APIKeyPrefix    string
	RequestType     int16
}

func NewAsyncImageHandler(tasks *service.ImageTaskService, openAI *OpenAIGatewayHandler) *AsyncImageHandler {
	h := &AsyncImageHandler{tasks: tasks, openAI: openAI}
	h.execute = h.executeWithGateway
	return h
}

// enabled reports whether the async image task feature is available. Object
// storage is the enablement gate: without it the endpoints are fully disabled
// so that large base64 results never land in Redis.
func (h *AsyncImageHandler) enabled() bool {
	return h != nil && h.tasks != nil && h.tasks.Enabled()
}

// pollable reports whether task lookups can be served. It is deliberately weaker
// than enabled(): results already written to Redis stay readable after the
// feature is switched off, so an in-flight task is never stranded.
func (h *AsyncImageHandler) pollable() bool {
	return h != nil && h.tasks != nil && h.tasks.Pollable()
}

// Submit accepts the same payload as the synchronous Images endpoint and
// returns before the upstream image generation begins.
func (h *AsyncImageHandler) Submit(c *gin.Context) {
	if !h.enabled() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	platform := ""
	if apiKey.Group != nil {
		platform = apiKey.Group.Platform
	}
	if platform != service.PlatformOpenAI && platform != service.PlatformGrok {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "Images API is not supported for this platform")
		return
	}
	if !service.GroupAllowsImageGeneration(apiKey.Group) {
		imageTaskJSONError(c, http.StatusForbidden, "permission_error", service.ImageGenerationPermissionMessage())
		return
	}
	if h == nil || h.tasks == nil || h.execute == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}

	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			imageTaskJSONError(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}
	if asyncImageRequestStreams(c.GetHeader("Content-Type"), body) {
		imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", "streaming image requests cannot be submitted as asynchronous tasks")
		return
	}
	if err := validateAsyncImageEditUploadLimits(c.Request.URL.Path, c.GetHeader("Content-Type"), body); err != nil {
		status := http.StatusBadRequest
		var uploadTooLarge *service.OpenAIImageUploadTooLargeError
		if errors.Is(err, errAsyncImageEditMultipartTooLarge) || errors.As(err, &uploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		imageTaskJSONError(c, status, "invalid_request_error", err.Error())
		return
	}
	if err := h.validateRequest(c, platform, body); err != nil {
		status := http.StatusBadRequest
		var uploadTooLarge *service.OpenAIImageUploadTooLargeError
		if errors.As(err, &uploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		imageTaskJSONError(c, status, "invalid_request_error", err.Error())
		return
	}
	if !h.checkSecurityAuditBeforeSubmit(c, apiKey, platform, body) {
		return
	}
	requestedImages := h.requestedImages(c, platform, body)
	reservation, err := h.reserveImages(c.Request.Context(), apiKey.UserID, requestedImages)
	if err != nil {
		handleAsyncImageReservationError(c, err)
		return
	}

	taskCtx, recorder, cancel := newAsyncImageContext(c, body, h.tasks.ExecutionTimeout())
	metadata := h.taskMetadata(c, platform, body)
	task, err := h.tasks.CreateWithMetadata(c.Request.Context(), service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}, metadata)
	if err != nil {
		cancel()
		if reservation != nil {
			if releaseErr := reservation.Release(context.Background()); releaseErr != nil {
				logger.L().Warn("async_image.rate_limit_release_failed", zap.Int64("user_id", apiKey.UserID), zap.Error(releaseErr))
			}
		}
		imageTaskError(c, err)
		return
	}

	pollURL := imageTaskPollURL(c.Request.URL.Path, task.ID)
	c.Header("Cache-Control", "no-store")
	c.Header("Location", pollURL)
	c.Header("Retry-After", "3")
	c.JSON(http.StatusAccepted, gin.H{
		"id":               task.ID,
		"task_id":          task.TaskID,
		"object":           task.Object,
		"requested_images": task.RequestedImages,
		"actual_images":    task.ActualImages,
		"status":           task.Status,
		"created_at":       task.CreatedAt,
		"expires_at":       task.ExpiresAt,
		"poll_url":         pollURL,
	})

	go h.run(task.ID, platform, taskCtx, recorder, cancel, h.asyncImageOpsSnapshot(c, apiKey, platform, task, metadata))
}

// Download returns one ZIP archive containing all images generated by an async
// task. It uses the exact same owner pair as Get so sharing a user account does
// not grant an API key access to another key's task archive.
func (h *AsyncImageHandler) Download(c *gin.Context) {
	if !h.pollable() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	taskID := strings.TrimSpace(c.Param("task_id"))
	// Build the archive before committing an HTTP response. Object storage can
	// fail partway through a read; writing directly to c.Writer would otherwise
	// leave the caller with a truncated 200 ZIP that cannot report its error.
	archive, err := os.CreateTemp("", "sub2api-image-task-*.zip")
	if err != nil {
		imageTaskError(c, service.ErrImageTaskDownload.WithCause(err))
		return
	}
	archivePath := archive.Name()
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archivePath)
	}()

	if _, err := h.tasks.StreamDownloadZip(c.Request.Context(), service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}, taskID, archive); err != nil {
		imageTaskError(c, err)
		return
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		imageTaskError(c, service.ErrImageTaskDownload.WithCause(err))
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": taskID + ".zip"}))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(c.Writer, archive); err != nil {
		logger.L().Warn("image_task.download_response_write_failed", zap.String("task_id", taskID), zap.Error(err))
	}
}

// List returns the authenticated API key's compact task history. It deliberately
// uses the same user/key ownership pair as Get, so one key cannot enumerate
// another key's image generations even when both belong to the same user.
func (h *AsyncImageHandler) List(c *gin.Context) {
	if !h.pollable() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	tasks, err := h.tasks.List(c.Request.Context(), service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}, service.ImageTaskHistoryFilter{
		Status: c.Query("status"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		imageTaskError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, tasks)
}

// AdminSupportList reads durable task history by the validated support target
// ID. It never loads a target API key or touches Redis execution state.
func (h *AsyncImageHandler) AdminSupportList(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	tasks, err := h.tasks.ListByUser(c.Request.Context(), userID, service.ImageTaskHistoryFilter{
		Status: c.Query("status"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, tasks)
}

// AdminSupportGet returns one durable task for the validated target account.
// No download, retry, deletion, or status-repair route is registered beside it.
func (h *AsyncImageHandler) AdminSupportGet(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		response.BadRequest(c, "Invalid user ID")
		return
	}
	task, err := h.tasks.GetByUser(c.Request.Context(), userID, c.Param("task_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, task)
}

// Delete removes only a failed task owned by the authenticated user and API
// key. It remains available after image submission is disabled, like List and
// Get, so existing history can still be managed.
func (h *AsyncImageHandler) Delete(c *gin.Context) {
	if !h.pollable() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	if err := h.tasks.Delete(c.Request.Context(), service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}, c.Param("task_id")); err != nil {
		imageTaskError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusNoContent)
}

func (h *AsyncImageHandler) checkSecurityAuditBeforeSubmit(c *gin.Context, apiKey *service.APIKey, platform string, body []byte) bool {
	if h == nil || h.openAI == nil {
		return true
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		imageTaskJSONError(c, http.StatusInternalServerError, "api_error", "User context not found")
		return false
	}
	model := ""
	moderationBody := body
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		model, moderationBody = parsed.Model, parsed.ModerationBody()
	} else if h.openAI.gatewayService != nil {
		parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err != nil {
			imageTaskJSONError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
			return false
		}
		model, moderationBody = parsed.Model, parsed.ModerationBody()
	}
	if len(moderationBody) == 0 {
		c.Set(securityAuditCompletedContextKey, true)
		return true
	}
	reqLog := requestLogger(c, "handler.async_image.security_audit",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", model))
	decision := h.openAI.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, model, moderationBody)
	if decision != nil && !decision.AllowNextStage {
		h.openAI.openAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func (h *AsyncImageHandler) Get(c *gin.Context) {
	// Polling deliberately does not require the feature to be enabled, only that
	// the task store is reachable. Turning the switch off in the admin UI must not
	// strand tasks that were already accepted — their results are still in Redis
	// and their submitters are still polling.
	if !h.pollable() {
		imageTaskJSONError(c, http.StatusNotFound, "not_found_error", "async image tasks are not enabled")
		return
	}
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.UserID <= 0 || apiKey.ID <= 0 {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), service.ImageTaskOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}, c.Param("task_id"))
	if err != nil {
		imageTaskError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	if task.Status == service.ImageTaskStatusProcessing {
		c.Header("Retry-After", "3")
	}
	c.JSON(http.StatusOK, task)
}

func (h *AsyncImageHandler) validateRequest(c *gin.Context, platform string, body []byte) error {
	if h.openAI == nil || h.openAI.gatewayService == nil {
		return nil
	}
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		if strings.TrimSpace(parsed.Model) == "" {
			return errors.New("model is required")
		}
		return nil
	}
	parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
	if err != nil {
		return err
	}
	if parsed.Stream {
		return errors.New("streaming image requests cannot be submitted as asynchronous tasks")
	}
	return nil
}

// validateAsyncImageEditUploadLimits keeps detached multipart edit requests
// within a bounded memory envelope. The same body is held by the submit
// request, copied into the background context, and parsed before forwarding.
func validateAsyncImageEditUploadLimits(path, contentType string, body []byte) error {
	if !strings.Contains(path, "/images/edits/") || !isMultipartImagesContentType(contentType) {
		return nil
	}
	if int64(len(body)) > asyncImageEditMaxMultipartBytes {
		return errAsyncImageEditMultipartTooLarge
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("invalid multipart content-type: %w", err)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return errors.New("multipart boundary is required")
	}

	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	inputImages := 0
	masks := 0
	var firstImageSize *asyncImageUploadSize
	var maskSize *asyncImageUploadSize
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("read multipart body: %w", nextErr)
		}
		name := strings.TrimSpace(part.FormName())
		fileName := strings.TrimSpace(part.FileName())
		if fileName == "" {
			_ = part.Close()
			continue
		}
		upload, readErr := readAsyncImageEditUpload(part)
		if readErr != nil {
			_ = part.Close()
			return fmt.Errorf("read multipart field %s: %w", name, readErr)
		}
		_ = part.Close()

		switch {
		case name == "image" || strings.HasPrefix(name, "image["):
			if !upload.isSupportedImage() {
				return errAsyncImageEditInvalidImage
			}
			inputImages++
			if inputImages > asyncImageEditMaxInputImages {
				return errAsyncImageEditTooManyInputImages
			}
			if firstImageSize == nil {
				firstImageSize = &upload
			}
		case name == "mask":
			if !upload.isSupportedMask() {
				return errAsyncImageEditInvalidMask
			}
			masks++
			if masks > 1 {
				return errAsyncImageEditTooManyMasks
			}
			maskSize = &upload
		default:
			return fmt.Errorf("unsupported image upload field %q", name)
		}
	}
	if inputImages == 0 {
		return errors.New("image file is required")
	}
	if maskSize != nil && firstImageSize != nil && (maskSize.width != firstImageSize.width || maskSize.height != firstImageSize.height) {
		return errAsyncImageEditMaskSizeMismatch
	}
	return nil
}

type asyncImageUploadSize struct {
	width  int
	height int
	format string
}

func (u asyncImageUploadSize) isSupportedImage() bool {
	return u.format == "png" || u.format == "jpeg" || u.format == "webp"
}

func (u asyncImageUploadSize) isSupportedMask() bool {
	return u.format == "png" || u.format == "webp"
}

func readAsyncImageEditUpload(part *multipart.Part) (asyncImageUploadSize, error) {
	data, err := io.ReadAll(io.LimitReader(part, service.OpenAIImageMaxUploadPartBytes+1))
	if err != nil {
		return asyncImageUploadSize{}, err
	}
	if int64(len(data)) > service.OpenAIImageMaxUploadPartBytes {
		return asyncImageUploadSize{}, &service.OpenAIImageUploadTooLargeError{Limit: service.OpenAIImageMaxUploadPartBytes}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return asyncImageUploadSize{}, errAsyncImageEditInvalidImage
	}
	return asyncImageUploadSize{width: config.Width, height: config.Height, format: strings.ToLower(strings.TrimSpace(format))}, nil
}

func (h *AsyncImageHandler) reserveImages(ctx context.Context, userID int64, requested int) (*service.AsyncImageRateLimitReservation, error) {
	if h == nil || h.rateLimiter == nil {
		return nil, nil
	}
	return h.rateLimiter.Reserve(ctx, userID, requested)
}

func (h *AsyncImageHandler) requestedImages(c *gin.Context, platform string, body []byte) int {
	if platform == service.PlatformGrok {
		return service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body).N
	}
	if h != nil && h.openAI != nil && h.openAI.gatewayService != nil {
		if parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body); err == nil && parsed != nil && parsed.N > 0 {
			return parsed.N
		}
	}
	var input struct {
		N int `json:"n"`
	}
	if json.Unmarshal(body, &input) == nil && input.N > 0 {
		return input.N
	}
	return 1
}

func handleAsyncImageReservationError(c *gin.Context, err error) {
	var exceeded *service.AsyncImageRateLimitExceeded
	if errors.As(err, &exceeded) {
		retryAfter := int((exceeded.RetryAfter + time.Second - 1) / time.Second)
		if retryAfter < 1 {
			retryAfter = 1
		}
		limit := strconv.Itoa(exceeded.Limit)
		seconds := strconv.Itoa(retryAfter)
		c.Header("Retry-After", seconds)
		c.Header("X-RateLimit-Limit", limit)
		c.Header("X-RateLimit-Remaining", "0")
		c.Header("X-RateLimit-Reset", seconds)
		imageTaskJSONError(c, http.StatusTooManyRequests, "async_image_user_image_limit_exceeded", "Async image generation limit exceeded: "+limit+" images per 60 seconds.")
		return
	}
	imageTaskJSONError(c, http.StatusServiceUnavailable, "async_image_rate_limiter_unavailable", "Async image rate limiter is temporarily unavailable")
}

func (h *AsyncImageHandler) taskMetadata(c *gin.Context, platform string, body []byte) service.ImageTaskMetadata {
	requestType := "generation"
	if strings.Contains(c.Request.URL.Path, "/images/edits/") {
		requestType = "edit"
	}
	if platform == service.PlatformGrok {
		parsed := service.ParseGrokMediaRequest(c.GetHeader("Content-Type"), body)
		return service.ImageTaskMetadata{RequestType: requestType, Model: parsed.Model, PromptPreview: parsed.Prompt, RequestedImages: parsed.N}
	}
	if h != nil && h.openAI != nil && h.openAI.gatewayService != nil {
		parsed, err := h.openAI.gatewayService.ParseOpenAIImagesRequest(c, body)
		if err == nil && parsed != nil {
			return service.ImageTaskMetadata{RequestType: requestType, Model: parsed.Model, PromptPreview: parsed.Prompt, RequestedImages: parsed.N}
		}
	}
	return service.ImageTaskMetadata{RequestType: requestType, RequestedImages: h.requestedImages(c, platform, body)}
}

func (h *AsyncImageHandler) executeWithGateway(platform string, c *gin.Context) {
	if h.openAI == nil {
		imageTaskJSONError(c, http.StatusServiceUnavailable, "api_error", "image gateway is unavailable")
		return
	}
	if platform == service.PlatformGrok {
		h.openAI.GrokImages(c)
		return
	}
	h.openAI.Images(c)
}

func (h *AsyncImageHandler) run(taskID, platform string, taskCtx *gin.Context, recorder *httptest.ResponseRecorder, cancel context.CancelFunc, snapshot asyncImageOpsSnapshot) {
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().Error("image_task.execution_panicked", zap.String("task_id", taskID), zap.Any("panic", recovered))
			h.failTask(taskID, http.StatusInternalServerError, imageTaskErrorPayload("api_error", "image generation task panicked"), taskCtx, snapshot)
		}
	}()

	h.execute(platform, taskCtx)
	body := bytes.TrimSpace(recorder.Body.Bytes())
	if err := taskCtx.Request.Context().Err(); err != nil && len(body) == 0 {
		h.failTask(taskID, http.StatusGatewayTimeout, imageTaskErrorPayload("timeout_error", "image generation task timed out"), taskCtx, snapshot)
		return
	}
	statusCode := recorder.Code
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		if len(body) == 0 || !json.Valid(body) {
			h.failTask(taskID, http.StatusBadGateway, imageTaskErrorPayload("api_error", "upstream returned an invalid image response"), taskCtx, snapshot)
			return
		}
		if err := h.tasks.Complete(context.Background(), taskID, statusCode, json.RawMessage(body)); err != nil {
			logger.L().Error("image_task.complete_store_failed", zap.String("task_id", taskID), zap.Error(err))
			return
		}
		// Complete can intentionally turn a task into failed (for example when
		// result offloading fails). Inspect the terminal task state so that this
		// path receives the same observability record as an HTTP failure.
		h.recordCompletedTaskFailure(taskID, taskCtx, snapshot)
		return
	}
	h.failTask(taskID, statusCode, extractImageTaskError(body), taskCtx, snapshot)
}

func (h *AsyncImageHandler) failTask(taskID string, statusCode int, taskErr json.RawMessage, taskCtx *gin.Context, snapshot asyncImageOpsSnapshot) {
	if err := h.tasks.Fail(context.Background(), taskID, statusCode, taskErr); err != nil {
		logger.L().Error("image_task.failure_store_failed", zap.String("task_id", taskID), zap.Error(err))
		return
	}
	h.recordAsyncImageTaskFailure(taskID, statusCode, taskErr, taskCtx, snapshot)
}

func (h *AsyncImageHandler) recordCompletedTaskFailure(taskID string, taskCtx *gin.Context, snapshot asyncImageOpsSnapshot) {
	if h == nil || h.tasks == nil || snapshot.UserID == nil || snapshot.APIKeyID == nil {
		return
	}
	task, err := h.tasks.Get(context.Background(), service.ImageTaskOwner{UserID: *snapshot.UserID, APIKeyID: *snapshot.APIKeyID}, taskID)
	if err != nil || task == nil || task.Status != service.ImageTaskStatusFailed {
		return
	}
	h.recordAsyncImageTaskFailure(taskID, task.HTTPStatus, task.Error, taskCtx, snapshot)
}

func (h *AsyncImageHandler) recordAsyncImageTaskFailure(taskID string, statusCode int, taskErr json.RawMessage, taskCtx *gin.Context, snapshot asyncImageOpsSnapshot) {
	if h == nil || h.ops == nil || strings.TrimSpace(taskID) == "" || taskCtx == nil || taskCtx.Request == nil {
		return
	}
	if !h.ops.IsMonitoringEnabled(context.Background()) {
		return
	}
	if value, ok := taskCtx.Get(service.OpsSkipPassthroughKey); ok {
		if skip, _ := value.(bool); skip {
			return
		}
	}

	parsed := parseAsyncImageTaskOpsError(taskErr)
	normalizedType := normalizeOpsErrorType(parsed.ErrorType, parsed.Code)
	errorBody, safeMessage := asyncImageSafeErrorBody(normalizedType, parsed.Code, parsed.Message)
	if shouldSkipOpsErrorLog(taskCtx.Request.Context(), h.ops, safeMessage, errorBody, snapshot.RequestPath) {
		return
	}

	phase, isBusinessLimited, errorOwner, errorSource := classifyOpsErrorLog(taskCtx, normalizedType, safeMessage, parsed.Code, statusCode)
	phase, errorOwner, errorSource = normalizeAsyncImageFailureClassification(taskCtx, phase, errorOwner, errorSource, statusCode, parsed.ErrorType, safeMessage)

	platform := snapshot.Platform
	if resolvedPlatform, ok := taskCtx.Request.Context().Value(ctxkey.Platform).(string); ok && strings.TrimSpace(resolvedPlatform) != "" {
		platform = strings.TrimSpace(resolvedPlatform)
	}
	upstreamEndpoint := GetUpstreamEndpoint(taskCtx, platform)
	if upstreamEndpoint == "" {
		upstreamEndpoint = DeriveUpstreamEndpoint(snapshot.InboundEndpoint, snapshot.RequestPath, platform)
	}

	entry := &service.OpsInsertErrorLogInput{
		RequestID:       snapshot.RequestID,
		ClientRequestID: snapshot.ClientRequestID,
		AsyncTaskID:     taskID,

		UserID:    snapshot.UserID,
		APIKeyID:  snapshot.APIKeyID,
		GroupID:   snapshot.GroupID,
		ClientIP:  snapshot.ClientIP,
		AccountID: asyncImageTaskAccountID(taskCtx),

		Platform:         platform,
		Model:            snapshot.Model,
		RequestPath:      snapshot.RequestPath,
		Stream:           false,
		InboundEndpoint:  snapshot.InboundEndpoint,
		UpstreamEndpoint: upstreamEndpoint,
		RequestedModel:   snapshot.Model,
		UpstreamModel:    asyncImageTaskUpstreamModel(taskCtx),
		RequestType:      int16Pointer(snapshot.RequestType),
		UserAgent:        snapshot.UserAgent,

		ErrorPhase:        phase,
		ErrorType:         normalizedType,
		Severity:          classifyOpsSeverity(normalizedType, statusCode),
		StatusCode:        statusCode,
		IsBusinessLimited: isBusinessLimited,
		ErrorMessage:      safeMessage,
		ErrorBody:         errorBody,
		ErrorSource:       errorSource,
		ErrorOwner:        errorOwner,
		CreatedAt:         time.Now(),
		APIKeyPrefix:      snapshot.APIKeyPrefix,
	}
	applyOpsLatencyFieldsFromContext(taskCtx, entry)
	applyOpsUpstreamFieldsFromContext(taskCtx, entry)
	enqueueOpsErrorLog(h.ops, entry)
}

func (h *AsyncImageHandler) asyncImageOpsSnapshot(c *gin.Context, apiKey *service.APIKey, platform string, task *service.ImageTask, metadata service.ImageTaskMetadata) asyncImageOpsSnapshot {
	snapshot := asyncImageOpsSnapshot{
		Platform:        truncateString(strings.TrimSpace(platform), 32),
		Model:           truncateString(strings.TrimSpace(metadata.Model), 100),
		RequestType:     int16(service.RequestTypeSync),
		InboundEndpoint: EndpointImagesGenerations,
	}
	if task != nil {
		snapshot.TaskID = truncateString(strings.TrimSpace(task.ID), 64)
	}
	if c != nil && c.Request != nil {
		if c.Request.URL != nil {
			snapshot.RequestPath = truncateString(c.Request.URL.Path, 256)
			logicalPath := strings.TrimSuffix(c.Request.URL.Path, "/async")
			snapshot.InboundEndpoint = NormalizeInboundEndpoint(logicalPath)
		}
		snapshot.UserAgent = c.GetHeader("User-Agent")
		if requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string); requestID != "" {
			snapshot.RequestID = requestID
		}
		if clientRequestID, _ := c.Request.Context().Value(ctxkey.ClientRequestID).(string); clientRequestID != "" {
			snapshot.ClientRequestID = clientRequestID
		}
		if clientIP := strings.TrimSpace(ip.GetClientIP(c)); clientIP != "" {
			snapshot.ClientIP = &clientIP
		}
	}
	if apiKey == nil {
		return snapshot
	}
	userID := apiKey.UserID
	apiKeyID := apiKey.ID
	snapshot.UserID = &userID
	snapshot.APIKeyID = &apiKeyID
	if apiKey.GroupID != nil {
		groupID := *apiKey.GroupID
		snapshot.GroupID = &groupID
	}
	snapshot.APIKeyPrefix = keyPrefix(apiKey.Key, 8)
	return snapshot
}

func parseAsyncImageTaskOpsError(raw json.RawMessage) parsedOpsError {
	parsed := parseOpsErrorResponse(raw)
	var payload struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		if value := strings.TrimSpace(payload.Type); value != "" {
			parsed.ErrorType = value
		}
		if value := strings.TrimSpace(payload.Code); value != "" {
			parsed.Code = value
		}
		if value := strings.TrimSpace(payload.Message); value != "" {
			parsed.Message = value
		}
	}
	if strings.TrimSpace(parsed.Message) == "" {
		parsed.Message = "image generation failed"
	}
	return parsed
}

func asyncImageSafeErrorBody(errorType, code, message string) (string, string) {
	// The task error may originate from an arbitrary upstream response. Do not
	// put that raw message in the user-visible error_body: existing JSON
	// redaction intentionally focuses on field values and cannot safely infer
	// credentials embedded in free-form text or URL query strings. Upstream
	// diagnostics are retained separately for administrators and sanitized by
	// the existing Ops service before persistence.
	safeMessage := asyncImageUserSafeErrorMessage(errorType, message)
	payload := gin.H{"error": gin.H{"type": errorType, "message": safeMessage}}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", "image generation failed"
	}
	body, _ := service.SanitizeOpsErrorBodyForQueue(string(raw))
	var sanitized struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &sanitized) == nil && strings.TrimSpace(sanitized.Error.Message) != "" {
		return body, strings.TrimSpace(sanitized.Error.Message)
	}
	return body, safeMessage
}

func asyncImageUserSafeErrorMessage(errorType, message string) string {
	message = strings.TrimSpace(message)
	lower := strings.ToLower(message)
	switch {
	case isOpsNoAvailableAccountMessage(message):
		return "No available compatible accounts"
	case strings.EqualFold(strings.TrimSpace(errorType), "timeout_error"), strings.Contains(lower, "timed out"):
		return "image generation task timed out"
	case message == "image generation task panicked",
		message == "upstream returned an invalid image response",
		message == "failed to store generated image to object storage":
		return message
	default:
		return "image generation failed"
	}
}

func normalizeAsyncImageFailureClassification(taskCtx *gin.Context, phase, owner, source string, statusCode int, errorType, message string) (string, string, string) {
	timedOut := statusCode == http.StatusGatewayTimeout || strings.EqualFold(strings.TrimSpace(errorType), "timeout_error") || strings.Contains(strings.ToLower(message), "timed out")
	if taskCtx != nil && taskCtx.Request != nil && errors.Is(taskCtx.Request.Context().Err(), context.DeadlineExceeded) {
		timedOut = true
	}
	if timedOut {
		return "network", "provider", "gateway"
	}
	if phase == "internal" && statusCode >= http.StatusBadGateway && statusCode <= http.StatusGatewayTimeout {
		return "upstream", "provider", "gateway"
	}
	return phase, owner, source
}

func asyncImageTaskAccountID(c *gin.Context) *int64 {
	if c == nil {
		return nil
	}
	if value, ok := c.Get(service.OpsUpstreamErrorsKey); ok {
		if events, ok := value.([]*service.OpsUpstreamErrorEvent); ok {
			for index := len(events) - 1; index >= 0; index-- {
				if events[index] != nil && events[index].AccountID > 0 {
					return int64Pointer(events[index].AccountID)
				}
			}
		}
	}
	if value, ok := c.Get(opsAccountIDKey); ok {
		if accountID, ok := value.(int64); ok && accountID > 0 {
			return int64Pointer(accountID)
		}
	}
	if c.Request != nil {
		if accountID, ok := c.Request.Context().Value(ctxkey.AccountID).(int64); ok && accountID > 0 {
			return int64Pointer(accountID)
		}
	}
	return nil
}

func asyncImageTaskUpstreamModel(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(opsUpstreamModelKey); ok {
		if model, ok := value.(string); ok {
			return truncateString(strings.TrimSpace(model), 100)
		}
	}
	return ""
}

func int64Pointer(value int64) *int64 { return &value }

func int16Pointer(value int16) *int16 { return &value }

func newAsyncImageContext(c *gin.Context, body []byte, timeoutDuration time.Duration) (*gin.Context, *httptest.ResponseRecorder, context.CancelFunc) {
	base := context.WithoutCancel(c.Request.Context())
	executionCtx, cancel := context.WithTimeout(base, timeoutDuration)
	request := c.Request.Clone(executionCtx)
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	request.ContentLength = int64(len(body))
	request.URL.Path = strings.TrimSuffix(request.URL.Path, "/async")

	taskCtx := c.Copy()
	recorder := httptest.NewRecorder()
	recorderCtx, _ := gin.CreateTestContext(recorder)
	taskCtx.Writer = recorderCtx.Writer
	taskCtx.Request = request
	return taskCtx, recorder, cancel
}

func asyncImageRequestStreams(contentType string, body []byte) bool {
	if isMultipartImagesContentType(contentType) {
		return false
	}
	var envelope struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &envelope) == nil && envelope.Stream
}

func imageTaskPollURL(submitPath, taskID string) string {
	if strings.HasPrefix(submitPath, "/v1/") {
		return "/v1/images/tasks/" + taskID
	}
	return "/images/tasks/" + taskID
}

func extractImageTaskError(body []byte) json.RawMessage {
	if json.Valid(body) {
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil && len(envelope.Error) > 0 && json.Valid(envelope.Error) {
			return envelope.Error
		}
		return json.RawMessage(body)
	}
	return imageTaskErrorPayload("api_error", "image generation failed")
}

func imageTaskErrorPayload(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(gin.H{"type": errorType, "message": message})
	return data
}

func imageTaskError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := infraerrors.Reason(err)
	message := infraerrors.Message(err)
	if status <= 0 {
		status = http.StatusInternalServerError
	}
	if strings.TrimSpace(code) == "" {
		code = "IMAGE_TASK_ERROR"
	}
	imageTaskJSONError(c, status, code, message)
}

func imageTaskJSONError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"error": gin.H{"type": code, "code": code, "message": message}})
}
