//go:build unit || !integration

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAsyncImageCompositeSubmitUsesResolvedTargetAndAdmittedSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{service.PlatformOpenAI, service.PlatformGrok} {
		t.Run(target, func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			openAI := &OpenAIGatewayHandler{unfinishedTasks: newGatewayAdmissionForTest().unfinishedTasks, concurrencyHelper: newGatewayAdmissionForTest().concurrencyHelper, gatewayService: &service.OpenAIGatewayService{}, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
			h := &AsyncImageHandler{tasks: tasks, openAI: openAI}
			groupID := int64(3)
			key := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, Group: &service.Group{ID: 3, Platform: service.PlatformComposite, RateMultiplier: 1, BillingPolicyVersion: 3, AllowImageGeneration: true}}
			upstreamModel := "gpt-image-1"
			if target == service.PlatformGrok {
				upstreamModel = "grok-imagine-image"
			}
			decision := service.CompositeRouteDecision{Matched: true, TargetPlatform: target, PublicModel: "my-image", UpstreamModel: upstreamModel}
			var admitted *service.BillingPricingSnapshot
			release := make(chan struct{})
			type observed struct {
				target   string
				snapshot *service.BillingPricingSnapshot
				data     json.RawMessage
				err      error
				ctxErr   error
			}
			completed := make(chan observed, 1)
			h.execute = func(platform string, c *gin.Context) {
				<-release
				data, err := service.ExportBillingPricingSnapshot(c.Request.Context())
				completed <- observed{platform, service.BillingPricingSnapshotFromContext(c.Request.Context()), data, err, c.Request.Context().Err()}
				c.JSON(http.StatusOK, gin.H{"created": 1, "data": []gin.H{{"url": "https://example.test/image.png"}}})
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(middleware2.ContextKeyAPIKey), key)
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
				c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
				c.Next()
				admitted = service.BillingPricingSnapshotFromContext(c.Request.Context())
			})
			router.POST("/v1/images/generations/async", h.Submit)
			requestCtx, cancel := context.WithCancel(context.Background())
			request := httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"`+upstreamModel+`","prompt":"safe current user"}`)).WithContext(requestCtx)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusAccepted, recorder.Code, recorder.Body.String())
			require.NotNil(t, admitted)
			require.Equal(t, int64(1), engine.calls.Load())
			require.False(t, engine.observedSnapshot.Load())
			cancel()
			key.Group.RateMultiplier, key.Group.BillingPolicyVersion = 9, 4
			close(release)
			select {
			case actual := <-completed:
				require.Equal(t, target, actual.target, "dispatch follows the already authorized concrete provider")
				require.Same(t, admitted, actual.snapshot, "detached execution must retain the submit admission snapshot")
				require.NoError(t, actual.err)
				require.NoError(t, actual.ctxErr)
				var stored struct {
					PublicModel string         `json:"public_model"`
					Platform    string         `json:"platform"`
					Group       *service.Group `json:"group"`
				}
				require.NoError(t, json.Unmarshal(actual.data, &stored))
				require.Equal(t, "my-image", stored.PublicModel, "public policy matching must not use the rewritten upstream model")
				require.Equal(t, target, stored.Platform)
				require.Equal(t, int64(3), stored.Group.BillingPolicyVersion)
				require.Equal(t, float64(1), stored.Group.RateMultiplier)
			case <-time.After(2 * time.Second):
				t.Fatal("detached image execution did not run")
			}
			require.Eventually(t, func() bool {
				store.mu.RLock()
				defer store.mu.RUnlock()
				for _, task := range store.tasks {
					if task.Status == service.ImageTaskStatusCompleted {
						return true
					}
				}
				return false
			}, time.Second, time.Millisecond)
		})
	}
}

func TestAsyncImageCompositeRejectsUnresolvedAndUnsupportedTargetsBeforeTaskCreation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, target := range []string{"", service.PlatformAnthropic, "future-unknown"} {
		t.Run(target, func(t *testing.T) {
			store := &asyncImageMemoryStore{tasks: make(map[string]*service.ImageTaskRecord)}
			tasks := service.NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
			engine := &pricingAdmissionEngine{kind: securityaudit.DecisionAllow}
			h := &AsyncImageHandler{tasks: tasks, openAI: &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}}
			executions := 0
			h.execute = func(string, *gin.Context) { executions++ }
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations/async", strings.NewReader(`{"model":"my-image","prompt":"safe"}`))
			c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{Matched: true, TargetPlatform: target, PublicModel: "my-image"}))
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: 7, Group: &service.Group{ID: 3, Platform: service.PlatformComposite, AllowImageGeneration: true}})
			h.Submit(c)
			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Empty(t, store.tasks)
			require.Zero(t, executions)
			require.Zero(t, engine.calls.Load(), "unsupported platform fails basic capability validation")
			require.Nil(t, service.BillingPricingSnapshotFromContext(c.Request.Context()))
		})
	}
}
