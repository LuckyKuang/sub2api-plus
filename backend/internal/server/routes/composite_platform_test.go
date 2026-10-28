//go:build unit || !integration

package routes

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/requestmodel"
	servermiddleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compositeRouteRepoStub struct {
	routes []service.CompositeModelRoute
}

func (s compositeRouteRepoStub) ListByGroup(ctx context.Context, groupID int64, includeDisabled bool) ([]service.CompositeModelRoute, error) {
	routes := make([]service.CompositeModelRoute, 0, len(s.routes))
	for _, route := range s.routes {
		if route.GroupID != groupID {
			continue
		}
		if !includeDisabled && !route.Enabled {
			continue
		}
		routes = append(routes, route)
	}
	return routes, nil
}

func (s compositeRouteRepoStub) Create(ctx context.Context, route *service.CompositeModelRoute) error {
	return nil
}

func (s compositeRouteRepoStub) Update(ctx context.Context, route *service.CompositeModelRoute) error {
	return nil
}

func (s compositeRouteRepoStub) Delete(ctx context.Context, id int64) error {
	return nil
}

func (s compositeRouteRepoStub) DeleteByGroup(ctx context.Context, groupID int64) error {
	return nil
}

func TestCompositeTargetPlatformMiddlewareResolvesModelAndRestoresBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(nil))
	router.POST("/", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformOpenAI, platform)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-5"}`, string(body))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCompositeTargetPlatformMiddlewareUsesExplicitRouteAndRewritesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{
		routes: []service.CompositeModelRoute{
			{
				ID:             1,
				GroupID:        1,
				PublicModel:    "openrouter/gpt-5",
				MatchType:      service.CompositeRouteMatchExact,
				TargetPlatform: service.PlatformOpenAI,
				UpstreamModel:  "gpt-5",
				Endpoint:       service.CompositeRouteEndpointAny,
				Priority:       100,
				Enabled:        true,
			},
		},
	})
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(resolver))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformOpenAI, platform)

		upstreamModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "gpt-5", upstreamModel)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"gpt-5","messages":[]}`, string(body))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openrouter/gpt-5","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCompositeTargetPlatformMiddlewareRewritesNestedLiveModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{
		routes: []service.CompositeModelRoute{
			{
				ID:             1,
				GroupID:        1,
				PublicModel:    "live-alias",
				MatchType:      service.CompositeRouteMatchExact,
				TargetPlatform: service.PlatformOpenAI,
				UpstreamModel:  "gpt-live",
				Endpoint:       service.CompositeRouteEndpointAny,
				Priority:       100,
				Enabled:        true,
			},
		},
	})
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(resolver))
	router.POST("/backend-api/codex/realtime/calls", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformOpenAI, platform)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"session":{"model":"gpt-live"},"sdp":"v=0"}`, string(body))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/backend-api/codex/realtime/calls",
		strings.NewReader(`{"session":{"model":"live-alias"},"sdp":"v=0"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

// Scenario: 回归 —— openai OAuth 与 deepseek API Key 同时映射 gpt-6-astra 时，
// 请求必须在路由层被拒绝，不能落到默认 Anthropic handler 后再按模型名猜平台。
// 仅 API Key 的跨平台歧义保持原行为（放行且不解析平台）。
func TestCompositeTargetPlatformMiddlewareRejectsOnlyOAuthAmbiguousOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name         string
		oauthClaimed bool
		wantStatus   int
	}{
		{name: "oauth claimant", oauthClaimed: true, wantStatus: http.StatusBadRequest},
		{name: "apikey only", oauthClaimed: false, wantStatus: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := service.NewCompositeRouteResolver(nil)
			resolver.SetModelOwnershipResolver(func(context.Context, int64, string) (service.CompositeModelOwnership, error) {
				return service.CompositeModelOwnership{Ambiguous: true, OpenAIGatewayOAuthClaimed: tc.oauthClaimed}, nil
			})
			router := gin.New()
			router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
				groupID := int64(8)
				c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
					GroupID: &groupID,
					Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
				})
				c.Next()
			})))
			router.Use(compositeTargetPlatformMiddleware(resolver))
			router.POST("/v1/responses", func(c *gin.Context) {
				_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
				require.False(t, resolved)
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, tc.wantStatus, w.Code)
			if tc.oauthClaimed {
				require.Contains(t, w.Body.String(), "explicit composite route")
			}
		})
	}
}

func TestCompositeRequestModelFromMultipartLiveSession(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("sdp", "v=0"))
	require.NoError(t, writer.WriteField("session", `{"model":"live-alias"}`))
	require.NoError(t, writer.Close())

	require.Equal(t, "live-alias", requestmodel.FromBody(writer.FormDataContentType(), body.Bytes()))
}

func TestCompositeCodexControlPathsUseResponsesRoutes(t *testing.T) {
	for _, path := range []string{
		"/v1/alpha/search",
		"/backend-api/codex/alpha/search",
		"/v1/live",
		"/backend-api/codex/realtime/calls",
	} {
		require.Equal(t, service.CompositeRouteEndpointResponses, compositeRouteEndpointForPath(path), "path=%s", path)
	}
}

func TestCompositeTargetPlatformMiddlewareUsesExplicitRouteForMultipartImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{
		routes: []service.CompositeModelRoute{
			{
				ID:             1,
				GroupID:        1,
				PublicModel:    "image-alias",
				MatchType:      service.CompositeRouteMatchExact,
				TargetPlatform: service.PlatformOpenAI,
				UpstreamModel:  "gpt-image-1",
				Endpoint:       service.CompositeRouteEndpointImages,
				Priority:       100,
				Enabled:        true,
			},
		},
	})
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(resolver))
	router.POST("/v1/images/edits", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformOpenAI, platform)

		upstreamModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "gpt-image-1", upstreamModel)

		publicModel, ok := service.RequestedPublicModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "image-alias", publicModel)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "image-alias")
		c.Status(http.StatusNoContent)
	})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "image-alias"))
	require.NoError(t, writer.WriteField("prompt", "draw"))
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCompositeGeminiTargetPlatformMiddlewareUsesPathRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{
		routes: []service.CompositeModelRoute{
			{
				ID:             1,
				GroupID:        1,
				PublicModel:    "openrouter/gemini-pro",
				MatchType:      service.CompositeRouteMatchExact,
				TargetPlatform: service.PlatformGemini,
				UpstreamModel:  "gemini-2.5-pro",
				Endpoint:       service.CompositeRouteEndpointGemini,
				Priority:       100,
				Enabled:        true,
			},
		},
	})
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeGeminiTargetPlatformMiddleware(resolver))
	router.POST("/v1beta/models/*modelAction", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformGemini, platform)

		upstreamModel, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "gemini-2.5-pro", upstreamModel)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/openrouter/gemini-pro:generateContent", strings.NewReader(`{"contents":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

// Live 入口顶层 model 与 session.model 不一致时，合成路由必须按 session.model
// 分发与改写（与白名单准入、Live handler 一致），不得命中顶层别名的映射并把
// session 模型覆盖为别名上游。
func TestCompositeLiveRouteDispatchesBySessionModelNotTopLevelAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{
		routes: []service.CompositeModelRoute{
			{
				ID:             1,
				GroupID:        1,
				PublicModel:    "live-alias",
				MatchType:      service.CompositeRouteMatchExact,
				TargetPlatform: service.PlatformOpenAI,
				UpstreamModel:  "gpt-alias-upstream",
				Endpoint:       service.CompositeRouteEndpointAny,
				Priority:       100,
				Enabled:        true,
			},
		},
	})
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(resolver))
	router.POST("/backend-api/codex/realtime/calls", func(c *gin.Context) {
		upstream, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "gpt-realtime-1", upstream,
			"Live dispatch must resolve the session model, never the top-level alias upstream")
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"live-alias","session":{"model":"gpt-realtime-1"},"sdp":"v=0"}`, string(body),
			"session.model must stay untouched when dispatch resolves by session model")
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/backend-api/codex/realtime/calls",
		strings.NewReader(`{"model":"live-alias","session":{"model":"gpt-realtime-1"},"sdp":"v=0"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

// Native APIs select their provider by endpoint. A body model remains a native
// parameter even if its prefix or an any-route alias belongs to another vendor.
func TestCompositeGrokNativeRoutesPinSupplierWithoutAliasRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, prefix := range []string{"", "/v1"} {
		for _, endpoint := range []string{"/tts", "/stt", "/custom-voices", "/custom-voices/:voice_id", "/custom-voices/:voice_id/audio", "/web_search", "/x_search", "/realtime"} {
			t.Run(prefix+endpoint, func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					id := int64(1)
					c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformComposite}})
					c.Next()
				})
				router.Use(compositeTargetPlatformMiddleware(nil))
				method := http.MethodPost
				if strings.Contains(endpoint, ":voice_id") || endpoint == "/realtime" {
					method = http.MethodGet
				}
				router.Handle(method, prefix+endpoint, func(c *gin.Context) {
					require.True(t, bindGrokNativeRoute(c))
					platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
					require.True(t, ok)
					require.Equal(t, service.PlatformGrok, platform)
					body, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"model":"gpt-5","input":"native argument"}`, string(body))
					_, mapped := service.ResolvedUpstreamModelFromContext(c.Request.Context())
					require.False(t, mapped, "fixed API must not inherit a text-model mapping")
					c.Status(http.StatusNoContent)
				})
				path := strings.ReplaceAll(prefix+endpoint, ":voice_id", "voice-1")
				request := httptest.NewRequest(method, path, strings.NewReader(`{"model":"gpt-5","input":"native argument"}`))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusNoContent, response.Code)
			})
		}
	}
}

func TestCompositeGeminiCacheCreateResolvesPublicModelAndPreservesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{{ID: 1, GroupID: 1, PublicModel: "cache-pro", UpstreamModel: "gemini-2.5-pro", TargetPlatform: service.PlatformGemini, MatchType: service.CompositeRouteMatchExact, Endpoint: service.CompositeRouteEndpointGemini, Enabled: true}}})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		id := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformComposite}})
		c.Next()
	})
	router.Use(compositeGeminiTargetPlatformMiddleware(resolver))
	router.POST("/v1beta/cachedContents", func(c *gin.Context) {
		public, ok := service.RequestedPublicModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "cache-pro", public)
		upstream, ok := service.ResolvedUpstreamModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "gemini-2.5-pro", upstream)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"models/cache-pro","contents":[{"parts":[{"text":"original"}]}]}`, string(body))
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1beta/cachedContents", strings.NewReader(`{"model":"models/cache-pro","contents":[{"parts":[{"text":"original"}]}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusNoContent, response.Code)
}

