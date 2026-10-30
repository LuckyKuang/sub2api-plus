//go:build unit

package service

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

// These expected wire triples are literal business fixtures. Native CLI OS,
// caller headers and an invalid higher-priority candidate cannot choose them.
func TestCodexCLITaskIdentitySourcePriorityAtExecution(t *testing.T) {
	const accountUA = "codex_cli_rs/0.150.0 (Ubuntu 24.04; x86_64) screen-256color"
	const globalUA = "codex-tui/0.151.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.151.0)"
	for _, scenario := range []struct{ name, accountUA, globalUA, userAgent, originator string }{
		{"account wins", accountUA, globalUA, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", "codex_cli_rs"},
		{"invalid global cannot displace account", accountUA, "bad-global", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", "codex_cli_rs"},
		{"empty account uses global", "", globalUA, "codex-tui/0.165.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.165.0)", "codex-tui"},
		{"invalid account uses global", "\r\ncodex_cli_rs/0.165.0", globalUA, "codex-tui/0.165.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.165.0)", "codex-tui"},
		{"empty uses exact default", "", "", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs"},
		{"invalid uses exact default", "untrusted/1", "untrusted/2", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_cli_rs"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("DATA_DIR", t.TempDir())
			account := enabledCodexCLIExecutionAccount(81, "owner-access", "owner-workspace")
			account.Credentials["user_agent"] = scenario.accountUA
			settings := &SettingService{settingRepo: &agentIdentitySettingRepoStub{values: map[string]string{
				SettingKeyCodexCLIBridgeEnabled:    "true",
				SettingKeyOpenAICodexUserAgent:     scenario.globalUA,
				SettingKeyOpenAICodexClientVersion: "0.165.0",
			}}}
			gateway := &OpenAIGatewayService{cfg: &config.Config{}, settingService: settings}
			called := 0
			runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
				called++
				contents, err := os.ReadFile(filepath.Join(request.Home, "config.toml"))
				require.NoError(t, err)
				var config map[string]any
				require.NoError(t, toml.Unmarshal(contents, &config))
				provider := config["model_providers"].(map[string]any)["sub2api_managed_codex"].(map[string]any)
				headers := provider["http_headers"].(map[string]any)
				require.Equal(t, scenario.userAgent, headers["User-Agent"])
				require.Equal(t, scenario.originator, headers["Originator"])
				require.Equal(t, "0.165.0", headers["Version"])
				require.Equal(t, "owner-workspace", headers["Chatgpt-Account-Id"])
				return codexcli.TaskResult{Started: true, Output: "done", Usage: codexcli.TaskUsage{InputTokens: 1, OutputTokens: 1}}, nil
			}}
			c, response := newCodexCLIExecutionContext()
			c.Request.Header.Set("Originator", "caller-forged")
			c.Request.Header.Set("Version", "9.9.9")
			ctx := WithOutboundIdentityScope(context.Background(), c)
			_, err := gateway.forwardCodexCLITaskWithRuntime(ctx, c, account, []byte(`{"model":"gpt-6-astra","input":"task"}`), runtime)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, 1, called)
		})
	}
}
