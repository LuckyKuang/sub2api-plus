//go:build unit || !integration

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayUnfinishedTaskConfigDefaultsYAMLAndEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 100, cfg.Gateway.MaxUnfinishedTasksPerUser)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("gateway:\n  max_unfinished_tasks_per_user: 7\n"), 0o600))
	t.Setenv("CONFIG_FILE", path)
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, 7, cfg.Gateway.MaxUnfinishedTasksPerUser)
	t.Setenv("GATEWAY_MAX_UNFINISHED_TASKS_PER_USER", "3")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, 3, cfg.Gateway.MaxUnfinishedTasksPerUser)
	for _, invalid := range []string{"0", "-1"} {
		t.Setenv("GATEWAY_MAX_UNFINISHED_TASKS_PER_USER", invalid)
		_, err = Load()
		require.ErrorContains(t, err, "gateway.max_unfinished_tasks_per_user must be positive")
	}
}
