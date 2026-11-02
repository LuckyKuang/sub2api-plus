package service

import infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"

func ValidateNativeAccessPreference(client, endpoint string) error {
	allowed := map[string][]string{
		"":              {"", "/v1/responses", "/v1/chat/completions", "/v1/messages", "/v1beta", "/v1/systemone"},
		"codex":         {"", "/v1/responses"},
		"claude":        {"", "/v1/messages"},
		"openai-sdk":    {"", "/v1/responses", "/v1/chat/completions"},
		"anthropic-sdk": {"", "/v1/messages"},
		"gemini":        {"", "/v1beta"},
		"opencode":      {"", "/v1/responses", "/v1/chat/completions", "/v1/messages", "/v1beta"},
		"custom":        {"", "/v1/responses", "/v1/chat/completions", "/v1/messages", "/v1beta", "/v1/systemone"},
	}
	for _, candidate := range allowed[client] {
		if candidate == endpoint {
			return nil
		}
	}
	return infraerrors.BadRequest("API_KEY_NATIVE_ACCESS_INVALID", "Choose a supported client and native endpoint combination")
}
