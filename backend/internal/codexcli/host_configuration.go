package codexcli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

const hostConfigurationConflict = "host_configuration_conflict"
const hostConfigurationCheckFailed = "host_configuration_check_failed"
const managedPreferencesTimeout = 5 * time.Second

// The reviewed CLI reads these Unix system sources independently of CODEX_HOME.
// Its legacy managed source and macOS forced preferences even outrank -c flags.
// Source: openai/codex commit 360319d8dee62d87e2d59fc0c21805d79519cfed,
// codex-rs/config/src/loader/{mod.rs,layer_io.rs,macos.rs}.
var hostConfigurationPaths = [...]string{
	"/etc/codex/config.toml",
	"/etc/codex/managed_config.toml",
	"/etc/codex/requirements.toml",
}

// Query only forced-key booleans, never their values. The API uses the service
// user's managed preferences even when the task receives a private HOME.
const managedPreferencesScript = `ObjC.import('Foundation');
var defaults = $.NSUserDefaults.standardUserDefaults;
var domain = 'com.openai.codex';
(defaults.objectIsForcedForKeyInDomain('config_toml_base64', domain) ||
 defaults.objectIsForcedForKeyInDomain('requirements_toml_base64', domain)) ? 'managed' : 'clear';`

type hostConfigurationError struct{ code string }

func (e *hostConfigurationError) Error() string {
	return "Codex CLI host configuration prevents managed execution: " + e.code
}

func hostConfigurationErrorCode(err error) string {
	var failure *hostConfigurationError
	if errors.As(err, &failure) && failure.code == hostConfigurationConflict {
		return hostConfigurationConflict
	}
	return hostConfigurationCheckFailed
}

type hostConfigurationProbe struct {
	platform string
	lstat    func(string) (os.FileInfo, error)
	run      func(context.Context, *exec.Cmd) error
}

func checkHostConfiguration(ctx context.Context) error {
	return (hostConfigurationProbe{
		platform: runtime.GOOS,
		lstat:    os.Lstat,
		run:      func(_ context.Context, command *exec.Cmd) error { return command.Run() },
	}).check(ctx)
}

func (p hostConfigurationProbe) check(ctx context.Context) error {
	if p.platform != "linux" && p.platform != "darwin" {
		// Unsupported execution platforms retain their ordinary unavailable code.
		return nil
	}
	for _, path := range hostConfigurationPaths {
		if ctx.Err() != nil {
			return &hostConfigurationError{code: hostConfigurationCheckFailed}
		}
		_, err := p.lstat(path)
		if err == nil {
			return &hostConfigurationError{code: hostConfigurationConflict}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return &hostConfigurationError{code: hostConfigurationCheckFailed}
		}
	}
	if p.platform != "darwin" {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, managedPreferencesTimeout)
	defer cancel()
	command := exec.CommandContext(probeCtx, "/usr/bin/osascript", "-l", "JavaScript", "-e", managedPreferencesScript)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "LANG=C.UTF-8"}
	var output boundedBuffer
	command.Stdout, command.Stderr = &output, io.Discard
	if configureTaskProcess(command) != nil || p.run(probeCtx, command) != nil || probeCtx.Err() != nil {
		return &hostConfigurationError{code: hostConfigurationCheckFailed}
	}
	switch output.String() {
	case "clear\n":
		return nil
	case "managed\n":
		return &hostConfigurationError{code: hostConfigurationConflict}
	default:
		return &hostConfigurationError{code: hostConfigurationCheckFailed}
	}
}

func applyHostConfigurationFailure(status *Status, err error) {
	status.ExecutionAvailable, status.CanInstall, status.proof = false, false, nil
	status.UnavailableCode = hostConfigurationErrorCode(err)
	if status.UnavailableCode == hostConfigurationConflict {
		status.Message = "Host Codex configuration conflicts with account-isolated task execution."
	} else {
		status.Message = "Host Codex configuration could not be checked before task execution."
	}
}

func (m *Manager) checkHost(ctx context.Context) error {
	if m.hostCheck != nil {
		return m.hostCheck(ctx)
	}
	return checkHostConfiguration(ctx)
}
