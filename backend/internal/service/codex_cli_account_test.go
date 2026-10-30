//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func codexCLIAccountFixture(id int64) *Account {
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{
		"access_token": "private-access-original", "refresh_token": "private-refresh-original", "id_token": "private-id-original", "chatgpt_account_id": "workspace-one", "chatgpt_user_id": "user-one",
	}}
}

func codexCLIClaimFixture(workspace, user string) string {
	// Independent native OAuth fixture: these claims prove only consistency,
	// never issuer authenticity. Expiry is the fixed 2100-01-01 boundary.
	payload, _ := json.Marshal(map[string]any{"sub": user, "exp": int64(4102444800), "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": workspace, "chatgpt_user_id": user}})
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".fixture"
}

// User contract: new accounts start Full Access/never/file; edited sandbox
// values take effect. Official exec is noninteractive and always uses never.
func TestCodexCLIAccountConfigurationDefaultsAndOverrides(t *testing.T) {
	account := codexCLIAccountFixture(1)
	view, err := CodexCLIAccountViewFromAccount(account)
	require.NoError(t, err)
	require.Equal(t, "sandbox_mode = \"danger-full-access\"\napproval_policy = \"never\"\ncli_auth_credentials_store = \"file\"\n", view.ConfigTOML)
	require.False(t, view.Enabled)
	require.True(t, view.Availability.Available)
	require.Equal(t, "exec_task_only", view.Availability.Code)

	text := "sandbox_mode = \"read-only\"\napproval_policy = \"never\"\n"
	updated, authChanged, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{ExpectedOwner: view.ExpectedOwner, ConfigTOML: &text})
	require.NoError(t, err)
	require.False(t, authChanged)
	require.Equal(t, "cli_auth_credentials_store = \"file\"\n"+text, updated[CodexCLIConfigCredentialKey].(map[string]any)["config_toml"])
	require.NotContains(t, account.Credentials, CodexCLIConfigCredentialKey, "preparation cannot mutate a live account")
}

func TestCodexCLITOMLRejectsAlternateAuthAndIdentitySources(t *testing.T) {
	for _, text := range []string{
		"auth_mode = \"chatgpt\"", "access_token = \"private-token-source\"", "experimental_bearer_token = \"private-token-source\"",
		"model_provider = \"other\"", "[model_providers.other]\nbase_url = \"https://untrusted.invalid\"", "originator = \"other-client\"",
		"[mcp_servers.other]\nuser_agent = \"other-client\"", "[mcp_servers.other.env]\nAPI_SECRET=\"private-token-source\"",
		"cli_auth_credentials_store = \"keyring\"", "sandbox_mode = \"future-unsafe\"", "approval_policy = false", "config = [\"private-token-source\"",
		"[mcp_servers.other]\ncli_auth_credentials_store = \"keyring\"", "[mcp_servers.other]\nsandbox_mode = \"unsafe\"", "[mcp_servers.other]\napproval_policy = false",
	} {
		t.Run(text, func(t *testing.T) {
			config, err := NormalizeCodexCLIConfigTOML(text)
			require.Error(t, err)
			require.Empty(t, config)
			require.NotContains(t, err.Error(), "private-token-source", "validation errors cannot echo secret source lines")
		})
	}
}

// rust-v0.165.0 exec/src/lib.rs build_exec_config fixes approvals to Never.
// Reject apparent interactive policies instead of accepting ineffective edits.
func TestCodexCLIAccountRejectsUnsupportedApprovalAndDiscoveryBeforeWrites(t *testing.T) {
	for _, text := range []string{
		"approval_policy = \"untrusted\"\n",
		"approval_policy = \"on-failure\"\n",
		"approval_policy = \"on-request\"\n",
		"project_root_markers = [\".git\"]\n",
	} {
		t.Run(text, func(t *testing.T) {
			account := codexCLIAccountFixture(103)
			before, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			credentials, changed, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text})
			require.Error(t, err)
			require.Nil(t, credentials)
			require.False(t, changed)
			after, marshalErr := json.Marshal(account.Credentials)
			require.NoError(t, marshalErr)
			require.Equal(t, before, after)
		})
	}
}

