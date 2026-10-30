//go:build unit

package codexcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testExecutable = "fixture official executable\n"
const testEntry = "codex-test-platform"

type tarEntry struct {
	name string
	body string
	kind byte
	link string
	size int64
}

func fixtureArchive(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var result bytes.Buffer
	gz := gzip.NewWriter(&result)
	tw := tar.NewWriter(gz)
	truncated := false
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		size := int64(len(entry.body))
		if entry.size > 0 {
			size = entry.size
			truncated = true
		}
		if err := tw.WriteHeader(&tar.Header{Name: entry.name, Typeflag: kind, Linkname: entry.link, Size: size, Mode: 0700}); err != nil {
			t.Fatal(err)
		}
		if entry.body != "" {
			if _, err := io.WriteString(tw, entry.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil && !truncated {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return result.Bytes()
}

func validFixture(t *testing.T) []byte {
	t.Helper()
	return fixtureArchive(t, tarEntry{name: testEntry, body: testExecutable})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func fixtureManager(t *testing.T, archive []byte) *Manager {
	t.Helper()
	m := NewManager(filepath.Join(t.TempDir(), "runtime"))
	m.hostCheck = func(context.Context) error { return nil }
	m.platform = "linux/amd64"
	digest := sha256.Sum256(archive)
	m.artifacts = map[string]artifact{m.platform: {
		Name: "pinned-fixture.tar.gz", SHA256: hex.EncodeToString(digest[:]), Size: int64(len(archive)), Entry: testEntry,
	}}
	m.lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	m.version = func(context.Context, string) (string, error) { return "0.165.0", nil }
	m.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != releaseURL+"pinned-fixture.tar.gz" {
			t.Errorf("unexpected download URL %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive)), ContentLength: int64(len(archive))}, nil
	})}
	return m
}

func seedManaged(t *testing.T, m *Manager, archive []byte, executable string) {
	t.Helper()
	if err := os.MkdirAll(m.installDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.installDir(), archiveName), archive, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.installDir(), "codex"), []byte(executable), 0700); err != nil {
		t.Fatal(err)
	}
}

func assertNoInstallTemps(t *testing.T, m *Manager) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(m.installDir()))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-") || strings.HasPrefix(entry.Name(), ".previous-") {
			t.Errorf("temporary installation leaked: %s", entry.Name())
		}
	}
}

func TestReviewedManifestUsesOnlyPinnedOfficialAssets(t *testing.T) {
	if TargetVersion() != "0.165.0" || releaseURL != "https://github.com/openai/codex/releases/download/rust-v0.165.0/" {
		t.Fatal("target version and official release URL changed without a reviewed baseline")
	}
	want := map[string]artifact{
		"darwin/arm64": {Name: "codex-aarch64-apple-darwin.tar.gz", SHA256: "78ea9c178d2dc8fde71d6494df3b9eab902fd6d6de0a84e65234220d93bb6144", BinarySHA256: "a45099154d7fb4a5ed8a56f40966a95deb938e508bab23e6985035c709e9a85a", Size: 102366249, Entry: "codex-aarch64-apple-darwin"},
		"darwin/amd64": {Name: "codex-x86_64-apple-darwin.tar.gz", SHA256: "f46451741936ebdf5201b8becd0820a41be2a01d271b98703e0d9a1670c99993", BinarySHA256: "f8096ee535ca7af9c57e315e5680f58457ff49dcc5fc619e5ba5eca24293ffe2", Size: 111288240, Entry: "codex-x86_64-apple-darwin"},
		"linux/arm64":  {Name: "codex-aarch64-unknown-linux-musl.tar.gz", SHA256: "6522e056d0be7f531da915c9a81ae05bc70811b782542499cc0a9776d3abf5d3", BinarySHA256: "b32ece31af06440da980b733fb8fefdd1f193439595fda84096f1caaceec543f", Size: 107804617, Entry: "codex-aarch64-unknown-linux-musl"},
		"linux/amd64":  {Name: "codex-x86_64-unknown-linux-musl.tar.gz", SHA256: "be635d3486a5bd060d0fff582ae6d30160280c31344dd85719707fa920e187f0", BinarySHA256: "c06a67fd8ac76508dc78bd99ce20c9f317b8d568beda59485f3f4134df9962d5", Size: 115837472, Entry: "codex-x86_64-unknown-linux-musl"},
	}
	if len(reviewedArtifacts) != len(want) {
		t.Fatalf("supported platforms = %d", len(reviewedArtifacts))
	}
	for platform, expected := range want {
		if reviewedArtifacts[platform] != expected {
			t.Errorf("unreviewed artifact for %s: %+v", platform, reviewedArtifacts[platform])
		}
	}
}

