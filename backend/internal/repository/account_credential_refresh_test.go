//go:build unit || !integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCNOAuthRefreshCASUsesCompleteGrantAndAtomicOutbox(t *testing.T) {
	for _, changed := range []int64{0, 1} {
		t.Run(string(rune('0'+changed)), func(t *testing.T) {
			exec := &recordingSQLExecutor{result: rowsAffectedResult(changed)}
			repo := newAccountRepositoryWithSQL(nil, exec, nil)
			proxy := int64(7)
			applied, err := repo.UpdateOAuthCredentialsIfUnchanged(context.Background(), 42, service.PlatformKimi, map[string]any{"access_token": "attempted", "refresh_token": "old"}, &proxy, map[string]any{"access_token": "new", "refresh_token": "rotated"})
			require.NoError(t, err)
			require.Equal(t, changed == 1, applied)
			require.Len(t, exec.execQueries, 1)
			query := normalizeSQLWhitespace(exec.execQueries[0])
			require.Contains(t, query, "WITH updated AS")
			require.Contains(t, query, "a.credentials = $5::jsonb")
			require.Contains(t, query, "a.proxy_id IS NOT DISTINCT FROM $6")
			require.Contains(t, query, "a.status = $7")
			require.Contains(t, query, "INSERT INTO scheduler_outbox")
			require.Equal(t, service.PlatformKimi, exec.execArgs[0][2])
			require.Equal(t, service.AccountTypeOAuth, exec.execArgs[0][3])
			require.Equal(t, &proxy, exec.execArgs[0][5])
			require.Equal(t, service.StatusActive, exec.execArgs[0][6])
		})
	}
}
