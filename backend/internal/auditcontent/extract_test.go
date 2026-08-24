package auditcontent

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractCanonicalProtocolPayloads(t *testing.T) {
	tests := []struct {
		name         string
		protocol     string
		body         string
		currentTexts []string
		sources      []Source
	}{
		{
			name: "responses function output", protocol: "openai_responses",
			body:         `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"run tests"}]},{"type":"function_call","call_id":"c1","name":"exec","arguments":"{\"cmd\":\"go test\"}"},{"type":"function_call_output","call_id":"c1","output":"all passed"}]}`,
			currentTexts: []string{"all passed"}, sources: []Source{SourceToolOutput},
		},
		{
			name: "responses trailing custom outputs", protocol: "openai_responses",
			body:         `{"input":[{"type":"custom_tool_call_output","call_id":"c1","output":{"b":2,"a":1}},{"type":"tool_search_output","call_id":"c2","output":"search result"}]}`,
			currentTexts: []string{`{"a":1,"b":2}`, "search result"}, sources: []Source{SourceToolOutput, SourceToolOutput},
		},
		{
			name: "responses reasoning and mcp continuation", protocol: "openai_responses",
			body:         `{"input":[{"type":"reasoning","encrypted_content":"gAAAAAB","summary":[]},{"type":"mcp_tool_call","call_id":"m1","name":"lookup","arguments":{"q":"docs"}},{"type":"mcp_tool_call_output","call_id":"m1","output":{"result":"found"}}]}`,
			currentTexts: []string{`{"result":"found"}`}, sources: []Source{SourceToolOutput},
		},
		{
			name: "nested responses websocket", protocol: "openai_responses",
			body:         `{"type":"response.create","model":"gpt-test","response":{"instructions":"inspect safely","input":"nested turn"}}`,
			currentTexts: []string{"inspect safely", "nested turn"}, sources: []Source{SourceInstruction, SourceMessage},
		},
		{
			name: "empty top-level input cannot shadow nested responses content", protocol: "openai_responses",
			body:         `{"type":"response.create","input":[],"response":{"instructions":"nested policy","input":"nested content"}}`,
			currentTexts: []string{"nested policy", "nested content"}, sources: []Source{SourceInstruction, SourceMessage},
		},
		{
			name: "alpha search", protocol: "openai_alpha_search",
			body:         `{"commands":{"search_query":[{"q":"OpenAI news"},{"q":"security updates"}]},"input":[{"type":"message","role":"user","content":"recent conversation"}]}`,
			currentTexts: []string{"OpenAI news", "security updates", "recent conversation"}, sources: []Source{SourceSearchQuery, SourceSearchQuery, SourceMessage},
		},
		{
			name: "live initial legacy transcription", protocol: "openai_live",
			body:         `{"model":"gpt-live-test","instructions":"live safety instruction","input_audio_transcription":{"model":"gpt-4o-transcribe","prompt":"legacy transcription context"}}`,
			currentTexts: []string{"live safety instruction", "legacy transcription context"}, sources: []Source{SourceInstruction, SourceInstruction},
		},
		{
			name: "live current transcription", protocol: "openai_live",
			body:         `{"type":"transcription","model":"gpt-live-transcribe","audio":{"input":{"format":{"type":"audio/pcm","rate":24000},"transcription":{"model":"gpt-live-transcribe","prompt":"current transcription context","keywords":["premium plan","AC-42"],"languages":["en","fr"],"delay":"low"},"turn_detection":null}}}`,
			currentTexts: []string{"current transcription context", "premium plan", "AC-42"}, sources: []Source{SourceInstruction, SourceInstruction, SourceInstruction},
		},
		{
			name: "live session update", protocol: "openai_live",
			body:         `{"type":"session.update","session":{"instructions":"updated live instruction","tools":[{"name":"lookup","description":"live tool policy"}]}}`,
			currentTexts: []string{"updated live instruction", `[{"description":"live tool policy","name":"lookup"}]`}, sources: []Source{SourceInstruction, SourceToolDefinition},
		},
		{
			name: "live conversation tool output", protocol: "openai_live",
			body:         `{"type":"conversation.item.create","item":{"type":"function_call_output","call_id":"call_live","output":"live tool result"}}`,
			currentTexts: []string{"live tool result"}, sources: []Source{SourceToolOutput},
		},
		{
			name: "live response create", protocol: "openai_live",
			body:         `{"type":"response.create","response":{"instructions":"live response instruction","output_modalities":["audio"],"audio":{"output":{"format":{"type":"audio/pcm","rate":24000},"voice":"marin"}}}}`,
			currentTexts: []string{"live response instruction"}, sources: []Source{SourceInstruction},
		},
		{
			name: "embeddings array", protocol: "openai_embeddings",
			body:         `{"input":["first embedding","second embedding"]}`,
			currentTexts: []string{"first embedding", "second embedding"}, sources: []Source{SourceEmbeddingInput, SourceEmbeddingInput},
		},
		{
			name: "chat tool result", protocol: "openai_chat_completions",
			body:         `{"messages":[{"role":"user","content":"lookup"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{}"}}]},{"role":"tool","content":"external result"}]}`,
			currentTexts: []string{"external result"}, sources: []Source{SourceToolOutput},
		},
		{
			name: "chat parallel structured tool results", protocol: "openai_chat_completions",
			body:         `{"messages":[{"role":"user","content":"lookup"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{}"}}]},{"role":"tool","content":{"first":true}},{"role":"function","content":{"second":false}}]}`,
			currentTexts: []string{`{"first":true}`, `{"second":false}`}, sources: []Source{SourceToolOutput, SourceToolOutput},
		},
		{
			name: "anthropic tool result", protocol: "anthropic_messages",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"tool_use","input":{"q":"weather"}}]},{"role":"user","content":[{"type":"tool_result","content":{"temp":25,"unit":"C"}}]}]}`,
			currentTexts: []string{`{"temp":25,"unit":"C"}`}, sources: []Source{SourceToolOutput},
		},
		{
			name: "anthropic thinking and server tool result", protocol: "anthropic_messages",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"private reasoning"},{"type":"redacted_thinking","data":"opaque"}]},{"role":"user","content":[{"type":"web_search_tool_result","content":{"title":"result"}}]}]}`,
			currentTexts: []string{`{"title":"result"}`}, sources: []Source{SourceToolOutput},
		},
		{
			name: "gemini function response", protocol: "gemini",
			body:         `{"contents":[{"role":"model","parts":[{"functionCall":{"args":{"city":"Taipei"}}}]},{"role":"user","parts":[{"functionResponse":{"response":{"temp":25}}}]}]}`,
			currentTexts: []string{`{"temp":25}`}, sources: []Source{SourceToolOutput},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := Extract(test.protocol, []byte(test.body))
			require.NoError(t, err)
			require.True(t, document.ContentBearing)
			require.False(t, document.Incomplete)
			current := currentSegments(document)
			require.Equal(t, test.currentTexts, segmentTexts(current))
			require.Equal(t, test.sources, segmentSources(current))
		})
	}
}