func TestDefaultManagerIsShared(t *testing.T) {
	if DefaultManager() != DefaultManager() {
		t.Fatal("default manager must share the installation gate")
	}
}

func TestNewManagerDefaultsUnderDataDirectory(t *testing.T) {
	t.Setenv("DATA_DIR", filepath.Join(t.TempDir(), "data"))
	m := NewManager("")
	if m.root != filepath.Join(os.Getenv("DATA_DIR"), "codex", "cli") {
		t.Fatalf("unexpected default root %s", m.root)
	}
	t.Setenv("DATA_DIR", " ")
	expected, err := filepath.Abs(filepath.Join("data", "codex", "cli"))
	if err != nil {
		t.Fatal(err)
	}
	if got := NewManager("").root; got != expected {
		t.Fatalf("unexpected empty DATA_DIR root %s", got)
	}
}

func TestStatusIsReadOnlyWhenMissing(t *testing.T) {
	m := fixtureManager(t, validFixture(t))
	m.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("status accessed network")
		return nil, errors.New("unexpected network")
	})
	m.version = func(context.Context, string) (string, error) {
		t.Error("version probe without installation")
		return "", nil
	}
	status := m.Status(context.Background())
	if status.Installed || !status.CanInstall || status.ExecutionAvailable || status.UnavailableCode != "not_installed" {
		t.Fatalf("unexpected missing status: %+v", status)
	}
	if status.Mode != "exec_task" || status.TransparentProxyAvailable {
		t.Fatalf("wrong transport capability: %+v", status)
	}
	if _, err := os.Stat(m.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status created installation root: %v", err)
	}
}

