package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModerationAsyncImageObservabilityMigrationAddsDurableMetadata(t *testing.T) {
	sql, err := FS.ReadFile("237_moderation_async_image_observability.sql")
	require.NoError(t, err)
	text := string(sql)
	for _, column := range []string{
		"moderation_endpoint_id",
		"moderation_endpoint_name",
		"storage_keys",
		"requested_images",
		"actual_images",
	} {
		require.Contains(t, text, column)
	}
	require.Contains(t, text, "jsonb_array_length(result->'data')")
}
