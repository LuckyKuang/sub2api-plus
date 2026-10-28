package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ProviderFiles has one audit point shared by upload/list/retrieve/delete. Basic
// syntax validation precedes it; ACL reads, slots, scheduling and writes follow.
func (h *OpenAIGatewayHandler) ProviderFiles(c *gin.Context) {
	key, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "API key is required")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Authentication is required")
		return
	}
	reqLog := requestLogger(c, "handler.provider_files", zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", key.ID))
	platform := openAICompatibleRequestPlatform(c.Request.Context(), key)
	if platform != service.PlatformDeepseek && platform != service.PlatformMiniMax {
		h.providerFilesError(c, service.ErrProviderFilesUnsupported)
		return
	}
	if h.providerFilesService == nil {
		h.providerFilesError(c, service.ErrProviderFileStore)
		return
	}
	if platform == service.PlatformMiniMax && c.Request.Method == http.MethodPost && !strings.HasSuffix(c.Request.URL.Path, "/files/upload") {
		h.providerFilesError(c, service.ErrProviderFilesUnsupported)
		return
	}
	if platform == service.PlatformDeepseek && strings.HasSuffix(c.Request.URL.Path, "/files/upload") {
		h.providerFilesError(c, service.ErrProviderFilesUnsupported)
		return
	}
	id := c.Param("file_id")
	if id != "" && (!strings.HasPrefix(id, service.ProviderFileIDPrefix) || len(id) > 128) {
		h.providerFilesError(c, service.ErrProviderFileNotFound)
		return
	}
	var upload *service.ProviderFileUpload
	if c.Request.Method == http.MethodPost {
		var cleanup func()
		var err error
		upload, cleanup, err = parseProviderFileUpload(c, platform)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			h.providerFilesError(c, err)
			return
		}
	}
	limit := 20
	after, before := c.Query("after_id"), c.Query("before_id")
	if c.Request.Method == http.MethodGet && id == "" {
		if after != "" && before != "" || c.Query("order") != "" || c.Query("purpose") != "" {
			h.providerFilesError(c, errors.New("unsupported Files list parameters"))
			return
		}
		if raw := c.Query("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > 1000 {
				h.providerFilesError(c, errors.New("limit must be between 1 and 1000"))
				return
			}
		}
	}
	operation := "metadata"
	if c.Request.Method == http.MethodPost {
		operation = "upload"
	} else if c.Request.Method == http.MethodDelete {
		operation = "delete"
	} else if id == "" {
		operation = "list"
	}
	auditMetadata := map[string]any{"operation": operation}
	if upload != nil {
		auditMetadata["file"] = map[string]any{"bytes": upload.Bytes}
	}
	auditBody, _ := json.Marshal(auditMetadata)
	if decision := h.checkSecurityAudit(c, reqLog, key, subject, service.ContentModerationProtocolProviderFiles, "", auditBody); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}

	userRelease, admitted := acquireGatewayUserRequest(c, h.concurrencyHelper, h.errorResponse)
	if !admitted {
		return
	}
	defer userRelease()
	c.Request = c.Request.WithContext(service.WithOpenAIProfitControlSuppressed(c.Request.Context()))
	switch c.Request.Method {
	case http.MethodPost:
		ctx, err := h.providerFilesService.ResolveUploadRetry(c.Request.Context(), key, platform, c.GetHeader("Idempotency-Key"))
		if err != nil {
			h.providerFilesError(c, err)
			return
		}
		c.Request = c.Request.WithContext(ctx)
		started := false
		selection, err := h.providerFilesService.SelectUploadAccount(c.Request.Context(), key, platform)
		if err != nil {
			h.providerFilesError(c, err)
			return
		}
		release, slot := h.acquireResponsesAccountSlot(c, key.GroupID, "", selection, false, &started, reqLog)
		if slot != openAISlotAcquireOK {
			return
		}
		if release != nil {
			defer release()
		}
		file, err := h.providerFilesService.Upload(c.Request.Context(), key, selection.Account, upload, c.GetHeader("Idempotency-Key"))
		if err != nil {
			h.providerFilesError(c, err)
			return
		}
		if platform == service.PlatformMiniMax {
			c.JSON(http.StatusOK, gin.H{"file": gin.H{"file_id": file.PublicID, "bytes": file.Bytes, "created_at": file.CreatedAt.Unix(), "filename": file.Filename, "purpose": file.Purpose}, "base_resp": gin.H{"status_code": 0, "status_msg": "success"}})
			return
		}
		c.JSON(http.StatusOK, providerFileMetadata(file))
	case http.MethodGet:
		if platform == service.PlatformMiniMax {
			h.providerFilesError(c, service.ErrProviderFilesUnsupported)
			return
		}
		if id != "" {
			file, err := h.providerFilesService.Get(c.Request.Context(), key, platform, id)
			if err != nil {
				h.providerFilesError(c, err)
				return
			}
			c.JSON(http.StatusOK, providerFileMetadata(file))
			return
		}
		files, err := h.providerFilesService.List(c.Request.Context(), key, platform, after, before, limit+1)
		if err != nil {
			h.providerFilesError(c, err)
			return
		}
		hasMore := len(files) > limit
		if hasMore {
			files = files[:limit]
		}
		items := make([]gin.H, 0, len(files))
		first, last := "", ""
		for _, file := range files {
			items = append(items, providerFileMetadata(file))
			if first == "" {
				first = file.PublicID
			}
			last = file.PublicID
		}
		c.JSON(http.StatusOK, gin.H{"data": items, "first_id": nullableFileID(first), "last_id": nullableFileID(last), "has_more": hasMore})
	case http.MethodDelete:
		err := h.providerFilesService.Delete(c.Request.Context(), key, platform, id)
		if err != nil {
			h.providerFilesError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": id, "type": "file_deleted"})
	default:
		h.providerFilesError(c, service.ErrProviderFilesUnsupported)
	}
}

