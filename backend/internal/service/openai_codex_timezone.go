package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	openAICodexEnvironmentOpenTag  = "<environment_context>"
	openAICodexEnvironmentCloseTag = "</environment_context>"
)

// resolveOpenAICodexEnvironmentTimezone 返回账号实际出口对应的模型可见时区。
//
// 规则：
//   - 代理账号：先匹配精确代理 ID，再匹配可选的 "*" 兜底；
//   - 直连账号：使用 gateway.openai_codex_direct_timezone；
//   - 代理账号未匹配：不改写。
//
// 最后一条是刻意约束：代理请求若回落到直连时区，反而会制造更明显的 IP/时区矛盾。
func (s *OpenAIGatewayService) resolveOpenAICodexEnvironmentTimezone(
	account *Account,
) (timezoneName, currentDate string, ok bool) {
	if s == nil || s.cfg == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return "", "", false
	}
	// UsesOpenAICodexProtocol 为旧入口兼容所有 OAuth 账号，包括没有显式 OpenAI
	// 平台字段的历史记录；这里保留该兼容，但拒绝改写显式 Grok、Anthropic 等平台。
	if account.Platform != "" && !account.IsOpenAI() {
		return "", "", false
	}

	if account.Proxy != nil && account.Proxy.ID > 0 {
		timezoneName = openAICodexProxyTimezoneForID(
			s.cfg.Gateway.OpenAICodexProxyTimezones,
			account.Proxy.ID,
		)
		if strings.TrimSpace(timezoneName) == "" {
			return "", "", false
		}
	} else {
		timezoneName = strings.TrimSpace(s.cfg.Gateway.OpenAICodexDirectTimezone)
		if timezoneName == "" {
			return "", "", false
		}
	}

	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		// 配置的时区无效时保持客户端原始上下文，不猜测兜底值。
		return "", "", false
	}

	return timezoneName, time.Now().In(loc).Format("2006-01-02"), true
}

func (s *OpenAIGatewayService) rewriteOpenAICodexEnvironmentTimezoneForAccount(
	body []byte,
	account *Account,
) ([]byte, bool, error) {
	timezoneName, currentDate, ok := s.resolveOpenAICodexEnvironmentTimezone(account)
	if !ok {
		return body, false, nil
	}
	return rewriteOpenAICodexEnvironmentTimezoneRaw(body, timezoneName, currentDate)
}

func openAICodexProxyTimezoneForID(raw string, proxyID int64) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || proxyID <= 0 {
		return ""
	}

	target := strconv.FormatInt(proxyID, 10)
	wildcard := ""
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';'
	}) {
		key, value, found := strings.Cut(strings.TrimSpace(entry), "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch key {
		case target:
			return value
		case "*":
			wildcard = value
		}
	}
	return wildcard
}

// rewriteOpenAICodexEnvironmentTimezoneRaw 只改写 Responses 请求中既有的
// user-role <environment_context>，客户端未发送时不会凭空创建。
func rewriteOpenAICodexEnvironmentTimezoneRaw(
	body []byte,
	timezoneName string,
	currentDate string,
) ([]byte, bool, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body, false, nil
	}

	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, false, nil
	}

	rewritten := body
	changed := false
	for i, item := range input.Array() {
		if !strings.EqualFold(strings.TrimSpace(item.Get("role").String()), "user") {
			continue
		}

		content := item.Get("content")
		if content.Type == gjson.String {
			nextText, textChanged := rewriteOpenAICodexEnvironmentContextText(
				content.String(),
				timezoneName,
				currentDate,
			)
			if textChanged {
				var err error
				rewritten, err = sjson.SetBytes(
					rewritten,
					fmt.Sprintf("input.%d.content", i),
					nextText,
				)
				if err != nil {
					return body, false, err
				}
				changed = true
			}
			continue
		}

		if !content.IsArray() {
			continue
		}
		for j, part := range content.Array() {
			var (
				text string
				path string
			)
			switch {
			case part.Type == gjson.String:
				text = part.String()
				path = fmt.Sprintf("input.%d.content.%d", i, j)
			case part.Get("text").Type == gjson.String:
				text = part.Get("text").String()
				path = fmt.Sprintf("input.%d.content.%d.text", i, j)
			default:
				continue
			}

			nextText, textChanged := rewriteOpenAICodexEnvironmentContextText(
				text,
				timezoneName,
				currentDate,
			)
			if !textChanged {
				continue
			}
			var err error
			rewritten, err = sjson.SetBytes(rewritten, path, nextText)
			if err != nil {
				return body, false, err
			}
			changed = true
		}
	}

	return rewritten, changed, nil
}

