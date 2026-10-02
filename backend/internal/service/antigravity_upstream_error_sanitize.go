package service

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var (
	antigravityProjectRefRegex = regexp.MustCompile(`(?i)\bprojects/[a-z0-9][a-z0-9._:-]*`)
	antigravityEmailRegex      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	antigravityConsumerRegex   = regexp.MustCompile(`(?i)\b(consumer|project(?:[ _-]?(?:id|number))?)(\s*[:=]?\s*['"]?)[0-9]{6,}`)
	// antigravityJSONNumericConsumerRegex 匹配 JSON 对象里以裸数字出现的 consumer/project 标识。
	// 纯文本脱敏会把这些数字替换成 `***`，导致合法 JSON 变成非法 token；先统一改写为字符串字面量。
	antigravityJSONNumericConsumerRegex = regexp.MustCompile(`(?i)("(?:consumer|project(?:[ _-]?(?:id|number))?)"\s*:\s*)([0-9]{6,})`)
)

// sanitizeAntigravityErrorText 清除上游错误文本中的 GCP 项目号/项目 ID、服务账号邮箱及敏感查询参数。
func sanitizeAntigravityErrorText(msg string) string {
	if msg == "" {
		return msg
	}
	msg = sanitizeUpstreamErrorMessage(msg)
	msg = antigravityProjectRefRegex.ReplaceAllString(msg, "projects/***")
	msg = antigravityEmailRegex.ReplaceAllString(msg, "***")
	msg = antigravityConsumerRegex.ReplaceAllString(msg, "$1$2***")
	return msg
}

// SanitizeAntigravityErrorMessage 是对 sanitizeAntigravityErrorText 的导出版本，
// 供 handler 层在回写 failover 上游错误体时复用同一脱敏边界。
func SanitizeAntigravityErrorMessage(msg string) string {
	return sanitizeAntigravityErrorText(msg)
}

// SanitizeFailoverClientMessage 在 handler 命中错误透传规则、准备把 failover
// 上游错误体回写客户端前调用：仅当该 failover 错误标记了 RedactClientBody
// （Antigravity 账号池身份）时脱敏，其他平台保持原有文案不变。
func SanitizeFailoverClientMessage(failoverErr *UpstreamFailoverError, msg string) string {
	if failoverErr == nil || !failoverErr.RedactClientBody {
		return msg
	}
	return sanitizeAntigravityErrorText(msg)
}

// sanitizeAntigravityErrorBody 对直接透传的原始上游错误体做脱敏。
// 脱敏正则只会命中字符串内容，但裸 JSON 数值（如 "consumer":123456789）被替换后
// 会产生非法 token，因此对合法 JSON 先做等价的字符串化，保证响应体仍可解析。
func sanitizeAntigravityErrorBody(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	text := string(body)
	if json.Valid(body) {
		text = antigravityJSONNumericConsumerRegex.ReplaceAllString(text, `$1"***"`)
	}
	return []byte(sanitizeAntigravityErrorText(text))
}

// buildAntigravityClientErrorBody 为客户端构造 Gemini 风格的错误体：
// 仅保留 code/status/message（message 已脱敏），丢弃 details 等可能含账号身份的字段。
func buildAntigravityClientErrorBody(statusCode int, body []byte) []byte {
	code := statusCode
	status := ""
	message := ""

	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error.Code != 0 {
			code = parsed.Error.Code
		}
		status = parsed.Error.Status
		message = parsed.Error.Message
	}
	if strings.TrimSpace(message) == "" {
		message = strings.TrimSpace(extractUpstreamErrorMessage(body))
	}
	if strings.TrimSpace(message) == "" {
		message = http.StatusText(statusCode)
		if message == "" {
			message = "Upstream request failed"
		}
	}
	if status == "" {
		status = antigravityGeminiStatusFromHTTP(statusCode)
	}

	out, err := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": sanitizeAntigravityErrorText(message),
			"status":  status,
		},
	})
	if err != nil {
		return []byte(`{"error":{"code":500,"message":"Upstream request failed","status":"INTERNAL"}}`)
	}
	return out
}

func antigravityGeminiStatusFromHTTP(statusCode int) string {
	switch statusCode {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusUnauthorized:
		return "UNAUTHENTICATED"
	case http.StatusForbidden:
		return "PERMISSION_DENIED"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	case http.StatusGatewayTimeout:
		return "DEADLINE_EXCEEDED"
	default:
		if statusCode >= 500 {
			return "INTERNAL"
		}
		return "UNKNOWN"
	}
}
