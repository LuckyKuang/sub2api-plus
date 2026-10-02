//go:build unit || !integration

package handler

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// System One 的用量记录在入队前完成快照：延迟 worker 执行时，请求级字段必须
// 来自不可变捕获值，不能读已被下一个请求复用的 Gin Context。
func TestSystemOneUsageRecordInputCapturesRequestScopedValues(t *testing.T) {
	h := &GatewayHandler{}
	groupID := int64(9)
	apiKey := &service.APIKey{ID: 4, UserID: 5, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformTypeSafe}}
	account := &service.Account{ID: 7, Platform: service.PlatformTypeSafe, Type: service.AccountTypeAPIKey}
	result := &service.SystemOneForwardResult{ForwardResult: service.ForwardResult{Model: "jev-latest"}}
	mapping := service.ChannelMappingResult{
		MappedModel:        "jev-latest-alias",
		ChannelID:          31,
		Mapped:             true,
		BillingModelSource: service.BillingModelSourceChannelMapped,
	}

	c, _ := newSystemOneHandlerContext(validSystemOneHandlerBody)
	requestCtx := context.WithValue(c.Request.Context(), ctxkey.RequestedPublicModel, "systemone-public")
	c.Request = c.Request.WithContext(requestCtx)
	c.Request.Header.Set("User-Agent", "client-original/1.0")

	input := h.systemOneUsageRecordInput(c, apiKey, account, nil, mapping, "jev-latest", []byte(`{"model":"jev-latest"}`), result, time.Now())

	// The Gin context is reused by the next request before the deferred worker runs.
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/another", strings.NewReader(`{"model":"other"}`))
	c.Request.Header.Set("User-Agent", "client-reused/2.0")

	require.Equal(t, "client-original/1.0", input.UserAgent)
	require.Equal(t, "systemone-public", input.OriginalModel)
	require.Equal(t, "jev-latest-alias", input.ChannelMappedModel)
	require.Equal(t, int64(31), input.ChannelID)
	require.Equal(t, service.BillingModelSourceChannelMapped, input.BillingModelSource)
	require.Equal(t, "systemone-public→jev-latest-alias", input.ModelMappingChain)
	require.Same(t, account, input.Account)
	require.Equal(t, "jev-latest", input.Result.Model)
	require.NotEmpty(t, input.RequestPayloadHash)
}

// mandatory 记录任务在 worker 池停止或队列溢出时不得静默丢失计费。
func TestSystemOneMandatoryUsageTaskNeverDropsRecording(t *testing.T) {
	t.Run("stopped pool", func(t *testing.T) {
		h := &GatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest()}
		executed := false
		h.submitMandatoryUsageRecordTask(context.Background(), func(context.Context) { executed = true })
		require.True(t, executed, "a stopped pool must not drop System One billing work")
	})

	t.Run("drop policy queue overflow", func(t *testing.T) {
		pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
			WorkerCount:    1,
			QueueSize:      1,
			TaskTimeout:    time.Minute,
			OverflowPolicy: config.UsageRecordOverflowPolicyDrop,
		})
		t.Cleanup(pool.Stop)
		started := make(chan struct{})
		block := make(chan struct{})
		t.Cleanup(func() { close(block) })
		require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) {
			close(started)
			<-block
		}))
		<-started
		require.Equal(t, service.UsageRecordSubmitModeEnqueued, pool.Submit(func(context.Context) { <-block }))

		h := &GatewayHandler{usageRecordWorkerPool: pool}
		executed := false
		h.submitMandatoryUsageRecordTask(context.Background(), func(context.Context) { executed = true })
		require.True(t, executed, "queue overflow must fall back to the mandatory synchronous record")
	})
}

// 结构约束：延迟闭包不得捕获可复用的 Gin Context，ChannelUsageFields 必须在
// 入队前的同步快照函数里计算。
func TestSystemOneUsageClosureNeverCapturesGinContext(t *testing.T) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "gateway_systemone.go", nil, 0)
	require.NoError(t, err)

	var recordFn, inputFn *ast.FuncDecl
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil {
			continue
		}
		switch fn.Name.Name {
		case "recordSystemOneUsage":
			recordFn = fn
		case "systemOneUsageRecordInput":
			inputFn = fn
		}
	}
	require.NotNil(t, recordFn)
	require.NotNil(t, inputFn)

	submissions := 0
	ast.Inspect(recordFn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "submitMandatoryUsageRecordTask" {
			return true
		}
		submissions++
		for _, arg := range call.Args {
			literal, ok := arg.(*ast.FuncLit)
			if !ok {
				continue
			}
			ast.Inspect(literal, func(inner ast.Node) bool {
				ident, ok := inner.(*ast.Ident)
				if ok && ident.Name == "c" {
					t.Errorf("the deferred System One usage task must not capture the Gin context at %s",
						fset.Position(ident.Pos()))
				}
				return true
			})
		}
		return true
	})
	require.Equal(t, 1, submissions)

	require.True(t, functionCallsIdent(inputFn, "clientRequestedUsageFields"),
		"systemOneUsageRecordInput must compute ChannelUsageFields before the task is enqueued")
	require.False(t, functionCallsIdent(recordFn, "clientRequestedUsageFields"),
		"ChannelUsageFields must not be computed inside the deferred submission function")
}

func functionCallsIdent(fn *ast.FuncDecl, name string) bool {
	found := false
	ast.Inspect(fn, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == name {
			found = true
		}
		return true
	})
	return found
}
