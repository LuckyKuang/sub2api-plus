package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBulkUpdateExplicitCodexFingerprintModeMergesValue(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)

	_, err := repo.BulkUpdate(context.Background(), []int64{27}, service.AccountBulkUpdate{
		Extra: map[string]any{service.CodexFingerprintModeExtraKey: "device"},
	})

	require.NoError(t, err)
	require.NotEmpty(t, exec.execQueries)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	require.Contains(t, query, "extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb")
	require.NotContains(t, query, "- 'codex_fingerprint_mode'")
	payload, ok := exec.execArgs[0][0].([]byte)
	require.True(t, ok)
	require.JSONEq(t, `{"codex_fingerprint_mode":"device"}`, string(payload))
}

func TestCodexFingerprintModeExtraUpdateIsSchedulerRelevant(t *testing.T) {
	require.True(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		service.CodexFingerprintModeExtraKey: "device",
	}))
}
