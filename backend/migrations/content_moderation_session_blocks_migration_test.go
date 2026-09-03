package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationSessionBlocksMigrationCreatesDurableIndex(t *testing.T) {
	sql, err := FS.ReadFile("248_content_moderation_session_blocks.sql")
	require.NoError(t, err)
	text := string(sql)
	for _, fragment := range []string{
		"content_moderation_session_blocks",
		"block_key",
		"session_id",
		"expires_at",
		"idx_content_moderation_session_blocks_block_key",
		"idx_content_moderation_session_blocks_session_id",
	} {
		require.Contains(t, text, fragment)
	}
}
