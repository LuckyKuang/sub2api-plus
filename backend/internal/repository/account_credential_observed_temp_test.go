//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCredentialStateObservedTempGuardIncludesAbsentDeadline(t *testing.T) {
	for _, observed := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "present"}[observed], func(t *testing.T) {
			owner := &service.Account{ID: 42, Platform: service.PlatformDeepseek, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "observed"}}
			until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
			mutation := service.CredentialStateMutation{RequireTempUnschedulableUnchanged: true, Extra: map[string]any{"balance": 0}}
			if observed {
				mutation.ObservedTempUntil, mutation.ObservedTempReason = &until, "observed-balance-cooldown"
			}
			executor := &recordingSQLExecutor{result: rowsAffectedResult(0)}
			repo := newAccountRepositoryWithSQL(nil, executor, nil)
			applied, err := repo.ApplyCredentialStateMutation(context.Background(), owner, mutation)
			require.NoError(t, err)
			require.False(t, applied)
			require.Len(t, executor.execQueries, 1)
			require.Contains(t, executor.execQueries[0], "a.temp_unschedulable_until IS NOT DISTINCT FROM $7::timestamptz")
			require.Contains(t, executor.execQueries[0], "COALESCE(a.temp_unschedulable_reason, '') = $8")
			require.Equal(t, mutation.ObservedTempUntil, executor.execArgs[0][6])
			require.Equal(t, mutation.ObservedTempReason, executor.execArgs[0][7])
		})
	}
}