func TestExtractClassifiesControlAndUnextractableContent(t *testing.T) {
	control, err := Extract("openai_responses", []byte(`{"type":"conversation.item.create","item":{"type":"message"}}`))
	require.NoError(t, err)
	require.False(t, control.ContentBearing)
	require.Empty(t, control.Segments)

	liveControl, err := Extract("openai_live", []byte(`{"type":"input_audio_buffer.commit"}`))
	require.NoError(t, err)
	require.False(t, liveControl.ContentBearing)
	require.Empty(t, liveControl.Segments)

	liveUnknown, err := Extract("openai_live", []byte(`{"type":"future.live.control","payload":"classify me"}`))
	require.NoError(t, err)
	require.True(t, liveUnknown.ContentBearing)
	require.True(t, liveUnknown.Incomplete)
	require.Empty(t, liveUnknown.Segments)

	unknown, err := Extract("openai_responses", []byte(`{"input":[{"type":"future_content","payload":"must add adapter"}]}`))
	require.NoError(t, err)
	require.True(t, unknown.ContentBearing)
	require.True(t, unknown.Incomplete)
	require.Empty(t, unknown.Segments)

	tokenIDs, err := Extract("openai_embeddings", []byte(`{"input":[15339,1917]}`))
	require.NoError(t, err)
	require.True(t, tokenIDs.ContentBearing)
	require.True(t, tokenIDs.Incomplete)
	require.Empty(t, tokenIDs.Segments)

	malformedSearch, err := Extract("openai_alpha_search", []byte(`{"commands":{"search_query":[{"query":"missing q adapter"}]}}`))
	require.NoError(t, err)
	require.True(t, malformedSearch.ContentBearing)
	require.True(t, malformedSearch.Incomplete)
	require.Empty(t, malformedSearch.Segments)

	httpTypeSpoof, err := Extract("openai_responses", []byte(`{"type":"future.control","input":"must still be audited"}`))
	require.NoError(t, err)
	require.Equal(t, []string{"must still be audited"}, segmentTexts(httpTypeSpoof.Segments))
	require.False(t, httpTypeSpoof.Incomplete)

	partial, err := Extract("openai_responses", []byte(`{"input":[{"type":"message","role":"user","content":"safe extracted text"},{"type":"future_content","payload":"must not be omitted"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"safe extracted text"}, segmentTexts(partial.Segments))
	require.True(t, partial.ContentBearing)
	require.True(t, partial.Incomplete)
}

func TestExtractResponsesReusablePromptVariables(t *testing.T) {
	document, err := Extract("openai_responses", []byte(`{
		"prompt":{"id":"pmpt_123","version":"4","variables":{
			"plain":"variable text",
			"typed":{"type":"input_text","text":"typed variable"},
			"image":{"type":"input_image","image_url":"https://example.test/image.png"}
		}}
	}`))
	require.NoError(t, err)
	require.True(t, document.ContentBearing)
	require.False(t, document.Incomplete)
	require.Equal(t, []string{"variable text", "typed variable"}, segmentTexts(document.Segments))
	require.Equal(t, []Source{SourcePromptVariable, SourcePromptVariable}, segmentSources(document.Segments))
	require.Equal(t, []string{"https://example.test/image.png"}, imageURLs(document.Images))
	require.Equal(t, SourcePromptVariable, document.Images[0].Source)
}

func TestExtractResponsesOfficialExtendedInputItems(t *testing.T) {
	document, err := Extract("openai_responses", []byte(`{"input":[
		{"type":"local_shell_call","call_id":"lc1","status":"completed","action":{"command":"pwd"}},
		{"type":"local_shell_call_output","call_id":"lc1","output":"local result","status":"completed"},
		{"type":"shell_call","call_id":"sc1","status":"completed","action":{"commands":["echo audited"]}},
		{"type":"shell_call_output","call_id":"sc1","status":"completed","output":[{"stdout":"shell result","stderr":"","outcome":{"type":"exit","exit_code":0}}]},
		{"type":"apply_patch_call","call_id":"ap1","status":"completed","operation":{"type":"update_file","diff":"patch text"}},
		{"type":"apply_patch_call_output","call_id":"ap1","status":"failed","output":"file not found"},
		{"type":"computer_call","call_id":"cc1","status":"completed","action":{"type":"click","x":10,"y":20},"pending_safety_checks":[{"id":"safe1","message":"confirm click"}]},
		{"type":"computer_call_output","call_id":"cc1","status":"completed","output":{"type":"computer_screenshot","image_url":"data:image/png;base64,AAAA"},"acknowledged_safety_checks":[{"id":"safe1","message":"click approved"}]},
		{"type":"mcp_call","id":"m1","status":"completed","server_label":"docs","name":"search","arguments":"{\"q\":\"docs\"}","output":"mcp result","error":null},
		{"type":"mcp_list_tools","id":"ml1","status":"completed","server_label":"docs","tools":[{"name":"lookup","description":"lookup docs"}],"error":null},
		{"type":"mcp_approval_request","id":"ma1","server_label":"docs","name":"write","arguments":"{\"path\":\"audit.txt\"}"},
		{"type":"mcp_approval_response","id":"mar1","approval_request_id":"ma1","approve":false,"reason":"approval denied"},
		{"type":"code_interpreter_call","id":"ci1","status":"completed","container_id":"ctr1","code":"print('audit')","outputs":[{"type":"logs","logs":"code result"}]},
		{"type":"program","id":"p1","status":"completed","call_id":"pc1","code":"const result = await tools.lookup()","fingerprint":"opaque"},
		{"type":"program_output","id":"po1","status":"completed","call_id":"pc1","result":"{\"answer\":42}"},
		{"type":"additional_tools","id":"at1","role":"developer","tools":[{"type":"function","name":"dynamic","description":"dynamic tool"}]},
		{"type":"compaction","id":"co1","encrypted_content":"opaque"}
	]}`))
	require.NoError(t, err)
	require.True(t, document.ContentBearing)
	require.False(t, document.Incomplete)
	joined := strings.Join(segmentTexts(document.Segments), "\n")
	for _, expected := range []string{
		"pwd", "local result", "echo audited", "shell result", "patch text", "file not found", "confirm click", "click approved",
		`{"q":"docs"}`, "mcp result", "lookup docs", `audit.txt`, "approval denied",
		"print('audit')", "code result", "const result = await tools.lookup()", `answer`, "dynamic tool",
	} {
		require.Contains(t, joined, expected)
	}
	require.NotContains(t, joined, "data:image")
	require.NotContains(t, joined, "opaque")
}

func TestExtractResponsesOfficialToolSearchOutputAuditsDynamicTools(t *testing.T) {
	document, err := Extract("openai_responses", []byte(`{"input":[{"type":"tool_search_output","execution":"client","call_id":"call_1","status":"completed","tools":[{"type":"function","name":"lookup","description":"dynamic search definition"}]}]}`))
	require.NoError(t, err)
	require.False(t, document.Incomplete)
	require.Contains(t, strings.Join(segmentTexts(document.Segments), "\n"), "dynamic search definition")
	require.Equal(t, []Source{SourceToolDefinition}, segmentSources(document.Segments))
}

func TestExtractKnownResponsesAndLiveTypesRejectUnknownContentFields(t *testing.T) {
	responses, err := Extract("openai_responses", []byte(`{"input":[{"type":"local_shell_call","call_id":"c1","action":{"command":"pwd"},"future_payload":"must not be hidden"}]}`))
	require.NoError(t, err)
	require.Contains(t, strings.Join(segmentTexts(responses.Segments), "\n"), "pwd")
	require.True(t, responses.Incomplete)

	live, err := Extract("openai_live", []byte(`{"type":"response.cancel","payload":"must not be hidden"}`))
	require.NoError(t, err)
	require.True(t, live.ContentBearing)
	require.True(t, live.Incomplete)

	responsesControl, err := Extract("openai_responses", []byte(`{"type":"response.cancel","payload":"must not be hidden"}`))
	require.NoError(t, err)
	require.True(t, responsesControl.ContentBearing)
	require.True(t, responsesControl.Incomplete)

	responsesUnknown, err := Extract("openai_responses", []byte(`{"type":"future.client.event","payload":"must be classified"}`))
	require.NoError(t, err)
	require.True(t, responsesUnknown.ContentBearing)
	require.True(t, responsesUnknown.Incomplete)

	conversation, err := Extract("openai_responses", []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":"sideband content"}}`))
	require.NoError(t, err)
	require.False(t, conversation.Incomplete)
	require.Equal(t, []string{"sideband content"}, segmentTexts(conversation.Segments))

	session, err := Extract("openai_responses", []byte(`{"type":"session.update","session":{"instructions":"session content"}}`))
	require.NoError(t, err)
	require.False(t, session.Incomplete)
	require.Equal(t, []string{"session content"}, segmentTexts(session.Segments))

	for _, protocol := range []string{"openai_responses", "openai_live"} {
		unknownSession, extractErr := Extract(protocol, []byte(`{"type":"session.update","session":{"instructions":"visible session content","future_payload":"must not be hidden"}}`))
		require.NoError(t, extractErr)
		require.Equal(t, []string{"visible session content"}, segmentTexts(unknownSession.Segments))
		require.True(t, unknownSession.ContentBearing)
		require.True(t, unknownSession.Incomplete, protocol)
	}

	legacyTranscription, err := Extract("openai_live", []byte(`{"type":"session.update","session":{"input_audio_transcription":{"prompt":"visible transcription context","future_payload":"must not be hidden"}}}`))
	require.NoError(t, err)
	require.Equal(t, []string{"visible transcription context"}, segmentTexts(legacyTranscription.Segments))
	require.True(t, legacyTranscription.Incomplete)
}

