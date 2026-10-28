package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var errGrokCatalogInvalidJSON = errors.New("grok model catalog: invalid json")

var _ service.GrokCatalogSnapshotRepository = (*accountRepository)(nil)

const compareAndSwapGrokObservedModelsSQL = `UPDATE accounts AS a
SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{grok_observed_models}', $3::jsonb, true),
    updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL AND platform = 'grok'
  AND COALESCE(extra -> 'grok_observed_models', 'null'::jsonb) = $2::jsonb`

func (r *accountRepository) CompareAndSwapGrokObservedModels(ctx context.Context, owner *service.Account, expected, next json.RawMessage) (bool, error) {
	if scoped := r.accountTransactionScope(ctx); scoped != r {
		return scoped.CompareAndSwapGrokObservedModels(ctx, owner, expected, next)
	}
	if owner == nil || owner.ID <= 0 {
		return false, errors.New("invalid Grok catalog credential owner")
	}
	if owner.Platform != service.PlatformGrok {
		return false, nil
	}
	expectedArg, err := grokCatalogJSONArg(expected)
	if err != nil {
		return false, err
	}
	nextArg, err := grokCatalogJSONArg(next)
	if err != nil {
		return false, err
	}
	credentials, err := json.Marshal(normalizeJSONMap(owner.Credentials))
	if err != nil {
		return false, err
	}
	query := compareAndSwapGrokObservedModelsSQL + " AND a.type = $4 AND a.credentials = $5::jsonb AND a.proxy_id IS NOT DISTINCT FROM $6 AND a.status = $7"
	args := []any{owner.ID, expectedArg, nextArg, owner.Type, string(credentials), owner.ProxyID, owner.Status}
	predicate, args, err := cnProxyOwnerPredicate(owner, args)
	if err != nil {
		return false, err
	}
	query += predicate
	result, err := clientFromContext(ctx, r.client).ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if dbent.TxFromContext(ctx) == nil {
		r.syncSchedulerAccountSnapshot(ctx, owner.ID)
	}
	return true, nil
}

func grokCatalogJSONArg(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "null", nil
	}
	if !json.Valid(trimmed) {
		return "", errGrokCatalogInvalidJSON
	}
	return string(trimmed), nil
}
