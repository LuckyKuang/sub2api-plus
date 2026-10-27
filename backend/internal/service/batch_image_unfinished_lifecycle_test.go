//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// Provider completion, result indexing and billing are distinct business
// lifecycles. A user may submit the next task once provider execution finishes,
// even when its output cannot yet be downloaded or its charge needs a retry.
func TestBatchImageProviderCompletionFreesUnfinishedCapacityBeforeIndexRetry(t *testing.T) {
	for _, initialStatus := range []string{BatchImageJobStatusSubmitted, BatchImageJobStatusIndexing} {
		t.Run(initialStatus, func(t *testing.T) {
			ctx := context.Background()
			repo, taskRepo, tasks, job := newBatchImageUnfinishedLifecycleFixture(t, initialStatus)
			provider := &unfinishedLifecycleBatchImageProvider{
				fakeProcessorProvider: &fakeProcessorProvider{
					status: &BatchProviderStatus{InternalState: BatchProviderStateSucceeded, RawState: "SUCCEEDED", ProviderOutputRef: "files/output"},
					result: `{"key":"image_001","response":{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + batchImageTestData + `"}}]}}]}}` + "\n",
				},
				openErr: errors.New("provider output temporarily unavailable"),
			}
			billing := &fakeBatchImageBillingRepo{}
			pipeline := newBatchImageUnfinishedLifecyclePipeline(repo, tasks, provider, billing)

			_, err := pipeline.Process(ctx, job.BatchID)
			require.ErrorIs(t, err, ErrBatchImageIndexOutputMissing)
			require.Equal(t, BatchImageJobStatusIndexing, repo.jobs[job.BatchID].Status)
			require.Equal(t, initialStatus != BatchImageJobStatusIndexing, provider.getCalled)
			require.True(t, provider.openResultCalled)
			require.Empty(t, billing.captures, "missing output cannot create a charge")
			require.Empty(t, billing.releases, "output retry must retain the balance hold")

			// This is an actual capacity admission, not an assertion that a release
			// method happened to be called. A different key shares the same cap.
			next := reserveNextBatchImageAfterProviderCompletion(t, tasks, job)
			require.Len(t, taskRepo.reservations, 1)
			require.Contains(t, taskRepo.reservations, next.ID)

			// Reconstruct the service to model a worker restart. The durable
			// indexing status is sufficient evidence; no provider poll is needed.
			provider.openErr = nil
			provider.getCalled = false
			tasks = newBatchImageUnfinishedLifecycleTaskService(taskRepo)
			pipeline = newBatchImageUnfinishedLifecyclePipeline(repo, tasks, provider, billing)
			result, err := pipeline.Process(ctx, job.BatchID)
			require.NoError(t, err)
			require.False(t, result.Terminal)
			require.False(t, provider.getCalled)
			require.Equal(t, BatchImageJobStatusSettling, repo.jobs[job.BatchID].Status)
			result, err = pipeline.Process(ctx, job.BatchID)
			require.NoError(t, err)
			require.True(t, result.Terminal)
			require.Equal(t, BatchImageJobStatusCompleted, repo.jobs[job.BatchID].Status)
			require.Len(t, billing.captures, 1)
			require.Equal(t, 0.25, billing.captures[0].ActualAmount)
			require.Len(t, taskRepo.reservations, 1, "old task retries must not release a newer task")
			require.Contains(t, taskRepo.reservations, next.ID)
		})
	}
}

