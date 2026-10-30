package codexcli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	MaxTaskPromptBytes = 1 << 20
	MaxTaskOutputBytes = 8 << 20
	MaxTaskLineBytes   = 2 << 20
	MaxTaskStderrBytes = 256 << 10
	maxTaskDuration    = 30 * time.Minute
	taskPath           = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
)

type TaskRequest struct {
	Runtime Status
	Home    string
	WorkDir string
	Prompt  string
	Model   string
	// ExtraEnv is an explicit allowlist, not an overlay on the server's
	// environment. It permits the task's mapped access token and proxy routing.
	ExtraEnv map[string]string
}

type TaskUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type TaskResult struct {
	Started         bool      `json:"started"`
	Output          string    `json:"output"`
	ThreadID        string    `json:"thread_id"`
	Usage           TaskUsage `json:"usage"`
	UsageIncomplete bool      `json:"usage_incomplete"`
}

// TaskError never contains child output, stderr, command arguments, filesystem
// paths, environment values, or provider diagnostics, any of which may contain
// credentials or user content. Code is suitable for structured server logs.
type TaskError struct {
	Code  string
	cause error
}

func (e *TaskError) Error() string { return "Codex CLI task failed: " + e.Code }
func (e *TaskError) Unwrap() error { return e.cause }

func taskError(code string) error { return &TaskError{Code: code} }

// RunTask executes exactly one CLI task and never retries. Runtime must come
// directly from a verified Manager.Status call, rather than client JSON. The
// caller owns account selection, audit, credential materialization, billing,
// and removal of the per-task Home and WorkDir after this function returns.
// These directories isolate state; they are not an OS security boundary.
func RunTask(ctx context.Context, request TaskRequest) (TaskResult, error) {
	return runTaskWithHostCheck(ctx, request, checkHostConfiguration)
}

func runTaskWithHostCheck(ctx context.Context, request TaskRequest, hostCheck func(context.Context) error) (TaskResult, error) {
	var result TaskResult
	status := request.Runtime
	if !status.ExecutionAvailable || !status.SourceVerified || !status.VersionMatch || (status.Source != "managed" && status.Source != "system") || status.Mode != "exec_task" || status.proof == nil || status.Path != status.proof.path || !filepath.IsAbs(status.Path) {
		return result, taskError("runtime_unavailable")
	}
	if strings.TrimSpace(request.Prompt) == "" || len(request.Prompt) > MaxTaskPromptBytes || strings.ContainsRune(request.Prompt, '\x00') {
		return result, taskError("invalid_prompt")
	}
	if !validTaskModel(request.Model) {
		return result, taskError("invalid_model")
	}
	if !isTaskDirectory(request.Home) || !isTaskDirectory(request.WorkDir) {
		return result, taskError("invalid_task_directory")
	}
	if err := ctx.Err(); err != nil {
		return result, &TaskError{Code: "cancelled", cause: err}
	}
	if err := hostCheck(ctx); err != nil {
		return result, taskError(hostConfigurationErrorCode(err))
	}
	environment, err := taskEnvironment(request.Home, request.ExtraEnv)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, &TaskError{Code: "cancelled", cause: err}
	}
	if err := verifyTaskExecutable(ctx, status); err != nil {
		return result, err
	}
	// Pin discovery to the freshly created workspace even when DATA_DIR is
	// inside a trusted Git project. Ancestor project config must not reactivate
	// auxiliary requests or change the account's file credential store.
	args := []string{"exec", "--json", "--ephemeral", "--skip-git-repo-check", "-c", "project_root_markers=[]"}
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	args = append(args, "-")
	runCtx, cancel := context.WithTimeout(ctx, maxTaskDuration)
	defer cancel()
	cmd := exec.CommandContext(runCtx, status.Path, args...)
	cmd.Dir, cmd.Env = request.WorkDir, environment
	cmd.Stdin = strings.NewReader(request.Prompt)
	if err := configureTaskProcess(cmd); err != nil {
		return result, taskError("unsupported_platform")
	}
	stderr := &taskStderr{cancel: cancel}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, taskError("process_setup_failed")
	}
	if err := cmd.Start(); err != nil {
		return result, taskError("process_start_failed")
	}
	result.Started, result.UsageIncomplete = true, true
	parseErr := parseTaskEvents(stdout, &result)
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if err := ctx.Err(); err != nil {
		result.Output = ""
		return result, &TaskError{Code: "cancelled", cause: err}
	}
	if stderr.exceeded.Load() {
		result.Output = ""
		return result, taskError("output_limit_exceeded")
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.Output = ""
		return result, &TaskError{Code: "deadline_exceeded", cause: context.DeadlineExceeded}
	}
	if parseErr != nil {
		result.Output = ""
		return result, parseErr
	}
	if waitErr != nil {
		result.Output = ""
		return result, taskError("process_failed")
	}
	return result, nil
}

