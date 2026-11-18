//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelPricingUltrafastMultiplierMigration(t *testing.T) {
	content, err := FS.ReadFile("316_channel_pricing_ultrafast_multiplier.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS ultrafast_multiplier NUMERIC(12,6)")
	require.Contains(t, sql, "channel_model_pricing_ultrafast_multiplier_positive")
	require.Contains(t, sql, "CHECK (ultrafast_multiplier IS NULL OR ultrafast_multiplier > 0)")
	require.Contains(t, sql, "COMMENT ON COLUMN channel_model_pricing.ultrafast_multiplier")
}
