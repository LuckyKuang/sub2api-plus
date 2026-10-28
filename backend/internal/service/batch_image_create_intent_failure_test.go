//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

// No provider execution starts unless its intent was durably recorded. A
// rejected create must return its task capacity and full balance hold at once;
// a request timeout cannot defer that financial cleanup to stale-job recovery.
func TestBatchImageCreateIntentFailureRestoresCapacityAndBalance(t *testing.T) {
	for _, test := range []struct {
		name          string
		cancelRequest bool
		cancelJob     bool
		releaseFails  bool
	}{
		{name: "intent storage unavailable"},
		{name: "request cancelled during intent write", cancelRequest: true},
		{name: "refund storage unavailable retains retry", releaseFails: true},
		{name: "concurrent cancellation retains terminal status", cancelJob: true},
		{name: "concurrent cancellation retains status through refund failure", cancelJob: true, releaseFails: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, repo, queue, provider, _ := newTestBatchImagePublicService(true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			taskRepo := &batchImageCreateIntentFailureRepo{unfinishedTaskRepoForTest: &unfinishedTaskRepoForTest{}}
			if test.cancelRequest {
				taskRepo.cancel = cancel
			}
			svc.UnfinishedTasks = NewGatewayUnfinishedTaskService(taskRepo, &config.Config{Gateway: config.GatewayConfig{MaxUnfinishedTasksPerUser: 1}})
			billing := &batchImageCreateIntentFailureBillingRepo{fakeBatchImageBillingRepo: svc.BillingRepo.(*fakeBatchImageBillingRepo)}
			if test.releaseFails {
				billing.releaseErr = errors.New("refund database unavailable")
			}
			svc.BillingRepo = billing
			if test.cancelJob {
				taskRepo.beforeError = func() {
					for _, job := range repo.jobs {
						_, cancelErr := svc.Cancel(context.Background(), testBatchImageOwner(), job.BatchID)
						if test.releaseFails {
							require.ErrorIs(t, cancelErr, ErrBatchImageCancelFailed)
						} else {
							require.NoError(t, cancelErr)
						}
						require.Equal(t, BatchImageJobStatusCancelled, job.Status)
					}
				}
			}
			got, err := svc.Submit(ctx, testBatchImageOwner(), validBatchImageSubmitRequest(), "")
			require.Nil(t, got)
			if test.releaseFails {
				require.ErrorIs(t, err, ErrBatchImageBillingHoldFailed)
			} else {
				require.ErrorIs(t, err, ErrGatewayUnfinishedTaskUnavailable, "an infrastructure write failure must not become a capacity-limit rejection")
			}
			require.Equal(t, 1, taskRepo.markCalls)
			require.Empty(t, provider.submits, "failed create intent must prevent every provider side effect")
			require.Len(t, billing.reserves, 1)
			refundAttempts := 1
			if test.cancelJob {
				refundAttempts++
			}
			require.Len(t, billing.releases, refundAttempts, "return the hold without waiting for stale recovery")
			require.Empty(t, billing.captures)
			require.Len(t, billing.releaseContextErrors, refundAttempts)
			for _, releaseContextError := range billing.releaseContextErrors {
				require.NoError(t, releaseContextError, "financial cleanup needs its own cancellation budget")
			}
			require.InDelta(t, 0.3, billing.releases[0].HoldAmount, 1e-12, "return the complete two-image hold")
			require.Empty(t, taskRepo.reservations)
			_, nextErr := svc.UnfinishedTasks.Reserve(context.Background(), GatewayTaskOwner{UserID: 11, APIKeyID: 23}, GatewayTaskKindSeedance)
			require.NoError(t, nextErr, "another API key must immediately regain the shared task capacity")
			require.Len(t, repo.jobs, 1)
			for _, job := range repo.jobs {
				if test.cancelJob {
					require.Equal(t, BatchImageJobStatusCancelled, job.Status, "failed create intent cannot overwrite a committed cancellation")
					require.Nil(t, job.UserDeletedAt, "a concurrent cancellation keeps its own visibility")
				} else {
					require.Equal(t, BatchImageJobStatusFailed, job.Status)
				}
				require.Nil(t, job.ProviderJobName)
				require.Equal(t, BatchImageReleaseRequestID(job.BatchID), billing.releases[0].RequestID)
				if test.releaseFails {
					if !test.cancelJob {
						require.Equal(t, "BILLING_RELEASE_FAILED", batchImageDerefString(job.LastErrorCode))
					}
					require.Contains(t, queue.enqueued, job.BatchID, "failed financial cleanup must retain its retry path")
					// Existing worker recovery is independent of provider submission
					// and idempotently returns the same original hold after recovery.
					billing.releaseErr = nil
					processor := &BatchImageProviderProcessor{Repo: repo, BillingRepo: billing}
					require.NoError(t, processor.releaseTerminalHold(context.Background(), job))
					require.Len(t, billing.releases, refundAttempts+1)
					require.Equal(t, billing.releases[0].RequestID, billing.releases[1].RequestID)
				} else if !test.cancelJob {
					require.Equal(t, "TASK_RESERVATION_FAILED", batchImageDerefString(job.LastErrorCode))
					require.NotNil(t, job.UserDeletedAt, "a rejected pre-provider submission must not remain visible as active work")
					require.Empty(t, queue.enqueued)
				}
			}
		})
	}
}

type batchImageCreateIntentFailureRepo struct {
	*unfinishedTaskRepoForTest
	cancel      context.CancelFunc
	beforeError func()
	markCalls   int
}

func (r *batchImageCreateIntentFailureRepo) MarkUncertain(context.Context, *GatewayTaskReservation, string) error {
	r.markCalls++
	if r.cancel != nil {
		r.cancel()
	}
	if r.beforeError != nil {
		r.beforeError()
	}
	return errors.New("create intent database unavailable")
}

type batchImageCreateIntentFailureBillingRepo struct {
	*fakeBatchImageBillingRepo
	releaseContextErrors []error
}

func (r *batchImageCreateIntentFailureBillingRepo) ReleaseBatchImageBalance(ctx context.Context, command *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	r.releaseContextErrors = append(r.releaseContextErrors, ctx.Err())
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.fakeBatchImageBillingRepo.ReleaseBatchImageBalance(ctx, command)
}