// The reviewed CLI may drop a requested paid tier using model metadata and
// JSONL has no actual-tier evidence. Only explicit standard config is accepted.
func TestCodexCLIAccountRejectsUnverifiableServiceTiersBeforeWrites(t *testing.T) {
	for _, text := range []string{
		`service_tier = "fast"`, `service_tier = "priority"`, `service_tier = "flex"`,
		`service_tier = "ultrafast"`, `service_tier = "auto"`, `service_tier = true`,
		`service_tier = "private-unknown-tier"`,
	} {
		t.Run(text, func(t *testing.T) {
			account := codexCLIAccountFixture(103)
			before, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			credentials, changed, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text})
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private-unknown-tier")
			require.Nil(t, credentials)
			require.False(t, changed)
			after, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
	config, err := NormalizeCodexCLIConfigTOML("service_tier = \"default\"\n")
	require.NoError(t, err)
	require.Contains(t, config, "service_tier = \"default\"")
}

func TestCodexCLIAccountRejectsProfilesBeforeAnyCredentialOrPersistenceWrite(t *testing.T) {
	for _, text := range []string{
		"profile = \"private-profile-name\"\n",
		"profile = \"\"\n",
		"[profiles]\n",
		"[profiles.private-profile-name]\nmodel_reasoning_effort = \"high\"\n",
	} {
		t.Run(text, func(t *testing.T) {
			normalized, err := NormalizeCodexCLIConfigTOML(text)
			require.Error(t, err)
			require.Empty(t, normalized)
			require.Contains(t, err.Error(), "write settings directly in the account config.toml")
			require.NotContains(t, err.Error(), "private-profile-name")

			account := codexCLIAccountFixture(1)
			account.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": false, "config_toml": DefaultCodexCLIConfigTOML}
			before, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			credentials, authChanged, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{
				ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text,
				Auth: &CodexCLIAuthPatch{Tokens: map[string]json.RawMessage{"refresh_token": json.RawMessage("null")}},
			})
			require.Error(t, err)
			require.Nil(t, credentials)
			require.False(t, authChanged)
			after, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "invalid profile settings cannot mutate the live account or clear its grant")

			repo := &updateAccountCredsRepoStub{account: account}
			svc := &adminServiceImpl{accountRepo: repo}
			_, err = svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: map[string]any{
				CodexCLIConfigCredentialKey: map[string]any{"enabled": false, "config_toml": text},
			}})
			require.Error(t, err)
			require.Zero(t, repo.updateCalls, "ordinary account writes must reject profiles before persistence")
		})
	}
}

