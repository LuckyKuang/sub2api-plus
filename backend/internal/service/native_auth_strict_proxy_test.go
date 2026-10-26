//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

// Even an executor that knows nothing about proxy policy must never be invoked
// by the shared refresh coordinator with a missing or malformed account route.
func TestOAuthRefreshCoordinatorRejectsUnavailableProxyForSevenProviders(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, failure := range []string{"missing", "wrong-row", "empty-host", "invalid-port", "invalid-protocol", "deleted-after-selection"} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				id := int64(7)
				selected := &Account{ID: 883014, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive,
					Credentials: map[string]any{"access_token": "fake-access", "refresh_token": "fake-refresh"}, ProxyID: &id,
					Proxy: &Proxy{ID: 7, Protocol: "http", Host: "proxy.invalid", Port: 8080, Status: StatusActive}}
				fresh := snapshotOAuthRefreshAccount(selected)
				switch failure {
				case "missing", "deleted-after-selection":
					fresh.Proxy = nil
				case "wrong-row":
					fresh.Proxy.ID = 8
				case "empty-host":
					fresh.Proxy.Host = ""
				case "invalid-port":
					fresh.Proxy.Port = 0
				case "invalid-protocol":
					fresh.Proxy.Protocol = "not-a-proxy"
				}
				if failure != "deleted-after-selection" {
					selected = snapshotOAuthRefreshAccount(fresh)
				}
				repo := &refreshAPIAccountRepo{account: fresh}
				executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: map[string]any{"access_token": "forbidden-rotation"}}
				_, err := NewOAuthRefreshAPI(repo, nil).RefreshIfNeeded(context.Background(), selected, executor, time.Minute)
				require.Error(t, err)
				require.Zero(t, executor.refreshCalls, "missing configured egress must fail before platform refresh")
				require.Zero(t, repo.updateCalls, "routing failure cannot persist or rotate credentials")
				require.Zero(t, repo.successCASCalls)
				require.Equal(t, "fake-access", repo.account.GetCredential("access_token"))
			})
		}
	}
}

func TestNativeTokenRefresherRejectsMissingProxyBeforeHTTP(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			account := cnRecoveryAccount(platform)
			id := int64(7)
			account.ProxyID = &id
			calls := 0
			client := &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("must not send") })}
			_, err := (&CNTokenRefresher{platform: platform, client: client}).Refresh(context.Background(), account)
			require.Error(t, err)
			require.Zero(t, calls, "native refresh must not turn missing proxy hydration into direct traffic")
		})
	}
}

func TestZhipuProjectTokenRejectsMissingProxyIncludingWarmCache(t *testing.T) {
	for _, warm := range []bool{false, true} {
		t.Run(fmt.Sprintf("warm-%v", warm), func(t *testing.T) {
			client := &projectTokenTestClient{}
			service := newProjectTokenTestService(t, client)
			account := projectTokenTestAccount(883015)
			id := int64(7)
			account.ProxyID = &id
			if warm {
				account.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "proxy.invalid", Port: 8080, Status: StatusActive}
				credential, err := service.credential(context.Background(), account, false)
				require.NoError(t, err)
				require.NotEmpty(t, credential.APIKey)
				account.Proxy = nil
			}
			before := client.calls
			credential, err := service.credential(context.Background(), account, false)
			require.Error(t, err)
			require.Nil(t, credential, "a cached PAT cannot authorize traffic whose configured route disappeared")
			require.Equal(t, before, client.calls, "missing route must stop before any Z.ai exchange or project-token issuance")
		})
	}
}
