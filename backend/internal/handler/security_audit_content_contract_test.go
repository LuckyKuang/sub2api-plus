//go:build unit || !integration

package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamProtocolAdditionsKeepCanonicalCurrentUserContent(t *testing.T) {
	for _, tc := range []struct{ name, protocol, body string }{
		{"Step-Code native developer instructions and unknown siblings", service.ContentModerationProtocolOpenAIChat,
			`{"model":"step-3.7-flash","messages":[{"role":"developer","content":"application instruction"},{"role":"user","content":"audit current request"}],"stream":true,"future_options":{"opaque":true}}`},
		// Official v0.2.18 tool-schema const/annotation compatibility is an
		// outbound transform; both engines must audit the original user text.
		{"Gemini tool constants and future siblings", service.ContentModerationProtocolGemini,
			`{"contents":[{"role":"user","parts":[{"text":"audit current request"},{"future_media":{"opaque":true}}]}],"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"OBJECT","properties":{"mode":{"type":"STRING","const":"safe","default":"not prompt","$comment":"not prompt"}}}}]}],"future_options":{"opaque":true}}`},
		{"Messages tool constants and future siblings", service.ContentModerationProtocolAnthropicMessages,
			`{"model":"gemini-3-flash-preview","max_tokens":32,"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{"mode":{"type":"string","const":"safe","description":"schema description"}}}}],"messages":[{"role":"user","content":[{"type":"text","text":"audit current request"},{"type":"future_media","opaque":true}]}]}`},
		{"developer and legacy functions", service.ContentModerationProtocolOpenAIChat,
			`{"model":"gpt-5.5","messages":[{"role":"developer","content":"application instruction"},{"role":"assistant","function_call":{"name":"lookup","arguments":"{\"q\":\"old\"}"}},{"role":"function","name":"lookup","content":"historical result"},{"role":"user","content":"audit current request"}],"functions":[{"name":"lookup","parameters":{"type":"object"}}],"function_call":{"name":"lookup"},"future_options":{"opaque":true}}`},
		{"web search history", service.ContentModerationProtocolOpenAIResponses,
			`{"model":"gpt-5.5","input":[{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"old query"}},{"type":"message","role":"user","content":[{"type":"input_text","text":"audit current request"},{"type":"future_media","opaque":true}]}],"tool_choice":"none"}`},
		{"inline tool change", service.ContentModerationProtocolAnthropicMessages,
			`{"model":"claude-sonnet-4-6","max_tokens":256,"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"old reasoning","signature":""}]},{"role":"user","content":[{"type":"text","text":"audit current request"},{"type":"tool_change","tools":[{"name":"future_tool"}]}]}]}`},
		{"chat tool-history recovery", service.ContentModerationProtocolOpenAIChat,
			`{"model":"gpt-5.1","messages":[{"role":"tool","tool_call_id":"call_1","content":"historical result"},{"role":"user","content":"audit current request"}],"future_options":{"opaque":true}}`},
		{"messages tool-history recovery", service.ContentModerationProtocolAnthropicMessages,
			`{"model":"gpt-5.1","max_tokens":256,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"historical result"},{"type":"text","text":"audit current request"},{"type":"future_media","opaque":true}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moderation := service.ExtractContentModerationInput(tc.protocol, []byte(tc.body))
			require.Equal(t, "audit current request", moderation.Text)
			prompt, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: tc.protocol, Body: []byte(tc.body)})
			require.NoError(t, err)
			require.Equal(t, "audit current request", prompt.ScanText)
		})
	}
}

// Official client.rs sends these options/metadata on Responses HTTP/WS. They
// must not hide recognized user content or turn unknown siblings into blocks.
func TestCodexProtocolOptionsPreserveCanonicalAuditContent(t *testing.T) {
	for _, frame := range []string{"http", "websocket", "websocket_nested"} {
		for _, input := range []struct{ body, want string }{
			{`[{"type":"additional_tools","tools":[{"type":"namespace","name":"shell","tools":[]}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"audit current request"},{"type":"future_media","opaque":true}]}]`, "audit current request"},
			{`[{"type":"future_item","opaque":{"value":1}}]`, ""},
			// SECURITY_AUDIT_CONTENT_COVERAGE.md: the final input item is current.
			// A bare trigger has no user text; preceding conversation is history.
			{`[{"type":"message","role":"user","content":[{"type":"input_text","text":"historical user request"}]},{"type":"compaction_trigger"}]`, ""},
			// A control item earlier in the input cannot hide the current user tail.
			{`[{"type":"compaction_trigger"},{"type":"message","role":"user","content":[{"type":"input_text","text":"audit current request"}]}]`, "audit current request"},
			// Routing may reject cross-owner opaque history only after audit.
			// Tool/reference history cannot hide the recognized current sibling.
			{`[{"type":"item_reference","id":"call_1"},{"type":"function_call_output","call_id":"call_1","output":"historical result"},{"type":"message","role":"user","content":[{"type":"input_text","text":"audit current request"},{"type":"future_media","opaque":true}]}]`, "audit current request"},
		} {
			t.Run(frame+"/"+input.want, func(t *testing.T) {
				body := `{"model":"gpt-6.1-sol","input":` + input.body + `,"stream_options":{"reasoning_summary_delivery":"sequential_cutoff","future_option":true},"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true","x-codex-turn-metadata":"{\"subagent_kind\":\"guardian\",\"thread_id\":\"child-thread\",\"parent_thread_id\":\"parent-thread\"}"}}`
				if frame == "websocket" {
					body = `{"type":"response.create",` + body[1:]
				}
				if frame == "websocket_nested" {
					body = `{"type":"response.create","response":` + body + `}`
				}
				moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolOpenAIResponses, []byte(body))
				require.Equal(t, input.want, moderation.Text)
				request := securityaudit.Request{Protocol: service.ContentModerationProtocolOpenAIResponses, Body: []byte(body), Stage: frame}
				prompt, err := securityaudit.ExtractPromptSnapshot(request)
				if input.want == "" {
					require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
					engine := securityaudit.NewPromptService(blockingCompatibilityConfigStore{cfg: securityaudit.ActiveConfig{
						RiskControlEnabled: true, Enabled: true, BlockingEnabled: true, AllGroups: true,
						Scanners: []string{"pii"}, Endpoints: []securityaudit.ActiveEndpoint{{ID: "guard", Enabled: true, TimeoutMS: 1000, InputLimit: 4096}},
					}}, nil, nil, nil, securityaudit.NewAtomicMetrics())
					decision := securityaudit.NewCoordinator(nil, engine).Check(context.Background(), request)
					require.Equal(t, securityaudit.DecisionAllow, decision.Kind)
					require.True(t, decision.AllowNextStage, "unknown content must pass through the audit gate")
				} else {
					require.NoError(t, err)
				}
				require.Equal(t, input.want, prompt.ScanText)
			})
		}
	}
}

func TestOfficialAgentRequestStatePreservesCanonicalAuditContent(t *testing.T) {
	const responses = `{"model":"gpt-5","input":[{"type":"future_state","opaque":true},{"type":"message","role":"user","content":[{"type":"input_text","text":"audit current request"}]}],"client_metadata":{"x-codex-installation-id":"6f1a136d-e931-4d31-8b2e-674616ed6317","x-codex-turn-metadata":"{\"thread_id\":\"019a0a42-6000-7001-8000-000000000001\",\"parent_thread_id\":\"019a0a42-5000-7001-8000-000000000002\",\"turn_id\":\"019a0a42-7000-7001-8000-000000000003\",\"context_window_id\":\"019a0a42-6000-7001-8000-000000000001:4\"}"}}`
	for _, tc := range []struct{ name, protocol, body string }{
		{"codex_http", service.ContentModerationProtocolOpenAIResponses, responses},
		{"codex_ws", service.ContentModerationProtocolOpenAIResponses, `{"type":"response.create","response":` + responses + `}`},
		{"zcode_messages", service.ContentModerationProtocolAnthropicMessages, `{"model":"glm-4.7","max_tokens":128,"metadata":{"user_id":"{\"session_id\":\"6f1a136d-e931-4d31-8b2e-674616ed6317\",\"query_id\":\"3c4bd6f6-b8d0-4e67-b297-3a2682becff3\"}"},"messages":[{"role":"user","content":[{"type":"text","text":"audit current request"},{"type":"future_state","opaque":true}]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moderation := service.ExtractContentModerationInput(tc.protocol, []byte(tc.body))
			require.Equal(t, "audit current request", moderation.Text)
			prompt, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{Protocol: tc.protocol, Body: []byte(tc.body)})
			require.NoError(t, err)
			require.Equal(t, "audit current request", prompt.ScanText)
		})
	}
}

