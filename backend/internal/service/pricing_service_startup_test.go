//go:build unit || !integration

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pricingmanifest"
	"github.com/stretchr/testify/require"
)

const (
	startupPricingHost    = "pricing.example.test"
	startupPricingVersion = "v0.2.22+custom.001"
)

// blockingManifestRemoteClient 一直阻塞到 release 关闭，模拟远端不可达时等满超时。
type blockingManifestRemoteClient struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingManifestRemoteClient) FetchPricingJSON(ctx context.Context, _ string, _ int64) ([]byte, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return nil, context.DeadlineExceeded
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// failingManifestRemoteClient 的所有请求立即失败，并统计调用次数。
type failingManifestRemoteClient struct {
	mu    sync.Mutex
	calls int
}

func (c *failingManifestRemoteClient) FetchPricingJSON(context.Context, string, int64) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return nil, errors.New("release endpoint unreachable")
}

func (c *failingManifestRemoteClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// releaseRemoteClient 按 URL 返回清单与价格正文，模拟 immutable Release 资产。
type releaseRemoteClient struct {
	mu          sync.Mutex
	manifestURL string
	manifest    []byte
	pricingURL  string
	pricingBody []byte
}

func (c *releaseRemoteClient) FetchPricingJSON(_ context.Context, url string, _ int64) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch url {
	case c.manifestURL:
		return append([]byte(nil), c.manifest...), nil
	case c.pricingURL:
		return append([]byte(nil), c.pricingBody...), nil
	default:
		return nil, fmt.Errorf("unexpected pricing URL %q", url)
	}
}

func startupManifest(version, body string) (pricingmanifest.Manifest, []byte) {
	digest := sha256.Sum256([]byte(body))
	manifest := pricingmanifest.Manifest{
		Version:    version,
		PricingURL: "https://" + startupPricingHost + "/releases/download/" + version + "/model-pricing.json",
		SHA256:     hex.EncodeToString(digest[:]),
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		panic(err)
	}
	return manifest, raw
}

func newStartupPricingService(t *testing.T, remote PricingRemoteClient) *PricingService {
	t.Helper()
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{
		DataDir:     t.TempDir(),
		ManifestURL: "https://" + startupPricingHost + "/releases/latest/download/manifest.json",
	}}, remote)
	svc.cfg.Security.URLAllowlist.PricingHosts = []string{startupPricingHost}
	require.NoError(t, os.WriteFile(svc.getPricingFilePath(), []byte(hotReloadCatalogJSON), 0644))
	return svc
}

// seedValidatedCache 写入一份受信任缓存（清单摘要与数据一致），启动可立即使用。
func seedValidatedCache(t *testing.T, svc *PricingService, version, body string) pricingmanifest.Manifest {
	t.Helper()
	manifest, raw := startupManifest(version, body)
	require.NoError(t, svc.writeValidatedPricingCache([]byte(body), releasePricingManifest{manifest: manifest, raw: raw}))
	return manifest
}

// 远端清单请求挂住时，启动不得等待：验证缓存已可计费，刷新在后台继续。
func TestPricingStartupDoesNotBlockOnRemoteManifest(t *testing.T) {
	remote := &blockingManifestRemoteClient{started: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(func() { close(remote.release) })
	svc := newStartupPricingService(t, remote)
	seedValidatedCache(t, svc, startupPricingVersion, hotReloadCatalogJSON)

	done := make(chan error, 1)
	go func() { done <- svc.checkAndUpdatePricing() }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("startup must not block on the remote manifest fetch")
	}
	require.NotNil(t, svc.GetModelPricing("remote-model"), "验证缓存启动即可用")
	select {
	case <-remote.started:
	case <-time.After(2 * time.Second):
		t.Fatal("release refresh must still run in the background")
	}
}

// 后台校验发现严格更新的 Release 时仍要下载、替换并持久化价格表。
func TestPricingStartupBackgroundRefreshInstallsNewerRelease(t *testing.T) {
	const nextVersion = "v0.2.22+custom.002"
	newBody := `{` + hotReloadModelJSON("remote-model", 3e-6, 6e-6) + `}`
	manifest, raw := startupManifest(nextVersion, newBody)
	remote := &releaseRemoteClient{
		manifestURL: "https://" + startupPricingHost + "/releases/latest/download/manifest.json",
		manifest:    raw,
		pricingURL:  manifest.PricingURL,
		pricingBody: []byte(newBody),
	}
	svc := newStartupPricingService(t, remote)
	seedValidatedCache(t, svc, startupPricingVersion, hotReloadCatalogJSON)

	require.NoError(t, svc.checkAndUpdatePricing())
	require.Eventually(t, func() bool {
		p := svc.GetModelPricing("remote-model")
		return p != nil && p.InputCostPerToken == 3e-6
	}, 2*time.Second, 10*time.Millisecond, "后台校验应下载并替换价格表")

	svc.mu.RLock()
	require.Equal(t, nextVersion, svc.localVersion)
	svc.mu.RUnlock()
	saved, err := os.ReadFile(svc.getPricingFilePath())
	require.NoError(t, err)
	require.Equal(t, newBody, string(saved))
}

// 后台清单获取失败时沿用已加载的验证缓存，只记日志。
func TestPricingStartupBackgroundRefreshKeepsValidatedCache(t *testing.T) {
	logs, restore := captureStructuredLog(t)
	defer restore()
	remote := &failingManifestRemoteClient{}
	svc := newStartupPricingService(t, remote)
	seedValidatedCache(t, svc, startupPricingVersion, hotReloadCatalogJSON)

	require.NoError(t, svc.checkAndUpdatePricing())

	require.Eventually(t, func() bool {
		return logs.ContainsMessage("Release refresh failed; keeping validated local cache: download pricing manifest: release endpoint unreachable")
	}, 2*time.Second, 10*time.Millisecond, "刷新失败应记日志")
	require.Equal(t, 1, remote.count())
	p := svc.GetModelPricing("remote-model")
	require.NotNil(t, p)
	require.InDelta(t, 1e-6, p.InputCostPerToken, 1e-12)
}

// 未验证的本地价格文件不能降级到后台：受信任 Release 必须先替换它，否则启动报错。
func TestPricingStartupUnvalidatedLocalFileStillRequiresRelease(t *testing.T) {
	remote := &failingManifestRemoteClient{}
	svc := newStartupPricingService(t, remote)

	err := svc.checkAndUpdatePricing()
	require.Error(t, err)
	require.Contains(t, err.Error(), "unvalidated local cache")
	require.Positive(t, remote.count())
	// 本地文件仍保持可用，只是启动不宣称已获得受信任版本。
	require.NotNil(t, svc.GetModelPricing("remote-model"))
}

// 停止后不再发起后台刷新，且重复 Stop 不会重复关闭停止信号。
func TestPricingServiceStopIsIdempotentAndStopsBackgroundRefresh(t *testing.T) {
	remote := &failingManifestRemoteClient{}
	svc := newStartupPricingService(t, remote)
	seedValidatedCache(t, svc, startupPricingVersion, hotReloadCatalogJSON)

	svc.Stop()
	svc.Stop()

	svc.refreshReleaseInBackground()
	require.Never(t, func() bool { return remote.count() > 0 }, 100*time.Millisecond, 10*time.Millisecond,
		"a stopped service must not start a release refresh")
}
