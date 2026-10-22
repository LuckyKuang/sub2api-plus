//go:build unit || !integration

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// The approved association contract requires one bounded, exact, owner-scoped
// lookup. A colliding client request ID alone must never select another bill.
func TestLiveUsageLookupBatchesExactBillingKeys(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	r := &usageLogRepository{sql: db}
	mock.ExpectQuery(`SELECT .* FROM usage_logs WHERE \(request_id = \$1 AND api_key_id = \$2 AND user_id = \$3\) OR \(request_id = \$4 AND api_key_id = \$5 AND user_id = \$6\) LIMIT 200`).
		WithArgs("client:collision", int64(9), int64(7), "ws:turn-2", int64(10), int64(8)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	rows, err := r.ReadLiveUsage(context.Background(), []service.LiveUsageKey{{RequestID: "client:collision", UserID: 7, APIKeyID: 9}, {RequestID: "ws:turn-2", UserID: 8, APIKeyID: 10}})
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
	_, err = r.ReadLiveUsage(context.Background(), make([]service.LiveUsageKey, 201))
	require.Error(t, err)
}