// rewriteOpenAICodexEnvironmentTimezoneMap 用于请求已经解码后的 WS response.create 热路径。
func rewriteOpenAICodexEnvironmentTimezoneMap(
	body map[string]any,
	timezoneName string,
	currentDate string,
) bool {
	if len(body) == 0 {
		return false
	}
	input, ok := body["input"].([]any)
	if !ok {
		return false
	}

	changed := false
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		role, _ := item["role"].(string)
		if !strings.EqualFold(strings.TrimSpace(role), "user") {
			continue
		}

		switch content := item["content"].(type) {
		case string:
			nextText, textChanged := rewriteOpenAICodexEnvironmentContextText(
				content,
				timezoneName,
				currentDate,
			)
			if textChanged {
				item["content"] = nextText
				changed = true
			}
		case []any:
			for i, rawPart := range content {
				switch part := rawPart.(type) {
				case string:
					nextText, textChanged := rewriteOpenAICodexEnvironmentContextText(
						part,
						timezoneName,
						currentDate,
					)
					if textChanged {
						content[i] = nextText
						changed = true
					}
				case map[string]any:
					text, _ := part["text"].(string)
					if text == "" {
						continue
					}
					nextText, textChanged := rewriteOpenAICodexEnvironmentContextText(
						text,
						timezoneName,
						currentDate,
					)
					if textChanged {
						part["text"] = nextText
						changed = true
					}
				}
			}
		}
	}
	return changed
}

func rewriteOpenAICodexEnvironmentContextText(
	text string,
	timezoneName string,
	currentDate string,
) (string, bool) {
	// Codex 会把 environment_context 作为独立文本 part 发送。只有整个 trim 后的 part
	// 恰好是该块才允许改写，避免篡改用户粘贴的日志、引文或 Markdown 示例。
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, openAICodexEnvironmentOpenTag) ||
		!strings.HasSuffix(trimmed, openAICodexEnvironmentCloseTag) {
		return text, false
	}

	contentStart := len(openAICodexEnvironmentOpenTag)
	closeRelative := strings.Index(trimmed[contentStart:], openAICodexEnvironmentCloseTag)
	if closeRelative < 0 || contentStart+closeRelative+len(openAICodexEnvironmentCloseTag) != len(trimmed) {
		return text, false
	}

	nextBlock, timezoneChanged := rewriteOpenAICodexEnvironmentElement(trimmed, "timezone", timezoneName)
	nextBlock, dateChanged := rewriteOpenAICodexEnvironmentElement(nextBlock, "current_date", currentDate)
	if !timezoneChanged && !dateChanged {
		return text, false
	}

	start := strings.Index(text, trimmed)
	return text[:start] + nextBlock + text[start+len(trimmed):], true
}

func rewriteOpenAICodexEnvironmentElement(
	block string,
	name string,
	value string,
) (string, bool) {
	openTag := "<" + name + ">"
	closeTag := "</" + name + ">"
	openIndex := strings.Index(block, openTag)
	if openIndex < 0 {
		return block, false
	}
	valueStart := openIndex + len(openTag)
	closeRelative := strings.Index(block[valueStart:], closeTag)
	if closeRelative < 0 {
		return block, false
	}
	valueEnd := valueStart + closeRelative
	if block[valueStart:valueEnd] == value {
		return block, false
	}
	return block[:valueStart] + value + block[valueEnd:], true
}
