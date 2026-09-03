//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func createClientDisconnectRiskUser(t *testing.T, role string) *service.User {
	t.Helper()
	repo := newUserRepositoryWithSQL(testEntClient(t), integrationDB)
	user := &service.User{
		Email:        fmt.Sprintf("disconnect-risk-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "test-password-hash",
		Role:         role,
		Status:       service.StatusActive,
		Concurrency:  5,
	}
	require.NoError(t, repo.Create(context.Background(), user))
	return user
}

func beginAndFinalizeDisconnectRisk(
	t *testing.T,
	repo service.ClientDisconnectRiskRepository,
	userID int64,
	requestID string,
	outcome service.ClientDisconnectOutcome,
	threshold int,
) service.ClientDisconnectRiskResult {
	t.Helper()
	ctx := context.Background()
	sequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: userID, Generation: 1, RequestID: requestID, Protocol: "test",
	})
	require.NoError(t, err)
	result, err := repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: userID, Generation: 1, Sequence: sequence,
		Outcome: outcome, Threshold: threshold, Enforce: true,
	})
	require.NoError(t, err)
	return result
}

func TestClientDisconnectRiskRepository_SuccessResetsStrictStreak(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)

	for i := 1; i <= 9; i++ {
		result := beginAndFinalizeDisconnectRisk(t, repo, user.ID, fmt.Sprintf("first-%d", i), service.ClientDisconnectOutcomeDisconnected, 10)
		require.False(t, result.AutoBanned)
	}
	result := beginAndFinalizeDisconnectRisk(t, repo, user.ID, "success", service.ClientDisconnectOutcomeCompleted, 10)
	require.Zero(t, result.ConsecutiveCount)
	for i := 1; i <= 9; i++ {
		result = beginAndFinalizeDisconnectRisk(t, repo, user.ID, fmt.Sprintf("second-%d", i), service.ClientDisconnectOutcomeDisconnected, 10)
		require.False(t, result.AutoBanned)
	}

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM users WHERE id = $1`, user.ID).Scan(&status))
	require.Equal(t, service.StatusActive, status)
	require.Equal(t, 9, result.ConsecutiveCount)

	result = beginAndFinalizeDisconnectRisk(t, repo, user.ID, "threshold", service.ClientDisconnectOutcomeDisconnected, 10)
	require.True(t, result.AutoBanned)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM users WHERE id = $1`, user.ID).Scan(&status))
	require.Equal(t, service.StatusDisabled, status)
	sequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 1, RequestID: "after-ban",
	})
	require.NoError(t, err)
	require.Zero(t, sequence, "disabled users must not create new risk events")
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET status = 'active' WHERE id = $1`, user.ID)
	require.NoError(t, err)
	result = beginAndFinalizeDisconnectRisk(t, repo, user.ID, "after-reenable", service.ClientDisconnectOutcomeDisconnected, 10)
	require.Equal(t, 1, result.ConsecutiveCount, "re-enabling a user must start a fresh streak")
}

func TestClientDisconnectRiskRepository_ProcessesConcurrentResultsInAcceptanceOrder(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)

	successSequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "success-first"})
	require.NoError(t, err)
	disconnectSequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "disconnect-second"})
	require.NoError(t, err)
	require.Less(t, successSequence, disconnectSequence)

	result, err := repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 1, Sequence: disconnectSequence,
		Outcome: service.ClientDisconnectOutcomeDisconnected, Threshold: 10, Enforce: true,
	})
	require.NoError(t, err)
	require.Zero(t, result.ConsecutiveCount, "later result must wait for the earlier accepted request")

	result, err = repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 1, Sequence: successSequence,
		Outcome: service.ClientDisconnectOutcomeCompleted, Threshold: 10, Enforce: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.ConsecutiveCount)
}

func TestClientDisconnectRiskRepository_NeutralOutcomeLeavesStreakUnchanged(t *testing.T) {
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)

	result := beginAndFinalizeDisconnectRisk(t, repo, user.ID, "disconnect-1", service.ClientDisconnectOutcomeDisconnected, 10)
	require.Equal(t, 1, result.ConsecutiveCount)
	result = beginAndFinalizeDisconnectRisk(t, repo, user.ID, "neutral", service.ClientDisconnectOutcomeNeutral, 10)
	require.Equal(t, 1, result.ConsecutiveCount)
	result = beginAndFinalizeDisconnectRisk(t, repo, user.ID, "disconnect-2", service.ClientDisconnectOutcomeDisconnected, 10)
	require.Equal(t, 2, result.ConsecutiveCount)
}

func TestClientDisconnectRiskRepository_UsesEachFinalizedEventsThreshold(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)

	first, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "first"})
	require.NoError(t, err)
	second, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "second"})
	require.NoError(t, err)

	_, err = repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 1, Sequence: second,
		Outcome: service.ClientDisconnectOutcomeDisconnected, Threshold: 1, Enforce: true,
	})
	require.NoError(t, err)
	result, err := repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 1, Sequence: first,
		Outcome: service.ClientDisconnectOutcomeNeutral, Threshold: 1000, Enforce: true,
	})
	require.NoError(t, err)
	require.True(t, result.AutoBanned, "the queued second event must retain its own threshold")
}

func TestClientDisconnectRiskRepository_ExpiresStalePendingWithoutBreakingStreak(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)

	stale, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "stale"})
	require.NoError(t, err)
	next, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "next"})
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
UPDATE client_disconnect_risk_events
SET accepted_at = NOW() - INTERVAL '25 hours'
WHERE user_id = $1 AND generation = 1 AND sequence = $2`, user.ID, stale)
	require.NoError(t, err)

	result, err := repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 1, Sequence: next,
		Outcome: service.ClientDisconnectOutcomeDisconnected, Threshold: 10, Enforce: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.ConsecutiveCount)

	var outcome string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT outcome FROM client_disconnect_risk_events
