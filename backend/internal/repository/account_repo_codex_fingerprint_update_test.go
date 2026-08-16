package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBulkUpdateNilCodexFingerprintModeRemovesKeyInsteadOfWritingJSONNull(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	_, err := repo.BulkUpdate(context.Background(), []int64{27}, service.AccountBulkUpdate{
		Extra: map[string]any{service.CodexFingerprintModeExtraKey: nil},
	})

	require.NoError(t, err)
	require.NotEmpty(t, exec.execQueries)
	require.Contains(t, normalizeSQLWhitespace(exec.execQueries[0]), "- 'codex_fingerprint_mode'")
}
