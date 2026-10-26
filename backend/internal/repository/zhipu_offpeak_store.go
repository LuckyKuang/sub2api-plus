package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
)

type ZhipuOffPeakRepository struct{ db *sql.DB }

func NewZhipuOffPeakRepository(db *sql.DB) service.ZhipuOffPeakStore {
	return &ZhipuOffPeakRepository{db: db}
}

const offPeakColumns = `id, account_id, owner_generation, task_id, ticket_id, state,
 key_id, encrypted_snapshot, position, retired, closed, fence, lease_id,
 lease_expires_at, last_used_at, retain_until, settlement_pending,
 next_attempt_at, attempts, last_error`

func scanOffPeak(row interface{ Scan(...any) error }) (service.ZhipuOffPeakRecord, error) {
	var r service.ZhipuOffPeakRecord
	var lease sql.NullTime
	var position sql.NullInt64
	err := row.Scan(&r.ID, &r.AccountID, &r.Generation, &r.TaskID, &r.TicketID, &r.State,
		&r.KeyID, &r.Snapshot, &position, &r.Retired, &r.Closed, &r.Fence, &r.LeaseID,
		&lease, &r.LastUsed, &r.RetainUntil, &r.SettlementPending, &r.NextAttempt, &r.Attempts, &r.LastError)
	if lease.Valid {
		r.LeaseUntil = lease.Time
	}
	if position.Valid {
		p := int(position.Int64)
		r.Position = &p
	}
	return r, err
}

func (r *ZhipuOffPeakRepository) Current(ctx context.Context, accountID int64) (service.ZhipuOffPeakRecord, error) {
	value, err := scanOffPeak(r.db.QueryRowContext(ctx, `SELECT `+offPeakColumns+` FROM zhipu_offpeak_tickets WHERE account_id=$1 AND NOT retired AND NOT closed`, accountID))
	if errors.Is(err, sql.ErrNoRows) {
		return service.ZhipuOffPeakRecord{}, service.ErrOffPeakRecordNotFound
	}
	return value, err
}

