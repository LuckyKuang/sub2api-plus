//go:build unit || !integration

package admin

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Source: openai/codex 154e8736a512, login/src/auth/manager/
// workspace_switching.rs selected_workspace_tokens and its persistence test.
// The original login ID/refresh tokens remain unchanged after switching.
func codexWorkspaceImportFixture(t *testing.T, account, user, plan string, fedramp bool) string {
	t.Helper()
	return buildCodexImportTestJWT(t, time.Now().Add(time.Hour), map[string]any{
		"email": "workspace@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": account, "chatgpt_user_id": user,
			"chatgpt_plan_type": plan, "chatgpt_account_is_fedramp": fedramp,
			"poid": "org-" + account,
		},
	})
}

func TestCodexWorkspaceImportUsesSelectedGrantAndKeepsOriginalLogin(t *testing.T) {
	for _, government := range []bool{false, true} {
		t.Run(map[bool]string{false: "commercial", true: "government"}[government], func(t *testing.T) {
			id := codexWorkspaceImportFixture(t, "personal", "user-1", "plus", !government)
			access := codexWorkspaceImportFixture(t, "business", "user-1", "business", government)
			raw := map[string]any{"tokens": map[string]any{
				"id_token": id, "access_token": access,
				"refresh_token": "retained-refresh", "account_id": "business",
			}}
			item, err := normalizeCodexImportEntry(codexImportEntry{Index: 1, Value: raw})
			require.NoError(t, err)
			require.Equal(t, "business", item.AccountID)
			require.Equal(t, "business", item.PlanType)
			require.Equal(t, "user-1", item.UserID)
			require.Equal(t, "org-business", item.Organization)
			require.Equal(t, government, item.Credentials["chatgpt_account_is_fedramp"])
			require.Equal(t, id, item.Credentials["id_token"])
			require.Equal(t, "retained-refresh", item.Credentials["refresh_token"])
			// A shared refresh token must not overwrite the original workspace row.
			svc := newCodexImportMemoryAdminService([]service.Account{{
				ID: 17, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"chatgpt_account_id": "personal", "chatgpt_user_id": "user-1", "refresh_token": "retained-refresh"},
			}})
			h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			result, err := h.importCodexSessions(context.Background(), CodexSessionImportRequest{SkipDefaultGroupBind: boolPtr(true)}, []codexImportEntry{{Index: 1, Value: raw}})
			require.NoError(t, err)
			require.Equal(t, 1, result.Created)
			require.Zero(t, result.Updated)
			require.Zero(t, result.Failed)
			require.Empty(t, svc.updatedAccounts)
			require.Equal(t, "business", svc.createdAccounts[0].Credentials["chatgpt_account_id"])
		})
	}
}

func TestCodexWorkspaceImportRejectsMismatchedGrantWithoutWrites(t *testing.T) {
	for _, tc := range []struct{ name, account, user, selected string }{
		{"wrong account", "other", "user-1", "business"},
		{"wrong user", "business", "user-2", "business"},
		{"missing user", "business", "", "business"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]any{"tokens": map[string]any{
				"id_token":      codexWorkspaceImportFixture(t, "personal", "user-1", "plus", false),
				"access_token":  codexWorkspaceImportFixture(t, tc.account, tc.user, "business", true),
				"refresh_token": "retained-refresh", "account_id": tc.selected,
			}}
			svc := newCodexImportMemoryAdminService(nil)
			h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			result, err := h.importCodexSessions(context.Background(), CodexSessionImportRequest{}, []codexImportEntry{{Index: 1, Value: raw}})
			require.NoError(t, err)
			require.Equal(t, 1, result.Failed)
			require.Empty(t, svc.createdAccounts)
			require.Empty(t, svc.updatedAccounts)
		})
	}
}

func TestCodexWorkspaceImportWithoutSwitchKeepsLoginMetadata(t *testing.T) {
	id := codexWorkspaceImportFixture(t, "personal", "user-1", "plus", false)
	access := codexWorkspaceImportFixture(t, "business", "user-1", "business", true)
	for _, selected := range []string{"", "personal"} {
		item, err := normalizeCodexImportEntry(codexImportEntry{Index: 1, Value: map[string]any{"tokens": map[string]any{
			"id_token": id, "access_token": access, "account_id": selected,
		}}})
		require.NoError(t, err)
		require.Equal(t, "personal", item.AccountID)
		require.Equal(t, "plus", item.PlanType)
		require.Equal(t, false, item.Credentials["chatgpt_account_is_fedramp"])
	}
}
