//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCodexRestrictionContext(userAgent, originator string, evidence bool) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	context.Request.Header.Set("User-Agent", userAgent)
	context.Request.Header.Set("originator", originator)
	if evidence {
		context.Request.Header.Set("x-codex-window-id", "window-1")
	}
	return context
}

func codexCLIOnlyAccount() *Account {
	return &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{"codex_cli_only": true},
	}
}

func TestOpenAICodexClientRestrictionDetector_Detect(t *testing.T) {
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	const officialUA = "codex_cli_rs/0.150.0 (Ubuntu 24.04; x86_64) xterm-256color"

	t.Run("restriction disabled bypasses profile checks", func(t *testing.T) {
		result := detector.Detect(newCodexRestrictionContext("curl/8", "curl", false), &Account{}, CodexRestrictionPolicy{}, nil)
		require.False(t, result.Enabled)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonDisabled, result.Reason)
	})

	t.Run("official coherent profile with known evidence is accepted", func(t *testing.T) {
		result := detector.Detect(newCodexRestrictionContext(officialUA, "codex_cli_rs", true), codexCLIOnlyAccount(), CodexRestrictionPolicy{}, nil)
		require.True(t, result.Enabled)
		require.True(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonMatchedOfficialProfile, result.Reason)
		require.Equal(t, "0.150.0", result.DetectedVersion)
	})

	t.Run("official profile without known evidence fails closed", func(t *testing.T) {
		result := detector.Detect(newCodexRestrictionContext(officialUA, "codex_cli_rs", false), codexCLIOnlyAccount(), CodexRestrictionPolicy{}, nil)
		require.True(t, result.Enabled)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonMissingKnownEvidence, result.Reason)
	})

	t.Run("deny list wins before official profile matching", func(t *testing.T) {
		policy := CodexRestrictionPolicy{Blacklist: []openai.AllowedClientEntry{{Originator: "codex_cli_rs"}}}
		result := detector.Detect(newCodexRestrictionContext(officialUA, "codex_cli_rs", true), codexCLIOnlyAccount(), policy, nil)
		require.True(t, result.Enabled)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonBlacklisted, result.Reason)
	})

	t.Run("explicit compatibility entry still requires known evidence", func(t *testing.T) {
		policy := CodexRestrictionPolicy{Whitelist: []openai.AllowedClientEntry{{Originator: "opencode", UAContains: []string{"opencode/"}}}}
		allowed := detector.Detect(newCodexRestrictionContext("opencode/1.0", "opencode", true), codexCLIOnlyAccount(), policy, nil)
		require.True(t, allowed.Matched)
		require.Equal(t, CodexClientRestrictionReasonMatchedCompatibilityEntry, allowed.Reason)

		rejected := detector.Detect(newCodexRestrictionContext("opencode/1.0", "opencode", false), codexCLIOnlyAccount(), policy, nil)
		require.False(t, rejected.Matched)
		require.Equal(t, CodexClientRestrictionReasonMissingKnownEvidence, rejected.Reason)
	})

	t.Run("recognized profiles honour version range", func(t *testing.T) {
		policy := CodexRestrictionPolicy{MinCodexVersion: "0.151.0"}
		result := detector.Detect(newCodexRestrictionContext(officialUA, "codex_cli_rs", true), codexCLIOnlyAccount(), policy, nil)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonVersionTooLow, result.Reason)
		require.Equal(t, "0.150.0", result.DetectedVersion)
		require.Equal(t, "0.151.0", result.MinCodexVersion)
	})
}
