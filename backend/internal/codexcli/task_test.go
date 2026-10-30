//go:build unit

package codexcli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const taskCompleted = `{"type":"turn.completed","usage":{"input_tokens":25,"cached_input_tokens":5,"cache_write_input_tokens":7,"output_tokens":12,"reasoning_output_tokens":4}}`
const taskFinal = `{"type":"item.completed","item":{"id":"message_1","type":"agent_message","text":"final answer"}}`

func TestTaskLimitsMatchDocumentedContract(t *testing.T) {
	if MaxTaskPromptBytes != 1048576 || MaxTaskOutputBytes != 8388608 || MaxTaskLineBytes != 2097152 || MaxTaskStderrBytes != 262144 {
		t.Fatal("task limits differ from the reviewed protocol contract")
	}
}

func shellLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func emitTaskEvents(events ...string) string {
	var result strings.Builder
	for _, event := range events {
		result.WriteString("printf '%s\\n' " + shellLiteral(event) + "\n")
	}
	return result.String()
}

func taskFixture(t *testing.T, body string) TaskRequest {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("CLI task process groups support Linux and macOS")
	}
	dir := t.TempDir()
	home, workspace := filepath.Join(dir, "home"), filepath.Join(dir, "workspace")
	for _, path := range []string{home, workspace} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(dir, "official cli fixture")
	executableContent := []byte("#!/bin/sh\nset -eu\n" + body)
	if err := os.WriteFile(executable, executableContent, 0700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(executableContent)
	return TaskRequest{
		Runtime: Status{Path: executable, Installed: true, Source: "managed", SourceVerified: true, InstalledVersion: "0.165.0", VersionMatch: true, ExecutionAvailable: true, Mode: "exec_task", proof: &runtimeProof{path: executable, executableSHA256: digest[:]}},
		Home:    home, WorkDir: workspace, Prompt: "user prompt",
	}
}

func assertTaskCode(t *testing.T, err error, expected string) {
	t.Helper()
	var taskErr *TaskError
	if !errors.As(err, &taskErr) || taskErr.Code != expected {
		t.Fatalf("task error = %v; want code %s", err, expected)
	}
}

func TestRunTaskUsesExecJSONLStdinAndLastAgentMessage(t *testing.T) {
	prompt := "prompt with spaces, $(not-a-shell-command), and 'quotes'"
	// The fixed CLI's project_root_markers=[] bounds configuration discovery
	// to the new private workspace; this must be a command-line override.
	body := `[ "$#" = 9 ] && [ "$1" = exec ] && [ "$2" = --json ] && [ "$3" = --ephemeral ] && [ "$4" = --skip-git-repo-check ] && [ "$5" = -c ] && [ "$6" = 'project_root_markers=[]' ] && [ "$7" = --model ] && [ "$8" = gpt-6.1-sol ] && [ "$9" = - ] || exit 90
prompt=$(cat)
[ "$prompt" = ` + shellLiteral(prompt) + ` ] || exit 91
` + emitTaskEvents(
		`{"type":"thread.started","thread_id":"thread-123"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"type":"command_execution","command":"a tool"}}`,
		`{"type":"item.completed","item":{"type":"reasoning","text":"private reasoning"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"intermediate message"}}`,
		`{"type":"future.event","content":{"ignored":true}}`,
		taskFinal, taskCompleted,
	)
	request := taskFixture(t, body)
	request.Prompt, request.Model = prompt, "gpt-6.1-sol"
	result, err := RunTask(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Started || result.UsageIncomplete || result.Output != "final answer" || result.ThreadID != "thread-123" {
		t.Fatalf("wrong task result: %+v", result)
	}
	expected := TaskUsage{InputTokens: 25, CachedInputTokens: 5, CacheWriteInputTokens: 7, OutputTokens: 12, ReasoningOutputTokens: 4}
	if result.Usage != expected {
		t.Fatalf("usage = %+v, want %+v", result.Usage, expected)
	}
}

func TestRunTaskFailureDoesNotRetryOrExposeProviderSecrets(t *testing.T) {
	secret := "oauth-super-secret-token-user-content"
	for _, tc := range []struct {
		name, body, code string
		usageIncomplete  bool
	}{
		{"turn_failed", emitTaskEvents(`{"type":"turn.failed","error":{"message":"` + secret + `"}}`), "turn_failed", true},
		{"upstream_error", emitTaskEvents(`{"type":"error","message":"` + secret + `"}`), "upstream_error", true},
		{"process_failed", emitTaskEvents(taskFinal, taskCompleted) + "exit 7\n", "process_failed", false},
		{"no_final", emitTaskEvents(taskCompleted), "final_output_missing", false},
		{"empty_final", emitTaskEvents(`{"type":"item.completed","item":{"type":"agent_message","text":"  "}}`, taskCompleted), "final_output_missing", false},
		{"turn_incomplete", emitTaskEvents(taskFinal), "turn_incomplete", true},
		{"invalid_json", emitTaskEvents(secret), "invalid_event_stream", true},
		{"missing_type", emitTaskEvents(`{"message":"` + secret + `"}`), "invalid_event_stream", true},
		{"missing_usage", emitTaskEvents(taskFinal, `{"type":"turn.completed"}`), "invalid_usage", true},
		{"missing_counter", emitTaskEvents(taskFinal, `{"type":"turn.completed","usage":{"input_tokens":25,"cached_input_tokens":0}}`), "invalid_usage", true},
		{"negative_usage", emitTaskEvents(taskFinal, `{"type":"turn.completed","usage":{"input_tokens":-1,"cached_input_tokens":0,"output_tokens":2}}`), "invalid_usage", true},
		{"cached_exceeds_input", emitTaskEvents(taskFinal, `{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":2,"output_tokens":2}}`), "invalid_usage", true},
		{"duplicate_completion", emitTaskEvents(taskFinal, taskCompleted, taskCompleted), "invalid_usage", false},
		{"message_after_completion", emitTaskEvents(taskFinal, taskCompleted, taskFinal), "invalid_event_stream", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := taskFixture(t, "printf x >> \"$CODEX_HOME/attempts\"\nprintf '%s' "+shellLiteral(secret)+" >&2\n"+tc.body)
			request.ExtraEnv = map[string]string{"SUB2API_CODEX_ACCESS_TOKEN": secret}
			result, err := RunTask(t.Context(), request)
			assertTaskCode(t, err, tc.code)
			if !result.Started || result.Output != "" || result.UsageIncomplete != tc.usageIncomplete {
				t.Fatalf("wrong failed task result: %+v", result)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), request.Home) {
				t.Fatalf("task error exposed raw provider data: %v", err)
			}
			attempts, readErr := os.ReadFile(filepath.Join(request.Home, "attempts"))
			if readErr != nil || string(attempts) != "x" {
				t.Fatalf("task was retried or never started: %q %v", attempts, readErr)
			}
		})
	}
}

