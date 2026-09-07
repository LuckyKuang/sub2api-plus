package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type promptAuditOrderCase struct {
	file       string
	function   string
	auditToken string
}

func TestPromptAuditGatePrecedesAccountBillingAndUpstreamSideEffects(t *testing.T) {
	tests := []promptAuditOrderCase{
		{file: "gateway_handler.go", function: "Messages", auditToken: "checkSecurityAudit"},
		{file: "gateway_handler_chat_completions.go", function: "ChatCompletions", auditToken: "checkSecurityAudit"},
		{file: "gateway_handler_responses.go", function: "Responses", auditToken: "checkSecurityAudit"},
		{file: "gemini_v1beta_handler.go", function: "GeminiV1BetaModels", auditToken: "checkSecurityAudit"},
		{file: "openai_gateway_handler.go", function: "Responses", auditToken: "checkSecurityAudit"},
		{file: "openai_gateway_handler.go", function: "Messages", auditToken: "checkSecurityAudit"},
		{file: "openai_chat_completions.go", function: "ChatCompletions", auditToken: "checkSecurityAudit"},
		{file: "openai_images.go", function: "Images", auditToken: "checkSecurityAudit"},
		{file: "grok_media.go", function: "handleGrokMedia", auditToken: "checkSecurityAudit"},
		{file: "openai_embeddings.go", function: "Embeddings", auditToken: "checkSecurityAudit"},
		{file: "openai_alpha_search.go", function: "AlphaSearch", auditToken: "checkSecurityAudit"},
		{file: "openai_live.go", function: "Live", auditToken: "checkSecurityAudit"},
		{file: "image_task_handler.go", function: "Submit", auditToken: "checkSecurityAuditBeforeSubmit"},
		{file: "batch_image_handler.go", function: "Submit", auditToken: "checkSecurityAuditBeforeSubmit"},
	}
	sideEffectTokens := []string{
		"CheckBillingEligibility(", "SelectAccount", ".Forward", "acquireResponsesUserSlot(",
		"AcquireUserSlot", "TryAcquireUserSlot", "acquireImageGenerationSlot(",
		"h.tasks.Create(", "h.tasks.CreateWithMetadata(", "h.service.Submit(", "CreateLiveCall(",
		"StartOpenAICompactSSEKeepalive(", "ResolveChannelMappingAndRestrict(",
	}
	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.function, func(t *testing.T) {
			functionSource := stripGoComments(goFunctionSource(t, tt.file, tt.function))
			auditIndex := strings.Index(functionSource, tt.auditToken)
			require.NotEqual(t, -1, auditIndex, "missing Prompt Audit gate")
			foundSideEffect := false
			for _, sideEffect := range sideEffectTokens {
				index := strings.Index(functionSource, sideEffect)
				if index < 0 {
					continue
				}
				foundSideEffect = true
				require.Lessf(t, auditIndex, index, "%s must run before %s", tt.auditToken, sideEffect)
			}
			require.True(t, foundSideEffect, "coverage case must contain a downstream side effect")
		})
	}
}

func TestLiveSidebandAuditHookPrecedesUpstreamWrite(t *testing.T) {
	functionSource := stripGoComments(goFunctionSource(t, "../service/openai_live.go", "ProxyLiveSidebandWithHooks"))
	auditIndex := strings.Index(functionSource, "hooks.BeforeClientFrame(proxyCtx, messageType, payload)")
	writeIndex := strings.Index(functionSource, "upstream.WriteFrame(proxyCtx, messageType, payload)")
	require.NotEqual(t, -1, auditIndex, "missing Live sideband content-audit hook")
	require.NotEqual(t, -1, writeIndex, "missing Live sideband upstream write")
	require.Less(t, auditIndex, writeIndex, "Live sideband audit must run before the client frame reaches upstream")
	handlerSource := stripGoComments(goFunctionSource(t, "openai_live.go", "LiveSideband"))
	require.NotContains(t, handlerSource, "messageType != coderws.MessageText", "binary control frames must not bypass canonical extraction")
}

func TestOpenAIResponsesAuditsImmutableInboundBodyBeforePostAuditSideEffects(t *testing.T) {
	source := stripGoComments(goFunctionSource(t, "openai_gateway_handler.go", "Responses"))
	captureIndex := strings.Index(source, "securityAuditBody := append([]byte(nil), body...)")
	normalizeIndex := strings.Index(source, "normalizeOpenAIResponsesCompactRequest(")
	automationIndex := strings.Index(source, "normalizeCodexAutomationBootstrap(")
	delegationIndex := strings.Index(source, "normalizeCodexDelegationBootstrap(")
	auditIndex := strings.Index(source, "checkSecurityAudit(")
	keepaliveIndex := strings.Index(source, "StartOpenAICompactSSEKeepalive(")
	require.NotEqual(t, -1, captureIndex)
	require.NotEqual(t, -1, normalizeIndex)
	require.NotEqual(t, -1, automationIndex)
	require.NotEqual(t, -1, delegationIndex)
	require.NotEqual(t, -1, auditIndex)
	require.NotEqual(t, -1, keepaliveIndex)
	require.Less(t, captureIndex, normalizeIndex, "the audited body must be frozen before compact normalization")
	require.Contains(t, source[auditIndex:], "securityAuditBody", "the audit gate must consume the immutable inbound body")
	require.Less(t, auditIndex, normalizeIndex, "security audit must complete before compact normalization")
	require.Less(t, auditIndex, automationIndex, "security audit must complete before automation bootstrap normalization")
	require.Less(t, auditIndex, delegationIndex, "security audit must complete before delegation bootstrap normalization")
	require.Less(t, auditIndex, keepaliveIndex, "compact keepalive must not commit a response before blocking audit completes")
}

func TestResponsesPassthroughAuditsRawPayloadOutsideResponseCreateGate(t *testing.T) {
	source := stripGoComments(goFunctionSource(t, "../service/openai_ws_v2_passthrough_adapter.go", "proxyResponsesWebSocketV2Passthrough"))
	captureIndex := strings.Index(source, "auditPayload := payload")
	hookIndex := strings.Index(source, "hooks.BeforeRequest(turnNo, auditPayload, requestModelForThisFrame)")
	require.NotEqual(t, -1, captureIndex)
	require.NotEqual(t, -1, hookIndex)
	require.Less(t, captureIndex, hookIndex)
	normalizeIndex := strings.Index(source, "normalizeOpenAIResponsesLitePayloadForAccount(payload, account)")
	require.Greater(t, normalizeIndex, hookIndex, "passthrough payload normalization must start after raw-frame audit")
	nextCreateGate := strings.Index(source[hookIndex:], "if isResponseCreate {")
	require.Greater(t, nextCreateGate, 0, "model mapping and turn lifecycle gate must begin after the all-frame audit hook")
}

func stripGoComments(source string) string {
	source = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(source, "")
	return regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(source, "")
}

func goFunctionSource(t *testing.T, filename, functionName string) string {
	t.Helper()
	raw, err := os.ReadFile(filename)
	require.NoError(t, err)
	files := token.NewFileSet()
	parsed, err := parser.ParseFile(files, filename, raw, 0)
	require.NoError(t, err)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != functionName || function.Body == nil {
			continue
		}
		start := files.Position(function.Pos()).Offset
		end := files.Position(function.End()).Offset
		require.Greater(t, end, start)
		return string(raw[start:end])
	}
	t.Fatalf("function %s not found in %s", functionName, filename)
	return ""
}
