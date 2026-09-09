package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestQuotaFollowResetObservationPropagatesDatabaseErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	failure := errors.New("database unavailable")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT platform").WillReturnError(failure)
	mock.ExpectRollback()
	_, err = (&openAIGroupQuotaFollowResetRepository{db: db}).ObserveWeeklyReset(context.Background(), 42, time.Now(), time.Now())
	require.ErrorIs(t, err, failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestQuotaFollowResetWorkerDoesNotSkipOnDatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	failure := errors.New("database unavailable")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT e.id").WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "source_account_id", "config_version", "effective_at", "include_monthly"}).AddRow(1, 2, 42, 1, time.Now(), true))
	mock.ExpectQuery("SELECT g.platform").WillReturnError(failure)
	mock.ExpectRollback()
	_, err = (&openAIGroupQuotaFollowResetRepository{db: db}).ProcessNextPending(context.Background())
	require.ErrorIs(t, err, failure)
	require.NoError(t, mock.ExpectationsWereMet())
}
