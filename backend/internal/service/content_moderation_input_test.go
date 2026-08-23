package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Agent 工具循环只审核当前结果，不回溯重复审核历史用户消息。

func TestExtractContentModerationInput_AnthropicAgentToolLoopAuditsCurrentResult(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"调用一下天气工具"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"weather","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"晴 25 度"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "晴 25 度", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_AnthropicFirstTurnExtractsUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "Q1", input.Text)
}

func TestExtractContentModerationInput_AnthropicMultiTurnExtractsLatestUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"},
			{"role":"assistant","content":"A1"},
			{"role":"user","content":"Q2"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "Q2", input.Text)
}

func TestExtractContentModerationInput_AnthropicStreamResendExtractsResend(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"原问题"},
			{"role":"assistant","content":"部分回答……"},
			{"role":"user","content":"重发"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "重发", input.Text)
}

func TestExtractContentModerationInput_OpenAIChatAgentToolLoopAuditsCurrentResult(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"system","content":"sys"},
			{"role":"user","content":"列出我的订单"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"orders","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[]"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "[] sys", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatAuditsParallelStructuredToolResults(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"system","content":"system context"},
			{"role":"user","content":"compare records"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"first","arguments":"{}"}},{"id":"call_2","type":"function","function":{"name":"second","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":{"result":"first"}},
			{"role":"tool","tool_call_id":"call_2","content":{"result":"second"}}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "{\"result\":\"first\"} {\"result\":\"second\"} system context", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatMultiTurnExtractsLatestUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"},
			{"role":"assistant","content":"A1"},
			{"role":"user","content":"Q2"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "Q2", input.Text)
}

func TestExtractContentModerationInput_GeminiAgentToolLoopAuditsCurrentResult(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"查询天气"}]},
			{"role":"model","parts":[{"functionCall":{"name":"weather","args":{}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"weather","response":{"temp":25}}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.JSONEq(t, `{"temp":25}`, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_GeminiFirstTurnExtractsUser(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"你好"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "你好", input.Text)
}

func TestExtractContentModerationInput_GeminiMultiTurnExtractsLatestUser(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"Q1"}]},
			{"role":"model","parts":[{"text":"A1"}]},
			{"role":"user","parts":[{"text":"Q2"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "Q2", input.Text)
}

func TestExtractContentModerationInput_ResponsesAgentToolLoopAuditsCurrentResult(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"运行测试"}]},
			{"type":"function_call","call_id":"call_1","name":"run_tests","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"all passed"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "all passed", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesLastUserMessageExtracted(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"latest"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "latest", input.Text)
}

func TestExtractContentModerationInput_ResponsesLastClientAssistantContentAudited(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"q1"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a1"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "a1", input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesTopLevelInputCannotShadowNestedContent(t *testing.T) {
	body := []byte(`{
		"type":"response.create",
		"input":[],
		"response":{"input":[{"type":"message","role":"user","content":[
			{"type":"input_text","text":"nested content"},
			{"type":"input_image","image_url":"https://example.test/nested.png"}
		]}]}
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "nested content", input.Text)
	require.Equal(t, []string{"https://example.test/nested.png"}, input.Images)
}

func TestExtractContentModerationInput_AuditsCurrentContentBeforeContext(t *testing.T) {
	body := []byte(`{
		"instructions":"<system-reminder>context must still be audited</system-reminder>",
		"tools":[{"type":"function","name":"lookup","description":"tool definition policy"}],
		"input":[{"type":"message","role":"assistant","content":"current assistant payload"}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.True(t, strings.HasPrefix(input.Text, "current assistant payload"))
	require.Contains(t, input.Text, "system-reminder")
	require.Contains(t, input.Text, "tool definition policy")
}

func TestExtractContentModerationInput_ChatCollectsImagesFromParallelToolResults(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":"inspect"},
		{"role":"tool","tool_call_id":"c1","content":{"image_url":"https://example.test/one.png"}},
		{"role":"function","content":[{"type":"input_image","image_url":"https://example.test/two.png"}]}
	]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, []string{"https://example.test/one.png", "https://example.test/two.png"}, input.Images)
}

func TestExtractContentModerationInput_ResponsesCollectsImagesFromTrailingOfficialOutputs(t *testing.T) {
	body := []byte(`{"input":[
		{"type":"message","role":"user","content":"inspect"},
		{"type":"function_call_output","call_id":"c1","output":{"image_url":"https://example.test/tool.png"}},
		{"type":"computer_call_output","call_id":"c2","output":{"type":"computer_screenshot","image_url":"https://example.test/screenshot.png"}}
	]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, []string{"https://example.test/tool.png", "https://example.test/screenshot.png"}, input.Images)
	require.NotContains(t, input.Text, "example.test")
}

func TestExtractContentModerationInput_AnthropicCollectsNestedToolResultImage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"url","url":"https://example.test/result.png"}}]}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, []string{"https://example.test/result.png"}, input.Images)
}

func TestExtractContentModerationInput_ResponsesCollectsReusablePromptVariableImage(t *testing.T) {
	body := []byte(`{"prompt":{"id":"pmpt_1","variables":{"photo":{"type":"input_image","image_url":"https://example.test/prompt.png"}}}}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, []string{"https://example.test/prompt.png"}, input.Images)
}

func TestExtractContentModerationInput_DoesNotTreatOrdinaryToolURLAsImage(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"c1","output":{"documentation_url":"https://example.test/docs","summary":"read docs"}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Images)
	require.Contains(t, input.Text, "documentation_url")
}

func TestExtractContentModerationInput_LiveConversationItemPreservesImage(t *testing.T) {
	body := []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/live.png"}]}}`)

	input := ExtractContentModerationInput("openai_live", body)

	require.Equal(t, []string{"https://example.test/live.png"}, input.Images)
}
