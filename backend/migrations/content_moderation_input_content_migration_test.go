package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationInputContentMigrationAddsScanWindowText(t *testing.T) {
	sql, err := FS.ReadFile("250_content_moderation_input_content.sql")
	require.NoError(t, err)
	text := string(sql)
	require.Contains(t, text, "input_content")
	require.Contains(t, text, "input_content_truncated")
}
