package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
)

var _ service.OpenAICodexUsageRepository = (*accountRepository)(nil)

// The owner row, its proxy, and an optional shadow target stay locked through
// the JSON merge. Snapshot ordering and observation-only scheduler behavior
// remain the same as UpdateExtra.
func (r *accountRepository) UpdateOpenAICodexUsageIfUnchanged(ctx context.Context, targetID int64, owner *service.Account, updates map[string]any) (bool, error) {
	if err := validateCredentialMutationOwner(owner); err != nil {
		return false, err
	}
	if owner.Platform != service.PlatformOpenAI || targetID <= 0 || len(updates) == 0 {
		return false, errors.New("invalid Codex observation owner or target")
	}
	if r == nil || r.client == nil {
		return false, errors.New("codex observation repository is unavailable")
	}
	expected, err := json.Marshal(normalizeJSONMap(owner.Credentials))
	if err != nil {
		return false, err
	}
	if owner.ProxyID == nil && owner.Proxy != nil || owner.ProxyID != nil && (owner.Proxy == nil || owner.Proxy.ID != *owner.ProxyID) {
		return false, errors.New("codex observation requires the hydrated bound proxy")
	}
	args := []any{owner.ID, owner.Platform, owner.Type, string(expected), owner.ProxyID, owner.Status}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txctx := dbent.NewTxContext(ctx, tx)
	var matched int64
	err = scanSingleRow(txctx, tx.Client(), `SELECT a.id FROM accounts a WHERE a.id=$1 AND a.platform=$2 AND a.type=$3 AND a.credentials=$4::jsonb AND a.proxy_id IS NOT DISTINCT FROM $5 AND a.status=$6 AND a.parent_account_id IS NULL AND a.deleted_at IS NULL FOR UPDATE OF a`, args, &matched)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Account and proxy are locked in separate statements. A repaired proxy's
	// committed row must be rechecked after its lock wait, never from an old
	// statement snapshot. This is the same account -> proxy -> group order used
	// by weekly observation and event/billing consumption.
	proxyMatches, err := lockAndMatchProbeProxyIdentity(txctx, tx.Client(), owner)
	if err != nil || !proxyMatches {
		return false, err
	}
	var saved sql.NullString
	var currentExtra []byte
	if targetID == owner.ID {
		err = scanSingleRow(txctx, tx.Client(), `SELECT extra->>'codex_usage_updated_at', extra FROM accounts WHERE id=$1 FOR UPDATE`, []any{targetID}, &saved, &currentExtra)
	} else {
		err = scanSingleRow(txctx, tx.Client(), `SELECT extra->>'codex_usage_updated_at', extra FROM accounts WHERE id=$1 AND parent_account_id=$2 AND platform=$3 AND deleted_at IS NULL FOR UPDATE`, []any{targetID, owner.ID, owner.Platform}, &saved, &currentExtra)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var savedExtra map[string]any
	if err := json.Unmarshal(currentExtra, &savedExtra); err != nil {
		return false, err
	}
	generation := service.CredentialStateGeneration(owner)
	if savedExtra[service.OpenAICodexUsageOwnerGenerationExtraKey] != generation {
		keys := make([]string, 0)
		for key := range savedExtra {
			if service.IsOpenAICodexObservationExtraKey(key) {
				keys = append(keys, key)
			}
		}
		if len(keys) > 0 {
			if _, err := tx.Client().ExecContext(txctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)-$2::text[] WHERE id=$1`, targetID, pq.Array(keys)); err != nil {
				return false, err
			}
		}
		saved = sql.NullString{}
	}
	localUpdates := make(map[string]any, len(updates)+1)
	for key, value := range updates {
		localUpdates[key] = value
	}
	localUpdates[service.OpenAICodexUsageOwnerGenerationExtraKey] = generation
	updates = localUpdates
	if sample, ok := updates["codex_usage_updated_at"].(string); ok {
		if observed, err := time.Parse(time.RFC3339Nano, sample); err == nil {
			if previous, err := time.Parse(time.RFC3339Nano, saved.String); err == nil && !observed.After(previous) {
				if observed.Before(previous) || !codexSnapshotFieldsMatch(currentExtra, updates) {
					return false, nil
				}
				// The same successfully stored sample may append credit details. UpdateExtra
				// strips its duplicate snapshot fields without losing those independent keys.
			}
		}
	}
	if err := r.UpdateExtra(txctx, targetID, updates); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	r.syncSchedulerAccountSnapshot(ctx, targetID)
	return true, nil
}

func codexSnapshotFieldsMatch(raw []byte, updates map[string]any) bool {
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		return false
	}
	for key, value := range updates {
		if key == "codex_usage_updated_at" {
			continue
		}
		if !strings.HasPrefix(key, "codex_primary_") && !strings.HasPrefix(key, "codex_secondary_") && !strings.HasPrefix(key, "codex_5h_") && !strings.HasPrefix(key, "codex_7d_") {
			continue
		}
		previous, ok := saved[key]
		if !ok {
			return false
		}
		a, err := json.Marshal(previous)
		if err != nil {
			return false
		}
		b, err := json.Marshal(value)
		if err != nil || !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}
