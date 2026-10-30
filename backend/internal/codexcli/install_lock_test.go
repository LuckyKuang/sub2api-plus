//go:build unit

package codexcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallationFileLockCancellationAndRelease(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("official CLI installation requires macOS or Linux")
	}
	path := filepath.Join(t.TempDir(), ".install.lock")
	unlock, err := acquireInstallFileLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if unexpectedUnlock, err := acquireInstallFileLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if unexpectedUnlock != nil {
			unexpectedUnlock()
		}
		unlock()
		t.Fatalf("waiting lock cancellation = %v", err)
	}
	unlock()
	nextUnlock, err := acquireInstallFileLock(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	nextUnlock()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file must stay present to retain one locking inode: %v", err)
	}
}

func TestInstallationFileLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".install.lock")
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if unlock, err := acquireInstallFileLock(t.Context(), path); err == nil {
		unlock()
		t.Fatal("installation lock followed symlink")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("lock changed outside file: %q %v", data, err)
	}
}

func TestVersionProbeDoesNotInheritGatewayCredentials(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "gateway-secret-must-not-reach-version-probe")
	t.Setenv("CODEX_HOME", "/shared/global-codex-home")
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --version ] && [ -z \"${OPENAI_API_KEY-}\" ] && [ -z \"${CODEX_HOME-}\" ] || exit 7\nprintf '%s\\n' 'codex-cli 0.165.0'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	version, err := executableVersion(ctx, path)
	if err != nil || version != "0.165.0" {
		t.Fatalf("isolated version probe failed: %q %v", version, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '"+strings.Repeat("x", 5000)+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if version, err := executableVersion(ctx, path); err == nil || version != "" {
		t.Fatal("oversized version output was accepted")
	}
}