func verifyTaskExecutable(ctx context.Context, status Status) error {
	file, err := regularFile(status.Path)
	if err != nil {
		return taskError("runtime_unavailable")
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maxExecutableBytes {
		return taskError("runtime_integrity_failed")
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(contextReader{ctx, file}, maxExecutableBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return &TaskError{Code: "cancelled", cause: ctx.Err()}
		}
		return taskError("runtime_integrity_failed")
	}
	if n != info.Size() {
		return taskError("runtime_integrity_failed")
	}
	if !equalDigest(digest.Sum(nil), status.proof.executableSHA256) {
		return taskError("runtime_integrity_failed")
	}
	return nil
}

func validTaskModel(model string) bool {
	if model == "" {
		return true
	}
	if len(model) > 128 || !asciiAlphaNum(model[0]) {
		return false
	}
	for _, c := range []byte(model) {
		if !asciiAlphaNum(c) && c != '-' && c != '_' && c != '.' && c != ':' && c != '/' {
			return false
		}
	}
	return true
}

func asciiAlphaNum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isTaskDirectory(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func taskEnvironment(home string, extra map[string]string) ([]string, error) {
	for key, value := range extra {
		switch key {
		case "SUB2API_CODEX_ACCESS_TOKEN", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY":
		default:
			return nil, taskError("invalid_environment")
		}
		if len(value) > 16<<10 || strings.ContainsAny(value, "\x00\r\n") {
			return nil, taskError("invalid_environment")
		}
	}
	tmpDir := filepath.Join(home, ".tmp")
	if err := os.Mkdir(tmpDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, taskError("task_environment_failed")
	}
	if !isTaskDirectory(tmpDir) {
		return nil, taskError("invalid_task_directory")
	}
	env := []string{
		"PATH=" + taskPath,
		"HOME=" + home,
		"CODEX_HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"TMPDIR=" + tmpDir,
		"TERM=dumb", "NO_COLOR=1", "LANG=C.UTF-8",
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+extra[key])
	}
	return env, nil
}

var errTaskOutputLimit = errors.New("task output limit exceeded")

type taskOutputReader struct {
	reader    io.Reader
	remaining int64
}

func (r *taskOutputReader) Read(p []byte) (int, error) {
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.reader.Read(p)
	if int64(n) > r.remaining {
		return 0, errTaskOutputLimit
	}
	r.remaining -= int64(n)
	return n, err
}

type taskStderr struct {
	written  int64
	exceeded atomic.Bool
	cancel   context.CancelFunc
}

func (s *taskStderr) Write(p []byte) (int, error) {
	s.written += int64(len(p))
	if s.written > MaxTaskStderrBytes {
		s.exceeded.Store(true)
		s.cancel()
		return 0, errTaskOutputLimit
	}
	return len(p), nil
}

type usageEvent struct {
	InputTokens           *int64 `json:"input_tokens"`
	CachedInputTokens     *int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64  `json:"cache_write_input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
}

func (u *usageEvent) usage() (TaskUsage, bool) {
	if u == nil || u.InputTokens == nil || u.CachedInputTokens == nil || u.OutputTokens == nil {
		return TaskUsage{}, false
	}
	usage := TaskUsage{InputTokens: *u.InputTokens, CachedInputTokens: *u.CachedInputTokens, CacheWriteInputTokens: u.CacheWriteInputTokens, OutputTokens: *u.OutputTokens, ReasoningOutputTokens: u.ReasoningOutputTokens}
	if usage.InputTokens < 0 || usage.CachedInputTokens < 0 || usage.CacheWriteInputTokens < 0 || usage.OutputTokens < 0 || usage.ReasoningOutputTokens < 0 || usage.CachedInputTokens > usage.InputTokens || usage.CacheWriteInputTokens > usage.InputTokens-usage.CachedInputTokens || usage.ReasoningOutputTokens > usage.OutputTokens || usage.OutputTokens > math.MaxInt64-usage.InputTokens {
		return TaskUsage{}, false
	}
	return usage, true
}

// The event names and usage fields follow codex-rs/exec/src/exec_events.rs at
// rust-v0.165.0. Unknown events/items are ignored; they are never Responses data.
func parseTaskEvents(reader io.Reader, result *TaskResult) error {
	scanner := bufio.NewScanner(&taskOutputReader{reader: reader, remaining: MaxTaskOutputBytes})
	scanner.Buffer(make([]byte, 64<<10), MaxTaskLineBytes)
	completed := false
	for scanner.Scan() {
		if errors.Is(scanner.Err(), errTaskOutputLimit) {
			return taskError("output_limit_exceeded")
		}
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var event struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Item     *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"item"`
			Usage *usageEvent `json:"usage"`
		}
		if err := json.Unmarshal(line, &event); err != nil || event.Type == "" {
			return taskError("invalid_event_stream")
		}
		switch event.Type {
		case "thread.started":
			if event.ThreadID == "" || len(event.ThreadID) > 128 || result.ThreadID != "" {
				return taskError("invalid_event_stream")
			}
			result.ThreadID = event.ThreadID
		case "item.completed":
			if event.Item != nil && event.Item.Type == "agent_message" {
				if completed {
					return taskError("invalid_event_stream")
				}
				result.Output = event.Item.Text
			}
		case "turn.completed":
			usage, valid := event.Usage.usage()
			if completed || !valid {
				return taskError("invalid_usage")
			}
			result.Usage, result.UsageIncomplete, completed = usage, false, true
		case "turn.failed":
			return taskError("turn_failed")
		case "error":
			return taskError("upstream_error")
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, errTaskOutputLimit) || errors.Is(err, bufio.ErrTooLong) {
			return taskError("output_limit_exceeded")
		}
		return taskError("event_read_failed")
	}
	if !completed {
		return taskError("turn_incomplete")
	}
	if strings.TrimSpace(result.Output) == "" {
		return taskError("final_output_missing")
	}
	return nil
}
