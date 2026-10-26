//go:build unit || !integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type cnOAuthCredentialWriter interface {
	UpdateCNOAuthCredentialsIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error)
}

func TestAccountRepositoryCNOAuthRefreshMatchesExactCredentialOwner(t *testing.T) {
	for _, affected := range []rowsAffectedResult{0, 1} {
		exec := &recordingSQLExecutor{result: affected}
		repo := newAccountRepositoryWithSQL(nil, exec, nil)
		writer, ok := any(repo).(cnOAuthCredentialWriter)
		require.True(t, ok, "native OAuth refresh cannot persist without an atomic owner port")
		proxyID := int64(19)
		expected := &service.Account{ID: 42, Platform: "kimi", Type: "oauth", ProxyID: &proxyID,
			Proxy:       &service.Proxy{ID: proxyID, Protocol: "http", Host: "original.example", Port: 8080, Status: service.StatusActive},
			Credentials: map[string]any{"oauth_provider": "kimi", "access_token": "observed", "refresh_token": "observed-refresh", "_token_version": 7}}
		applied, err := writer.UpdateCNOAuthCredentialsIfUnchanged(context.Background(), expected,
			map[string]any{"oauth_provider": "kimi", "access_token": "rotated", "refresh_token": "rotated-refresh", "_token_version": 8})
		require.NoError(t, err)
		require.Equal(t, affected == 1, applied)
		require.Len(t, exec.execQueries, 1)
		query := normalizeSQLWhitespace(exec.execQueries[0])
		for _, fragment := range []string{"WITH updated AS", "UPDATE accounts AS a", "SET credentials = $1::jsonb",
			"a.id = $2", "a.deleted_at IS NULL", "a.platform = $3", "a.type = $4", "a.credentials = $5::jsonb",
			"a.proxy_id IS NOT DISTINCT FROM $6", "p.id = a.proxy_id", "p.deleted_at IS NULL", "p.protocol = $7", "p.host = $8", "p.port = $9", "FOR SHARE", "INSERT INTO scheduler_outbox", "FROM updated"} {
			require.Contains(t, query, fragment)
		}
		require.Len(t, exec.execArgs[0], 14)
		credentialsJSON, ok := exec.execArgs[0][0].(string)
		require.True(t, ok, "replacement credentials SQL parameter must contain serialized JSON")
		require.JSONEq(t, `{"oauth_provider":"kimi","access_token":"rotated","refresh_token":"rotated-refresh","_token_version":8}`, credentialsJSON)
		require.Equal(t, int64(42), exec.execArgs[0][1])
		require.Equal(t, "kimi", exec.execArgs[0][2])
		require.Equal(t, "oauth", exec.execArgs[0][3])
		expectedJSON, ok := exec.execArgs[0][4].(string)
		require.True(t, ok, "credential-owner SQL parameter must contain serialized JSON")
		require.JSONEq(t, `{"oauth_provider":"kimi","access_token":"observed","refresh_token":"observed-refresh","_token_version":7}`, expectedJSON)
		require.Equal(t, &proxyID, exec.execArgs[0][5])
		require.Equal(t, service.SchedulerOutboxEventAccountChanged, exec.execArgs[0][13])
	}
}
