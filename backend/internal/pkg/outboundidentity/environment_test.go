//go:build unit || !integration

package outboundidentity

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestEnvironmentLocalesRejectInvalidAndAmbientValues(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "Europe/Amsterdam", "US/Eastern"} {
		require.NoError(t, ValidateTimezone(zone))
	}
	for _, zone := range []string{"", "Local", "unknown", "Mars/Olympus", "/etc/localtime", "UTC\r\nX: 1"} {
		require.Error(t, ValidateTimezone(zone))
	}
	for _, tag := range []string{"en-US", "zh-CN", "zh-Hant", "nl-NL"} {
		require.NoError(t, ValidateLanguage(tag))
	}
	for _, tag := range []string{"", "und", "not a language", "en_US", "en-US\r\nX: 1"} {
		require.Error(t, ValidateLanguage(tag))
	}
}

func TestIdentityTimezoneUsesSelectedZoneAndDST(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	winter := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	summer := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	require.Zero(t, (Identity{}).TimezoneOffset(summer))
	identity := Identity{Timezone: "Europe/Amsterdam"}
	require.Equal(t, 3600, identity.TimezoneOffset(winter))
	require.Equal(t, 7200, identity.TimezoneOffset(summer))
	require.Equal(t, 28800, (Identity{Timezone: "Asia/Shanghai"}).TimezoneOffset(summer))
}
