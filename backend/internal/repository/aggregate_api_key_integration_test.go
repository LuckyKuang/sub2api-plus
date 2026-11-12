//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	dbmigrations "github.com/LuckyKuang/sub2api-plus/migrations"
	"github.com/stretchr/testify/require"
)

// Requirements: new columns must preserve every legacy row and reject storage
// states that could reinterpret an old key or enable aggregate conversion.
func TestAggregateKeyMigrationPreservesLegacyAndConstrainsModes(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE api_keys (id BIGINT PRIMARY KEY, group_id BIGINT, conversion_enabled BOOLEAN NOT NULL DEFAULT false,default_client TEXT NOT NULL DEFAULT '',default_endpoint TEXT NOT NULL DEFAULT '') ON COMMIT DROP;
	INSERT INTO api_keys (id,group_id,conversion_enabled,default_client,default_endpoint) VALUES (1,NULL,false,'',''),(2,7,true,'codex','/v1/responses')`)
	require.NoError(t, err)
	migration, err := dbmigrations.FS.ReadFile("308_aggregate_api_keys.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	var mode, ids, client, endpoint string
	var enabled bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT group_binding_mode,aggregate_group_ids::text,conversion_enabled,default_client,default_endpoint FROM api_keys WHERE id=2`).Scan(&mode, &ids, &enabled, &client, &endpoint))
	require.Equal(t, "single", mode)
	require.JSONEq(t, "[]", ids)
	require.True(t, enabled)
	require.Equal(t, "codex", client)
	require.Equal(t, "/v1/responses", endpoint)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT group_binding_mode FROM api_keys WHERE id=1`).Scan(&mode))
	require.Equal(t, "single", mode)
	_, err = tx.ExecContext(ctx, `INSERT INTO api_keys (id,group_binding_mode) VALUES (3,'all')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO api_keys (id,group_binding_mode,aggregate_group_ids) VALUES (4,'custom','[7,8]')`)
	require.NoError(t, err)
	for _, query := range []string{
		`INSERT INTO api_keys (id,group_binding_mode) VALUES (5,'custom')`,
		`INSERT INTO api_keys (id,group_binding_mode,group_id) VALUES (5,'all',7)`,
		`INSERT INTO api_keys (id,group_binding_mode,conversion_enabled) VALUES (5,'all',true)`,
		`INSERT INTO api_keys (id,group_binding_mode,default_client) VALUES (5,'all','codex')`,
		`INSERT INTO api_keys (id,group_binding_mode,aggregate_group_ids) VALUES (5,'custom','{}')`,
		`INSERT INTO api_keys (id,group_binding_mode) VALUES (5,'unknown')`,
	} {
		_, err = tx.ExecContext(ctx, "SAVEPOINT invalid_aggregate")
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, query)
		require.Error(t, err)
		_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT invalid_aggregate")
		require.NoError(t, err)
	}
}

func (s *APIKeyRepoSuite) TestAggregateBindingAllReadPathsAndAtomicUsageIsolation() {
	user := s.mustCreateUser("aggregate-roundtrip@test.invalid")
	key := &service.APIKey{UserID: user.ID, Key: "sk-aggregate-fixture", Name: "Aggregate", Status: service.StatusActive, GroupBindingMode: service.GroupBindingCustom, AggregateGroupIDs: []int64{7, 8}}
	s.Require().NoError(s.repo.Create(s.ctx, key))
	_, err := s.repo.IncrementQuotaUsed(s.ctx, key.ID, 2.5)
	s.Require().NoError(err)
	for _, read := range []func() (*service.APIKey, error){
		func() (*service.APIKey, error) { return s.repo.GetByID(s.ctx, key.ID) },
		func() (*service.APIKey, error) { return s.repo.GetByKey(s.ctx, key.Key) },
		func() (*service.APIKey, error) { return s.repo.GetByKeyForAuth(s.ctx, key.Key) },
	} {
		got, err := read()
		s.Require().NoError(err)
		s.Equal("custom", got.GroupBindingMode)
		s.Equal([]int64{7, 8}, got.AggregateGroupIDs)
		s.Nil(got.GroupID)
		s.Equal(2.5, got.QuotaUsed)
	}
	listed, _, err := s.repo.ListByUserID(s.ctx, user.ID, pagination.PaginationParams{Page: 1, PageSize: 10}, service.APIKeyListFilters{})
	s.Require().NoError(err)
	s.Require().Len(listed, 1)
	s.Equal([]int64{7, 8}, listed[0].AggregateGroupIDs)
	key.GroupBindingMode, key.AggregateGroupIDs = service.GroupBindingAll, nil
	s.Require().NoError(s.repo.Update(s.ctx, key, service.APIKeyUpdateFields{GroupBinding: true}))
	got, err := s.repo.GetByKeyForAuth(s.ctx, key.Key)
	s.Require().NoError(err)
	s.Equal("all", got.GroupBindingMode)
	s.Empty(got.AggregateGroupIDs)
	s.Equal(2.5, got.QuotaUsed, "editing an old snapshot cannot erase concurrent charges")
}