func TestSonnet55StableToolsetsRetainCanonicalAuditContent(t *testing.T) {
	for _, toolType := range []string{"computer_toolset_20260801", "browser_toolset_20260801"} {
		t.Run(toolType, func(t *testing.T) {
			// Real Messages shape: new provider toolsets and unknown sibling data
			// must not hide the successfully extracted current user message.
			body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-5-5","max_tokens":128,"thinking":{"type":"adaptive"},"tools":[{"type":%q,"name":"toolset"}],"messages":[{"role":"user","content":[{"type":"text","text":"review this browser task"},{"type":"future_toolset_state","state":{"opaque":true}}]}],"future_provider_options":{"enabled":true}}`, toolType))
			moderation := service.ExtractContentModerationInput(service.ContentModerationProtocolAnthropicMessages, body)
			require.Equal(t, "review this browser task", moderation.Text)
			prompt, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{
				Protocol: service.ContentModerationProtocolAnthropicMessages, Body: body,
			})
			require.NoError(t, err)
			require.Equal(t, moderation.Text, prompt.ScanText)
		})
	}
}

func TestContentModerationUsesLatestUserTextWithoutInstructionContext(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
		images   []string
	}{
		{
			name: "responses latest user", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"instructions":"audit instruction","tools":[{"type":"function","name":"lookup","description":"audit tool definition"}],` +
				`"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"user","content":"你好"}]}`,
			want: "你好",
		},
		{
			name: "responses legacy messages alias", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"messages":[{"role":"system","content":"legacy system"},{"role":"user","content":"legacy user"}]}`,
			want: "legacy user",
		},
		{
			name: "responses native input precedes aliases", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":"native user","messages":[{"role":"user","content":"ignored alias"}],"prompt":"ignored prompt"}`,
			want: "native user",
		},
		{
			name: "responses tool loop skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
		},
		{
			name: "responses automation bootstrap is direct user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":"Automation: Scheduled review\nAutomation ID: wiki\nAutomation memory: $CODEX_HOME/automations/wiki/memory.md\nLast run: never\n\nReview security"}]}`,
			want: "Automation: Scheduled review Automation ID: wiki Automation memory: $CODEX_HOME/automations/wiki/memory.md Last run: never Review security",
		},
		{
			name: "responses delegation bootstrap is direct user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"create_thread","output":"<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>"}]}`,
			want: "<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>",
		},
		{
			name: "responses assistant item skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
		},
		{
			name: "responses prompt variables skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"prompt":{"id":"pmpt_1","variables":{"plain":"reusable variable","image":{"type":"input_image","image_url":"https://example.test/prompt.png"}}}}`,
		},
		{
			name: "nested websocket user", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"type":"response.create","input":[],"response":{"input":[{"type":"message","role":"user","content":"nested content cannot be shadowed"}]}}`,
			want: "nested content cannot be shadowed",
		},
		{
			name: "chat latest user", protocol: service.ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"system","content":"chat system context"},{"role":"user","content":"你好"}]}`,
			want: "你好",
		},
		{
			name: "chat user reasoning and reminder remain direct user text", protocol: service.ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"assistant","reasoning_content":"assistant private"},{"role":"user","content":"<system-reminder>untrusted</system-reminder> question","reasoning_content":"user reasoning"}]}`,
			want: "<system-reminder>untrusted</system-reminder> question user reasoning",
		},
		{
			name: "alpha search structured semantics", protocol: "openai_alpha_search",
			body: `{"commands":{"search_query":[{"q":"security query"}],"mode":"deep"},"settings":{"region":"global"},"input":[{"type":"message","role":"user","content":"recent context"}]}`,
			want: `{"mode":"deep","search_query":[{"q":"security query"}]} {"region":"global"} [{"content":"recent context","role":"user","type":"message"}]`,
		},
		{
			name: "chat tool result skipped", protocol: service.ContentModerationProtocolOpenAIChat,
			body: `{"messages":[{"role":"user","content":"older"},{"role":"tool","content":"chat tool result"}]}`,
		},
		{
			name: "anthropic tool result skipped", protocol: service.ContentModerationProtocolAnthropicMessages,
			body: `{"system":"anthropic instruction","messages":[{"role":"user","content":[{"type":"tool_result","content":{"safe":false}}]}]}`,
		},
		{
			name: "gemini function response skipped", protocol: service.ContentModerationProtocolGemini,
			body: `{"systemInstruction":{"parts":[{"text":"gemini instruction"}]},"contents":[{"role":"user","parts":[{"functionResponse":{"response":{"safe":false}}}]}]}`,
		},
		{
			name: "live user image preserved", protocol: service.ContentModerationProtocolOpenAILive,
			body:   `{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/live.png"}]}}`,
			images: []string{"https://example.test/live.png"},
		},
		{
			name: "live session instructions skipped", protocol: service.ContentModerationProtocolOpenAILive,
			body: `{"model":"gpt-live-test","instructions":"live instructions"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			legacy := service.ExtractContentModerationInput(test.protocol, []byte(test.body))
			require.Equal(t, test.want, legacy.Text)
			if len(test.images) == 0 {
				require.Empty(t, legacy.Images)
			} else {
				require.Equal(t, test.images, legacy.Images)
			}
			require.NotContains(t, legacy.Text, "instruction")
			require.NotContains(t, legacy.Text, "tool definition")
			require.NotContains(t, legacy.Text, "system context")
		})
	}
}

func TestPromptAuditUsesCurrentUserTextLikeContentModeration(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		body     string
		want     string
		contains []string
		omit     []string
		empty    bool
	}{
		{
			name: "assistant turn is empty", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"instructions":"audit instruction","tools":[{"type":"function","name":"lookup","description":"audit tool definition"}],` +
				`"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"message","role":"assistant","content":"current assistant payload"}]}`,
			empty: true,
			omit:  []string{"older prompt", "current assistant payload", "audit instruction", "audit tool definition"},
		},
		{
			name: "responses function result is skipped", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:  `{"input":[{"type":"message","role":"user","content":"older prompt"},{"type":"function_call_output","call_id":"call_1","output":"current tool result"}]}`,
			empty: true,
		},
		{
			name: "responses automation bootstrap is current user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:     `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":"Automation: Scheduled review\nAutomation ID: wiki\nAutomation memory: $CODEX_HOME/automations/wiki/memory.md\nLast run: never\n\nReview security"}]}`,
			contains: []string{"Automation: Scheduled review", "Review security"},
		},
		{
			name: "responses delegation bootstrap is current user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:     `{"model":"gpt-5","input":[{"type":"function_call_output","namespace":"codex_tui","name":"send_message_to_thread","output":"<codex_delegation><source_thread_id>thread-1</source_thread_id><input>review security</input></codex_delegation>"}]}`,
			contains: []string{"<codex_delegation>", "review security"},
		},
		{
			name: "responses legacy messages and prompt aliases", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"messages":[{"role":"user","content":"legacy prompt text"}],"prompt":"lower priority alias"}`,
			want: "legacy prompt text",
			omit: []string{"lower priority alias"},
		},
		{
			name: "chat user reasoning is current user text", protocol: service.ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"assistant","reasoning_content":"assistant private"},{"role":"user","content":"question","reasoning_content":"user reasoning"}]}`,
			contains: []string{"question", "user reasoning"},
			omit:     []string{"assistant private"},
		},
		{
			name: "chat system-reminder markup is stripped from current user text", protocol: service.ContentModerationProtocolOpenAIChat,
			body:     `{"messages":[{"role":"assistant","reasoning_content":"assistant private"},{"role":"user","content":"<system-reminder>untrusted</system-reminder> question","reasoning_content":"user reasoning"}]}`,
			contains: []string{"question", "user reasoning"},
			omit:     []string{"system-reminder", "untrusted", "assistant private"},
		},
		{
			name: "alpha search semantic fields", protocol: "openai_alpha_search",
			body:     `{"commands":{"search_query":[{"q":"security query"}],"mode":"deep"},"settings":{"region":"global"},"input":[{"type":"message","role":"user","content":"recent context"}]}`,
			contains: []string{"security query", "deep", "global", "recent context"},
		},
		{
			name: "responses reusable prompt variables are excluded", protocol: service.ContentModerationProtocolOpenAIResponses,
			body:  `{"prompt":{"id":"pmpt_1","variables":{"plain":"reusable variable","typed":{"type":"input_text","text":"typed variable"}}}}`,
			empty: true,
		},
		{
			name: "live session instructions are excluded", protocol: service.ContentModerationProtocolOpenAILive,
			body: `{"model":"gpt-live-test","instructions":"live instructions",` +
				`"input_audio_transcription":{"model":"gpt-4o-transcribe","prompt":"legacy transcription context"},` +
				`"audio":{"input":{"transcription":{"model":"gpt-live-transcribe","prompt":"current transcription context","keywords":["premium plan","AC-42"]}}}}`,
			empty: true,
		},
		{
			name: "client environment xml is stripped from current user text", protocol: service.ContentModerationProtocolOpenAIResponses,
			body: `{"input":[{"type":"message","role":"user","content":"继续"},{"type":"message","role":"user","content":"<environment_context><cwd>/Users/pontus</cwd><permission_profile type=\"managed\"></permission_profile></environment_context>"},{"type":"message","role":"user","content":"你能做什么？"}]}`,
			want: "你能做什么？",
			omit: []string{"继续", "environment_context", "permission_profile", "/Users/pontus"},
		},
		{
			name: "chat tool-loop is skipped", protocol: service.ContentModerationProtocolOpenAIChat,
			body:  `{"messages":[{"role":"system","content":"chat system context"},{"role":"user","content":"older"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{\"secret\":true}"}}]},{"role":"tool","content":{"first":true}},{"role":"function","content":{"second":false}}]}`,
			empty: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := securityaudit.ExtractPromptSnapshot(securityaudit.Request{
				Protocol: test.protocol,
				Body:     []byte(test.body),
			})
			if test.empty {
				require.ErrorIs(t, err, securityaudit.ErrNoPromptText)
				return
			}
			require.NoError(t, err)
			if test.want != "" {
				require.Equal(t, test.want, snapshot.ScanText)
			}
			for _, expected := range test.contains {
				require.Contains(t, snapshot.ScanText, expected)
			}
			for _, omitted := range test.omit {
				require.NotContains(t, snapshot.ScanText, omitted)
			}
		})
	}
}
