//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newUserImageTaskRouter(tasks *service.ImageTaskService) *gin.Engine {
	handler := NewImageTaskUserHandler(tasks, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 2})
		c.Next()
	})
	router.GET("/image-tasks", handler.List)
	router.GET("/image-tasks/:task_id", handler.Get)
	router.DELETE("/image-tasks/:task_id", handler.Delete)
	router.GET("/image-tasks/modes", handler.Modes)
	return router
}

// The console endpoint must include every key of the signed-in user and never
// expose another user's task, which is the whole point of user-scoped history.
func TestImageTaskUserHandlerListsEveryKeyOfTheSignedInUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	history := &asyncImageMemoryHistory{}
	tasks := service.NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	tasks.SetHistoryRepository(history)

	ownKey, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	otherKey, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 10})
	require.NoError(t, err)
	foreign, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 8, APIKeyID: 11})
	require.NoError(t, err)

	router := newUserImageTaskRouter(tasks)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), ownKey.ID)
	require.Contains(t, rec.Body.String(), otherKey.ID)
	require.NotContains(t, rec.Body.String(), foreign.ID)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/"+foreign.ID, nil))
	require.Equal(t, http.StatusNotFound, rec.Code, "another user's task must be indistinguishable from missing")
}

// Successful tasks keep their images and records; only failed ones are removable.
func TestImageTaskUserHandlerDeletesOnlyFailedOwnTasks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	history := &asyncImageMemoryHistory{}
	tasks := service.NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	tasks.SetHistoryRepository(history)

	completed, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	failed, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 7, APIKeyID: 9})
	require.NoError(t, err)
	require.NoError(t, tasks.Fail(context.Background(), failed.ID, http.StatusBadGateway, []byte(`{"message":"upstream failed"}`)))

	router := newUserImageTaskRouter(tasks)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/image-tasks/"+completed.ID, nil))
	require.Equal(t, http.StatusConflict, rec.Code, "a successful task must never be deletable")

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/image-tasks/"+failed.ID, nil))
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/"+failed.ID, nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// Invalid filters are rejected instead of being silently ignored, which would
// return a plausible-looking but wrong page.
func TestImageTaskUserHandlerRejectsInvalidFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tasks := service.NewImageTaskServiceWithOptions(&asyncImageMemoryStore{}, time.Hour, time.Minute)
	router := newUserImageTaskRouter(tasks)

	for _, target := range []string{
		"/image-tasks?status=bogus",
		"/image-tasks?limit=oops",
		"/image-tasks?offset=-1",
		"/image-tasks?limit=0",
		"/image-tasks?limit=101",
		"/image-tasks?offset=999999999999999999999999",
		"/image-tasks?since=not-a-time",
		"/image-tasks?since=200&until=100",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, target)
	}
}

// An enabled mode must stay visible so the entry can explain a missing API key
// instead of disappearing from the console menu.
func TestImageTaskUserHandlerModesKeepsEnabledModeVisibleWithoutEligibleKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tasks := service.NewImageTaskServiceWithOptions(&asyncImageMemoryStore{}, time.Hour, time.Minute)
	batch := service.NewBatchImageSettingsService(&toggleSettingRepo{values: map[string]string{
		"batch_image_enabled": "true",
	}}, false)
	handler := NewImageTaskUserHandler(tasks, nil, batch, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 2})
		c.Next()
	})
	router.GET("/image-tasks/modes", handler.Modes)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ordinary":{"enabled":false,"can_submit":false,"can_view":false,"reason":"disabled"},"batch":{"enabled":true,"can_submit":false,"can_view":true,"reason":"no_eligible_key"}}`, rec.Body.String())
}

func TestImageTaskUserHandlerModesReportsDisabledReasons(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tasks := service.NewImageTaskServiceWithOptions(&asyncImageMemoryStore{}, time.Hour, time.Minute)
	router := newUserImageTaskRouter(tasks)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ordinary":{"enabled":false,"can_submit":false,"can_view":false,"reason":"disabled"},"batch":{"enabled":false,"can_submit":false,"can_view":false,"reason":"disabled"}}`, rec.Body.String())
}