func nullableFileID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func providerFileMetadata(file *service.ProviderFile) gin.H {
	return gin.H{"id": file.PublicID, "type": "file", "size_bytes": file.Bytes, "created_at": file.CreatedAt.UTC().Format(time.RFC3339), "filename": file.Filename, "mime_type": file.MIMEType}
}

func (h *OpenAIGatewayHandler) providerFilesError(c *gin.Context, err error) {
	status, kind, message := http.StatusBadRequest, "invalid_request_error", "Invalid file request"
	switch {
	case errors.Is(err, service.ErrProviderFilesUnsupported):
		status, kind, message = http.StatusNotImplemented, "unsupported_operation", err.Error()
	case errors.Is(err, service.ErrProviderFileNotFound):
		status, kind, message = http.StatusNotFound, "not_found_error", err.Error()
	case errors.Is(err, service.ErrProviderFileConflict), errors.Is(err, service.ErrProviderFileOwnerChanged):
		status, kind, message = http.StatusConflict, "file_conflict", err.Error()
	case errors.Is(err, service.ErrProviderFileStore):
		status, kind, message = http.StatusServiceUnavailable, "api_error", err.Error()
	case errors.Is(err, service.ErrProviderFileQuota):
		status, kind, message = http.StatusRequestEntityTooLarge, "file_quota_exceeded", err.Error()
	case errors.Is(err, service.ErrProviderFileUpstream):
		status, kind, message = http.StatusBadGateway, "api_error", err.Error()
	}
	h.errorResponse(c, status, kind, message)
}