func TestRunTaskAcceptsBaselineUsageWithoutOptionalCounters(t *testing.T) {
	request := taskFixture(t, emitTaskEvents(taskFinal, `{"type":"turn.completed","usage":{"input_tokens":25,"cached_input_tokens":5,"output_tokens":12}}`))
	result, err := RunTask(t.Context(), request)
	if err != nil || result.UsageIncomplete || result.Usage != (TaskUsage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 12}) {
		t.Fatalf("wrong baseline usage: %+v %v", result, err)
	}
}

func TestTaskUsageCountersRespectBillingArithmeticBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, usage string
		valid       bool
	}{
		{"largest_safe_total", `{"input_tokens":9223372036854775806,"cached_input_tokens":0,"output_tokens":1}`, true},
		{"int64_total_overflow", `{"input_tokens":9223372036854775807,"cached_input_tokens":0,"output_tokens":1}`, false},
		{"cache_subsets_fill_input", `{"input_tokens":25,"cached_input_tokens":5,"cache_write_input_tokens":20,"output_tokens":12}`, true},
		{"cache_subsets_exceed_input", `{"input_tokens":25,"cached_input_tokens":5,"cache_write_input_tokens":21,"output_tokens":12}`, false},
		{"cache_write_exceeds_input", `{"input_tokens":25,"cached_input_tokens":0,"cache_write_input_tokens":26,"output_tokens":12}`, false},
		{"cache_subsets_would_overflow", `{"input_tokens":9223372036854775807,"cached_input_tokens":9223372036854775807,"cache_write_input_tokens":1,"output_tokens":0}`, false},
		{"reasoning_subset_fills_output", `{"input_tokens":25,"cached_input_tokens":5,"output_tokens":12,"reasoning_output_tokens":12}`, true},
		{"reasoning_exceeds_output", `{"input_tokens":25,"cached_input_tokens":5,"output_tokens":12,"reasoning_output_tokens":13}`, false},
		{"negative_cache_write", `{"input_tokens":25,"cached_input_tokens":5,"output_tokens":12,"cache_write_input_tokens":-1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := TaskResult{Started: true, UsageIncomplete: true}
			stream := taskFinal + "\n" + `{"type":"turn.completed","usage":` + tc.usage + "}\n"
			err := parseTaskEvents(strings.NewReader(stream), &result)
			if tc.valid {
				if err != nil || result.UsageIncomplete {
					t.Fatalf("valid billing counters rejected: %+v %v", result, err)
				}
			} else {
				assertTaskCode(t, err, "invalid_usage")
				if !result.UsageIncomplete {
					t.Fatal("invalid counters marked complete for billing")
				}
			}
		})
	}
}

func TestRunTaskRejectsOversizedStdoutStderrAndJSONLines(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"line", fmt.Sprintf("head -c %d /dev/zero | tr '\\000' x\n", MaxTaskLineBytes+1)},
		{"total", fmt.Sprintf("yes '%s' | head -c %d\n", `{"type":"future.event"}`, MaxTaskOutputBytes+1000)},
		{"stderr", fmt.Sprintf("head -c %d /dev/zero >&2\n", MaxTaskStderrBytes+1) + emitTaskEvents(taskFinal, taskCompleted)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := taskFixture(t, tc.body)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			result, err := RunTask(ctx, request)
			assertTaskCode(t, err, "output_limit_exceeded")
			if !result.Started || result.Output != "" {
				t.Fatalf("oversized output was returned: %+v", result)
			}
		})
	}
}

func TestRunTaskEnvironmentAndWorkingDirectoryAreAccountSpecific(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "gateway-global-secret")
	t.Setenv("DATABASE_URL", "gateway-database-secret")
	t.Setenv("CODEX_HOME", "/shared/codex-home")
	t.Setenv("HTTPS_PROXY", "http://host-proxy.invalid")
	body := `printf '%s\n' "$HOME" "$CODEX_HOME" "$PWD" "$SUB2API_CODEX_ACCESS_TOKEN" "${OPENAI_API_KEY-unset}" "${DATABASE_URL-unset}" "$HTTPS_PROXY" "$PATH" "$TMPDIR" > "$CODEX_HOME/environment"
` + emitTaskEvents(taskFinal, taskCompleted)
	requests := []TaskRequest{taskFixture(t, body), taskFixture(t, body)}
	var wg sync.WaitGroup
	for index := range requests {
		request := requests[index]
		request.ExtraEnv = map[string]string{"SUB2API_CODEX_ACCESS_TOKEN": fmt.Sprintf("account-%d-token", index), "HTTPS_PROXY": fmt.Sprintf("http://account-%d.proxy", index)}
		wg.Go(func() {
			if _, err := RunTask(t.Context(), request); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for index, request := range requests {
		data, err := os.ReadFile(filepath.Join(request.Home, "environment"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		want := []string{request.Home, request.Home, request.WorkDir, fmt.Sprintf("account-%d-token", index), "unset", "unset", fmt.Sprintf("http://account-%d.proxy", index), taskPath, filepath.Join(request.Home, ".tmp")}
		if strings.Join(lines, "\n") != strings.Join(want, "\n") {
			t.Fatalf("account %d environment leaked or changed: %q", index, lines)
		}
	}
}

func TestRunTaskPreflightCannotLaunchUnverifiedOrMalformedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		mutate     func(*testing.T, *TaskRequest)
	}{
		{"unverified", "runtime_unavailable", func(_ *testing.T, r *TaskRequest) { r.Runtime.proof = nil }},
		{"different_path", "runtime_unavailable", func(_ *testing.T, r *TaskRequest) { r.Runtime.Path += ".replacement" }},
		{"unknown_source", "runtime_unavailable", func(_ *testing.T, r *TaskRequest) { r.Runtime.Source = "external" }},
		{"disabled", "runtime_unavailable", func(_ *testing.T, r *TaskRequest) { r.Runtime.ExecutionAvailable = false }},
		{"json_status", "runtime_unavailable", func(t *testing.T, r *TaskRequest) {
			data, err := json.Marshal(r.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			var status Status
			if err := json.Unmarshal(data, &status); err != nil {
				t.Fatal(err)
			}
			r.Runtime = status
		}},
		{"relative_home", "invalid_task_directory", func(_ *testing.T, r *TaskRequest) { r.Home = "relative" }},
		{"missing_workdir", "invalid_task_directory", func(_ *testing.T, r *TaskRequest) { r.WorkDir += ".missing" }},
		{"symlink_home", "invalid_task_directory", func(t *testing.T, r *TaskRequest) {
			path := r.Home + ".link"
			if err := os.Symlink(r.Home, path); err != nil {
				t.Fatal(err)
			}
			r.Home = path
		}},
		{"empty_prompt", "invalid_prompt", func(_ *testing.T, r *TaskRequest) { r.Prompt = "  " }},
		{"oversized_prompt", "invalid_prompt", func(_ *testing.T, r *TaskRequest) { r.Prompt = strings.Repeat("x", MaxTaskPromptBytes+1) }},
		{"nul_prompt", "invalid_prompt", func(_ *testing.T, r *TaskRequest) { r.Prompt = "prompt\x00value" }},
		{"invalid_model", "invalid_model", func(_ *testing.T, r *TaskRequest) { r.Model = "--config=secret" }},
		{"missing_executable", "runtime_unavailable", func(t *testing.T, r *TaskRequest) {
			if err := os.Remove(r.Runtime.Path); err != nil {
				t.Fatal(err)
			}
		}},
		{"non_executable", "process_start_failed", func(t *testing.T, r *TaskRequest) {
			if err := os.Chmod(r.Runtime.Path, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"override_home", "invalid_environment", func(_ *testing.T, r *TaskRequest) { r.ExtraEnv = map[string]string{"HOME": "/other"} }},
		{"override_path", "invalid_environment", func(_ *testing.T, r *TaskRequest) { r.ExtraEnv = map[string]string{"PATH": "/other"} }},
		{"loader_injection", "invalid_environment", func(_ *testing.T, r *TaskRequest) { r.ExtraEnv = map[string]string{"LD_PRELOAD": "/other"} }},
		{"env_newline", "invalid_environment", func(_ *testing.T, r *TaskRequest) {
			r.ExtraEnv = map[string]string{"HTTPS_PROXY": "http://proxy\nvalue"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := taskFixture(t, "printf x > \"$CODEX_HOME/started\"\n"+emitTaskEvents(taskFinal, taskCompleted))
			originalHome := request.Home
			tc.mutate(t, &request)
			result, err := RunTask(t.Context(), request)
			assertTaskCode(t, err, tc.code)
			if result.Started {
				t.Fatal("invalid request started a process")
			}
			if _, err := os.Stat(filepath.Join(originalHome, "started")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid request had process side effects")
			}
		})
	}
}

func TestRunTaskCancellationBeforeStartDoesNotLaunchOrCreateState(t *testing.T) {
	request := taskFixture(t, emitTaskEvents(taskFinal, taskCompleted))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, err := RunTask(ctx, request)
	assertTaskCode(t, err, "cancelled")
	if result.Started || !errors.Is(err, context.Canceled) {
		t.Fatalf("wrong pre-start cancellation: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(request.Home, ".tmp")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled task created state")
	}
}

func TestRunTaskRechecksExecutableAfterVerifiedStatus(t *testing.T) {
	request := taskFixture(t, "printf original > \"$CODEX_HOME/started\"\n"+emitTaskEvents(taskFinal, taskCompleted))
	if err := os.WriteFile(request.Runtime.Path, []byte("#!/bin/sh\nprintf replacement > \"$CODEX_HOME/started\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := RunTask(t.Context(), request)
	assertTaskCode(t, err, "runtime_integrity_failed")
	if result.Started {
		t.Fatal("binary replaced after status verification was started")
	}
	if _, err := os.Stat(filepath.Join(request.Home, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement binary had side effects")
	}
}

func TestRunTaskAcceptsVerifiedSystemNativeExecutable(t *testing.T) {
	request := taskFixture(t, emitTaskEvents(taskFinal, taskCompleted))
	request.Runtime.Source = "system"
	result, err := RunTask(t.Context(), request)
	if err != nil || !result.Started || result.Output != "final answer" {
		t.Fatalf("reviewed system executable was not runnable: %+v %v", result, err)
	}
}

func TestRunTaskCancellationTerminatesTheWholeProcessGroup(t *testing.T) {
	request := taskFixture(t, `sleep 30 &
printf '%s' "$!" > "$CODEX_HOME/child.pid"
wait
`)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	type outcome struct {
		result TaskResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := RunTask(ctx, request); done <- outcome{result, err} }()
	deadline := time.Now().Add(5 * time.Second)
	var childPID int
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(request.Home, "child.pid"))
		if err == nil {
			childPID, err = strconv.Atoi(string(data))
			if err == nil && childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID <= 0 {
		t.Fatal("fixture child did not start")
	}
	cancel()
	select {
	case outcome := <-done:
		assertTaskCode(t, outcome.err, "cancelled")
		if !outcome.result.Started || !outcome.result.UsageIncomplete || !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("wrong cancellation result: %+v %v", outcome.result, outcome.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("task cancellation did not release process pipes")
	}
	if runtime.GOOS == "linux" {
		// A container's PID 1 may leave an adopted child as a zombie briefly.
		// A zombie has exited and cannot keep running or hold task pipes.
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPID))
			if errors.Is(err, os.ErrNotExist) {
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			end := strings.LastIndexByte(string(data), ')')
			if end >= 0 && strings.HasPrefix(string(data[end+1:]), " Z ") {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("task child survived process-group cancellation")
	}
}