func TestCodexCLIConfigRejectsNestedSecretsOnEveryNormalizedWrite(t *testing.T) {
	for _, input := range []map[string]any{
		{"auth_json": "private-token-source"}, {"enabled": "true"}, {"config_toml": 10}, {"tokens": map[string]any{"refresh_token": "private-token-source"}},
	} {
		credentials := map[string]any{CodexCLIConfigCredentialKey: input}
		err := NormalizeCodexCLIAccountCredentials(PlatformOpenAI, AccountTypeOAuth, credentials)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-token-source")
	}
	for _, account := range []*Account{
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, {Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": "agentIdentity"}},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"auth_mode": "personalAccessToken"}},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"openai_auth_mode": " PERSONAL_ACCESS_TOKEN "}},
	} {
		credentials := MergeCredentials(account.Credentials, map[string]any{CodexCLIConfigCredentialKey: map[string]any{"config_toml": ""}})
		require.Error(t, NormalizeCodexCLIAccountCredentials(account.Platform, account.Type, credentials))
	}
}

func TestCodexCLIAccountRejectsCredentialShadows(t *testing.T) {
	parent := int64(7)
	shadow := codexCLIAccountFixture(8)
	shadow.ParentAccountID = &parent
	require.Error(t, ValidateCodexCLIAccount(shadow))
	_, err := CodexCLIAccountViewFromAccount(shadow)
	require.Error(t, err)
	_, err = BuildCodexCLIAuthJSON(shadow)
	require.Error(t, err, "a shadow must never materialize independently owned credentials")
}

// The CLI editor never reads secrets; omission keeps each canonical credential,
// null clears it, and replacement updates only the requested account's grant.
func TestCodexCLIAuthPatchPreserveClearAndReplace(t *testing.T) {
	account := codexCLIAccountFixture(1)
	text := ""
	for _, scenario := range []string{"omit", "clear-refresh", "replace-access"} {
		t.Run(scenario, func(t *testing.T) {
			input := CodexCLIAccountUpdate{ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text}
			switch scenario {
			case "clear-refresh":
				input.Auth = &CodexCLIAuthPatch{Tokens: map[string]json.RawMessage{"refresh_token": json.RawMessage("null")}}
			case "replace-access":
				raw, err := json.Marshal(codexCLIClaimFixture("workspace-one", "user-one"))
				require.NoError(t, err)
				input.Auth = &CodexCLIAuthPatch{Tokens: map[string]json.RawMessage{"access_token": raw}}
			}
			credentials, changed, err := PrepareCodexCLIAccountUpdate(account, input)
			require.NoError(t, err)
			require.Equal(t, scenario != "omit", changed)
			require.Equal(t, "private-id-original", credentials["id_token"])
			if scenario == "clear-refresh" {
				require.Equal(t, "", credentials["refresh_token"])
			} else {
				require.Equal(t, "private-refresh-original", credentials["refresh_token"])
			}
			if scenario == "replace-access" {
				require.Equal(t, "2100-01-01T00:00:00Z", credentials["expires_at"])
			}
			require.Equal(t, "private-access-original", account.GetCredential("access_token"))
			require.Equal(t, "private-refresh-original", account.GetCredential("refresh_token"))
		})
	}
}

func TestCodexCLIAuthRejectsOwnerMixingAndMaskedValues(t *testing.T) {
	account := codexCLIAccountFixture(1)
	text := ""
	for _, patch := range []string{
		`{"refresh_token":"***"}`, `{"refresh_token":""}`, `{"refresh_token":42}`, `{"account_id":"workspace-two"}`,
		`{"refresh_token":"[REDACTED]"}`,
		`{"unknown":"private-secret"}`, `{"access_token":"opaque-token"}`,
	} {
		var tokens map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(patch), &tokens))
		_, _, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text, Auth: &CodexCLIAuthPatch{Tokens: tokens}})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-secret")
	}
	for _, token := range []string{codexCLIClaimFixture("workspace-two", "user-one"), codexCLIClaimFixture("workspace-one", "user-two")} {
		raw, err := json.Marshal(token)
		require.NoError(t, err)
		_, _, err = PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text, Auth: &CodexCLIAuthPatch{Tokens: map[string]json.RawMessage{"access_token": raw}}})
		require.Error(t, err)
	}
}

func TestCodexCLIAccountOwnerStableForOrdinaryEditorAndRejectsChangedGrant(t *testing.T) {
	account := codexCLIAccountFixture(1)
	owner := CodexCLIAccountOwner(account)
	account.Name, account.Priority = "renamed", 40
	account.Credentials["user_agent"] = "trusted-admin-identity"
	account.Credentials["model_mapping"] = map[string]any{"a": "b"}
	account.Extra = map[string]any{"passive_usage": 15}
	require.Equal(t, owner, CodexCLIAccountOwner(account))
	account.Credentials["refresh_token"] = "new-login-refresh"
	text := ""
	_, _, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{ExpectedOwner: owner, ConfigTOML: &text})
	require.Error(t, err)
	require.Contains(t, err.Error(), "changed")
	require.Equal(t, "new-login-refresh", account.GetCredential("refresh_token"))
}