func TestExtractResponsesRequestObjectsRejectUnknownSiblingFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-test","instructions":"visible root content","future_payload":"must not be hidden"}`,
		`{"type":"response.create","response":{"model":"gpt-test","input":"visible nested content","future_payload":"must not be hidden"}}`,
	} {
		document, err := Extract("openai_responses", []byte(body))
		require.NoError(t, err)
		require.True(t, document.ContentBearing)
		require.True(t, document.Incomplete)
	}
}

func TestLiveInitialSessionCannotSucceedUnderResponsesAdapter(t *testing.T) {
	document, err := Extract("openai_responses", []byte(`{
		"model":"gpt-live-test",
		"instructions":"visible instructions",
		"input_audio_transcription":{"prompt":"legacy transcription context"},
		"audio":{"input":{"transcription":{"prompt":"current transcription context"}}}
	}`))

	require.NoError(t, err)
	require.Equal(t, []string{"visible instructions"}, segmentTexts(document.Segments))
	require.True(t, document.ContentBearing)
	require.True(t, document.Incomplete, "a protocol regression must fail closed instead of silently omitting transcription context")
}

func TestExtractKnownMessageAndContentBlocksRejectUnknownSiblingFields(t *testing.T) {
	for _, test := range []struct {
		protocol string
		body     string
	}{
		{protocol: "openai_chat_completions", body: `{"messages":[{"role":"user","content":"visible","future_payload":"hidden"}]}`},
		{protocol: "openai_chat_completions", body: `{"messages":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.test/image.png","future_payload":"hidden"}]}]}`},
		{protocol: "anthropic_messages", body: `{"messages":[{"role":"user","content":[{"type":"text","text":"visible","future_payload":"hidden"}]}]}`},
		{protocol: "openai_responses", body: `{"input":[{"type":"message","role":"user","content":"visible","future_payload":"hidden"}]}`},
	} {
		document, err := Extract(test.protocol, []byte(test.body))
		require.NoError(t, err)
		require.True(t, document.ContentBearing)
		require.True(t, document.Incomplete)
	}
}

