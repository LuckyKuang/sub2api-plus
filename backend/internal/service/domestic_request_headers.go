package service

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/tidwall/gjson"
)

func hasDomesticRequestHeaderContract(account *Account) bool {
	if account == nil {
		return false
	}
	switch account.Platform {
	case PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformZhipu, PlatformStepFun:
		return true
	}
	return false
}

// Prepare request state from the selected provider, rather than carrying a
// caller's Claude/Codex SDK or execution declarations under a different UA.
// Authentication and capability declarations remain protocol-owned.
func prepareDomesticRequestHeaders(req *http.Request, account *Account) {
	if req == nil || !hasDomesticRequestHeaderContract(account) {
		return
	}
	for name := range req.Header {
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.HasPrefix(lower, "x-codex-") || strings.HasPrefix(lower, "x-grok-") ||
			strings.HasPrefix(lower, "x-openai-") || strings.HasPrefix(lower, "x-responsesapi-") ||
			strings.HasPrefix(lower, "x-claude-") || lower == "x-client-request-id" ||
			strings.HasPrefix(lower, "x-mavis-") || strings.HasPrefix(lower, "x-deepseek-harness-") ||
			lower == "x-zcode-trace-id" || lower == "x-zcode-session-type" || lower == "x-zcode-query-source" ||
			lower == "x-stainless-helper-method" || lower == "x-stainless-retry-count" ||
			lower == "x-stainless-timeout" || lower == "x-stainless-read-timeout" ||
			lower == "x-stainless-connect-timeout" || lower == "anthropic-dangerous-direct-browser-access" {
			delete(req.Header, name)
		}
		switch lower {
		case "session-id", "session_id", "thread-id", "conversation_id", "x-request-id", "x-session-id", "x-query-id", "x-session-affinity", "x-opencode-session", "chatgpt-account-id":
			delete(req.Header, name)
		}
	}
	protocol := outboundidentity.RequestProtocol(req)
	if protocol == "" {
		return
	}
	body := domesticRequestBody(req)
	deleteHeaderAllForms(req.Header, "Accept")
	if account.Platform == PlatformDeepseek && protocol == APIProtocolAnthropic && gjson.GetBytes(body, "stream").Bool() {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if account.Platform == PlatformKimi || account.Platform == PlatformMiniMax || account.Platform == PlatformStepFun {
		// Each gateway attempt invokes the official SDK operation with SDK
		// retries disabled; gateway failover is not an SDK retry counter.
		req.Header.Set("X-Stainless-Retry-Count", "0")
		if deadline, ok := req.Context().Deadline(); ok {
			seconds := time.Until(deadline).Seconds()
			if seconds > 0 {
				req.Header.Set("X-Stainless-Timeout", strconv.FormatFloat(seconds, 'f', 3, 64))
			}
		}
	}
	if protocol != APIProtocolAnthropic {
		return
	}
	deleteHeaderAllForms(req.Header, "Anthropic-Version")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	if account.Platform == PlatformMiniMax {
		req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
	}
	beta := domesticAnthropicBeta(account, body, getHeaderRaw(req.Header, "anthropic-beta"))
	deleteHeaderAllForms(req.Header, "anthropic-beta")
	if beta != "" {
		req.Header.Set("anthropic-beta", beta)
	}
}

func domesticRequestBody(req *http.Request) []byte {
	if req == nil || req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(body)
	if err != nil {
		return nil
	}
	return data
}

func domesticAnthropicBeta(account *Account, body []byte, declared string) string {
	if account.Platform == PlatformDeepseek {
		if !gjson.ValidBytes(body) {
			return ""
		}
		files, changes := false, false
		var hasFile func(gjson.Result) bool
		hasFile = func(block gjson.Result) bool {
			if block.Get("type").String() == "image" && block.Get("source.type").String() == "file" && block.Get("source.file_id").String() != "" {
				return true
			}
			if block.Get("type").String() == "tool_result" {
				for _, child := range block.Get("content").Array() {
					if hasFile(child) {
						return true
					}
				}
			}
			return false
		}
		for _, message := range gjson.GetBytes(body, "messages").Array() {
			for _, block := range message.Get("content").Array() {
				kind := block.Get("type").String()
				changes = changes || kind == "tool_addition" || kind == "tool_removal"
				files = files || hasFile(block)
			}
		}
		var tokens []string
		if files {
			tokens = append(tokens, "files-api-2025-04-14")
		}
		if changes {
			tokens = append(tokens, "mid-conversation-tool-changes-2026-07-01")
		}
		return strings.Join(tokens, ",")
	}
	if account.Platform == PlatformKimi || account.Platform == PlatformMiniMax {
		var tokens []string
		adaptive := gjson.GetBytes(body, "thinking.type").String() == "adaptive"
		if account.Platform == PlatformMiniMax && gjson.GetBytes(body, "output_config.effort").Exists() {
			adaptive = true
		}
		if !adaptive {
			tokens = append(tokens, "interleaved-thinking-2025-05-14")
		}
		if account.Platform == PlatformKimi && gjson.GetBytes(body, "context_management.edits").IsArray() {
			tokens = append(tokens, "context-management-2025-06-27")
		}
		return strings.Join(tokens, ",")
	}
	// Retain only supported protocol capabilities. Claude login/client betas
	// are never declarations of a native domestic provider.
	allowed := map[string]bool{"interleaved-thinking-2025-05-14": true, "fine-grained-tool-streaming-2025-05-14": true, "prompt-caching-2024-07-31": true}
	var result []string
	seen := map[string]bool{}
	for _, token := range strings.Split(declared, ",") {
		token = strings.TrimSpace(token)
		if allowed[token] && !seen[token] {
			result = append(result, token)
			seen[token] = true
		}
	}
	return strings.Join(result, ",")
}
