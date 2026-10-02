//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/model"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// antigravitySensitiveMessage 同时包含三类敏感标识：projects/<id> 引用、服务账号邮箱、consumer 数字 id。
const antigravitySensitiveMessage = "Permission denied on projects/pool-project-123 for pool-sa@my-gcp-proj.iam.gserviceaccount.com; consumer: 987654321"

// requireNoAntigravityIdentifiers 断言客户端可见文本中不再出现任何敏感标识。
func requireNoAntigravityIdentifiers(t *testing.T, body string) {
	t.Helper()
	require.NotContains(t, body, "pool-project-123")
	require.NotContains(t, body, "pool-sa@")
	require.NotContains(t, body, "gserviceaccount.com")
	require.NotContains(t, body, "987654321")
}

func TestBuildAntigravityClientErrorBody_ScrubsPoolIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := []byte(`{"error":{"code":403,"message":"Permission denied on resource project projects/123456789 for consumer: projects/123456789; caller pool-sa@my-gcp-proj.iam.gserviceaccount.com","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","metadata":{"consumer":"projects/123456789","service":"cloudcode-pa.googleapis.com"}}]}}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Data(http.StatusForbidden, "application/json", buildAntigravityClientErrorBody(http.StatusForbidden, upstream))

	require.Equal(t, http.StatusForbidden, rec.Code)
	out := rec.Body.String()
	require.NotContains(t, out, "123456789")
	require.NotContains(t, out, "pool-sa@")
	require.NotContains(t, out, "gserviceaccount.com")
	require.NotContains(t, out, "details")

	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed))
	require.Equal(t, 403, parsed.Error.Code)
	require.Equal(t, "PERMISSION_DENIED", parsed.Error.Status)
	require.True(t, strings.Contains(parsed.Error.Message, "Permission denied"))
}

func TestBuildAntigravityClientErrorBody_NonJSONBody(t *testing.T) {
	out := string(buildAntigravityClientErrorBody(http.StatusTooManyRequests, []byte("quota exceeded for consumer 987654321 sa@x.iam.gserviceaccount.com")))
	require.NotContains(t, out, "987654321")
	require.NotContains(t, out, "gserviceaccount")
	require.Contains(t, out, `"status":"RESOURCE_EXHAUSTED"`)
	require.Contains(t, out, `"code":429`)
}

// TestBuildAntigravityClientErrorBody_RedactsAllIdentifierKinds 覆盖 direct Gemini
// 返回分支（antigravity_gateway_gemini.go 的 buildAntigravityClientErrorBody 调用点）。
func TestBuildAntigravityClientErrorBody_RedactsAllIdentifierKinds(t *testing.T) {
	upstream := []byte(`{"error":{"code":403,"message":"` + antigravitySensitiveMessage + `","status":"PERMISSION_DENIED"}}`)

	out := string(buildAntigravityClientErrorBody(http.StatusForbidden, upstream))

	requireNoAntigravityIdentifiers(t, out)
	require.True(t, json.Valid([]byte(out)), "client body must remain valid JSON")
	require.Equal(t, 403, int(gjsonInt(t, out, "error.code")))
	require.Equal(t, "PERMISSION_DENIED", gjsonString(t, out, "error.status"))
	require.Contains(t, out, "Permission denied")
}

func TestSanitizeAntigravityErrorBody_PreservesJSONWithBareNumericConsumer(t *testing.T) {
	body := []byte(`{"error":{"code":403,"message":"denied","metadata":{"consumer":987654321,"project_number":123456789,"owner":"pool-sa@my-gcp-proj.iam.gserviceaccount.com"}}}`)

	out := sanitizeAntigravityErrorBody(body)

	require.True(t, json.Valid(out), "sanitized passthrough body must stay valid JSON: %s", string(out))
	requireNoAntigravityIdentifiers(t, string(out))
	require.NotContains(t, string(out), "123456789")
	require.Equal(t, 403, int(gjsonInt(t, string(out), "error.code")))
	require.Equal(t, "denied", gjsonString(t, string(out), "error.message"))
}

func TestSanitizeAntigravityErrorBody_NonJSONFallsBackToText(t *testing.T) {
	out := string(sanitizeAntigravityErrorBody([]byte("upstream failed for " + antigravitySensitiveMessage)))

	requireNoAntigravityIdentifiers(t, out)
}

// TestSanitizeAntigravityErrorText_Idempotent 保证同一分支重复经过脱敏边界不会二次转义。
func TestSanitizeAntigravityErrorText_Idempotent(t *testing.T) {
	once := sanitizeAntigravityErrorText(antigravitySensitiveMessage + " ?key=secret-token&access_token=abc")

	requireNoAntigravityIdentifiers(t, once)
	require.NotContains(t, once, "secret-token")
	require.Equal(t, once, sanitizeAntigravityErrorText(once))
	require.Equal(t, once, SanitizeAntigravityErrorMessage(once))
}

func TestWriteClaudeError_SanitizesClientMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	err := (&AntigravityGatewayService{}).writeClaudeError(c, http.StatusBadGateway, "upstream_error", antigravitySensitiveMessage)

	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	requireNoAntigravityIdentifiers(t, rec.Body.String())
	require.Equal(t, "error", gjsonString(t, rec.Body.String(), "type"))
	require.Equal(t, "upstream_error", gjsonString(t, rec.Body.String(), "error.type"))
	require.NotContains(t, err.Error(), "987654321")
}

func TestWriteGoogleError_SanitizesClientMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	err := (&AntigravityGatewayService{}).writeGoogleError(c, http.StatusForbidden, antigravitySensitiveMessage)

	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, rec.Code)
	requireNoAntigravityIdentifiers(t, rec.Body.String())
	require.Equal(t, "UNKNOWN", gjsonString(t, rec.Body.String(), "error.status"))
	require.Equal(t, 403, int(gjsonInt(t, rec.Body.String(), "error.code")))
}

func TestWriteAntigravityCompatError_SanitizesClientMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	err := (&AntigravityGatewayService{}).writeAntigravityCompatError(c, http.StatusBadGateway, "upstream_error", antigravitySensitiveMessage)

	require.Error(t, err)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	requireNoAntigravityIdentifiers(t, rec.Body.String())
	require.Equal(t, "upstream_error", gjsonString(t, rec.Body.String(), "error.type"))
}

// TestWriteMappedClaudeError_PassthroughBodyBranch 覆盖 400 白名单透传分支：
// message 必须仍包含 "prompt is too long"，但敏感标识不得出现在客户端响应中。
func TestWriteMappedClaudeError_PassthroughBodyBranch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{"error":{"message":"prompt is too long: ` + antigravitySensitiveMessage + `","status":"INVALID_ARGUMENT"}}`)
	account := &Account{ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}

	err := (&AntigravityGatewayService{}).writeMappedClaudeError(c, account, http.StatusBadRequest, "req-1", body)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjsonString(t, rec.Body.String(), "error.type"))
	require.Contains(t, gjsonString(t, rec.Body.String(), "error.message"), "prompt is too long")
	requireNoAntigravityIdentifiers(t, rec.Body.String())
	// ops 侧仍保留未追加脱敏的 upstream message，证明只改变客户端回写内容。
	opsMsg, ok := c.Get(OpsUpstreamErrorMessageKey)
	require.True(t, ok)
	opsMsgStr, ok := opsMsg.(string)
	require.True(t, ok)
	require.Contains(t, opsMsgStr, "pool-sa@my-gcp-proj.iam.gserviceaccount.com")
}