WHERE user_id = $1 AND generation = 1 AND sequence = $2`, user.ID, stale).Scan(&outcome))
	require.Equal(t, string(service.ClientDisconnectOutcomeNeutral), outcome)
}

func TestClientDisconnectRiskRepository_AdminCannotBeAutoBanned(t *testing.T) {
	ctx := context.Background()
	admin := createClientDisconnectRiskUser(t, service.RoleAdmin)
	repo := NewClientDisconnectRiskRepository(integrationDB)
	for i := 1; i <= 3; i++ {
		result := beginAndFinalizeDisconnectRisk(t, repo, admin.ID, fmt.Sprintf("admin-%d", i), service.ClientDisconnectOutcomeDisconnected, 1)
		require.False(t, result.AutoBanned)
	}
	var stateCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_disconnect_risk_states WHERE user_id = $1`, admin.ID).Scan(&stateCount))
	require.Equal(t, 1, stateCount, "administrator requests remain auditable")
	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM users WHERE id = $1`, admin.ID).Scan(&status))
	require.Equal(t, service.StatusActive, status)
	var enforce bool
	var completionStatus string
	var usageMissing bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT enforce, completion_status, usage_missing
FROM client_disconnect_risk_events
WHERE user_id = $1 ORDER BY sequence DESC LIMIT 1`, admin.ID).Scan(&enforce, &completionStatus, &usageMissing))
	require.False(t, enforce, "the repository must persist the effective administrator exemption")
	require.Equal(t, "client_disconnected", completionStatus)
	require.True(t, usageMissing)
	var streak int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT consecutive_count FROM client_disconnect_risk_states WHERE user_id = $1`, admin.ID).Scan(&streak))
	require.Zero(t, streak, "administrator audit events must not build a latent streak")

	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET role = 'user' WHERE id = $1`, admin.ID)
	require.NoError(t, err)
	result := beginAndFinalizeDisconnectRisk(t, repo, admin.ID, "after-demotion", service.ClientDisconnectOutcomeDisconnected, 2)
	require.Equal(t, 1, result.ConsecutiveCount)
	require.False(t, result.AutoBanned)
}

func TestClientDisconnectRiskRepository_PersistsLifecycleMetadataAndPriorGenerations(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)
	client := testEntClient(t)
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: fmt.Sprintf("sk-disconnect-lifecycle-%d", time.Now().UnixNano()), Name: "disconnect-lifecycle",
	})

	first, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 1, RequestID: "generation-one", APIKeyID: apiKey.ID, Protocol: "responses",
	})
	require.NoError(t, err)
	_, err = repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 1, Sequence: first,
		Outcome: service.ClientDisconnectOutcomeDisconnected, Threshold: 10, Enforce: true,
		CompletionStatus: "client_disconnected", UsageSource: "partial", UsageMissing: false,
	})
	require.NoError(t, err)

	second, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 2, RequestID: "generation-two", APIKeyID: apiKey.ID, Protocol: "grok_realtime",
	})
	require.NoError(t, err)
	_, err = repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 2, Sequence: second,
		Outcome: service.ClientDisconnectOutcomeCompleted, Threshold: 10, Enforce: true,
	})
	require.NoError(t, err)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM client_disconnect_risk_events
