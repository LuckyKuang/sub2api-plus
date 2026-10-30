package codexcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	versionTimeout  = 5 * time.Second
	archiveName     = "official.tar.gz"
	execTaskMessage = "The reviewed CLI supports independent exec tasks and a transparent HTTP Responses proxy; WebSocket frames use the gateway protocol adapter."
)

// Capabilities distinguish exec JSONL from the separate reviewed official
// responses-api-proxy HTTP transport; exec output is never a wire proxy.
type Capabilities struct {
	ExecJSON           bool `json:"exec_json"`
	Ephemeral          bool `json:"ephemeral"`
	AccountConfig      bool `json:"account_config"`
	HTTPResponsesProxy bool `json:"http_responses_proxy"`
}

type Status struct {
	TargetVersion             string       `json:"target_version"`
	InstalledVersion          string       `json:"installed_version"`
	Path                      string       `json:"path"`
	Source                    string       `json:"source"`
	SourceVerified            bool         `json:"source_verified"`
	Platform                  string       `json:"platform"`
	VersionMatch              bool         `json:"version_match"`
	Installed                 bool         `json:"installed"`
	CanInstall                bool         `json:"can_install"`
	ExecutionAvailable        bool         `json:"execution_available"`
	Mode                      string       `json:"mode"`
	TransparentProxyAvailable bool         `json:"transparent_proxy_available"`
	UnavailableCode           string       `json:"unavailable_code"`
	Message                   string       `json:"message"`
	Capabilities              Capabilities `json:"capabilities"`
	proof                     *runtimeProof
}

// A JSON round trip or a caller setting public status fields cannot authorize
// execution. The proof is created only after official integrity verification.
type runtimeProof struct {
	path             string
	executableSHA256 []byte
}

// Manager owns an installation root; it never replaces a global executable.
// Install calls on one Manager serialize and waiting callers can cancel.
type Manager struct {
	root        string
	platform    string
	artifacts   map[string]artifact
	client      *http.Client
	baseURL     string
	lookPath    func(string) (string, error)
	version     func(context.Context, string) (string, error)
	hostCheck   func(context.Context) error
	installGate chan struct{}
}

var defaultManager = sync.OnceValue(func() *Manager { return NewManager("") })

// DefaultManager shares one installation gate between settings, account
// enablement, and task execution in this server process.
func DefaultManager() *Manager { return defaultManager() }

func NewManager(root string) *Manager {
	if strings.TrimSpace(root) == "" {
		dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
		if dataDir == "" {
			dataDir = "./data"
		}
		root = filepath.Join(dataDir, "codex", "cli")
	}
	// Task workers run in account-specific working directories. Resolve the
	// executable location before a caller changes Cmd.Dir.
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return &Manager{
		root:        filepath.Clean(root),
		platform:    runtime.GOOS + "/" + runtime.GOARCH,
		artifacts:   reviewedArtifacts,
		client:      &http.Client{Timeout: 10 * time.Minute},
		baseURL:     releaseURL,
		lookPath:    exec.LookPath,
		version:     executableVersion,
		hostCheck:   checkHostConfiguration,
		installGate: make(chan struct{}, 1),
	}
}

func (m *Manager) installDir() string {
	return filepath.Join(m.root, "versions", targetVersion, strings.ReplaceAll(m.platform, "/", "-"))
}

