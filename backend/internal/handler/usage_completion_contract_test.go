//go:build unit

package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Stream-capable gateway handlers must explicitly set IsComplete. The handler
// knows whether Forward returned an error, which cannot be inferred from the
// result alone when preserving partial upstream usage for billing.
func TestStreamGatewayUsageInputsCarryExplicitCompletion(t *testing.T) {
	tests := []struct {
		file     string
		typeName string
	}{
		{file: "gateway_handler.go", typeName: "RecordUsageInput"},
		{file: "gateway_handler_chat_completions.go", typeName: "RecordUsageInput"},
		{file: "gateway_handler_responses.go", typeName: "RecordUsageInput"},
		{file: "openai_gateway_handler.go", typeName: "OpenAIRecordUsageInput"},
		{file: "gemini_v1beta_handler.go", typeName: "RecordUsageInput"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(".", tt.file), nil, 0)
			require.NoError(t, err)

			var missing []token.Position
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || !isServiceInputLiteral(literal.Type, tt.typeName) {
					return true
				}
				if !compositeLiteralHasKey(literal, "IsComplete") {
					missing = append(missing, fset.Position(literal.Lbrace))
				}
				return true
			})

			require.NotEmpty(t, allServiceInputLiterals(file, tt.typeName), "expected a %s literal", tt.typeName)
			require.Empty(t, missing, "stream-capable handler usage records must set IsComplete explicitly")
		})
	}
}

func TestOpenAIResponsesUsageBillingHasSingleSubmissionPath(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(".", "openai_gateway_handler.go"), nil, 0)
	require.NoError(t, err)

	var responses *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "Responses" {
			responses = function
			break
		}
	}
	require.NotNil(t, responses, "expected Responses handler")

	submissionCalls := 0
	ast.Inspect(responses.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "submitOpenAIUsageRecordTask" {
			submissionCalls++
		}
		return true
	})

	require.Equal(t, 1, submissionCalls,
		"Responses must submit one usage record per forwarding attempt; partial errors use the same path")
}

func isServiceInputLiteral(expr ast.Expr, typeName string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != typeName {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "service"
}

func allServiceInputLiterals(file *ast.File, typeName string) []ast.Node {
	var literals []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if ok && isServiceInputLiteral(literal.Type, typeName) {
			literals = append(literals, literal)
		}
		return true
	})
	return literals
}
