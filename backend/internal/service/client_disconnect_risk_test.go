//go:build unit

package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type clientDisconnectRiskRepoStub struct {
	mu           sync.Mutex
	begins       []ClientDisconnectRiskBegin
	finalizes    []ClientDisconnectRiskFinalize
	sequence     int64
	beginGate    <-chan struct{}
	beginStarted chan<- struct{}
	finalResult  ClientDisconnectRiskResult
	beginErr     error
	finalizeErr  error
}

func (r *clientDisconnectRiskRepoStub) Begin(_ context.Context, input ClientDisconnectRiskBegin) (int64, error) {
	if r.beginStarted != nil {
		r.beginStarted <- struct{}{}
	}
	if r.beginGate != nil {
		<-r.beginGate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beginErr != nil {
		return 0, r.beginErr
	}
	r.sequence++
	r.begins = append(r.begins, input)
	return r.sequence, nil
}

func TestClientDisconnectRiskLifecycle_FinalizeWaitsForAcceptedSequence(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	repo := &clientDisconnectRiskRepoStub{beginGate: gate, beginStarted: started}
	service := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := service.NewLifecycle(7, 11, RoleUser, "request-concurrent", "test")

	acceptedDone := make(chan struct{})
	go func() {
		lifecycle.Accepted(context.Background())
		close(acceptedDone)
	}()
	<-started
	finalizedDone := make(chan struct{})
	go func() {
		lifecycle.Disconnected(context.Background())
		close(finalizedDone)
	}()
	close(gate)
	<-acceptedDone
	<-finalizedDone

	require.Len(t, repo.begins, 1)
	require.Len(t, repo.finalizes, 1)
	require.Positive(t, repo.finalizes[0].Sequence)
}

func (r *clientDisconnectRiskRepoStub) Finalize(_ context.Context, input ClientDisconnectRiskFinalize) (ClientDisconnectRiskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalizes = append(r.finalizes, input)
	if r.finalizeErr != nil {
		return ClientDisconnectRiskResult{}, r.finalizeErr
	}
	return r.finalResult, nil
}

func (r *clientDisconnectRiskRepoStub) ClearUser(context.Context, int64) error { return nil }
func (r *clientDisconnectRiskRepoStub) ListEvents(context.Context, ClientDisconnectRiskEventFilter) ([]ClientDisconnectRiskEvent, int64, error) {
	return nil, 0, nil
}

func TestClientDisconnectRiskLifecycle_DeduplicatesFinalOutcome(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	service := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := service.NewLifecycle(7, 11, RoleUser, "request-1", "test")
	require.NotNil(t, lifecycle)

	lifecycle.Accepted(context.Background())
	lifecycle.Disconnected(context.Background())
	lifecycle.Completed(context.Background())
	lifecycle.Neutral(context.Background())

	require.Len(t, repo.begins, 1)
	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
	require.Equal(t, 10, repo.finalizes[0].Threshold)
	require.True(t, repo.finalizes[0].Enforce)
}

func TestClientDisconnectRiskLifecycle_RetriesBeginDuringFinalization(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{beginErr: errors.New("temporary begin failure")}
	service := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := service.NewLifecycle(7, 11, RoleUser, "request-retry-begin", "test")

	lifecycle.Accepted(context.Background())
	repo.beginErr = nil
	lifecycle.Disconnected(context.Background())

	require.Len(t, repo.begins, 1)
	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
}

func TestClientDisconnectRiskLifecycle_RetriesOriginalFinalOutcome(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{finalizeErr: errors.New("temporary finalize failure")}
	service := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := service.NewLifecycle(7, 11, RoleUser, "request-retry-finalize", "test")

	lifecycle.Accepted(context.Background())
	lifecycle.Disconnected(context.Background())
	repo.finalizeErr = nil
	lifecycle.Neutral(context.Background())

	require.Len(t, repo.finalizes, 2)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[1].Outcome)
}

func TestClientDisconnectRiskLifecycle_AdministratorIsAuditedButNotEnforced(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	service := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := service.NewLifecycle(7, 11, RoleAdmin, "request-1", "test")
	require.NotNil(t, lifecycle)
	lifecycle.Accepted(context.Background())
	lifecycle.Disconnected(context.Background())
	require.Len(t, repo.begins, 1)
	require.Len(t, repo.finalizes, 1)
	require.False(t, repo.finalizes[0].Enforce)
}

type clientDisconnectRiskAuthInvalidatorStub struct {
	mu            sync.Mutex
	userIDs       []int64
	contextErrors []error
}

func (s *clientDisconnectRiskAuthInvalidatorStub) InvalidateAuthCacheByKey(context.Context, string) {}
func (s *clientDisconnectRiskAuthInvalidatorStub) InvalidateAuthCacheByGroupID(context.Context, int64) {
}
func (s *clientDisconnectRiskAuthInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userIDs = append(s.userIDs, userID)
	s.contextErrors = append(s.contextErrors, ctx.Err())
}

