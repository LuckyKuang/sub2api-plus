package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 当数组末尾不是用户消息时（典型场景：Agent 工具循环结束于 tool/assistant），
// 应直接跳过内容审核——不再把 system/tools/tool 回包送进 Moderations。

func TestExtractContentModerationInput_AnthropicAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"调用一下天气工具"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"weather","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"晴 25 度"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Empty(t, input.Text)
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

func TestExtractContentModerationInput_OpenAIChatAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"system","content":"sys"},
			{"role":"user","content":"列出我的订单"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"orders","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[]"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatSkipsParallelStructuredToolResults(t *testing.T) {
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

	require.Empty(t, input.Text)
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

func TestExtractContentModerationInput_GeminiAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"查询天气"}]},
			{"role":"model","parts":[{"functionCall":{"name":"weather","args":{}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"weather","response":{"temp":25}}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Empty(t, input.Text)
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

func TestExtractContentModerationInput_ResponsesAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"运行测试"}]},
			{"type":"function_call","call_id":"call_1","name":"run_tests","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"all passed"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
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

func TestExtractContentModerationInput_ResponsesLastAssistantSkipped(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"q1"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a1"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
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

func TestExtractContentModerationInput_IgnoresCodexAndClaudeMetadata(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-luna",
		"originator":"codex_cli_rs",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi","cache_control":{"type":"ephemeral"}}]}]
	}`)
	input, contentBearing, _, err := extractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)
	require.NoError(t, err)
	require.True(t, contentBearing)
	require.Equal(t, "hi", input.Text)

	chat, chatBearing, _, chatErr := extractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(`{
		"messages":[{"role":"user","content":"hi","cache_control":{"type":"ephemeral"}}]
	}`))
	require.NoError(t, chatErr)
	require.True(t, chatBearing)
	require.Equal(t, "hi", chat.Text)
}

func TestExtractContentModerationInput_DoesNotAuditInstructionsOrToolDefinitions(t *testing.T) {
	body := []byte(`{
		"instructions":"<system-reminder>context must still be audited</system-reminder>",
		"tools":[{"type":"function","name":"lookup","description":"tool definition policy"}],
		"input":[{"type":"message","role":"user","content":"你好"}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "你好", input.Text)
	require.NotContains(t, input.Text, "system-reminder")
	require.NotContains(t, input.Text, "tool definition policy")
}

func TestExtractContentModerationInput_ChatDoesNotCollectImagesFromToolResults(t *testing.T) {
	body := []byte(`{"messages":[
		{"role":"user","content":"inspect"},
		{"role":"tool","tool_call_id":"c1","content":{"image_url":"https://example.test/one.png"}},
		{"role":"function","content":[{"type":"input_image","image_url":"https://example.test/two.png"}]}
	]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesDoesNotCollectImagesFromTrailingOfficialOutputs(t *testing.T) {
	body := []byte(`{"input":[
		{"type":"message","role":"user","content":"inspect"},
		{"type":"function_call_output","call_id":"c1","output":{"image_url":"https://example.test/tool.png"}},
		{"type":"computer_call_output","call_id":"c2","output":{"type":"computer_screenshot","image_url":"https://example.test/screenshot.png"}}
	]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_AnthropicDoesNotCollectNestedToolResultImage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"image","source":{"type":"url","url":"https://example.test/result.png"}}]}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesDoesNotAuditReusablePromptVariables(t *testing.T) {
	body := []byte(`{"prompt":{"id":"pmpt_1","variables":{"plain":"prompt variable","photo":{"type":"input_image","image_url":"https://example.test/prompt.png"}}}}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_DoesNotTreatOrdinaryToolURLAsImage(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call_output","call_id":"c1","output":{"documentation_url":"https://example.test/docs","summary":"read docs"}}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Images)
	require.Empty(t, input.Text)
}

func TestExtractContentModerationInput_LiveConversationItemPreservesImage(t *testing.T) {
	body := []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/live.png"}]}}`)

	input := ExtractContentModerationInput("openai_live", body)

	require.Equal(t, []string{"https://example.test/live.png"}, input.Images)
}

func TestExtractContentModerationInput_LiveSessionInputPreservesImage(t *testing.T) {
	body := []byte(`{"type":"session.update","session":{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/live-session.png"}]}]}}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAILive, body)

	require.Equal(t, []string{"https://example.test/live-session.png"}, input.Images)
}

func TestExtractContentModerationInput_AuditsDirectEmbeddingAndSearchInput(t *testing.T) {
	embeddings := ExtractContentModerationInput("openai_embeddings", []byte(`{"input":["embedding one","embedding two"]}`))
	require.Equal(t, "embedding one embedding two", embeddings.Text)

	search := ExtractContentModerationInput("openai_alpha_search", []byte(`{"commands":{"search_query":[{"q":"security query"}]},"input":[{"type":"message","role":"user","content":"recent search context"}]}`))
	require.Equal(t, "security query recent search context", search.Text)
}

func TestExtractContentModerationInput_AnthropicThinkingIsNotAudited(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"thinking","thinking":"private reasoning about violence"},{"type":"text","text":"你好"}]}]}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "你好", input.Text)
	require.NotContains(t, input.Text, "private reasoning")
}

func TestExtractContentModerationInput_ModelOutputFormsAreNotDirectUserInput(t *testing.T) {
	responses := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, []byte(`{"input":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"private reasoning"}]},
		{"type":"message","content":[{"type":"output_text","text":"model output"},{"type":"refusal","refusal":"model refusal"}]},
		{"type":"output_text","text":"direct output item"}
	]}`))
	require.Empty(t, responses.Text)
	require.Empty(t, responses.Images)

	gemini := ExtractContentModerationInput(ContentModerationProtocolGemini, []byte(`{"contents":[{"parts":[{"text":"private thought","thought":true},{"text":"你好"}]}]}`))
	require.Equal(t, "你好", gemini.Text)
	require.NotContains(t, gemini.Text, "private thought")

	anthropic := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, []byte(`{"messages":[{"role":"user","content":[{"type":"output_text","text":"model output"},{"type":"text","text":"你好"}]}]}`))
	require.Equal(t, "你好", anthropic.Text)
	require.NotContains(t, anthropic.Text, "model output")
}

func TestExtractContentModerationInput_ResponsesStringInputIsAudited(t *testing.T) {
	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, []byte(`{"input":"你好"}`))
	require.Equal(t, "你好", input.Text)
}

func TestExtractContentModerationInput_UnknownSiblingIsIgnored(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":"你好","foo":"bar"}]}`)

	input, contentBearing, reasons, err := extractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.NoError(t, err)
	require.True(t, contentBearing)
	require.Equal(t, "你好", input.Text)
	require.Empty(t, reasons)
}

func TestExtractContentModerationInput_RequiresExplicitUserRoleForChatAndAnthropic(t *testing.T) {
	chat := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(`{"messages":[{"content":"roleless chat"}]}`))
	require.Empty(t, chat.Text)

	anthropic := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, []byte(`{"messages":[{"content":"roleless anthropic"}]}`))
	require.Empty(t, anthropic.Text)

	responses := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, []byte(`{"input":[{"type":"message","content":"roleless responses"}]}`))
	require.Equal(t, "roleless responses", responses.Text)

	gemini := ExtractContentModerationInput(ContentModerationProtocolGemini, []byte(`{"contents":[{"parts":[{"text":"roleless gemini"}]}]}`))
	require.Equal(t, "roleless gemini", gemini.Text)
}

func TestExtractContentModerationInput_LiveSessionInstructionsAreNotAudited(t *testing.T) {
	body := []byte(`{"model":"gpt-live-test","instructions":"live safety policy about violence"}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAILive, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}
