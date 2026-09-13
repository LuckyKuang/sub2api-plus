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

func weeklyObservation(resetAt, observedAt time.Time, used float64) service.OpenAIWeeklyQuotaObservation {
	return service.OpenAIWeeklyQuotaObservation{ResetAt: resetAt, ObservedAt: observedAt, UsedPercent: &used, FromSession: true}
}

func standaloneWeeklyObservation(resetAt, observedAt time.Time, used float64) service.OpenAIWeeklyQuotaObservation {
	return service.OpenAIWeeklyQuotaObservation{ResetAt: resetAt, ObservedAt: observedAt, UsedPercent: &used}
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
			state, accepted, reset := advanceOpenAIWeeklyObservation(openAIWeeklyObservationState{}, weeklyObservation(tc.previous, now.Add(-time.Minute), 80))
			require.True(t, accepted)
			require.False(t, reset)
			state, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now, tc.used))
			require.True(t, accepted)
			require.False(t, reset, "the first session cannot confirm an early reset")
			state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now.Add(500*time.Millisecond), tc.used))
			require.False(t, reset, "a sample under one second later cannot confirm pending evidence")
			state, _, reset = advanceOpenAIWeeklyObservation(state, standaloneWeeklyObservation(tc.next, now.Add(2*time.Second), tc.used))
			require.False(t, reset, "a standalone quota query cannot confirm pending evidence")
			state, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now.Add(time.Minute), tc.used+1))
			require.True(t, accepted)
			require.True(t, reset, "a later real session confirms the new window without observing zero")
			require.Equal(t, int64(1), state.sequence)
			require.Equal(t, tc.next, state.resetAt.Time)
			state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.next, now.Add(2*time.Minute), tc.used+2))
			require.False(t, reset)
			require.Equal(t, int64(1), state.sequence)
			_, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(tc.previous, now.Add(-time.Minute), 80))
			require.False(t, accepted, "a delayed older sample must be ignored")
			require.False(t, reset)
		})
	}
}

func TestQuotaFollowResetRejectsClockDriftAndUnconfirmedDrops(t *testing.T) {
	now := time.Now().UTC()
	deadline := now.Add(6 * 24 * time.Hour)
	state, _, _ := advanceOpenAIWeeklyObservation(openAIWeeklyObservationState{}, weeklyObservation(deadline, now, 80))
	state, _, reset := advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline.Add(time.Minute), now.Add(time.Minute), 80))
	require.False(t, reset)
	require.Equal(t, deadline, state.resetAt.Time, "clock corrections do not move the canonical deadline")
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(2*time.Minute), 0))
	require.False(t, reset)
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(3*time.Minute), 81))
	require.False(t, reset, "a later session with high usage disproves a stale zero response")
	require.False(t, state.pendingResetAt.Valid)
	state, _, _ = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(4*time.Minute), 0))
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(5*time.Minute), 0))
	require.True(t, reset)
	state, _, _ = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(6*time.Minute), 81))
	state, _, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now.Add(7*time.Minute), 1))
	require.False(t, reset, "late high usage cannot restore a pre-reset watermark")
	require.False(t, state.pendingResetAt.Valid)
}

func TestQuotaFollowResetHandshakeHeadersDoNotConfirmPending(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	previous := now.Add(7 * 24 * time.Hour)
	next := now.Add(6*24*time.Hour + 22*time.Hour)
	state, accepted, reset := advanceOpenAIWeeklyObservation(openAIWeeklyObservationState{}, weeklyObservation(previous, now.Add(-time.Minute), 80))
	require.True(t, accepted)
	require.False(t, reset)
	state, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(next, now, 7))
	require.True(t, accepted)
	require.False(t, reset)
	require.True(t, state.pendingResetAt.Valid)

	state, accepted, reset = advanceOpenAIWeeklyObservation(state, standaloneWeeklyObservation(next, now.Add(2*time.Second), 7))
	require.True(t, accepted)
	require.False(t, reset, "handshake leftovers with the new window must not confirm pending evidence")
	require.True(t, state.pendingResetAt.Valid)

	state, accepted, reset = advanceOpenAIWeeklyObservation(state, standaloneWeeklyObservation(previous, now.Add(3*time.Second), 80))
	require.True(t, accepted)
	require.False(t, reset, "handshake leftovers with the pre-drop watermark must not dismiss pending evidence")
	require.True(t, state.pendingResetAt.Valid)

	state, accepted, reset = advanceOpenAIWeeklyObservation(state, weeklyObservation(next, now.Add(time.Minute), 8))
	require.True(t, accepted)
	require.True(t, reset)
}

func TestQuotaFollowResetNaturalWindowAndInvalidObservations(t *testing.T) {
	now := time.Now().UTC()
	state := openAIWeeklyObservationState{resetAt: sql.NullTime{Time: now, Valid: true}, observedAt: now.Add(-time.Hour)}
	next, accepted, reset := advanceOpenAIWeeklyObservation(state, weeklyObservation(now.Add(7*24*time.Hour), now, 7))
	require.True(t, accepted)
	require.True(t, reset)
	require.Equal(t, int64(1), next.sequence)
	for _, deadline := range []time.Time{time.Time{}, now.Add(-time.Second), now.Add(8 * 24 * time.Hour)} {
		_, accepted, reset := advanceOpenAIWeeklyObservation(state, weeklyObservation(deadline, now, 0))
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
	_, err = (&openAIGroupQuotaFollowResetRepository{db: db}).ObserveWeeklyReset(context.Background(), 42, service.OpenAIWeeklyQuotaObservation{ResetAt: time.Now(), ObservedAt: time.Now(), FromSession: true})
	require.ErrorIs(t, err, failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestQuotaFollowResetIgnoresStandaloneRepositoryObservations(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	created, err := (&openAIGroupQuotaFollowResetRepository{db: db}).ObserveWeeklyReset(
		context.Background(),
		42,
		standaloneWeeklyObservation(time.Now().Add(7*24*time.Hour), time.Now(), 7),
	)
	require.NoError(t, err)
	require.Zero(t, created)
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
