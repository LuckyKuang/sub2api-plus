package service

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// openAIUpstreamClientErrorFallbackType 是上游没给 error.type 时的兜底值。
// 与 handleCompatErrorResponse 对 400 的取值保持一致。
const openAIUpstreamClientErrorFallbackType = "invalid_request_error"

// openAIUpstreamClientErrorFallbackMessage 是上游连 message 都没给时的兜底文案。
// 仍然比 "Upstream request failed" 明确：它说明拒绝来自请求本身，而不是链路故障。
const openAIUpstreamClientErrorFallbackMessage = "Upstream rejected the request"

// isOpenAIDeterministicClientError 判断上游状态码是否表示「请求本身非法」。
//
// 只认 400：同一份请求体换任何账号、重试多少次都会得到同样的结果。
//   - 401/402/403 是网关运营方的凭据/账单问题，继续包成 502，不向客户端暴露上游账号状态。
//   - 404/405 既可能是模型不存在，也可能是上游 base_url 配错，同属运营方问题，保持现状。
//   - 429 已有独立分支映射成 429。
//   - 413 在更上面就按 request-body-too-large 走 failover 了。
func isOpenAIDeterministicClientError(statusCode int) bool {
	return statusCode == http.StatusBadRequest
}

// writeOpenAIUpstreamClientError 以 OpenAI 错误体形状回写确定性客户端错误。
//
// 保留上游的 type/code/param：客户端靠 param 定位是哪个字段非法（上游会给出形如
// input[8].tools[1].tools[2].parameters 的路径），靠 code 判断是否值得重试。归一成
// {type:"upstream_error", message:"Upstream request failed"} 会把这些信息全部抹掉。
//
// upstreamMsg 由调用方传入，调用方已做过 sanitizeUpstreamErrorMessage 与
// redactAgentIdentitySensitiveBody；这里不重复清洗，也不回落读取原始 body 的
// message，避免绕开那两道脱敏。
func writeOpenAIUpstreamClientError(c *gin.Context, statusCode int, body []byte, upstreamMsg string) {
	if nested := gjson.GetBytes(body, "response.error"); !gjson.GetBytes(body, "error").Exists() && nested.IsObject() {
		body = []byte(`{"error":` + nested.Raw + `}`)
	}
	errorPayload := gin.H{"type": openAIUpstreamClientErrorFallbackType}
	if errType := openAIPassthroughClientErrorField(nil, gjson.GetBytes(body, "error.type")); errType != "" {
		errorPayload["type"] = errType
	}
	if code := openAIPassthroughClientErrorCode(nil, body); code != "" {
		errorPayload["code"] = code
	}
	if param := openAIPassthroughClientErrorField(nil, gjson.GetBytes(body, "error.param")); param != "" {
		errorPayload["param"] = param
	}
	message := strings.TrimSpace(upstreamMsg)
	if message == "" {
		message = openAIUpstreamClientErrorFallbackMessage
	}
	errorPayload["message"] = message

	publicStatus := statusCode
	key := getOpenAIErrorMessageSource(c).key
	if key == "" && isOpenAIContextWindowError(upstreamMsg, body) && strings.TrimSpace(upstreamMsg) != "" {
		key = "context_length_exceeded"
	}
	classified := 0
	if key == "" {
		key, classified = ClassifyCodexPublicFailure(statusCode, body)
	} else {
		key, classified = ClassifyCodexPublicFailureKey(statusCode, key)
	}
	if key != "" {
		SetCodexPublicErrorFinal(c, key, classified)
		if statusCode >= 400 {
			publicStatus = classified
		}
		if definition, ok := codexPublicErrorByKey(key); ok {
			// Direct callers (without the output middleware) still receive the
			// public type/code/message; the middleware rebuilds the same fields.
			if definition.ErrorType != "" {
				errorPayload["type"] = definition.ErrorType
			}
			if definition.PublicCode != "" {
				errorPayload["code"] = definition.PublicCode
			}
			errorPayload["message"] = definition.DefaultMessage
			if param, _ := errorPayload["param"].(string); !codexPublicErrorParamAllowed(param) {
				delete(errorPayload, "param")
			}
		}
	}
	c.JSON(publicStatus, gin.H{"error": errorPayload})
}

// WriteOpenAIUpstreamClientError preserves a structured deterministic upstream
// client error when the handler has exhausted all eligible accounts.
func WriteOpenAIUpstreamClientError(c *gin.Context, statusCode int, body []byte, upstreamMsg string) {
	writeOpenAIUpstreamClientError(c, statusCode, body, upstreamMsg)
}

