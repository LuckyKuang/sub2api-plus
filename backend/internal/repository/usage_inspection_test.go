//go:build unit || !integration

package repository

import (
	"context"
	"database/sql/driver"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

// The boundary applies to both count and page queries, under the owner's filter.
func TestUsageInspectionInsertionBoundary(t *testing.T) {
	for _, capture := range []bool{true, false} {
		t.Run(map[bool]string{true: "capture", false: "continue"}[capture], func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			boundary := int64(42)
			if capture {
				boundary = -1
				mock.ExpectQuery(`SELECT COALESCE\(MAX\(id\), 0\) FROM usage_logs WHERE user_id = \$1`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"max"}).AddRow(42))
			}
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM usage_logs WHERE user_id = \$1 AND id <= \$2`).WithArgs(int64(7), int64(42)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			mock.ExpectQuery(`SELECT .* FROM usage_logs WHERE user_id = \$1 AND id <= \$2 ORDER BY created_at DESC, id DESC LIMIT \$3 OFFSET \$4`).WithArgs(int64(7), int64(42), 20, 20).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			_, page, err := repo.ListWithFilters(context.Background(), pagination.PaginationParams{Page: 2, PageSize: 20, SortBy: "created_at"}, usagestats.UsageLogFilters{UserID: 7, SnapshotMaxID: &boundary})
			require.NoError(t, err)
			require.Equal(t, int64(42), *page.SnapshotMaxID)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type inspectionMidnight struct{ zone string }

func (m inspectionMidnight) Match(value driver.Value) bool {
	actual, ok := value.(time.Time)
	if !ok {
		return false
	}
	loc, _ := time.LoadLocation(m.zone)
	now := time.Now().In(loc)
	// Expected boundary comes from the user's calendar, independently of repo helpers.
	return actual.Equal(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc))
}

func TestUserOverviewUsesRequestedTimezoneForBothDailyQueries(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_keys`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM api_keys`).WithArgs(int64(7), "active").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)SELECT.*total_requests.*FROM usage_logs`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d", "e", "f", "g", "h"}).AddRow(1, 10, 20, 30, 40, 1.0, 0.5, 100))
	mock.ExpectQuery(`(?s)SELECT.*today_requests.*FROM usage_logs`).WithArgs(int64(7), inspectionMidnight{"America/Los_Angeles"}).WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d", "e", "f", "g"}).AddRow(1, 10, 20, 30, 40, 1.0, 0.5))
	mock.ExpectQuery(`(?s)SELECT.*request_count.*FROM usage_logs`).WithArgs(sqlmock.AnyArg(), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"a", "b"}).AddRow(5, 150))
	mock.ExpectQuery(`(?s)SELECT.*FROM usage_logs ul`).WithArgs(int64(7), inspectionMidnight{"America/Los_Angeles"}).WillReturnRows(sqlmock.NewRows([]string{"platform"}))
	stats, err := repo.GetUserDashboardStats(context.Background(), 7, "America/Los_Angeles")
	require.NoError(t, err)
	require.Equal(t, "America/Los_Angeles", stats.Timezone)
	require.NotEmpty(t, stats.GeneratedAt)
	require.Equal(t, int64(100), stats.TodayTokens)
	require.NoError(t, mock.ExpectationsWereMet())
}