// Detection must never execute unreviewed code with the service user's OS
// permissions. Emptying its environment does not prevent access to local data.
func TestUnverifiedSystemCLIDetectionNeverExecutesVersionProbe(t *testing.T) {
	for _, tc := range []struct{ name, platform, code string }{
		{"native", "linux/amd64", "source_unverified"},
		{"npm_launcher", "linux/amd64", "source_unverified"},
		{"unsupported_platform", "windows/amd64", "unsupported_platform"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureManager(t, validFixture(t))
			m.platform = tc.platform
			root := t.TempDir()
			binary := filepath.Join(root, "codex")
			if tc.name == "npm_launcher" {
				binary = filepath.Join(root, "node_modules", "@openai", "codex", "bin", "codex.js")
				if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(filepath.Dir(binary)), "package.json"), []byte(`{"name":"@openai/codex"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(root, "version-probe-started")
			script := fmt.Sprintf("#!/bin/sh\n: > %q\nprintf 'codex-cli 0.165.0\\n'\n", marker)
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			m.lookPath = func(name string) (string, error) {
				if name != "codex" {
					t.Fatal(name)
				}
				return binary, nil
			}
			probes := 0
			m.version = func(ctx context.Context, path string) (string, error) {
				probes++
				return executableVersion(ctx, path)
			}
			status := m.Status(context.Background())
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("unreviewed version probe produced a filesystem side effect: %v", err)
			}
			if probes != 0 {
				t.Fatalf("version probe invoked for an unverified executable: %d", probes)
			}
			if !status.Installed || status.Source != "system" || status.SourceVerified || status.ExecutionAvailable || status.UnavailableCode != tc.code || status.InstalledVersion != "" || status.VersionMatch || status.proof != nil {
				t.Fatalf("unexpected system status: %+v", status)
			}
		})
	}
}

func TestSystemNativeCLINeedsReviewedBinaryDigestAndExactVersion(t *testing.T) {
	for _, tc := range []struct {
		name, pin, version, code string
		verified                 bool
	}{
		{name: "official_native", pin: "fixture", version: "0.165.0", verified: true},
		{name: "official_wrong_version", pin: "fixture", version: "0.164.0", code: "version_mismatch", verified: true},
		{name: "counterfeit_same_version", pin: strings.Repeat("0", 64), version: "0.165.0", code: "source_unverified"},
		{name: "missing_reviewed_pin", version: "0.165.0", code: "source_unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureManager(t, validFixture(t))
			binary := filepath.Join(t.TempDir(), "codex")
			if err := os.WriteFile(binary, []byte(testExecutable), 0700); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(testExecutable))
			pin := tc.pin
			if pin == "fixture" {
				pin = hex.EncodeToString(digest[:])
			}
			a := m.artifacts[m.platform]
			a.BinarySHA256 = pin
			m.artifacts[m.platform] = a
			m.lookPath = func(string) (string, error) { return binary, nil }
			m.version = func(context.Context, string) (string, error) {
				if !tc.verified {
					t.Error("unverified executable was invoked")
				}
				return tc.version, nil
			}
			s := m.Status(t.Context())
			if s.Source != "system" || !s.Installed || s.SourceVerified != tc.verified || s.ExecutionAvailable != (tc.verified && tc.version == "0.165.0") || s.UnavailableCode != tc.code {
				t.Fatalf("wrong system provenance status: %+v", s)
			}
			if s.ExecutionAvailable && (s.proof == nil || !equalDigest(s.proof.executableSHA256, digest[:])) {
				t.Fatal("verified system CLI has no matching runtime proof")
			}
		})
	}
}

func TestSystemNativeCLICanBeReferencedThroughSymlink(t *testing.T) {
	m := fixtureManager(t, validFixture(t))
	dir := t.TempDir()
	binary, link := filepath.Join(dir, "official-codex"), filepath.Join(dir, "codex")
	if err := os.WriteFile(binary, []byte(testExecutable), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, link); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(testExecutable))
	a := m.artifacts[m.platform]
	a.BinarySHA256 = hex.EncodeToString(digest[:])
	m.artifacts[m.platform] = a
	m.lookPath = func(string) (string, error) { return link, nil }
	s := m.Status(t.Context())
	if !s.ExecutionAvailable || s.Path != binary || s.Source != "system" {
		t.Fatalf("reviewed symlinked system executable unavailable: %+v", s)
	}
}

func TestVerifiedManagedCLIHasPrecedenceAndReviewedTaskAndProxyCapabilities(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	seedManaged(t, m, archive, testExecutable)
	m.lookPath = func(string) (string, error) {
		t.Error("managed target must precede PATH lookup")
		return "/system/codex", nil
	}
	m.version = func(ctx context.Context, path string) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("version detection has no timeout")
		}
		if path != filepath.Join(m.installDir(), "codex") {
			t.Errorf("wrong executable: %s", path)
		}
		return "0.165.0", nil
	}
	s := m.Status(context.Background())
	if !s.Installed || s.Source != "managed" || !s.SourceVerified || !s.VersionMatch || !s.ExecutionAvailable || s.UnavailableCode != "" {
		t.Fatalf("unexpected managed status: %+v", s)
	}
	if s.Mode != "exec_task" || !s.TransparentProxyAvailable || !s.Capabilities.HTTPResponsesProxy || !s.Capabilities.ExecJSON || !s.Capabilities.Ephemeral || !s.Capabilities.AccountConfig {
		t.Fatalf("wrong task capability contract: %+v", s)
	}
	if !strings.Contains(s.Message, "transparent HTTP Responses proxy") {
		t.Fatalf("missing capability explanation: %s", s.Message)
	}
}

func TestManagedIntegrityMustBeCheckedBeforeExecutingVersionProbe(t *testing.T) {
	for _, tc := range []string{"changed_binary", "changed_archive", "symlink_binary", "missing_archive", "not_executable"} {
		t.Run(tc, func(t *testing.T) {
			archive := validFixture(t)
			m := fixtureManager(t, archive)
			seedManaged(t, m, archive, testExecutable)
			binary := filepath.Join(m.installDir(), "codex")
			switch tc {
			case "changed_binary":
				if err := os.WriteFile(binary, []byte("replacement executable"), 0700); err != nil {
					t.Fatal(err)
				}
			case "changed_archive":
				if err := os.WriteFile(filepath.Join(m.installDir(), archiveName), []byte("replacement archive"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing_archive":
				if err := os.Remove(filepath.Join(m.installDir(), archiveName)); err != nil {
					t.Fatal(err)
				}
			case "not_executable":
				if err := os.Chmod(binary, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink_binary":
				if err := os.Remove(binary); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(outside, []byte(testExecutable), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, binary); err != nil {
					t.Fatal(err)
				}
			}
			m.version = func(context.Context, string) (string, error) {
				t.Error("unverified executable was invoked")
				return "0.165.0", nil
			}
			status := m.Status(context.Background())
			if status.SourceVerified || status.ExecutionAvailable || status.UnavailableCode != "integrity_check_failed" {
				t.Fatalf("untrusted managed executable accepted: %+v", status)
			}
		})
	}
}

func TestVersionFailureAndMismatchPreventManagedExecution(t *testing.T) {
	for _, tc := range []struct {
		name, version, code string
		err                 error
	}{
		{name: "mismatch", version: "0.164.0", code: "version_mismatch"},
		{name: "failure", code: "version_detection_failed", err: errors.New("probe failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := validFixture(t)
			m := fixtureManager(t, archive)
			seedManaged(t, m, archive, testExecutable)
			m.version = func(context.Context, string) (string, error) { return tc.version, tc.err }
			status := m.Status(context.Background())
			if !status.SourceVerified || status.ExecutionAvailable || status.UnavailableCode != tc.code {
				t.Fatalf("bad version accepted: %+v", status)
			}
		})
	}
}

func TestUnsupportedPlatformDoesNotInstall(t *testing.T) {
	m := fixtureManager(t, validFixture(t))
	m.platform = "windows/amd64"
	status := m.Status(context.Background())
	if status.CanInstall || status.ExecutionAvailable || status.UnavailableCode != "unsupported_platform" {
		t.Fatalf("unsupported platform enabled: %+v", status)
	}
	if _, err := m.Install(context.Background()); err == nil {
		t.Fatal("unsupported installation succeeded")
	}
	if _, err := os.Stat(m.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported installation created files")
	}
}

func TestInstallPublishesPinnedArchiveAndReusesHealthyTarget(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	var downloads atomic.Int32
	transport := m.client.Transport
	m.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) { downloads.Add(1); return transport.RoundTrip(req) })
	for range 2 {
		status, err := m.Install(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !status.ExecutionAvailable || status.Source != "managed" || status.InstalledVersion != "0.165.0" {
			t.Fatalf("install did not enable reviewed task mode: %+v", status)
		}
	}
	if downloads.Load() != 1 {
		t.Fatalf("download count = %d, want 1", downloads.Load())
	}
	got, err := os.ReadFile(filepath.Join(m.installDir(), "codex"))
	if err != nil || string(got) != testExecutable {
		t.Fatalf("wrong published executable: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(m.installDir(), archiveName)); err != nil || !bytes.Equal(got, archive) {
		t.Fatalf("pinned archive was not retained: %v", err)
	}
	assertNoInstallTemps(t, m)
}

func TestInstallFailuresPreservePreviousFilesAndRemoveStages(t *testing.T) {
	for _, tc := range []string{"checksum", "short_download", "large_download", "http", "network", "unsafe_archive", "wrong_version", "version_error", "cancel_before_publish"} {
		t.Run(tc, func(t *testing.T) {
			archive := validFixture(t)
			if tc == "unsafe_archive" {
				archive = fixtureArchive(t, tarEntry{name: testEntry, kind: tar.TypeSymlink, link: "elsewhere"})
			}
			m := fixtureManager(t, archive)
			seedManaged(t, m, []byte("previous archive"), "previous binary")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tc {
			case "checksum":
				a := m.artifacts[m.platform]
				a.SHA256 = strings.Repeat("0", 64)
				m.artifacts[m.platform] = a
			case "short_download":
				m.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(archive[:len(archive)-1])), ContentLength: -1}, nil
				})
			case "large_download":
				m.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(append(append([]byte{}, archive...), 0))), ContentLength: -1}, nil
				})
			case "http":
				m.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
				})
			case "network":
				m.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
			case "wrong_version":
				m.version = func(context.Context, string) (string, error) { return "0.166.0", nil }
			case "version_error":
				m.version = func(context.Context, string) (string, error) { return "", errors.New("not executable") }
			case "cancel_before_publish":
				m.version = func(context.Context, string) (string, error) { cancel(); return "0.165.0", nil }
			}
			if _, err := m.Install(ctx); err == nil {
				t.Fatal("unsafe or incomplete installation succeeded")
			}
			for name, expected := range map[string]string{"codex": "previous binary", archiveName: "previous archive"} {
				got, err := os.ReadFile(filepath.Join(m.installDir(), name))
				if err != nil || string(got) != expected {
					t.Fatalf("previous %s changed after failure: %q, %v", name, got, err)
				}
			}
			assertNoInstallTemps(t, m)
		})
	}
}

func TestInstallReplacesDamagedTargetAfterSuccessfulStaging(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	seedManaged(t, m, []byte("damaged archive"), "old binary")
	status, err := m.Install(context.Background())
	if err != nil || !status.ExecutionAvailable {
		t.Fatalf("replacement failed: %+v %v", status, err)
	}
	assertNoInstallTemps(t, m)
}

func TestInstallCancellationDoesNotCreateDirectories(t *testing.T) {
	m := fixtureManager(t, validFixture(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Install(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled install = %v", err)
	}
	if _, err := os.Stat(m.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled installation created files")
	}
}

func TestInstallGateAllowsWaitingCallerToCancel(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	started, finishDownload := make(chan struct{}), make(chan struct{})
	transport := m.client.Transport
	m.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-finishDownload:
			return transport.RoundTrip(req)
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})
	firstDone := make(chan error, 1)
	go func() { _, err := m.Install(context.Background()); firstDone <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first installation did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Install(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("waiting caller cancellation = %v", err)
	}
	close(finishDownload)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first installation did not finish")
	}
	assertNoInstallTemps(t, m)
}

func TestConcurrentInstallCallsDownloadOnce(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	var downloads atomic.Int32
	transport := m.client.Transport
	m.client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) { downloads.Add(1); return transport.RoundTrip(req) })
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			s, err := m.Install(context.Background())
			if err != nil || !s.ExecutionAvailable {
				t.Errorf("concurrent install: %+v %v", s, err)
			}
		})
	}
	wg.Wait()
	if downloads.Load() != 1 {
		t.Fatalf("concurrent downloads = %d", downloads.Load())
	}
	assertNoInstallTemps(t, m)
}

func TestIndependentManagersShareInstallationFilesystemLock(t *testing.T) {
	archive := validFixture(t)
	first := fixtureManager(t, archive)
	second := fixtureManager(t, archive)
	second.root = first.root
	started, release := make(chan struct{}), make(chan struct{})
	var downloads atomic.Int32
	transport := first.client.Transport
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if downloads.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		return transport.RoundTrip(req)
	})}
	first.client, second.client = client, client
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := first.Install(t.Context()); firstDone <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first installer did not start")
	}
	go func() { _, err := second.Install(t.Context()); secondDone <- err }()
	// The second Manager has a different in-memory gate. It can only be held
	// away from download/publication by the shared filesystem lock.
	select {
	case err := <-secondDone:
		close(release)
		t.Fatalf("second installer escaped shared lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for _, done := range []chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("installer failed to finish")
		}
	}
	if downloads.Load() != 1 {
		t.Fatalf("independent managers downloaded %d times", downloads.Load())
	}
	if !second.Status(t.Context()).ExecutionAvailable {
		t.Fatal("shared installed runtime is unavailable")
	}
	assertNoInstallTemps(t, first)
}

func TestVersionGrammarRejectsUnrelatedOrAmbiguousVersions(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{"0.165.0", true}, {"1.0.0", true}, {"0.165", false}, {"0.165.0-beta.1", false}, {"latest", false}, {"0.165.0\n", false}, {"0..0", false}, {"-1.165.0", false},
	} {
		if got := validVersion(tc.input); got != tc.valid {
			t.Errorf("validVersion(%q) = %v", tc.input, got)
		}
	}
}

func TestVersionOutputBufferIsBounded(t *testing.T) {
	var b boundedBuffer
	if _, err := fmt.Fprint(&b, strings.Repeat("x", 4096)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(&b, "x"); err == nil {
		t.Fatal("unbounded version output accepted")
	}
}
