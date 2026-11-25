//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterStaticCatalogModelsHidesGLM54WithoutCodingPlan(t *testing.T) {
	ids := []string{"glm-5.4", "glm-5.3", "glm-4.6v"}
	for _, tc := range []struct {
		name        string
		account     *Account
		wantVisible bool
	}{
		{
			name:        "pay as you go api key",
			account:     &Account{Platform: PlatformZhipu, Credentials: map[string]any{"account_mode": AccountModePayG}},
			wantVisible: false,
		},
		{
			name:        "coding mode api key",
			account:     &Account{Platform: PlatformZhipu, Credentials: map[string]any{"account_mode": AccountModeCoding}},
			wantVisible: true,
		},
		{
			name:        "individual coding oauth",
			account:     &Account{Platform: PlatformZhipu, Credentials: map[string]any{"plan_kind": ZhipuPlanIndividualCodingPlan}},
			wantVisible: true,
		},
		{
			name:        "team coding oauth",
			account:     &Account{Platform: PlatformZhipu, Credentials: map[string]any{"plan_kind": ZhipuPlanTeamCodingPlan}},
			wantVisible: true,
		},
		{
			name:        "start plan needs its own observation",
			account:     &Account{Platform: PlatformZhipu, Credentials: map[string]any{"account_mode": AccountModeCoding, "plan_kind": ZhipuPlanStartPlan}},
			wantVisible: false,
		},
		{
			name:        "off peak has no new entitlement",
			account:     &Account{Platform: PlatformZhipu, Credentials: map[string]any{"account_mode": AccountModeCoding, "plan_kind": ZhipuPlanOffPeak}},
			wantVisible: false,
		},
		{
			name:        "unknown mode is not positive evidence",
			account:     &Account{Platform: PlatformZhipu},
			wantVisible: false,
		},
		{
			name:        "other platform unchanged",
			account:     &Account{Platform: PlatformOpenAI},
			wantVisible: true,
		},
		{
			name:        "nil account unchanged",
			account:     nil,
			wantVisible: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterStaticCatalogModels(tc.account, ids)
			if tc.wantVisible {
				require.Contains(t, got, "glm-5.4")
			} else {
				require.NotContains(t, got, "glm-5.4")
			}
			require.Contains(t, got, "glm-5.3")
		})
	}
}

func TestAggregateAccountModelAllowedRequiresCodingPlanEvidenceForGLM54(t *testing.T) {
	payg := &Account{Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": AccountModePayG}}
	require.True(t, aggregateAccountModelAllowed(context.Background(), payg, "glm-5.3"))
	require.False(t, aggregateAccountModelAllowed(context.Background(), payg, "glm-5.4"))

	coding := &Account{Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": AccountModeCoding}}
	require.True(t, aggregateAccountModelAllowed(context.Background(), coding, "glm-5.4"))

	// A live snapshot is positive evidence even on a pay-as-you-go account.
	observed := &Account{Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_mode": AccountModePayG}}
	observed.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{
		Models: map[string]UpstreamModelMetadata{"glm-5.4": {ID: "glm-5.4"}},
	})
	require.True(t, aggregateAccountModelAllowed(context.Background(), observed, "glm-5.4"))
}
