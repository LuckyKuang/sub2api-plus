//go:build unit || !integration

package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type providerFilesReviewReadCounter struct {
	io.Reader
	reads int
}

func (r *providerFilesReviewReadCounter) Read(p []byte) (int, error) {
	r.reads++
	return r.Reader.Read(p)
}

func TestProviderFilesReviewAllowlistLeavesUploadBodyUnread(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/files", "/v1/files/upload"} {
		t.Run(path, func(t *testing.T) {
			body := "--review-boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"review.png\"\r\nContent-Type: image/png\r\n\r\n\x89PNG\r\n\x1a\nreview-bytes\r\n--review-boundary--\r\n"
			reader := &providerFilesReviewReadCounter{Reader: strings.NewReader(body)}
			key := &service.APIKey{Group: &service.Group{Platform: service.PlatformDeepseek, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"allowed-model"}}}}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
			router.Use(GroupModelAllowlist())
			called := false
			router.POST(path, func(c *gin.Context) {
				called = true
				require.Zero(t, reader.reads, "Files has no model field: model admission must not preload binary bytes before bounded private spooling")
				got, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, body, string(got), "the upload parser must receive the untouched multipart stream")
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, path, reader)
			request.Header.Set("Content-Type", "multipart/form-data; boundary=review-boundary")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.True(t, called)
			require.Equal(t, http.StatusNoContent, response.Code)
		})
	}
}

func TestProviderFilesReviewUploadExemptionPreservesModelAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name, path, body string
		allowed          bool
	}{
		{"chat-allowed", "/v1/chat/completions", `{"model":"allowed-model","messages":[{"role":"user","content":"file-s2a- is only text"}]}`, true},
		{"chat-denied", "/v1/chat/completions", `{"model":"forbidden-model","messages":[{"role":"user","content":"hello"}]}`, false},
		{"live-session-allowed", "/v1/live", `{"model":"forbidden-model","session":{"model":"allowed-model"}}`, true},
		{"live-session-denied", "/v1/live", `{"model":"allowed-model","session":{"model":"forbidden-model"}}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			key := &service.APIKey{Group: &service.Group{Platform: service.PlatformDeepseek, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"allowed-model"}}}}
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), key); c.Next() })
			router.Use(GroupModelAllowlist())
			called := false
			router.POST(scenario.path, func(c *gin.Context) { called = true; c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(http.MethodPost, scenario.path, strings.NewReader(scenario.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, scenario.allowed, called)
			want := http.StatusNotFound
			if scenario.allowed {
				want = http.StatusNoContent
			}
			require.Equal(t, want, response.Code)
		})
	}
}
