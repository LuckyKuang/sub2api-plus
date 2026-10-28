//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pluginDirRepoStub embeds AccountRepository so it satisfies the full interface;
// only the directory's listing and direct account lookup are implemented.
// Any other call panics, which keeps the test honest about the surface it touches.
type pluginDirRepoStub struct {
	AccountRepository
	byPlatform map[string][]Account
	account    *Account
}

func (r *pluginDirRepoStub) ListByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.byPlatform[platform], nil
}

func (r *pluginDirRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func TestListPluginAccounts_ScopeAndSchedulable(t *testing.T) {
	future := time.Now().Add(time.Hour)
	parentID := int64(1)
	openai := []Account{
		// schedulable active oauth. Credentials hold secrets (stripped); Extra is
		// released.
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			Name:        "primary",
			Credentials: map[string]any{"access_token": "SECRET-TOKEN", "refresh_token": "SECRET-REFRESH"},
			Extra:       map[string]any{"existing_key": "ek-value", "openai_compact_mode": "auto"}},
		// active but temp-unschedulable (paused) — status stays active, must still be
		// returned, but not schedulable
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			TempUnschedulableUntil: &future, TempUnschedulableReason: "429 from upstream"},
		// active but rate-limited (429) — status stays active, returned, not schedulable
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			RateLimitResetAt: &future},
		// shadow oauth — excluded entirely
		{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			ParentAccountID: &parentID},
		// apikey type — out of (openai, oauth) scope, excluded
		{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true},
	}
	svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{byPlatform: map[string][]Account{PlatformOpenAI: openai}}}
	scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})

	infos, err := svc.ListPluginAccounts(context.Background(), scope, "", "")
	require.NoError(t, err)

	got := map[int64]PluginAccountInfo{}
	for _, info := range infos {
		got[info.ID] = info
	}
	// 1,2,3 in scope; 4 shadow and 5 apikey excluded.
	require.Len(t, infos, 3)
	assert.Contains(t, got, int64(1))
	assert.Contains(t, got, int64(2))
	assert.Contains(t, got, int64(3))
	assert.NotContains(t, got, int64(4), "shadow account must be excluded")
	assert.NotContains(t, got, int64(5), "out-of-scope account type must be excluded")

	// Host-authoritative schedulable decision.
	assert.True(t, got[1].Schedulable, "active oauth is schedulable")
	assert.False(t, got[2].Schedulable, "temp-unschedulable account is not schedulable")
	assert.False(t, got[3].Schedulable, "rate-limited account is not schedulable")

	// Metadata carries readable info (incl. Extra) but NEVER the raw Credentials blob.
	meta := string(got[1].MetadataJSON)
	assert.NotContains(t, meta, "SECRET-TOKEN", "credentials must never appear in metadata")
	assert.NotContains(t, meta, "SECRET-REFRESH", "credentials must never appear in metadata")
	assert.Contains(t, meta, "ek-value", "Extra is intentionally released")
	assert.Contains(t, meta, "openai_compact_mode", "Extra is intentionally released")
	assert.Contains(t, meta, "primary", "readable name must be present in metadata")
	assert.Contains(t, string(got[2].MetadataJSON), "429 from upstream", "pause reason must be readable in metadata")
}

