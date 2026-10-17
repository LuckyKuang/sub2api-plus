package service

import (
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
)

func configuredMinOutputTokens(cfg *config.Config) int {
	if cfg != nil && cfg.Gateway.MinOutputTokens >= 16 {
		return cfg.Gateway.MinOutputTokens
	}
	return apicompat.DefaultMinMaxOutputTokens
}

func chatCompletionsToResponses(cfg *config.Config, req *apicompat.ChatCompletionsRequest) (*apicompat.ResponsesRequest, error) {
	return apicompat.ChatCompletionsToResponsesWithMinOutputTokens(req, configuredMinOutputTokens(cfg))
}

func anthropicToResponses(cfg *config.Config, req *apicompat.AnthropicRequest) (*apicompat.ResponsesRequest, error) {
	return apicompat.AnthropicToResponsesWithMinOutputTokens(req, configuredMinOutputTokens(cfg))
}

func settingServiceConfig(settingService *SettingService) *config.Config {
	if settingService == nil {
		return nil
	}
	return settingService.cfg
}
