//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

func TestStepFunOAuthCatalogBeforeCreation(t *testing.T) {
	for _, region := range []string{"cn", "global"} {
		t.Run(region, func(t *testing.T) {
			admin := &cnOAuthAdminStub{}
			proxyID := int64(9)
			proxyRepo := &mockProxyRepoForOAuth{getByIDFunc: func(_ context.Context, id int64) (*Proxy, error) {
				require.Equal(t, proxyID, id)
				return &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}, nil
			}}
			svc := NewCNOAuthService(proxyRepo, nil, admin)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"step-3.7-flash","model_type":"大语言模型"},{"id":"step-router","model_type":"路由模型"},{"id":"step-image","model_type":"图片模型"}]}`))}}
			svc.modelTester = &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
			ctx := context.Background()
			id := cnOAuthReady(t, svc, "stepfun", 0)
			require.NoError(t, svc.store.Update(ctx, id, func(_ context.Context, session *cnoauth.Session) error {
				session.Flow.Region = region
				session.ProxyID = &proxyID
				return nil
			}))
			models, err := svc.PreviewModels(ctx, 7, "stepfun", id)
			require.NoError(t, err)
			require.Equal(t, []string{"step-3.7-flash", "step-router"}, models)
			host := "api.stepfun.com"
			if region == "global" {
				host = "api.stepfun.ai"
			}
			require.Equal(t, "https://"+host+"/step_plan/v1/models", upstream.lastReq.URL.String())
			require.Equal(t, "Bearer private-grant", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, "http://proxy.example:8080", upstream.lastProxyURL)
			requireStepFunWire(t, upstream.lastReq, false)
			require.Zero(t, admin.writes, "catalog discovery must not create an account")
			_, err = svc.Complete(ctx, 7, "stepfun", id, CNOAuthCompleteInput{ModelMapping: map[string]string{"step-3.7-flash": "step-3.7-flash"}})
			require.NoError(t, err)
			require.Equal(t, map[string]any{"step-3.7-flash": "step-3.7-flash"}, admin.input.Credentials["model_mapping"])
			_, err = svc.PreviewModels(ctx, 7, "stepfun", id)
			require.Error(t, err, "consumed grants cannot fetch models")
			require.Len(t, upstream.requests, 1)
		})
	}
}

func TestStepFunOAuthCatalogRejectsInvalidSessionBeforeUpstream(t *testing.T) {
	for _, scenario := range []string{"owner", "platform", "pending", "cancelled", "expired", "grant-expired", "committing"} {
		t.Run(scenario, func(t *testing.T) {
			svc := NewCNOAuthService(nil, nil, nil)
			upstream := &httpUpstreamRecorder{}
			svc.modelTester = &AccountTestService{cfg: &config.Config{}, httpUpstream: upstream}
			id := cnOAuthReady(t, svc, "stepfun", 0)
			owner, platform := int64(7), "stepfun"
			require.NoError(t, svc.store.Update(context.Background(), id, func(_ context.Context, session *cnoauth.Session) error {
				switch scenario {
				case "owner":
					owner = 8
				case "platform":
					platform = "kimi"
				case "pending":
					session.Grant = nil
				case "cancelled":
					session.Cancelled = true
				case "expired":
					session.Flow.ExpiresAt = time.Now().Add(-time.Second)
				case "grant-expired":
					session.Grant.ExpiresAt = time.Now().Add(-time.Second)
				case "committing":
					session.Committing = true
				}
				return nil
			}))
			models, err := svc.PreviewModels(context.Background(), owner, platform, id)
			require.Error(t, err)
			require.Nil(t, models)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestStepFunOAuthCatalogFailureKeepsGrantUsable(t *testing.T) {
	admin := &cnOAuthAdminStub{}
	svc := NewCNOAuthService(nil, nil, admin)
	svc.modelTester = &AccountTestService{cfg: &config.Config{}, httpUpstream: &httpUpstreamRecorder{err: errors.New("private-grant transport detail")}}
	id := cnOAuthReady(t, svc, "stepfun", 0)
	_, err := svc.PreviewModels(context.Background(), 7, "stepfun", id)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-grant")
	_, err = svc.Complete(context.Background(), 7, "stepfun", id, CNOAuthCompleteInput{})
	require.NoError(t, err)
	require.NotContains(t, admin.input.Credentials, "model_mapping", "no selection means unrestricted")
}

type stepFunCatalogReadHook struct {
	io.Reader
	beforeRead func()
}

func (r *stepFunCatalogReadHook) Read(p []byte) (int, error) {
	if r.beforeRead != nil {
		r.beforeRead()
		r.beforeRead = nil
	}
	return r.Reader.Read(p)
}

func TestStepFunOAuthCatalogDiscardsResponseAfterCancellation(t *testing.T) {
	svc := NewCNOAuthService(nil, nil, nil)
	id := cnOAuthReady(t, svc, "stepfun", 0)
	body := &stepFunCatalogReadHook{Reader: strings.NewReader(`{"data":[{"id":"step-3.7-flash"}]}`), beforeRead: func() {
		_, err := svc.Advance(context.Background(), 7, "stepfun", id, "", true)
		require.NoError(t, err)
	}}
	svc.modelTester = &AccountTestService{cfg: &config.Config{}, httpUpstream: &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(body)}}}
	models, err := svc.PreviewModels(context.Background(), 7, "stepfun", id)
	require.Error(t, err)
	require.Nil(t, models)
}

func TestStepFunModelRestrictionControlsActualAccountSelection(t *testing.T) {
	for _, kind := range []string{"oauth", "apikey"} {
		for _, requested := range []string{"step-3.7-flash", "public-step"} {
			t.Run(kind+"/"+requested, func(t *testing.T) {
				mapping := map[string]string{requested: "step-3.7-flash"}
				account := stepFunTestAccount(kind, "cn")
				if kind == "oauth" {
					admin := &cnOAuthAdminStub{}
					svc := NewCNOAuthService(nil, nil, admin)
					id := cnOAuthReady(t, svc, "stepfun", 0)
					_, err := svc.Complete(context.Background(), 7, "stepfun", id, CNOAuthCompleteInput{ModelMapping: mapping})
					require.NoError(t, err)
					account.Credentials = admin.input.Credentials
				} else {
					stored := map[string]any{}
					for from, to := range mapping {
						stored[from] = to
					}
					account.Credentials["model_mapping"] = stored
				}
				account.Status, account.Schedulable = StatusActive, true
				repo := &mockAccountRepoForPlatform{accounts: []Account{*account}, accountsByID: map[int64]*Account{account.ID: account}}
				gateway := &GatewayService{accountRepo: repo, cache: &mockGatewayCacheForPlatform{}, cfg: testConfig()}
				for requested := range mapping {
					selected, err := gateway.selectAccountForModelWithPlatform(context.Background(), nil, "", requested, nil, "stepfun")
					require.NoError(t, err)
					require.Equal(t, account.ID, selected.ID)
					require.Equal(t, "step-3.7-flash", selected.GetMappedModel(requested))
				}
				selected, err := gateway.selectAccountForModelWithPlatform(context.Background(), nil, "", "step-not-selected", nil, "stepfun")
				require.Error(t, err)
				require.Nil(t, selected)
			})
		}
	}
}
