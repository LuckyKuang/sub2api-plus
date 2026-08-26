package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGetCodexRestrictionPolicy(t *testing.T) {
	svc := NewSettingService(&codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyMinCodexVersion:                              "0.141.0",
		SettingKeyMaxCodexVersion:                              "0.200.0",
		SettingKeyCodexLegacyClientProfileCompatibilityEnabled: "true",
		SettingKeyCodexCLIOnlyWhitelist:                        `[{"originator":"opencode","ua_contains":["opencode/"]}]`,
		SettingKeyCodexCLIOnlyBlacklist:                        `[{"originator":"evil"}]`,
	}}, &config.Config{})

	pol := svc.GetCodexRestrictionPolicy(context.Background())
	require.Equal(t, "0.141.0", pol.MinCodexVersion)
	require.Equal(t, "0.200.0", pol.MaxCodexVersion)
	require.True(t, pol.LegacyClientProfileCompatibilityEnabled)
	require.Len(t, pol.Whitelist, 1)
	require.Equal(t, "opencode", pol.Whitelist[0].Originator)
	require.Equal(t, []string{"opencode/"}, pol.Whitelist[0].UAContains)
	require.Len(t, pol.Blacklist, 1)
	require.Equal(t, "evil", pol.Blacklist[0].Originator)
}

func TestGetCodexRestrictionPolicy_DefaultsSafe(t *testing.T) {
	svc := NewSettingService(&codexPolicyMigrationRepoStub{values: map[string]string{}}, &config.Config{})

	pol := svc.GetCodexRestrictionPolicy(context.Background())
	require.Empty(t, pol.MinCodexVersion)
	require.Empty(t, pol.Whitelist)
	require.Empty(t, pol.Blacklist)
	require.False(t, pol.LegacyClientProfileCompatibilityEnabled)
}

func TestGetCodexRestrictionPolicy_InvalidJSONSafe(t *testing.T) {
	svc := NewSettingService(&codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyCodexCLIOnlyWhitelist: "not-json",
		SettingKeyCodexCLIOnlyBlacklist: "{bad",
	}}, &config.Config{})

	pol := svc.GetCodexRestrictionPolicy(context.Background())
	require.Empty(t, pol.Whitelist, "非法 JSON → 安全空名单")
	require.Empty(t, pol.Blacklist, "非法 JSON → 安全空名单")
}

type codexPolicyMigrationRepoStub struct {
	values map[string]string
	sets   map[string]string
}

func (s *codexPolicyMigrationRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unused")
}
func (s *codexPolicyMigrationRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}
func (s *codexPolicyMigrationRepoStub) Set(ctx context.Context, key, value string) error {
	if s.sets == nil {
		s.sets = map[string]string{}
	}
	s.sets[key] = value
	s.values[key] = value
	return nil
}
func (s *codexPolicyMigrationRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unused")
}
func (s *codexPolicyMigrationRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unused")
}
func (s *codexPolicyMigrationRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unused")
}
func (s *codexPolicyMigrationRepoStub) Delete(ctx context.Context, key string) error {
	delete(s.values, key)
	return nil
}

func TestMigrateOpenAIAllowClaudeCodeCodexPluginSetting(t *testing.T) {
	t.Run("legacy true appends Claude Code entry to whitelist", func(t *testing.T) {
		repo := &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAIAllowClaudeCodeCodexPlugin: "true",
			SettingKeyCodexCLIOnlyWhitelist:            `[{"originator":"opencode","ua_contains":["opencode/"]}]`,
		}}
		svc := NewSettingService(repo, &config.Config{})

		require.NoError(t, svc.MigrateOpenAIAllowClaudeCodeCodexPluginSetting(context.Background()))

		raw := repo.sets[SettingKeyCodexCLIOnlyWhitelist]
		require.NotEmpty(t, raw)
		var entries []struct {
			Originator string   `json:"originator"`
			UAContains []string `json:"ua_contains"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &entries))
		require.Len(t, entries, 2)
		require.Equal(t, "opencode", entries[0].Originator)
		require.Equal(t, "Claude Code", entries[1].Originator)
		require.Equal(t, []string{"Claude Code/"}, entries[1].UAContains)
		_, exists := repo.values[SettingKeyOpenAIAllowClaudeCodeCodexPlugin]
		require.False(t, exists, "successful migration must consume the deprecated key")
	})

	t.Run("legacy true does not duplicate existing Claude Code entry", func(t *testing.T) {
		repo := &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAIAllowClaudeCodeCodexPlugin: "true",
			SettingKeyCodexCLIOnlyWhitelist:            `[{"originator":"Claude Code","ua_contains":["Claude Code/"]}]`,
		}}
		svc := NewSettingService(repo, &config.Config{})

		require.NoError(t, svc.MigrateOpenAIAllowClaudeCodeCodexPluginSetting(context.Background()))

		_, wrote := repo.sets[SettingKeyCodexCLIOnlyWhitelist]
		require.False(t, wrote)
		_, exists := repo.values[SettingKeyOpenAIAllowClaudeCodeCodexPlugin]
		require.False(t, exists, "already-migrated state must still consume the deprecated key")
	})

	t.Run("legacy false is consumed without changing the whitelist", func(t *testing.T) {
		repo := &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAIAllowClaudeCodeCodexPlugin: "false",
			SettingKeyCodexCLIOnlyWhitelist:            `[{"originator":"opencode","ua_contains":["opencode/"]}]`,
		}}
		svc := NewSettingService(repo, &config.Config{})

		require.NoError(t, svc.MigrateOpenAIAllowClaudeCodeCodexPluginSetting(context.Background()))
		require.Equal(t, `[{"originator":"opencode","ua_contains":["opencode/"]}]`, repo.values[SettingKeyCodexCLIOnlyWhitelist])
		_, exists := repo.values[SettingKeyOpenAIAllowClaudeCodeCodexPlugin]
		require.False(t, exists)
	})
}

func TestMigrateGrokDefaultTextModel(t *testing.T) {
	t.Run("upgrades legacy built-in default", func(t *testing.T) {
		repo := &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyGrokDefaultTextModel: "grok-4.5",
		}}
		svc := NewSettingService(repo, &config.Config{})
		require.NoError(t, svc.MigrateGrokDefaultTextModel(context.Background()))
		require.Equal(t, "grok-4.6", repo.values[SettingKeyGrokDefaultTextModel])
		require.Equal(t, "grok-4.6", repo.sets[SettingKeyGrokDefaultTextModel])
	})

	t.Run("does not overwrite an explicit model", func(t *testing.T) {
		repo := &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyGrokDefaultTextModel: "grok-4.3",
		}}
		svc := NewSettingService(repo, &config.Config{})
		require.NoError(t, svc.MigrateGrokDefaultTextModel(context.Background()))
		require.Equal(t, "grok-4.3", repo.values[SettingKeyGrokDefaultTextModel])
		_, wrote := repo.sets[SettingKeyGrokDefaultTextModel]
		require.False(t, wrote)
	})

	t.Run("missing setting is left for normal defaults", func(t *testing.T) {
		repo := &codexPolicyMigrationRepoStub{values: map[string]string{}}
		svc := NewSettingService(repo, &config.Config{})
		require.NoError(t, svc.MigrateGrokDefaultTextModel(context.Background()))
		_, wrote := repo.sets[SettingKeyGrokDefaultTextModel]
		require.False(t, wrote)
	})
}
