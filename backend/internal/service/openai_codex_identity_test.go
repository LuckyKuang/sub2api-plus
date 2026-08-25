//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func requireCanonicalCodexIdentity(t *testing.T, headers http.Header) {
	t.Helper()
	require.Equal(t, DefaultOpenAICodexUserAgent, headers.Get("user-agent"))
	require.Equal(t, openai.CodexDefaultOriginator, headers.Get("originator"))
	require.Equal(t, DefaultOpenAICodexVersion, headers.Get("version"))
}

// Regression coverage for the common OAuth header stage. Inbound identity
// headers only provide request classification; they must not survive to the
// credential-owning account's upstream request.
func TestEnsureCodexIdentityHeaders(t *testing.T) {
	t.Run("fills missing headers before enforcement", func(t *testing.T) {
		headers := make(http.Header)

		ensureCodexIdentityHeaders(headers)
		enforceCodexIdentityHeaders(headers)

		requireCanonicalCodexIdentity(t, headers)
		require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
	})

	t.Run("enforcement replaces inbound identity", func(t *testing.T) {
		headers := make(http.Header)
		headers.Set("user-agent", "codex_vscode/0.150.0 (Ubuntu 24.04; x86_64) vscode")
		headers.Set("originator", "codex_vscode")
		headers.Set("version", "0.150.0")

		ensureCodexIdentityHeaders(headers)
		enforceCodexIdentityHeaders(headers)

		requireCanonicalCodexIdentity(t, headers)
	})
}

func TestEnforceCodexIdentityHeaders(t *testing.T) {
	for _, userAgent := range []string{
		"codex-tui/0.140.2 (Mac OS X 14.0; arm64) iTerm",
		"codex_vscode/0.150.0 (Ubuntu 24.04; x86_64) vscode",
		"third-party-client/1.0",
	} {
		t.Run(userAgent, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("originator", "untrusted")
			headers.Set("user-agent", userAgent)
			headers.Set("version", "0.1.0")

			enforceCodexIdentityHeaders(headers)

			requireCanonicalCodexIdentity(t, headers)
		})
	}
}

func TestEnforceCodexIdentityHeadersWithAccountOverrideKeepsOnlyFingerprint(t *testing.T) {
	headers := make(http.Header)
	headers.Set("originator", "codex_cli_rs")

	enforceCodexIdentityHeadersWithUA(headers, "codex_vscode/0.125.0 (Ubuntu 24.04; x86_64) vscode")

	require.Equal(t, "codex_vscode", headers.Get("originator"))
	require.Equal(t, "codex_vscode/"+DefaultOpenAICodexVersion+" (Ubuntu 24.04; x86_64) vscode", headers.Get("user-agent"))
	require.Equal(t, DefaultOpenAICodexVersion, headers.Get("version"))
	require.NotContains(t, headers.Get("user-agent"), "0.125.0")
}

func TestEnforceCodexIdentityHeadersFollowsCanonicalResolver(t *testing.T) {
	SetCodexCanonicalUserAgentResolver(func() string {
		return "codex_cli_rs/0.200.1 (Ubuntu 24.04; x86_64) xterm-256color"
	})
	t.Cleanup(func() { SetCodexCanonicalUserAgentResolver(nil) })

	headers := make(http.Header)
	headers.Set("originator", "codex-tui")
	enforceCodexIdentityHeaders(headers)

	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
	require.Equal(t, "codex_cli_rs/0.200.1 (Ubuntu 24.04; x86_64) xterm-256color", headers.Get("user-agent"))
	require.Equal(t, "0.200.1", headers.Get("version"))
}

func TestResolveOpenAIOutboundIdentityCandidatesKeepsSourcePriority(t *testing.T) {
	accountUA := "codex_vscode/0.150.0 (Ubuntu 24.04; x86_64) vscode"
	globalUA := "codex-tui/0.151.0 (Mac OS X 15.0; arm64) iTerm"

	identity := resolveOpenAIOutboundIdentityCandidates(accountUA, globalUA)
	require.Equal(t, openAIOutboundIdentitySourceAccount, identity.Source)
	require.Contains(t, identity.UserAgent, "codex_vscode/")
	require.Equal(t, "codex_vscode", identity.Originator)

	identity = resolveOpenAIOutboundIdentityCandidates("not-a-codex-client", globalUA)
	require.Equal(t, openAIOutboundIdentitySourceGlobal, identity.Source)
	require.Contains(t, identity.UserAgent, "codex-tui/")
	require.Equal(t, "codex-tui", identity.Originator)

	identity = resolveOpenAIOutboundIdentityCandidates("not-a-codex-client", "also-invalid")
	require.Equal(t, openAIOutboundIdentitySourceDefault, identity.Source)
	require.Equal(t, DefaultOpenAICodexUserAgent, identity.UserAgent)
	require.Equal(t, openai.CodexDefaultOriginator, identity.Originator)
}
