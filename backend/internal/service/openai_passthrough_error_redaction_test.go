//go:build unit

package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 上游错误文案可能带回中转地址或账号凭据，面向客户端的透传错误必须先脱敏。
func TestSanitizeOpenAIPassthroughClientErrorTextRedactsAccountSecrets(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-live-secret-value",
			"base_url": "https://relay.internal.example.com/v1",
		},
	}
	sanitized := sanitizeOpenAIPassthroughClientErrorText(account,
		"request to relay.internal.example.com rejected for key sk-live-secret-value?token=abc")
	require.NotContains(t, sanitized, "relay.internal.example.com")
	require.NotContains(t, sanitized, "sk-live-secret-value")
	require.Contains(t, sanitized, "[upstream]")
	require.Contains(t, sanitized, "[redacted]")
}

// strings.ToLower 会改变部分字符的字节长度，按小写串下标切原串会越界；
// 大小写不敏感替换必须直接在原串上匹配。
func TestReplaceCaseInsensitiveSurvivesLengthChangingCaseFolds(t *testing.T) {
	require.Equal(t, "[upstream]", replaceCaseInsensitive("K", "k", "[upstream]")) // Kelvin sign
	require.Equal(t, "[upstream]", replaceCaseInsensitive("Ⱥ", "Ⱥ", "[upstream]")) // 2→3 字节
	require.Equal(t, "a[upstream]b", replaceCaseInsensitive("aHOSTb", "host", "[upstream]"))
	require.Equal(t, "", replaceCaseInsensitive("text", "\xff", "x"))
}

func TestIsOpenAIInvalidEncryptedContentError(t *testing.T) {
	require.True(t, isOpenAIInvalidEncryptedContentError(http.StatusBadRequest, "",
		[]byte(`{"error":{"code":"invalid_encrypted_content","message":"bad"}}`)))
	require.True(t, isOpenAIInvalidEncryptedContentError(http.StatusBadRequest,
		"Encrypted content could not be verified",
		[]byte(`{"error":{"code":"thinking_signature_invalid"}}`)))
	require.False(t, isOpenAIInvalidEncryptedContentError(http.StatusInternalServerError, "",
		[]byte(`{"error":{"code":"invalid_encrypted_content"}}`)))
	require.False(t, isOpenAIInvalidEncryptedContentError(http.StatusBadRequest,
		"rate limited", []byte(`{"error":{"code":"slow_down"}}`)))
}

// error.type/code/param 只接受标量并限制长度，避免把上游任意扩展字段塞进信封。
func TestOpenAIPassthroughClientErrorFieldRejectsNonScalarsAndOversizedValues(t *testing.T) {
	require.Equal(t, "", openAIPassthroughClientErrorField(nil,
		gjson.Parse(`{"nested":"value"}`)))
	require.Equal(t, "", openAIPassthroughClientErrorField(nil,
		gjson.Parse(`"`+strings.Repeat("x", openAIPassthroughClientErrorFieldMaxBytes+1)+`"`)))
	require.Equal(t, "400", openAIPassthroughClientErrorField(nil, gjson.Parse(`400`)))
	require.Equal(t, "invalid_request_error", openAIPassthroughClientErrorField(nil,
		gjson.Parse(`"invalid_request_error"`)))
}

func TestWriteOpenAIUpstreamClientErrorKeepsEnvelopeScalar(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	writeOpenAIUpstreamClientError(c, http.StatusBadRequest, []byte(`{
		"error": {
			"type": {"unexpected": "object"},
			"code": "invalid_encrypted_content",
			"param": {"path": "input[0]"},
			"message": "bad encrypted content"
		}
	}`), "bad encrypted content")

	body := recorder.Body.String()
	require.NotContains(t, body, `"unexpected"`)
	require.NotContains(t, body, `"path"`)
	errorPayload := gjson.Get(body, "error")
	require.Equal(t, "invalid_request_error", errorPayload.Get("type").String())
	require.Equal(t, "invalid_encrypted_content", errorPayload.Get("code").String())
	require.False(t, errorPayload.Get("param").Exists())
}
