package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
)

const (
	defaultContentModerationSessionBlockTTLSeconds = 30 * 24 * 60 * 60
	minContentModerationSessionBlockTTLSeconds     = 60
	maxContentModerationSessionBlockTTLSeconds     = 90 * 24 * 60 * 60
	defaultContentModerationSessionBlockMessage    = "该会话已被内容审核策略屏蔽，请开启新会话 / This session is blocked by content policy, please start a new session"
	ContentModerationErrorCodeSessionBlocked       = "session_blocked_by_content_policy"
)

type ContentModerationSessionBlock struct {
	ID              int64     `json:"id"`
	BlockKey        string    `json:"block_key"`
	SessionID       string    `json:"session_id"`
	UserID          *int64    `json:"user_id,omitempty"`
	UserEmail       string    `json:"user_email"`
	APIKeyID        *int64    `json:"api_key_id,omitempty"`
	APIKeyName      string    `json:"api_key_name"`
	RequestID       string    `json:"request_id"`
	Endpoint        string    `json:"endpoint"`
	Protocol        string    `json:"protocol"`
	Model           string    `json:"model"`
	HighestCategory string    `json:"highest_category"`
	HighestScore    float64   `json:"highest_score"`
	ExpiresAt       time.Time `json:"expires_at"`
	CreatedAt       time.Time `json:"created_at"`
}

type ContentModerationSessionBlockFilter struct {
	Pagination pagination.PaginationParams
	SessionID  string
	UserID     *int64
	Search     string
}

type ContentModerationDeleteSessionBlockResult struct {
	BlockKey  string `json:"block_key"`
	SessionID string `json:"session_id"`
	Deleted   bool   `json:"deleted"`
}

type ContentModerationClearSessionBlocksResult struct {
	Deleted int64 `json:"deleted"`
}

func DefaultContentModerationSessionBlockTTLSeconds() int {
	return defaultContentModerationSessionBlockTTLSeconds
}

func contentModerationSessionBlockTTL(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = defaultContentModerationSessionBlockTTLSeconds
	}
	if seconds < minContentModerationSessionBlockTTLSeconds {
		seconds = minContentModerationSessionBlockTTLSeconds
	}
	if seconds > maxContentModerationSessionBlockTTLSeconds {
		seconds = maxContentModerationSessionBlockTTLSeconds
	}
	return time.Duration(seconds) * time.Second
}

func contentModerationSessionBlockKey(userID, apiKeyID int64, sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	tenant := ""
	switch {
	case userID > 0:
		tenant = "user:" + strconv.FormatInt(userID, 10)
	case apiKeyID > 0:
		tenant = "api_key:" + strconv.FormatInt(apiKeyID, 10)
	default:
		return ""
	}
	sum := sha256.Sum256([]byte(tenant + "|" + sessionID))
	return hex.EncodeToString(sum[:])
}