// isOpenAIInvalidEncryptedContentError 判断上游 400 错误是否为 Responses
// reasoning/compaction 密文校验失败（invalid_encrypted_content）。
// 该错误属于确定性请求失败，客户端需据此识别密文失效或验签失败。
func isOpenAIInvalidEncryptedContentError(statusCode int, upstreamMsg string, body []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	code := strings.ToLower(strings.TrimSpace(extractUpstreamErrorCode(body)))
	if code == "invalid_encrypted_content" {
		return true
	}
	if code == "thinking_signature_invalid" {
		lowerMsg := strings.ToLower(strings.TrimSpace(upstreamMsg))
		return strings.Contains(lowerMsg, "encrypted content") &&
			(strings.Contains(lowerMsg, "could not be verified") || strings.Contains(lowerMsg, "could not be decrypted"))
	}
	return false
}

// sanitizeOpenAIPassthroughClientErrorText 脱敏透传错误字段，除常规敏感参数外，
// 若账号配置了自定义 base_url 或凭据（api_key/access_token），一并屏蔽，
// 避免上游错误文案泄露真实上游地址或账号凭据。
func sanitizeOpenAIPassthroughClientErrorText(account *Account, text string) string {
	sanitized := sanitizeUpstreamErrorMessage(strings.TrimSpace(text))
	if sanitized == "" || account == nil {
		return sanitized
	}
	for _, credKey := range []string{"api_key", "access_token", "refresh_token"} {
		if secret := strings.TrimSpace(account.GetCredential(credKey)); len(secret) >= 6 {
			sanitized = strings.ReplaceAll(sanitized, secret, "[redacted]")
		}
	}
	rawBaseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if rawBaseURL == "" {
		return sanitized
	}
	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return sanitized
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return sanitized
	}
	return replaceCaseInsensitive(sanitized, host, "[upstream]")
}

// replaceCaseInsensitive 把 input 中所有大小写不敏感匹配 target 的片段替换为
// replacement。
//
// 必须直接在原始字节上匹配：strings.ToLower 会改变字节长度（"Ⱥ" 2→3 字节、
// Kelvin 符号 "K" 3→1 字节、非法 UTF-8 单字节→3 字节 U+FFFD），用小写串的下标
// 去切原串会越界 panic 或漏掉 target。正则的 (?i) 在原串上逐 rune 折叠匹配，
// 不存在下标错位。
//
// target 无法编译为模式（如含非法 UTF-8）时返回空串：调用方会回退到通用文案，
// 宁可丢弃整段文本，也不把未脱敏的内容写给客户端。
func replaceCaseInsensitive(input, target, replacement string) string {
	if input == "" || target == "" {
		return input
	}
	pattern, err := regexp.Compile("(?i)" + regexp.QuoteMeta(target))
	if err != nil {
		return ""
	}
	return pattern.ReplaceAllLiteralString(input, replacement)
}

// openAIPassthroughClientErrorFieldMaxBytes 限制透出的 error.type/code/param
// 单字段长度。三者都是短标识符，超长只可能是上游在夹带其它内容。
const openAIPassthroughClientErrorFieldMaxBytes = 256

// openAIPassthroughClientErrorField 提取允许写给客户端的标量错误字段。
// gjson 对对象/数组的 String() 返回整段原始 JSON，直接透出等于把上游任意
// 扩展字段塞进 type/code/param，因此只接受字符串与数字（部分上游的 code 为
// 数字），并在脱敏后做长度上限校验，超限即丢弃。
func openAIPassthroughClientErrorField(account *Account, value gjson.Result) string {
	if value.Type != gjson.String && value.Type != gjson.Number {
		return ""
	}
	field := sanitizeOpenAIPassthroughClientErrorText(account, value.String())
	if len(field) > openAIPassthroughClientErrorFieldMaxBytes {
		return ""
	}
	return field
}

// openAIPassthroughClientErrorCode 返回允许写给客户端的 error.code。
// error.code 缺失、为 null 或为空白字符串时，沿用 extractUpstreamErrorCode 对
// "message 内嵌 JSON"形态的兼容提取（与错误分类看到的 code 保持一致）；
// error.code 是对象、数组或布尔值时不回退，直接视为无 code。
func openAIPassthroughClientErrorCode(account *Account, body []byte) string {
	code := gjson.GetBytes(body, "error.code")
	blank := !code.Exists() || code.Type == gjson.Null ||
		(code.Type == gjson.String && strings.TrimSpace(code.Str) == "")
	if !blank {
		return openAIPassthroughClientErrorField(account, code)
	}
	inner := sanitizeOpenAIPassthroughClientErrorText(account, extractUpstreamErrorCode(body))
	if len(inner) > openAIPassthroughClientErrorFieldMaxBytes || strings.HasPrefix(inner, "{") || strings.HasPrefix(inner, "[") {
		return ""
	}
	return inner
}