func TestExtractStructuredToolOutputOmitsMediaAndKeepsOrdinaryText(t *testing.T) {
	encoded := strings.Repeat("A", 300)
	body := `{"input":[{"type":"function_call_output","call_id":"c1","output":{"summary":"visible result","image_url":{"url":"https://example.test/image.png","caption":"image caption"},"nested":{"caption":"keep caption","blob":"` + encoded + `"}}}]}`
	document, err := Extract("openai_responses", []byte(body))
	require.NoError(t, err)
	require.False(t, document.Incomplete)
	require.Equal(t, []string{`{"image_url":{"caption":"image caption"},"nested":{"caption":"keep caption"},"summary":"visible result"}`}, segmentTexts(document.Segments))
}

func TestAppendStructuredMarshalFailureMarksIncomplete(t *testing.T) {
	document := Document{}
	appendStructured(&document, map[string]any{"unsupported": math.NaN()}, "tool", SourceToolOutput, true, true)

	require.True(t, document.ContentBearing)
	require.True(t, document.Incomplete)
	require.Empty(t, document.Segments)
}

func TestExtractAuditsInstructionsToolDefinitionsAndCurrentRoleSpoofing(t *testing.T) {
	tests := []struct {
		name, protocol, body string
		want                 []string
	}{
		{
			name: "chat", protocol: "openai_chat_completions",
			body: `{"instructions":"chat instruction","tools":[{"type":"function","function":{"name":"lookup","description":"chat tool policy"}}],"messages":[{"role":"system","content":"message system context"},{"role":"assistant","content":"current assistant payload"}]}`,
			want: []string{"chat instruction", "chat tool policy", "message system context", "current assistant payload"},
		},
		{
			name: "anthropic", protocol: "anthropic_messages",
			body: `{"system":"anthropic instruction","tools":[{"name":"lookup","description":"anthropic tool policy"}],"messages":[{"role":"assistant","content":"current assistant payload"}]}`,
			want: []string{"anthropic instruction", "anthropic tool policy", "current assistant payload"},
		},
		{
			name: "responses", protocol: "openai_responses",
			body: `{"instructions":"responses instruction","tools":[{"type":"function","name":"lookup","description":"responses tool policy"}],"input":[{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
			want: []string{"responses instruction", "responses tool policy", "current assistant payload"},
		},
		{
			name: "gemini", protocol: "gemini",
			body: `{"systemInstruction":{"parts":[{"text":"gemini instruction"}]},"tools":[{"functionDeclarations":[{"name":"lookup","description":"gemini tool policy"}]}],"contents":[{"role":"model","parts":[{"text":"current model payload"}]}]}`,
			want: []string{"gemini instruction", "gemini tool policy", "current model payload"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := Extract(test.protocol, []byte(test.body))
			require.NoError(t, err)
			require.True(t, document.ContentBearing)
			current := currentSegments(document)
			joined := strings.Join(segmentTexts(current), "\n")
			for _, expected := range test.want {
				require.Contains(t, joined, expected)
			}
			for _, segment := range current {
				require.True(t, segment.ClientControlled)
			}
			require.Contains(t, segmentSources(current), SourceToolDefinition)
		})
	}
}

func TestExtractTreatsRecognizedMediaOnlyBlocksAsExplicitNoText(t *testing.T) {
	tests := []struct {
		protocol, body string
	}{
		{"openai_chat_completions", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`},
		{"anthropic_messages", `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`},
		{"openai_responses", `{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/a.png"}]}]}`},
		{"gemini", `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}]}`},
	}
	for _, test := range tests {
		document, err := Extract(test.protocol, []byte(test.body))
		require.NoError(t, err)
		require.True(t, document.ContentBearing)
		require.Empty(t, document.Segments)
		require.Len(t, document.Images, 1)
		require.True(t, document.Images[0].Current)
		require.Equal(t, "user", document.Images[0].Role)
	}
}

