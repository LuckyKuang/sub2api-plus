package service

import "strings"

// NativeImageAccess describes image generation through existing gateway routes.
// requestModel is the model seen by endpoint validation (after an explicit
// Composite rewrite); mappedModel includes channel and account mappings. This
// is management metadata only and never changes gateway admission or auditing.
func NativeImageAccess(account *Account, requestModel, mappedModel string) (endpoint string, image bool) {
	if account == nil {
		return "", false
	}
	switch account.Platform {
	case PlatformOpenAI:
		if validateCompatibleImagesModel(mappedModel) != nil {
			return "", false
		}
		capability := OpenAIImagesCapabilityBasic
		if isGeminiCompatibleImageModel(mappedModel) || isNanoBananaImageModel(mappedModel) {
			capability = OpenAIImagesCapabilityAPIKey
		}
		if validateCompatibleImagesModel(requestModel) != nil || !account.SupportsOpenAIImageCapability(capability) {
			return "", true
		}
		return openAIImagesGenerationsEndpoint, true
	case PlatformGrok:
		if isGrokImageGenerationModel(mappedModel) {
			if strings.EqualFold(strings.TrimSpace(mappedModel), "grok-imagine-edit") {
				return "", true
			}
			return openAIImagesGenerationsEndpoint, true
		}
	case PlatformGemini, PlatformAntigravity:
		if isImageGenerationModel(mappedModel) {
			return "/v1beta", true
		}
	}
	return "", false
}
