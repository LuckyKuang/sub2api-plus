//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const codexNamedContextFixture = `{"type":"function_call_output","namespace":"agent","name":"notifications","output":"Alice mentioned you."}`

func TestCodexNamedToolOutputNamespaceIsContextMetadata(t *testing.T) {
	// Official additional_context.rs emits this namespace as the source of
	// unsolicited context; it is not a tool declaration/call namespace to flatten.
	for _, keepCalls := range []bool{false, true} {
		body := []byte(`{"input":[` + codexNamedContextFixture + `,{"type":"message","namespace":"legacy","role":"user","content":"review"}]}`)
		out, err := stripOpenAIResponsesInputNamespaces(body, keepCalls)
		require.NoError(t, err)
		require.JSONEq(t, codexNamedContextFixture, gjson.GetBytes(out, "input.0").Raw)
		require.False(t, gjson.GetBytes(out, "input.1.namespace").Exists())
	}
}

// Named additional context needs no previous tool call or response anchor:
// openai/codex 6d8f311b00e823052622eac2b463919ad86a666d.
func TestCodexNamedToolOutputIsSelfContainedContext(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":[` + codexNamedContextFixture + `,{"type":"message","role":"user","content":"review this"}]}`)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, FunctionCallOutputValidation{}, ValidateFunctionCallOutputContext(decoded))
	require.Equal(t, FunctionCallOutputValidation{}, ValidateFunctionCallOutputContextBytes(body))
	require.Equal(t, ToolContinuationSignals{}, AnalyzeToolContinuationSignals(decoded))
	require.Equal(t, ToolCallOutputContextCoverage{}, AnalyzeToolCallOutputContextCoverageBytes(body))
	require.False(t, shouldInferIngressFunctionCallOutputPreviousResponseID(true, 2,
		AnalyzeToolContinuationSignals(decoded), "", "resp_previous"))
	fullInput := []json.RawMessage{json.RawMessage(codexNamedContextFixture), json.RawMessage(`{"type":"message","role":"user","content":"review this"}`)}
	require.False(t, openAIWSRawItemsHasFunctionCallOutput(fullInput), "named context cannot force closure of a reconnectable socket")
	require.False(t, openAIWSRawPayloadHasToolCallOutput(body))
	retry, replayable, err := buildOpenAIWSCurrentTurnRetryPayload(body, fullInput, true, "gpt-6.1-sol")
	require.NoError(t, err)
	require.True(t, replayable, "a complete context/user turn can be replayed after owner failover")
	require.Equal(t, "agent", gjson.GetBytes(retry, "input.0.namespace").String())
	require.False(t, gjson.GetBytes(retry, "input.0.call_id").Exists())
}

func TestCodexNamedToolOutputDoesNotSupplyOrdinaryCallCoverage(t *testing.T) {
	for _, extra := range []struct {
		name, input string
		validation  FunctionCallOutputValidation
		coverage    ToolCallOutputContextCoverage
	}{
		{"ordinary missing call", `,{"type":"function_call_output","output":"ordinary result"}`, FunctionCallOutputValidation{HasFunctionCallOutput: true, HasFunctionCallOutputMissingCallID: true}, ToolCallOutputContextCoverage{HasFunctionCallOutput: true}},
		{"unmatched paired output", `,{"type":"function_call_output","call_id":"call_1","output":"result"}`, FunctionCallOutputValidation{HasFunctionCallOutput: true}, ToolCallOutputContextCoverage{HasFunctionCallOutput: true}},
		{"referenced paired output", `,{"type":"function_call_output","call_id":"call_1","output":"result"},{"type":"item_reference","id":"call_1"}`, FunctionCallOutputValidation{HasFunctionCallOutput: true, HasItemReferenceForAllCallIDs: true}, ToolCallOutputContextCoverage{HasFunctionCallOutput: true, ContextCoversAllCallIDs: true}},
	} {
		t.Run(extra.name, func(t *testing.T) {
			body := []byte(`{"input":[` + codexNamedContextFixture + extra.input + `]}`)
			var decoded map[string]any
			require.NoError(t, json.Unmarshal(body, &decoded))
			require.Equal(t, extra.validation, ValidateFunctionCallOutputContext(decoded))
			require.Equal(t, extra.validation, ValidateFunctionCallOutputContextBytes(body))
			require.Equal(t, extra.coverage, AnalyzeToolCallOutputContextCoverageBytes(body))
			require.True(t, openAIWSRawPayloadHasToolCallOutput(body), "ordinary replies retain the socket continuation boundary")
		})
	}
}

func TestCodexNamedToolOutputForwardPreservesOfficialPayloadForBothAccountTypes(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", accountType, passthrough), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
				svc := newOpenAIRejectedFieldTestService(upstream)
				account := newOpenAIRejectedFieldTestAccount()
				if accountType == AccountTypeOAuth {
					account = newOpenAIOAuthNamespaceTestAccount()
				}
				account.Extra = map[string]any{"openai_passthrough": passthrough, "responses_supported": true}
				body := []byte(`{"model":"gpt-6.1-sol","stream":true,"store":false,"tool_choice":"auto","input":[` + codexNamedContextFixture + `,{"type":"message","role":"user","id":"msg_input_8c8317ec-e000-4eef-8a92-72c18e1cdbae","content":[{"type":"input_text","text":"review this"}]}]}`)
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request.Header.Set("User-Agent", "codex_cli_rs/0.164.0 (Ubuntu 24.04; x86_64) xterm-256color")
				_, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, "function_call_output", gjson.GetBytes(upstream.lastBody, "input.0.type").String())
				require.Equal(t, "agent", gjson.GetBytes(upstream.lastBody, "input.0.namespace").String())
				require.Equal(t, "notifications", gjson.GetBytes(upstream.lastBody, "input.0.name").String())
				require.Equal(t, "Alice mentioned you.", gjson.GetBytes(upstream.lastBody, "input.0.output").String())
				require.False(t, gjson.GetBytes(upstream.lastBody, "input.0.call_id").Exists(), "do not invent a tool invocation for context")
				require.Equal(t, "msg_input_8c8317ec-e000-4eef-8a92-72c18e1cdbae", gjson.GetBytes(upstream.lastBody, "input.1.id").String())
			})
		}
	}
}
