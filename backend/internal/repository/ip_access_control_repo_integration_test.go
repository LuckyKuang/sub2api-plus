//go:build integration

package repository

import (
	"context"
	"testing"

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
