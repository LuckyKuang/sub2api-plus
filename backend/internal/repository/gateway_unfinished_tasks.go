package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

type gatewayUnfinishedTaskRepository struct{ db *sql.DB }

func NewGatewayUnfinishedTaskRepository(db *sql.DB) service.GatewayUnfinishedTaskRepository {
	return &gatewayUnfinishedTaskRepository{db: db}
}

func (r *gatewayUnfinishedTaskRepository) Reserve(ctx context.Context, reservation *service.GatewayTaskReservation, limit int) error {
	if r == nil || r.db == nil || limit <= 0 {
		return service.ErrGatewayUnfinishedTaskUnavailable
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Every kind and API key shares this lock, including separate instances.
	var userID int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE", reservation.Owner.UserID).Scan(&userID); err != nil {
		return service.ErrGatewayUnfinishedTaskUnavailable
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM gateway_unfinished_tasks WHERE user_id=$1 AND released_at IS NULL", userID).Scan(&count); err != nil {
		return err
	}
	if count >= limit {
		return service.ErrGatewayUnfinishedTaskLimit
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO gateway_unfinished_tasks (reservation_id,user_id,api_key_id,kind) VALUES ($1,$2,$3,$4)", reservation.ID, userID, reservation.Owner.APIKeyID, reservation.Kind); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *gatewayUnfinishedTaskRepository) Bind(ctx context.Context, reservation *service.GatewayTaskReservation, taskID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE gateway_unfinished_tasks SET task_id=$5, state='bound', updated_at=NOW()
WHERE reservation_id=$1 AND user_id=$2 AND api_key_id=$3 AND kind=$4 AND released_at IS NULL AND (task_id IS NULL OR task_id=$5)`, reservation.ID, reservation.Owner.UserID, reservation.Owner.APIKeyID, reservation.Kind, taskID)
	return gatewayReservationMutation(result, err)
}

func (r *gatewayUnfinishedTaskRepository) MarkUncertain(ctx context.Context, reservation *service.GatewayTaskReservation, reason string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE gateway_unfinished_tasks SET state='uncertain',reason=$5,updated_at=NOW()
WHERE reservation_id=$1 AND user_id=$2 AND api_key_id=$3 AND kind=$4 AND released_at IS NULL`, reservation.ID, reservation.Owner.UserID, reservation.Owner.APIKeyID, reservation.Kind, reason)
	return gatewayReservationMutation(result, err)
}

func (r *gatewayUnfinishedTaskRepository) Release(ctx context.Context, reservation *service.GatewayTaskReservation, reason string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE gateway_unfinished_tasks SET state='released',reason=$5,released_at=COALESCE(released_at,NOW()),updated_at=NOW()
WHERE reservation_id=$1 AND user_id=$2 AND api_key_id=$3 AND kind=$4`, reservation.ID, reservation.Owner.UserID, reservation.Owner.APIKeyID, reservation.Kind, reason)
	return gatewayReservationMutation(result, err)
}

func (r *gatewayUnfinishedTaskRepository) ReleaseTask(ctx context.Context, owner service.GatewayTaskOwner, kind, taskID, reason string) error {
	// Zero rows permits historical tasks created before this persistent cap.
	_, err := r.db.ExecContext(ctx, `UPDATE gateway_unfinished_tasks SET state='released',reason=$5,released_at=COALESCE(released_at,NOW()),updated_at=NOW()
WHERE user_id=$1 AND api_key_id=$2 AND kind=$3 AND task_id=$4
AND (state <> 'uncertain' OR $5 IN ('provider_terminal','task_completed','task_cancelled_confirmed'))`, owner.UserID, owner.APIKeyID, kind, taskID, reason)
	return err
}

func gatewayReservationMutation(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("unfinished task mutation returned no result")
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrGatewayTaskReservationConflict
	}
	return nil
}

var _ service.GatewayUnfinishedTaskRepository = (*gatewayUnfinishedTaskRepository)(nil)
