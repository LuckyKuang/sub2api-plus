package codexcli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	maxArchiveBytes    int64 = 256 << 20
	maxExecutableBytes int64 = 512 << 20
)

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func regularFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("CLI installation contains a non-regular file")
	}
	return os.Open(path)
}

func verifyArchive(ctx context.Context, path string, a artifact) error {
	f, err := regularFile(path)
	if err != nil {
		return fmt.Errorf("open reviewed CLI archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() != a.Size || info.Size() > maxArchiveBytes {
		return errors.New("CLI archive does not match reviewed size")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, a.Size+1))
	if err != nil {
		return fmt.Errorf("verify CLI archive: %w", err)
	}
	if n != a.Size {
		return errors.New("CLI archive changed size during verification")
	}
	if hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return errors.New("CLI archive does not match reviewed SHA-256")
	}
	return nil
}

// readExecutable accepts one reviewed regular-file entry only. It never writes
// a path taken from the archive, and rejects links, directories, extra files,
// special files, traversal, duplicates, and oversized unpacked executables.
func readExecutable(ctx context.Context, archivePath, expectedEntry string, out io.Writer) error {
	f, err := regularFile(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(contextReader{ctx, f})
	if err != nil {
		return fmt.Errorf("open CLI archive compression: %w", err)
	}
	defer func() { _ = gz.Close() }()
	unpacked := &io.LimitedReader{R: gz, N: maxExecutableBytes + (1 << 20)}
	tr := tar.NewReader(unpacked)
	found := false
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read CLI archive: %w", err)
		}
		// tar.Reader normalizes legacy regular entries to TypeReg.
		if found || header.Name != expectedEntry || filepath.Base(header.Name) != header.Name || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxExecutableBytes {
			return errors.New("CLI archive contains an unexpected or unsafe entry")
		}
		if _, err := io.Copy(out, contextReader{ctx, tr}); err != nil {
			return fmt.Errorf("read CLI executable: %w", err)
		}
		found = true
	}
	if !found {
		return errors.New("CLI archive has no executable")
	}
	// Consume through gzip's trailer so truncated compressed streams and a bad
	// gzip checksum cannot be accepted after tar's end-of-archive marker.
	if _, err := io.Copy(io.Discard, contextReader{ctx, unpacked}); err != nil {
		return fmt.Errorf("finish CLI archive: %w", err)
	}
	if unpacked.N <= 0 {
		return errors.New("CLI archive exceeds unpacked size limit")
	}
	return nil
}

func extractExecutable(ctx context.Context, archivePath, destination, expectedEntry string) (err error) {
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return fmt.Errorf("create staged CLI executable: %w", err)
	}
	defer func() {
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(destination)
		}
	}()
	return readExecutable(ctx, archivePath, expectedEntry, f)
}

func verifySystemExecutable(ctx context.Context, path, pinnedDigest string) ([]byte, error) {
	expected, err := hex.DecodeString(pinnedDigest)
	if err != nil || len(expected) != sha256.Size {
		return nil, errors.New("reviewed system CLI binary digest is unavailable")
	}
	binary, err := regularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = binary.Close() }()
	info, err := binary.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maxExecutableBytes || info.Mode()&0111 == 0 {
		return nil, errors.New("system CLI executable metadata is invalid")
	}
	actual := sha256.New()
	n, err := io.Copy(actual, io.LimitReader(contextReader{ctx, binary}, maxExecutableBytes+1))
	if err != nil {
		return nil, err
	}
	if n != info.Size() {
		return nil, errors.New("system CLI executable changed size during verification")
	}
	if !equalDigest(actual.Sum(nil), expected) {
		return nil, errors.New("system CLI binary does not match the reviewed digest")
	}
	return actual.Sum(nil), nil
}

func verifiedInstallationDigest(ctx context.Context, directory string, a artifact) ([]byte, error) {
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("CLI installation directory is invalid")
	}
	archivePath := filepath.Join(directory, archiveName)
	if err := verifyArchive(ctx, archivePath, a); err != nil {
		return nil, err
	}
	binary, err := regularFile(filepath.Join(directory, "codex"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = binary.Close() }()
	info, err = binary.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > maxExecutableBytes || info.Mode()&0111 == 0 {
		return nil, errors.New("CLI executable metadata is invalid")
	}
	actual := sha256.New()
	n, err := io.Copy(actual, io.LimitReader(contextReader{ctx, binary}, maxExecutableBytes+1))
	if err != nil {
		return nil, err
	}
	if n != info.Size() {
		return nil, errors.New("CLI executable changed size during verification")
	}
	expected := sha256.New()
	if err := readExecutable(ctx, archivePath, a.Entry, expected); err != nil {
		return nil, err
	}
	if !equalDigest(actual.Sum(nil), expected.Sum(nil)) {
		return nil, errors.New("CLI executable differs from the reviewed archive")
	}
	return actual.Sum(nil), nil
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for i := range a {
		different |= a[i] ^ b[i]
	}
	return different == 0
}
