package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWeeklyResetAdvancedRequiresExpiredWindow(t *testing.T) {
	previous := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	next := previous.Add(7 * 24 * time.Hour)
	require.False(t, openAIWeeklyResetAdvanced(previous, previous.Add(time.Minute), previous.Add(-time.Hour)), "clock skew before expiry must not reset groups")
	require.False(t, openAIWeeklyResetAdvanced(previous, next, previous.Add(-time.Second)))
	require.False(t, openAIWeeklyResetAdvanced(previous, previous.Add(time.Minute), previous), "clock skew after expiry must not reset groups")
	require.False(t, openAIWeeklyResetAdvanced(previous, previous.Add(24*time.Hour), previous), "a one-day jump is not a weekly window")
	require.True(t, openAIWeeklyResetAdvanced(previous, next, previous))
	require.True(t, openAIWeeklyResetAdvanced(previous, next, previous.Add(time.Minute)))
}

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