type imageTaskModesKeyRepo struct {
	service.APIKeyRepository
	keys   []service.APIKey
	err    error
	calls  int
	userID int64
}

func (r *imageTaskModesKeyRepo) ListByUserID(_ context.Context, userID int64, params pagination.PaginationParams, _ service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	r.calls++
	r.userID = userID
	if r.err != nil {
		return nil, nil, r.err
	}
	start := params.Offset()
	if start >= len(r.keys) {
		return nil, nil, nil
	}
	end := start + params.Limit()
	if end > len(r.keys) {
		end = len(r.keys)
	}
	return append([]service.APIKey(nil), r.keys[start:end]...), nil, nil
}

func userImageTaskModesRouter(handler *ImageTaskUserHandler) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Next()
	})
	router.GET("/image-tasks/modes", handler.Modes)
	return router
}

func TestImageTaskUserHandlerModesFindsEligibleKeyBeyondTenPages(t *testing.T) {
	repo := &imageTaskModesKeyRepo{keys: make([]service.APIKey, 1101)}
	repo.keys[1100] = service.APIKey{Status: service.StatusAPIKeyActive, Group: &service.Group{
		Platform: service.PlatformGemini, Status: "active", AllowBatchImageGeneration: true,
	}}
	keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	batch := service.NewBatchImageSettingsService(&toggleSettingRepo{values: map[string]string{}}, true)
	handler := ProvideImageTaskUserHandler(nil, nil, batch, keys, &imageTaskModesAccountRepo{accounts: map[int64][]service.Account{
		0: {{Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "test-key"}}},
	}}, nil)
	rec := httptest.NewRecorder()
	userImageTaskModesRouter(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"can_submit":true`)
	require.Equal(t, 12, repo.calls, "both modes must share one walk through the keys")
}

func TestImageTaskUserHandlerModesPreservesDependencyFailure(t *testing.T) {
	repo := &imageTaskModesKeyRepo{err: errors.New("database unavailable")}
	keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	batch := service.NewBatchImageSettingsService(&toggleSettingRepo{values: map[string]string{}}, true)
	handler := NewImageTaskUserHandler(nil, nil, batch, keys)
	rec := httptest.NewRecorder()
	userImageTaskModesRouter(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
	require.JSONEq(t, `{"ordinary":{"enabled":false,"can_submit":false,"can_view":true,"reason":"dependency_unavailable"},"batch":{"enabled":true,"can_submit":false,"can_view":true,"reason":"dependency_unavailable"}}`, rec.Body.String())
	require.Equal(t, 1, repo.calls)
}

type imageTaskModesUnavailableHistory struct {
	service.ImageTaskHistoryRepository
}

func (*imageTaskModesUnavailableHistory) HasAnyTaskForUser(context.Context, int64) (bool, error) {
	return false, errors.New("history unavailable")
}

func TestImageTaskUserHandlerModesKeepsUnknownHistoryDiscoverable(t *testing.T) {
	tasks := service.NewImageTaskServiceWithOptions(&asyncImageMemoryStore{}, time.Hour, time.Minute)
	tasks.SetHistoryRepository(&imageTaskModesUnavailableHistory{})
	handler := NewImageTaskUserHandler(tasks, nil, nil, nil)
	rec := httptest.NewRecorder()
	userImageTaskModesRouter(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
	require.JSONEq(t, `{"ordinary":{"enabled":false,"can_submit":false,"can_view":true,"reason":"dependency_unavailable"},"batch":{"enabled":false,"can_submit":false,"can_view":false,"reason":"disabled"}}`, rec.Body.String())
}

func TestImageTaskUserHandlerModesWorkerDisabled(t *testing.T) {
	repo := &imageTaskModesKeyRepo{keys: []service.APIKey{{Status: service.StatusAPIKeyActive, Group: &service.Group{
		Platform: service.PlatformGemini, Status: "active", AllowBatchImageGeneration: true,
	}}}}
	keys := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil)
	batch := service.NewBatchImageSettingsService(&toggleSettingRepo{values: map[string]string{}}, true)
	batch.SetWorkerEnabled(false)
	handler := NewImageTaskUserHandler(nil, nil, batch, keys)
	rec := httptest.NewRecorder()
	userImageTaskModesRouter(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
	require.Contains(t, rec.Body.String(), `"batch":{"can_submit":false,"can_view":true,"enabled":true,"reason":"worker_disabled"}`)
}

type imageTaskModesAccountRepo struct {
	service.AccountRepository
	accounts map[int64][]service.Account
	errors   map[int64]error
	calls    map[int64]int
}

func (r *imageTaskModesAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, id int64, _ string) ([]service.Account, error) {
	if r.calls == nil {
		r.calls = make(map[int64]int)
	}
	r.calls[id]++
	return r.accounts[id], r.errors[id]
}

// Only local SupportsAccount is implemented. Any provider operation would call
// the nil embedded interface and fail the test; repository selection methods
// are likewise intentionally absent from the metadata-only repository stub.
type imageTaskModesProvider struct {
	service.BatchImageProvider
	supportChecks int
}

func (p *imageTaskModesProvider) Name() string { return service.BatchImageProviderGeminiAPI }
func (p *imageTaskModesProvider) SupportsAccount(account *service.Account) bool {
	p.supportChecks++
	return account.Type == service.AccountTypeAPIKey && account.Credentials["api_key"] != ""
}

func TestImageTaskUserHandlerModesChecksLocalChannelsWithoutProviderProbes(t *testing.T) {
	ordinaryAccount := service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: "active", Schedulable: true}
	batchAccount := service.Account{Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "test-key"}}
	for _, tc := range []struct {
		name        string
		accounts    map[int64][]service.Account
		errors      map[int64]error
		wantReason  string
		wantSubmit  bool
		extraGroups bool
	}{
		{name: "no configured channels", wantReason: "no_channel"},
		{name: "configured channels", accounts: map[int64][]service.Account{1: {ordinaryAccount}, 2: {batchAccount}}, wantSubmit: true},
		{name: "metadata unavailable", errors: map[int64]error{1: errors.New("unavailable"), 2: errors.New("unavailable")}, wantReason: "dependency_unavailable"},
		{name: "healthy group survives another group error", accounts: map[int64][]service.Account{3: {ordinaryAccount}, 4: {batchAccount}}, errors: map[int64]error{1: errors.New("unavailable"), 2: errors.New("unavailable")}, extraGroups: true, wantSubmit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyRepo := &imageTaskModesKeyRepo{}
			for _, group := range []service.Group{
				{ID: 1, Platform: service.PlatformOpenAI, Status: "active", AllowImageGeneration: true},
				{ID: 2, Platform: service.PlatformGemini, Status: "active", AllowBatchImageGeneration: true},
			} {
				groupCopy := group
				keyRepo.keys = append(keyRepo.keys, service.APIKey{Status: service.StatusAPIKeyActive, Group: &groupCopy})
				keyRepo.keys = append(keyRepo.keys, service.APIKey{Status: service.StatusAPIKeyActive, Group: &groupCopy})
				if tc.extraGroups {
					extra := group
					extra.ID += 2
					keyRepo.keys = append(keyRepo.keys, service.APIKey{Status: service.StatusAPIKeyActive, Group: &extra})
				}
			}
			keys := service.NewAPIKeyService(keyRepo, nil, nil, nil, nil, nil, nil)
			settingsRepo := &toggleSettingRepo{values: map[string]string{}}
			storage := service.NewImageStorageSettingService(settingsRepo, passthroughEncryptor{}, nil, func(context.Context, *config.ImageStorageConfig) (service.ImageStorage, error) {
				return noopImageStorage{}, nil
			}, config.ImageStorageConfig{Enabled: true, Bucket: "images", AccessKeyID: "test-ak", SecretAccessKey: "test-sk"})
			batch := service.NewBatchImageSettingsService(settingsRepo, true)
			accounts := &imageTaskModesAccountRepo{accounts: tc.accounts, errors: tc.errors}
			handler := ProvideImageTaskUserHandler(nil, storage, batch, keys, accounts, nil)
			provider := &imageTaskModesProvider{}
			handler.batchProviders = service.NewBatchImageProviderRegistry(provider)
			rec := httptest.NewRecorder()
			userImageTaskModesRouter(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var result map[string]struct {
				Enabled   bool   `json:"enabled"`
				CanSubmit bool   `json:"can_submit"`
				CanView   bool   `json:"can_view"`
				Reason    string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			for _, mode := range []string{"ordinary", "batch"} {
				require.True(t, result[mode].Enabled)
				require.True(t, result[mode].CanView)
				require.Equal(t, tc.wantSubmit, result[mode].CanSubmit)
				require.Equal(t, tc.wantReason, result[mode].Reason)
			}
			for _, calls := range accounts.calls {
				require.Equal(t, 1, calls, "duplicate keys must not reread a group's accounts")
			}
			if tc.wantSubmit {
				require.Positive(t, provider.supportChecks)
			}
		})
	}
}

func TestImageTaskUserHandlerBatchChannelRequiresSupportedAccountAndVertexConfiguration(t *testing.T) {
	keys := []service.APIKey{{Status: service.StatusAPIKeyActive, Group: &service.Group{ID: 1, Platform: service.PlatformGemini, Status: "active", AllowBatchImageGeneration: true}}}
	for _, tc := range []struct {
		name          string
		account       service.Account
		vertexEnabled bool
		bucket        string
		want          bool
	}{
		{name: "OAuth is not a batch account", account: service.Account{Type: service.AccountTypeOAuth, Credentials: map[string]any{"api_key": "test-key"}}},
		{name: "API key credentials required", account: service.Account{Type: service.AccountTypeAPIKey}},
		{name: "API key account works without Vertex", account: service.Account{Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}, want: true},
		{name: "Vertex disabled", account: service.Account{Type: service.AccountTypeServiceAccount}},
		{name: "Vertex managed bucket required", account: service.Account{Type: service.AccountTypeServiceAccount}, vertexEnabled: true},
		{name: "Vertex configured", account: service.Account{Type: service.AccountTypeServiceAccount}, vertexEnabled: true, bucket: "managed-batch", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := tc.account
			account.Platform, account.Status, account.Schedulable = service.PlatformGemini, "active", true
			if account.Type == service.AccountTypeServiceAccount {
				account.Credentials = map[string]any{"service_account_json": map[string]any{
					"project_id": "test-project", "client_email": "test@test-project.iam.gserviceaccount.com", "private_key": "test-key",
				}}
			}
			repo := &imageTaskModesAccountRepo{accounts: map[int64][]service.Account{1: {account}}}
			handler := ProvideImageTaskUserHandler(nil, nil, nil, nil, repo, &config.Config{BatchImage: config.BatchImageConfig{VertexEnabled: tc.vertexEnabled, VertexManagedGCSBucket: tc.bucket}})
			_, _, ready, err := handler.availableChannels(context.Background(), keys, false, true)
			require.NoError(t, err)
			require.Equal(t, tc.want, ready)
		})
	}
}

// R3/R5 preserve Batch's existing expiry/quota authentication for history;
// ordinary user history has its own independent owner-scoped lookup.
func TestImageTaskKeyAccessExcludesExpiredAndExhaustedKeys(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name string
		key  service.APIKey
	}{
		{"expired active key", service.APIKey{Status: service.StatusAPIKeyActive, ExpiresAt: &past}},
		{"exhausted active key", service.APIKey{Status: service.StatusAPIKeyActive, Quota: 10, QuotaUsed: 10}},
		{"expired status", service.APIKey{Status: service.StatusAPIKeyExpired}},
		{"quota exhausted status", service.APIKey{Status: service.StatusAPIKeyQuotaExhausted}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.key.Group = &service.Group{Status: "active", Platform: service.PlatformGemini, AllowBatchImageGeneration: true}
			_, batch, history := imageTaskKeyAccess([]service.APIKey{tc.key})
			require.False(t, batch)
			require.False(t, history, "ineligible keys cannot establish readable batch history")
			tc.key.Group.Platform = service.PlatformOpenAI
			tc.key.Group.AllowImageGeneration = true
			ordinary, _, _ := imageTaskKeyAccess([]service.APIKey{tc.key})
			require.False(t, ordinary)
		})
	}
	_, batch, history := imageTaskKeyAccess([]service.APIKey{{Status: service.StatusAPIKeyActive, Group: &service.Group{Status: "active", Platform: service.PlatformGemini, AllowBatchImageGeneration: true}}})
	require.True(t, batch)
	require.True(t, history)
}

func TestImageTaskUserHandlerSupportReadsUseTargetAndDeletesUseActor(t *testing.T) {
	store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
	history := &asyncImageMemoryHistory{}
	tasks := service.NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	tasks.SetHistoryRepository(history)
	targetTask, err := tasks.Create(context.Background(), service.ImageTaskOwner{UserID: 8, APIKeyID: 10})
	require.NoError(t, err)
	require.NoError(t, tasks.Fail(context.Background(), targetTask.ID, http.StatusBadGateway, []byte(`{"message":"upstream failed"}`)))
	handler := NewImageTaskUserHandler(tasks, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
		c.Set(string(middleware2.ContextKeyUserRole), "admin")
		c.Set(middleware2.SupportReadTargetKey, middleware2.SupportReadTarget{Subject: middleware2.AuthSubject{UserID: 8}, Role: "user"})
		c.Next()
	})
	router.GET("/image-tasks", handler.List)
	router.GET("/image-tasks/:task_id", handler.Get)
	router.DELETE("/image-tasks/:task_id", handler.Delete)
	for _, target := range []string{"/image-tasks", "/image-tasks/" + targetTask.ID} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), targetTask.ID)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/image-tasks/"+targetTask.ID, nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// Existing Composite async admission resolves concrete OpenAI/Grok targets
// (image_task_composite_test.go). Console capabilities must expose that same
// supported flow without selecting accounts, probing or bypassing group flags.
func TestImageTaskUserHandlerModesCompositeImageKeys(t *testing.T) {
	for _, tc := range []struct {
		name       string
		platform   string
		permission bool
		want       bool
		reason     string
	}{
		{"OpenAI target", service.PlatformOpenAI, true, true, ""},
		{"Grok target", service.PlatformGrok, true, true, ""},
		{"unsupported target", service.PlatformAnthropic, true, false, "no_channel"},
		{"permission disabled", service.PlatformOpenAI, false, false, "no_eligible_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keyRepo := &imageTaskModesKeyRepo{keys: []service.APIKey{{Status: service.StatusAPIKeyActive, Group: &service.Group{ID: 979, Platform: service.PlatformComposite, Status: "active", AllowImageGeneration: tc.permission}}}}
			keys := service.NewAPIKeyService(keyRepo, nil, nil, nil, nil, nil, nil)
			accounts := &imageTaskModesAccountRepo{accounts: map[int64][]service.Account{979: {{Platform: tc.platform, Type: service.AccountTypeAPIKey, Status: "active", Schedulable: true}}}}
			storage := service.NewImageStorageSettingService(&toggleSettingRepo{values: map[string]string{}}, passthroughEncryptor{}, nil, func(context.Context, *config.ImageStorageConfig) (service.ImageStorage, error) {
				return noopImageStorage{}, nil
			}, config.ImageStorageConfig{Enabled: true, Bucket: "images", AccessKeyID: "test-ak", SecretAccessKey: "test-sk"})
			handler := ProvideImageTaskUserHandler(nil, storage, nil, keys, accounts, nil)
			rec := httptest.NewRecorder()
			userImageTaskModesRouter(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/image-tasks/modes", nil))
			require.Equal(t, http.StatusOK, rec.Code)
			var body struct {
				Ordinary struct {
					CanSubmit bool   `json:"can_submit"`
					Reason    string `json:"reason"`
				} `json:"ordinary"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, tc.want, body.Ordinary.CanSubmit)
			require.Equal(t, tc.reason, body.Ordinary.Reason)
		})
	}
}

func (r *imageTaskModesAccountRepo) ListSchedulableByGroupID(ctx context.Context, id int64) ([]service.Account, error) {
	return r.ListSchedulableByGroupIDAndPlatform(ctx, id, "")
}
