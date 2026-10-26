//go:build unit

package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// docs/UI_INTERACTION.md: public metadata remains small, while active images
// have immutable, content-addressed responses and stale URLs never cache 404s.
func TestConsoleBackgroundHTTPAndPublicMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var imageBytes bytes.Buffer
	require.NoError(t, jpeg.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	sum := sha256.Sum256(imageBytes.Bytes())
	revision := hex.EncodeToString(sum[:])
	path := "/api/v1/settings/console-background/" + revision
	repo := &settingHandlerPublicRepoStub{values: map[string]string{
		"console_appearance":       fmt.Sprintf(`{"background_url":%q,"light_mask":30,"dark_mask":48,"card_transparency":45,"accent_color":"#F59E0B"}`, path),
		"console_background_image": base64.StdEncoding.EncodeToString(imageBytes.Bytes()),
	}}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test")
	router := gin.New()
	router.GET("/api/v1/settings/console-background/:revision", h.GetConsoleBackground)
	router.GET("/api/v1/settings/public", h.GetPublicSettings)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, 200, rec.Code)
	require.Equal(t, imageBytes.Bytes(), rec.Body.Bytes())
	require.Equal(t, "image/jpeg", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Cache-Control"), "immutable")
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	conditional := httptest.NewRequest(http.MethodGet, path, nil)
	conditional.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, conditional)
	require.Equal(t, 304, rec.Code)
	require.Empty(t, rec.Body.Bytes())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil))
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), `"console_appearance"`)
	require.Contains(t, rec.Body.String(), path)
	require.Contains(t, rec.Body.String(), `"accent_color":"#f59e0b"`)
	require.NotContains(t, rec.Body.String(), repo.values["console_background_image"])
	repo.values["console_appearance"] = `{"background_url":"","light_mask":30,"dark_mask":48,"card_transparency":null}`
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, 404, rec.Code)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}
