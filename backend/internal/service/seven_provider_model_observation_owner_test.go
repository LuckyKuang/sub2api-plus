//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// A saved capability catalog affects model admission and protocol adaptation.
// Every supported provider must attach it to the key/route that fetched it,
// including manual API keys, rather than just native OAuth accounts.
func TestSevenProviderModelObservationRequiresCurrentCredentialAndProxyOwner(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, change := range []string{"unchanged", "key-replaced", "proxy-edited", "unbound", "storage-failure"} {
			t.Run(platform+"/"+change, func(t *testing.T) {
				account := openAIAuxiliaryProxy(t, "valid-bound", func(http.ResponseWriter, *http.Request) {})
				account.Platform = platform
				repo := &modelObservationCatalogOwnerRepo{auxiliaryProbeOwnerRepo: &auxiliaryProbeOwnerRepo{current: account, failStorage: change == "storage-failure"}}
				body := `{"data":[{"id":"observed-owner-model","reasoning":false,"input_modalities":["text"],"context_window":64000}]}`
				if platform == PlatformStepFun {
					body = `{"data":[{"id":"observed-owner-model","enable_reason":false,"enable_vision_input":false,"max_input_tokens":64000}]}`
				}
				upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					require.Equal(t, http.MethodGet, request.Method)
					switch change {
					case "key-replaced":
						account.Credentials["api_key"] = "replacement-model-owner"
					case "proxy-edited":
						account.Proxy.Password = "replacement-model-proxy"
					case "unbound":
						account.ProxyID = nil
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
				}}
				svc := &AccountTestService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream}
				catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
				require.Zero(t, repo.legacyWrites, "an ID-only metadata write cannot establish credential ownership")
				if change == "unchanged" {
					require.NoError(t, err)
					require.NotNil(t, catalog)
					require.Equal(t, []string{"observed-owner-model"}, catalog.Models)
					require.Equal(t, 1, repo.atomicWrites)
					require.NotNil(t, account.GetUpstreamModelMetadataSnapshot())
				} else {
					require.Error(t, err)
					require.Nil(t, catalog)
					require.Zero(t, repo.atomicWrites)
					require.Nil(t, account.GetUpstreamModelMetadataSnapshot())
				}
			})
		}
	}
}

type modelObservationShadowOwnerRepo struct {
	grokObservedStrictAccountRepo
	lookups     []int64
	failStorage bool
}

func (r *modelObservationShadowOwnerRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.lookups = append(r.lookups, id)
	if r.account == nil || r.account.ID != id {
		return nil, nil
	}
	return r.account, nil
}

func (r *modelObservationShadowOwnerRepo) UpdateCNControlExtraIfUnchanged(ctx context.Context, expected *Account, updates map[string]any) (bool, error) {
	if r.failStorage {
		r.casCalls++
		return false, errors.New("injected model metadata storage failure")
	}
	return r.grokObservedStrictAccountRepo.UpdateCNControlExtraIfUnchanged(ctx, expected, updates)
}

// A Spark shadow has no independent grant. Its admin catalog may display the
// parent's response, but that observation belongs only to the parent row.
func TestShadowModelObservationPersistsOnlyCurrentCredentialParent(t *testing.T) {
	for _, change := range []string{"unchanged", "parent-relinked", "parent-proxy-edited", "storage-failure"} {
		t.Run(change, func(t *testing.T) {
			parent := openAIAuxiliaryProxy(t, "valid-bound", func(http.ResponseWriter, *http.Request) {})
			parent.Type = AccountTypeOAuth
			parent.Credentials = map[string]any{
				"access_token": "shadow-parent-grant",
				"expires_at":   time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			}
			parentID := parent.ID
			shadow := &Account{ID: parentID + 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				ParentAccountID: &parentID, Credentials: map[string]any{}, Extra: map[string]any{"shadow_marker": "preserve"}}
			repo := &modelObservationShadowOwnerRepo{grokObservedStrictAccountRepo: grokObservedStrictAccountRepo{account: parent}, failStorage: change == "storage-failure"}
			upstream := &codexModelsHTTPUpstreamStub{do: func(request *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
				require.Equal(t, parentID, id)
				require.Equal(t, "Bearer shadow-parent-grant", request.Header.Get("Authorization"))
				require.Contains(t, proxy, "private-proxy-user")
				switch change {
				case "parent-relinked":
					parent.Credentials["access_token"] = "replacement-parent-grant"
				case "parent-proxy-edited":
					parent.Proxy.Password = "replacement-parent-route"
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
					`{"models":[{"slug":"gpt-6-astra","reasoning":false,"input_modalities":["text"],"context_window":64000}]}`))}, nil
			}}
			svc := &AccountTestService{cfg: &config.Config{}, accountRepo: repo, httpUpstream: upstream}
			catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), shadow)
			require.Equal(t, []int64{parentID}, repo.lookups, "resolve the parent before capturing the request owner")
			require.Zero(t, repo.legacyWrites)
			require.Equal(t, 1, repo.casCalls)
			require.Nil(t, shadow.GetUpstreamModelMetadataSnapshot(), "a parent observation cannot become shadow-owned metadata")
			require.Equal(t, map[string]any{"shadow_marker": "preserve"}, shadow.Extra)
			if change == "unchanged" {
				require.NoError(t, err)
				require.NotNil(t, catalog)
				require.Equal(t, []string{"gpt-6-astra"}, catalog.Models)
				require.Equal(t, 1, repo.writes)
				require.NotNil(t, parent.GetUpstreamModelMetadataSnapshot())
			} else {
				require.Error(t, err)
				require.Nil(t, catalog)
				require.Zero(t, repo.writes)
				require.Nil(t, parent.GetUpstreamModelMetadataSnapshot())
			}
		})
	}
}

// Catalog observation and capability metadata use separate owner-fenced writes.
type modelObservationCatalogOwnerRepo struct{ *auxiliaryProbeOwnerRepo }

func (r *modelObservationCatalogOwnerRepo) CompareAndSwapGrokObservedModels(ctx context.Context, owner *Account, expected, next json.RawMessage) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if r.failStorage {
		return false, errors.New("injected catalog storage failure")
	}
	if !grokCatalogOwnerMatches(r.current, owner) {
		return false, nil
	}
	var current any
	if r.current.Extra != nil {
		current = r.current.Extra[grokObservedModelsExtraKey]
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return false, err
	}
	if !jsonValuesEqual(encoded, expected) {
		return false, nil
	}
	var decoded any
	if err := json.Unmarshal(next, &decoded); err != nil {
		return false, err
	}
	if r.current.Extra == nil {
		r.current.Extra = map[string]any{}
	}
	r.current.Extra[grokObservedModelsExtraKey] = decoded
	return true, nil
}
