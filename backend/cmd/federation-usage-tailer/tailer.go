package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/federationusagecursor"
	"github.com/LuckyKuang/sub2api-plus/ent/usagelog"
	"github.com/LuckyKuang/sub2api-plus/internal/federation"
)

type tailer struct {
	db       *dbent.Client
	mainland *mainlandClient
	cfg      tailerConfig
	logger   *slog.Logger
}

func (t *tailer) runOnce(ctx context.Context) error {
	cursor, err := t.loadOrCreateCursor(ctx)
	if err != nil {
		return fmt.Errorf("load cursor: %w", err)
	}
	if cursor.NextRetryAt != nil && time.Now().Before(*cursor.NextRetryAt) {
		return nil // still backing off the blocking row
	}

	rows, err := t.db.UsageLog.Query().
		Where(usagelog.IDGT(cursor.LastDeliveredID)).
		Order(dbent.Asc(usagelog.FieldID)).
		Limit(t.cfg.BatchSize).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query usage_log: %w", err)
	}

	for _, row := range rows {
		if err := t.deliverOne(ctx, row); err != nil {
			t.markBlocked(ctx, cursor, err)
			return nil // strict order: stop the batch, do not touch later rows
		}
		if err := t.advanceCursor(ctx, cursor, row.ID); err != nil {
			return fmt.Errorf("advance cursor: %w", err)
		}
	}
	return nil
}

// deliverOne bills one usage_log row against the mainland balance. Rows
// with nothing to bill (zero/negative cost, or a user that no longer
// exists -- soft-deleted users are excluded by the default query filter)
// are treated as delivered without a network call: skipping them loses no
// money, unlike skipping a row that failed to bill a real cost. A user
// mainland definitively doesn't know is a local-only user of this
// deployment: its usage was already billed to the local balance, so the row
// is skipped too. Lookup failures still block the cursor.
func (t *tailer) deliverOne(ctx context.Context, row *dbent.UsageLog) error {
	if row.ActualCost <= 0 {
		return nil
	}
	localUser, err := t.db.User.Get(ctx, row.UserID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil
		}
		return federation.RetryableErr(fmt.Errorf("load local user %d: %w", row.UserID, err))
	}

	mainlandID, found, err := t.mainland.resolveUserID(ctx, localUser.Email)
	if err != nil {
		return err
	}
	if !found {
		t.logger.Info("federation-usage-tailer: skipping local-only user (no mainland account)",
			"usage_log_id", row.ID, "user_id", row.UserID)
		return nil
	}

	// The "-od" suffix marks the overdraft-capable request body. Mainland
	// fingerprints the payload per key, so reusing the old key for a row that
	// was refused before allow_negative existed would be a key conflict.
	idempotencyKey := fmt.Sprintf("federation-usage-%d-od", row.ID)
	notes := fmt.Sprintf("federation usage_log id=%d", row.ID)
	if err := t.mainland.subtractBalance(ctx, idempotencyKey, mainlandID, row.ActualCost, notes); err != nil {
		return err
	}
	t.logger.Info("federation-usage-tailer: delivered", "usage_log_id", row.ID, "cost", row.ActualCost, "mainland_user_id", mainlandID)

	// Best-effort: the balance subtract already succeeded, which is the
	// money-correctness-critical part. A failure here just means mainland's
	// next balance.snapshot push carries a stale watermark -- see
	// mainlandClient.bumpWatermark's doc comment -- so it must not block
	// this row from being considered delivered.
	if err := t.mainland.bumpWatermark(ctx, idempotencyKey+"-watermark", mainlandID, row.ID); err != nil {
		t.logger.Warn("federation-usage-tailer: watermark bump failed, balance subtract still applied",
			"usage_log_id", row.ID, "mainland_user_id", mainlandID, "error", err)
	}
	return nil
}

func (t *tailer) loadOrCreateCursor(ctx context.Context) (*dbent.FederationUsageCursor, error) {
	cursor, err := t.db.FederationUsageCursor.Query().
		Where(federationusagecursor.ScopeEQ(t.cfg.CursorScope)).
		Only(ctx)
	if err == nil {
		return cursor, nil
	}
	if !dbent.IsNotFound(err) {
		return nil, err
	}
	// A new cursor starts at the current end of usage_log, not at 0: usage
	// recorded before federation was already paid from this deployment's own
	// balance, and replaying that history would bill it to mainland again.
	var agg []struct {
		Max int64 `json:"max"`
	}
	if err := t.db.UsageLog.Query().
		Aggregate(dbent.As(dbent.Max(usagelog.FieldID), "max")).
		Scan(ctx, &agg); err != nil {
		return nil, fmt.Errorf("find usage_log head: %w", err)
	}
	var start int64
	if len(agg) > 0 {
		start = agg[0].Max
	}
	t.logger.Info("federation-usage-tailer: new cursor starts at current usage_log head",
		"scope", t.cfg.CursorScope, "last_delivered_id", start)
	return t.db.FederationUsageCursor.Create().
		SetScope(t.cfg.CursorScope).
		SetLastDeliveredID(start).
		Save(ctx)
}

func (t *tailer) advanceCursor(ctx context.Context, cursor *dbent.FederationUsageCursor, deliveredID int64) error {
	updated, err := t.db.FederationUsageCursor.UpdateOneID(cursor.ID).
		SetLastDeliveredID(deliveredID).
		SetAttempts(0).
		ClearLastError().
		ClearNextRetryAt().
		Save(ctx)
	if err != nil {
		return err
	}
	*cursor = *updated
	return nil
}

func (t *tailer) markBlocked(ctx context.Context, cursor *dbent.FederationUsageCursor, cause error) {
	attempts := cursor.Attempts + 1
	next := time.Now().Add(federation.RetryBackoff(attempts))
	if _, err := t.db.FederationUsageCursor.UpdateOneID(cursor.ID).
		SetAttempts(attempts).
		SetLastError(federation.Truncate(cause.Error(), 2000)).
		SetNextRetryAt(next).
		Save(ctx); err != nil {
		t.logger.Error("federation-usage-tailer: mark blocked", "cursor_id", cursor.ID, "error", err)
		return
	}
	t.logger.Warn("federation-usage-tailer: blocked at usage_log id > last_delivered_id, retrying",
		"last_delivered_id", cursor.LastDeliveredID, "attempts", attempts, "next_retry_at", next, "cause", cause)
}
