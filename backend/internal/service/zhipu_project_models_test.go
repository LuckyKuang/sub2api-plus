//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

func TestZhipuProjectModelsDiscoveryUsesCurrentPAT(t *testing.T) {
	for _, tc := range []struct {
		name, provider, plan, target string
		team                         bool
	}{
		{"bigmodel-personal", "bigmodel", ZhipuPlanIndividualCodingPlan, "https://open.bigmodel.cn/api/coding/paas/v4/models", false},
		{"zai-personal", "zai", ZhipuPlanIndividualCodingPlan, "https://api.z.ai/api/coding/paas/v4/models", false},
		{"bigmodel-team", "bigmodel", ZhipuPlanTeamCodingPlan, "https://open.bigmodel.cn/api/coding/paas/v4/models", true},
		{"zai-team", "zai", ZhipuPlanTeamCodingPlan, "https://api.z.ai/api/coding/paas/v4/models", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			a := f.repo.account
			a.Credentials["oauth_provider"], a.Credentials["plan_kind"] = tc.provider, tc.plan
			a.Credentials["api_protocol"] = APIProtocolAnthropic
			a.Credentials[outboundIdentityCredential] = map[string]any{"preset": "zcode", "version": "0.16.9"}
			f.issuer.issue = func(ctx context.Context, login string) (*zcode.ProjectAccessToken, error) {
				require.Equal(t, "owner-login", login)
				identity, ok := outboundidentity.FromContext(ctx)
				require.True(t, ok)
				require.Equal(t, "account", identity.Source)
				require.Equal(t, "0.16.9", identity.Version)
				return &zcode.ProjectAccessToken{Token: "models-current-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			f.upstream.body = `{"data":[{"id":"glm-5"},{"id":"glm-4.7"}]}`
			tester := &AccountTestService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
			models, err := tester.FetchUpstreamSupportedModels(context.Background(), a)
			require.NoError(t, err, "linked personal/team accounts support live model discovery")
			require.Equal(t, []string{"glm-4.7", "glm-5"}, models)
			require.Equal(t, http.MethodGet, f.upstream.request.Method)
			require.Equal(t, tc.target, f.upstream.request.URL.String(), "discovery uses the estate coding models endpoint regardless of inference protocol or custom URL")
			require.Equal(t, "Bearer models-current-pat", f.upstream.request.Header.Get("Authorization"))
			require.Equal(t, "ZCode/0.16.9", f.upstream.request.Header.Get("User-Agent"))
			require.Equal(t, "0.16.9", f.upstream.request.Header.Get("X-ZCode-App-Version"))
			require.Equal(t, "http://proxy.example:8080", f.upstream.proxy)
			require.True(t, HTTPUpstreamRedirectsDisabled(f.upstream.request.Context()))
			if tc.team {
				require.Equal(t, "org", f.upstream.request.Header.Get("bigmodel-organization"))
				require.Equal(t, "project", f.upstream.request.Header.Get("bigmodel-project"))
			} else {
				require.Empty(t, f.upstream.request.Header.Get("bigmodel-organization"))
				require.Empty(t, f.upstream.request.Header.Get("bigmodel-project"))
			}
			require.EqualValues(t, 1, f.issuer.calls.Load())
		})
	}
}

func TestZhipuProjectModelsDiscoveryPolicyAndUnsupportedPlansDoNotIssue(t *testing.T) {
	for _, tc := range []string{"url-policy", "start-plan", "off-peak"} {
		t.Run(tc, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			tester := &AccountTestService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
			if tc == "url-policy" {
				tester.cfg = cnProbeAllowlistConfig("other.example")
			} else {
				f.repo.account.Credentials["plan_kind"] = tc
			}
			_, err := tester.FetchUpstreamSupportedModels(context.Background(), f.repo.account)
			require.Error(t, err)
			require.Zero(t, f.issuer.calls.Load())
			require.Zero(t, f.upstream.calls)
		})
	}
}

func TestZhipuProjectModelsDiscoveryRejectsRelinkBetweenBuildAndSend(t *testing.T) {
	f := newZhipuProjectQuotaFixture()
	tester := &AccountTestService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
	req, err := tester.buildUpstreamModelsRequest(context.Background(), f.repo.account)
	require.NoError(t, err)
	f.repo.account.Credentials["access_token"] = "replacement-owner"
	_, err = tester.doUpstreamModelsRequest(req, f.repo.account)
	require.Error(t, err, "a request built for an earlier login cannot acquire the replacement owner's PAT")
	require.Zero(t, f.issuer.calls.Load())
	require.Zero(t, f.upstream.calls)
}