func (s *ContentModerationService) lookupBlockedSession(ctx context.Context, cfg *ContentModerationConfig, input ContentModerationCheckInput) *ContentModerationDecision {
	if s == nil || cfg == nil || !cfg.SessionBlockEnabled || strings.TrimSpace(input.SessionID) == "" {
		return nil
	}
	blockKey := contentModerationSessionBlockKey(input.UserID, input.APIKeyID, input.SessionID)
	if blockKey == "" {
		return nil
	}
	cached := false
	if s.hashCache != nil {
		hit, err := s.hashCache.HasBlockedSession(ctx, blockKey)
		if err != nil {
			slog.Warn("content_moderation.session_block_check_failed",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"stage", contentModerationAuditStage(input.Stage),
				"body_bytes", len(input.Body),
				"error_code", "session_block_check_failed",
				"error_kind", contentModerationErrorKind(err))
		} else {
			cached = hit
		}
	}
	matched := cached
	if s.repo != nil {
		block, err := s.repo.GetSessionBlockByKey(ctx, blockKey)
		if err != nil {
			slog.Warn("content_moderation.session_block_lookup_failed",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"stage", contentModerationAuditStage(input.Stage),
				"body_bytes", len(input.Body),
				"error_code", "session_block_lookup_failed",
				"error_kind", contentModerationErrorKind(err))
			if !cached {
				return nil
			}
		} else if block != nil && block.ExpiresAt.After(time.Now()) {
			matched = true
			if !cached && s.hashCache != nil {
				ttl := time.Until(block.ExpiresAt)
				if ttl > 0 {
					if err := s.hashCache.RecordBlockedSession(ctx, blockKey, ttl); err != nil {
						slog.Warn("content_moderation.session_block_rehydrate_failed",
							"request_id", input.RequestID,
							"user_id", input.UserID,
							"endpoint", input.Endpoint,
							"protocol", input.Protocol,
							"stage", contentModerationAuditStage(input.Stage),
							"body_bytes", len(input.Body),
							"error_code", "session_block_rehydrate_failed",
							"error_kind", contentModerationErrorKind(err))
					}
				}
			}
		} else {
			matched = false
			if cached && s.hashCache != nil {
				if _, err := s.hashCache.DeleteBlockedSession(ctx, blockKey); err != nil {
					slog.Warn("content_moderation.session_block_stale_cache_delete_failed",
						"request_id", input.RequestID,
						"user_id", input.UserID,
						"endpoint", input.Endpoint,
						"protocol", input.Protocol,
						"stage", contentModerationAuditStage(input.Stage),
						"body_bytes", len(input.Body),
						"error_code", "session_block_stale_cache_delete_failed",
						"error_kind", contentModerationErrorKind(err))
				}
			}
		}
	}

	if !matched {
		return nil
	}
	s.recordPreBlockSyncMetric(0, ContentModerationActionSessionBlock)
	slog.Info("content_moderation.session_block",
		"request_id", input.RequestID,
		"user_id", input.UserID,
		"api_key_id", input.APIKeyID,
		"endpoint", input.Endpoint,
		"protocol", input.Protocol,
		"stage", contentModerationAuditStage(input.Stage))
	log := s.buildLog(input, cfg, ContentModerationActionSessionBlock, true, "session", 1.0, map[string]float64{"session": 1.0}, "", nil, nil, "")
	s.enqueueRecord(input, cfg, log, "", false, false)
	return &ContentModerationDecision{
		Allowed:    false,
		Blocked:    true,
		Flagged:    true,
		Message:    defaultContentModerationSessionBlockMessage,
		StatusCode: cfg.BlockStatus,
		Action:     ContentModerationActionSessionBlock,
		ErrorCode:  ContentModerationErrorCodeSessionBlocked,
	}
}

func (s *ContentModerationService) recordAPISessionBlock(ctx context.Context, cfg *ContentModerationConfig, input ContentModerationCheckInput, decision *ContentModerationDecision) {
	if s == nil || cfg == nil || decision == nil || !cfg.SessionBlockEnabled || input.AdminUser {
		return
	}
	if decision.Action != ContentModerationActionBlock || !decision.Blocked {
		return
	}
	sessionID := strings.TrimSpace(input.SessionID)
	blockKey := contentModerationSessionBlockKey(input.UserID, input.APIKeyID, sessionID)
	if blockKey == "" {
		if sessionID == "" {
			slog.Info("content_moderation.session_block_skipped_missing_id",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"api_key_id", input.APIKeyID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"stage", contentModerationAuditStage(input.Stage))
		}
		return
	}
	ttl := contentModerationSessionBlockTTL(cfg.SessionBlockTTLSeconds)
	expiresAt := time.Now().Add(ttl)
	var userID *int64
	if input.UserID > 0 {
		id := input.UserID
		userID = &id
	}
	var apiKeyID *int64
	if input.APIKeyID > 0 {
		id := input.APIKeyID
		apiKeyID = &id
	}
	block := &ContentModerationSessionBlock{
		BlockKey:        blockKey,
		SessionID:       sessionID,
		UserID:          userID,
		APIKeyID:        apiKeyID,
		RequestID:       input.RequestID,
		Endpoint:        input.Endpoint,
		Protocol:        input.Protocol,
		Model:           input.Model,
		HighestCategory: decision.HighestCategory,
		HighestScore:    decision.HighestScore,
		ExpiresAt:       expiresAt,
	}
	if s.repo != nil {
		if err := s.repo.UpsertSessionBlock(ctx, block); err != nil {
			slog.Warn("content_moderation.session_block_persist_failed",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"stage", contentModerationAuditStage(input.Stage),
				"body_bytes", len(input.Body),
				"error_code", "session_block_persist_failed",
				"error_kind", contentModerationErrorKind(err))
		} else if !block.ExpiresAt.IsZero() {
			ttl = time.Until(block.ExpiresAt)
		}
	}
	if s.hashCache != nil && ttl > 0 {
		if err := s.hashCache.RecordBlockedSession(ctx, blockKey, ttl); err != nil {
			slog.Warn("content_moderation.session_block_record_failed",
				"request_id", input.RequestID,
				"user_id", input.UserID,
				"endpoint", input.Endpoint,
				"protocol", input.Protocol,
				"stage", contentModerationAuditStage(input.Stage),
				"body_bytes", len(input.Body),
				"error_code", "session_block_record_failed",
				"error_kind", contentModerationErrorKind(err))
		}
	}
}