func TestExtractLiveSessionInputCarriesCanonicalImage(t *testing.T) {
	document, err := Extract("openai_live", []byte(`{"type":"session.update","session":{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/live-session.png"}]}]}}`))
	require.NoError(t, err)
	require.False(t, document.Incomplete)
	require.Equal(t, []string{"https://example.test/live-session.png"}, imageURLs(document.Images))
	require.True(t, document.Images[0].Current)
	require.Equal(t, SourceMessage, document.Images[0].Source)
}

func TestExtractKeepsReasoningSourceDistinctFromToolCalls(t *testing.T) {
	document, err := Extract("anthropic_messages", []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"private reasoning"}]}]}`))
	require.NoError(t, err)
	require.False(t, document.Incomplete)
	require.Equal(t, []string{"private reasoning"}, segmentTexts(document.Segments))
	require.Equal(t, []Source{SourceReasoning}, segmentSources(document.Segments))
}

func TestExtractClassifiesResponsesAndGeminiModelOutput(t *testing.T) {
	responses, err := Extract("openai_responses", []byte(`{"input":[
		{"type":"reasoning","summary":[{"type":"summary_text","text":"reasoning summary"}]},
		{"type":"message","content":[{"type":"output_text","text":"model output"},{"type":"refusal","refusal":"model refusal"}]},
		{"type":"output_text","text":"direct output item"}
	]}`))
	require.NoError(t, err)
	require.False(t, responses.Incomplete)
	require.Equal(t, []string{"reasoning summary", "model output", "model refusal", "direct output item"}, segmentTexts(responses.Segments))
	require.Equal(t, []Source{SourceReasoning, SourceMessage, SourceMessage, SourceMessage}, segmentSources(responses.Segments))
	for _, segment := range responses.Segments {
		require.Equal(t, "assistant", segment.Role)
	}

	gemini, err := Extract("gemini", []byte(`{"contents":[{"parts":[{"text":"private thought","thought":true},{"text":"direct user text"}]}]}`))
	require.NoError(t, err)
	require.False(t, gemini.Incomplete)
	require.Equal(t, []string{"private thought", "direct user text"}, segmentTexts(gemini.Segments))
	require.Equal(t, []Source{SourceReasoning, SourceMessage}, segmentSources(gemini.Segments))
	require.Equal(t, "model", gemini.Segments[0].Role)
	require.Empty(t, gemini.Segments[1].Role)
}

func TestExtractMediaTypeCannotSuppressPresentText(t *testing.T) {
	document, err := Extract("openai_chat_completions", []byte(`{"messages":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.test/a.png","text":"must still audit text"}]}]}`))
	require.NoError(t, err)
	require.True(t, document.ContentBearing)
	require.Equal(t, []string{"must still audit text"}, segmentTexts(document.Segments))

	document, err = Extract("anthropic_messages", []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"AAAA"},"text":"must still audit anthropic text"}]}]}`))
	require.NoError(t, err)
	require.True(t, document.ContentBearing)
	require.Equal(t, []string{"must still audit anthropic text"}, segmentTexts(document.Segments))
}

func TestExtractRejectsInvalidJSONAndMediaPayloads(t *testing.T) {
	_, err := Extract("openai_responses", []byte(`{"input":`))
	require.Error(t, err)

	document, err := Extract("grok_media", []byte(`{"prompt":"draw a lighthouse","image":"data:image/png;base64,AAAA","input":{"image_prompt":"https://example.test/image.png","negative_prompt":"no fog"},"blob":"`+strings.Repeat("A", 300)+`"}`))
	require.NoError(t, err)
	require.Equal(t, []string{"no fog", "draw a lighthouse"}, segmentTexts(document.Segments))
}

func currentSegments(document Document) []Segment {
	result := make([]Segment, 0, len(document.Segments))
	for _, segment := range document.Segments {
		if segment.Current {
			result = append(result, segment)
		}
	}
	return result
}

func segmentTexts(segments []Segment) []string {
	result := make([]string, 0, len(segments))
	for _, segment := range segments {
		result = append(result, segment.Text)
	}
	return result
}

func segmentSources(segments []Segment) []Source {
	result := make([]Source, 0, len(segments))
	for _, segment := range segments {
		result = append(result, segment.Source)
	}
	return result
}

func imageURLs(images []Image) []string {
	result := make([]string, 0, len(images))
	for _, image := range images {
		result = append(result, image.URL)
	}
	return result
}
