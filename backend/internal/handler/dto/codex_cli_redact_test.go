//go:build unit

package dto

import (
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Existing malicious/imported nested state must not bypass the account DTO's
// top-level secret redaction; only the validated CLI-specific view exposes TOML.
func TestCodexCLIAccountDTOHidesWholeManagedConfiguration(t *testing.T) {
	account := &service.Account{ID: 1, Credentials: map[string]any{"codex_cli": map[string]any{
		"config_toml": "api_key=\"private-invalid-config\"", "auth_json": map[string]any{"refresh_token": "private-nested-refresh"},
	}}}
	view := AccountFromServiceShallow(account)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, view.Credentials, "codex_cli")
	require.True(t, view.CredentialsStatus["has_codex_cli"])
	require.NotContains(t, string(encoded), "private-invalid-config")
	require.NotContains(t, string(encoded), "private-nested-refresh")
	require.NotNil(t, account.Credentials["codex_cli"], "redaction cannot mutate stored state")
}
