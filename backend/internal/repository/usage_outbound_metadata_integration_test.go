//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUsageOutboundMigrationPreservesHistoryAndEvidenceConstraints(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "outbound-migration-" + uuid.NewString() + "@example.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-migration-" + uuid.NewString(), Name: "migration"})
	account := mustCreateAccount(t, client, &service.Account{Name: "outbound-migration-" + uuid.NewString()})
	request := uuid.NewString()
	cleanupPersistedUsageLogFixture(t, ctx, user.ID, key.ID, account.ID, request)
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, "ALTER TABLE usage_logs DROP COLUMN outbound_metadata")
	require.NoError(t, err)
	var id int64
	require.NoError(t, tx.QueryRowContext(ctx, "INSERT INTO usage_logs (user_id,api_key_id,account_id,request_id,model) VALUES ($1,$2,$3,$4,'gpt-6.1-sol') RETURNING id", user.ID, key.ID, account.ID, request).Scan(&id))
	migration, err := dbmigrations.FS.ReadFile("304_usage_outbound_metadata.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	var historical sql.NullString
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT outbound_metadata FROM usage_logs WHERE id=$1", id).Scan(&historical))
	require.False(t, historical.Valid, "upgrades cannot invent past dispatch evidence")
	for _, test := range []struct {
		payload string
		valid   bool
	}{
		{`{"transport":"native","evidence":"gateway_send"}`, true},
		{`{"transport":"codex_cli_bridge","evidence":"gateway_send","cli_version":"0.165.0"}`, true},
		{`{"transport":"codex_cli_task","evidence":"cli_process","cli_version":"0.165.0"}`, true},
		{`{"transport":"codex_cli_task","evidence":"gateway_send"}`, false},
		{`{"transport":"codex_cli_bridge","evidence":"cli_process"}`, false},
		{`{}`, false}, {`[]`, false},
	} {
		_, err = tx.ExecContext(ctx, "SAVEPOINT evidence_case")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET outbound_metadata=$1 WHERE id=$2", test.payload, id)
		if test.valid {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
		_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT evidence_case")
		require.NoError(t, err)
	}
}