func TestBatchImageProviderCompletionFreesUnfinishedCapacityBeforeBillingRetry(t *testing.T) {
	for _, initialStatus := range []string{BatchImageJobStatusSubmitted, BatchImageJobStatusSettling} {
		t.Run(initialStatus, func(t *testing.T) {
			ctx := context.Background()
			repo, taskRepo, tasks, job := newBatchImageUnfinishedLifecycleFixture(t, initialStatus)
			provider := &unfinishedLifecycleBatchImageProvider{fakeProcessorProvider: &fakeProcessorProvider{
				status: &BatchProviderStatus{InternalState: BatchProviderStateSucceeded, RawState: "SUCCEEDED", ProviderOutputRef: "files/output"},
				result: `{"key":"image_001","response":{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"` + batchImageTestData + `"}}]}}]}}` + "\n",
			}}
			billing := &fakeBatchImageBillingRepo{captureErr: errors.New("billing database temporarily unavailable")}
			pipeline := newBatchImageUnfinishedLifecyclePipeline(repo, tasks, provider, billing)
			if initialStatus == BatchImageJobStatusSubmitted {
				result, err := pipeline.Process(ctx, job.BatchID)
				require.NoError(t, err)
				require.False(t, result.Terminal)
				require.Equal(t, BatchImageJobStatusSettling, repo.jobs[job.BatchID].Status)
			}

			result, err := pipeline.Process(ctx, job.BatchID)
			require.NoError(t, err)
			require.False(t, result.Terminal)
			require.Equal(t, batchImageSettlementRetryDelay, result.RequeueAfter)
			require.Equal(t, BatchImageJobStatusSettling, repo.jobs[job.BatchID].Status)
			require.Equal(t, "SETTLEMENT_BILLING_FAILED", batchImageDerefString(repo.jobs[job.BatchID].LastErrorCode))
			require.Len(t, billing.captures, 1)
			require.Empty(t, billing.seen, "failed billing must not apply a charge")
			require.Empty(t, billing.releases, "billing retry must retain the balance hold")
			if initialStatus == BatchImageJobStatusSettling {
				require.False(t, provider.getCalled, "restart settlement must not depend on a fresh provider request")
				require.False(t, provider.openResultCalled)
			}

			next := reserveNextBatchImageAfterProviderCompletion(t, tasks, job)
			require.Len(t, taskRepo.reservations, 1)
			require.Contains(t, taskRepo.reservations, next.ID)

			// A fresh worker retries the financial operation independently of
			// task admission. Completion and repeat processing charge only once.
			billing.captureErr = nil
			tasks = newBatchImageUnfinishedLifecycleTaskService(taskRepo)
			pipeline = newBatchImageUnfinishedLifecyclePipeline(repo, tasks, provider, billing)
			result, err = pipeline.Process(ctx, job.BatchID)
			require.NoError(t, err)
			require.True(t, result.Terminal)
			require.Equal(t, BatchImageJobStatusCompleted, repo.jobs[job.BatchID].Status)
			require.Len(t, billing.captures, 2, "one failed attempt and one successful capture")
			require.Equal(t, 0.25, billing.captures[1].ActualAmount)
			require.Len(t, billing.seen, 1)
			require.Contains(t, billing.seen, BatchImageCaptureRequestID(job.BatchID))
			result, err = pipeline.Process(ctx, job.BatchID)
			require.NoError(t, err)
			require.True(t, result.Terminal)
			require.Len(t, billing.captures, 2)
			require.Len(t, taskRepo.reservations, 1, "settlement retry cannot release another key's task")
			require.Contains(t, taskRepo.reservations, next.ID)
			_, err = tasks.Reserve(ctx, next.Owner, GatewayTaskKindBatchImage)
			require.ErrorIs(t, err, ErrGatewayUnfinishedTaskLimit)
		})
	}
}

