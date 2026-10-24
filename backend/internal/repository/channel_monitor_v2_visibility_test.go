//go:build unit || !integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV2ExclusionsWinBeforeFoldingAndSeeding(t *testing.T) {
	cfg := service.ChannelMonitorV2Config{Platforms: []service.ChannelMonitorV2PlatformConfig{
		{Platform: "openai", Enabled: true, Models: []string{"hidden", "visible"}, ExcludedModels: []string{"hidden", "not-named"}},
		{Platform: "grok", Enabled: true},
	}}
	for _, filter := range []service.ChannelMonitorV2Filter{{}, {Models: []string{"hidden"}}, {Models: []string{service.ChannelMonitorV2OtherModel}}} {
		require.False(t, channelMonitorV2ModelSelected(filter, cfg, "openai", "hidden"))
		require.False(t, channelMonitorV2ModelSelected(filter, cfg, "openai", "not-named"))
	}
	require.True(t, channelMonitorV2ModelSelected(service.ChannelMonitorV2Filter{}, cfg, "grok", "hidden"))
	require.True(t, channelMonitorV2ModelSelected(service.ChannelMonitorV2Filter{}, cfg, "openai", "Hidden"))
	require.Equal(t, []string{"visible"}, configuredChannelMonitorV2Models(cfg, "openai", service.ChannelMonitorV2Filter{}))
	accs := seedChannelMonitorV2MatrixAccumulators(service.ChannelMonitorV2Filter{}, cfg, service.ChannelMonitorV2GroupByPlatformModel, nil)
	_, hidden := accs[channelMonitorV2MatrixKey{platform: "openai", model: "hidden"}]
	require.False(t, hidden, "excluded named models must not reappear as zero-traffic rows")

	cfg.Platforms[0].Models = nil
	for _, mode := range []string{"", service.ChannelMonitorV2DisplayAll, service.ChannelMonitorV2DisplaySelected} {
		cfg.Platforms[0].ModelDisplayMode = mode
		require.False(t, channelMonitorV2ModelSelected(service.ChannelMonitorV2Filter{}, cfg, "openai", "hidden"))
	}
	cfg.Platforms[0].ModelDisplayMode = service.ChannelMonitorV2DisplaySelected
	require.Equal(t, service.ChannelMonitorV2OtherModel, channelMonitorV2DisplayModel(cfg, "openai", "visible"))
}

func TestChannelMonitorV2WhereExcludesExactPlatformModelPairs(t *testing.T) {
	cfg := service.ChannelMonitorV2Config{Platforms: []service.ChannelMonitorV2PlatformConfig{
		{Platform: "openai", Enabled: true, ExcludedModels: []string{"Case", "quoted' OR TRUE--"}},
		{Platform: "grok", Enabled: true, ExcludedModels: []string{"only-grok"}},
	}}
	filter := service.ChannelMonitorV2Filter{Start: time.Unix(1, 0), End: time.Unix(2, 0), Bucket: 5 * time.Minute}
	for _, alias := range []string{"m", "h", "e"} {
		where, args, _ := channelMonitorV2WhereWithRollup(filter, cfg, alias)
		require.Contains(t, where, "NOT ("+alias+".platform = $4 AND "+alias+".model = ANY($5))")
		require.Contains(t, where, "NOT ("+alias+".platform = $6 AND "+alias+".model = ANY($7))")
		require.Contains(t, where, alias+".bucket_seconds = $8")
		require.NotContains(t, where, "quoted'")
		require.Equal(t, "openai", args[3])
		require.Equal(t, pq.Array([]string{"Case", "quoted' OR TRUE--"}), args[4])
	}
	require.Equal(t, "$2 $65 $81 $101", shiftSQLPlaceholders("$1 $64 $80 $100", 1))
}

func TestChannelMonitorV2CatalogRetainsExcludedRealNamesAndZeroTrafficSeeds(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	filter := service.ChannelMonitorV2Filter{Range: "30d", Start: time.Unix(1, 0), End: time.Unix(2, 0), Platforms: []string{"custom"}, GroupIDs: []int64{7}}
	cfg := service.ChannelMonitorV2Config{Version: 8, Platforms: []service.ChannelMonitorV2PlatformConfig{
		{Platform: "custom", Enabled: false, Models: []string{"zero", "both"}, ExcludedModels: []string{"both", "excluded-zero"}},
		{Platform: "openai", Models: []string{"outside-scope"}},
	}}
	mock.ExpectQuery(`(?s)WITH observations AS.*ul.requested_model.*ul.group_id = ANY\(\$4\).*current_error.requested_model.*current_error.group_id = ANY\(\$4\).*newer.request_id = current_error.request_id.*GROUP BY platform, model`).
		WithArgs(filter.Start, filter.End, pq.Array(filter.Platforms), pq.Array(filter.GroupIDs)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "model", "success", "errors"}).
			AddRow("custom", "both", 4, 3).AddRow("custom", "FailedOnly", 0, 2).AddRow("custom", "success-only", 1, 0))
	result, err := (&channelMonitorV2Repository{db: db}).GetModelCatalog(context.Background(), filter, cfg)
	require.NoError(t, err)
	require.Equal(t, 8, result.ConfigVersion)
	require.Equal(t, []service.ChannelMonitorV2ModelCatalogItem{
		{Platform: "custom", Model: "FailedOnly", ErrorRequests: 2, Sources: []string{"error"}},
		{Platform: "custom", Model: "both", SuccessRequests: 4, ErrorRequests: 3, Sources: []string{"success", "error", "configured", "excluded"}},
		{Platform: "custom", Model: "excluded-zero", Sources: []string{"excluded"}},
		{Platform: "custom", Model: "success-only", SuccessRequests: 1, Sources: []string{"success"}},
		{Platform: "custom", Model: "zero", Sources: []string{"configured"}},
	}, result.Items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2LegacyPlatformJSONGetsExplicitDefaults(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("FROM channel_monitor_v2_config WHERE id = 1").WillReturnRows(sqlmock.NewRows([]string{
		"version", "enabled", "refresh", "platforms", "groups", "ignored", "thresholds", "at", "by",
	}).AddRow(1, true, 300, `[{"platform":"openai","enabled":true,"models":["old"]},{"platform":"custom","enabled":false}]`, "{}", "{}", "{}", time.Unix(1, 0), nil))
	cfg, err := (&channelMonitorV2Repository{db: db}).GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, service.ChannelMonitorV2DisplaySelected, cfg.Platforms[0].ModelDisplayMode)
	require.Equal(t, service.ChannelMonitorV2DisplayAll, cfg.Platforms[1].ModelDisplayMode)
	require.Equal(t, []string{}, cfg.Platforms[0].ExcludedModels)
	require.Equal(t, []string{}, cfg.Platforms[1].Models)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestChannelMonitorV2CatalogRejectsOversizeInsteadOfReturningTruncatedCandidates(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	rows := sqlmock.NewRows([]string{"platform", "model", "success", "errors"})
	for i := 0; i <= service.ChannelMonitorV2ModelCatalogLimit; i++ {
		rows.AddRow("openai", fmt.Sprint(i), 1, 0)
	}
	mock.ExpectQuery(`LIMIT 10001`).WillReturnRows(rows)
	catalog, err := (&channelMonitorV2Repository{db: db}).GetModelCatalog(context.Background(), service.ChannelMonitorV2Filter{}, service.ChannelMonitorV2Config{})
	require.ErrorIs(t, err, service.ErrChannelMonitorV2CatalogTooLarge)
	require.Nil(t, catalog)
	require.NoError(t, mock.ExpectationsWereMet())
}
