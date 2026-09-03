//go:build unit

package handler

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/ctxkey"
	middleware2 "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type clientDisconnectRiskHandlerRepoStub struct {
	mu        sync.Mutex
	begins    []service.ClientDisconnectRiskBegin
	finalizes []service.ClientDisconnectRiskFinalize
}

func (r *clientDisconnectRiskHandlerRepoStub) Begin(_ context.Context, input service.ClientDisconnectRiskBegin) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.begins = append(r.begins, input)
	return int64(len(r.begins)), nil
}

func (r *clientDisconnectRiskHandlerRepoStub) Finalize(_ context.Context, input service.ClientDisconnectRiskFinalize) (service.ClientDisconnectRiskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalizes = append(r.finalizes, input)
	return service.ClientDisconnectRiskResult{}, nil
}

func (r *clientDisconnectRiskHandlerRepoStub) ClearUser(context.Context, int64) error { return nil }
func (r *clientDisconnectRiskHandlerRepoStub) ListEvents(context.Context, service.ClientDisconnectRiskEventFilter) ([]service.ClientDisconnectRiskEvent, int64, error) {
	return nil, 0, nil
}

func newClientDisconnectRiskHandlerContext(requestID, clientRequestID string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, requestID)
	if clientRequestID != "" {
		ctx = context.WithValue(ctx, ctxkey.ClientRequestID, clientRequestID)
	}
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	c.Set(string(middleware2.ContextKeyUserRole), service.RoleUser)
	return c
}

func TestStartClientDisconnectRiskLifecycleUsesServerClientRequestID(t *testing.T) {
	repo := &clientDisconnectRiskHandlerRepoStub{}
	risk := service.NewClientDisconnectRiskService(repo, nil, nil)

	first := newClientDisconnectRiskHandlerContext("attacker-reused-id", "server-request-1")
	second := newClientDisconnectRiskHandlerContext("attacker-reused-id", "server-request-2")
	startClientDisconnectRiskLifecycle(first, risk, 7, 11, "test").Accepted(first.Request.Context())
	startClientDisconnectRiskLifecycle(second, risk, 7, 11, "test").Accepted(second.Request.Context())

	require.Len(t, repo.begins, 2)
	require.Equal(t, "server-request-1", repo.begins[0].RequestID)
	require.Equal(t, "server-request-2", repo.begins[1].RequestID)
}

func TestStartClientDisconnectRiskLifecycleGeneratesTrustedFallbackID(t *testing.T) {
	repo := &clientDisconnectRiskHandlerRepoStub{}
	risk := service.NewClientDisconnectRiskService(repo, nil, nil)

	first := newClientDisconnectRiskHandlerContext("attacker-reused-id", "")
	second := newClientDisconnectRiskHandlerContext("attacker-reused-id", "")
	startClientDisconnectRiskLifecycle(first, risk, 7, 11, "test").Accepted(first.Request.Context())
	startClientDisconnectRiskLifecycle(second, risk, 7, 11, "test").Accepted(second.Request.Context())

	require.Len(t, repo.begins, 2)
	require.NotEqual(t, "attacker-reused-id", repo.begins[0].RequestID)
	require.NotEqual(t, repo.begins[0].RequestID, repo.begins[1].RequestID)
}