func TestBatchImageUnconfirmedProviderStateKeepsUnfinishedCapacity(t *testing.T) {
	for _, test := range []struct {
		name   string
		status *BatchProviderStatus
		err    error
	}{
		{name: "queued", status: &BatchProviderStatus{InternalState: BatchProviderStateQueued}},
		{name: "running", status: &BatchProviderStatus{InternalState: BatchProviderStateRunning}},
		{name: "status unavailable", err: errors.New("provider status timeout")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, taskRepo, tasks, job := newBatchImageUnfinishedLifecycleFixture(t, BatchImageJobStatusSubmitted)
			provider := &unfinishedLifecycleBatchImageProvider{fakeProcessorProvider: &fakeProcessorProvider{status: test.status, getErr: test.err}}
			billing := &fakeBatchImageBillingRepo{}
			result, err := newBatchImageUnfinishedLifecyclePipeline(repo, tasks, provider, billing).Process(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.False(t, result.Terminal)
			require.False(t, provider.openResultCalled)
			require.Empty(t, billing.captures)
			require.Len(t, taskRepo.reservations, 1)
			_, err = tasks.Reserve(context.Background(), GatewayTaskOwner{UserID: job.UserID, APIKeyID: *job.APIKeyID + 1}, GatewayTaskKindBatchImage)
			require.ErrorIs(t, err, ErrGatewayUnfinishedTaskLimit, "waiting, running and unknown outcomes still consume capacity")
		})
	}
}

func TestBatchImageProviderObservationCapacitySurvivesOutputRefWriteFailure(t *testing.T) {
	for _, test := range []struct {
		state    BatchProviderInternalState
		terminal bool
	}{
		{state: BatchProviderStateSucceeded, terminal: true},
		{state: BatchProviderStateFailed, terminal: true},
		{state: BatchProviderStateCancelled, terminal: true},
		{state: BatchProviderStateExpired, terminal: true},
		{state: BatchProviderStateQueued},
		{state: BatchProviderStateRunning},
		{state: BatchProviderInternalState("unknown")},
	} {
		t.Run(string(test.state), func(t *testing.T) {
			ctx := context.Background()
			repo, taskRepo, tasks, job := newBatchImageUnfinishedLifecycleFixture(t, BatchImageJobStatusSubmitted)
			writeErr := errors.New("output reference write failed")
			failingRepo := &unfinishedLifecycleOutputRefFailureRepo{fakeBatchImageRepository: repo, err: writeErr}
			provider := &unfinishedLifecycleBatchImageProvider{fakeProcessorProvider: &fakeProcessorProvider{
				status: &BatchProviderStatus{InternalState: test.state, ProviderOutputRef: "files/new-output"},
			}}
			billing := &fakeBatchImageBillingRepo{}
			pipeline := newBatchImageUnfinishedLifecyclePipeline(repo, tasks, provider, billing)
			pipeline.ProviderProcessor.Repo = failingRepo
			pipeline.SettlementService.Repo = failingRepo

			_, err := pipeline.Process(ctx, job.BatchID)
			require.ErrorIs(t, err, writeErr, "storage failure still returns for the worker to retry")
			require.Equal(t, 1, failingRepo.calls)
			require.True(t, provider.getCalled)
			require.False(t, provider.openResultCalled, "failed reference persistence cannot start output retrieval")
			require.Equal(t, BatchImageJobStatusSubmitted, repo.jobs[job.BatchID].Status)
			require.Empty(t, repo.transitions[job.BatchID])
			require.Empty(t, billing.captures)
			require.Empty(t, billing.releases, "task capacity and balance holds have separate recovery")

			if test.terminal {
				next := reserveNextBatchImageAfterProviderCompletion(t, tasks, job)
				require.Len(t, taskRepo.reservations, 1)
				require.Contains(t, taskRepo.reservations, next.ID)
			} else {
				_, err = tasks.Reserve(ctx, GatewayTaskOwner{UserID: job.UserID, APIKeyID: *job.APIKeyID + 1}, GatewayTaskKindBatchImage)
				require.ErrorIs(t, err, ErrGatewayUnfinishedTaskLimit, "a failed local write cannot prove queued, running or unknown work finished")
				require.Len(t, taskRepo.reservations, 1)
			}
		})
	}
}

