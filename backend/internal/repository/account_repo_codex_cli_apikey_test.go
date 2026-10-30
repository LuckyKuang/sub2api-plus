//go:build unit || !integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexCLIAPIKeyConfigurationCASPreservesKeyAndRoute(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		exec := &recordingSQLExecutor{result: rowsAffectedResult(affected)}
		repo := newAccountRepositoryWithSQL(nil, exec, nil)
		owner := &service.Account{ID: 81, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "private-key", "base_url": "https://compatible.example/v1"}}
		credentials := service.MergeCredentials(owner.Credentials, map[string]any{service.CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": service.DefaultCodexCLIConfigTOML}})
		applied, err := repo.UpdateCodexCLIAPIKeyConfigurationIfUnchanged(context.Background(), owner, credentials)
		require.NoError(t, err)
		require.Equal(t, affected == 1, applied)
		require.Len(t, exec.execQueries, 1)
		query := normalizeSQLWhitespace(exec.execQueries[0])
		require.Contains(t, query, "a.credentials=$8::jsonb")
		require.Contains(t, query, "a.proxy_id IS NOT DISTINCT FROM $9")
		require.Contains(t, query, "INSERT INTO scheduler_outbox")
		require.Equal(t, service.AccountTypeAPIKey, exec.execArgs[0][2])
		require.Equal(t, false, exec.execArgs[0][3], "configuration writes never recover scheduling state")
		require.Equal(t, service.AccountTypeAPIKey, exec.execArgs[0][6])
		before := len(exec.execQueries)
		credentials["api_key"] = "rotated-without-authorization"
		_, err = repo.UpdateCodexCLIAPIKeyConfigurationIfUnchanged(context.Background(), owner, credentials)
		require.Error(t, err)
		require.Len(t, exec.execQueries, before)
		_, err = repo.UpdateOAuthCredentialsIfUnchanged(context.Background(), owner, service.AccountTypeAPIKey, credentials, nil, false)
		require.Error(t, err, "OAuth-only authorization persistence cannot be widened by CLI support")
		require.Len(t, exec.execQueries, before)
	}
}
