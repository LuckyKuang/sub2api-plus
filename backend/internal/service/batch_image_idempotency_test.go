//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The accepted identity is specified independently of validation and hashing:
// an omitted user label stays empty, while the job has its own display label.
const batchImageUnnamedAcceptedPayload = `{"model":"gemini-2.5-flash-image","task_name":"","parent_batch_id":"","provider":"gemini_api","items":[{"custom_id":"cover","prompt":"hero"}],"response_mime_type":"image/png","aspect_ratio":"1:1","image_size":"1K","metadata":null}`

func batchImageUnnamedRequestFixture() BatchImageSubmitRequest {
	return BatchImageSubmitRequest{
		Model: "gemini-2.5-flash-image", Provider: BatchImageProviderGeminiAPI,
		Items:            []BatchImageSubmitItem{{CustomID: "cover", Prompt: "hero"}},
		ResponseMimeType: "image/png", AspectRatio: "1:1", ImageSize: "1K",
	}
}

func TestBatchImageSubmitNormalizationPreservesEmptyTaskName(t *testing.T) {
	for _, taskName := range []string{"", " \t "} {
		svc, _, _, _, _ := newTestBatchImagePublicService(true)
		req := batchImageUnnamedRequestFixture()
		req.TaskName = taskName
		normalized, err := svc.validateSubmitRequest(req)
		require.NoError(t, err)
		require.Empty(t, normalized.TaskName, "a server-generated display time must not become request identity")
		payload, err := json.Marshal(normalized)
		require.NoError(t, err)
		require.JSONEq(t, batchImageUnnamedAcceptedPayload, string(payload))
		acceptedDigest := sha256.Sum256([]byte(batchImageUnnamedAcceptedPayload))
		require.Equal(t, hex.EncodeToString(acceptedDigest[:]), HashBatchImageSubmitRequest(normalized))
	}
}

func TestBatchImageSubmitIdempotencyReplaysAcceptedUnnamedPayload(t *testing.T) {
	for _, taskName := range []string{"", " \t "} {
		svc, repo, queue, gemini, vertex := newTestBatchImagePublicService(true)
		acceptedDigest := sha256.Sum256([]byte(batchImageUnnamedAcceptedPayload))
		acceptedHash := hex.EncodeToString(acceptedDigest[:])
		// An already completed job was accepted on a different day. No sleep or
		// current-clock assumption can hide a generated-label fingerprint leak.
		apiKeyID := int64(22)
		job := &BatchImageJob{
			BatchID: "accepted-unnamed-batch", UserID: 11, APIKeyID: &apiKeyID,
			Status: BatchImageJobStatusCompleted, Provider: BatchImageProviderGeminiAPI,
			Model: "gemini-2.5-flash-image", TaskName: "2020-01-02 03:04:05", ItemCount: 1,
			CreatedAt:      time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
			IdempotencyKey: batchImageStringPtr("accepted-key"), RequestHash: batchImageStringPtr(acceptedHash),
			SessionID: batchImageStringPtr("accepted-session"),
		}
		repo.jobs[job.BatchID] = job
		req := batchImageUnnamedRequestFixture()
		req.TaskName = taskName
		req.SessionID = batchImageStringPtr("retry-session")

		replayed, err := svc.Submit(context.Background(), testBatchImageOwner(), req, "accepted-key")

		require.NoError(t, err, "an identical unnamed retry must reuse its accepted batch across clock boundaries")
		require.Equal(t, job.BatchID, replayed.ID)
		require.Equal(t, "2020-01-02 03:04:05", replayed.TaskName)
		require.Equal(t, "accepted-session", batchImageDerefString(job.SessionID))
		require.Equal(t, acceptedHash, batchImageDerefString(job.RequestHash), "replay must not rewrite the accepted billing fingerprint")
		require.Len(t, repo.jobs, 1)
		require.Empty(t, repo.items)
		require.Empty(t, gemini.submits)
		require.Empty(t, vertex.submits)
		require.Empty(t, queue.enqueued)
		billing := svc.BillingRepo.(*fakeBatchImageBillingRepo)
		require.Empty(t, billing.reserves)
		require.Empty(t, billing.releases)
		require.Empty(t, svc.AuthCache.(*fakeBatchImageAuthCacheInvalidator).userIDs)
	}
}

func TestBatchImageSubmitIdempotencyKeepsExplicitTaskNameInIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, firstName, retryName string
		copyGeneratedLabel         bool
		conflict                   bool
	}{
		{name: "same explicit name", firstName: "Campaign Alpha", retryName: "Campaign Alpha"},
		{name: "equivalent trimmed explicit name", firstName: " Campaign Alpha ", retryName: "Campaign Alpha"},
		{name: "changed explicit name", firstName: "Campaign Alpha", retryName: "Campaign Beta", conflict: true},
		{name: "removed explicit name", firstName: "Campaign Alpha", conflict: true},
		{name: "blanked explicit name", firstName: "Campaign Alpha", retryName: " \t ", conflict: true},
		{name: "generated display label becomes explicit", copyGeneratedLabel: true, conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, queue, gemini, vertex := newTestBatchImagePublicService(true)
			initial := batchImageUnnamedRequestFixture()
			initial.TaskName = tc.firstName
			first, err := svc.Submit(context.Background(), testBatchImageOwner(), initial, "named-key")
			require.NoError(t, err)
			require.NotEmpty(t, first.TaskName, "new unnamed jobs must still store their generated display label")
			require.Equal(t, first.TaskName, repo.jobs[first.ID].TaskName)
			if tc.firstName == "" {
				_, err := time.Parse("2006-01-02 15:04:05", first.TaskName)
				require.NoError(t, err)
			}
			retry := batchImageUnnamedRequestFixture()
			retry.TaskName = tc.retryName
			if tc.copyGeneratedLabel {
				retry.TaskName = first.TaskName
			}

			second, err := svc.Submit(context.Background(), testBatchImageOwner(), retry, "named-key")

			if tc.conflict {
				require.ErrorIs(t, err, ErrBatchImageIdempotencyConflict, "a changed user-provided label must remain a different payload")
				require.Nil(t, second)
			} else {
				require.NoError(t, err)
				require.Equal(t, first.ID, second.ID)
			}
			require.Len(t, repo.jobs, 1)
			require.Len(t, gemini.submits, 1)
			require.Empty(t, vertex.submits)
			require.Len(t, repo.items[first.ID], 1)
			require.Equal(t, []string{first.ID}, queue.enqueued)
			billing := svc.BillingRepo.(*fakeBatchImageBillingRepo)
			require.Len(t, billing.reserves, 1)
			require.Empty(t, billing.releases)
		})
	}
}