func newBatchImageUnfinishedLifecycleFixture(t *testing.T, status string) (*fakeBatchImageRepository, *unfinishedTaskRepoForTest, *GatewayUnfinishedTaskService, *BatchImageJob) {
	t.Helper()
	ctx := context.Background()
	repo := newFakeBatchImageRepository()
	taskRepo := &unfinishedTaskRepoForTest{}
	tasks := newBatchImageUnfinishedLifecycleTaskService(taskRepo)
	job := testSettlingBatchImageJob("imgbatch_capacity_lifecycle")
	job.Provider, job.Status = "fake", status
	job.ItemCount, job.SuccessCount, job.FailCount = 1, 1, 0
	if status != BatchImageJobStatusSettling {
		job.SuccessCount = 0
	}
	repo.jobs[job.BatchID] = job
	owner := GatewayTaskOwner{UserID: job.UserID, APIKeyID: *job.APIKeyID}
	reservation, err := tasks.Reserve(ctx, owner, GatewayTaskKindBatchImage)
	require.NoError(t, err)
	require.NoError(t, tasks.MarkUncertain(ctx, reservation, "provider_create_started"))
	require.NoError(t, tasks.Bind(ctx, reservation, job.BatchID))
	_, err = tasks.Reserve(ctx, GatewayTaskOwner{UserID: job.UserID, APIKeyID: *job.APIKeyID + 1}, GatewayTaskKindBatchImage)
	require.ErrorIs(t, err, ErrGatewayUnfinishedTaskLimit, "the accepted task must consume the shared cap before completion")
	return repo, taskRepo, tasks, job
}

func newBatchImageUnfinishedLifecycleTaskService(repo *unfinishedTaskRepoForTest) *GatewayUnfinishedTaskService {
	return NewGatewayUnfinishedTaskService(repo, &config.Config{Gateway: config.GatewayConfig{MaxUnfinishedTasksPerUser: 1}})
}

func newBatchImageUnfinishedLifecyclePipeline(repo *fakeBatchImageRepository, tasks *GatewayUnfinishedTaskService, provider BatchImageProvider, billing *fakeBatchImageBillingRepo) *BatchImagePipelineProcessor {
	return &BatchImagePipelineProcessor{
		ProviderProcessor: &BatchImageProviderProcessor{
			UnfinishedTasks:  tasks,
			Concurrency:      NewConcurrencyService(&stubConcurrencyCacheForTest{acquireResult: true}),
			Repo:             repo,
			ProviderRegistry: NewBatchImageProviderRegistry(provider),
			AccountResolver:  &fakeBatchImageAccountResolver{account: &Account{}},
			BillingRepo:      billing,
		},
		SettlementService: &BatchImageSettlementService{
			UnfinishedTasks: tasks,
			Repo:            repo,
			BillingRepo:     billing,
			Pricing:         &fakeBatchImagePricingResolver{unitPrice: 0.25},
		},
	}
}

func reserveNextBatchImageAfterProviderCompletion(t *testing.T, tasks *GatewayUnfinishedTaskService, job *BatchImageJob) *GatewayTaskReservation {
	t.Helper()
	next, err := tasks.Reserve(context.Background(), GatewayTaskOwner{UserID: job.UserID, APIKeyID: *job.APIKeyID + 1}, GatewayTaskKindBatchImage)
	require.NoError(t, err, "confirmed provider completion must free unfinished capacity before result and billing retries finish")
	require.NotNil(t, next)
	require.NoError(t, tasks.Bind(context.Background(), next, "imgbatch_next_key_task"))
	return next
}

type unfinishedLifecycleBatchImageProvider struct {
	*fakeProcessorProvider
	openErr error
}

func (p *unfinishedLifecycleBatchImageProvider) OpenResult(ctx context.Context, job *BatchImageJob, account *Account) (io.ReadCloser, string, error) {
	p.openResultCalled = true
	if p.openErr != nil {
		return nil, "", p.openErr
	}
	return p.fakeProcessorProvider.OpenResult(ctx, job, account)
}

type unfinishedLifecycleOutputRefFailureRepo struct {
	*fakeBatchImageRepository
	err   error
	calls int
}

func (r *unfinishedLifecycleOutputRefFailureRepo) UpdateBatchImageJobProviderOutputRef(context.Context, string, string) error {
	r.calls++
	return r.err
}