// TestAccountReadableSnapshot_DenylistTripwire fails whenever a new EXPORTED field
// is added to Account without being classified as either safe-to-expose or
// stripped by accountReadableSnapshotJSON. Because the snapshot is a denylist, a
// newly added secret-bearing field would otherwise silently ship to plugins. When
// this test fails: add the field to `stripped` (and zero it in
// accountReadableSnapshotJSON) if it can hold secrets/heavy data, otherwise add it
// to `safeToExpose`.
func TestAccountReadableSnapshot_DenylistTripwire(t *testing.T) {
	// Fields the snapshot intentionally strips. Credentials = long-lived secret
	// (refresh_token) not handed out by ResolveOutboundIdentity. Groups/AccountGroups
	// = relational graphs with back-references that would cycle under encoding/json.
	stripped := map[string]struct{}{
		"Credentials": {}, "Groups": {}, "AccountGroups": {},
		"CredentialGeneration": {}, "CNQuotaGeneration": {},
	}
	// Fields intentionally exposed as readable metadata (incl. Extra and Proxy —
	// the proxy password is already handed out via ResolveOutboundIdentity's URL).
	safeToExpose := map[string]struct{}{
		"ID": {}, "Name": {}, "Notes": {}, "Platform": {}, "Type": {}, "Extra": {},
		"Proxy": {}, "ProxyID": {}, "ProxyFallbackOriginID": {}, "ProxyFallbackOriginName": {},
		"Concurrency": {}, "Priority": {}, "RateMultiplier": {}, "LoadFactor": {},
		"Status": {}, "ErrorMessage": {}, "LastUsedAt": {}, "ExpiresAt": {},
		"AutoPauseOnExpired": {}, "CreatedAt": {}, "UpdatedAt": {}, "Schedulable": {},
		"RateLimitedAt": {}, "RateLimitResetAt": {}, "OverloadUntil": {},
		"TempUnschedulableUntil": {}, "TempUnschedulableReason": {},
		"SessionWindowStart": {}, "SessionWindowEnd": {}, "SessionWindowStatus": {},
		"ParentAccountID": {}, "QuotaDimension": {}, "GroupIDs": {},
	}
	tp := reflect.TypeOf(Account{})
	for i := 0; i < tp.NumField(); i++ {
		f := tp.Field(i)
		if f.PkgPath != "" {
			continue // unexported: never marshaled by encoding/json
		}
		_, isStripped := stripped[f.Name]
		_, isSafe := safeToExpose[f.Name]
		if !isStripped && !isSafe {
			t.Fatalf("Account.%s is a new exported field not classified for the plugin snapshot: "+
				"add it to accountReadableSnapshotJSON's denylist (if it holds secrets or is a "+
				"cyclic/heavy relation) or to safeToExpose (if it is non-secret readable metadata)", f.Name)
		}
	}

	// The raw Credentials blob must never serialize; Extra and the proxy ARE released.
	acct := &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"access_token": "AT", "refresh_token": "LEAK-REFRESH"},
		Extra:       map[string]any{"opaque": "extra-released"},
		Proxy:       &Proxy{Host: "host", Port: 1, Username: "user", Password: "pw-released"},
	}
	snap := accountReadableSnapshotJSON(acct)
	require.NotNil(t, snap)
	var m map[string]any
	require.NoError(t, json.Unmarshal(snap, &m))
	assert.NotContains(t, string(snap), "LEAK-REFRESH", "raw Credentials must never appear in metadata")
	assert.Contains(t, string(snap), "extra-released", "Extra is intentionally released")
	assert.Contains(t, string(snap), "pw-released", "proxy is intentionally released (already exposed via 打票)")

	// Cycle safety: a populated Groups/AccountGroups back-reference cycle must NOT
	// crash json.Marshal (encoding/json does not detect cycles). Stripping them
	// guarantees the snapshot still returns valid JSON instead of stack-overflowing.
	g := &Group{ID: 7, Name: "g7"}
	ag := AccountGroup{GroupID: 7, Group: g, Account: acct}
	g.AccountGroups = []AccountGroup{ag} // g -> ag -> g  (and ag -> acct -> ...)
	acct.Groups = []*Group{g}
	acct.AccountGroups = []AccountGroup{ag}
	cyc := accountReadableSnapshotJSON(acct)
	require.NotNil(t, cyc, "snapshot must survive a cyclic Groups/AccountGroups graph")
	require.NoError(t, json.Unmarshal(cyc, &m))

	// Shallow-copy safety: the source account must be untouched.
	assert.NotNil(t, acct.Credentials, "snapshot must not mutate the source account")
	assert.NotNil(t, acct.Proxy, "snapshot must not mutate the source account")
	assert.Len(t, acct.Groups, 1, "snapshot must not mutate the source account's Groups")
}

func TestListPluginAccounts_ExcludesNonActiveDefenseInDepth(t *testing.T) {
	// Even if a repo were to return a non-active account, the directory must not
	// expose it (defense-in-depth beyond ListByPlatform's DB filter).
	svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{byPlatform: map[string][]Account{
		PlatformOpenAI: {
			{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true},
			{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusDisabled},
			{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError},
		},
	}}}
	scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
	infos, err := svc.ListPluginAccounts(context.Background(), scope, "", "")
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, int64(1), infos[0].ID)
}

