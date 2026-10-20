//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The account credential contract requires an atomic conditional quarantine:
// reauthorization/rotation must not be disabled by a failed older attempt, and
// scheduler notification must commit with the state transition.
func TestCredentialFailureQuarantineFencesExactOwnerAndCredentials(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	proxy := int64(17)
	changed, err := repo.SetCredentialErrorIfUnchanged(context.Background(), 42, service.PlatformKimi, service.AccountTypeOAuth,
		map[string]any{"access_token": "old", "_token_version": 7}, &proxy, "grant_revoked")
	require.NoError(t, err)
	require.False(t, changed)
	require.Len(t, exec.execQueries, 1)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	for _, condition := range []string{"a.deleted_at IS NULL", "a.status = $4", "a.platform = $5", "a.type = $6", "a.credentials = $7::jsonb", "a.proxy_id IS NOT DISTINCT FROM $8", "INSERT INTO scheduler_outbox", "FROM updated"} {
		require.Contains(t, query, condition)
	}
	require.Equal(t, service.PlatformKimi, exec.execArgs[0][4])
	require.Equal(t, service.AccountTypeOAuth, exec.execArgs[0][5])
	require.JSONEq(t, `{"access_token":"old","_token_version":7}`, exec.execArgs[0][6].(string))
	require.Equal(t, &proxy, exec.execArgs[0][7])
}

func TestCredential401CooldownFencesExactOwnerAndPreservesLongerWindow(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	proxy := int64(17)
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	changed, err := repo.SetCredentialTempUnschedulableIfUnchanged(context.Background(), 42, service.PlatformOpenAI, service.AccountTypeOAuth,
		map[string]any{"access_token": "sent", "refresh_token": "old", "_token_version": 7}, &proxy, until, "oauth_401")
	require.NoError(t, err)
	require.False(t, changed)
	require.Len(t, exec.execQueries, 1)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	for _, condition := range []string{"a.deleted_at IS NULL", "a.status = $4", "a.platform = $5", "a.type = $6", "a.credentials = $7::jsonb", "a.proxy_id IS NOT DISTINCT FROM $8", "a.temp_unschedulable_until < $1", "INSERT INTO scheduler_outbox", "FROM updated"} {
		require.Contains(t, query, condition)
	}
	require.Equal(t, until, exec.execArgs[0][0])
	require.Equal(t, service.StatusActive, exec.execArgs[0][3])
	require.JSONEq(t, `{"access_token":"sent","refresh_token":"old","_token_version":7}`, exec.execArgs[0][6].(string))
	require.Equal(t, &proxy, exec.execArgs[0][7])
}
