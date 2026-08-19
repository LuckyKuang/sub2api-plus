//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIPAccessControlRepositoryCreateManualRulePostgres(t *testing.T) {
	ctx := context.Background()
	repo := NewIPAccessControlRepository(integrationDB)
	rule, err := repo.CreateManualIPAccessRule(ctx, &service.IPAccessRule{
		IPOrCIDR: "192.0.2.0/24",
		RuleKind: service.IPAccessRuleKindManualBlock,
		Reason:   "integration test",
	})
	require.NoError(t, err)
	require.NotNil(t, rule)
	require.NotZero(t, rule.ID)
	require.NotNil(t, rule.BlockedAt)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_access_rules WHERE id = $1", rule.ID)
	})
}

func TestIPAccessControlRepositoryRecordsConsecutiveFailuresAndBlocksAtThreshold(t *testing.T) {
	ctx := context.Background()
	const normalizedIP = "192.0.2.203"
	cleanup := func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_access_rules WHERE ip_or_cidr = $1", normalizedIP)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_login_failure_states WHERE normalized_ip = $1", normalizedIP)
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := NewIPAccessControlRepository(integrationDB)
	first, err := repo.RecordFailedLogin(ctx, normalizedIP, 2, 15*time.Minute, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, first.FailureCount)
	require.False(t, first.Blocked)

	second, err := repo.RecordFailedLogin(ctx, normalizedIP, 2, 15*time.Minute, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 2, second.FailureCount)
	require.True(t, second.Blocked)
	require.NotNil(t, second.Rule)
	require.Equal(t, service.IPAccessRuleKindAutoBlock, second.Rule.RuleKind)

	var persistedCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT failure_count FROM ip_login_failure_states WHERE normalized_ip = $1",
		normalizedIP,
	).Scan(&persistedCount))
	require.Equal(t, 2, persistedCount)

	var activeAutoBlock bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM ip_access_rules
		WHERE ip_or_cidr = $1 AND rule_kind = 'auto_block' AND status = 'active'
		AND expires_at > NOW()
	)`, normalizedIP).Scan(&activeAutoBlock))
	require.True(t, activeAutoBlock)
}

func TestIPAccessControlRepositoryQuickBlockUpgradesManualRuleToPermanent(t *testing.T) {
	ctx := context.Background()
	const normalizedIP = "192.0.2.204"
	_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ip_access_rules WHERE ip_or_cidr = $1", normalizedIP)
	_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ip_login_failure_states WHERE normalized_ip = $1", normalizedIP)

	actor := mustCreateUser(t, integrationEntClient, &service.User{Role: service.RoleAdmin})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_access_rules WHERE ip_or_cidr = $1", normalizedIP)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_login_failure_states WHERE normalized_ip = $1", normalizedIP)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", actor.ID)
	})

	_, err := integrationDB.ExecContext(ctx, `INSERT INTO ip_login_failure_states (
		normalized_ip, failure_count, window_started_at, last_failed_at, created_at, updated_at
	) VALUES ($1, 3, NOW(), NOW(), NOW(), NOW())`, normalizedIP)
	require.NoError(t, err)

	repo := NewIPAccessControlRepository(integrationDB)
	expiresAt := time.Now().UTC().Add(time.Hour)
	temporary, err := repo.CreateManualIPAccessRule(ctx, &service.IPAccessRule{
		IPOrCIDR: normalizedIP, RuleKind: service.IPAccessRuleKindManualBlock,
		Reason: "temporary", ExpiresAt: &expiresAt, CreatedByUserID: &actor.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, temporary)
	require.NotNil(t, temporary.ExpiresAt)

	result, err := repo.CreateManualIPBlockForFailureState(ctx, normalizedIP, "permanent", actor.ID)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.AlreadyBlocked)
	require.NotNil(t, result.Rule)
	require.Equal(t, temporary.ID, result.Rule.ID)
	require.Equal(t, service.IPAccessRuleKindManualBlock, result.Rule.RuleKind)
	require.Nil(t, result.Rule.ExpiresAt)

	var activeManualCount, failureCount int
	var permanent bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(bool_and(expires_at IS NULL), false)
		FROM ip_access_rules
		WHERE ip_or_cidr = $1 AND rule_kind = 'manual_block' AND status = 'active'`, normalizedIP).
		Scan(&activeManualCount, &permanent))
	require.Equal(t, 1, activeManualCount)
	require.True(t, permanent)
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT failure_count FROM ip_login_failure_states WHERE normalized_ip = $1",
		normalizedIP,
	).Scan(&failureCount))
	require.Equal(t, 3, failureCount)
}

func TestIPAccessControlRepositoryQuickBlockSupersedesExactAutoBlock(t *testing.T) {
	ctx := context.Background()
	const normalizedIP = "192.0.2.205"
	cleanup := func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_access_rules WHERE ip_or_cidr = $1", normalizedIP)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ip_login_failure_states WHERE normalized_ip = $1", normalizedIP)
	}
	cleanup()

	actor := mustCreateUser(t, integrationEntClient, &service.User{Role: service.RoleAdmin})
	t.Cleanup(func() {
		cleanup()
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", actor.ID)
	})

	repo := NewIPAccessControlRepository(integrationDB)
	first, err := repo.RecordFailedLogin(ctx, normalizedIP, 2, 15*time.Minute, time.Hour)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.False(t, first.Blocked)
	automatic, err := repo.RecordFailedLogin(ctx, normalizedIP, 2, 15*time.Minute, time.Hour)
	require.NoError(t, err)
	require.NotNil(t, automatic)
	require.True(t, automatic.Blocked)
	require.NotNil(t, automatic.Rule)
	require.Equal(t, service.IPAccessRuleKindAutoBlock, automatic.Rule.RuleKind)

	result, err := repo.CreateManualIPBlockForFailureState(ctx, normalizedIP, "permanent", actor.ID)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.AlreadyBlocked)
	require.NotNil(t, result.Rule)
	require.Equal(t, service.IPAccessRuleKindManualBlock, result.Rule.RuleKind)
	require.Nil(t, result.Rule.ExpiresAt)

	var autoStatus string
	var releasedBy int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status, released_by_user_id
		FROM ip_access_rules WHERE id = $1`, automatic.Rule.ID).Scan(&autoStatus, &releasedBy))
	require.Equal(t, string(service.IPAccessRuleStatusReleased), autoStatus)
	require.Equal(t, actor.ID, releasedBy)

	_, err = repo.ReleaseIPAccessRuleAndReset(ctx, result.Rule.ID, actor.ID)
	require.NoError(t, err)
	var activeExactBlocks int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ip_access_rules
		WHERE ip_or_cidr = $1 AND rule_kind IN ('manual_block', 'auto_block')
		AND status = 'active' AND (expires_at IS NULL OR expires_at > NOW())`, normalizedIP).Scan(&activeExactBlocks))
	require.Zero(t, activeExactBlocks)
}
