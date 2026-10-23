//go:build unit || !integration

package externalredis

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// CONTRIBUTING.md requires external integration data to be isolated from the
// running application. Missing/invalid DB selection must never fall back to DB 0.
func TestExternalRedisRejectsUnsafeDatabaseSelection(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "invalid"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("TEST_REDIS_DB", value)
			options, err := Options("test-redis:6379", "TEST_REDIS_DB")
			require.ErrorContains(t, err, "dedicated nonzero Redis database")
			require.Nil(t, options)
		})
	}
}

func TestExternalRedisUsesExplicitDatabaseAndCredentials(t *testing.T) {
	t.Setenv("TEST_REDIS_DB", "15")
	t.Setenv("SUB2API_TEST_REDIS_USERNAME", "validation")
	t.Setenv("SUB2API_TEST_REDIS_PASSWORD", "test-secret")
	options, err := Options("test-redis:6379", "TEST_REDIS_DB")
	require.NoError(t, err)
	require.Equal(t, 15, options.DB)
	require.Equal(t, "validation", options.Username)
	require.Equal(t, "test-secret", options.Password)
	_, err = Options("", "TEST_REDIS_DB")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "test-secret")
}
