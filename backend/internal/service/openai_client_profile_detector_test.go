package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newCodexProfileDetectorContext(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	for name, value := range headers {
		c.Request.Header.Set(name, value)
	}
	return c
}

func codexProfileOnlyAccount() *Account {
	return &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{"codex_cli_only": true}}
}

func officialCodexProfileHeaders(originator string) map[string]string {
	return map[string]string{
		"User-Agent":              originator + "/0.147.0 (Mac OS 26.0; arm64) terminal (" + originator + "; 0.147.0)",
		"originator":              originator,
		"x-codex-installation-id": "installation-1",
	}
}

func TestDetectCodexClientProfile(t *testing.T) {
	detector := NewOpenAICodexClientRestrictionDetector(nil)
	account := codexProfileOnlyAccount()

	t.Run("built-in CLI profile accepts coherent known evidence", func(t *testing.T) {
		result := detector.Detect(newCodexProfileDetectorContext(officialCodexProfileHeaders("codex_cli_rs")), account, CodexRestrictionPolicy{}, nil)
		require.True(t, result.Enabled)
		require.True(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonMatchedOfficialProfile, result.Reason)
		require.Equal(t, string(openai.CodexClientProfileCLI), result.Profile)
		require.Equal(t, "0.147.0", result.DetectedVersion)
	})

	t.Run("official CLI, TUI, VS Code, and desktop aliases are accepted", func(t *testing.T) {
		for _, originator := range []string{
			"codex_cli_rs",
			"codex-tui",
			"codex_vscode",
			"codex_chatgpt_desktop",
			"codex_atlas",
		} {
			t.Run(originator, func(t *testing.T) {
				result := detector.Detect(newCodexProfileDetectorContext(officialCodexProfileHeaders(originator)), account, CodexRestrictionPolicy{}, nil)
				require.True(t, result.Matched)
			})
		}
	})

	t.Run("official Desktop and JetBrains product-family profiles are accepted", func(t *testing.T) {
		for _, originator := range []string{"Codex Desktop", "Codex JetBrains"} {
			t.Run(originator, func(t *testing.T) {
				result := detector.Detect(newCodexProfileDetectorContext(officialCodexProfileHeaders(originator)), account, CodexRestrictionPolicy{}, nil)
				require.True(t, result.Matched)
				require.Equal(t, string(openai.CodexClientProfileFamily), result.Profile)
			})
		}
	})

	t.Run("unverified legacy aliases are not treated as official", func(t *testing.T) {
		for _, originator := range []string{"codex_app", "codex_exec", "codex_sdk_ts", "codex_vscode_copilot"} {
			t.Run(originator, func(t *testing.T) {
				result := detector.Detect(newCodexProfileDetectorContext(officialCodexProfileHeaders(originator)), account, CodexRestrictionPolicy{}, nil)
				require.False(t, result.Matched)
				require.Equal(t, CodexClientRestrictionReasonNotMatchedProfile, result.Reason)
			})
		}
	})

	t.Run("legacy aliases are accepted only by explicit global compatibility mode", func(t *testing.T) {
		for _, originator := range []string{"codex_app", "codex_exec", "codex_sdk_ts", "codex_vscode_copilot"} {
			t.Run(originator, func(t *testing.T) {
				result := detector.Detect(
					newCodexProfileDetectorContext(officialCodexProfileHeaders(originator)),
					account,
					CodexRestrictionPolicy{LegacyClientProfileCompatibilityEnabled: true},
					nil,
				)
				require.True(t, result.Matched)
				require.Equal(t, CodexClientRestrictionReasonMatchedLegacyCompatibilityProfile, result.Reason)
				require.Equal(t, string(openai.CodexClientProfileLegacyCompatibility), result.Profile)
			})
		}
	})

	t.Run("legacy compatibility still requires evidence and version bounds", func(t *testing.T) {
		headers := officialCodexProfileHeaders("codex_exec")
		delete(headers, "x-codex-installation-id")
		missingEvidence := detector.Detect(
			newCodexProfileDetectorContext(headers), account,
			CodexRestrictionPolicy{LegacyClientProfileCompatibilityEnabled: true}, nil,
		)
		require.False(t, missingEvidence.Matched)
		require.Equal(t, CodexClientRestrictionReasonMissingKnownEvidence, missingEvidence.Reason)

		headers = officialCodexProfileHeaders("codex_exec")
		headers["User-Agent"] = "codex_exec/0.140.0 (Mac OS 26.0; arm64) terminal"
		tooOld := detector.Detect(
			newCodexProfileDetectorContext(headers), account,
			CodexRestrictionPolicy{LegacyClientProfileCompatibilityEnabled: true, MinCodexVersion: "0.141.0"}, nil,
		)
		require.False(t, tooOld.Matched)
		require.Equal(t, CodexClientRestrictionReasonVersionTooLow, tooOld.Reason)
	})

	t.Run("UA and Originator must be an exact coherent pair", func(t *testing.T) {
		headers := officialCodexProfileHeaders("codex_cli_rs")
		headers["originator"] = "codex_vscode"
		result := detector.Detect(newCodexProfileDetectorContext(headers), account, CodexRestrictionPolicy{}, nil)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonNotMatchedProfile, result.Reason)
	})

	t.Run("UA alone and Originator alone are rejected", func(t *testing.T) {
		uaOnly := officialCodexProfileHeaders("codex_cli_rs")
		delete(uaOnly, "originator")
		require.False(t, detector.Detect(newCodexProfileDetectorContext(uaOnly), account, CodexRestrictionPolicy{}, nil).Matched)

		originatorOnly := officialCodexProfileHeaders("codex_cli_rs")
		originatorOnly["User-Agent"] = "curl/8.0"
		require.False(t, detector.Detect(newCodexProfileDetectorContext(originatorOnly), account, CodexRestrictionPolicy{}, nil).Matched)
	})

	t.Run("UA trailer and case-insensitive Codex family cannot bypass", func(t *testing.T) {
		trailer := officialCodexProfileHeaders("codex_cli_rs")
		trailer["User-Agent"] = "curl/8.0 (codex_cli_rs; 0.147.0)"
		require.False(t, detector.Detect(newCodexProfileDetectorContext(trailer), account, CodexRestrictionPolicy{}, nil).Matched)

		family := officialCodexProfileHeaders("Codex Desktop")
		family["originator"] = "codex desktop"
		require.False(t, detector.Detect(newCodexProfileDetectorContext(family), account, CodexRestrictionPolicy{}, nil).Matched)
	})

	t.Run("only known non-empty evidence names count", func(t *testing.T) {
		fake := officialCodexProfileHeaders("codex_cli_rs")
		delete(fake, "x-codex-installation-id")
		fake["x-codex-fake"] = "1"
		result := detector.Detect(newCodexProfileDetectorContext(fake), account, CodexRestrictionPolicy{}, nil)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonMissingKnownEvidence, result.Reason)
	})

	t.Run("Responses WebSocket handshake accepts the official window identifier", func(t *testing.T) {
		// Upstream Codex builds its Responses WebSocket handshake with the
		// coherent originator/UA pair and x-codex-window-id. Keep this fixture
		// separate from the HTTP installation-id shape so a future tightening
		// cannot accidentally block the official streaming transport.
		headers := officialCodexProfileHeaders("codex_cli_rs")
		delete(headers, "x-codex-installation-id")
		headers["x-codex-window-id"] = "window-1"

		result := detector.Detect(newCodexProfileDetectorContext(headers), account, CodexRestrictionPolicy{}, []byte(`{"type":"response.create"}`))
		require.True(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonMatchedOfficialProfile, result.Reason)
	})

	t.Run("version bounds apply to built-in profiles", func(t *testing.T) {
		low := officialCodexProfileHeaders("codex_cli_rs")
		low["User-Agent"] = "codex_cli_rs/0.140.0 (Mac OS 26.0; arm64) terminal"
		result := detector.Detect(newCodexProfileDetectorContext(low), account, CodexRestrictionPolicy{MinCodexVersion: "0.141.0"}, nil)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonVersionTooLow, result.Reason)
	})

	t.Run("blacklist precedes official profile", func(t *testing.T) {
		result := detector.Detect(
			newCodexProfileDetectorContext(officialCodexProfileHeaders("codex_cli_rs")),
			account,
			CodexRestrictionPolicy{Blacklist: []openai.AllowedClientEntry{{Originator: "codex_cli_rs"}}},
			nil,
		)
		require.False(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonBlacklisted, result.Reason)
	})

	t.Run("explicit compatibility profile remains separate and needs known evidence", func(t *testing.T) {
		headers := map[string]string{
			"User-Agent":        "opencode/1.0.0",
			"originator":        "opencode",
			"x-codex-window-id": "window-1",
		}
		result := detector.Detect(
			newCodexProfileDetectorContext(headers),
			account,
			CodexRestrictionPolicy{Whitelist: []openai.AllowedClientEntry{{Originator: "opencode", UAContains: []string{"opencode/"}}}},
			nil,
		)
		require.True(t, result.Matched)
		require.Equal(t, CodexClientRestrictionReasonMatchedCompatibilityEntry, result.Reason)
		require.Empty(t, result.Profile)
	})
}

func TestDetectCodexClientProfile_ForceCodexCLIDoesNotBypass(t *testing.T) {
	detector := NewOpenAICodexClientRestrictionDetector(&config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: true}})
	result := detector.Detect(
		newCodexProfileDetectorContext(map[string]string{"User-Agent": "curl/8.0", "x-codex-window-id": "window-1"}),
		codexProfileOnlyAccount(),
		CodexRestrictionPolicy{},
		nil,
	)
	require.True(t, result.Enabled)
	require.False(t, result.Matched)
	require.Equal(t, CodexClientRestrictionReasonNotMatchedProfile, result.Reason)
}