func (s *ContentModerationService) ListSessionBlocks(ctx context.Context, filter ContentModerationSessionBlockFilter) ([]ContentModerationSessionBlock, *pagination.PaginationResult, error) {
	if s == nil || s.repo == nil {
		return nil, nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "内容审计仓储不可用")
	}
	if filter.Pagination.Page <= 0 {
		filter.Pagination.Page = 1
	}
	if filter.Pagination.PageSize <= 0 {
		filter.Pagination.PageSize = 20
	}
	if filter.Pagination.PageSize > 100 {
		filter.Pagination.PageSize = 100
	}
	if filter.Pagination.SortOrder == "" {
		filter.Pagination.SortOrder = pagination.SortOrderDesc
	}
	return s.repo.ListSessionBlocks(ctx, filter)
}

func (s *ContentModerationService) DeleteSessionBlock(ctx context.Context, blockKey string) (*ContentModerationDeleteSessionBlockResult, error) {
	blockKey = strings.ToLower(strings.TrimSpace(blockKey))
	if len(blockKey) != 64 {
		return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_SESSION_BLOCK_KEY", "会话封禁键无效")
	}
	for _, r := range blockKey {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return nil, infraerrors.BadRequest("INVALID_CONTENT_MODERATION_SESSION_BLOCK_KEY", "会话封禁键无效")
		}
	}
	if s == nil || s.repo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "内容审计仓储不可用")
	}
	existing, err := s.repo.GetSessionBlockByKey(ctx, blockKey)
	if err != nil {
		return nil, fmt.Errorf("get content moderation session block: %w", err)
	}
	deleted, err := s.repo.DeleteSessionBlockByKey(ctx, blockKey)
	if err != nil {
		return nil, fmt.Errorf("delete content moderation session block: %w", err)
	}
	if s.hashCache != nil {
		if _, err := s.hashCache.DeleteBlockedSession(ctx, blockKey); err != nil {
			return nil, fmt.Errorf("delete content moderation session block cache: %w", err)
		}
	}
	sessionID := ""
	if existing != nil {
		sessionID = existing.SessionID
	}
	return &ContentModerationDeleteSessionBlockResult{
		BlockKey:  blockKey,
		SessionID: sessionID,
		Deleted:   deleted > 0 || existing != nil,
	}, nil
}

func (s *ContentModerationService) ClearSessionBlocks(ctx context.Context) (*ContentModerationClearSessionBlocksResult, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.InternalServer("CONTENT_MODERATION_REPOSITORY_UNAVAILABLE", "内容审计仓储不可用")
	}
	deleted, err := s.repo.ClearSessionBlocks(ctx)
	if err != nil {
		return nil, fmt.Errorf("clear content moderation session blocks: %w", err)
	}
	if s.hashCache != nil {
		if _, err := s.hashCache.ClearBlockedSessions(ctx); err != nil {
			return nil, fmt.Errorf("clear content moderation session block cache: %w", err)
		}
	}
	return &ContentModerationClearSessionBlocksResult{Deleted: deleted}, nil
}

func (s *ContentModerationService) cleanupExpiredSessionBlocks(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	if _, err := s.repo.DeleteExpiredSessionBlocks(ctx, time.Now()); err != nil {
		slog.Warn("content_moderation.session_block_cleanup_failed", contentModerationRuntimeExceptionAttrs("session_block_cleanup_failed", err)...)
	}
}