func TestListPluginAccounts_EmptyScopeReturnsNothing(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{byPlatform: map[string][]Account{
		PlatformOpenAI: {{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}},
	}}}
	infos, err := svc.ListPluginAccounts(context.Background(), PluginAccountScope{}, "", "")
	require.NoError(t, err)
	assert.Empty(t, infos, "an empty scope must never enumerate accounts")
}

type pluginDirTokenCache struct {
	OpenAITokenCache
	acquire func(context.Context) string
}

func (c *pluginDirTokenCache) GetAccessToken(ctx context.Context, _ string) (string, error) {
	return c.acquire(ctx), nil
}

type pluginDirRefreshExecutor struct {
	credentials map[string]any
	refresh     func()
	calls       int
}

func (e *pluginDirRefreshExecutor) CanRefresh(account *Account) bool {
	return account.IsOpenAIOAuth()
}

func (e *pluginDirRefreshExecutor) NeedsRefresh(*Account, time.Duration) bool { return true }

func (e *pluginDirRefreshExecutor) Refresh(context.Context, *Account) (map[string]any, error) {
	e.calls++
	if e.refresh != nil {
		e.refresh()
	}
	return e.credentials, nil
}

func (e *pluginDirRefreshExecutor) CacheKey(account *Account) string {
	return OpenAITokenCacheKey(account)
}

// The plugin directory exports bearer credentials only. AGENTS.md requires
// credential operations to avoid unrelated provider writes: a signed account
// with no task must not register a task merely to discover it has no bearer.
func TestResolvePluginOutboundIdentitySkipsAgentIdentityBeforeRegistration(t *testing.T) {
	key, privateKey := newTestAgentIdentityKey(t)
	var registrations atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		registrations.Add(1)
		_, _ = w.Write([]byte(`{"task_id":"unexpected-task"}`))
	}))
	defer server.Close()
	oldBase := openAIAgentIdentityAuthAPIBaseURL
	openAIAgentIdentityAuthAPIBaseURL = server.URL
	t.Cleanup(func() { openAIAgentIdentityAuthAPIBaseURL = oldBase })
	account := &Account{ID: 9401, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID,
			"agent_private_key": privateKey,
		},
	}
	repo := &agentIdentityCredentialsRepo{account: account}
	svc := &OpenAIGatewayService{accountRepo: repo}
	scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
	identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), scope, account.ID)
	require.NoError(t, err)
	require.Nil(t, identity)
	require.Zero(t, registrations.Load(), "unsupported signing credentials must cause no registration request")
	require.Nil(t, repo.credentials, "no task or credential update may be persisted")
	require.Empty(t, account.GetCredential("task_id"))
}

// The directory has the same immutable Codex source chain as forwarding:
// valid account > valid global > exact compiled fingerprint. Its exported
// headers are the plugin RPC's boundary, before any plugin transport executes.
func TestResolvePluginOutboundIdentityCodexSourcePriority(t *testing.T) {
	const accountUA = "codex_vscode/0.180.0 (Windows 11; aarch64) WindowsTerminal"
	const globalUA = "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode"
	for _, tc := range []struct {
		name, accountUA, globalUA, version, wantUA, wantOriginator, wantVersion string
	}{
		{"account", accountUA, globalUA, "0.200.1", "codex_vscode/0.200.1 (Windows 11; aarch64) WindowsTerminal", "codex_vscode", "0.200.1"},
		{"empty-account", "", globalUA, "0.200.1", "codex_vscode/0.200.1 (Ubuntu 24.04; aarch64) vscode", "codex_vscode", "0.200.1"},
		{"invalid-account", "curl/8.0", globalUA, "0.200.1", "codex_vscode/0.200.1 (Ubuntu 24.04; aarch64) vscode", "codex_vscode", "0.200.1"},
		{"invalid-global", "curl/8.0", "curl/8.1", "0.200.1", "codex_cli_rs/0.200.1 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs", "0.200.1"},
		{"compiled", "", "", "", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs", "0.165.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 9402, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "directory-token", "user_agent": tc.accountUA},
			}
			settings := &SettingService{settingRepo: &agentIdentitySettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexUserAgent: tc.globalUA, SettingKeyOpenAICodexClientVersion: tc.version,
			}}}
			svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{account: account}, settingService: settings}
			scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), scope, account.ID)
			require.NoError(t, err)
			require.NotNil(t, identity)
			require.Equal(t, "directory-token", identity.Token)
			require.Equal(t, tc.wantUA, identity.Headers.Get("User-Agent"))
			require.Equal(t, tc.wantOriginator, identity.Headers.Get("Originator"))
			require.Equal(t, tc.wantVersion, identity.Headers.Get("Version"))
		})
	}
}

