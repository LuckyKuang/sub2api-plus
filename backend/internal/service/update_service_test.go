//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
	latestErr      error
	downloadFunc   func(context.Context, string, string, int64) error
	checksumData   []byte
	checksumErr    error
	latestRepo     string
	recentRepo     string
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.latestRepo = repo
	return s.release, s.latestErr
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(_ context.Context, repo string, _ int) ([]*GitHubRelease, error) {
	s.recentRepo = repo
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	if s.downloadFunc != nil {
		return s.downloadFunc(ctx, url, dest, maxSize)
	}
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	if s.checksumData != nil || s.checksumErr != nil {
		return s.checksumData, s.checksumErr
	}
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132+custom.001",
				Name:    "v0.1.132+custom.001",
			},
		},
		"0.1.132+custom.001",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServicePerformUpdatePropagatesLatestReleaseFetchFailure(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{latestErr: errors.New("github unavailable")},
		"0.1.132+custom.001",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to check latest release")
	require.ErrorContains(t, err, "github unavailable")
	require.NotErrorIs(t, err, ErrNoUpdateAvailable)
}

func TestUpdateServiceDownloadAndVerifyArchiveUsesAssetNameWhenURLIsEncoded(t *testing.T) {
	archiveName := fmt.Sprintf("sub2api_0.1.169+custom.001_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	downloadURL := fmt.Sprintf("https://github.com/LuckyKuang/sub2api-plus/releases/download/v0.1.169%%2Bcustom.001/sub2api_0.1.169%%2Bcustom.001_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	payload := []byte("release archive")
	expectedHash := fmt.Sprintf("%x", sha256.Sum256(payload))

	var requestedURL string
	var downloadedPath string
	github := &updateServiceGitHubClientStub{
		checksumData: []byte(fmt.Sprintf("%s  %s\n", expectedHash, archiveName)),
		downloadFunc: func(_ context.Context, url, dest string, _ int64) error {
			requestedURL = url
			downloadedPath = dest
			return os.WriteFile(dest, payload, 0o600)
		},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, github, "0.1.166+custom.009", "release")

	archivePath, err := svc.downloadAndVerifyArchive(context.Background(), t.TempDir(), archiveName, downloadURL, "https://github.com/checksums.txt")

	require.NoError(t, err)
	require.Equal(t, downloadURL, requestedURL)
	require.Equal(t, archiveName, filepath.Base(downloadedPath))
	require.Equal(t, archivePath, downloadedPath)
}

func TestUpdateServiceDownloadAndVerifyArchiveRejectsActualChecksumMismatch(t *testing.T) {
	archiveName := fmt.Sprintf("sub2api_0.1.169+custom.001_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	github := &updateServiceGitHubClientStub{
		checksumData: []byte(fmt.Sprintf("%064x  %s\n", 0, archiveName)),
		downloadFunc: func(_ context.Context, _ string, dest string, _ int64) error {
			return os.WriteFile(dest, []byte("tampered release archive"), 0o600)
		},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, github, "0.1.166+custom.009", "release")

	_, err := svc.downloadAndVerifyArchive(context.Background(), t.TempDir(), archiveName, "https://github.com/download", "https://github.com/checksums.txt")

	require.Error(t, err)
	require.ErrorContains(t, err, "checksum verification failed: checksum mismatch")
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148+custom.001", PublishedAt: "2026-07-09T00:00:00Z"},                   // newer than current: excluded
		{TagName: "v0.1.147+custom.002", PublishedAt: "2026-07-08T00:00:00Z"},                   // current: excluded
		{TagName: "v0.1.146+custom.001", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146+custom.001", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145+custom.001", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144+custom.003", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144+custom.003", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143+custom.001", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142+custom.001", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147+custom.002", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146+custom.001", versions[0].Version)
	require.Equal(t, "0.1.144+custom.003", versions[1].Version)
	require.Equal(t, "0.1.143+custom.001", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144+custom.001"},
		{TagName: "v0.1.146+custom.001"},
		{TagName: "v0.1.145+custom.001"},
	}
	svc := newRollbackTestService("0.1.147+custom.001", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146+custom.001", versions[0].Version)
	require.Equal(t, "0.1.145+custom.001", versions[1].Version)
	require.Equal(t, "0.1.144+custom.001", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147+custom.001"},
		{TagName: "v0.1.148+custom.001"},
	}
	svc := newRollbackTestService("0.1.147+custom.001", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147+custom.001",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148+custom.001"},
		{TagName: "v0.1.147+custom.002"},
		{TagName: "v0.1.146+custom.001"},
		{TagName: "v0.1.145+custom.001"},
		{TagName: "v0.1.144+custom.001"},
		{TagName: "v0.1.143+custom.001"},
		{TagName: "v0.1.142+custom.001"},
	}
	svc := newRollbackTestService("0.1.147+custom.002", releases)

	for _, target := range []string{
		"",                    // empty
		"0.1.147+custom.002",  // current version
		"v0.1.147+custom.002", // current version with prefix
		"0.1.148+custom.001",  // newer than current
		"0.1.142+custom.001",  // older than the 3 most recent
		"9.9.9+custom.001",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147+custom.001"},
		{TagName: "v0.1.146+custom.001"},
	}
	svc := newRollbackTestService("0.1.147+custom.001", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146+custom.001")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

func TestUpdateServiceUsesForkRepositoryAndCustomIteration(t *testing.T) {
	github := &updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v0.1.164+custom.002"},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, github, "0.1.164+custom.001", "release")

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, githubRepo, github.latestRepo)
	require.Equal(t, "0.1.164+custom.002", info.LatestVersion)
	require.True(t, info.HasUpdate)
}

func TestUpdateServiceUsesForkRepositoryForRollbackCandidates(t *testing.T) {
	github := &updateServiceGitHubClientStub{
		recentReleases: []*GitHubRelease{{TagName: "v0.1.163+custom.001"}},
	}
	svc := NewUpdateService(&updateServiceCacheStub{}, github, "0.1.164+custom.001", "release")

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, githubRepo, github.recentRepo)
}

func TestUpdateServiceArchiveNameRetainsCustomVersion(t *testing.T) {
	svc := NewUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{}, "0.1.164+custom.001", "release")

	archiveName := svc.getArchiveName("v0.1.164+custom.001")

	require.Equal(
		t,
		fmt.Sprintf("sub2api_0.1.164+custom.001_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH),
		archiveName,
	)
}

func TestUpdateVersionComparison(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    int
	}{
		{current: "0.1.164+custom.001", latest: "0.1.164+custom.002", want: -1},
		{current: "0.1.164+custom.002", latest: "0.1.164+custom.001", want: 1},
		{current: "0.1.164+custom.999", latest: "0.1.165+custom.001", want: -1},
		{current: "0.1.164", latest: "0.1.164+custom.001", want: -1},
		{current: "v0.1.164+custom.001", latest: "0.1.164+custom.001", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.current+"_to_"+tt.latest, func(t *testing.T) {
			require.Equal(t, tt.want, compareVersions(tt.current, tt.latest))
		})
	}
}

func TestUpdateServiceRejectsNonForkLatestRelease(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v0.1.164"}},
		"0.1.164+custom.001",
		"release",
	)

	_, err := svc.fetchLatestRelease(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "valid fork version")
}
