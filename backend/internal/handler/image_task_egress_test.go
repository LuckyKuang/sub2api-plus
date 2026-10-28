//go:build unit || !integration

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type asyncImageOwnerGuardTransport func(*http.Request) (*http.Response, error)

func (f asyncImageOwnerGuardTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// A successful HTTP body is insufficient evidence to choose CDN egress. The
// handler must carry the gateway's owner scope into Complete; absent capture
// fails the task before the uploader's independent/default client can send.
func TestAsyncImageHandlerURLOnlyCompletionRequiresCapturedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	direct := 0
	client := &http.Client{Transport: asyncImageOwnerGuardTransport(func(request *http.Request) (*http.Response, error) {
		direct++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(asyncImageTestPNG)), Request: request}, nil
	})}
	storage := &asyncImageDownloadStorage{}
	tasks := service.NewImageTaskServiceWithUploader(&asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}, service.NewImageResultUploader(storage, "images", false, 0, client), time.Hour, time.Minute)
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	task, err := tasks.Create(context.Background(), owner)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: owner.UserID, Concurrency: 3})
	h := &AsyncImageHandler{openAI: newGatewayAdmissionForTest(), tasks: tasks, execute: func(_ string, c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": []gin.H{{"url": "https://cdn.example.test/image.png"}}})
	}}
	h.run(task.ID, service.PlatformOpenAI, c, recorder, cancel, asyncImageOpsSnapshot{})
	completed, err := tasks.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, completed.Status)
	require.Zero(t, direct, "losing the final credential owner must not fall back to the uploader's default client")
	require.Empty(t, storage.objects)
	require.Empty(t, completed.Result)
}
