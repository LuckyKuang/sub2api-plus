package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
)

// ImageTaskUserHandler serves the signed-in console. It is deliberately
// separate from AsyncImageHandler: gateway routes stay scoped to one API key,
// while the console lists every key of the authenticated user so deleting or
// rotating a key never hides existing history.
type ImageTaskUserHandler struct {
	tasks   *service.ImageTaskService
	storage *service.ImageStorageSettingService
	batch   *service.BatchImageSettingsService
	keys    *service.APIKeyService
}

func NewImageTaskUserHandler(
	tasks *service.ImageTaskService,
	storage *service.ImageStorageSettingService,
	batch *service.BatchImageSettingsService,
	keys *service.APIKeyService,
) *ImageTaskUserHandler {
	return &ImageTaskUserHandler{tasks: tasks, storage: storage, batch: batch, keys: keys}
}

// Modes reports which generation modes the console may offer. The global switch
// is not a permission: can_submit also requires an eligible key, and can_view
// keeps history reachable after submission stops. Reasons are stable
// identifiers, not translated text.
func (h *ImageTaskUserHandler) Modes(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	ordinary := service.ImageStorageAvailability{Reason: "disabled"}
	if h.storage != nil {
		ordinary = h.storage.Availability()
	}
	batch := service.BatchImageAvailability{Reason: "disabled"}
	if h.batch != nil {
		batch = h.batch.Availability(ctx)
	}

	ordinaryKey, hasOrdinaryHistory := h.ordinaryAccess(ctx, owner.UserID)
	batchKey, anyBatchKey := h.batchAccess(ctx, owner.UserID)
	ordinaryReason := ordinary.Reason
	if ordinary.Enabled && !ordinaryKey {
		ordinaryReason = "no_eligible_key"
	}
	batchReason := batch.Reason
	if batch.Enabled && !batchKey {
		batchReason = "no_eligible_key"
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"ordinary": gin.H{
			"enabled":    ordinary.Enabled,
			"can_submit": ordinary.Enabled && ordinaryKey,
			"can_view":   (ordinary.Enabled && ordinaryKey) || hasOrdinaryHistory,
			"reason":     ordinaryReason,
		},
		"batch": gin.H{
			"enabled":    batch.Enabled,
			"can_submit": batch.Enabled && batchKey,
			"can_view":   (batch.Enabled && batchKey) || anyBatchKey,
			"reason":     batchReason,
		},
	})
}

// ordinaryAccess reports whether the user can submit, and whether any ordinary
// history row exists regardless of current keys.
func (h *ImageTaskUserHandler) ordinaryAccess(ctx context.Context, userID int64) (bool, bool) {
	hasKey := h.anyKey(ctx, userID, func(key service.APIKey) bool {
		if key.Status != "active" || key.Group == nil || !key.Group.AllowImageGeneration {
			return false
		}
		return key.Group.Platform == service.PlatformOpenAI || key.Group.Platform == service.PlatformGrok
	})
	if h.tasks == nil {
		return hasKey, false
	}
	hasHistory, err := h.tasks.HasAnyTask(ctx, userID)
	if err != nil {
		hasHistory = false
	}
	return hasKey, hasHistory
}

// batchAccess reports whether the user can submit batch jobs, and whether any
// batch-capable key exists, including keys that can no longer submit.
func (h *ImageTaskUserHandler) batchAccess(ctx context.Context, userID int64) (bool, bool) {
	eligible := func(key service.APIKey) bool {
		return key.Group != nil && key.Group.AllowBatchImageGeneration && key.Group.Platform == service.PlatformGemini
	}
	active := h.anyKey(ctx, userID, func(key service.APIKey) bool {
		return key.Status == "active" && eligible(key)
	})
	return active, h.anyKey(ctx, userID, eligible)
}

// anyKey walks the user's keys in bounded pages. A permission decision never
// depends on the first page alone.
func (h *ImageTaskUserHandler) anyKey(ctx context.Context, userID int64, match func(service.APIKey) bool) bool {
	if h.keys == nil || userID <= 0 {
		return false
	}
	for page := 1; page <= 10; page++ {
		keys, _, err := h.keys.List(ctx, userID, pagination.PaginationParams{Page: page, PageSize: 100}, service.APIKeyListFilters{})
		if err != nil || len(keys) == 0 {
			return false
		}
		for _, key := range keys {
			if match(key) {
				return true
			}
		}
		if len(keys) < 100 {
			return false
		}
	}
	return false
}

