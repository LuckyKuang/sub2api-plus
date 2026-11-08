//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// SECURITY_AUDIT_CONTENT_COVERAGE.md: native image options and unknown siblings
// never replace the canonical prompt, and empty content remains pass-through.
func TestUpstream020NativeImageOptionsPreserveCanonicalAudits(t *testing.T) {
	for _, prompt := range []string{"audit native image", ""} {
		body := []byte(`{"model":"gpt-image-2.6-flare","prompt":"` + prompt + `","complexity":"high","svg":{"model":"gpt-6.1-sol","reasoning":{"effort":"low"}},"future_options":{"opaque":true}}`)
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

func TestUpstream020NativeImageAuditsPrecedeSideEffects(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			t.Run(accountType+"/"+engine, func(t *testing.T) {
				coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 1)
				account := service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: accountType, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "fixture", "api_key": "fixture"}}
				h, boundary := newCodexCLIIngressHandler(t, coordinator, []service.Account{account}, false)
				c, w := codexCLIIngressContext(`{"model":"gpt-image-2.6-flare","prompt":"audit native image","complexity":"high","svg":{"model":"gpt-6.1-sol"},"future_options":{"opaque":true}}`, true)
				c.Request.URL.Path = "/v1/images/generations"
				c.Request.Header.Set("Content-Type", "application/json")
				key, _ := middleware2.GetAPIKeyFromContext(c)
				key.Group.AllowImageGeneration = true
				h.Images(c)
				require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
				scans := supplier.snapshot()
				require.Len(t, scans, 1)
				require.Contains(t, scans[0], "audit native image")
				assertCodexCLIIngressNoSideEffects(t, boundary)
			})
		}
	}
}

// Native edits add markup as a distinct image input. The canonical contract
// must audit it alongside the prompt, primary image and mask (coverage doc).
func TestUpstream020MarkupEntersCanonicalAudits(t *testing.T) {
	const body = `{"model":"gpt-image-2.6-flare","prompt":"edit the diagram","images":[{"image_url":"https://example.com/base.png"}],"mask":{"image_url":"https://example.com/mask.png"},"markup":{"image_url":"https://example.com/markup.png"},"svg":{"model":"gpt-6.1-sol"},"future_options":{"opaque":true}}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	parsed, err := (&service.OpenAIGatewayService{}).ParseOpenAIImagesRequest(c, []byte(body))
	require.NoError(t, err)
	for _, payload := range [][]byte{[]byte(body), parsed.ModerationBody()} {
		input := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIImages, payload)
		require.Equal(t, "edit the diagram", input.Text)
		require.ElementsMatch(t, []string{"https://example.com/base.png", "https://example.com/mask.png", "https://example.com/markup.png"}, input.Images)
		snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIImages, Body: payload})
		require.NoError(t, err)
		require.Equal(t, "edit the diagram", snapshot.ScanText)
		require.NotContains(t, snapshot.FullPrompt, "https://")
	}
}

func TestUpstream020EmptyNativeImageContentPassesAudits(t *testing.T) {
	for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
		for _, engine := range []string{"moderation", "prompt"} {
			t.Run(accountType+"/"+engine, func(t *testing.T) {
				coordinator, supplier := newCountTokensAuditCoordinator(t, engine, true, 0)
				h, boundary := newCodexCLIIngressHandler(t, coordinator, nil, false)
				c, _ := codexCLIIngressContext(`{"model":"gpt-image-2.6-flare","prompt":"","complexity":"high","future_options":{"opaque":true}}`, true)
				c.Request.URL.Path = "/v1/images/generations"
				c.Request.Header.Set("Content-Type", "application/json")
				key, _ := middleware2.GetAPIKeyFromContext(c)
				key.Group.AllowImageGeneration = true
				boundary.slots.acquireUserSlotFn = func(context.Context, int64, int, string) (bool, error) {
					completed, _ := c.Get(securityAuditCompletedContextKey)
					require.Equal(t, true, completed, "empty extracted content must pass before concurrency admission")
					boundary.userSlots.Add(1)
					return true, nil
				}
				h.Images(c)
				require.Empty(t, supplier.snapshot())
				require.Equal(t, int32(1), boundary.userSlots.Load())
				require.Positive(t, boundary.repositoryReads.Load(), "unextractable content must reach scheduling")
			})
		}
	}
}
