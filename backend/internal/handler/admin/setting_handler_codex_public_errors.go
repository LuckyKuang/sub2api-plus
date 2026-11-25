package admin

import (
	"errors"
	"log/slog"
	"sort"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/response"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"

	"github.com/gin-gonic/gin"
)

type patchCodexPublicErrorMessagesRequest struct {
	Revision      string             `json:"revision"`
	CustomEnabled *bool              `json:"custom_enabled"`
	Changes       map[string]*string `json:"changes"`
}

// GetCodexErrorMessages returns the complete catalog with the effective text.
func (h *SettingHandler) GetCodexErrorMessages(c *gin.Context) {
	if h == nil || h.settingService == nil {
		response.Error(c, 500, "setting service unavailable")
		return
	}
	response.Success(c, h.settingService.CodexPublicErrorSnapshot(c.Request.Context()))
}

// PatchCodexErrorMessages applies a revision-checked draft. A stale revision is
// a conflict, not a silent last-writer-wins overwrite.
func (h *SettingHandler) PatchCodexErrorMessages(c *gin.Context) {
	if h == nil || h.settingService == nil {
		response.Error(c, 500, "setting service unavailable")
		return
	}
	var request patchCodexPublicErrorMessagesRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, 400, "invalid request body")
		return
	}
	snapshot, err := h.settingService.SaveCodexPublicErrorSettings(c.Request.Context(), request.Revision, request.CustomEnabled, request.Changes)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCodexPublicErrorRevisionConflict):
			response.Error(c, 409, "configuration changed; reload before saving")
		case errors.Is(err, service.ErrCodexPublicErrorInvalid):
			response.Error(c, 400, err.Error())
		default:
			response.Error(c, 500, "failed to save codex error messages")
		}
		return
	}
	// The request-body audit is size-limited, so a large mixed batch needs its
	// own per-key action record. Values are never logged, only key and action.
	setKeys := make([]string, 0, len(request.Changes))
	restoredKeys := make([]string, 0, len(request.Changes))
	for key, value := range request.Changes {
		if value == nil {
			restoredKeys = append(restoredKeys, key)
		} else {
			setKeys = append(setKeys, key)
		}
	}
	sort.Strings(setKeys)
	sort.Strings(restoredKeys)
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	role, _ := middleware.GetUserRoleFromContext(c)
	modeChanged := request.CustomEnabled != nil
	slog.Info("codex public error messages updated",
		"audit", true,
		"user_id", subject.UserID,
		"role", role,
		"set", setKeys,
		"restored", restoredKeys,
		"mode_changed", modeChanged,
		"custom_enabled", snapshot.CustomEnabled,
	)
	response.Success(c, snapshot)
}
