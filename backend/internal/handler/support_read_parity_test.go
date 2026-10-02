//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/pagination"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func supportParityContext(rec *httptest.ResponseRecorder, path string, support bool) *gin.Context {
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	actor := int64(42)
	if support {
		actor = 1
	}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: actor})
	c.Set(string(middleware.ContextKeyUserRole), "user")
	if support {
		c.Set(string(middleware.ContextKeyUserRole), "admin")
		c.Set(middleware.SupportReadTargetKey, middleware.SupportReadTarget{Subject: middleware.AuthSubject{UserID: 42}, Role: "user"})
	}
	return c
}

type supportKeyRepository struct {
	service.APIKeyRepository
	key        service.APIKey
	listUserID int64
	filters    service.APIKeyListFilters
}

func (r *supportKeyRepository) GetByID(context.Context, int64) (*service.APIKey, error) {
	return &r.key, nil
}
func (r *supportKeyRepository) ListByUserID(_ context.Context, userID int64, _ pagination.PaginationParams, filters service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	r.listUserID, r.filters = userID, filters
	return []service.APIKey{r.key}, &pagination.PaginationResult{Total: 1}, nil
}

func TestSupportKeyReadsMatchUserIncludingFullCredentialsAndIPRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &supportKeyRepository{key: service.APIKey{ID: 7, UserID: 42, Key: "sk-support-parity-fixture", Name: "first key", IPWhitelist: []string{"192.0.2.0/24"}, IPBlacklist: []string{"198.51.100.10"}}}
	h := NewAPIKeyHandler(service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, nil))
	for _, detail := range []bool{false, true} {
		invoke := func(support bool) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			c := supportParityContext(rec, "/keys?search=first&status=active&group_id=9", support)
			if detail {
				c.Params = gin.Params{{Key: "id", Value: "7"}}
				h.GetByID(c)
			} else {
				h.List(c)
				require.Equal(t, int64(42), repo.listUserID)
				require.Equal(t, "first", repo.filters.Search)
			}
			return rec
		}
		user, support := invoke(false), invoke(true)
		require.Equal(t, http.StatusOK, support.Code)
		require.JSONEq(t, user.Body.String(), support.Body.String())
		require.Contains(t, support.Body.String(), repo.key.Key)
		require.Contains(t, support.Body.String(), "192.0.2.0/24")
		require.Contains(t, support.Body.String(), "198.51.100.10")
	}
	rec := httptest.NewRecorder()
	c := supportParityContext(rec, "/images/tasks?api_key_id=7", true)
	h.RequireSupportImageKey(c)
	key, ok := middleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.Equal(t, int64(7), key.ID)
	actor, _ := middleware.GetAuthSubjectFromContext(c)
	require.Equal(t, int64(1), actor.UserID)
	repo.key.UserID = 43
	for _, image := range []bool{false, true} {
		rec = httptest.NewRecorder()
		c = supportParityContext(rec, "/images/tasks?api_key_id=7", true)
		if image {
			h.RequireSupportImageKey(c)
		} else {
			c.Params = gin.Params{{Key: "id", Value: "7"}}
			h.GetByID(c)
		}
		require.Equal(t, http.StatusNotFound, rec.Code)
		require.NotContains(t, rec.Body.String(), repo.key.Key)
	}
}

func TestSupportProfileMatchesUserVisibleAuthenticationInformation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userHandlerRepoStub{user: &service.User{ID: 42, Email: "support-fixture@example.com", Username: "first-user", Role: "user", Status: "active", Balance: 12.5, Concurrency: 3, PasswordHash: "private-password-hash"}, identities: []service.UserAuthIdentityRecord{{ProviderType: "oidc", ProviderKey: "https://issuer.example.com", ProviderSubject: "private-provider-subject", Metadata: map[string]any{"suggested_display_name": "First user OIDC"}}}}
	h := NewUserHandler(service.NewUserService(repo, nil, nil, nil), nil, nil, nil, nil, nil)
	user, support := httptest.NewRecorder(), httptest.NewRecorder()
	h.GetProfile(supportParityContext(user, "/user/profile", false))
	h.GetProfile(supportParityContext(support, "/user/profile", true))
	require.Equal(t, http.StatusOK, support.Code)
	require.JSONEq(t, user.Body.String(), support.Body.String())
	require.Contains(t, support.Body.String(), "First user OIDC")
	require.Contains(t, support.Body.String(), "https://issuer.example.com")
	require.NotContains(t, support.Body.String(), "private-password-hash")
	require.NotContains(t, support.Body.String(), "private-provider-subject")
}

type supportDownloadRepository struct {
	service.BatchImageRepository
	writes int
}

func (r *supportDownloadRepository) GetBatchImageJobByBatchIDForOwner(context.Context, int64, int64, string) (*service.BatchImageJob, error) {
	return &service.BatchImageJob{BatchID: "batch-fixture"}, nil
}
func (r *supportDownloadRepository) MarkBatchImageDownloaded(context.Context, string, time.Time) error {
	r.writes++
	return nil
}

func TestSupportBatchDownloadPreservesUserDownloadedState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &supportDownloadRepository{}
	h := NewBatchImageHandler(&service.BatchImagePublicService{Repo: repo}, nil, nil)
	owner := service.BatchImageOwner{UserID: 42, APIKeyID: 7}
	h.markDownloadedBestEffort(supportParityContext(httptest.NewRecorder(), "/images/batches/batch-fixture/download", true), owner)
	require.Zero(t, repo.writes)
	h.markDownloadedBestEffort(supportParityContext(httptest.NewRecorder(), "/images/batches/batch-fixture/download", false), owner)
	require.Equal(t, 1, repo.writes)
}

func TestSupportMonitorUsesTargetPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := supportParityContext(httptest.NewRecorder(), "/channel-monitor-v2/errors", true)
	require.False(t, channelMonitorV2IsAdmin(c))
	role, _ := middleware.GetUserRoleFromContext(c)
	require.Equal(t, "admin", role)
}
