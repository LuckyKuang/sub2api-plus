package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

func (r *contentModerationRepository) UpsertSessionBlock(ctx context.Context, block *service.ContentModerationSessionBlock) error {
	if r == nil || r.db == nil || block == nil || strings.TrimSpace(block.BlockKey) == "" || strings.TrimSpace(block.SessionID) == "" {
		return nil
	}
	err := r.db.QueryRowContext(ctx, `
INSERT INTO content_moderation_session_blocks (
    block_key, session_id, user_id, api_key_id, request_id, endpoint, protocol, model,
    highest_category, highest_score, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11
)
ON CONFLICT (block_key) DO UPDATE SET
    session_id = EXCLUDED.session_id,
    user_id = EXCLUDED.user_id,
    api_key_id = EXCLUDED.api_key_id,
    request_id = EXCLUDED.request_id,
    endpoint = EXCLUDED.endpoint,
    protocol = EXCLUDED.protocol,
    model = EXCLUDED.model,
    highest_category = EXCLUDED.highest_category,
    highest_score = EXCLUDED.highest_score,
    expires_at = CASE
        WHEN content_moderation_session_blocks.expires_at > NOW()
            THEN content_moderation_session_blocks.expires_at
        ELSE EXCLUDED.expires_at
    END,
    created_at = CASE
        WHEN content_moderation_session_blocks.expires_at > NOW()
            THEN content_moderation_session_blocks.created_at
        ELSE NOW()
    END
RETURNING id, created_at, expires_at`,
		block.BlockKey,
		block.SessionID,
		nullableInt64Ptr(block.UserID),
		nullableInt64Ptr(block.APIKeyID),
		block.RequestID,
		block.Endpoint,
		block.Protocol,
		block.Model,
		block.HighestCategory,
		block.HighestScore,
		block.ExpiresAt,
	).Scan(&block.ID, &block.CreatedAt, &block.ExpiresAt)
	if err != nil {
		return fmt.Errorf("upsert content moderation session block: %w", err)
	}
	return nil
}

