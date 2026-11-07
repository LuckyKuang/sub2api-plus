//go:build unit

package handler

import (
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOfficialCodexGIFControlsPreserveBothCanonicalAudits(t *testing.T) {
	for _, prompt := range []string{"audit animated image", ""} {
		body := []byte(`{"prompt":"` + prompt + `","output_format":"gif","frame_count":2,"frame_duration_ms":200,"future_option":{"opaque":true}}`)
		moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIImages, body)
		require.Equal(t, prompt, moderation.Text)
		snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIImages, Body: body})
		if prompt == "" {
			require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, prompt, snapshot.ScanText)
	}
}

func TestOfficialCodexGIFBothRealAuditsPrecedeImageSideEffects(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			t.Run(accountType+"/"+engine, func(t *testing.T) {
				coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 1)
				account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: accountType, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture", "api_key": "fixture"}}
				h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, false)
				c, w := codexCLIIngressContext(`{"model":"gpt-image-2","prompt":"audit animated image","output_format":"gif","frame_count":2,"frame_duration_ms":200,"future_option":{"opaque":true}}`, true)
				c.Request.URL.Path = "/v1/images/generations"
				c.Request.Header.Set("Content-Type", "application/json")
				key, _ := middleware2.GetAPIKeyFromContext(c)
				key.Group.AllowImageGeneration = true
				h.Images(c)
				require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
				scans := supplier.snapshot()
				require.Len(t, scans, 1)
				require.Contains(t, scans[0], "audit animated image")
				assertCodexCLIIngressNoSideEffects(t, boundary)
			})
		}
	}
}
