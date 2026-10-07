//go:build unit

package deepseek

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestControlIdentityLocaleTimezoneAndSnapshot(t *testing.T) {
	for _, tc := range []struct{ language, zone, locale, winter, summer string }{
		{"zh-CN", "Asia/Shanghai", "zh_CN", "28800", "28800"},
		{"en-US", "Europe/Amsterdam", "en_US", "3600", "7200"},
		{"en-US", "America/New_York", "en_US", "-18000", "-14400"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			identity := DefaultIdentity()
			identity.Language, identity.Timezone = tc.language, tc.zone
			winter := CaptureIdentity(identity, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			summer := CaptureIdentity(identity, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
			require.Equal(t, tc.summer, summer.ControlHeaders["X-Client-Timezone-Offset"])
			data, err := json.Marshal(winter)
			require.NoError(t, err)
			var restored outboundidentity.Identity
			require.NoError(t, json.Unmarshal(data, &restored))
			restored = CaptureIdentity(restored, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
			control := ControlIdentity(restored)
			require.Equal(t, tc.winter, control.Headers["X-Client-Timezone-Offset"])
			require.Equal(t, tc.locale, control.Headers["X-Client-Locale"])
			require.Equal(t, "web", control.Headers["X-Client-Platform"])
			require.Contains(t, control.Headers, "X-Client-Bundle-Id")
			require.Empty(t, control.Headers["X-Client-Bundle-Id"])
			require.Equal(t, identity.Version, control.Headers["X-Client-Version"])
			require.Equal(t, map[string]string{"User-Agent": identity.UserAgent}, winter.Headers)
			require.Empty(t, identity.ControlHeaders, "capturing must not mutate the source")
		})
	}
}
