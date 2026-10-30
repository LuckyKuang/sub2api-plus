//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"
)

// The endpoint contract is intentionally narrower than Responses: never
// silently discard a caller's tools, continuation, media or generation knobs.
func TestCodexCLITaskRequestContract(t *testing.T) {
	valid := `{"model":"gpt-6-astra","input":"Explain threads","instructions":"Be concise","stream":true,"reasoning":{"effort":"high"}}`
	request, err := ParseCodexCLITaskRequest([]byte(valid))
	require.NoError(t, err)
	require.Equal(t, "gpt-6-astra", request.Model)
	require.Equal(t, "Explain threads", request.Input)
	require.Equal(t, "Be concise", request.Instructions)
	require.Equal(t, "high", request.ReasoningEffort)
	require.True(t, request.Stream)
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"model":"gpt-6-astra"}`, `{"model":"","input":"task"}`,
		`{"model":"gpt-6-astra","input":[]}`, `{"model":"gpt-6-astra","input":"  "}`,
		`{"model":"gpt-6-astra","input":"task","instructions":null}`,
		`{"model":"gpt-6-astra","input":"task","stream":null}`,
		`{"model":"gpt-6-astra","input":"task","previous_response_id":"resp_prior"}`,
		`{"model":"gpt-6-astra","input":"task","tools":[]}`,
		`{"model":"gpt-6-astra","input":"task","temperature":0.1}`,
		`{"model":"gpt-6-astra","input":"task","reasoning":{"effort":"high","summary":"auto"}}`,
		`{"model":"gpt-6-astra","input":"task","reasoning":{"effort":"future"}}`,
		`{"model":"--model","input":"task"}`, `{"model":"gpt model","input":"task"}`,
		`{"model":"gpt-6-astra","input":"first","input":"second"}`,
		`{"model":"gpt-6-astra","input":"task","reasoning":{"effort":"low","effort":"high"}}`,
	} {
		_, err := ParseCodexCLITaskRequest([]byte(body))
		require.Error(t, err, body)
	}
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh"} {
		body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "task", "reasoning": map[string]string{"effort": effort}})
		require.NoError(t, err)
		request, err := ParseCodexCLITaskRequest(body)
		require.NoError(t, err)
		require.Equal(t, effort, request.ReasoningEffort)
	}
}

func TestCodexCLITaskRequestLimitsApplyBeforeExecution(t *testing.T) {
	for _, scenario := range []struct {
		model, input, instructions string
		allowed                    bool
	}{
		{strings.Repeat("a", 128), "task", "", true},
		{strings.Repeat("a", 129), "task", "", false},
		{"gpt-6-astra", strings.Repeat("a", 1<<20), "", true},
		{"gpt-6-astra", strings.Repeat("a", 1<<20), "b", false},
		{"gpt-6-astra", "a", strings.Repeat("b", 1<<20), false},
	} {
		body, err := json.Marshal(map[string]any{"model": scenario.model, "input": scenario.input, "instructions": scenario.instructions})
		require.NoError(t, err)
		_, err = ParseCodexCLITaskRequest(body)
		if scenario.allowed {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

// The selected trusted identity and per-request parameters own the provider
// overlay. Saved account sandbox settings remain effective; noninteractive
// approvals are validated as never before the configuration reaches execution.
// The user's Full Access default also applies to partial config edits. Native
// CLI sandbox defaults must not silently replace the project's requested mode.
func TestCodexCLITaskDefaultPermissionsSurvivePartialAccountConfig(t *testing.T) {
	for _, test := range []struct{ name, config, sandbox string }{
		{"reasoning only", "model_reasoning_effort = \"high\"\n", "danger-full-access"},
		{"table only", "[shell_environment_policy]\ninherit = \"none\"\n", "danger-full-access"},
		{"explicit read only", "sandbox_mode = \"read-only\"\n", "read-only"},
		{"explicit workspace write", "sandbox_mode = \"workspace-write\"\n", "workspace-write"},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := NormalizeCodexCLIConfigTOML(test.config)
			require.NoError(t, err)
			encoded, _, err := codexCLITaskConfig(normalized, CodexCLITaskRequest{}, http.Header{})
			require.NoError(t, err)
			var execution map[string]any
			require.NoError(t, toml.Unmarshal(encoded, &execution))
			require.Equal(t, test.sandbox, execution["sandbox_mode"])
		})
	}
}

func TestCodexCLITaskManagedOverlayPreservesPoliciesAndIdentity(t *testing.T) {
	headers := http.Header{}
	headers.Set("User-Agent", "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color")
	headers.Set("Originator", "codex_cli_rs")
	headers.Set("Version", "0.165.0")
	headers.Set("Chatgpt-Account-Id", "workspace-one")
	text := "sandbox_mode = \"read-only\"\napproval_policy = \"never\"\ndeveloper_instructions = \"stale account instructions\"\ncheck_for_update_on_startup = true\nmodel_reasoning_effort = \"low\"\n"
	normalized, err := NormalizeCodexCLIConfigTOML(text)
	require.NoError(t, err)
	configBytes, _, err := codexCLITaskConfig(normalized, CodexCLITaskRequest{Instructions: "request instructions", ReasoningEffort: "high"}, headers)
	require.NoError(t, err)
	var config map[string]any
	require.NoError(t, toml.Unmarshal(configBytes, &config))
	require.Equal(t, "read-only", config["sandbox_mode"])
	require.Equal(t, "never", config["approval_policy"])
	require.Equal(t, "request instructions", config["developer_instructions"])
	require.Equal(t, "file", config["cli_auth_credentials_store"])
	require.Equal(t, false, config["check_for_update_on_startup"])
	require.Equal(t, "sub2api_managed_codex", config["model_provider"])
	require.Equal(t, "high", config["model_reasoning_effort"])
	require.Equal(t, "disabled", config["web_search"])
	require.Equal(t, map[string]any{"enabled": false}, config["analytics"])
	require.NotContains(t, config, "profile")
	require.NotContains(t, config, "profiles")
	provider := config["model_providers"].(map[string]any)["sub2api_managed_codex"].(map[string]any)
	require.Equal(t, "https://chatgpt.com/backend-api/codex", provider["base_url"])
	require.Equal(t, "SUB2API_CODEX_ACCESS_TOKEN", provider["env_key"])
	require.Equal(t, false, provider["requires_openai_auth"])
	require.Equal(t, false, provider["supports_websockets"])
	require.EqualValues(t, 0, provider["request_max_retries"])
	require.EqualValues(t, 0, provider["stream_max_retries"])
	trusted := provider["http_headers"].(map[string]any)
	require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", trusted["User-Agent"])
	require.Equal(t, "codex_cli_rs", trusted["Originator"])
	require.Equal(t, "0.165.0", trusted["Version"])
	require.Equal(t, "workspace-one", trusted["Chatgpt-Account-Id"])
	require.NotContains(t, string(configBytes), "private-access")
}

func TestCodexCLITaskCannotReenableAuxiliaryNetworkFromSavedConfiguration(t *testing.T) {
	// rust-v0.165.0 config.schema.json defines FeatureToggleToml as an
	// object with an enabled boolean. cloud.skills does not accept a bool.
	text := "sandbox_mode = \"danger-full-access\"\napproval_policy = \"never\"\n[features]\nplugins = true\nremote_plugin = true\nplugin_sharing = true\napps = true\ntool_suggest = true\nrecommended_plugins = true\nshell_tool = true\n[cloud.skills]\nenabled = true\n"
	config, _, err := codexCLITaskConfig(text, CodexCLITaskRequest{}, http.Header{})
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, toml.Unmarshal(config, &parsed))
	features := parsed["features"].(map[string]any)
	for _, key := range []string{"plugins", "remote_plugin", "plugin_sharing", "apps", "tool_suggest", "recommended_plugins"} {
		require.Equal(t, false, features[key], "saved settings cannot reactivate independent network paths")
	}
	require.Equal(t, true, features["shell_tool"], "local task tools remain editable")
	require.Equal(t, map[string]any{"enabled": false}, parsed["cloud"].(map[string]any)["skills"])
}

func TestCodexCLITaskUsesSavedReasoningWithoutRequestOverride(t *testing.T) {
	normalized, err := NormalizeCodexCLIConfigTOML("model_reasoning_effort = \"high\"\nsandbox_mode = \"workspace-write\"\napproval_policy = \"never\"\n")
	require.NoError(t, err)
	config, _, err := codexCLITaskConfig(normalized, CodexCLITaskRequest{}, http.Header{})
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, toml.Unmarshal(config, &parsed))
	require.Equal(t, "high", parsed["model_reasoning_effort"])
	require.Equal(t, "workspace-write", parsed["sandbox_mode"])
	require.Equal(t, "never", parsed["approval_policy"])
}

func TestCodexCLITaskHomesArePrivateAndNeverShared(t *testing.T) {
	root := t.TempDir()
	runs := make(map[string]bool)
	for _, owner := range []int64{11, 11, 22} {
		run, home, workspace, err := materializeCodexCLITask(root, owner, []byte(`{"tokens":{"access_token":"private-access"}}`), []byte("sandbox_mode = \"read-only\"\n"))
		require.NoError(t, err)
		require.False(t, runs[run], "separate requests must not reuse auth or tool state")
		runs[run] = true
		require.Equal(t, run, filepath.Dir(home))
		require.Equal(t, run, filepath.Dir(workspace))
		for _, path := range []string{run, home, workspace} {
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0700), info.Mode().Perm())
		}
		for _, name := range []string{"auth.json", "config.toml"} {
			info, err := os.Stat(filepath.Join(home, name))
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0600), info.Mode().Perm())
		}
		auth, err := os.ReadFile(filepath.Join(home, "auth.json"))
		require.NoError(t, err)
		require.Equal(t, `{"tokens":{"access_token":"private-access"}}`, string(auth))
		require.NoError(t, os.RemoveAll(run))
		_, err = os.Stat(run)
		require.True(t, os.IsNotExist(err))
	}
}

func TestCodexCLITaskMaterializationRejectsSymlinkAndInvalidOwner(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "codex")))
	_, _, _, err := materializeCodexCLITask(root, 11, []byte("private-secret"), []byte("config"))
	require.Error(t, err)
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	require.Empty(t, entries, "an unsafe path cannot receive auth files")
	_, _, _, err = materializeCodexCLITask(t.TempDir(), 0, []byte("private-secret"), []byte("config"))
	require.Error(t, err)
}

// Exec reports aggregate native usage. Preserve input/cache/output exactly;
// never add cached tokens a second time, invent a token clock or bill as final
// complete after an incomplete execution.
func TestCodexCLITaskResponseUsageAndIncompleteBillingProjection(t *testing.T) {
	task := codexcli.TaskResult{Output: "final answer", UsageIncomplete: true, Usage: codexcli.TaskUsage{InputTokens: 100, CachedInputTokens: 30, OutputTokens: 20, ReasoningOutputTokens: 5}}
	request := CodexCLITaskRequest{Model: "client-alias", Stream: true}
	result := codexCLITaskForwardResult(task, request, "gpt-6-astra", "resp_fixture", 2*time.Second)
	require.Equal(t, "client-alias", result.Model)
	require.Equal(t, "gpt-6-astra", result.UpstreamModel)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 30, result.Usage.CacheReadInputTokens)
	require.Equal(t, 20, result.Usage.OutputTokens)
	require.True(t, result.UsageIncomplete)
	require.Nil(t, result.FirstTokenMs)
	response := codexCLITaskResponse(task, "client-alias", "resp_fixture")
	usage := response["usage"].(gin.H)
	require.EqualValues(t, 120, usage["total_tokens"])
	require.EqualValues(t, 30, usage["input_tokens_details"].(gin.H)["cached_tokens"])
	require.EqualValues(t, 5, usage["output_tokens_details"].(gin.H)["reasoning_tokens"])
	require.Equal(t, "client-alias", response["model"])
}

func TestCodexCLITaskErrorsRespectCommittedSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, committed := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/codex-cli/responses", nil)
		if committed {
			c.Header("Content-Type", "text/event-stream")
			_, err := c.Writer.WriteString(": keepalive\n\n")
			require.NoError(t, err)
			c.Writer.Flush()
		}
		result, err := (&OpenAIGatewayService{}).forwardCodexCLITask(context.Background(), c, nil, []byte(`{"model":"gpt-6-astra","input":"task","tools":[]}`))
		require.Error(t, err)
		require.Nil(t, result)
		if committed {
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Contains(t, recorder.Body.String(), "data: ")
			require.NotContains(t, recorder.Body.String(), ": keepalive\n\n{", "a committed SSE response cannot acquire a bare JSON suffix")
		} else {
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.True(t, json.Valid(recorder.Body.Bytes()))
		}
		require.Contains(t, recorder.Body.String(), "unsupported_task_request")
	}
}

type codexCLIExecutionRuntime struct {
	available bool
	run       func(context.Context, codexcli.TaskRequest) (codexcli.TaskResult, error)
}

func (r codexCLIExecutionRuntime) Status(context.Context) codexcli.Status {
	return codexcli.Status{ExecutionAvailable: r.available, SourceVerified: r.available, Source: "managed", VersionMatch: r.available, Mode: "exec_task", TargetVersion: "0.165.0"}
}

func (r codexCLIExecutionRuntime) RunTask(ctx context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
	return r.run(ctx, request)
}

func enabledCodexCLIExecutionAccount(id int64, access, workspace string) *Account {
	account := codexCLIAccountFixture(id)
	account.Credentials["access_token"] = access
	account.Credentials["chatgpt_account_id"] = workspace
	account.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": true, "config_toml": "sandbox_mode = \"read-only\"\napproval_policy = \"never\"\n"}
	return account
}

func newCodexCLIExecutionContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/codex-cli/responses", nil)
	c.Request.Header.Set("User-Agent", "untrusted-client/9")
	return c, recorder
}

// Two admitted requests can overlap. Each CLI invocation must receive only its
// own account's auth, static bearer, workspace, policy and proxy, and cleanup
// must remove both run directories after completion. HTTP fallback is forbidden.
func TestCodexCLITaskConcurrentAccountsKeepCredentialsAndFilesIndependent(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	gin.SetMode(gin.TestMode)
	accounts := []*Account{
		enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one"),
		enabledCodexCLIExecutionAccount(22, "owner-two-access", "workspace-two"),
	}
	proxyID := int64(101)
	accounts[0].Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color"
	accounts[0].ProxyID = &proxyID
	accounts[0].Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "proxy-one.invalid", Port: 8080, Status: StatusActive}
	type executionSnapshot struct {
		request      codexcli.TaskRequest
		auth, config []byte
		err          error
	}
	snapshots := make(chan executionSnapshot, 2)
	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
		snapshot := executionSnapshot{request: request}
		snapshot.auth, snapshot.err = os.ReadFile(filepath.Join(request.Home, "auth.json"))
		if snapshot.err == nil {
			snapshot.config, snapshot.err = os.ReadFile(filepath.Join(request.Home, "config.toml"))
		}
		snapshots <- snapshot
		<-release
		return codexcli.TaskResult{Started: true, Output: "completed independently", Usage: codexcli.TaskUsage{InputTokens: 10, OutputTokens: 2}}, snapshot.err
	}}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &codexModelsHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		panic("a CLI task must never use direct HTTP fallback")
	}}}
	type outcome struct {
		result   *OpenAIForwardResult
		err      error
		recorder *httptest.ResponseRecorder
	}
	outcomes := make(chan outcome, 2)
	for _, account := range accounts {
		go func(account *Account) {
			c, recorder := newCodexCLIExecutionContext()
			ctx := WithOutboundIdentityScope(context.Background(), c)
			result, err := gateway.forwardCodexCLITaskWithRuntime(ctx, c, account, []byte(`{"model":"gpt-6-astra","input":"do one task"}`), runtime)
			outcomes <- outcome{result, err, recorder}
		}(account)
	}
	var homes []string
	for i := 0; i < 2; i++ {
		var snapshot executionSnapshot
		select {
		case snapshot = <-snapshots:
		case <-time.After(5 * time.Second):
			t.Fatal("independent tasks did not both reach CLI execution")
		}
		require.NoError(t, snapshot.err)
		homes = append(homes, snapshot.request.Home)
		var auth struct {
			Access string `json:"OPENAI_API_KEY"`
		}
		require.NoError(t, json.Unmarshal(snapshot.auth, &auth))
		var expectedToken, expectedWorkspace, foreignToken string
		if strings.Contains(snapshot.request.Home, string(filepath.Separator)+"11"+string(filepath.Separator)) {
			expectedToken, expectedWorkspace, foreignToken = "owner-one-access", "workspace-one", "owner-two-access"
			require.Equal(t, "http://proxy-one.invalid:8080", snapshot.request.ExtraEnv["HTTPS_PROXY"])
		} else {
			expectedToken, expectedWorkspace, foreignToken = "owner-two-access", "workspace-two", "owner-one-access"
			require.Empty(t, snapshot.request.ExtraEnv["HTTPS_PROXY"])
		}
		require.Equal(t, expectedToken, auth.Access)
		require.NotContains(t, string(snapshot.auth), "refresh_token")
		require.NotContains(t, string(snapshot.auth), "id_token")
		require.Equal(t, expectedToken, snapshot.request.ExtraEnv["SUB2API_CODEX_ACCESS_TOKEN"])
		require.NotContains(t, string(snapshot.auth), foreignToken)
		require.NotContains(t, string(snapshot.config), expectedToken)
		var parsed map[string]any
		require.NoError(t, toml.Unmarshal(snapshot.config, &parsed))
		require.Equal(t, "read-only", parsed["sandbox_mode"])
		provider := parsed["model_providers"].(map[string]any)["sub2api_managed_codex"].(map[string]any)
		require.Equal(t, expectedWorkspace, provider["http_headers"].(map[string]any)["Chatgpt-Account-Id"])
		if expectedWorkspace == "workspace-one" {
			require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", provider["http_headers"].(map[string]any)["User-Agent"])
		} else {
			require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", provider["http_headers"].(map[string]any)["User-Agent"])
		}
		require.NotContains(t, string(snapshot.config), "untrusted-client")
	}
	require.NotEqual(t, homes[0], homes[1])
	close(release)
	released = true
	for i := 0; i < 2; i++ {
		var completed outcome
		select {
		case completed = <-outcomes:
		case <-time.After(5 * time.Second):
			t.Fatal("CLI task completion did not release its run")
		}
		require.NoError(t, completed.err)
		require.NotNil(t, completed.result)
		require.Equal(t, http.StatusOK, completed.recorder.Code)
		require.NotContains(t, completed.recorder.Body.String(), "owner-")
	}
	for _, home := range homes {
		_, err := os.Stat(filepath.Dir(home))
		require.True(t, os.IsNotExist(err), "task exit must remove auth, config and tool workspace")
	}
}

func TestCodexCLITaskExecutionFailurePreservesUsageWithoutReplay(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
	c, recorder := newCodexCLIExecutionContext()
	calls, home := 0, ""
	runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
		calls++
		home = request.Home
		return codexcli.TaskResult{Started: true, Output: "private partial model output", Usage: codexcli.TaskUsage{InputTokens: 100, CachedInputTokens: 30, OutputTokens: 20}}, &codexcli.TaskError{Code: "upstream_error"}
	}}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &codexModelsHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		panic("side-effectful tasks cannot acquire a direct HTTP fallback")
	}}}
	ctx := WithOutboundIdentityScope(context.Background(), c)
	result, err := gateway.forwardCodexCLITaskWithRuntime(ctx, c, account, []byte(`{"model":"gpt-6-astra","input":"one potentially side-effectful task"}`), runtime)
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover), "execution failure cannot authorize account replay")
	require.Equal(t, 1, calls)
	require.NotNil(t, result, "reported usage must reach the normal billing submission on error")
	require.True(t, result.UsageIncomplete)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 30, result.Usage.CacheReadInputTokens)
	require.Equal(t, 20, result.Usage.OutputTokens)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "cli_task_failed")
	require.NotContains(t, recorder.Body.String(), "private partial")
	require.NotContains(t, recorder.Body.String(), "owner-one-access")
	_, err = os.Stat(filepath.Dir(home))
	require.True(t, os.IsNotExist(err), "failure must still remove the run's credential files")
}

func TestCodexCLITaskUnavailableRuntimePrecedesCredentialLookup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA_DIR", root)
	account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
	c, recorder := newCodexCLIExecutionContext()
	cache := &countTokensOwnerCache{}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, openAITokenProvider: NewOpenAITokenProvider(nil, cache, nil)}
	runtime := codexCLIExecutionRuntime{available: false, run: func(context.Context, codexcli.TaskRequest) (codexcli.TaskResult, error) {
		panic("unavailable runtime cannot start a task")
	}}
	result, err := gateway.forwardCodexCLITaskWithRuntime(context.Background(), c, account, []byte(`{"model":"gpt-6-astra","input":"task"}`), runtime)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Zero(t, cache.reads)
	require.Zero(t, cache.writes)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Empty(t, entries, "runtime failure cannot leave credential or tool files")
}

func TestCodexCLITaskClientCancellationPreservesPartialUsageAndCleansHome(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
	c, _ := newCodexCLIExecutionContext()
	ctx, cancel := context.WithCancel(WithOutboundIdentityScope(context.Background(), c))
	defer cancel()
	home := ""
	runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
		home = request.Home
		cancel()
		return codexcli.TaskResult{Started: true, Usage: codexcli.TaskUsage{InputTokens: 10, OutputTokens: 2}}, context.Canceled
	}}
	result, err := (&OpenAIGatewayService{cfg: &config.Config{}}).forwardCodexCLITaskWithRuntime(ctx, c, account, []byte(`{"model":"gpt-6-astra","input":"task"}`), runtime)
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.True(t, result.UsageIncomplete)
	require.Equal(t, 10, result.Usage.InputTokens)
	_, err = os.Stat(filepath.Dir(home))
	require.True(t, os.IsNotExist(err))
}

func TestCodexCLITaskStreamingEmitsOnlyCompletedResponse(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
	c, recorder := newCodexCLIExecutionContext()
	runtime := codexCLIExecutionRuntime{available: true, run: func(context.Context, codexcli.TaskRequest) (codexcli.TaskResult, error) {
		return codexcli.TaskResult{Started: true, Output: "final task answer", Usage: codexcli.TaskUsage{InputTokens: 10, OutputTokens: 2}}, nil
	}}
	result, err := (&OpenAIGatewayService{cfg: &config.Config{}}).forwardCodexCLITaskWithRuntime(WithOutboundIdentityScope(context.Background(), c), c, account, []byte(`{"model":"gpt-6-astra","input":"task","stream":true}`), runtime)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/event-stream")
	require.Equal(t, 1, strings.Count(recorder.Body.String(), "event: response.completed\n"))
	require.Contains(t, recorder.Body.String(), "data: [DONE]\n\n")
	require.Contains(t, recorder.Body.String(), "final task answer")
	require.NotContains(t, recorder.Body.String(), "output_text.delta")
	require.NotContains(t, recorder.Body.String(), "thread.started")
}

// The existing pricing contract applies reasoning multipliers to the actual
// forwarded effort. Account defaults and explicit request overrides must reach
// the same billing boundary, including reported usage from failed tasks.
func TestCodexCLITaskBillingUsesEffectiveConfiguredReasoning(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	for _, test := range []struct {
		name, saved, requested, expected string
		failed                           bool
		cost                             float64
	}{
		{name: "saved high", saved: "high", expected: "high", cost: 2},
		{name: "request overrides saved high", saved: "high", requested: "medium", expected: "medium", cost: 1.5},
		{name: "request high", requested: "high", expected: "high", cost: 2},
		{name: "request max", requested: "max", expected: "max", cost: 3},
		{name: "unconfigured default", cost: 1},
		{name: "explicit none overrides high", saved: "high", requested: "none", expected: "none", cost: 1},
		{name: "failed task saved high", saved: "high", expected: "high", failed: true, cost: 2},
		{name: "failed task override", saved: "high", requested: "medium", expected: "medium", failed: true, cost: 1.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
			configuration := account.Credentials[CodexCLIConfigCredentialKey].(map[string]any)
			if test.saved != "" {
				configuration["config_toml"] = configuration["config_toml"].(string) + "model_reasoning_effort = \"" + test.saved + "\"\n"
			}
			body := map[string]any{"model": "gpt-6-astra", "input": "independent task"}
			if test.requested != "" {
				body["reasoning"] = map[string]string{"effort": test.requested}
			}
			encoded, err := json.Marshal(body)
			require.NoError(t, err)
			runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
				encodedConfig, err := os.ReadFile(filepath.Join(request.Home, "config.toml"))
				require.NoError(t, err)
				var executionConfig map[string]any
				require.NoError(t, toml.Unmarshal(encodedConfig, &executionConfig))
				if test.expected == "" {
					require.NotContains(t, executionConfig, "model_reasoning_effort", "an unspecified CLI default must not become an invented billing value")
				} else {
					require.Equal(t, test.expected, executionConfig["model_reasoning_effort"])
				}
				result := codexcli.TaskResult{Started: true, Output: "answer", Usage: codexcli.TaskUsage{InputTokens: 1_000_000, OutputTokens: 20}}
				if test.failed {
					return result, &codexcli.TaskError{Code: "upstream_error"}
				}
				return result, nil
			}}
			cfg := rawChatCompletionsTestConfig()
			billing := NewBillingService(cfg, nil)
			gateway := &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
			c, recorder := newCodexCLIExecutionContext()
			result, err := gateway.forwardCodexCLITaskWithRuntime(WithOutboundIdentityScope(context.Background(), c), c, account, encoded, runtime)
			if test.failed {
				require.Error(t, err)
				require.Equal(t, http.StatusBadGateway, recorder.Code)
				require.True(t, result.UsageIncomplete)
			} else {
				require.NoError(t, err)
				require.Equal(t, http.StatusOK, recorder.Code)
			}
			require.NotNil(t, result)
			require.Equal(t, test.expected, optionalStringValue(result.ReasoningEffort))
			group := &Group{ID: 1, Platform: PlatformOpenAI, ModelPricing: []ChannelModelPricing{{
				Models: []string{"gpt-6-astra"}, BillingMode: BillingModeToken,
				InputPrice: testPtrFloat64(1e-6), OutputPrice: testPtrFloat64(0),
				ReasoningEffortMultipliers: map[string]float64{"medium": 1.5, "high": 2, "max": 3},
			}}}
			cost, err := gateway.calculateOpenAIRecordUsageCost(context.Background(), result, &APIKey{GroupID: &group.ID, Group: group}, []string{"gpt-6-astra"}, 1, 1, 1, 1,
				UsageTokens{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens}, "", nil, false, time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			require.InDelta(t, test.cost, cost.TotalCost, 1e-12, "1M input at $1/M uses the explicitly configured effective reasoning multiplier")
			require.InDelta(t, test.cost, cost.ActualCost, 1e-12)
		})
	}
}

// Existing group mappings can emit max. rust-v0.165.0's official
// protocol/src/openai_models.rs ReasoningEffort supports that exact value.
func TestCodexCLITaskAcceptsMaxFromExistingGroupReasoningMapping(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	body, changed, err := ApplyOpenAIReasoningEffortPolicy([]byte(`{"model":"gpt-6-astra","input":"task","reasoning":{"effort":"high"}}`), "", []ReasoningEffortMapping{{From: "high", To: "max"}}, "")
	require.NoError(t, err)
	require.True(t, changed)
	account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
	runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
		encodedConfig, err := os.ReadFile(filepath.Join(request.Home, "config.toml"))
		require.NoError(t, err)
		var executionConfig map[string]any
		require.NoError(t, toml.Unmarshal(encodedConfig, &executionConfig))
		require.Equal(t, "max", executionConfig["model_reasoning_effort"])
		return codexcli.TaskResult{Started: true, Output: "mapped answer"}, nil
	}}
	c, recorder := newCodexCLIExecutionContext()
	result, err := (&OpenAIGatewayService{cfg: &config.Config{}}).forwardCodexCLITaskWithRuntime(WithOutboundIdentityScope(context.Background(), c), c, account, body, runtime)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "max", optionalStringValue(result.ReasoningEffort))
}

// Paid service tiers are not observable in exec JSONL and can be filtered by
// the CLI's model catalog. Standard tasks must obey existing Fast policies and
// reject required paid execution before credentials, files or process startup.
func TestCodexCLITaskStandardTierHonorsFastPolicyBeforeSideEffects(t *testing.T) {
	for _, test := range []struct {
		name, tier, action, whitelist string
		forceGroup, rejected          bool
	}{
		{name: "standard success"},
		{name: "group force rejected", forceGroup: true, rejected: true},
		{name: "group force pass rejected", forceGroup: true, tier: OpenAIFastTierPriority, action: BetaPolicyActionPass, rejected: true},
		{name: "group force filter allows standard", forceGroup: true, tier: OpenAIFastTierPriority, action: BetaPolicyActionFilter},
		{name: "group force block rejected", forceGroup: true, tier: OpenAIFastTierPriority, action: BetaPolicyActionBlock, rejected: true},
		{name: "global missing force rejected", tier: OpenAIFastTierMissing, action: OpenAIFastPolicyActionForcePriority, rejected: true},
		{name: "global any force leaves missing standard", tier: OpenAIFastTierAny, action: OpenAIFastPolicyActionForcePriority},
		{name: "global priority block leaves standard", tier: OpenAIFastTierPriority, action: BetaPolicyActionBlock},
		{name: "missing force for another model leaves standard", tier: OpenAIFastTierMissing, action: OpenAIFastPolicyActionForcePriority, whitelist: "other-model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("DATA_DIR", root)
			account := enabledCodexCLIExecutionAccount(11, "owner-one-access", "workspace-one")
			c, recorder := newCodexCLIExecutionContext()
			ctx := WithOutboundIdentityScope(context.Background(), c)
			ctx = context.WithValue(ctx, ctxkey.Group, &Group{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, Hydrated: true, ForceOpenAIFast: test.forceGroup})
			settings := &OpenAIFastPolicySettings{}
			if test.action != "" {
				rule := OpenAIFastPolicyRule{ServiceTier: test.tier, Action: test.action, Scope: BetaPolicyScopeAll}
				if test.whitelist != "" {
					rule.ModelWhitelist = []string{test.whitelist}
					rule.FallbackAction = BetaPolicyActionPass
				}
				settings.Rules = []OpenAIFastPolicyRule{rule}
			}
			ctx = withOpenAIFastPolicyContext(ctx, settings)
			cache := &countTokensOwnerCache{}
			gateway := &OpenAIGatewayService{cfg: &config.Config{}}
			if test.rejected {
				gateway.openAITokenProvider = NewOpenAITokenProvider(nil, cache, nil)
			}
			calls := 0
			runtime := codexCLIExecutionRuntime{available: true, run: func(_ context.Context, request codexcli.TaskRequest) (codexcli.TaskResult, error) {
				calls++
				encodedConfig, err := os.ReadFile(filepath.Join(request.Home, "config.toml"))
				require.NoError(t, err)
				var executionConfig map[string]any
				require.NoError(t, toml.Unmarshal(encodedConfig, &executionConfig))
				require.Equal(t, "default", executionConfig["service_tier"], "the native default sentinel prevents model-catalog paid defaults")
				return codexcli.TaskResult{Started: true, Output: "standard answer", Usage: codexcli.TaskUsage{InputTokens: 10, OutputTokens: 2}}, nil
			}}
			result, err := gateway.forwardCodexCLITaskWithRuntime(ctx, c, account, []byte(`{"model":"gpt-6-astra","input":"standard task"}`), runtime)
			if test.rejected {
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusForbidden, recorder.Code)
				require.Contains(t, recorder.Body.String(), "cli_service_tier_policy_denied")
				require.Zero(t, cache.reads, "a denied policy cannot acquire credentials")
				require.Zero(t, cache.writes)
				require.Zero(t, calls)
				entries, err := os.ReadDir(root)
				require.NoError(t, err)
				require.Empty(t, entries, "a denied policy cannot materialize auth/config/workspace")
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Equal(t, 1, calls)
				require.Nil(t, result.ServiceTier, "standard execution must not acquire paid-tier billing")
			}
		})
	}
}
