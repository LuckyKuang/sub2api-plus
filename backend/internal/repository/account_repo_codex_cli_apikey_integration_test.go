//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexCLIAPIKeyConfigurationRealCASRejectsStaleKey(t *testing.T) {
	ctx := context.Background()
	repo, _, fixture, _, _, _ := newAdminOAuthFixture(t, service.PlatformOpenAI, "success")
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET type='apikey',credentials='{"api_key":"original-key","base_url":"https://compatible.example/v1"}'::jsonb WHERE id=$1`, fixture.ID)
	require.NoError(t, err)
	owner, err := repo.GetByID(ctx, fixture.ID)
	require.NoError(t, err)
	credentials := service.MergeCredentials(owner.Credentials, map[string]any{service.CodexCLIConfigCredentialKey: map[string]any{"enabled": true, "config_toml": service.DefaultCodexCLIConfigTOML}})
	applied, err := repo.UpdateCodexCLIAPIKeyConfigurationIfUnchanged(ctx, owner, credentials)
	require.NoError(t, err)
	require.True(t, applied)
	accepted, err := repo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, "original-key", accepted.GetCredential("api_key"))
	require.Equal(t, "https://compatible.example/v1", accepted.GetCredential("base_url"))
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=jsonb_set(credentials,'{api_key}','"newer-key"'::jsonb) WHERE id=$1`, owner.ID)
	require.NoError(t, err)
	applied, err = repo.UpdateCodexCLIAPIKeyConfigurationIfUnchanged(ctx, accepted, credentials)
	require.NoError(t, err)
	require.False(t, applied)
	current, err := repo.GetByID(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, "newer-key", current.GetCredential("api_key"))
}
