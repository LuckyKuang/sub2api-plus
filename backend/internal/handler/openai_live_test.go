package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseLiveCallRequestMultipartPreservesSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	session := `{"model":"gpt-live-test","delegation":{"type":"client"},"instructions":"你好"}`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("sdp", "v=0\r\n"))
	require.NoError(t, writer.WriteField("session", session))
	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/live", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	parsed, err := parseLiveCallRequest(context)
	require.NoError(t, err)
	require.Equal(t, "v=0\r\n", parsed.SDP)
	require.JSONEq(t, session, string(parsed.Session))
	require.Equal(t, "client", jsonPathString(t, parsed.Session, "delegation", "type"))
}

func TestParseLiveCallRequestJSONPreservesSessionWithoutDelegation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"sdp":"v=0\\r\\n","session":{"model":"gpt-live-test","instructions":"standalone"}}`
	request := httptest.NewRequest("POST", "/backend-api/codex/realtime/calls", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	parsed, err := parseLiveCallRequest(context)
	require.NoError(t, err)
	require.NotContains(t, string(parsed.Session), "delegation")
	require.Equal(t, "standalone", jsonPathString(t, parsed.Session, "instructions"))
}

func TestParseLiveCallRequestRejectsInvalidJSONShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	testCases := []string{
		`{"session":{"type":"quicksilver"}}`,
		`{"sdp":"v=0\\r\\n","session":[]}`,
		`{"sdp":"v=0\\r\\n","session":null}`,
		`{"sdp":"v=0\\r\\n","session":{"type":"quicksilver"}} {}`,
	}
	for _, body := range testCases {
		request := httptest.NewRequest("POST", "/backend-api/codex/realtime/calls", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = request
		_, err := parseLiveCallRequest(context)
		require.Error(t, err)
	}
}

func TestLiveInitialSessionUsesLiveAuditProtocolBeforeDownstreamServices(t *testing.T) {
	gin.SetMode(gin.TestMode)
	session := `{"model":"gpt-live-test","instructions":"audit instructions","input_audio_transcription":{"prompt":"audit transcription context"}}`

	for _, test := range []struct {
		name      string
		multipart bool
	}{
		{name: "json"},
		{name: "multipart", multipart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := blockingHandlerPromptEngine()
			handler := &OpenAIGatewayHandler{securityAuditCoordinator: securityaudit.NewCoordinator(nil, engine)}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				groupID := int64(3)
				user := &service.User{ID: 7, Username: "live-user", Email: "live@example.test"}
				c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
					ID: 9, UserID: 7, User: user, Name: "live-key", GroupID: &groupID,
					Group: &service.Group{ID: groupID, Name: "live-group", Platform: service.PlatformOpenAI, AllowLive: true},
				})
				c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7, Concurrency: 2})
				c.Next()
			})
			router.POST("/v1/live", handler.Live)

			contentType := "application/json"
			body := bytes.NewBufferString(`{"sdp":"v=0\\r\\n","session":` + session + `}`)
			if test.multipart {
				body.Reset()
				writer := multipart.NewWriter(body)
				require.NoError(t, writer.WriteField("sdp", "v=0\r\n"))
				require.NoError(t, writer.WriteField("session", session))
				require.NoError(t, writer.Close())
				contentType = writer.FormDataContentType()
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/live", body)
			request.Header.Set("Content-Type", contentType)
			recorder := httptest.NewRecorder()
			require.NotPanics(t, func() { router.ServeHTTP(recorder, request) }, "blocking audit must stop before nil downstream services")

			require.Equal(t, http.StatusForbidden, recorder.Code)
			evaluated, _, requests := engine.snapshot()
			require.Equal(t, 1, evaluated)
			require.Len(t, requests, 1)
			require.Equal(t, service.ContentModerationProtocolOpenAILive, requests[0].Protocol)
			require.JSONEq(t, session, string(requests[0].Body))
			require.Equal(t, "gpt-live-test", requests[0].Model)
		})
	}
}

func TestLiveSidebandLocationMatchesCreateRoute(t *testing.T) {
	require.Equal(t, "/v1/live/call_123", liveSidebandLocation("/v1/live", "call_123"))
	require.Equal(
		t,
		"/backend-api/codex/call_123",
		liveSidebandLocation("/backend-api/codex/realtime/calls", "call_123"),
	)
}

func TestLiveEnabledForAPIKey(t *testing.T) {
	require.False(t, liveEnabledForAPIKey(nil))
	require.False(t, liveEnabledForAPIKey(&service.APIKey{}))
	require.False(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI},
	}))
	require.False(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformAnthropic, AllowLive: true},
	}))
	require.True(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformOpenAI, AllowLive: true},
	}))
	require.True(t, liveEnabledForAPIKey(&service.APIKey{
		Group: &service.Group{Platform: service.PlatformComposite, AllowLive: true},
	}))
}

func TestLiveAttestationErrorIsExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	(&OpenAIGatewayHandler{}).writeLiveCreateError(context, &service.LiveAttestationUnavailableError{
		Reason: "Live attestation is only supported when Sub2API runs on macOS",
	})

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Sub2API runs on macOS")
}

func TestLiveOAuthSessionPolicyDenialIsForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	(&OpenAIGatewayHandler{}).writeLiveCreateError(context, service.ErrOpenAIOAuthSessionAccessDenied)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "permission_error")
	require.Contains(t, recorder.Body.String(), "authorized API key groups")
}

func jsonPathString(t *testing.T, raw json.RawMessage, keys ...string) string {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	current := value
	for _, key := range keys {
		object, ok := current.(map[string]any)
		require.True(t, ok)
		current = object[key]
	}
	result, ok := current.(string)
	require.True(t, ok)
	return result
}
