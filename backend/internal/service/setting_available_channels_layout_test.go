//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// docs/AVAILABLE_CHANNELS.md: legacy/malformed storage defaults to cards; the
// site-wide list choice must survive admin reads, public reads and HTML injection.
func TestAvailableChannelsLayoutSettings(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{"", "card"}, {"card", "card"}, {"list", "list"}, {"LIST", "card"}, {"unknown", "card"},
	} {
		t.Run("stored_"+tc.stored, func(t *testing.T) {
			ctx := context.Background()
			values := map[string]string{SettingKeyAvailableChannelsLayout: tc.stored}
			admin := NewSettingService(&settingGetAllRepoStub{values: values}, &config.Config{})
			settings, err := admin.GetAllSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, settings.AvailableChannelsLayout)

			public := NewSettingService(&settingPublicRepoStub{values: values}, &config.Config{})
			view, err := public.GetPublicSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, view.AvailableChannelsLayout)
			injected, err := public.GetPublicSettingsForInjection(ctx)
			require.NoError(t, err)
			require.Equal(t, tc.want, injected.(*PublicSettingsInjectionPayload).AvailableChannelsLayout)
		})
	}
}

func TestAvailableChannelsLayoutFreshInstallDefaultsAndPersistence(t *testing.T) {
	ctx := context.Background()
	fresh := &forwardedIPMigrationRepoStub{values: map[string]string{}}
	require.NoError(t, NewSettingService(fresh, &config.Config{}).InitializeDefaultSettings(ctx))
	require.Equal(t, "card", fresh.values[SettingKeyAvailableChannelsLayout])
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})
	for _, layout := range []string{"card", "list", ""} {
		require.NoError(t, svc.UpdateSettings(ctx, &SystemSettings{AvailableChannelsLayout: layout}))
		want := "card"
		if layout == "list" {
			want = "list"
		}
		require.Equal(t, want, repo.updates[SettingKeyAvailableChannelsLayout])
	}
}
