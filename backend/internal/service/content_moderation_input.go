package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/auditcontent"
	"github.com/tidwall/gjson"
)

func ExtractContentModerationText(protocol string, body []byte) string {
	return ExtractContentModerationInput(protocol, body).Text
}

func ExtractContentModerationInput(protocol string, body []byte) ContentModerationInput {
	input, _, _ := extractContentModerationInput(protocol, body)
	return input
}

func extractContentModerationInput(protocol string, body []byte) (ContentModerationInput, bool, error) {
	document, err := auditcontent.Extract(protocol, body)
	if err != nil {
		return ContentModerationInput{}, false, err
	}
	var parts []string
	for _, includeContext := range []bool{false, true} {
		for _, segment := range document.Segments {
			if !segment.Current || isModerationContextSegment(segment) != includeContext {
				continue
			}
			if text := strings.TrimSpace(segment.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}

	var images []string
	switch protocol {
	case ContentModerationProtocolAnthropicMessages:
		collectCurrentAnthropicImages(gjson.GetBytes(body, "messages"), &images)
	case ContentModerationProtocolOpenAIChat:
		collectCurrentChatImages(gjson.GetBytes(body, "messages"), &images)
	case ContentModerationProtocolOpenAIResponses:
		collectCurrentResponsesImages(gjson.GetBytes(body, "input"), &images)
		collectCurrentResponsesImages(gjson.GetBytes(body, "response.input"), &images)
		collectModerationImages(gjson.GetBytes(body, "item"), false, &images)
		collectResponsesPromptImages(gjson.GetBytes(body, "prompt.variables"), &images)
		collectResponsesPromptImages(gjson.GetBytes(body, "response.prompt.variables"), &images)
		collectResponsesPromptImages(gjson.GetBytes(body, "session.prompt.variables"), &images)
	case ContentModerationProtocolGemini:
		collectCurrentGeminiImages(gjson.GetBytes(body, "contents"), &images)
	case ContentModerationProtocolOpenAIImages:
		collectModerationImages(gjson.GetBytes(body, "images"), true, &images)
	default:
		collectCurrentResponsesImages(gjson.GetBytes(body, "input"), &images)
		collectCurrentResponsesImages(gjson.GetBytes(body, "response.input"), &images)
		collectModerationImages(gjson.GetBytes(body, "item"), false, &images)
		collectResponsesPromptImages(gjson.GetBytes(body, "prompt.variables"), &images)
		collectResponsesPromptImages(gjson.GetBytes(body, "response.prompt.variables"), &images)
		collectResponsesPromptImages(gjson.GetBytes(body, "session.prompt.variables"), &images)
		collectCurrentChatImages(gjson.GetBytes(body, "messages"), &images)
		collectCurrentGeminiImages(gjson.GetBytes(body, "contents"), &images)
	}
	out := ContentModerationInput{
		Text:   normalizeContentModerationText(strings.Join(parts, "\n")),
		Images: normalizeModerationImages(images),
	}
	out.Normalize()
	if document.Incomplete {
		return out, true, auditcontent.ErrIncompleteContent
	}
	return out, document.ContentBearing || len(images) > 0, nil
}

func isModerationContextSegment(segment auditcontent.Segment) bool {
	return segment.Source == auditcontent.SourceInstruction || segment.Source == auditcontent.SourceToolDefinition
}

func collectCurrentChatImages(messages gjson.Result, images *[]string) {
	if !messages.IsArray() {
		return
	}
	array := messages.Array()
	if len(array) == 0 {
		return
	}
	currentStart := len(array) - 1
	if isChatModerationToolOutput(array[currentStart]) {
		for currentStart > 0 && isChatModerationToolOutput(array[currentStart-1]) {
			currentStart--
		}
	}
	for _, message := range array[currentStart:] {
		collectModerationImages(message.Get("content"), false, images)
	}
}

func isChatModerationToolOutput(message gjson.Result) bool {
	role := strings.ToLower(strings.TrimSpace(message.Get("role").String()))
	return role == "tool" || role == "function"
}

func collectCurrentAnthropicImages(messages gjson.Result, images *[]string) {
	if !messages.IsArray() {
		return
	}
	array := messages.Array()
	if len(array) == 0 {
		return
	}
	collectModerationImages(array[len(array)-1].Get("content"), false, images)
}

func collectCurrentResponsesImages(input gjson.Result, images *[]string) {
	switch {
	case !input.Exists():
		return
	case input.IsArray():
		array := input.Array()
		if len(array) == 0 {
			return
		}
		currentStart := len(array) - 1
		if isResponsesModerationToolOutput(array[currentStart]) {
			for currentStart > 0 && isResponsesModerationToolOutput(array[currentStart-1]) {
				currentStart--
			}
		}
		for _, item := range array[currentStart:] {
			collectModerationImages(item, false, images)
		}
	case input.IsObject():
		collectModerationImages(input, false, images)
	}
}

func isResponsesModerationToolOutput(item gjson.Result) bool {
	switch strings.ToLower(strings.TrimSpace(item.Get("type").String())) {
	case "function_call_output", "custom_tool_call_output", "tool_search_output", "mcp_tool_call_output",
		"local_shell_call_output", "shell_call_output", "apply_patch_call_output", "computer_call_output",
		"program_output", "mcp_approval_response", "mcp_call":
		return true
	default:
		return false
	}
}

func collectCurrentGeminiImages(contents gjson.Result, images *[]string) {
	if !contents.IsArray() {
		return
	}
	array := contents.Array()
	if len(array) == 0 {
		return
	}
	last := array[len(array)-1]
	if arr := last.Get("parts"); arr.IsArray() {
		arr.ForEach(func(_, part gjson.Result) bool {
			addGeminiModerationImage(images, part)
			collectModerationImages(part, false, images)
			return true
		})
	}
}

func collectResponsesPromptImages(variables gjson.Result, images *[]string) {
	if !variables.IsObject() {
		return
	}
	variables.ForEach(func(_, value gjson.Result) bool {
		collectModerationImages(value, false, images)
		return true
	})
}

func collectModerationImages(value gjson.Result, mediaContext bool, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		candidate := strings.TrimSpace(value.String())
		if mediaContext || strings.HasPrefix(strings.ToLower(candidate), "data:image/") {
			addModerationImage(images, candidate)
		}
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectModerationImages(item, mediaContext, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		addModerationImageData(images, value.Get("source.media_type").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("source.mediaType").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("media_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mime_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mimeType").String(), value.Get("data").String())
		objectMedia := mediaContext || isModerationImageType(typ)
		value.ForEach(func(key, child gjson.Result) bool {
			childMedia := objectMedia || isModerationImageField(key.String())
			collectModerationImages(child, childMedia, images)
			return true
		})
	}
}

func isModerationImageType(typeName string) bool {
	switch typeName {
	case "image", "image_url", "input_image", "output_image", "computer_screenshot":
		return true
	default:
		return false
	}
}

func isModerationImageField(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "image", "images", "image_url", "imageurl", "screenshot", "partial_image":
		return true
	default:
		return false
	}
}

func addGeminiModerationImage(images *[]string, part gjson.Result) {
	if inlineData := part.Get("inline_data"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mime_type").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	if inlineData := part.Get("inlineData"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mimeType").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	addModerationImage(images, part.Get("file_data.file_uri").String())
	addModerationImage(images, part.Get("fileData.fileUri").String())
}

func addModerationImageData(images *[]string, mimeType string, data string) {
	mimeType = strings.TrimSpace(mimeType)
	data = strings.TrimSpace(data)
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") || data == "" {
		return
	}
	addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
}

func addModerationImage(images *[]string, image string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	if strings.HasPrefix(image, "data:") || strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		*images = append(*images, image)
	}
}

func normalizeModerationImages(images []string) []string {
	out := make([]string, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		out = append(out, image)
	}
	return out
}

func limitContentModerationImages(images []string) []string {
	if len(images) <= maxContentModerationInputImages {
		return images
	}
	idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(images))))
	if err != nil {
		return images[:maxContentModerationInputImages]
	}
	return []string{images[int(idx.Int64())]}
}

func normalizeContentModerationText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}
