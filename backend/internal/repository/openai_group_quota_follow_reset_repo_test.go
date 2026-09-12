package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func weeklyObservation(resetAt, observedAt time.Time, used float64, query bool) service.OpenAIWeeklyQuotaObservation {
	return service.OpenAIWeeklyQuotaObservation{ResetAt: resetAt, ObservedAt: observedAt, UsedPercent: &used, QuotaQuery: query}
}

func TestQuotaFollowResetConfirmsEarlyAndSameDeadlineResets(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name           string
		previous, next time.Time
		used           float64
	}{
		{"early global reset already at seven percent", now.Add(24 * time.Hour), now.Add(6*24*time.Hour + 22*time.Hour), 7},
		{"early deadline moved backwards", now.Add(7 * 24 * time.Hour), now.Add(6 * 24 * time.Hour), 7},
		{"same deadline zero", now.Add(6 * 24 * time.Hour), now.Add(6 * 24 * time.Hour), 0},
		{"same deadline already used", now.Add(6 * 24 * time.Hour), now.Add(6 * 24 * time.Hour), 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, accepted, reset := advanceOpenAIWeeklyObservation(openAIWeeklyObservationState{}, weeklyObservation(tc.previous, now.Add(-time.Minute), 80, true))
			require.True(t, accepted)
			require.False(t, reset)
			state, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now, tc.used, false))
			require.True(t, accepted)
			require.False(t, reset, "one traffic response cannot confirm an early reset")
			for i := 1; i < 5; i++ {
				state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now.Add(time.Duration(i)*time.Second), tc.used, false))
				require.False(t, reset)
			}
			state, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now.Add(time.Minute), tc.used+1, true))
			require.True(t, accepted)
			require.True(t, reset, "a later query confirms the new window without observing zero")
			require.Equal(t, int64(1), state.sequence)
			require.Equal(t, tc.next, state.resetAt.Time)
			state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now.Add(2*time.Minute), tc.used+2, true))
			require.False(t, reset)
			require.Equal(t, int64(1), state.sequence)
			_, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.previous, now.Add(-time.Minute), 80, true))
			require.False(t, accepted, "a delayed older query must be ignored")
			require.False(t, reset)
		})
	}
}

func TestQuotaFollowResetRejectsClockDriftAndUnconfirmedDrops(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(6 * 24 * time.Hour)
	state, _, _ := advanceOpenAIWeeklyObservation(openAIWeeklyObservationState{}, weeklyObservation(deadline, now, 80, true))
	state, _, reset := advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline.Add(time.Minute), now.Add(time.Minute), 80, true))
	require.False(t, reset)
	require.Equal(t, deadline, state.resetAt.Time, "clock corrections do not move the canonical deadline")
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(2*time.Minute), 0, false))
	require.False(t, reset)
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(3*time.Minute), 81, true))
	require.False(t, reset, "the fresh query disproves a stale zero response")
	require.False(t, state.pendingResetAt.Valid)
	state, _, _ = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(4*time.Minute), 0, true))
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(5*time.Minute), 0, true))
	require.True(t, reset)
	state, _, _ = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(6*time.Minute), 81, false))
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(7*time.Minute), 1, true))
	require.False(t, reset, "late high traffic usage cannot restore a pre-reset watermark")
	require.False(t, state.pendingResetAt.Valid)
}

func TestQuotaFollowResetNaturalWindowAndInvalidObservations(t *testing.T) {
	now := time.Now().UTC()
	state := openAIWeeklyObservationState{resetAt: sql.NullTime{Time: now, Valid: true}, observedAt: now.Add(-time.Hour)}
	next, accepted, reset := advanceOpenAIWeeklyObservation(state, weeklyObservation(now.Add(7*24*time.Hour), now, 7, false))
	require.True(t, accepted)
	require.True(t, reset)
	require.Equal(t, int64(1), next.sequence)
	for _, deadline := range []time.Time{time.Time{}, now.Add(-time.Second), now.Add(8 * 24 * time.Hour)} {
		_, accepted, reset := advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now, 0, true))
		require.False(t, accepted)
		require.False(t, reset)
	}
}

func TestOpenAIWeeklyResetNaturalRollover(t *testing.T) {
	previous := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	next := previous.Add(7 * 24 * time.Hour)
	require.False(t, openAIWeeklyResetAdvanced(previous, previous.Add(time.Minute), previous.Add(-time.Hour)), "clock skew before expiry must not reset groups")
	require.False(t, openAIWeeklyResetAdvanced(previous, next, previous.Add(-time.Second)))
	require.False(t, openAIWeeklyResetAdvanced(previous, previous.Add(time.Minute), previous), "clock skew after expiry must not reset groups")
	require.True(t, openAIWeeklyResetAdvanced(previous, previous.Add(24*time.Hour), previous), "a confirmed weekly window need not advance by half a week")
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
	_, err = (&openAIGroupQuotaFollowResetRepository{db: db}).ObserveWeeklyReset(context.Background(), 42, service.OpenAIWeeklyQuotaObservation{ResetAt: time.Now(), ObservedAt: time.Now()})
	require.ErrorIs(t, err, failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestQuotaFollowResetWorkerDoesNotSkipOnDatabaseError(t *testing.T) {
	for _, stage := range []string{"group lock", "source validation"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			failure := errors.New("database unavailable")
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT e.id").WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "source_account_id", "config_version", "effective_at", "include_monthly"}).AddRow(1, 2, 42, 1, time.Now(), true))
			if stage == "group lock" {
				mock.ExpectQuery("SELECT id FROM groups").WillReturnError(failure)
			} else {
				mock.ExpectQuery("SELECT id FROM groups").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
				mock.ExpectQuery("SELECT g.platform").WillReturnError(failure)
			}
			mock.ExpectRollback()
			_, err = (&openAIGroupQuotaFollowResetRepository{db: db}).ProcessNextPending(context.Background())
			require.ErrorIs(t, err, failure)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