// The account editor must retain the existing OAuth principal even when an
// imported account has only an access token. Members of a shared workspace
// cannot replace one another's grant through a same-workspace token patch.
func TestCodexCLIAuthPatchRetainsAccessTokenOwnerWithoutIDTokenOrUserMetadata(t *testing.T) {
	for _, key := range []string{"access_token", "id_token"} {
		for _, user := range []string{"user-one", "user-two"} {
			t.Run(key+"/"+user, func(t *testing.T) {
				account := codexCLIAccountFixture(201)
				delete(account.Credentials, "chatgpt_user_id")
				delete(account.Credentials, "id_token")
				account.Credentials["access_token"] = codexCLIClaimFixture("workspace-one", "user-one")
				original, err := json.Marshal(account.Credentials)
				require.NoError(t, err)
				raw, err := json.Marshal(codexCLIClaimFixture("workspace-one", user))
				require.NoError(t, err)
				text := ""
				credentials, _, err := PrepareCodexCLIAccountUpdate(account, CodexCLIAccountUpdate{
					ExpectedOwner: CodexCLIAccountOwner(account), ConfigTOML: &text,
					Auth: &CodexCLIAuthPatch{Tokens: map[string]json.RawMessage{key: raw}},
				})
				if user == "user-two" {
					require.Error(t, err)
					require.Nil(t, credentials)
					require.Contains(t, err.Error(), "existing ChatGPT user and workspace")
				} else {
					require.NoError(t, err)
					require.Equal(t, "user-one", credentials["chatgpt_user_id"])
					require.Equal(t, "workspace-one", credentials["chatgpt_account_id"])
				}
				after, marshalErr := json.Marshal(account.Credentials)
				require.NoError(t, marshalErr)
				require.Equal(t, string(original), string(after), "preparing a credential edit cannot mutate the live account")
			})
		}
	}
}

func TestCodexCLIAccountViewAndMaterializationUseOneOwner(t *testing.T) {
	first, second := codexCLIAccountFixture(1), codexCLIAccountFixture(2)
	second.Credentials["access_token"], second.Credentials["chatgpt_account_id"] = "account-two-private-access", "workspace-two"
	for _, account := range []*Account{first, second} {
		view, err := CodexCLIAccountViewFromAccount(account)
		require.NoError(t, err)
		data, err := json.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, string(data), "private-access")
		require.NotContains(t, string(data), "private-refresh")
		require.NotContains(t, string(data), "private-id")
		require.True(t, view.AuthStatus.HasAccessToken)
		materialized, err := BuildCodexCLIAuthJSON(account)
		require.NoError(t, err)
		var auth struct {
			AuthMode string `json:"auth_mode"`
			Access   string `json:"OPENAI_API_KEY"`
		}
		require.NoError(t, json.Unmarshal(materialized, &auth))
		require.Equal(t, "apikey", auth.AuthMode, "execution uses official static credentials; OAuth refresh belongs to the gateway")
		require.Equal(t, account.GetCredential("access_token"), auth.Access)
		require.NotContains(t, string(materialized), "refresh_token")
		require.NotContains(t, string(materialized), "id_token")
		require.NotContains(t, string(materialized), account.GetCredential("refresh_token"))
		require.NotContains(t, string(materialized), account.GetCredential("id_token"))
	}
}

func TestCodexCLIConfigSurvivesOrdinaryAccountUpdateAndRejectsInvalidWrites(t *testing.T) {
	account := codexCLIAccountFixture(1)
	account.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": false, "config_toml": DefaultCodexCLIConfigTOML}
	repo := &updateAccountCredsRepoStub{account: account}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: map[string]any{"model_mapping": map[string]any{"a": "b"}}})
	require.NoError(t, err)
	require.Equal(t, DefaultCodexCLIConfigTOML, updated.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["config_toml"])
	calls := repo.updateCalls
	_, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: map[string]any{CodexCLIConfigCredentialKey: map[string]any{"auth_json": "private-secret"}}})
	require.Error(t, err)
	require.Equal(t, calls, repo.updateCalls, "invalid CLI payload must not reach persistence")
	require.False(t, strings.Contains(err.Error(), "private-secret"))
}

