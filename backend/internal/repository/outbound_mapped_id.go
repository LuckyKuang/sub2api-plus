package repository

import (
	"context"
	"errors"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

var _ service.OutboundIDStore = (*settingRepository)(nil)

// Operational identities live outside the settings document, so a settings
// update cannot delete conversation mappings. A conflict returns the winner,
// including when another instance created it concurrently.
func (r *settingRepository) ResolveOutboundID(ctx context.Context, namespace, digest, candidate string) (string, error) {
	// Resume reads do not rewrite the persistent row on every model request.
	var existing entsql.Rows
	if err := r.client.Driver().Query(ctx, `SELECT value FROM outbound_identity_ids WHERE namespace=$1 AND scope_digest=$2`, []any{namespace, digest}, &existing); err != nil {
		return "", err
	}
	if existing.Next() {
		var value string
		err := existing.Scan(&value)
		_ = existing.Close()
		return value, err
	}
	err := existing.Err()
	_ = existing.Close()
	if err != nil {
		return "", err
	}
	var rows entsql.Rows
	err = r.client.Driver().Query(ctx, `
		INSERT INTO outbound_identity_ids (namespace, scope_digest, value)
		VALUES ($1, $2, $3)
		ON CONFLICT (namespace, scope_digest) DO UPDATE
		SET value = outbound_identity_ids.value
		RETURNING value`, []any{namespace, digest, candidate}, &rows)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", errors.New("outbound identity mapping returned no row")
	}
	var value string
	if err := rows.Scan(&value); err != nil {
		return "", err
	}
	return value, rows.Err()
}