func TestWriteMappedClaudeError_ErrorPassthroughRuleBranch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	ruleSvc := &ErrorPassthroughService{}
	ruleSvc.setLocalCache([]*model.ErrorPassthroughRule{{
		ID:              1,
		Name:            "antigravity-passthrough",
		Enabled:         true,
		Priority:        1,
		ErrorCodes:      []int{http.StatusBadRequest},
		Keywords:        []string{"prompt is too long"},
		MatchMode:       model.MatchModeAll,
		PassthroughCode: true,
		PassthroughBody: true,
	}})
	BindErrorPassthroughService(c, ruleSvc)

	body := []byte(`{"error":{"message":"prompt is too long: ` + antigravitySensitiveMessage + `","status":"INVALID_ARGUMENT"}}`)
	account := &Account{ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}

	err := (&AntigravityGatewayService{}).writeMappedClaudeError(c, account, http.StatusBadRequest, "req-2", body)

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "upstream_error", gjsonString(t, rec.Body.String(), "error.type"))
	require.Contains(t, gjsonString(t, rec.Body.String(), "error.message"), "prompt is too long")
	requireNoAntigravityIdentifiers(t, rec.Body.String())
}

func TestWriteMappedAntigravityCompatError_SanitizesClientMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := []byte(`{"error":{"code":403,"message":"prompt is too long: ` + antigravitySensitiveMessage + `","status":"PERMISSION_DENIED"}}`)
	account := &Account{ID: 7, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}

	err := (&AntigravityGatewayService{}).writeMappedAntigravityCompatError(c, account, http.StatusForbidden, "req-3", body)

	require.Error(t, err)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "upstream_error", gjsonString(t, rec.Body.String(), "error.type"))
	require.Contains(t, gjsonString(t, rec.Body.String(), "error.message"), "prompt is too long")
	requireNoAntigravityIdentifiers(t, rec.Body.String())
}

