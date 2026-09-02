package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

const (
	clientDisconnectRiskPendingMaxAge = 24 * time.Hour
)

type clientDisconnectRiskRepository struct {
	db *sql.DB
}

func NewClientDisconnectRiskRepository(db *sql.DB) service.ClientDisconnectRiskRepository {
	return &clientDisconnectRiskRepository{db: db}
}

func (r *clientDisconnectRiskRepository) Begin(ctx context.Context, input service.ClientDisconnectRiskBegin) (int64, error) {
	if input.UserID <= 0 || input.Generation <= 0 || strings.TrimSpace(input.RequestID) == "" {
		return 0, fmt.Errorf("invalid client disconnect risk begin input")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id = $1 AND deleted_at IS NULL FOR SHARE`, input.UserID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("lock client disconnect user: %w", err)
	}
	if status != service.StatusActive {
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return 0, nil
	}

	stateResult, err := tx.ExecContext(ctx, `
INSERT INTO client_disconnect_risk_states (user_id, generation)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET
    generation = EXCLUDED.generation,
    next_sequence = CASE WHEN client_disconnect_risk_states.generation = EXCLUDED.generation THEN client_disconnect_risk_states.next_sequence ELSE 0 END,
    processed_sequence = CASE WHEN client_disconnect_risk_states.generation = EXCLUDED.generation THEN client_disconnect_risk_states.processed_sequence ELSE 0 END,
    consecutive_count = CASE WHEN client_disconnect_risk_states.generation = EXCLUDED.generation THEN client_disconnect_risk_states.consecutive_count ELSE 0 END,
	updated_at = NOW()
WHERE client_disconnect_risk_states.generation <= EXCLUDED.generation`, input.UserID, input.Generation)
	if err != nil {
		return 0, fmt.Errorf("upsert client disconnect state: %w", err)
	}
	stateRows, err := stateResult.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read client disconnect state update result: %w", err)
	}
	if stateRows == 0 {
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	var sequence int64
	err = tx.QueryRowContext(ctx, `
SELECT next_sequence FROM client_disconnect_risk_states
WHERE user_id = $1 AND generation = $2
FOR UPDATE`, input.UserID, input.Generation).Scan(&sequence)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("lock client disconnect state: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
SELECT sequence FROM client_disconnect_risk_events
WHERE user_id = $1 AND generation = $2 AND request_id = $3`,
		input.UserID, input.Generation, input.RequestID).Scan(&sequence)
	if err == nil {
		if err = tx.Commit(); err != nil {
			return 0, err
		}
		return sequence, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read existing client disconnect sequence: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
UPDATE client_disconnect_risk_states
SET next_sequence = next_sequence + 1, updated_at = NOW()
WHERE user_id = $1 AND generation = $2
RETURNING next_sequence`, input.UserID, input.Generation).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("allocate client disconnect sequence: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO client_disconnect_risk_events
    (user_id, generation, sequence, request_id, api_key_id, protocol)
VALUES ($1, $2, $3, $4, NULLIF($5, 0), $6)`,
		input.UserID, input.Generation, sequence, input.RequestID, input.APIKeyID, input.Protocol)
	if err != nil {
		return 0, fmt.Errorf("insert client disconnect event: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return sequence, nil
}

func (r *clientDisconnectRiskRepository) Finalize(ctx context.Context, input service.ClientDisconnectRiskFinalize) (service.ClientDisconnectRiskResult, error) {
	result := service.ClientDisconnectRiskResult{}
	if input.UserID <= 0 || input.Generation <= 0 {
		return result, fmt.Errorf("invalid client disconnect risk finalize input")
	}
	if input.Sequence <= 0 {
		return result, nil
	}
	if input.Threshold < 1 || input.Threshold > 1000 {
		return result, fmt.Errorf("client disconnect threshold must be between 1 and 1000")
	}
	switch input.Outcome {
	case service.ClientDisconnectOutcomeCompleted, service.ClientDisconnectOutcomeDisconnected, service.ClientDisconnectOutcomeNeutral:
	default:
		return result, fmt.Errorf("invalid client disconnect outcome %q", input.Outcome)
	}
	completionStatus := strings.TrimSpace(input.CompletionStatus)
	usageSource := strings.TrimSpace(input.UsageSource)
	usageMissing := input.UsageMissing
	if completionStatus == "" {
		switch input.Outcome {
		case service.ClientDisconnectOutcomeCompleted:
			completionStatus, usageSource, usageMissing = "completed", "upstream_exact", false
		case service.ClientDisconnectOutcomeDisconnected:
			completionStatus, usageSource, usageMissing = "client_disconnected", "", true
		default:
			completionStatus, usageSource, usageMissing = "upstream_failed", "", true
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `
UPDATE client_disconnect_risk_events
SET outcome = $4, threshold = $5,
    enforce = $6 AND EXISTS (
        SELECT 1 FROM users WHERE id = $1 AND role <> 'admin' AND deleted_at IS NULL
    ),
    completion_status = $7, usage_source = NULLIF($8, ''), usage_missing = $9,
    finalized_at = NOW()
WHERE user_id = $1 AND generation = $2 AND sequence = $3 AND outcome = 'pending'`,
		input.UserID, input.Generation, input.Sequence, input.Outcome, input.Threshold, input.Enforce,
		completionStatus, usageSource, usageMissing); err != nil {
		return result, fmt.Errorf("finalize client disconnect event: %w", err)
	}

	var processed int64
	var streak int
	err = tx.QueryRowContext(ctx, `
SELECT processed_sequence, consecutive_count
FROM client_disconnect_risk_states
WHERE user_id = $1 AND generation = $2
FOR UPDATE`, input.UserID, input.Generation).Scan(&processed, &streak)
	if errors.Is(err, sql.ErrNoRows) {
		if err = tx.Commit(); err != nil {
			return result, err
		}
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("lock client disconnect state: %w", err)
	}

	for {
		next := processed + 1
		var outcome service.ClientDisconnectOutcome
		var eventThreshold sql.NullInt64
		var eventEnforce sql.NullBool
		var acceptedAt time.Time
		err = tx.QueryRowContext(ctx, `
SELECT outcome, threshold, enforce, accepted_at FROM client_disconnect_risk_events
WHERE user_id = $1 AND generation = $2 AND sequence = $3`,
			input.UserID, input.Generation, next).Scan(&outcome, &eventThreshold, &eventEnforce, &acceptedAt)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("read ordered client disconnect event: %w", err)
		}
		if outcome == "pending" {
			if time.Since(acceptedAt) < clientDisconnectRiskPendingMaxAge {
				break
			}
			outcome = service.ClientDisconnectOutcomeNeutral
			if _, err = tx.ExecContext(ctx, `
UPDATE client_disconnect_risk_events
SET outcome = $4, threshold = $5, enforce = FALSE,
    completion_status = 'upstream_timeout', usage_missing = TRUE, finalized_at = NOW()
WHERE user_id = $1 AND generation = $2 AND sequence = $3 AND outcome = 'pending'`,
				input.UserID, input.Generation, next, outcome, input.Threshold); err != nil {
				return result, fmt.Errorf("expire stale client disconnect event: %w", err)
			}
			eventThreshold = sql.NullInt64{Int64: int64(input.Threshold), Valid: true}
			eventEnforce = sql.NullBool{Bool: false, Valid: true}
		}

		if eventEnforce.Valid && eventEnforce.Bool {
			switch outcome {
			case service.ClientDisconnectOutcomeCompleted:
				streak = 0
			case service.ClientDisconnectOutcomeDisconnected:
				streak++
			case service.ClientDisconnectOutcomeNeutral:
				// A neutral result neither increments nor breaks the consecutive streak.
			default:
				return result, fmt.Errorf("invalid client disconnect outcome %q", outcome)
			}
		}

		autoBanned := false
		if eventEnforce.Valid && eventEnforce.Bool && eventThreshold.Valid &&
			outcome == service.ClientDisconnectOutcomeDisconnected && streak >= int(eventThreshold.Int64) {
			var bannedUserID int64
			err = tx.QueryRowContext(ctx, `
UPDATE users
SET status = 'disabled', updated_at = NOW()
WHERE id = $1 AND role <> 'admin' AND status = 'active' AND deleted_at IS NULL
RETURNING id`, input.UserID).Scan(&bannedUserID)
			if err == nil {
				autoBanned = true
				result.AutoBanned = true
			} else if !errors.Is(err, sql.ErrNoRows) {
				return result, fmt.Errorf("auto-ban client disconnect user: %w", err)
			}
		}

		if _, err = tx.ExecContext(ctx, `
UPDATE client_disconnect_risk_events
SET consecutive_after = $4, auto_banned = $5
WHERE user_id = $1 AND generation = $2 AND sequence = $3`,
			input.UserID, input.Generation, next, streak, autoBanned); err != nil {
			return result, fmt.Errorf("annotate client disconnect event: %w", err)
		}
		processed = next
	}

	if _, err = tx.ExecContext(ctx, `
UPDATE client_disconnect_risk_states
SET processed_sequence = $3, consecutive_count = $4, updated_at = NOW()
WHERE user_id = $1 AND generation = $2`, input.UserID, input.Generation, processed, streak); err != nil {
		return result, fmt.Errorf("update client disconnect state: %w", err)
	}
	result.ConsecutiveCount = streak
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (r *clientDisconnectRiskRepository) ClearUser(ctx context.Context, userID int64) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE client_disconnect_risk_states
SET processed_sequence = next_sequence, consecutive_count = 0, updated_at = NOW()
WHERE user_id = $1`, userID)
	return err
}

func (r *clientDisconnectRiskRepository) ListEvents(ctx context.Context, filter service.ClientDisconnectRiskEventFilter) ([]service.ClientDisconnectRiskEvent, int64, error) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 6)
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filter.UserID > 0 {
		add("user_id = $%d", filter.UserID)
	}
	if filter.APIKeyID > 0 {
		add("api_key_id = $%d", filter.APIKeyID)
	}
	if filter.Outcome != "" {
		add("outcome = $%d", filter.Outcome)
	}
	if filter.CompletionStatus == "pending" {
		conditions = append(conditions, "completion_status IS NULL")
	} else if filter.CompletionStatus != "" {
		add("completion_status = $%d", filter.CompletionStatus)
	}
	if filter.UsageMissing != nil {
		add("usage_missing = $%d", *filter.UsageMissing)
	}
	if filter.AutoBanned != nil {
		add("auto_banned = $%d", *filter.AutoBanned)
	}
	where := strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM client_disconnect_risk_events WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count client disconnect events: %w", err)
	}
	offset := (filter.Page - 1) * filter.PageSize
	queryArgs := append(append([]any(nil), args...), filter.PageSize, offset)
	rows, err := r.db.QueryContext(ctx, `
SELECT user_id, api_key_id, request_id, protocol, generation, sequence,
       outcome, completion_status, usage_source, usage_missing,
       consecutive_after, threshold, enforce, auto_banned, accepted_at, finalized_at
FROM client_disconnect_risk_events
WHERE `+where+fmt.Sprintf(" ORDER BY accepted_at DESC, user_id DESC, generation DESC, sequence DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list client disconnect events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]service.ClientDisconnectRiskEvent, 0, filter.PageSize)
	for rows.Next() {
		var event service.ClientDisconnectRiskEvent
		var apiKeyID sql.NullInt64
		var completionStatus, usageSource sql.NullString
		var consecutiveAfter, threshold sql.NullInt64
		var enforce sql.NullBool
		var finalizedAt sql.NullTime
		if err := rows.Scan(&event.UserID, &apiKeyID, &event.RequestID, &event.Protocol,
			&event.Generation, &event.Sequence, &event.Outcome, &completionStatus, &usageSource,
			&event.UsageMissing, &consecutiveAfter, &threshold, &enforce, &event.AutoBanned,
			&event.AcceptedAt, &finalizedAt); err != nil {
			return nil, 0, fmt.Errorf("scan client disconnect event: %w", err)
		}
		if apiKeyID.Valid {
			value := apiKeyID.Int64
			event.APIKeyID = &value
		}
		if completionStatus.Valid {
			event.CompletionStatus = completionStatus.String
		} else {
			event.CompletionStatus = "pending"
		}
		if usageSource.Valid {
			value := usageSource.String
			event.UsageSource = &value
		}
		if consecutiveAfter.Valid {
			value := int(consecutiveAfter.Int64)
			event.ConsecutiveAfter = &value
		}
		if threshold.Valid {
			value := int(threshold.Int64)
			event.Threshold = &value
		}
		if enforce.Valid {
			value := enforce.Bool
			event.Enforce = &value
		}
		if finalizedAt.Valid {
			value := finalizedAt.Time
			event.FinalizedAt = &value
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate client disconnect events: %w", err)
	}
	return events, total, nil
}