// Status does not install, create directories, fetch release metadata, or access
// the network. Managed provenance is rechecked against the retained pinned
// archive, so a writable receipt cannot authorize a replacement executable.
func (m *Manager) Status(ctx context.Context) (s Status) {
	defer func() {
		if err := m.checkHost(ctx); err != nil {
			applyHostConfigurationFailure(&s, err)
		}
	}()
	_, supported := m.artifacts[m.platform]
	s = Status{TargetVersion: targetVersion, Platform: m.platform, CanInstall: supported, Mode: "exec_task", Message: execTaskMessage}
	if err := ctx.Err(); err != nil {
		s.UnavailableCode = "check_cancelled"
		return s
	}
	managedPath := filepath.Join(m.installDir(), "codex")
	var executableDigest []byte
	if _, err := os.Lstat(managedPath); err == nil {
		s.Path, s.Source, s.Installed = managedPath, "managed", true
		var verifyErr error
		if supported {
			executableDigest, verifyErr = verifiedInstallationDigest(ctx, m.installDir(), m.artifacts[m.platform])
		}
		if !supported || verifyErr != nil {
			s.UnavailableCode = "integrity_check_failed"
			return s
		}
		s.SourceVerified = true
	} else if !errors.Is(err, os.ErrNotExist) {
		s.Path, s.Source, s.UnavailableCode = managedPath, "managed", "installation_unreadable"
		return s
	} else if systemPath, err := m.lookPath("codex"); err == nil {
		s.Path, s.Source, s.Installed = systemPath, "system", true
		if resolved, err := filepath.EvalSymlinks(systemPath); err == nil {
			s.Path = resolved
		}
		if absolute, err := filepath.Abs(s.Path); err == nil {
			s.Path = absolute
		}
		if supported {
			if nativePath, digest, err := m.systemExecutable(ctx, s.Path); err == nil {
				s.Path, executableDigest, s.SourceVerified = nativePath, digest, true
			}
		}
	} else {
		s.UnavailableCode = "not_installed"
		if !supported {
			s.UnavailableCode = "unsupported_platform"
		}
		return s
	}
	// A version probe is code execution with the service user's permissions.
	// Do not invoke arbitrary PATH candidates or npm launchers merely to label
	// their version: only the pinned native executable may be executed.
	if !supported {
		s.UnavailableCode = "unsupported_platform"
		return s
	}
	if !s.SourceVerified {
		s.UnavailableCode = "source_unverified"
		return s
	}
	versionCtx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	version, err := m.version(versionCtx, s.Path)
	if err != nil {
		s.UnavailableCode = "version_detection_failed"
		return s
	}
	s.InstalledVersion = version
	s.VersionMatch = version == targetVersion
	if !s.VersionMatch {
		s.UnavailableCode = "version_mismatch"
	} else {
		s.ExecutionAvailable = true
		// rust-v0.165.0 cli/src/main.rs exposes ResponsesApiProxy; its
		// responses-api-proxy/src/lib.rs forwards HTTP/SSE without an agent loop.
		s.TransparentProxyAvailable = true
		s.Capabilities = Capabilities{ExecJSON: true, Ephemeral: true, AccountConfig: true, HTTPResponsesProxy: true}
		s.proof = &runtimeProof{path: s.Path, executableSHA256: executableDigest}
	}
	return s
}