func TestCompositeGrokNativeRegisteredRootAndV1RoutesReachHandlers(t *testing.T) {
	cases := []struct{ method, path string }{
		{http.MethodPost, "/tts"}, {http.MethodPost, "/stt"},
		{http.MethodPost, "/custom-voices"}, {http.MethodGet, "/custom-voices"},
		{http.MethodGet, "/custom-voices/voice-1"}, {http.MethodGet, "/custom-voices/voice-1/audio"},
		{http.MethodPatch, "/custom-voices/voice-1"}, {http.MethodDelete, "/custom-voices/voice-1"},
		{http.MethodGet, "/realtime"}, {http.MethodPost, "/web_search"}, {http.MethodPost, "/x_search"},
	}
	for _, prefix := range []string{"", "/v1"} {
		for _, item := range cases {
			t.Run(item.method+prefix+item.path, func(t *testing.T) {
				platforms := append([]string{service.PlatformComposite}, domain.ConcretePlatformIDs()...)
				for _, platform := range platforms {
					router := newGatewayRoutesTestRouter(platform)
					request := httptest.NewRequest(item.method, prefix+item.path, strings.NewReader(`{"query":"native search","input":"native voice"}`))
					request.Header.Set("Content-Type", "application/json")
					if item.path == "/realtime" {
						request.Header.Set("Connection", "Upgrade")
						request.Header.Set("Upgrade", "websocket")
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, request)
					if platform != service.PlatformComposite && platform != service.PlatformGrok {
						require.Equal(t, http.StatusNotFound, response.Code, "unrelated concrete platform=%s must remain unavailable", platform)
					} else {
						require.NotEqual(t, http.StatusNotFound, response.Code, "registered native API must reach its real handler for Composite keys")
						require.NotContains(t, response.Body.String(), "not supported for this platform")
					}
				}
			})
		}
	}
}

