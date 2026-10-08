//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnmodels"
	"github.com/stretchr/testify/require"
)

type accountRepoStubForCompositeModelsList struct {
	accountRepoStub
	accounts []Account
}

func (s *accountRepoStubForCompositeModelsList) ListSchedulableByGroupID(_ context.Context, _ int64) ([]Account, error) {
	return s.accounts, nil
}

func TestAdminService_CreateCompositeGroupCopiesAccountsFromConcreteGroups(t *testing.T) {
	var copiedFrom []int64
	var boundGroupID int64
	var boundAccountIDs []int64
	groupRepo := &groupRepoStubForAdmin{
		createID: 99,
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformOpenAI},
			20: {ID: 20, Platform: PlatformGemini},
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			copiedFrom = append([]int64{}, groupIDs...)
			return []int64{101, 202}, nil
		},
		bindAccountsToGroupFn: func(groupID int64, accountIDs []int64) error {
			boundGroupID = groupID
			boundAccountIDs = append([]int64{}, accountIDs...)
			return nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo}

	group, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                        "Composite",
		Platform:                    PlatformComposite,
		RateMultiplier:              1,
		MaxReasoningEffort:          "medium",
		MaxReasoningEffortOverLimit: ReasoningEffortOverLimitDeny,
		ReasoningEffortMappings: []ReasoningEffortMapping{
			{From: "max", To: "xhigh"},
		},
		CopyAccountsFromGroupIDs: []int64{10, 20, 10},
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, groupRepo.created.Platform)
	require.Equal(t, "medium", groupRepo.created.MaxReasoningEffort)
	require.Equal(t, ReasoningEffortOverLimitDeny, groupRepo.created.MaxReasoningEffortOverLimit)
	require.Equal(t, []ReasoningEffortMapping{{From: "max", To: "xhigh"}}, groupRepo.created.ReasoningEffortMappings)
	require.Equal(t, int64(99), group.ID)
	require.Equal(t, int64(2), group.AccountCount)
	require.ElementsMatch(t, []int64{10, 20}, copiedFrom)
	require.Equal(t, int64(99), boundGroupID)
	require.ElementsMatch(t, []int64{101, 202}, boundAccountIDs)
}

func TestAdminService_UpdateCompositeGroupCopiesAccountsFromConcreteGroups(t *testing.T) {
	var clearedGroupID int64
	var copiedFrom []int64
	var boundGroupID int64
	var boundAccountIDs []int64
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			10: {ID: 10, Platform: PlatformOpenAI},
			20: {ID: 20, Platform: PlatformGrok},
			99: {ID: 99, Platform: PlatformComposite, RateMultiplier: 1, SubscriptionType: SubscriptionTypeStandard},
		},
		deleteAccountGroupsByGroupIDFn: func(groupID int64) (int64, error) {
			clearedGroupID = groupID
			return 2, nil
		},
		getAccountIDsByGroupIDsFn: func(groupIDs []int64) ([]int64, error) {
			copiedFrom = append([]int64{}, groupIDs...)
			return []int64{301, 302}, nil
		},
		bindAccountsToGroupFn: func(groupID int64, accountIDs []int64) error {
			boundGroupID = groupID
			boundAccountIDs = append([]int64{}, accountIDs...)
			return nil
		},
	}
	svc := &adminServiceImpl{groupRepo: groupRepo}
	maxReasoningEffort := "low"
	maxReasoningEffortOverLimit := ReasoningEffortOverLimitDeny
	reasoningEffortMappings := []ReasoningEffortMapping{{From: "max", To: "high"}}

	group, err := svc.UpdateGroup(context.Background(), 99, &UpdateGroupInput{
		MaxReasoningEffort:          &maxReasoningEffort,
		MaxReasoningEffortOverLimit: &maxReasoningEffortOverLimit,
		ReasoningEffortMappings:     &reasoningEffortMappings,
		CopyAccountsFromGroupIDs:    []int64{10, 20},
	})

	require.NoError(t, err)
	require.Equal(t, PlatformComposite, group.Platform)
	require.Equal(t, "low", group.MaxReasoningEffort)
	require.Equal(t, ReasoningEffortOverLimitDeny, group.MaxReasoningEffortOverLimit)
	require.Equal(t, reasoningEffortMappings, group.ReasoningEffortMappings)
	require.Equal(t, int64(99), clearedGroupID)
	require.ElementsMatch(t, []int64{10, 20}, copiedFrom)
	require.Equal(t, int64(99), boundGroupID)
	require.ElementsMatch(t, []int64{301, 302}, boundAccountIDs)
}