func TestZhipuProjectModelsDiscoveryPreservesManualAPIKeys(t *testing.T) {
	a := &Account{ID: 95, Platform: PlatformZhipu, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"account_mode": AccountModeCoding, "api_key": "manually-entered-key", "base_url": "https://relay.example/v1",
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"custom-glm"}]}`))}}
	tester := &AccountTestService{httpUpstream: upstream, cfg: upstreamModelSyncTestConfig()}
	models, err := tester.FetchUpstreamSupportedModels(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, []string{"custom-glm"}, models)
	require.Equal(t, "https://relay.example/v1/models", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer manually-entered-key", upstream.lastReq.Header.Get("Authorization"))
}

func TestZhipuProjectPublicModelsUsesPATAndSeparatesLoginGenerations(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		t.Run(provider, func(t *testing.T) {
			f := newZhipuProjectQuotaFixture()
			f.repo.account.Credentials["oauth_provider"] = provider
			f.repo.account.Credentials[outboundIdentityCredential] = map[string]any{"preset": "zcode", "version": "0.16.9"}
			f.upstream.body = `{"data":[{"id":"old-owner-model"}]}`
			f.issuer.issue = func(ctx context.Context, login string) (*zcode.ProjectAccessToken, error) {
				identity, ok := outboundidentity.FromContext(ctx)
				require.True(t, ok)
				require.Equal(t, "account", identity.Source)
				require.Equal(t, "0.16.9", identity.Version)
				return &zcode.ProjectAccessToken{Token: login + "-models-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
			}
			gateway := &OpenAIGatewayService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
			first, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
			require.NoError(t, err)
			require.Contains(t, string(first.Body), "old-owner-model")
			require.Equal(t, "Bearer owner-login-models-pat", f.upstream.request.Header.Get("Authorization"), "the public models transport must prepare project authentication")
			require.Equal(t, "ZCode/0.16.9", f.upstream.request.Header.Get("User-Agent"))
			require.True(t, HTTPUpstreamRedirectsDisabled(f.upstream.request.Context()))
			require.Equal(t, "http://proxy.example:8080", f.upstream.proxy)
			f.repo.account.Credentials["access_token"] = "replacement-owner"
			f.upstream.body = `{"data":[{"id":"replacement-owner-model"}]}`
			second, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
			require.NoError(t, err)
			require.Contains(t, string(second.Body), "replacement-owner-model", "a replacement login cannot receive the previous login's cached model list")
			require.NotContains(t, string(second.Body), "old-owner-model")
			require.Equal(t, "Bearer replacement-owner-models-pat", f.upstream.request.Header.Get("Authorization"))
			require.EqualValues(t, 2, f.issuer.calls.Load())
			require.Equal(t, 2, f.upstream.calls)
		})
	}
}

func TestZhipuProjectPublicModelsRejectsPATAndRelinkBeforeSend(t *testing.T) {
	t.Run("provider-rejected-token", func(t *testing.T) {
		f := newZhipuProjectQuotaFixture()
		f.upstream.status, f.upstream.body = http.StatusUnauthorized, `{"error":"unauthorized"}`
		gateway := &OpenAIGatewayService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
		_, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
		require.Error(t, err)
		require.Equal(t, 2, f.upstream.calls, "an explicit 401 receives exactly one same-owner recovery attempt")
		require.EqualValues(t, 2, f.issuer.calls.Load())
		f.upstream.status, f.upstream.body = http.StatusOK, `{"data":[{"id":"glm"}]}`
		f.issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) {
			return &zcode.ProjectAccessToken{Token: "fresh-models-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		_, err = gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
		require.NoError(t, err)
		require.EqualValues(t, 3, f.issuer.calls.Load(), "a PAT rejected by the bounded replay cannot be reused by the next operation")
		require.Equal(t, 3, f.upstream.calls)
		require.Equal(t, "Bearer fresh-models-pat", f.upstream.request.Header.Get("Authorization"))
	})
	t.Run("login-changed-during-issuance", func(t *testing.T) {
		f := newZhipuProjectQuotaFixture()
		f.upstream.body = `{"data":[{"id":"glm"}]}`
		f.issuer.issue = func(context.Context, string) (*zcode.ProjectAccessToken, error) {
			f.repo.account.Credentials["access_token"] = "replacement-owner"
			return &zcode.ProjectAccessToken{Token: "late-old-owner-pat", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}
		gateway := &OpenAIGatewayService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
		_, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
		require.Error(t, err)
		require.Zero(t, f.upstream.calls, "no request may use a late old-owner PAT")
	})
}

func TestZhipuProjectPublicModelsRecoversExplicit401Once(t *testing.T) {
	f := newZhipuProjectQuotaFixture()
	f.issuer.issue = func(_ context.Context, login string) (*zcode.ProjectAccessToken, error) {
		require.Equal(t, "owner-login", login)
		return &zcode.ProjectAccessToken{Token: fmt.Sprintf("models-pat-%d", f.issuer.calls.Load()), ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	var sent []string
	f.upstream.onRequest = func() {
		sent = append(sent, f.upstream.request.Header.Get("Authorization"))
		require.Equal(t, "http://proxy.example:8080", f.upstream.proxy)
		if f.upstream.calls == 1 {
			f.upstream.status, f.upstream.body = http.StatusUnauthorized, `{"error":"expired project access token"}`
		} else {
			f.upstream.status, f.upstream.body = http.StatusOK, `{"data":[{"id":"recovered-model"}]}`
		}
	}
	gateway := &OpenAIGatewayService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
	result, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
	require.NoError(t, err)
	require.Contains(t, string(result.Body), "recovered-model")
	require.Equal(t, []string{"Bearer models-pat-1", "Bearer models-pat-2"}, sent)
	require.EqualValues(t, 2, f.issuer.calls.Load())
	require.Equal(t, 2, f.upstream.calls)
}

func TestZhipuProjectPublicModelsForbiddenDoesNotReplayOrEvict(t *testing.T) {
	f := newZhipuProjectQuotaFixture()
	f.upstream.status, f.upstream.body = http.StatusForbidden, `{"error":"model access denied"}`
	gateway := &OpenAIGatewayService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
	_, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
	require.Error(t, err)
	require.Equal(t, 1, f.upstream.calls, "a permission denial cannot authorize token rotation or replay")
	require.EqualValues(t, 1, f.issuer.calls.Load())
	f.upstream.status, f.upstream.body = http.StatusOK, `{"data":[{"id":"allowed-model"}]}`
	result, err := gateway.FetchOpenAIModelsList(context.Background(), f.repo.account)
	require.NoError(t, err)
	require.Contains(t, string(result.Body), "allowed-model")
	require.Equal(t, 2, f.upstream.calls)
	require.EqualValues(t, 1, f.issuer.calls.Load(), "a 403 does not prove that the current PAT is invalid")
	require.Equal(t, "Bearer short-lived-project-token", f.upstream.request.Header.Get("Authorization"))
}

func TestZhipuProjectModelCatalogSyncRejectsResponseAfterRelink(t *testing.T) {
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, inPlace := range []bool{false, true} {
			t.Run(provider+"/"+map[bool]string{false: "replacement", true: "in-place"}[inPlace], func(t *testing.T) {
				f := newZhipuProjectQuotaFixture()
				a := f.repo.account
				a.Credentials["oauth_provider"] = provider
				changed := snapshotOAuthRefreshAccount(a)
				changed.Credentials["access_token"] = "replacement-model-login"
				f.upstream.body = `{"data":[{"id":"old-owner-model","reasoning":false,"input_modalities":["text"],"context_window":64000}]}`
				f.upstream.onRequest = func() {
					if inPlace {
						a.Credentials["access_token"] = "replacement-model-login"
					} else {
						f.repo.account = changed
					}
				}
				tester := &AccountTestService{accountRepo: f.repo, httpUpstream: f.upstream, cfg: f.service.cfg, projectTokens: f.tokens}
				catalog, err := tester.SyncUpstreamModelCatalog(context.Background(), a)
				require.Error(t, err, "capability metadata belongs to the login that issued the model request")
				require.Nil(t, catalog)
				require.Equal(t, 1, f.upstream.calls)
				require.Nil(t, f.repo.updates, "a relink cannot inherit old project-owner model capabilities")
				require.Nil(t, f.repo.account.GetUpstreamModelMetadataSnapshot())
			})
		}
	}
}