// Native Ark task models must not be rewritten by even a matching any-route
// text alias. The endpoint's account protocol fixes its supplier to OpenAI.
func TestCompositeSeedanceNativeRoutesPreserveModelAndSupplier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{{ID: 1, GroupID: 1, PublicModel: "native-model", UpstreamModel: "grok-4.5", TargetPlatform: service.PlatformGrok, MatchType: service.CompositeRouteMatchExact, Endpoint: service.CompositeRouteEndpointAny, Enabled: true}}})
	for _, prefix := range []string{"/api/v3", "/v3", "/v1", ""} {
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
			t.Run(method+prefix, func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					id := int64(1)
					c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{GroupID: &id, Group: &service.Group{ID: id, Platform: service.PlatformComposite}})
					c.Next()
				})
				router.Use(compositeTargetPlatformMiddleware(resolver))
				template := prefix + "/contents/generations/tasks"
				if method != http.MethodPost {
					template += "/:task_id"
				}
				router.Handle(method, template, func(c *gin.Context) {
					actual, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
					require.True(t, ok)
					require.Equal(t, service.PlatformOpenAI, actual)
					raw, err := io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"model":"native-model","content":[]}`, string(raw))
					_, mapped := service.ResolvedUpstreamModelFromContext(c.Request.Context())
					require.False(t, mapped, "a text any-route cannot change the native model or supplier")
					c.Status(http.StatusNoContent)
				})
				path := strings.ReplaceAll(template, ":task_id", "task-1")
				request := httptest.NewRequest(method, path, strings.NewReader(`{"model":"native-model","content":[]}`))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusNoContent, response.Code)
			})
		}
	}
}