// Terminate fences exactly the inspected segment and commits retirement intent.
// Existing request leases drain normally; the outbox settles with its snapshot.
func (r *ZhipuOffPeakRepository) Terminate(ctx context.Context, accountID int64, id, generation string) (service.ZhipuOffPeakRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('zhipu_offpeak:' || $1::text, 0))`, accountID); err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	current, err := scanOffPeak(tx.QueryRowContext(ctx, `SELECT `+offPeakColumns+` FROM zhipu_offpeak_tickets WHERE id=$1 AND account_id=$2 AND owner_generation=$3 FOR UPDATE`, id, accountID, generation))
	if errors.Is(err, sql.ErrNoRows) {
		return service.ZhipuOffPeakRecord{}, service.ErrOffPeakFenceLost
	}
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	if !current.Retired && !current.Closed {
		current, err = scanOffPeak(tx.QueryRowContext(ctx, `UPDATE zhipu_offpeak_tickets SET retired=TRUE,
 settlement_pending=(ticket_id<>''), fence=fence+1, lease_id='', lease_expires_at=NULL,
 next_attempt_at=clock_timestamp(), updated_at=clock_timestamp() WHERE id=$1 RETURNING `+offPeakColumns, id))
		if err != nil {
			return service.ZhipuOffPeakRecord{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	return current, nil
}

// Open serializes generations, checks the live credential/route owner, and
// retires the old generation atomically. Old in-flight leases remain intact.
func (r *ZhipuOffPeakRepository) Open(ctx context.Context, owner *service.Account, candidate service.ZhipuOffPeakRecord) (service.ZhipuOffPeakRecord, error) {
	if owner == nil || owner.ID != candidate.AccountID || candidate.ID == "" || candidate.Generation == "" || len(candidate.Snapshot) == 0 {
		return service.ZhipuOffPeakRecord{}, errors.New("invalid off-peak owner snapshot")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('zhipu_offpeak:' || $1::text, 0))`, owner.ID); err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	credentials, err := json.Marshal(normalizeJSONMap(owner.Credentials))
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	args := []any{owner.ID, owner.Platform, owner.Type, string(credentials), owner.ProxyID, owner.Status, owner.CreatedAt}
	predicate, args, err := cnProxyOwnerPredicate(owner, args)
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a WHERE a.id=$1 AND a.platform=$2 AND a.type=$3
 AND a.credentials=$4::jsonb AND a.proxy_id IS NOT DISTINCT FROM $5 AND a.status=$6
 AND a.created_at=$7 AND a.deleted_at IS NULL `+predicate+` FOR SHARE OF a`, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ZhipuOffPeakRecord{}, service.ErrOffPeakFenceLost
	}
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	current, err := scanOffPeak(tx.QueryRowContext(ctx, `SELECT `+offPeakColumns+` FROM zhipu_offpeak_tickets WHERE account_id=$1 AND NOT retired AND NOT closed FOR UPDATE`, owner.ID))
	if err == nil && current.Generation == candidate.Generation {
		if err = tx.Commit(); err != nil {
			return service.ZhipuOffPeakRecord{}, err
		}
		return current, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return service.ZhipuOffPeakRecord{}, err
	}
	if current.ID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE zhipu_offpeak_tickets SET retired=TRUE, fence=fence+1, lease_id='', lease_expires_at=NULL,
 settlement_pending=(ticket_id<>''), next_attempt_at=clock_timestamp(), updated_at=clock_timestamp() WHERE id=$1`, current.ID)
		if err != nil {
			return service.ZhipuOffPeakRecord{}, err
		}
	}
	created, err := scanOffPeak(tx.QueryRowContext(ctx, `INSERT INTO zhipu_offpeak_tickets
 (id, account_id, owner_generation, task_id, key_id, encrypted_snapshot, retain_until)
 VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+offPeakColumns,
		candidate.ID, candidate.AccountID, candidate.Generation, candidate.TaskID, candidate.KeyID, candidate.Snapshot, candidate.RetainUntil))
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	if err = tx.Commit(); err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	return created, nil
}

func (r *ZhipuOffPeakRepository) Claim(ctx context.Context, id string, settlement bool, idle, ttl time.Duration) (service.ZhipuOffPeakRecord, error) {
	if ttl <= 0 {
		return service.ZhipuOffPeakRecord{}, service.ErrOffPeakLeaseBusy
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()
	// Every admission/renewal/settlement locks this parent row first. The
	// subsequent statement gets a fresh READ COMMITTED snapshot of leases.
	current, err := scanOffPeak(tx.QueryRowContext(ctx, `SELECT `+offPeakColumns+` FROM zhipu_offpeak_tickets WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if !settlement {
				return service.ZhipuOffPeakRecord{}, service.ErrOffPeakFenceLost
			}
			return service.ZhipuOffPeakRecord{}, service.ErrOffPeakLeaseBusy
		}
		return service.ZhipuOffPeakRecord{}, err
	}
	if !settlement && (current.Retired || current.Closed) {
		return service.ZhipuOffPeakRecord{}, service.ErrOffPeakFenceLost
	}
	criteria := ` AND NOT retired AND NOT settlement_pending AND retain_until>clock_timestamp() AND (state<>'new' OR next_attempt_at<=clock_timestamp())`
	update := `, last_used_at=clock_timestamp()`
	if settlement {
		criteria = ` AND NOT EXISTS (SELECT 1 FROM zhipu_offpeak_request_leases l WHERE l.ticket_record_id=t.id AND l.expires_at>clock_timestamp())
 AND (retain_until<=clock_timestamp() OR (ticket_id<>'' AND next_attempt_at<=clock_timestamp() AND (retired OR settlement_pending OR last_used_at<=clock_timestamp()-make_interval(secs=>$4))))`
		update = `, settlement_pending=(ticket_id<>''), attempts=attempts+1`
	}
	query := `UPDATE zhipu_offpeak_tickets t SET fence=fence+1, lease_id=$2,
 lease_expires_at=clock_timestamp()+make_interval(secs=>$3), updated_at=clock_timestamp()` + update + `
 WHERE id=$1 AND NOT closed AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp())` + criteria + ` RETURNING ` + offPeakColumns
	args := []any{id, uuid.NewString(), ttl.Seconds()}
	if settlement {
		args = append(args, idle.Seconds())
	}
	value, err := scanOffPeak(tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		// Preserve the locked segment's observation for concurrent waiters. A
		// quota rejection committed by another instance is not a queued ticket,
		// and observing it must not reopen or acquire a replacement segment.
		return current, service.ErrOffPeakLeaseBusy
	}
	if err != nil {
		return value, err
	}
	if err = tx.Commit(); err != nil {
		return service.ZhipuOffPeakRecord{}, err
	}
	return value, nil
}

func (r *ZhipuOffPeakRepository) Save(ctx context.Context, record service.ZhipuOffPeakRecord) error {
	result, err := r.db.ExecContext(ctx, `UPDATE zhipu_offpeak_tickets SET ticket_id=$4,state=$5,position=$6,
 settlement_pending=CASE WHEN $7 THEN FALSE ELSE settlement_pending OR $8 END, closed=$7,
 encrypted_snapshot=CASE WHEN $7 THEN ''::bytea ELSE encrypted_snapshot END,
 next_attempt_at=$9,last_error=$10,lease_id='',lease_expires_at=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND fence=$2 AND lease_id=$3 AND lease_expires_at>clock_timestamp() AND NOT closed`,
		record.ID, record.Fence, record.LeaseID, record.TicketID, record.State, record.Position,
		record.Closed, record.SettlementPending, record.NextAttempt, record.LastError)
	return offPeakAffected(result, err)
}

func offPeakAffected(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrOffPeakFenceLost
	}
	return nil
}

func (r *ZhipuOffPeakRepository) BeginTake(ctx context.Context, record service.ZhipuOffPeakRecord) error {
	res, err := r.db.ExecContext(ctx, `UPDATE zhipu_offpeak_tickets SET state='uncertain',updated_at=clock_timestamp()
 WHERE id=$1 AND fence=$2 AND lease_id=$3 AND lease_expires_at>clock_timestamp() AND state='new' AND NOT retired AND NOT closed`, record.ID, record.Fence, record.LeaseID)
	return offPeakAffected(res, err)
}