func (r *contentModerationRepository) ListSessionBlocks(ctx context.Context, filter service.ContentModerationSessionBlockFilter) ([]service.ContentModerationSessionBlock, *pagination.PaginationResult, error) {
	where, args := buildContentModerationSessionBlockWhere(filter)
	whereSQL := "WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM content_moderation_session_blocks b LEFT JOIN users u ON u.id = b.user_id LEFT JOIN api_keys k ON k.id = b.api_key_id "+whereSQL, args...).Scan(&total); err != nil {
		return nil, nil, fmt.Errorf("count content moderation session blocks: %w", err)
	}
	params := filter.Pagination
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, params.Limit(), params.Offset())
	rows, err := r.db.QueryContext(ctx, `
SELECT
    b.id, b.block_key, b.session_id, b.user_id, COALESCE(u.email, ''), b.api_key_id, COALESCE(k.name, ''),
    b.request_id, b.endpoint, b.protocol, b.model, b.highest_category, b.highest_score, b.expires_at, b.created_at
FROM content_moderation_session_blocks b
LEFT JOIN users u ON u.id = b.user_id
LEFT JOIN api_keys k ON k.id = b.api_key_id `+whereSQL+`
ORDER BY b.created_at DESC, b.id DESC
LIMIT $`+fmt.Sprint(len(queryArgs)-1)+` OFFSET $`+fmt.Sprint(len(queryArgs)),
		queryArgs...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("list content moderation session blocks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.ContentModerationSessionBlock, 0)
	for rows.Next() {
		var item service.ContentModerationSessionBlock
		var userID, apiKeyID sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.BlockKey,
			&item.SessionID,
			&userID,
			&item.UserEmail,
			&apiKeyID,
			&item.APIKeyName,
			&item.RequestID,
			&item.Endpoint,
			&item.Protocol,
			&item.Model,
			&item.HighestCategory,
			&item.HighestScore,
			&item.ExpiresAt,
			&item.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("scan content moderation session block: %w", err)
		}
		if userID.Valid {
			v := userID.Int64
			item.UserID = &v
		}
		if apiKeyID.Valid {
			v := apiKeyID.Int64
			item.APIKeyID = &v
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate content moderation session blocks: %w", err)
	}
	return items, paginationResultFromTotal(total, params), nil
}

func (r *contentModerationRepository) GetSessionBlockByKey(ctx context.Context, blockKey string) (*service.ContentModerationSessionBlock, error) {
	blockKey = strings.TrimSpace(blockKey)
	if blockKey == "" {
		return nil, nil
	}
	var item service.ContentModerationSessionBlock
	var userID, apiKeyID sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
SELECT
    b.id, b.block_key, b.session_id, b.user_id, COALESCE(u.email, ''), b.api_key_id, COALESCE(k.name, ''),
    b.request_id, b.endpoint, b.protocol, b.model, b.highest_category, b.highest_score, b.expires_at, b.created_at
FROM content_moderation_session_blocks b
LEFT JOIN users u ON u.id = b.user_id
LEFT JOIN api_keys k ON k.id = b.api_key_id
WHERE b.block_key = $1`, blockKey).Scan(
		&item.ID,
		&item.BlockKey,
		&item.SessionID,
		&userID,
		&item.UserEmail,
		&apiKeyID,
		&item.APIKeyName,
		&item.RequestID,
		&item.Endpoint,
		&item.Protocol,
		&item.Model,
		&item.HighestCategory,
		&item.HighestScore,
		&item.ExpiresAt,
		&item.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get content moderation session block: %w", err)
	}
	if userID.Valid {
		v := userID.Int64
		item.UserID = &v
	}
	if apiKeyID.Valid {
		v := apiKeyID.Int64
		item.APIKeyID = &v
	}
	return &item, nil
}

func (r *contentModerationRepository) DeleteSessionBlockByKey(ctx context.Context, blockKey string) (int64, error) {
	blockKey = strings.TrimSpace(blockKey)
	if blockKey == "" {
		return 0, nil
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM content_moderation_session_blocks WHERE block_key = $1`, blockKey)
	if err != nil {
		return 0, fmt.Errorf("delete content moderation session block: %w", err)
	}
	deleted, _ := result.RowsAffected()
	return deleted, nil
}

func (r *contentModerationRepository) ClearSessionBlocks(ctx context.Context) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM content_moderation_session_blocks`)
	if err != nil {
		return 0, fmt.Errorf("clear content moderation session blocks: %w", err)
	}
	deleted, _ := result.RowsAffected()
	return deleted, nil
}

func (r *contentModerationRepository) CountActiveSessionBlocks(ctx context.Context, now time.Time) (int64, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_moderation_session_blocks WHERE expires_at > $1`, now).Scan(&count); err != nil {
		return 0, fmt.Errorf("count content moderation session blocks: %w", err)
	}
	return count, nil
}

func (r *contentModerationRepository) DeleteExpiredSessionBlocks(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM content_moderation_session_blocks WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired content moderation session blocks: %w", err)
	}
	deleted, _ := result.RowsAffected()
	return deleted, nil
}

func nullableInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func buildContentModerationSessionBlockWhere(filter service.ContentModerationSessionBlockFilter) ([]string, []any) {
	where := []string{"b.expires_at > NOW()"}
	args := make([]any, 0)
	add := func(expr string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(expr, len(args)))
	}
	if sessionID := strings.TrimSpace(filter.SessionID); sessionID != "" {
		add("b.session_id = $%d", sessionID)
	}
	if filter.UserID != nil && *filter.UserID > 0 {
		add("b.user_id = $%d", *filter.UserID)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		args = append(args, like, like, like, like)
		idx := len(args) - 3
		where = append(where, fmt.Sprintf("(b.session_id ILIKE $%d OR COALESCE(u.email, '') ILIKE $%d OR COALESCE(k.name, '') ILIKE $%d OR b.request_id ILIKE $%d)", idx, idx+1, idx+2, idx+3))
	}
	return where, args
}
