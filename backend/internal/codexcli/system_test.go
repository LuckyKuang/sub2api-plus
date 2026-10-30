//go:build unit

package codexcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemNPMLauncherResolvesOnlyReviewedNativeExecutable(t *testing.T) {
	for _, layout := range []string{"nested", "hoisted", "embedded"} {
		t.Run(layout, func(t *testing.T) {
			m := fixtureManager(t, validFixture(t))
			root := filepath.Join(t.TempDir(), "node_modules", "@openai", "codex")
			launcher := filepath.Join(root, "bin", "codex.js")
			if err := os.MkdirAll(filepath.Dir(launcher), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(launcher, []byte("unexecuted JavaScript launcher"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"@openai/codex","version":"0.165.0"}`), 0600); err != nil {
				t.Fatal(err)
			}
			// Official rust-v0.165.0 codex-cli/bin/codex.js resolves these
			// package layouts. Build the fixture independently of the resolver.
			bases := map[string]string{
				"nested":   filepath.Join(root, "node_modules", "@openai", "codex-linux-x64"),
				"hoisted":  filepath.Join(filepath.Dir(root), "codex-linux-x64"),
				"embedded": root,
			}
			binary := filepath.Join(bases[layout], "vendor", "x86_64-unknown-linux-musl", "bin", "codex")
			if err := os.MkdirAll(filepath.Dir(binary), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte(testExecutable), 0700); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256([]byte(testExecutable))
			a := m.artifacts[m.platform]
			a.Entry = "codex-x86_64-unknown-linux-musl"
			a.BinarySHA256 = hex.EncodeToString(digest[:])
			m.artifacts[m.platform] = a
			m.lookPath = func(string) (string, error) { return launcher, nil }
			m.version = func(_ context.Context, path string) (string, error) {
				if path != binary {
					t.Errorf("npm launcher executed instead of reviewed native binary: %s", path)
				}
				return "0.165.0", nil
			}
			s := m.Status(t.Context())
			if !s.ExecutionAvailable || !s.SourceVerified || s.Path != binary || s.Source != "system" {
				t.Fatalf("reviewed npm native executable unavailable: %+v", s)
			}
			if err := os.WriteFile(binary, []byte("counterfeit native binary"), 0700); err != nil {
				t.Fatal(err)
			}
			m.version = func(context.Context, string) (string, error) {
				t.Error("counterfeit native binary or npm launcher was invoked")
				return "0.165.0", nil
			}
			if s := m.Status(t.Context()); s.ExecutionAvailable || s.SourceVerified {
				t.Fatal("npm metadata authorized a counterfeit native executable")
			}
		})
	}
}
