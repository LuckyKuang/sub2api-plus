//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
		"/image-tasks?since=not-a-time",
		"/image-tasks?since=200&until=100",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, target)
	}
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
