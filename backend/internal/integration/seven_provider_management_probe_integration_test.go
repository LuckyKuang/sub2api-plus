//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// A latency/exit-IP/quality result belongs to the proxy route that was probed,
// rather than every future route that reuses the same row ID. The production
// probe client and independent admin services exercise real TCP, PostgreSQL and
// Redis, including the completion order that previously poisoned a repaired
// proxy with an old success or old failure.
func TestSevenProviderManagementLateProxyProbeCannotReplaceRepairedRoute(t *testing.T) {
	services := newSevenBusinessServices(t)
	for _, scenario := range []struct {
		name      string
		oldStatus int
		fresh     bool
		quality   bool
	}{
		{"late-success-without-new-probe", http.StatusOK, false, false},
		{"late-success-after-new-success", http.StatusOK, true, false},
		{"late-failure-after-new-success", http.StatusBadGateway, true, false},
		{"late-quality-failure-after-new-success", http.StatusBadGateway, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			prefix := "seven-management-probe:" + uuid.NewString() + ":"
			t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
			redisA, redisB := services.namespacedRedis(t, prefix), services.namespacedRedis(t, prefix)
			proxyRepo := repository.NewProxyRepository(services.ent, services.db)
			var directTCP atomic.Int64
			destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "direct probe forbidden", http.StatusBadGateway)
			}))
			destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					directTCP.Add(1)
				}
			}
			destination.Start()
			t.Cleanup(destination.Close)
			accepted, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			releaseOld := func() { releaseOnce.Do(func() { close(release) }) }
			oldProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				close(accepted)
				select {
				case <-release:
				case <-request.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(scenario.oldStatus)
				_, _ = io.WriteString(w, `{"status":"success","query":"203.0.113.1","country":"STALE","countryCode":"ZZ","regionName":"old-region","city":"old-city"}`)
			}))
			t.Cleanup(oldProxy.Close)
			t.Cleanup(releaseOld)
			newProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"success","query":"203.0.113.2","country":"FRESH","countryCode":"SE","regionName":"new-region","city":"new-city"}`)
			}))
			t.Cleanup(newProxy.Close)
			oldURL, err := url.Parse(oldProxy.URL)
			require.NoError(t, err)
			oldPort, err := strconv.Atoi(oldURL.Port())
			require.NoError(t, err)
			proxy := &service.Proxy{Name: "seven-management-probe-" + uuid.NewString(), Protocol: "http", Host: oldURL.Hostname(), Port: oldPort, Status: service.StatusActive}
			require.NoError(t, proxyRepo.Create(ctx, proxy))
			t.Cleanup(func() {
				_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", proxy.ID)
				require.NoError(t, err)
			})
			cfg := &config.Config{}
			cfg.Security.URLAllowlist.AllowPrivateHosts = true
			cfg.Security.ProxyProbe.URLs = []config.ProbeURLConfig{{URL: destination.URL + "/probe", Parser: "ip-api"}}
			newAdmin := func(cache service.ProxyLatencyCache) service.AdminService {
				return service.NewAdminService(cfg, nil, nil, nil, proxyRepo, nil, nil, nil, nil, nil,
					repository.NewProxyExitInfoProber(cfg), cache, nil, services.ent, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			}
			adminA, adminB := newAdmin(repository.NewProxyLatencyCache(redisA)), newAdmin(repository.NewProxyLatencyCache(redisB))
			type probeResult struct {
				success bool
				err     error
			}
			done := make(chan probeResult, 1)
			go func() {
				if scenario.quality {
					result, err := adminA.CheckProxyQuality(ctx, proxy.ID)
					done <- probeResult{result != nil && result.FailedCount == 0, err}
					return
				}
				result, err := adminA.TestProxy(ctx, proxy.ID)
				done <- probeResult{result != nil && result.Success, err}
			}()
			select {
			case <-accepted:
			case <-time.After(5 * time.Second):
				t.Fatal("production probe did not reach the original proxy")
			}
			newURL, err := url.Parse(newProxy.URL)
			require.NoError(t, err)
			newPort, err := strconv.Atoi(newURL.Port())
			require.NoError(t, err)
			_, err = adminB.UpdateProxy(ctx, proxy.ID, &service.UpdateProxyInput{Host: newURL.Hostname(), Port: newPort})
			require.NoError(t, err)
			if scenario.fresh {
				result, err := adminB.TestProxy(ctx, proxy.ID)
				require.NoError(t, err)
				require.True(t, result.Success)
				require.Equal(t, "FRESH", result.Country)
			}
			releaseOld()
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.Equal(t, scenario.oldStatus == http.StatusOK, result.success)
			case <-time.After(5 * time.Second):
				t.Fatal("old probe did not finish")
			}
			for index, admin := range []service.AdminService{adminA, adminB} {
				all, err := admin.GetAllProxiesWithAccountCount(ctx)
				require.NoError(t, err)
				found := false
				for _, got := range all {
					if got.ID != proxy.ID {
						continue
					}
					found = true
					require.Equal(t, newPort, got.Port)
					require.NotEqual(t, "STALE", got.Country, "instance %d must not attach an observation made for the replaced route", index)
					if scenario.fresh {
						require.Equal(t, "FRESH", got.Country, "late old completion must preserve the repaired route's own success")
						require.Equal(t, "success", got.LatencyStatus)
						require.Empty(t, got.QualityStatus, "a quality result for the previous route cannot certify the repaired route")
					} else {
						require.Empty(t, got.Country, "the new route has no successful observation yet")
						require.Empty(t, got.LatencyStatus)
					}
				}
				require.True(t, found, fmt.Sprintf("instance %d lost the repaired proxy", index))
			}
			require.Zero(t, directTCP.Load(), "probe requests must keep their captured proxy route")
		})
	}
}

// Creation probes run asynchronously using the newly saved proxy. Their
// observation must still match the row subsequently loaded from PostgreSQL,
// including PostgreSQL's timestamp precision, and be visible on another node.
func TestSevenProviderManagementCreatedProxyBackgroundProbeVisibleOnOtherInstance(t *testing.T) {
	services := newSevenBusinessServices(t)
	ctx := context.Background()
	prefix := "seven-management-created-probe:" + uuid.NewString() + ":"
	t.Cleanup(func() { services.cleanRedisNamespace(t, prefix) })
	redisA, redisB := services.namespacedRedis(t, prefix), services.namespacedRedis(t, prefix)
	proxyRepo := repository.NewProxyRepository(services.ent, services.db)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","query":"203.0.113.3","country":"CREATED","countryCode":"SE"}`)
	}))
	t.Cleanup(proxyServer.Close)
	endpoint, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(endpoint.Port())
	require.NoError(t, err)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.ProxyProbe.URLs = []config.ProbeURLConfig{{URL: "http://probe.invalid/creation", Parser: "ip-api"}}
	newAdmin := func(cache service.ProxyLatencyCache) service.AdminService {
		return service.NewAdminService(cfg, nil, nil, nil, proxyRepo, nil, nil, nil, nil, nil,
			repository.NewProxyExitInfoProber(cfg), cache, nil, services.ent, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}
	adminA, adminB := newAdmin(repository.NewProxyLatencyCache(redisA)), newAdmin(repository.NewProxyLatencyCache(redisB))
	created, err := adminA.CreateProxy(ctx, &service.CreateProxyInput{Name: "management-created-" + uuid.NewString(), Protocol: "http", Host: endpoint.Hostname(), Port: port})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := services.db.ExecContext(ctx, "DELETE FROM proxies WHERE id=$1", created.ID)
		require.NoError(t, err)
	})
	require.Eventually(t, func() bool {
		all, err := adminB.GetAllProxiesWithAccountCount(ctx)
		if err != nil {
			return false
		}
		for _, proxy := range all {
			if proxy.ID == created.ID {
				return proxy.Country == "CREATED" && proxy.LatencyStatus == "success"
			}
		}
		return false
	}, 5*time.Second, 20*time.Millisecond, "creation's background probe must remain visible through the persisted proxy snapshot")
}