func (r *ZhipuOffPeakRepository) Admit(ctx context.Context, record service.ZhipuOffPeakRecord, owner *service.Account, requestID string, ttl time.Duration) error {
	if owner == nil || owner.ID != record.AccountID {
		return service.ErrOffPeakFenceLost
	}
	if record.State != "ready" && record.State != "active" {
		return service.ErrOffPeakFenceLost
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	credentials, err := json.Marshal(normalizeJSONMap(owner.Credentials))
	if err != nil {
		return err
	}
	args := []any{owner.ID, owner.Platform, owner.Type, string(credentials), owner.ProxyID, owner.Status, owner.CreatedAt}
	predicate, args, err := cnProxyOwnerPredicate(owner, args)
	if err != nil {
		return err
	}
	var accountID int64
	err = tx.QueryRowContext(ctx, `SELECT a.id FROM accounts a WHERE a.id=$1 AND a.platform=$2 AND a.type=$3
 AND a.credentials=$4::jsonb AND a.proxy_id IS NOT DISTINCT FROM $5 AND a.status=$6 AND a.created_at=$7 AND a.deleted_at IS NULL `+predicate+` FOR SHARE OF a`, args...).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrOffPeakFenceLost
	}
	if err != nil {
		return err
	}
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM zhipu_offpeak_tickets WHERE id=$1 AND fence=$2 AND lease_id=$3 AND lease_expires_at>clock_timestamp()
 AND NOT retired AND NOT closed AND NOT settlement_pending AND ticket_id=$4 AND retain_until>clock_timestamp() FOR UPDATE`, record.ID, record.Fence, record.LeaseID, record.TicketID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrOffPeakFenceLost
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO zhipu_offpeak_request_leases(ticket_record_id,request_id,expires_at) VALUES($1,$2,clock_timestamp()+make_interval(secs=>$3))`, id, requestID, ttl.Seconds())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE zhipu_offpeak_tickets SET state=$2,position=NULL,lease_id='',lease_expires_at=NULL,last_used_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, id, record.State)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ZhipuOffPeakRepository) RenewRequest(ctx context.Context, id, requestID string, ttl time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var locked string
	err = tx.QueryRowContext(ctx, `SELECT id FROM zhipu_offpeak_tickets WHERE id=$1 AND NOT closed AND NOT(settlement_pending AND lease_id<>'') FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrOffPeakFenceLost
	}
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE zhipu_offpeak_request_leases SET expires_at=clock_timestamp()+make_interval(secs=>$3)
 WHERE ticket_record_id=$1 AND request_id=$2 AND expires_at>clock_timestamp()
 AND EXISTS(SELECT 1 FROM zhipu_offpeak_tickets t WHERE t.id=$1 AND NOT t.closed)`, id, requestID, ttl.Seconds())
	if err = offPeakAffected(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ZhipuOffPeakRepository) ReleaseRequest(ctx context.Context, id, requestID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Renewal and settlement also lock the ticket before its request leases.
	// Keep release idempotent when the ticket or lease has already gone away.
	var locked string
	err = tx.QueryRowContext(ctx, `SELECT id FROM zhipu_offpeak_tickets WHERE id=$1 FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH released AS (DELETE FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1 AND request_id=$2 RETURNING ticket_record_id)
 UPDATE zhipu_offpeak_tickets SET last_used_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id IN(SELECT ticket_record_id FROM released)`, id, requestID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *ZhipuOffPeakRepository) Due(ctx context.Context, idle time.Duration, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid off-peak sweep limit")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT t.id FROM zhipu_offpeak_tickets t WHERE NOT closed
 AND (lease_expires_at IS NULL OR lease_expires_at<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM zhipu_offpeak_request_leases l WHERE l.ticket_record_id=t.id AND l.expires_at>clock_timestamp())
 AND (retain_until<=clock_timestamp() OR (ticket_id<>'' AND next_attempt_at<=clock_timestamp() AND (retired OR settlement_pending OR last_used_at<=clock_timestamp()-make_interval(secs=>$1))))
 ORDER BY next_attempt_at,id LIMIT $2`, idle.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// A Take response arriving after its lease expired is cleanup evidence only.
// Its original row/snapshot is retained; it never acquires a newer generation.
func (r *ZhipuOffPeakRepository) RegisterLateTicket(ctx context.Context, record service.ZhipuOffPeakRecord) error {
	if record.TicketID == "" {
		return errors.New("late off-peak evidence has no ticket")
	}
	_, err := r.db.ExecContext(ctx, `UPDATE zhipu_offpeak_tickets SET ticket_id=$4,state='settling',retired=TRUE,
 settlement_pending=TRUE,next_attempt_at=clock_timestamp(),fence=fence+1,lease_id='',lease_expires_at=NULL,updated_at=clock_timestamp()
 WHERE id=$1 AND account_id=$2 AND owner_generation=$3 AND task_id=$5 AND ticket_id='' AND NOT closed`,
		record.ID, record.AccountID, record.Generation, record.TicketID, record.TaskID)
	return err
}
