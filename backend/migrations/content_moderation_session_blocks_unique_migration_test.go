package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationSessionBlocksUniqueMigrationPromotesConstraint(t *testing.T) {
	sql, err := FS.ReadFile("242_content_moderation_session_blocks_unique.sql")
	require.NoError(t, err)
	text := string(sql)
	require.Contains(t, text, "content_moderation_session_blocks_block_key_key")
	require.Contains(t, text, "UNIQUE (block_key)")
	require.Contains(t, text, "DROP INDEX IF EXISTS idx_content_moderation_session_blocks_block_key")
}
