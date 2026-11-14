package admin

import (
	"context"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
)

// ImageTaskSettingsHandler owns the single admin surface for generation modes:
// the object-storage target, the ordinary-mode switch and the batch-mode switch.
type ImageTaskSettingsHandler struct {
	storage *service.ImageStorageSettingService
	batch   *service.BatchImageSettingsService
}

func NewImageTaskSettingsHandler(storage *service.ImageStorageSettingService, batch *service.BatchImageSettingsService) *ImageTaskSettingsHandler {
	return &ImageTaskSettingsHandler{storage: storage, batch: batch}
}

type imageTaskSettingsPayload struct {
	OrdinaryEnabled *bool                         `json:"ordinary_enabled"`
	BatchEnabled    *bool                         `json:"batch_enabled"`
	Storage         *service.ImageStorageSettings `json:"storage"`
}

// Get returns both switches and the masked storage configuration.
func (h *ImageTaskSettingsHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	snapshot, err := h.snapshot(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}

// Update applies only the fields present in the payload. Enabling the ordinary
// mode requires a complete target; changing endpoint or bucket while archived
// images or accepted tasks exist is rejected by the service layer.
func (h *ImageTaskSettingsHandler) Update(c *gin.Context) {
	var req imageTaskSettingsPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ctx := c.Request.Context()

	storageTouched := req.Storage != nil || req.OrdinaryEnabled != nil
	if storageTouched {
		current, err := h.storage.Get(ctx)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		next := *current
		if req.Storage != nil {
			next = *req.Storage
		}
		if req.OrdinaryEnabled != nil {
			next.Enabled = *req.OrdinaryEnabled
		}
		if _, err := h.storage.Update(ctx, next); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	if req.BatchEnabled != nil {
		if err := h.batch.SetEnabled(ctx, *req.BatchEnabled); err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}

	snapshot, err := h.snapshot(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}

// TestConnection validates the submitted target without saving it.
func (h *ImageTaskSettingsHandler) TestConnection(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.storage.TestConnection(c.Request.Context(), req); err != nil {
		response.Success(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "connection successful"})
}

func (h *ImageTaskSettingsHandler) snapshot(ctx context.Context) (gin.H, error) {
	storage, err := h.storage.Get(ctx)
	if err != nil {
		return nil, err
	}
	return gin.H{
		"ordinary_enabled":          storage.Enabled,
		"batch_enabled":             h.batch.Enabled(ctx),
		"storage":                   storage,
		"storage_secret_configured": h.storage.SecretConfigured(ctx),
	}, nil
}
