//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

const testOpenAIAccountUserAgent = "codex_cli_rs/9.9.9 (Ubuntu 22.4.0; x86_64) xterm-256color"
const testOpenAIAccountCurrentUserAgent = "codex_cli_rs/" + codexCLIVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color"

type openAIIdentitySettingRepoStub struct {
	values map[string]string
}

func (s *openAIIdentitySettingRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (s *openAIIdentitySettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *openAIIdentitySettingRepoStub) Set(context.Context, string, string) error { return nil }

func (s *openAIIdentitySettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (s *openAIIdentitySettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}

func (s *openAIIdentitySettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *openAIIdentitySettingRepoStub) Delete(context.Context, string) error { return nil }

type openAIIdentityOAuthClientStub struct {
	exchangeUserAgent  string
	exchangeOriginator string
	exchangeVersion    string
	refreshUserAgent   string
	refreshOriginator  string
	refreshVersion     string
}

func (s *openAIIdentityOAuthClientStub) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	return nil, errors.New("identity-aware exchange was not used")
}

func (s *openAIIdentityOAuthClientStub) ExchangeCodeWithIdentity(_ context.Context, _, _, _, _, _, userAgent, originator, version string) (*openai.TokenResponse, error) {
	s.exchangeUserAgent = userAgent
	s.exchangeOriginator = originator
	s.exchangeVersion = version
	return &openai.TokenResponse{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600}, nil
}

func (s *openAIIdentityOAuthClientStub) RefreshToken(context.Context, string, string) (*openai.TokenResponse, error) {
	return nil, errors.New("identity-aware refresh was not used")
}

func (s *openAIIdentityOAuthClientStub) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	return nil, errors.New("identity-aware refresh was not used")
}

func (s *openAIIdentityOAuthClientStub) RefreshTokenWithClientIDAndIdentity(_ context.Context, _, _, _, userAgent, originator, version string) (*openai.TokenResponse, error) {
	s.refreshUserAgent = userAgent
	s.refreshOriginator = originator
	s.refreshVersion = version
	return &openai.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
}

func TestNormalizeOpenAIAccountUserAgent(t *testing.T) {
	t.Run("keeps exact current official identity", func(t *testing.T) {
		credentials := map[string]any{"user_agent": testOpenAIAccountUserAgent}
		require.NoError(t, NormalizeOpenAIAccountUserAgent(PlatformOpenAI, credentials))
		require.Equal(t, testOpenAIAccountUserAgent, credentials["user_agent"])
	})

	t.Run("rejects case variants and only accepts legacy identity when enabled", func(t *testing.T) {
		require.Error(t, NormalizeOpenAIAccountUserAgent(PlatformOpenAI, map[string]any{
			"user_agent": "CODEX_CLI_RS/9.9.9 (Ubuntu 22.4.0; x86_64) xterm-256color",
		}))
		legacy := map[string]any{"user_agent": "codex_exec/9.9.9 (Ubuntu 22.4.0; x86_64) xterm-256color"}
		require.Error(t, NormalizeOpenAIAccountUserAgent(PlatformOpenAI, legacy))
		require.NoError(t, NormalizeOpenAIAccountUserAgentWithCompatibility(PlatformOpenAI, legacy, true))
		require.Equal(t, "codex_exec/9.9.9 (Ubuntu 22.4.0; x86_64) xterm-256color", legacy["user_agent"])
	})

	t.Run("empty inherits global identity", func(t *testing.T) {
		credentials := map[string]any{"user_agent": "  "}
		require.NoError(t, NormalizeOpenAIAccountUserAgent(PlatformOpenAI, credentials))
		require.NotContains(t, credentials, "user_agent")
	})

	t.Run("rejects unsupported identity", func(t *testing.T) {
		err := NormalizeOpenAIAccountUserAgent(PlatformOpenAI, map[string]any{"user_agent": "curl/8.0"})
		require.Error(t, err)
	})
}

func TestOpenAIOAuthService_ReauthorizationUsesCredentialOwnerIdentity(t *testing.T) {
	account := Account{
		ID:       71,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"user_agent": testOpenAIAccountUserAgent,
		},
	}
	client := &openAIIdentityOAuthClientStub{}
	svc := NewOpenAIOAuthService(nil, client)
	svc.SetAccountRepository(stubOpenAIAccountRepo{accounts: []Account{account}})
	defer svc.Stop()

	result, err := svc.GenerateAuthURLForAccount(context.Background(), &account, nil, "", PlatformOpenAI)
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.NotNil(t, session.AccountID)
	require.Equal(t, account.ID, *session.AccountID)

	_, err = svc.ExchangeCode(context.Background(), &OpenAIExchangeCodeInput{
		SessionID: result.SessionID,
		State:     session.State,
		Code:      "authorization-code",
	})
	require.NoError(t, err)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, client.exchangeUserAgent)
	require.Equal(t, "codex_cli_rs", client.exchangeOriginator)
	require.Equal(t, codexCLIVersion, client.exchangeVersion)
}

