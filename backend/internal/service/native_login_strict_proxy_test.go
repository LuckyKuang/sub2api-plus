//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type nativeLoginProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
	calls int
}

func (r *nativeLoginProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	r.calls++
	return r.proxy, r.err
}

func TestNativeLoginExplicitProxyMustResolveBeforeStart(t *testing.T) {
	for _, failure := range []string{"no-repository", "deleted", "lookup-error", "wrong-row", "invalid-host", "invalid-port", "invalid-protocol", "invalid-id"} {
		for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
			t.Run(platform+"/"+failure, func(t *testing.T) {
				id := int64(7)
				selected := &nativeLoginProxyRepo{}
				switch failure {
				case "lookup-error":
					selected.err = errors.New("private database detail")
				case "wrong-row":
					selected.proxy = &Proxy{ID: 8, Protocol: "http", Host: "proxy.invalid", Port: 8080}
				case "invalid-host":
					selected.proxy = &Proxy{ID: 7, Protocol: "http", Port: 8080}
				case "invalid-port":
					selected.proxy = &Proxy{ID: 7, Protocol: "http", Host: "proxy.invalid", Port: 0}
				case "invalid-protocol":
					selected.proxy = &Proxy{ID: 7, Protocol: "file", Host: "proxy.invalid", Port: 8080}
				case "invalid-id":
					id = 0
				}
				var proxyRepo ProxyRepository = selected
				if failure == "no-repository" {
					proxyRepo = nil
				}
				var err error
				if platform == PlatformZhipu {
					client := &zhipuOAuthStubClient{initFlow: zhipuTestFlowInit()}
					svc := NewZhipuOAuthService(client, proxyRepo)
					defer svc.Stop()
					_, err = svc.StartLink(context.Background(), StartZhipuLinkInput{Provider: zcode.ProviderZai, ProxyID: &id})
					require.Empty(t, client.identities, "the handshake must not start without its selected route")
				} else {
					svc := NewCNOAuthService(proxyRepo, nil, nil)
					calls := 0
					svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected Start HTTP") })}
					_, err = svc.Start(context.Background(), 7, platform, "cn", &id, 0)
					require.Zero(t, calls, "native login must not fall back to host networking")
				}
				require.Error(t, err)
				var status *infraerrors.ApplicationError
				require.ErrorAs(t, err, &status)
				expected := "CN_OAUTH_PROXY_UNAVAILABLE"
				if platform == PlatformZhipu {
					expected = "ZHIPU_OAUTH_PROXY_NOT_FOUND"
				}
				if strings.HasPrefix(failure, "invalid-") && failure != "invalid-id" || failure == "wrong-row" {
					expected = "CN_OAUTH_PROXY_INVALID"
					if platform == PlatformZhipu {
						expected = "ZHIPU_OAUTH_PROXY_INVALID"
					}
				}
				require.Equal(t, expected, status.Reason)
				require.NotContains(t, err.Error(), "private database detail")
			})
		}
	}
}

func TestNativeAccountReloginUsesHydratedOwnerWithoutProxyReread(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing-route", true: "captured-route"}[available], func(t *testing.T) {
			account := cnRecoveryAccount(PlatformKimi)
			id := int64(7)
			account.ProxyID = &id
			if available {
				account.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "original.example", Port: 8080}
			}
			proxyRepo := &nativeLoginProxyRepo{proxy: &Proxy{ID: 7, Protocol: "http", Host: "replacement.example", Port: 8888}}
			svc := NewCNOAuthService(proxyRepo, &refreshAPIAccountRepo{account: account}, nil)
			calls := 0
			svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				calls++
				return cnRecoveryResponse(http.StatusOK, `{"device_code":"fake-device","user_code":"fake-code","verification_uri_complete":"https://auth.kimi.com/verify?user_code=fake-code","expires_in":300,"interval":2}`), nil
			})}
			view, err := svc.Start(context.Background(), 7, PlatformKimi, "cn", nil, account.ID)
			require.Zero(t, proxyRepo.calls, "a current operation cannot replace its owner's hydrated proxy with a mutable row")
			if !available {
				require.Error(t, err)
				require.Zero(t, calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.NoError(t, svc.store.Update(context.Background(), view.SessionID, func(_ context.Context, session *cnoauth.Session) error {
				require.Equal(t, "http://original.example:8080", session.ProxyURL)
				return nil
			}))
		})
	}
}

func TestNativePreviewModelsRetainsFrozenLoginProxy(t *testing.T) {
	for _, route := range []string{"http://owner:fake-password@original.example:8080", "", "file://original.example:8080"} {
		t.Run(route, func(t *testing.T) {
			svc := NewCNOAuthService(nil, nil, nil)
			upstream := &httpUpstreamRecorder{resp: cnRecoveryResponse(http.StatusOK, `{"data":[{"id":"step-3.7-flash","model_type":"大语言模型"}]}`)}
			svc.modelTester = &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
			id := cnOAuthReady(t, svc, PlatformStepFun, 0)
			proxyID := int64(7)
			require.NoError(t, svc.store.Update(context.Background(), id, func(_ context.Context, session *cnoauth.Session) error {
				session.ProxyID = &proxyID
				session.ProxyURL = route
				return nil
			}))
			models, err := svc.PreviewModels(context.Background(), 7, PlatformStepFun, id)
			if route == "" || strings.HasPrefix(route, "file:") {
				require.Error(t, err)
				require.Empty(t, upstream.requests)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"step-3.7-flash"}, models)
			require.Equal(t, route, upstream.lastProxyURL)
		})
	}
}