// Install downloads only the embedded reviewed artifact. A failure before
// publication leaves the existing installation untouched. Running CLI workers
// are never signalled or stopped by an installation.
func (m *Manager) Install(ctx context.Context) (Status, error) {
	select {
	case m.installGate <- struct{}{}:
		defer func() { <-m.installGate }()
	case <-ctx.Done():
		return m.Status(ctx), ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return m.Status(ctx), err
	}
	if err := m.checkHost(ctx); err != nil {
		status := m.Status(ctx)
		applyHostConfigurationFailure(&status, err)
		return status, &hostConfigurationError{code: hostConfigurationErrorCode(err)}
	}
	a, ok := m.artifacts[m.platform]
	if !ok {
		return m.Status(ctx), fmt.Errorf("official Codex CLI installation is unsupported on %s", m.platform)
	}
	if status := m.Status(ctx); status.UnavailableCode == hostConfigurationConflict || status.UnavailableCode == hostConfigurationCheckFailed {
		return status, &hostConfigurationError{code: status.UnavailableCode}
	} else if status.ExecutionAvailable {
		return status, nil
	}
	parent := filepath.Dir(m.installDir())
	if err := os.MkdirAll(parent, 0700); err != nil {
		return m.Status(ctx), fmt.Errorf("create CLI installation directory: %w", err)
	}
	unlock, err := acquireInstallFileLock(ctx, filepath.Join(parent, ".install.lock"))
	if err != nil {
		return m.Status(ctx), err
	}
	defer unlock()
	// Another process/container sharing this data directory may have completed
	// the exact target while this caller waited for the filesystem lock.
	if status := m.Status(ctx); status.UnavailableCode == hostConfigurationConflict || status.UnavailableCode == hostConfigurationCheckFailed {
		return status, &hostConfigurationError{code: status.UnavailableCode}
	} else if status.ExecutionAvailable {
		return status, nil
	}
	if err := ctx.Err(); err != nil {
		return m.Status(ctx), err
	}
	stage, err := os.MkdirTemp(parent, ".install-")
	if err != nil {
		return m.Status(ctx), fmt.Errorf("stage CLI installation: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := m.download(ctx, a, filepath.Join(stage, archiveName)); err != nil {
		return m.Status(ctx), err
	}
	if err := extractExecutable(ctx, filepath.Join(stage, archiveName), filepath.Join(stage, "codex"), a.Entry); err != nil {
		return m.Status(ctx), err
	}
	versionCtx, cancel := context.WithTimeout(ctx, versionTimeout)
	version, err := m.version(versionCtx, filepath.Join(stage, "codex"))
	cancel()
	if err != nil {
		return m.Status(ctx), fmt.Errorf("check staged CLI version: %w", err)
	}
	if version != targetVersion {
		return m.Status(ctx), errors.New("staged CLI does not match the reviewed version")
	}
	if err := ctx.Err(); err != nil {
		return m.Status(ctx), err
	}
	if err := publishInstallation(stage, m.installDir()); err != nil {
		return m.Status(ctx), err
	}
	return m.Status(ctx), nil
}

func (m *Manager) download(ctx context.Context, a artifact, destination string) error {
	if a.Size <= 0 || a.Size > maxArchiveBytes {
		return errors.New("reviewed CLI archive size is invalid")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+a.Name, nil)
	if err != nil {
		return errors.New("construct CLI download request")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download reviewed CLI: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download reviewed CLI: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > a.Size {
		return errors.New("CLI archive exceeds reviewed size")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create CLI archive: %w", err)
	}
	n, copyErr := io.Copy(f, io.LimitReader(resp.Body, a.Size+1))
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("download CLI archive: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("save CLI archive: %w", closeErr)
	}
	if n != a.Size {
		return errors.New("CLI archive does not match reviewed size")
	}
	if err := verifyArchive(ctx, destination, a); err != nil {
		return err
	}
	return nil
}

func publishInstallation(stage, destination string) error {
	if _, err := os.Lstat(destination); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(stage, destination); err != nil {
			return fmt.Errorf("publish CLI installation: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect existing CLI installation: %w", err)
	}
	// A healthy target is reused earlier. Preserve an incomplete or damaged target
	// until the fully verified replacement can be renamed into place.
	backup, err := os.MkdirTemp(filepath.Dir(destination), ".previous-")
	if err != nil {
		return fmt.Errorf("prepare CLI replacement: %w", err)
	}
	if err := os.Remove(backup); err != nil {
		return err
	}
	if err := os.Rename(destination, backup); err != nil {
		return fmt.Errorf("preserve existing CLI installation: %w", err)
	}
	if err := os.Rename(stage, destination); err != nil {
		if restoreErr := os.Rename(backup, destination); restoreErr != nil {
			return fmt.Errorf("publish CLI installation: %w; restore failed: %v", err, restoreErr)
		}
		return fmt.Errorf("publish CLI installation: %w", err)
	}
	return os.RemoveAll(backup)
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 4096 {
		return 0, errors.New("CLI version output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func executableVersion(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "--version")
	if err := configureTaskProcess(cmd); err != nil {
		return "", errors.New("CLI version process is unsupported")
	}
	var stdout, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	// --version needs neither account tokens nor the gateway's environment.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if err := cmd.Run(); err != nil {
		return "", errors.New("CLI version probe failed")
	}
	fields := strings.Fields(stdout.String())
	if len(fields) != 2 || fields[0] != "codex-cli" || !validVersion(fields[1]) {
		return "", errors.New("unrecognized CLI version output")
	}
	return fields[1], nil
}

func validVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, c := range part {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