// TestSanitizeFailoverClientMessage 覆盖 failover 耗尽后 handler 透传分支的脱敏边界。
func TestSanitizeFailoverClientMessage(t *testing.T) {
	antigravityErr := newAntigravityUpstreamFailoverError(http.StatusForbidden, []byte(`{}`), false)
	require.True(t, antigravityErr.RedactClientBody, "antigravity failover errors must opt into client-body redaction")
	requireNoAntigravityIdentifiers(t, SanitizeFailoverClientMessage(antigravityErr, antigravitySensitiveMessage))

	other := &UpstreamFailoverError{StatusCode: http.StatusForbidden}
	require.Equal(t, antigravitySensitiveMessage, SanitizeFailoverClientMessage(other, antigravitySensitiveMessage))
	require.Equal(t, antigravitySensitiveMessage, SanitizeFailoverClientMessage(nil, antigravitySensitiveMessage))
}

// TestWriteGeminiNativeUpstreamError_AntigravityBodySanitized 覆盖 Antigravity APIKey
// 账号经 GeminiMessagesCompatService 原生透传写回原始上游错误体的分支。
func TestWriteGeminiNativeUpstreamError_AntigravityBodySanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"error":{"code":403,"message":"` + antigravitySensitiveMessage + `","status":"PERMISSION_DENIED"}}`)

	t.Run("antigravity account is redacted", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}}
		account := &Account{ID: 9, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}

		err := (&GeminiMessagesCompatService{}).writeGeminiNativeUpstreamError(c, account, resp, body, "req-a", false)

		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.True(t, json.Valid(rec.Body.Bytes()), "sanitized body must remain valid JSON: %s", rec.Body.String())
		require.Equal(t, 403, int(gjsonInt(t, rec.Body.String(), "error.code")))
		require.Equal(t, "PERMISSION_DENIED", gjsonString(t, rec.Body.String(), "error.status"))
		requireNoAntigravityIdentifiers(t, rec.Body.String())
	})

	t.Run("gemini account body is untouched", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": []string{"application/json"}}}
		account := &Account{ID: 10, Platform: PlatformGemini, Type: AccountTypeAPIKey}

		err := (&GeminiMessagesCompatService{}).writeGeminiNativeUpstreamError(c, account, resp, body, "req-b", false)

		require.Error(t, err)
		require.Equal(t, string(body), rec.Body.String())
	})
}

// TestWriteGeminiMappedError_AntigravityMessageSanitized 覆盖 Antigravity APIKey 账号
// 的 Gemini 错误映射分支（400 回传上游 message）与透传规则分支。
func TestWriteGeminiMappedError_AntigravityMessageSanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("400 branch echoes sanitized upstream message", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		body := []byte(`{"error":{"code":400,"message":"` + antigravitySensitiveMessage + `","status":"INVALID_ARGUMENT"}}`)
		account := &Account{ID: 9, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}

		err := (&GeminiMessagesCompatService{}).writeGeminiMappedError(c, account, http.StatusBadRequest, "req-c", body)

		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "invalid_request_error", gjsonString(t, rec.Body.String(), "error.type"))
		require.Contains(t, gjsonString(t, rec.Body.String(), "error.message"), "Permission denied")
		requireNoAntigravityIdentifiers(t, rec.Body.String())
	})

	t.Run("passthrough rule branch is sanitized", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		ruleSvc := &ErrorPassthroughService{}
		ruleSvc.setLocalCache([]*model.ErrorPassthroughRule{{
			ID:              2,
			Name:            "antigravity-gemini-passthrough",
			Enabled:         true,
			Priority:        1,
			ErrorCodes:      []int{http.StatusForbidden},
			Keywords:        []string{"permission denied"},
			MatchMode:       model.MatchModeAll,
			PassthroughCode: true,
			PassthroughBody: true,
		}})
		BindErrorPassthroughService(c, ruleSvc)

		body := []byte(`{"error":{"code":403,"message":"` + antigravitySensitiveMessage + `","status":"PERMISSION_DENIED"}}`)
		account := &Account{ID: 9, Platform: PlatformAntigravity, Type: AccountTypeAPIKey}

		err := (&GeminiMessagesCompatService{}).writeGeminiMappedError(c, account, http.StatusForbidden, "req-d", body)

		require.Error(t, err)
		require.Equal(t, http.StatusForbidden, rec.Code)
		require.Equal(t, "upstream_error", gjsonString(t, rec.Body.String(), "error.type"))
		require.Contains(t, gjsonString(t, rec.Body.String(), "error.message"), "Permission denied")
		requireNoAntigravityIdentifiers(t, rec.Body.String())
	})
}

// antigravitySanitizeErrorUpstream 返回固定的上游错误响应，供 ForwardUpstream 透传分支测试使用。
type antigravitySanitizeErrorUpstream struct {
	status int
	body   string
}

func (u *antigravitySanitizeErrorUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return &http.Response{
		StatusCode: u.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(u.body)),
	}, nil
}

func (u *antigravitySanitizeErrorUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

// TestForwardUpstream_ErrorPassthroughBodySanitized 覆盖 ForwardUpstream 直接把上游
// 错误体透传写回客户端的分支（Claude 格式 passthrough），状态码与错误语义保持不变。
func TestForwardUpstream_ErrorPassthroughBodySanitized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamBody := `{"type":"error","error":{"type":"permission_error","message":"` + antigravitySensitiveMessage + `"}}`
	upstream := &antigravitySanitizeErrorUpstream{status: http.StatusForbidden, body: upstreamBody}
	svc := &AntigravityGatewayService{httpUpstream: upstream}

	reqBody := []byte(`{"model":"claude-sonnet-4-5","stream":false,"messages":[]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(reqBody))

	account := &Account{
		ID:          42,
		Name:        "pool-upstream",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeUpstream,
		Credentials: map[string]any{"base_url": "https://upstream.example", "api_key": "sk-test"},
	}

	result, err := svc.ForwardUpstream(context.Background(), c, account, reqBody)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.True(t, json.Valid(rec.Body.Bytes()), "passthrough error body must remain valid JSON: %s", rec.Body.String())
	require.Equal(t, "permission_error", gjsonString(t, rec.Body.String(), "error.type"))
	requireNoAntigravityIdentifiers(t, rec.Body.String())
}

func gjsonString(t *testing.T, body, path string) string {
	t.Helper()
	var payload any
	require.NoError(t, json.Unmarshal([]byte(body), &payload))
	cur := payload
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		require.True(t, ok, "path %s not found in %s", path, body)
		cur, ok = m[part]
		require.True(t, ok, "path %s not found in %s", path, body)
	}
	s, ok := cur.(string)
	require.True(t, ok, "path %s is not a string in %s", path, body)
	return s
}

func gjsonInt(t *testing.T, body, path string) int64 {
	t.Helper()
	var payload any
	require.NoError(t, json.Unmarshal([]byte(body), &payload))
	cur := payload
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		require.True(t, ok, "path %s not found in %s", path, body)
		cur, ok = m[part]
		require.True(t, ok, "path %s not found in %s", path, body)
	}
	f, ok := cur.(float64)
	require.True(t, ok, "path %s is not a number in %s", path, body)
	return int64(f)
}