// List returns the authenticated user's tasks across every API key.
func (h *ImageTaskUserHandler) List(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != service.ImageTaskStatusProcessing && status != service.ImageTaskStatusCompleted && status != service.ImageTaskStatusFailed {
		imageTaskJSONError(c, http.StatusBadRequest, "INVALID_STATUS", "status must be processing, completed, or failed")
		return
	}
	since, ok := queryTimestamp(c, "since")
	if !ok {
		imageTaskJSONError(c, http.StatusBadRequest, "INVALID_TIME_RANGE", "since must be a unix timestamp")
		return
	}
	until, ok := queryTimestamp(c, "until")
	if !ok {
		imageTaskJSONError(c, http.StatusBadRequest, "INVALID_TIME_RANGE", "until must be a unix timestamp")
		return
	}
	if since > 0 && until > 0 && since >= until {
		imageTaskJSONError(c, http.StatusBadRequest, "INVALID_TIME_RANGE", "since must be earlier than until")
		return
	}
	filter := service.ImageTaskHistoryFilter{
		Status:   status,
		Platform: strings.TrimSpace(c.Query("platform")),
		Model:    strings.TrimSpace(c.Query("model")),
		Since:    since,
		Until:    until,
		Limit:    int(queryInt64(c, "limit")),
		Offset:   int(queryInt64(c, "offset")),
	}
	result, err := h.tasks.List(c.Request.Context(), owner, filter)
	if err != nil {
		imageTaskError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// Get returns one task owned by the authenticated user.
func (h *ImageTaskUserHandler) Get(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	task, err := h.requireTasks(c, owner, c.Param("task_id"))
	if err != nil {
		return
	}
	c.Header("Cache-Control", "no-store")
	if task.Status == service.ImageTaskStatusProcessing {
		c.Header("Retry-After", "3")
	}
	c.JSON(http.StatusOK, task)
}

// Download streams a ZIP built from the archived objects. The archive is
// prepared before the response is committed so a storage failure can still be
// reported instead of returning a truncated 200.
func (h *ImageTaskUserHandler) Download(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}
	taskID := strings.TrimSpace(c.Param("task_id"))
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

	if _, err := h.tasks.StreamDownloadZip(c.Request.Context(), owner, taskID, archive); err != nil {
		imageTaskError(c, err)
		return
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		imageTaskError(c, service.ErrImageTaskDownload.WithCause(err))
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", taskID+".zip"))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(c.Writer, archive); err != nil {
		return
	}
}

// Delete removes a failed task. Successful tasks keep their images and records.
func (h *ImageTaskUserHandler) Delete(c *gin.Context) {
	owner, ok := h.owner(c)
	if !ok {
		return
	}
	if h.tasks == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return
	}
	if err := h.tasks.Delete(c.Request.Context(), owner, c.Param("task_id")); err != nil {
		imageTaskError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// owner resolves the signed-in user with no key scope, which is the console
// contract: history belongs to the user even after the creating key is gone.
func (h *ImageTaskUserHandler) owner(c *gin.Context) (service.ImageTaskOwner, bool) {
	if h == nil {
		imageTaskJSONError(c, http.StatusServiceUnavailable, "IMAGE_TASK_UNAVAILABLE", "image tasks are unavailable")
		return service.ImageTaskOwner{}, false
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		imageTaskJSONError(c, http.StatusUnauthorized, "IMAGE_TASK_UNAUTHORIZED", "authentication is required")
		return service.ImageTaskOwner{}, false
	}
	return service.ImageTaskOwner{UserID: subject.UserID}, true
}

func (h *ImageTaskUserHandler) requireTasks(c *gin.Context, owner service.ImageTaskOwner, taskID string) (*service.ImageTask, error) {
	if h.tasks == nil {
		imageTaskError(c, service.ErrImageTaskUnavailable)
		return nil, service.ErrImageTaskUnavailable
	}
	task, err := h.tasks.Get(c.Request.Context(), owner, taskID)
	if err != nil {
		imageTaskError(c, err)
		return nil, err
	}
	return task, nil
}

// queryTimestamp parses an optional unix timestamp and distinguishes an empty
// value from an invalid one, so a typo cannot silently widen the range.
func queryTimestamp(c *gin.Context, name string) (int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

func queryInt64(c *gin.Context, name string) int64 {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}