// Runtime installation is an enablement prerequisite, not a prerequisite for
// managing an existing account. This applies identically to all write paths.
func TestCodexCLIEnablementRequiresRuntimeOnlyForNewEnable(t *testing.T) {
	config := func(enabled bool) map[string]any {
		return map[string]any{CodexCLIConfigCredentialKey: map[string]any{"enabled": enabled}}
	}
	for _, scenario := range []struct {
		name           string
		previous, next bool
		runtime        bool
		wantError      bool
	}{
		{"enable-unavailable", false, true, false, true},
		{"enable-available", false, true, true, false},
		{"edit-enabled-unavailable", true, true, false, false},
		{"disable-unavailable", true, false, false, false},
		{"edit-disabled-unavailable", false, false, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			checks := 0
			err := validateCodexCLIEnablement(config(scenario.previous), config(scenario.next), func() bool { checks++; return scenario.runtime })
			if scenario.wantError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "project-pinned")
			} else {
				require.NoError(t, err)
			}
			if scenario.previous || !scenario.next {
				require.Zero(t, checks, "configuration edits and disabling must not depend on runtime availability")
			}
		})
	}
}

type codexCLITypeChangeRepo struct{ *updateAccountCredsRepoStub }

func (*codexCLITypeChangeRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}

func TestCodexCLIConfigCannotRemainOnIncompatibleAccountType(t *testing.T) {
	account := codexCLIAccountFixture(1)
	account.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": false, "config_toml": DefaultCodexCLIConfigTOML}
	repo := &codexCLITypeChangeRepo{updateAccountCredsRepoStub: &updateAccountCredsRepoStub{account: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Type: AccountTypeAPIKey})
	require.Error(t, err)
	require.Zero(t, repo.updateCalls, "changing only the account type cannot bypass CLI eligibility")
}

func TestCodexCLIManagementAuditNeverRecordsTOMLOrAuthSecrets(t *testing.T) {
	redacted := RedactAuditBody([]byte(`{"config_toml":"api_key = \"private-rejected-toml\"","auth_json":"private-rejected-auth","auth":{"tokens":{"refresh_token":"private-refresh"}},"expected_owner":"opaque-owner"}`), "application/json")
	require.NotContains(t, redacted, "private-")
	require.Contains(t, redacted, "opaque-owner", "owner evidence remains available for management accountability")
}

type codexCLIBulkAccountRepo struct {
	mockAccountRepoForGemini
	written AccountBulkUpdate
	calls   int
}

func (r *codexCLIBulkAccountRepo) BulkUpdate(_ context.Context, _ []int64, updates AccountBulkUpdate) (int64, error) {
	r.calls++
	r.written = updates
	return 2, nil
}

// A bulk CLI configuration edit is a shared JSONB delta. Validation must not
// expand that delta with either account's grant and copy it to other owners.
func TestCodexCLIBulkConfigurationCannotCopyOneAccountsCredentialsToAnother(t *testing.T) {
	first, second := codexCLIAccountFixture(11), codexCLIAccountFixture(22)
	first.Credentials["access_token"] = "first-owner-private-access"
	second.Credentials["access_token"] = "second-owner-private-access"
	repo := &codexCLIBulkAccountRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{11: first, 22: second}}}
	svc := &adminServiceImpl{accountRepo: repo}
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{11, 22}, Credentials: map[string]any{
		CodexCLIConfigCredentialKey: map[string]any{"enabled": false, "config_toml": "sandbox_mode = \"read-only\"\n"},
	}})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 1, repo.calls)
	require.Len(t, repo.written.Credentials, 1)
	require.Contains(t, repo.written.Credentials, CodexCLIConfigCredentialKey)
	require.NotContains(t, repo.written.Credentials, "access_token")
	require.Equal(t, "first-owner-private-access", first.GetCredential("access_token"))
	require.Equal(t, "second-owner-private-access", second.GetCredential("access_token"))
}
