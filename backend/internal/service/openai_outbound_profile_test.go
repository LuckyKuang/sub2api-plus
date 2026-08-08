package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const testCodexCLIUserAgent = "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color"

func TestResolveOpenAIOutboundIdentityCandidates(t *testing.T) {
	accountUA := "codex-tui/0.150.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.150.0)"

	tests := []struct {
		name           string
		accountUA      string
		systemUA       string
		wantUserAgent  string
		wantOriginator string
		wantVersion    string
	}{
		{
			name:           "合法账号 UA 优先于系统设置",
			accountUA:      accountUA,
			systemUA:       testCodexCLIUserAgent,
			wantUserAgent:  accountUA,
			wantOriginator: "codex-tui",
			wantVersion:    "0.150.0",
		},
		{
			name:           "无效账号 UA 回退到系统设置",
			accountUA:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
			systemUA:       testCodexCLIUserAgent,
			wantUserAgent:  testCodexCLIUserAgent,
			wantOriginator: "codex_cli_rs",
			wantVersion:    "0.144.1",
		},
		{
			name:           "无效账号与系统 UA 回退内置默认",
			accountUA:      "curl/8.7.1",
			systemUA:       "not-a-codex-client/1.0",
			wantUserAgent:  DefaultOpenAICodexUserAgent,
			wantOriginator: "codex_cli_rs",
			wantVersion:    codexCLIVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			identity := resolveOpenAIOutboundIdentityCandidates(tt.accountUA, tt.systemUA)
			require.Equal(t, tt.wantUserAgent, identity.UserAgent)
			require.Equal(t, tt.wantOriginator, identity.Originator)
			require.Equal(t, tt.wantVersion, identity.Version)
		})
	}
}

func TestApplyResolvedOpenAIOutboundIdentity(t *testing.T) {
	identity := resolveOpenAIOutboundIdentityCandidates("", testCodexCLIUserAgent)

	t.Run("OAuth 请求设置配套身份", func(t *testing.T) {
		headers := http.Header{
			"User-Agent": {"Mozilla/5.0"},
			"Originator": {"client-controlled"},
		}
		applyResolvedOpenAIOutboundIdentity(headers, identity, true)
		require.Equal(t, testCodexCLIUserAgent, headers.Get("User-Agent"))
		require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
		require.Equal(t, "0.144.1", headers.Get("Version"))
	})

	t.Run("API Key 请求不携带 OAuth identity", func(t *testing.T) {
		headers := http.Header{
			"User-Agent": {"Mozilla/5.0"},
			"Originator": {"client-controlled"},
		}
		applyResolvedOpenAIOutboundIdentity(headers, identity, false)
		require.Equal(t, testCodexCLIUserAgent, headers.Get("User-Agent"))
		require.Empty(t, headers.Get("Originator"))
		require.Empty(t, headers.Get("Version"))
	})

	t.Run("API Key compact 请求同步既有协议版本", func(t *testing.T) {
		headers := http.Header{"Version": {"0.1.0"}}
		applyResolvedOpenAIOutboundIdentity(headers, identity, false)
		require.Equal(t, "0.144.1", headers.Get("Version"))
	})
}

func TestOpenAIProtectedHeaderOverrides(t *testing.T) {
	for _, name := range []string{
		"User-Agent",
		"Originator",
		"Version",
		"OpenAI-Beta",
		"X-Codex-Turn-Metadata",
		"Session_ID",
		"Conversation_ID",
	} {
		require.Truef(t, isOpenAIProtectedHeaderOverrideName(name), "%s must be protected", name)
	}
	require.False(t, isOpenAIProtectedHeaderOverrideName("X-Route"))
}

func requireOpenAIAPIKeyProbeHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	require.Equal(t, DefaultOpenAICodexUserAgent, headers.Get("User-Agent"))
	require.Empty(t, headers.Get("Originator"))
}
