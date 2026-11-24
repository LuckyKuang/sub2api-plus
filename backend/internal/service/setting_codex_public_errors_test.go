//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func codexPublicErrorString(value string) *string { return &value }

func codexPublicErrorItemByKey(t *testing.T, snapshot *CodexPublicErrorSnapshot, key string) CodexPublicErrorItem {
	t.Helper()
	for _, item := range snapshot.Items {
		if item.Key == key {
			return item
		}
	}
	t.Fatalf("catalog item %s not found", key)
	return CodexPublicErrorItem{}
}

func TestCodexPublicErrorSettingsRevisionAndOverrides(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	settings := svc.settingService
	ctx := t.Context()

	snapshot := settings.CodexPublicErrorSnapshot(ctx)
	require.Len(t, snapshot.Items, 76)
	require.False(t, snapshot.CustomEnabled)
	require.Equal(t, "当前请求受到频率限制，请稍后再试。", settings.CodexPublicErrorMessage(ctx, "rate_limit_exceeded"))

	enabled := true
	message := "  请求较多，请稍后重试。  "
	next, err := settings.SaveCodexPublicErrorSettings(ctx, snapshot.Revision, &enabled, map[string]*string{
		"rate_limit_exceeded": &message,
	})
	require.NoError(t, err)
	require.True(t, next.CustomEnabled)
	require.Equal(t, "请求较多，请稍后重试。", settings.CodexPublicErrorMessage(ctx, "rate_limit_exceeded"))

	// A stale draft keeps its local revision and cannot overwrite the new text.
	_, err = settings.SaveCodexPublicErrorSettings(ctx, snapshot.Revision, &enabled, nil)
	require.ErrorIs(t, err, ErrCodexPublicErrorRevisionConflict)

	// Disabling custom text keeps the saved override for later re-enabling.
	disabled := false
	next, err = settings.SaveCodexPublicErrorSettings(ctx, next.Revision, &disabled, nil)
	require.NoError(t, err)
	require.Equal(t, "当前请求受到频率限制，请稍后再试。", settings.CodexPublicErrorMessage(ctx, "rate_limit_exceeded"))
	require.Equal(t, "请求较多，请稍后重试。", *codexPublicErrorItemByKey(t, next, "rate_limit_exceeded").CustomMessage)

	// Restoring one entry deletes only that override.
	enabled = true
	next, err = settings.SaveCodexPublicErrorSettings(ctx, next.Revision, &enabled, map[string]*string{
		"rate_limit_exceeded": nil,
	})
	require.NoError(t, err)
	require.Equal(t, "当前请求受到频率限制，请稍后再试。", settings.CodexPublicErrorMessage(ctx, "rate_limit_exceeded"))
	require.Nil(t, codexPublicErrorItemByKey(t, next, "rate_limit_exceeded").CustomMessage)

	reloaded := NewSettingService(repo, nil)
	require.Equal(t, "当前请求受到频率限制，请稍后再试。", reloaded.CodexPublicErrorMessage(ctx, "rate_limit_exceeded"))
}

func TestCodexPublicErrorSettingsValidationAndHistory(t *testing.T) {
	svc, repo := safetyBufferingTestService(t)
	settings := svc.settingService
	ctx := t.Context()
	snapshot := settings.CodexPublicErrorSnapshot(ctx)

	_, err := settings.SaveCodexPublicErrorSettings(ctx, snapshot.Revision, nil, map[string]*string{
		"not_a_catalog_key": codexPublicErrorString("secret"),
	})
	require.ErrorIs(t, err, ErrCodexPublicErrorInvalid)

	_, err = settings.SaveCodexPublicErrorSettings(ctx, snapshot.Revision, nil, map[string]*string{
		"rate_limit_exceeded": codexPublicErrorString(strings.Repeat("x", 501)),
	})
	require.ErrorIs(t, err, ErrCodexPublicErrorInvalid)

	_, err = settings.SaveCodexPublicErrorSettings(ctx, snapshot.Revision, nil, map[string]*string{
		"rate_limit_exceeded": codexPublicErrorString("first line\nsecond line"),
	})
	require.ErrorIs(t, err, ErrCodexPublicErrorInvalid)

	// Historical unknown keys are ignored rather than rendered.
	repo.mu.Lock()
	repo.values[SettingKeyOpenAIErrorMessageOverrides] = `{"removed_key":"old text","rate_limit_exceeded":"saved text"}`
	repo.values[SettingKeyOpenAIErrorMessagesNormalized] = "true"
	repo.mu.Unlock()
	settings.refreshCodexSafetyBuffering(ctx)
	require.Equal(t, "saved text", settings.CodexPublicErrorMessage(ctx, "rate_limit_exceeded"))
	for _, item := range settings.CodexPublicErrorSnapshot(ctx).Items {
		require.NotEqual(t, "removed_key", item.Key)
	}
}