WHERE user_id = $1 AND request_id IN ('generation-one', 'generation-two')`, user.ID).Scan(&count))
	require.Equal(t, 2, count, "changing settings generation must not erase lifecycle audit history")

	var status, source string
	var missing bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT completion_status, usage_source, usage_missing
FROM client_disconnect_risk_events
WHERE user_id = $1 AND generation = 1 AND request_id = 'generation-one'`, user.ID).Scan(&status, &source, &missing))
	require.Equal(t, "client_disconnected", status)
	require.Equal(t, "partial", source)
	require.False(t, missing)

	events, total, err := repo.ListEvents(ctx, service.ClientDisconnectRiskEventFilter{
		UserID: user.ID, CompletionStatus: "client_disconnected", UsageMissing: func() *bool { value := false; return &value }(),
		Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, events, 1)
	require.Equal(t, "generation-one", events[0].RequestID)
	require.Equal(t, apiKey.ID, *events[0].APIKeyID)

	_, err = repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 2, RequestID: "still-pending", APIKeyID: apiKey.ID, Protocol: "responses",
	})
	require.NoError(t, err)
	pending, pendingTotal, err := repo.ListEvents(ctx, service.ClientDisconnectRiskEventFilter{
		UserID: user.ID, CompletionStatus: "pending", Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), pendingTotal)
	require.Len(t, pending, 1)
	require.Equal(t, "still-pending", pending[0].RequestID)
}

func TestClientDisconnectRiskRepository_AdminPromotionClearsExistingStreak(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)
	result := beginAndFinalizeDisconnectRisk(t, repo, user.ID, "before-promotion", service.ClientDisconnectOutcomeDisconnected, 10)
	require.Equal(t, 1, result.ConsecutiveCount)

	_, err := integrationDB.ExecContext(ctx, `UPDATE users SET role = 'admin' WHERE id = $1`, user.ID)
	require.NoError(t, err)
	var streak int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT consecutive_count FROM client_disconnect_risk_states WHERE user_id = $1`, user.ID).Scan(&streak))
	require.Zero(t, streak)
	sequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 1, RequestID: "after-promotion",
	})
	require.NoError(t, err)
	require.Positive(t, sequence, "promoted administrators remain auditable while enforcement stays disabled by the service")
}

func TestClientDisconnectRiskRepository_DeduplicatesLogicalRequest(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)
	first, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "same-request"})
	require.NoError(t, err)
	second, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{UserID: user.ID, Generation: 1, RequestID: "same-request"})
	require.NoError(t, err)
	require.Equal(t, first, second)
	var nextSequence int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT next_sequence FROM client_disconnect_risk_states WHERE user_id = $1`, user.ID).Scan(&nextSequence))
	require.Equal(t, int64(1), nextSequence)
}

func TestClientDisconnectRiskRepository_StaleGenerationCannotRollBackState(t *testing.T) {
	ctx := context.Background()
	user := createClientDisconnectRiskUser(t, service.RoleUser)
	repo := NewClientDisconnectRiskRepository(integrationDB)

	currentSequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 2, RequestID: "current-generation",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), currentSequence)

	staleSequence, err := repo.Begin(ctx, service.ClientDisconnectRiskBegin{
		UserID: user.ID, Generation: 1, RequestID: "stale-generation",
	})
	require.NoError(t, err)
	require.Zero(t, staleSequence)

	var generation, nextSequence int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT generation, next_sequence
FROM client_disconnect_risk_states
WHERE user_id = $1`, user.ID).Scan(&generation, &nextSequence))
	require.Equal(t, int64(2), generation)
	require.Equal(t, int64(1), nextSequence)

	var currentEvents, staleEvents int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM client_disconnect_risk_events
WHERE user_id = $1 AND generation = 2`, user.ID).Scan(&currentEvents))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM client_disconnect_risk_events
WHERE user_id = $1 AND generation = 1`, user.ID).Scan(&staleEvents))
	require.Equal(t, 1, currentEvents)
	require.Zero(t, staleEvents)

	result, err := repo.Finalize(ctx, service.ClientDisconnectRiskFinalize{
		UserID: user.ID, Generation: 2, Sequence: currentSequence,
		Outcome: service.ClientDisconnectOutcomeDisconnected, Threshold: 10, Enforce: true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.ConsecutiveCount)
}
