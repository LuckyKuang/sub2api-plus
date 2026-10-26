//go:build unit || !integration

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The native control flow must have a persistence port capable of matching the
// credential owner in the same statement that writes a provider observation.
// This local interface makes the pre-fix regression fail as a business-port
// assertion rather than a missing-symbol compilation error.
type cnControlSnapshotWriter interface {
	UpdateCNControlExtraIfUnchanged(context.Context, *service.Account, map[string]any) (bool, error)
}

func requireCNControlSnapshotWriter(t *testing.T, repo *accountRepository) cnControlSnapshotWriter {
	t.Helper()
	writer, ok := any(repo).(cnControlSnapshotWriter)
	require.True(t, ok, "native usage and wallet observations require atomic credential-owner persistence")
	return writer
}

func TestAccountRepositoryCNControlSnapshotUsesAtomicOwnerAndOutbox(t *testing.T) {
	for _, affected := range []rowsAffectedResult{0, 1} {
		t.Run(map[rowsAffectedResult]string{0: "relinked owner", 1: "same owner"}[affected], func(t *testing.T) {
			exec := &recordingSQLExecutor{result: affected}
			writer := requireCNControlSnapshotWriter(t, newAccountRepositoryWithSQL(nil, exec, nil))
			proxyID := int64(8)
			expected := &service.Account{ID: 42, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Status: service.StatusActive,
				Proxy:   &service.Proxy{ID: proxyID, Protocol: "http", Host: "original.example", Port: 8080, Status: service.StatusActive},
				ProxyID: &proxyID, Credentials: map[string]any{"access_token": "observed-token", "refresh_token": "observed-refresh", "_token_version": 7}}
			applied, err := writer.UpdateCNControlExtraIfUnchanged(context.Background(), expected,
				map[string]any{"kimi_5h_used_percent": 25.0, "kimi_weekly_used_percent": nil})
			require.NoError(t, err)
			require.Equal(t, affected == 1, applied)
			require.Len(t, exec.execQueries, 1, "owner comparison, merge and invalidation must share one SQL statement")
			query := normalizeSQLWhitespace(exec.execQueries[0])
			for _, fragment := range []string{"WITH updated AS", "UPDATE accounts AS a", "COALESCE(a.extra, '{}'::jsonb) || $1::jsonb",
				"a.id = $2", "a.deleted_at IS NULL", "a.platform = $3", "a.type = $4", "a.credentials = $5::jsonb",
				"a.proxy_id IS NOT DISTINCT FROM $6", "a.status = $14", "p.id = a.proxy_id", "p.deleted_at IS NULL", "p.protocol = $7", "p.host = $8", "p.port = $9", "FOR SHARE", "INSERT INTO scheduler_outbox", "FROM updated"} {
				require.Contains(t, query, fragment)
			}
			require.Len(t, exec.execArgs[0], 15)
			updatesJSON, ok := exec.execArgs[0][0].(string)
			require.True(t, ok, "observation SQL parameter must contain serialized JSON")
			require.JSONEq(t, `{"kimi_5h_used_percent":25,"kimi_weekly_used_percent":null}`, updatesJSON)
			require.Equal(t, int64(42), exec.execArgs[0][1])
			require.Equal(t, "kimi", exec.execArgs[0][2])
			require.Equal(t, "oauth", exec.execArgs[0][3])
			credentialsJSON, ok := exec.execArgs[0][4].(string)
			require.True(t, ok, "credential-owner SQL parameter must contain serialized JSON")
			require.JSONEq(t, `{"access_token":"observed-token","refresh_token":"observed-refresh","_token_version":7}`, credentialsJSON)
			require.Equal(t, &proxyID, exec.execArgs[0][5])
			require.Equal(t, service.SchedulerOutboxEventAccountChanged, exec.execArgs[0][14])
		})
	}
}

func TestAccountRepositoryCNControlSnapshotKeepsNeutralObservationSemantics(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
	writer := requireCNControlSnapshotWriter(t, newAccountRepositoryWithSQL(nil, exec, nil))
	applied, err := writer.UpdateCNControlExtraIfUnchanged(context.Background(),
		&service.Account{ID: 42, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{}},
		map[string]any{"passive_usage_updated_at": "2026-10-25T00:00:00Z"})
	require.NoError(t, err)
	require.True(t, applied)
	require.Len(t, exec.execQueries, 1)
	require.NotContains(t, exec.execQueries[0], "scheduler_outbox", "neutral metadata must not rebuild scheduler buckets")
	require.Contains(t, exec.execQueries[0], "credentials = $5::jsonb")
	require.Len(t, exec.execArgs[0], 7)
}

func TestAccountRepositoryCNControlSnapshotRejectsInvalidInputWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected *service.Account
		updates  map[string]any
	}{
		{"missing owner", nil, map[string]any{"kimi_5h_used_percent": 1}},
		{"missing ID", &service.Account{Platform: "kimi", Type: "oauth"}, map[string]any{"kimi_5h_used_percent": 1}},
		{"invalid credentials", &service.Account{ID: 1, Platform: "kimi", Type: "oauth", Credentials: map[string]any{"bad": make(chan int)}}, map[string]any{"kimi_5h_used_percent": 1}},
		{"invalid observation", &service.Account{ID: 1, Platform: "kimi", Type: "oauth"}, map[string]any{"bad": make(chan int)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
			writer := requireCNControlSnapshotWriter(t, newAccountRepositoryWithSQL(nil, exec, nil))
			applied, err := writer.UpdateCNControlExtraIfUnchanged(context.Background(), tc.expected, tc.updates)
			require.Error(t, err)
			require.False(t, applied)
			require.Empty(t, exec.execQueries)
		})
	}
	failure := errors.New("database unavailable")
	exec := &recordingSQLExecutor{err: failure}
	writer := requireCNControlSnapshotWriter(t, newAccountRepositoryWithSQL(nil, exec, nil))
	applied, err := writer.UpdateCNControlExtraIfUnchanged(context.Background(),
		&service.Account{ID: 1, Platform: "deepseek", Type: "oauth"}, map[string]any{"deepseek_balance": 1})
	require.ErrorIs(t, err, failure)
	require.False(t, applied)
}

func TestCNControlBalanceMetadataFencesObservedSchedulerState(t *testing.T) {
	exec := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, exec, nil)
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	expected := &service.Account{ID: 42, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "observed-key"},
		TempUnschedulableUntil: &until, TempUnschedulableReason: "cn_balance_low: previous wallet"}
	applied, err := repo.UpdateCNControlExtraIfUnchanged(context.Background(), expected, map[string]any{"deepseek_balance": 50.0, "deepseek_balance_low": false})
	require.NoError(t, err)
	require.False(t, applied)
	require.Len(t, exec.execQueries, 1)
	query := normalizeSQLWhitespace(exec.execQueries[0])
	for _, fragment := range []string{"a.status = $7", "a.schedulable = $8", "a.temp_unschedulable_until IS NOT DISTINCT FROM $9::timestamptz", "COALESCE(a.temp_unschedulable_reason, '') = $10", "INSERT INTO scheduler_outbox"} {
		require.Contains(t, query, fragment)
	}
	require.Equal(t, []any{service.StatusActive, true, &until, "cn_balance_low: previous wallet", service.SchedulerOutboxEventAccountChanged}, exec.execArgs[0][6:])
}