func TestResolvePluginOutboundIdentityUsesRefreshedOwner(t *testing.T) {
	for _, candidate := range []string{"codex_vscode/0.180.0 (Windows 11; aarch64) WindowsTerminal", "", "curl/8.0"} {
		t.Run(candidate, func(t *testing.T) {
			proxy := &Proxy{ID: 9403, Protocol: "http", Host: "refreshed-proxy.invalid", Port: 8080, Status: StatusActive}
			account := &Account{ID: 9403, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
				ProxyID: &proxy.ID, Proxy: proxy,
				Credentials: map[string]any{
					"access_token": "expired-token", "refresh_token": "initial-refresh",
					"expires_at":         time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
					"chatgpt_account_id": "selected-principal",
					"user_agent":         "codex_cli_rs/0.180.0 (Ubuntu 24.04; x86_64) xterm-256color",
				},
			}
			repo := &agentIdentityCredentialsRepo{account: account}
			settingRepo := &agentIdentitySettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexUserAgent:     "codex_vscode/0.180.0 (Ubuntu 24.04; aarch64) vscode",
				SettingKeyOpenAICodexClientVersion: "0.200.1",
			}}
			settings := &SettingService{settingRepo: settingRepo}
			executor := &pluginDirRefreshExecutor{credentials: map[string]any{
				"access_token": "refreshed-token", "refresh_token": "rotated-refresh",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				"user_agent": candidate, "chatgpt_account_id": "refreshed-principal", "chatgpt_account_is_fedramp": true,
			}, refresh: func() {
				settingRepo.setValue(SettingKeyOpenAICodexClientVersion, "0.200.2")
				settings.InvalidateOpenAICodexClientVersionCache()
			}}
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), executor)
			svc := &OpenAIGatewayService{accountRepo: repo, openAITokenProvider: provider, settingService: settings}
			scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), scope, account.ID)
			require.NoError(t, err)
			require.NotNil(t, identity)
			require.Equal(t, 1, executor.calls, "the test must execute the refresh lifecycle")
			require.Equal(t, "refreshed-token", identity.Token)
			require.Equal(t, "refreshed-principal", identity.Headers.Get("Chatgpt-Account-Id"))
			require.Equal(t, "true", identity.Headers.Get("X-Openai-Fedramp"))
			require.Equal(t, "http://refreshed-proxy.invalid:8080", identity.ProxyURL)
			wantUA := "codex_vscode/0.200.1 (Ubuntu 24.04; aarch64) vscode"
			if candidate != "" && candidate != "curl/8.0" {
				wantUA = "codex_vscode/0.200.1 (Windows 11; aarch64) WindowsTerminal"
			}
			require.Equal(t, wantUA, identity.Headers.Get("User-Agent"))
			require.Equal(t, "codex_vscode", identity.Headers.Get("Originator"))
			require.Equal(t, "0.200.1", identity.Headers.Get("Version"), "same-owner fallback settings stay frozen across refresh")
		})
	}
}