func TestOpenAIOAuthService_RefreshAndPATUseAccountIdentity(t *testing.T) {
	account := &Account{
		ID:       72,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"user_agent":    testOpenAIAccountUserAgent,
			"refresh_token": "refresh",
		},
	}
	client := &openAIIdentityOAuthClientStub{}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	_, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, client.refreshUserAgent)
	require.Equal(t, "codex_cli_rs", client.refreshOriginator)
	require.Equal(t, codexCLIVersion, client.refreshVersion)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testOpenAIAccountCurrentUserAgent, r.Header.Get("User-Agent"))
		require.Equal(t, "codex_cli_rs", r.Header.Get("Originator"))
		require.Equal(t, codexCLIVersion, r.Header.Get("Version"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"email":"user@example.com","chatgpt_user_id":"user","chatgpt_account_id":"account","chatgpt_plan_type":"plus","chatgpt_account_is_fedramp":false}`))
	}))
	defer server.Close()

	previousURL := openAICodexPATWhoamiURL
	openAICodexPATWhoamiURL = server.URL
	t.Cleanup(func() { openAICodexPATWhoamiURL = previousURL })

	_, err = svc.validateCodexPersonalAccessTokenWithAccount(context.Background(), "at-token", "", account)
	require.NoError(t, err)
}

func TestOpenAIGatewayService_ShadowUsesCredentialOwnerIdentity(t *testing.T) {
	parentID := int64(73)
	parent := Account{
		ID:       parentID,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"user_agent": testOpenAIAccountUserAgent,
		},
	}
	shadow := &Account{
		ID:              74,
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		ParentAccountID: &parentID,
		Credentials:     map[string]any{},
	}
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{parent}}}
	identity := svc.resolveOpenAIOutboundIdentity(context.Background(), shadow)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, identity.UserAgent)
	require.Equal(t, "codex_cli_rs", identity.Originator)
	require.Equal(t, codexCLIVersion, identity.Version)
}

func TestOpenAIAccountUserAgentLimit(t *testing.T) {
	credentials := map[string]any{"user_agent": strings.Repeat("x", 513)}
	require.Error(t, NormalizeOpenAIAccountUserAgent(PlatformOpenAI, credentials))
}

func TestOpenAIGatewayService_ResolvedIdentityPrefersAccountThenGlobalThenDefault(t *testing.T) {
	globalUA := "codex-tui/8.8.8 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 8.8.8)"
	globalCurrentUA := "codex-tui/" + codexCLIVersion + " (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; " + codexCLIVersion + ")"
	settingService := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexUserAgent: globalUA,
	}}}
	svc := &OpenAIGatewayService{settingService: settingService}

	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"user_agent": testOpenAIAccountUserAgent,
		},
	}
	identity := svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, identity.UserAgent)
	require.Equal(t, codexCLIVersion, identity.Version)

	account.Credentials = map[string]any{}
	identity = svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, globalCurrentUA, identity.UserAgent)
	require.Equal(t, codexCLIVersion, identity.Version)

	svc.settingService = nil
	identity = svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, DefaultOpenAICodexUserAgent, identity.UserAgent)
}

func TestOpenAIGatewayService_LegacyIdentityRequiresGlobalCompatibilityMode(t *testing.T) {
	legacyUA := "codex_vscode_copilot/8.8.8 (Ubuntu 22.4.0; x86_64) xterm-256color"
	settings := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexUserAgent: legacyUA,
	}}}
	svc := &OpenAIGatewayService{settingService: settings}

	identity := svc.resolveOpenAIOutboundIdentity(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.Equal(t, DefaultOpenAICodexUserAgent, identity.UserAgent)
	require.Equal(t, openai.CodexDefaultOriginator, identity.Originator)

	settings = &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexUserAgent:                         legacyUA,
		SettingKeyCodexLegacyClientProfileCompatibilityEnabled: "true",
	}}}
	svc.settingService = settings
	identity = svc.resolveOpenAIOutboundIdentity(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.Equal(t, "codex_vscode_copilot/"+codexCLIVersion+" (Ubuntu 22.4.0; x86_64) xterm-256color", identity.UserAgent)
	require.Equal(t, "codex_vscode_copilot", identity.Originator)
	require.Equal(t, openAIOutboundIdentitySourceGlobal, identity.Source)
}

func TestOpenAIGatewayService_StaleSyncedVersionDoesNotChangeSelectedSource(t *testing.T) {
	globalUA := "codex-tui/8.8.8 (Ubuntu 22.4.0; x86_64) WindowsTerminal (codex-tui; 8.8.8)"
	settingService := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexUserAgent:           globalUA,
		SettingKeyOpenAICodexClientVersionSynced: "0.146.0",
	}}}
	svc := &OpenAIGatewayService{settingService: settingService}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"user_agent": testOpenAIAccountUserAgent,
		},
	}

	identity := svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, identity.UserAgent)
	require.Equal(t, "codex_cli_rs", identity.Originator)
	require.Equal(t, codexCLIVersion, identity.Version)

	account.Credentials = map[string]any{}
	identity = svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t,
		"codex-tui/"+codexCLIVersion+" (Ubuntu 22.4.0; x86_64) WindowsTerminal (codex-tui; "+codexCLIVersion+")",
		identity.UserAgent,
	)
	require.Equal(t, "codex-tui", identity.Originator)
	require.Equal(t, codexCLIVersion, identity.Version)
}

func TestOpenAIGatewayService_ForceCodexCLILeavesOutboundIdentityPriorityIntact(t *testing.T) {
	globalUA := "codex-tui/8.8.8 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 8.8.8)"
	settingService := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexUserAgent: globalUA,
	}}}
	svc := &OpenAIGatewayService{
		cfg:            &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: true}},
		settingService: settingService,
	}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"user_agent": testOpenAIAccountUserAgent,
		},
	}

	identity := svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, identity.UserAgent)
	require.Equal(t, "codex_cli_rs", identity.Originator)
	require.Equal(t, codexCLIVersion, identity.Version)
}

func TestAdminOpenAIPrivacyIdentitySynchronizesConfiguredVersion(t *testing.T) {
	settingService := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexClientVersion: "0.200.1",
	}}}
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"user_agent": testOpenAIAccountUserAgent,
		},
	}

	identity := (&adminServiceImpl{settingService: settingService}).resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, "codex_cli_rs/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color", identity.UserAgent)
	require.Equal(t, "codex_cli_rs", identity.Originator)
	require.Equal(t, "0.200.1", identity.Version)
}
