//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// docs/UI_INTERACTION.md: explicit per-card save, integer percentages, no
// writes on malformed data. Authorization is tested through real admin routes.
func TestConsoleAppearanceHTTPUpdateValidation(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, body := range []string{
		`{"light_mask":30,"dark_mask":48,"card_transparency":45}`,
		`{"background_image":"","light_mask":30,"dark_mask":48,"card_transparency":null}`,
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/appearance", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.UpdateConsoleAppearance(c)
		require.Equal(t, 200, rec.Code)
		require.Contains(t, rec.Body.String(), `"background_url":""`)
	}
	before := repo.values["console_appearance"]
	for _, body := range []string{
		`{"light_mask":30.5}`, `{"card_transparency":101}`, `{"dark_mask":-1}`,
		`{"background_image":"https://example.com/a.png"}`, `{`,
		`{"background_image":"` + strings.Repeat("a", 3<<20) + `"}`,
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/appearance", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		h.UpdateConsoleAppearance(c)
		require.Equal(t, 400, rec.Code)
		require.Equal(t, before, repo.values["console_appearance"])
	}
}
