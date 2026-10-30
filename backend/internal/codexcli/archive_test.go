//go:build unit

package codexcli

import (
	"archive/tar"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveExtractionAllowsOnlyExpectedRegularExecutable(t *testing.T) {
	archive := validFixture(t)
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "archive.tar.gz"), filepath.Join(dir, "codex")
	if err := os.WriteFile(source, archive, 0600); err != nil {
		t.Fatal(err)
	}
	if err := extractExecutable(context.Background(), source, destination, testEntry); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(destination)
	if err != nil || string(got) != testExecutable {
		t.Fatalf("wrong extracted executable %q %v", got, err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("wrong executable permissions %v %v", info, err)
	}
	if err := extractExecutable(context.Background(), source, destination, testEntry); err == nil {
		t.Fatal("existing destination was overwritten")
	}
}

func TestArchiveExtractionRejectsUnsafeEntriesAndRemovesOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []tarEntry
	}{
		{"traversal", []tarEntry{{name: "../escape", body: "bad"}}},
		{"absolute", []tarEntry{{name: "/tmp/escape", body: "bad"}}},
		{"windows_traversal", []tarEntry{{name: "..\\escape", body: "bad"}}},
		{"symlink", []tarEntry{{name: testEntry, kind: tar.TypeSymlink, link: "../escape"}}},
		{"hardlink", []tarEntry{{name: testEntry, kind: tar.TypeLink, link: "../escape"}}},
		{"directory", []tarEntry{{name: testEntry, kind: tar.TypeDir}}},
		{"device", []tarEntry{{name: testEntry, kind: tar.TypeChar}}},
		{"fifo", []tarEntry{{name: testEntry, kind: tar.TypeFifo}}},
		{"duplicate", []tarEntry{{name: testEntry, body: testExecutable}, {name: testEntry, body: "replacement"}}},
		{"extra_file", []tarEntry{{name: testEntry, body: testExecutable}, {name: "extra", body: "bad"}}},
		{"empty_executable", []tarEntry{{name: testEntry}}},
		{"oversized_executable", []tarEntry{{name: testEntry, size: (512 << 20) + 1}}},
		{"missing_executable", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "archive.tar.gz"), filepath.Join(dir, "codex")
			if err := os.WriteFile(source, fixtureArchive(t, tc.entries...), 0600); err != nil {
				t.Fatal(err)
			}
			if err := extractExecutable(context.Background(), source, destination, testEntry); err == nil {
				t.Fatal("unsafe archive accepted")
			}
			if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed extraction retained output: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(dir, "escape")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("archive path was used as an output path")
			}
		})
	}
}

func TestArchiveExtractionChecksCompressionTrailer(t *testing.T) {
	for _, tc := range []string{"truncated", "bad_checksum"} {
		t.Run(tc, func(t *testing.T) {
			archive := validFixture(t)
			if tc == "truncated" {
				archive = archive[:len(archive)-1]
			} else {
				archive[len(archive)-8] ^= 1
			}
			dir := t.TempDir()
			source, destination := filepath.Join(dir, "archive.tar.gz"), filepath.Join(dir, "codex")
			if err := os.WriteFile(source, archive, 0600); err != nil {
				t.Fatal(err)
			}
			if err := extractExecutable(context.Background(), source, destination, testEntry); err == nil {
				t.Fatal("invalid gzip stream accepted")
			}
			if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid gzip retained executable")
			}
		})
	}
}

func TestArchiveVerificationAndExtractionRespectCancellation(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	seedManaged(t, m, archive, testExecutable)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyArchive(ctx, filepath.Join(m.installDir(), archiveName), m.artifacts[m.platform]); !errors.Is(err, context.Canceled) {
		t.Fatalf("archive cancellation = %v", err)
	}
	destination := filepath.Join(t.TempDir(), "codex")
	if err := extractExecutable(ctx, filepath.Join(m.installDir(), archiveName), destination, testEntry); !errors.Is(err, context.Canceled) {
		t.Fatalf("extraction cancellation = %v", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled extraction retained executable")
	}
}

func TestArchiveSymlinkIsNotAcceptedAsManagedProvenance(t *testing.T) {
	archive := validFixture(t)
	m := fixtureManager(t, archive)
	seedManaged(t, m, archive, testExecutable)
	source := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(source, archive, 0600); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(m.installDir(), archiveName)
	if err := os.Remove(managed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, managed); err != nil {
		t.Fatal(err)
	}
	if _, err := verifiedInstallationDigest(context.Background(), m.installDir(), m.artifacts[m.platform]); err == nil {
		t.Fatal("symlink provenance archive accepted")
	}
}
