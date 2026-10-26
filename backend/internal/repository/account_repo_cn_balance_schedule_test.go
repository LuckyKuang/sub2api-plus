//go:build unit || !integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type cnBalanceScheduleWriter interface {
	UpdateCNBalanceScheduleIfUnchanged(context.Context, *service.Account, *time.Time, string) (bool, error)
}

func requireCNBalanceScheduleWriter(t *testing.T, repo *accountRepository) cnBalanceScheduleWriter {
	t.Helper()
	writer, ok := any(repo).(cnBalanceScheduleWriter)
	require.True(t, ok, "native wallet scheduling must compare its credential owner and previous cooldown atomically")
	return writer
}

func TestAccountRepositoryCNBalanceScheduleAtomicOwnerAndCooldown(t *testing.T) {
	for _, clear := range []bool{false, true} {
		for _, affected := range []rowsAffectedResult{0, 1} {
			t.Run(map[bool]string{false: "pause", true: "recover"}[clear]+map[rowsAffectedResult]string{0: " changed", 1: " matching"}[affected], func(t *testing.T) {
				exec := &recordingSQLExecutor{result: affected}
				writer := requireCNBalanceScheduleWriter(t, newAccountRepositoryWithSQL(nil, exec, nil))
				expected := &service.Account{ID: 42, Platform: service.PlatformDeepseek, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"oauth_provider": "deepseek", "access_token": "wallet-owner"}}
				until := time.Now().Add(time.Hour)
				next := &until
				reason := "cn_balance_low: low wallet"
				if clear {
					expected.TempUnschedulableUntil = &until
					expected.TempUnschedulableReason = reason
					next = nil
					reason = ""
				}
				applied, err := writer.UpdateCNBalanceScheduleIfUnchanged(context.Background(), expected, next, reason)
				require.NoError(t, err)
				require.Equal(t, affected == 1, applied)
				require.Len(t, exec.execQueries, 1, "scheduler invalidation must be in the same atomic write")
				query := normalizeSQLWhitespace(exec.execQueries[0])
				for _, fragment := range []string{"WITH updated AS", "UPDATE accounts AS a", "a.deleted_at IS NULL", "a.credentials = $7::jsonb", "a.proxy_id IS NOT DISTINCT FROM $8", "a.temp_unschedulable_until IS NOT DISTINCT FROM $9", "COALESCE(a.temp_unschedulable_reason, '') = $10", "a.schedulable = $11", "INSERT INTO scheduler_outbox", "FROM updated"} {
					require.Contains(t, query, fragment)
				}
				require.Equal(t, expected.ID, exec.execArgs[0][2])
				credentials, ok := exec.execArgs[0][6].(string)
				require.True(t, ok)
				require.JSONEq(t, `{"oauth_provider":"deepseek","access_token":"wallet-owner"}`, credentials)
				require.Equal(t, service.SchedulerOutboxEventAccountChanged, exec.execArgs[0][len(exec.execArgs[0])-1])
			})
		}
	}
}

func TestAccountRepositoryCNBalanceScheduleInvalidOwnerHasNoWrites(t *testing.T) {
	for _, owner := range []*service.Account{nil, {ID: 42, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey}, {ID: 42, Platform: service.PlatformKimi, Type: service.AccountTypeOAuth, Credentials: map[string]any{"oauth_provider": "kimi", "access_token": "native"}}} {
		exec := &recordingSQLExecutor{result: rowsAffectedResult(1)}
		writer := requireCNBalanceScheduleWriter(t, newAccountRepositoryWithSQL(nil, exec, nil))
		_, err := writer.UpdateCNBalanceScheduleIfUnchanged(context.Background(), owner, nil, "")
		require.Error(t, err)
		require.Empty(t, exec.execQueries)
	}
}