func TestClientDisconnectRiskLifecycle_AutoBanInvalidatesUserAuthCache(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{finalResult: ClientDisconnectRiskResult{
		ConsecutiveCount: 10,
		AutoBanned:       true,
	}}
	invalidator := &clientDisconnectRiskAuthInvalidatorStub{}
	service := NewClientDisconnectRiskService(repo, nil, invalidator)
	lifecycle := service.NewLifecycle(7, 11, RoleUser, "request-ban", "test")

	lifecycle.Accepted(context.Background())
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	lifecycle.Disconnected(canceledCtx)

	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Equal(t, []error{nil}, invalidator.contextErrors)
}

func TestFinalizeClientDisconnectForwardResult_IncompleteResultDoesNotResetStreak(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	riskService := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := riskService.NewLifecycle(7, 11, RoleUser, "request-incomplete", "test")
	ctx := WithClientDisconnectLifecycle(context.Background(), lifecycle)
	lifecycle.Accepted(ctx)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	finalizeClientDisconnectForwardResult(ctx, c, &OpenAIForwardResult{
		Stream:                true,
		OpenAIWSMode:          true,
		UpstreamTerminalEvent: "response.incomplete",
	}, nil)
	lifecycle.Neutral(ctx)

	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeNeutral, repo.finalizes[0].Outcome)
}

func TestFinalizeClientDisconnectForwardResult_DisconnectWithResultMarksPartialUsage(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	riskService := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := riskService.NewLifecycle(7, 11, RoleUser, "request-partial", "test")
	ctx := WithClientDisconnectLifecycle(context.Background(), lifecycle)
	lifecycle.Accepted(ctx)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	finalizeClientDisconnectForwardResult(ctx, c, &OpenAIForwardResult{ClientDisconnect: true}, context.Canceled)

	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
	require.Equal(t, "client_disconnected", repo.finalizes[0].CompletionStatus)
	require.Equal(t, "partial", repo.finalizes[0].UsageSource)
	require.False(t, repo.finalizes[0].UsageMissing)
}

func TestFinalizeClientDisconnectForwardResult_NonStreamCancellationMarksExactUsageDisconnect(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	riskService := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := riskService.NewLifecycle(7, 11, RoleUser, "request-non-stream", "test")
	ctx := WithClientDisconnectLifecycle(context.Background(), lifecycle)
	lifecycle.Accepted(ctx)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(requestCtx)
	cancel()
	result := &OpenAIForwardResult{Stream: false}
	finalizeClientDisconnectForwardResult(ctx, c, result, nil)

	require.True(t, result.ClientDisconnect)
	require.Equal(t, UsageSourceUpstreamExact, result.ClientDisconnectUsageSource)
	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
	require.Equal(t, UsageSourceUpstreamExact, repo.finalizes[0].UsageSource)
	require.False(t, repo.finalizes[0].UsageMissing)
}

func TestFinalizeClientDisconnectForwardResult_StreamCompletionIgnoresLateContextCancellation(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	riskService := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := riskService.NewLifecycle(7, 11, RoleUser, "request-stream-complete", "test")
	ctx := WithClientDisconnectLifecycle(context.Background(), lifecycle)
	lifecycle.Accepted(ctx)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestCtx, cancel := context.WithCancel(context.Background())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(requestCtx)
	cancel()
	result := &OpenAIForwardResult{Stream: true}
	finalizeClientDisconnectForwardResult(ctx, c, result, nil)

	require.False(t, result.ClientDisconnect)
	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeCompleted, repo.finalizes[0].Outcome)
}

func TestFinalizeUnbilledClientDisconnectRequest_CancellationWinsOverSuccess(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	riskService := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := riskService.NewLifecycle(7, 11, RoleUser, "request-count-tokens", "count_tokens")
	requestCtx, cancel := context.WithCancel(WithClientDisconnectLifecycle(context.Background(), lifecycle))
	lifecycle.Accepted(requestCtx)
	cancel()

	finalizeUnbilledClientDisconnectRequest(requestCtx, true)
	lifecycle.Neutral(requestCtx)

	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeDisconnected, repo.finalizes[0].Outcome)
	require.True(t, repo.finalizes[0].UsageMissing)
}

func TestFinalizeUnbilledClientDisconnectRequest_CompletedResetsStreak(t *testing.T) {
	repo := &clientDisconnectRiskRepoStub{}
	riskService := NewClientDisconnectRiskService(repo, nil, nil)
	lifecycle := riskService.NewLifecycle(7, 11, RoleUser, "request-count-tokens", "count_tokens")
	requestCtx := WithClientDisconnectLifecycle(context.Background(), lifecycle)
	lifecycle.Accepted(requestCtx)

	finalizeUnbilledClientDisconnectRequest(requestCtx, true)
	lifecycle.Neutral(requestCtx)

	require.Len(t, repo.finalizes, 1)
	require.Equal(t, ClientDisconnectOutcomeCompleted, repo.finalizes[0].Outcome)
}