func TestResolvePluginOutboundIdentityUsesAcquiredProxyAndMetadata(t *testing.T) {
	selectedProxy := &Proxy{ID: 9404, Protocol: "http", Host: "selected-proxy.invalid", Port: 8080, Status: StatusActive}
	acquiredProxy := &Proxy{ID: 9405, Protocol: "http", Host: "acquired-proxy.invalid", Port: 8081, Status: StatusActive}
	selected := &Account{ID: 9404, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		ProxyID: &selectedProxy.ID, Proxy: selectedProxy,
		Credentials: map[string]any{"access_token": "selected-token", "chatgpt_account_id": "selected-principal"},
	}
	acquired := &Account{ID: selected.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		ProxyID: &acquiredProxy.ID, Proxy: acquiredProxy,
		Credentials: map[string]any{
			"access_token": "acquired-token", "chatgpt_account_id": "acquired-principal", "chatgpt_account_is_fedramp": true,
			"user_agent": "codex_vscode/0.180.0 (Windows 11; aarch64) WindowsTerminal",
		},
	}
	provider := NewOpenAITokenProvider(nil, &pluginDirTokenCache{acquire: func(ctx context.Context) string {
		recordRequestCredentialOwner(ctx, acquired, "acquired-token")
		selected.Platform, selected.Type = PlatformGemini, AccountTypeAPIKey
		selected.Credentials = map[string]any{"api_key": "relinked-key"}
		return "acquired-token"
	}}, nil)
	svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{account: selected}, openAITokenProvider: provider}
	scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
	identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), scope, selected.ID)
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, PlatformOpenAI, identity.Platform)
	require.Equal(t, AccountTypeOAuth, identity.AccountType)
	require.Equal(t, "acquired-token", identity.Token)
	require.Equal(t, "http://acquired-proxy.invalid:8081", identity.ProxyURL)
	require.Equal(t, "acquired-principal", identity.Headers.Get("Chatgpt-Account-Id"))
	require.Equal(t, "true", identity.Headers.Get("X-Openai-Fedramp"))
	require.Equal(t, "codex_vscode/0.165.0 (Windows 11; aarch64) WindowsTerminal", identity.Headers.Get("User-Agent"))
	require.Equal(t, "codex_vscode", identity.Headers.Get("Originator"))
	require.Equal(t, "0.165.0", identity.Headers.Get("Version"))
}

func TestResolvePluginOutboundIdentityRejectsUnavailableAcquiredOwner(t *testing.T) {
	for _, mode := range []string{"missing-proxy", "out-of-scope-platform", "unsupported-type", "shadow", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			selected := &Account{ID: 9406, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "selected-token"},
			}
			acquired := &Account{ID: selected.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "acquired-token"},
			}
			switch mode {
			case "missing-proxy":
				proxyID := int64(9407)
				acquired.ProxyID = &proxyID
			case "out-of-scope-platform":
				acquired.Platform = PlatformGemini
			case "unsupported-type":
				acquired.Type = AccountTypeAPIKey
				acquired.Credentials = map[string]any{"api_key": "acquired-token"}
			case "shadow":
				parentID := int64(9408)
				acquired.ParentAccountID = &parentID
			}
			provider := NewOpenAITokenProvider(nil, &pluginDirTokenCache{acquire: func(ctx context.Context) string {
				recordRequestCredentialOwner(ctx, acquired, "acquired-token")
				if mode == "ambiguous" {
					conflicting := snapshotRequestCredentialAccount(acquired)
					conflicting.Credentials["chatgpt_account_id"] = "conflicting-principal"
					recordRequestCredentialOwner(ctx, conflicting, "acquired-token")
				}
				return "acquired-token"
			}}, nil)
			svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{account: selected}, openAITokenProvider: provider}
			scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), scope, selected.ID)
			require.Nil(t, identity, "no credential may escape a failed owner or capability check")
			if mode == "missing-proxy" || mode == "ambiguous" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestResolvePluginOutboundIdentityIgnoresCallerCredentialEvidence(t *testing.T) {
	for _, transmitted := range []bool{false, true} {
		t.Run(map[bool]string{false: "acquired", true: "transmitted"}[transmitted], func(t *testing.T) {
			previous := &Account{ID: 9409, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "same-token", "chatgpt_account_id": "caller-principal"},
			}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			recordRequestCredentialOwner(ctx, previous, "same-token")
			ctx = withRequestCredentialOwner(ctx, previous, "same-token")
			if transmitted {
				ctx = context.WithValue(ctx, requestCredentialTransportKey{}, requestCredentialTransportEvidence{owner: previous})
			}
			current := &Account{ID: previous.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "same-token", "chatgpt_account_id": "directory-principal"},
			}
			svc := &OpenAIGatewayService{accountRepo: &pluginDirRepoStub{account: current}}
			scope := newPluginAccountScope(pluginAccountScopeEntry{Platform: PlatformOpenAI, AccountType: AccountTypeOAuth})
			identity, err := svc.ResolvePluginOutboundIdentity(ctx, scope, current.ID)
			require.NoError(t, err)
			require.NotNil(t, identity)
			require.Equal(t, "same-token", identity.Token)
			require.Equal(t, "directory-principal", identity.Headers.Get("Chatgpt-Account-Id"))
		})
	}
}