func parseProviderFileUpload(c *gin.Context, platform string) (*service.ProviderFileUpload, func(), error) {
	defer func() { _ = c.Request.Body.Close() }()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.ProviderFileMaxBytes+16<<10)
	form, err := c.Request.MultipartReader()
	if err != nil {
		return nil, nil, err
	}
	var temporary *os.File
	cleanup := func() {
		if temporary != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}
	fields := map[string]string{}
	name, contentType := "", ""
	for {
		part, err := form.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, cleanup, err
		}
		field := part.FormName()
		if part.FileName() != "" {
			if field != "file" || temporary != nil {
				_ = part.Close()
				return nil, cleanup, errors.New("exactly one file is required")
			}
			name = part.FileName()
			contentType = part.Header.Get("Content-Type")
			if contentType != "" {
				contentType, _, err = mime.ParseMediaType(contentType)
				if err != nil {
					_ = part.Close()
					return nil, cleanup, err
				}
			}
			temporary, err = os.CreateTemp("", "sub2api-provider-file-*")
			if err != nil {
				_ = part.Close()
				return nil, cleanup, err
			}
			// An open, unlinked spool is reclaimed by the kernel even if the
			// process crashes. It never becomes retained gateway file storage.
			if err = os.Remove(temporary.Name()); err != nil {
				_ = part.Close()
				return nil, cleanup, err
			}
			written, copyErr := io.Copy(temporary, io.LimitReader(&providerFileContextReader{context: c.Request.Context(), reader: part}, service.ProviderFileMaxBytes+1))
			_ = part.Close()
			if copyErr != nil {
				return nil, cleanup, copyErr
			}
			if written > service.ProviderFileMaxBytes {
				return nil, cleanup, errors.New("file is too large")
			}
		} else {
			if field != "purpose" && field != "expires_after[anchor]" && field != "expires_after[seconds]" {
				_ = part.Close()
				return nil, cleanup, errors.New("unsupported file form field")
			}
			if _, exists := fields[field]; exists {
				_ = part.Close()
				return nil, cleanup, errors.New("duplicate file form field")
			}
			value, err := io.ReadAll(io.LimitReader(part, 1025))
			_ = part.Close()
			if err != nil || len(value) > 1024 {
				return nil, cleanup, errors.New("invalid file form field")
			}
			fields[field] = string(value)
		}
	}
	if temporary == nil {
		return nil, cleanup, errors.New("file is required")
	}
	seconds := int64(0)
	anchor, hasAnchor := fields["expires_after[anchor]"]
	raw, hasSeconds := fields["expires_after[seconds]"]
	if hasAnchor != hasSeconds || hasAnchor && anchor != "created_at" {
		return nil, cleanup, errors.New("invalid file expiry")
	}
	if hasSeconds {
		seconds, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || seconds == 0 {
			return nil, cleanup, errors.New("invalid file expiry")
		}
	}
	file, err := service.ValidateProviderFileUpload(platform, name, contentType, fields["purpose"], seconds, temporary)
	return file, cleanup, err
}

type providerFileContextReader struct {
	context interface{ Err() error }
	reader  io.Reader
}

func (r *providerFileContextReader) Read(buffer []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

func (h *OpenAIGatewayHandler) resolveProviderFilesForInference(c *gin.Context, key *service.APIKey, body []byte, protocol string) ([]byte, error) {
	if !service.ProviderFileReferencesPresent(body) {
		return body, nil
	}
	platform := openAICompatibleRequestPlatform(c.Request.Context(), key)
	if platform == service.PlatformMiniMax && protocol != service.APIProtocolChatCompletions {
		return nil, service.ErrProviderFilesUnsupported
	}
	if platform == service.PlatformDeepseek && protocol != service.APIProtocolAnthropic {
		return nil, service.ErrProviderFilesUnsupported
	}
	if h.providerFilesService == nil {
		return nil, service.ErrProviderFileStore
	}
	ctx, resolved, err := h.providerFilesService.ResolveReferences(c.Request.Context(), key, platform, body)
	if err != nil {
		return nil, err
	}
	c.Request = c.Request.WithContext(ctx)
	return resolved, nil
}
