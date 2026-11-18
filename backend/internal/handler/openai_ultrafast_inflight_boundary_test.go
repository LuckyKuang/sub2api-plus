//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// One thousand output tokens cost $0.001 at Standard and $0.006 at Ultrafast.
// Only Standard fits the fixture's $0.003 balance and foreign $0.0001 hold.
// Every HTTP ingress must reject the forced tier before any upstream write.
func TestOpenAIHTTPHandlersRejectUnfundedForcedUltrafast(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, fast := range []bool{false, true} {
		for _, endpoint := range []string{"responses", "chat/completions", "messages"} {
			t.Run(fmt.Sprintf("fast=%t/%s", fast, endpoint), func(t *testing.T) {
				f := newOpenAIAccountInflightFixture(t, "gpt-6.1-sol")
				configureUltrafastInflightFixture(f, fast, "gpt-6.1-sol")
				body := `{"model":"gpt-6.1-sol","max_output_tokens":1000,"input":"hi"}`
				handle := f.handler.Responses
				if endpoint == "chat/completions" {
					body = `{"model":"gpt-6.1-sol","max_completion_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`
					handle = f.handler.ChatCompletions
				} else if endpoint == "messages" {
					body = `{"model":"gpt-6.1-sol","max_tokens":1000,"messages":[{"role":"user","content":"hi"}]}`
					handle = f.handler.Messages
				}
				router := gin.New()
				router.Use(func(c *gin.Context) {
					c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.Group, f.key.Group))
					c.Set(string(middleware.ContextKeyAPIKey), f.key)
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: f.key.UserID, Concurrency: 3})
					c.Next()
				})
				path := "/v1/" + endpoint
				router.POST(path, handle)
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(recorder, request)
				require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
				require.Contains(t, recorder.Body.String(), "billing_error")
				require.Zero(t, f.counts.accountAcquisitions.Load(), "forced-tier admission must fail before account acquisition")
				f.assertOnlyForeignOwnersRemain(t)
			})
		}
	}
}

func TestOpenAIAccountSlotReconcilesForcedUltrafastAfterMappingChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newOpenAIAccountInflightFixture(t, "gpt-6.1-sol")
	configureUltrafastInflightFixture(f, false, "gpt-6-luna")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	body := []byte(`{"model":"gpt-6.1-sol","input":"hi","max_output_tokens":1000}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	ctx := context.WithValue(c.Request.Context(), ctxkey.Group, f.key.Group)
	ctx = f.handler.gatewayService.WithBillingPricingSnapshot(ctx, f.key, time.Now(), "gpt-6.1-sol")
	c.Request = c.Request.WithContext(ctx)
	done, err := reserveInflightBalance(c, f.handler.billingCacheService, f.handler.gatewayService, f.key, nil,
		tokenInflightEstimate(service.ContentModerationProtocolOpenAIResponses, "gpt-6.1-sol", body))
	require.NoError(t, err, "the unsupported preselection wire model keeps the affordable Standard tier")
	t.Cleanup(done)
	require.InDelta(t, .001, service.InflightReservationFromContext(c.Request.Context()).Amount(), 1e-12)
	account := *f.accounts[0]
	account.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6-astra"}}
	selection := &service.AccountSelectionResult{Account: &account,
		WaitPlan: &service.AccountWaitPlan{AccountID: account.ID, MaxConcurrency: 2, MaxWaiting: 2, Timeout: time.Second}}
	var started bool
	release, result := f.handler.acquireOpenAIAccountSlot(c, f.key.GroupID, "", selection, true, &started, zap.NewNop(), nil)
	require.Equal(t, openAISlotAcquireFailed, result, recorder.Body.String())
	require.Nil(t, release)
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "billing_error")
	require.False(t, started)
	require.Equal(t, int32(1), f.counts.accountAcquisitions.Load())
	done()
	f.assertOnlyForeignOwnersRemain(t)
}

func configureUltrafastInflightFixture(f *openAIAccountInflightFixture, fast bool, wire string) {
	input, output, priority := 0.0, .000001, 2.0
	f.key.Group.ForceOpenAIUltrafast, f.key.Group.ForceOpenAIFast = true, fast
	f.key.Group.ModelPricing = []service.ChannelModelPricing{{Platform: service.PlatformOpenAI, Models: []string{"gpt-6.1-sol"},
		BillingMode: service.BillingModeToken, InputPrice: &input, OutputPrice: &output, FastMultiplier: &priority}}
	for _, account := range f.accounts {
		account.Credentials["model_mapping"] = map[string]any{"gpt-6.1-sol": wire}
	}
}
