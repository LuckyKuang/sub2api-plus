package service

import (
	"encoding/json"
	"strings"
)

type stepFunModel struct {
	ID        string   `json:"id"`
	Type      *string  `json:"model_type"`
	Context   int64    `json:"max_input_tokens"`
	Reasoning *bool    `json:"enable_reason"`
	Vision    *bool    `json:"enable_vision_input"`
	Efforts   []string `json:"reasoning_effort_support_list"`
}

func stepFunChatModels(body []byte) ([]stepFunModel, error) {
	var response struct {
		Data []stepFunModel `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	typed := false
	for _, model := range response.Data {
		typed = typed || model.Type != nil
	}
	result := make([]stepFunModel, 0, len(response.Data))
	for _, model := range response.Data {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			continue
		}
		if typed && (model.Type == nil || (*model.Type != "大语言模型" && *model.Type != "路由模型")) {
			continue
		}
		result = append(result, model)
	}
	return result, nil
}

func extractStepFunModelIDs(body []byte) ([]string, error) {
	models, err := stepFunChatModels(body)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return dedupeAndSortModelIDs(ids), nil
}

func extractStepFunMetadata(body []byte) map[string]UpstreamModelMetadata {
	models, _ := stepFunChatModels(body)
	result := make(map[string]UpstreamModelMetadata, len(models))
	for _, model := range models {
		metadata := UpstreamModelMetadata{ID: model.ID, DisplayName: model.ID, ContextWindow: model.Context, MaxContextWindow: model.Context, Reasoning: model.Reasoning, SupportedReasoningLevels: normalizeReasoningLevels(model.Efforts)}
		if model.Vision != nil {
			metadata.InputModalities = []string{"text"}
			if *model.Vision {
				metadata.InputModalities = append(metadata.InputModalities, "image")
			}
		}
		if model.Reasoning == nil && len(model.Efforts) > 0 {
			reasoning := true
			metadata.Reasoning = &reasoning
		}
		result[model.ID] = metadata
	}
	return result
}