func TestAdminService_CreateAccountAllowsCompositeGroupAssignment(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{createID: 7}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "OpenAI account",
		Platform:              PlatformOpenAI,
		Type:                  AccountTypeAPIKey,
		Concurrency:           1,
		GroupIDs:              []int64{99},
		SkipDefaultGroupBind:  true,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Equal(t, PlatformOpenAI, accountRepo.createAccount.Platform)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_UpdateAccountAllowsCompositeGroupAssignment(t *testing.T) {
	accountRepo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			7: {ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Status: StatusActive, Extra: map[string]any{}},
		},
	}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}
	groupIDs := []int64{99}

	account, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(7), account.ID)
	require.Len(t, accountRepo.updatedAccounts, 1)
	require.ElementsMatch(t, []int64{99}, accountRepo.bindGroupsByAccount[7])
}

func TestAdminService_CompositeModelsListCandidatesIncludeConcreteAccountMappings(t *testing.T) {
	accountRepo := &accountRepoStubForCompositeModelsList{
		accounts: []Account{
			{
				ID:       1,
				Platform: PlatformOpenAI,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-custom": "gpt-5"},
				},
			},
			{
				ID:       2,
				Platform: PlatformGemini,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gemini-custom": "gemini-2.5-flash"},
				},
			},
			{
				ID:       3,
				Platform: PlatformKimi,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"kimi-custom": "kimi-k2"},
				},
			},
		},
	}
	groupRepo := &groupRepoStubForAdmin{
		getByIDByID: map[int64]*Group{
			99: {ID: 99, Platform: PlatformComposite},
		},
	}
	svc := &adminServiceImpl{accountRepo: accountRepo, groupRepo: groupRepo}

	candidates, err := svc.GetGroupModelsListCandidates(context.Background(), 99, PlatformComposite)

	require.NoError(t, err)
	require.Contains(t, candidates, "gpt-custom")
	require.Contains(t, candidates, "gemini-custom")
	require.Contains(t, candidates, "kimi-custom")
	require.Contains(t, candidates, "gpt-5.5")
	require.Contains(t, candidates, "gemini-2.5-flash")
	for _, model := range []string{"deepseek-flash", "kimi-k3", "glm-5.3-flashx", "MiniMax-M3.1-Flash-Preview", "step-5-preview", "step-router-v1"} {
		require.Contains(t, candidates, model)
	}
}

func TestAdminService_CNProviderModelsListCandidatesUseSelectedPlatform(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformStepFun} {
		t.Run(platform, func(t *testing.T) {
			defaults := cnmodels.DefaultModelIDs(platform)
			svc := &adminServiceImpl{
				groupRepo: &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{
					99:  {ID: 99, Platform: PlatformAnthropic},
					100: {ID: 100, Platform: platform},
				}},
				accountRepo: &accountRepoStubForCompositeModelsList{accounts: []Account{
					{Platform: platform, Credentials: map[string]any{"model_mapping": map[string]any{
						defaults[0]: defaults[0], "custom-native": defaults[0], "claude-compatible-alias": defaults[0],
					}}},
					{Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"unrelated-provider-model": "claude-opus-4-6"}}},
				}},
			}
			created, err := svc.GetGroupModelsListCandidates(context.Background(), 0, platform)
			require.NoError(t, err)
			require.Equal(t, defaults, created)
			for _, model := range created {
				require.NotContains(t, model, "claude")
			}
			// Editing uses the selected platform even when the stored platform
			// is Anthropic; account mappings from other platforms are excluded.
			edited, err := svc.GetGroupModelsListCandidates(context.Background(), 99, platform)
			require.NoError(t, err)
			require.Equal(t, defaults, edited[:len(defaults)])
			require.Contains(t, edited, "custom-native")
			require.Contains(t, edited, "claude-compatible-alias")
			require.NotContains(t, edited, "unrelated-provider-model")
			require.Len(t, edited, len(defaults)+2)
			inferred, err := svc.GetGroupModelsListCandidates(context.Background(), 100, "")
			require.NoError(t, err)
			require.ElementsMatch(t, edited, inferred)
		})
	}
}

func TestAdminService_ModelsListCandidatesRejectUnknownPlatform(t *testing.T) {
	svc := &adminServiceImpl{}
	_, err := svc.GetGroupModelsListCandidates(context.Background(), 0, "unknown")
	require.Error(t, err)
	require.Empty(t, defaultModelsListCandidateIDs("unknown"))
	defaults, err := svc.GetGroupModelsListCandidates(context.Background(), 0, "")
	require.NoError(t, err)
	require.Contains(t, defaults, "claude-opus-4-6")
}
