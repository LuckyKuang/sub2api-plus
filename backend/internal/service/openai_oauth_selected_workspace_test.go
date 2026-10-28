//go:build unit || !integration

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type selectedWorkspaceRefreshClient struct {
	openaiOAuthClientRefreshStub
	response *openai.TokenResponse
}

func (c *selectedWorkspaceRefreshClient) RefreshTokenWithClientIDAndIdentity(context.Context, string, string, string, string, string, string) (*openai.TokenResponse, error) {
	return c.response, nil
}

func selectedWorkspaceJWT(body string) string {
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(body)) + ".signature"
}

func TestOpenAIRefreshSelectedWorkspaceOwnership(t *testing.T) {
	for _, tc := range []struct {
		name      string
		access    string
		id        string
		wantError bool
	}{
		{"selected workspace with retained home ID token", `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"user","chatgpt_plan_type":"business","chatgpt_account_is_fedramp":false}}`, `{"https://api.openai.com/auth":{"chatgpt_account_id":"home","chatgpt_user_id":"user","chatgpt_plan_type":"free"}}`, false},
		{"selected workspace with fresh ID token", `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"user","chatgpt_plan_type":"business","chatgpt_account_is_fedramp":false}}`, `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"user","chatgpt_plan_type":"business"}}`, false},
		{"other access workspace", `{"https://api.openai.com/auth":{"chatgpt_account_id":"other","chatgpt_user_id":"user"}}`, `{}`, true},
		{"other access principal", `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"other"}}`, `{}`, true},
		{"other ID principal", `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"user"}}`, `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"other"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 77, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "old", "refresh_token": "refresh", "chatgpt_account_id": "selected", "chatgpt_user_id": "user", "plan_type": "old-plan", "chatgpt_account_is_fedramp": true,
			}}
			client := &selectedWorkspaceRefreshClient{response: &openai.TokenResponse{AccessToken: selectedWorkspaceJWT(tc.access), IDToken: selectedWorkspaceJWT(tc.id), RefreshToken: "rotated", ExpiresIn: 3600}}
			svc := NewOpenAIOAuthService(nil, client)
			t.Cleanup(svc.Stop)
			svc.SetPrivacyClientFactory(nil)
			if tc.wantError {
				svc.SetPrivacyClientFactory(func(string) (*req.Client, error) {
					t.Error("ownership mismatch must fail before enrichment network side effects")
					return nil, errors.New("forbidden enrichment")
				})
			}
			info, err := svc.RefreshAccountToken(context.Background(), account)
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, info)
			} else {
				require.NoError(t, err)
				creds := svc.BuildAccountCredentials(info)
				require.Equal(t, "selected", creds["chatgpt_account_id"])
				require.Equal(t, "user", creds["chatgpt_user_id"])
				require.Equal(t, "business", creds["plan_type"])
				require.Equal(t, false, creds["chatgpt_account_is_fedramp"])
				require.Equal(t, "rotated", creds["refresh_token"])
			}
			require.Equal(t, "old", account.Credentials["access_token"])
			require.Equal(t, "refresh", account.Credentials["refresh_token"])
			require.Equal(t, true, account.Credentials["chatgpt_account_is_fedramp"])
		})
	}
}

func TestOpenAIRefreshSelectedWorkspaceMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, access, id, wantPlan, wantOrg string
		wantFedRAMP                         any
	}{
		{"unknown access retains selected metadata", `{}`, `{"https://api.openai.com/auth":{"chatgpt_account_id":"home","user_id":"user","chatgpt_plan_type":"free","chatgpt_account_is_fedramp":true,"organizations":[{"id":"home-org"}]}}`, "selected-plan", "selected-org", nil},
		{"fallback principal selects access organization", `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","user_id":"user","chatgpt_plan_type":"business","poid":"new-org","chatgpt_account_is_fedramp":false}}`, `{"https://api.openai.com/auth":{"chatgpt_account_id":"home","user_id":"user","organizations":[{"id":"home-org"}]}}`, "business", "new-org", false},
		{"selected ID clears flag without access claims", `{}`, `{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","user_id":"user","chatgpt_plan_type":"pro","chatgpt_account_is_fedramp":false,"organizations":[{"id":"selected-new-org"}]}}`, "pro", "selected-new-org", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 77, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "old", "refresh_token": "refresh", "chatgpt_account_id": "selected", "id_token": selectedWorkspaceJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"home","user_id":"user"}}`), "plan_type": "selected-plan", "organization_id": "selected-org", "chatgpt_account_is_fedramp": true,
			}}
			client := &selectedWorkspaceRefreshClient{response: &openai.TokenResponse{AccessToken: selectedWorkspaceJWT(tc.access), IDToken: selectedWorkspaceJWT(tc.id), ExpiresIn: 3600}}
			svc := NewOpenAIOAuthService(nil, client)
			t.Cleanup(svc.Stop)
			svc.SetPrivacyClientFactory(nil)
			info, err := svc.RefreshAccountToken(context.Background(), account)
			require.NoError(t, err)
			creds := svc.BuildAccountCredentials(info)
			require.Equal(t, "selected", creds["chatgpt_account_id"])
			require.Equal(t, "user", creds["chatgpt_user_id"])
			require.Equal(t, tc.wantPlan, creds["plan_type"])
			require.Equal(t, tc.wantOrg, creds["organization_id"])
			if tc.wantFedRAMP == nil {
				require.NotContains(t, creds, "chatgpt_account_is_fedramp")
			} else {
				require.Equal(t, tc.wantFedRAMP, creds["chatgpt_account_is_fedramp"])
			}
		})
	}
}

func TestOpenAIRefreshUnknownOriginalUserRequiresCoherentResponse(t *testing.T) {
	// Official selected_workspace_tokens requires the access-token principal to
	// match the retained/new ID-token principal before selecting its metadata.
	for _, accessUser := range []string{"id-user", "different-user"} {
		t.Run(accessUser, func(t *testing.T) {
			account := &Account{ID: 77, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "old", "refresh_token": "refresh", "chatgpt_account_id": "selected",
			}}
			client := &selectedWorkspaceRefreshClient{response: &openai.TokenResponse{
				AccessToken: selectedWorkspaceJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"` + accessUser + `"}}`),
				IDToken:     selectedWorkspaceJWT(`{"https://api.openai.com/auth":{"chatgpt_account_id":"selected","chatgpt_user_id":"id-user","chatgpt_plan_type":"business"}}`),
				ExpiresIn:   3600,
			}}
			svc := NewOpenAIOAuthService(nil, client)
			t.Cleanup(svc.Stop)
			svc.SetPrivacyClientFactory(nil)
			if accessUser != "id-user" {
				svc.SetPrivacyClientFactory(func(string) (*req.Client, error) {
					t.Error("conflicting response must fail before enrichment")
					return nil, errors.New("forbidden enrichment")
				})
			}
			info, err := svc.RefreshAccountToken(context.Background(), account)
			if accessUser != "id-user" {
				require.Error(t, err)
				require.Nil(t, info)
			} else {
				require.NoError(t, err)
				creds := svc.BuildAccountCredentials(info)
				require.Equal(t, "selected", creds["chatgpt_account_id"])
				require.Equal(t, "id-user", creds["chatgpt_user_id"])
				require.Equal(t, "business", creds["plan_type"])
			}
			require.Equal(t, "old", account.Credentials["access_token"])
			require.Equal(t, "refresh", account.Credentials["refresh_token"])
		})
	}
}
