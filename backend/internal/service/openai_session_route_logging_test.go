//go:build unit || !integration

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestOpenAISessionRouteFailureWarningsAreSafeAndIndexed(t *testing.T) {
	for _, fixture := range []struct {
		name, stage, code, reason string
		err                       error
	}{
		{"store", "selection", "route_store_unavailable", "route_store_unavailable", fmt.Errorf("%w: credential=secret-prompt", ErrOpenAISessionRouteStoreUnavailable)},
		{"busy", "selection", "lease_lost", "migration_in_progress", ErrOpenAISessionRouteLeaseLost},
		{"admission", "admission", "lease_lost", "lease_lost", ErrOpenAISessionRouteLeaseLost},
		{"acceptance", "forward", "candidate_not_accepted", "candidate_not_accepted", ErrOpenAISessionRouteNotAccepted},
		{"socket-owner", "owner_validation", "lease_lost", "lease_lost", ErrOpenAISessionRouteLeaseLost},
		{"cleanup", "abandon", "route_store_unavailable", "route_store_unavailable", ErrOpenAISessionRouteStoreUnavailable},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			core, logs := observer.New(zap.WarnLevel)
			ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "review-request")))
			route := OpenAISessionRoute{OwnerAccountID: 101, CandidateAccountID: 102, Generation: 7,
				SessionDigest: "private-session-digest", LeaseID: "private-lease", LeaseExpiresAt: time.Now().Add(time.Minute)}
			logOpenAISessionRouteFailure(ctx, fixture.err, fixture.stage, route, 102)
			entries := logs.All()
			require.Len(t, entries, 1)
			entry := entries[0]
			require.Equal(t, "openai_session_route_failed", entry.Message)
			fields := entry.ContextMap()
			require.Equal(t, "review-request", fields["request_id"])
			require.Equal(t, fixture.stage, fields["stage"])
			require.Equal(t, fixture.code, fields["error_code"])
			require.Equal(t, fixture.reason, fields["reason"])
			require.EqualValues(t, 101, fields["owner_account_id"])
			require.EqualValues(t, 102, fields["candidate_account_id"])
			require.EqualValues(t, 7, fields["generation"])
			remaining, ok := fields["lease_remaining_ms"].(int64)
			require.True(t, ok)
			require.Greater(t, remaining, int64(0))
			for _, secret := range []string{"secret-prompt", "private-session-digest", "private-lease"} {
				require.NotContains(t, fmt.Sprint(fields), secret)
			}
			sink := &OpsSystemLogSink{}
			require.True(t, sink.shouldIndex(&logger.LogEvent{Level: "warn", Message: entry.Message, Fields: fields}))
		})
	}
}

func TestOpenAISessionRouteFailureLoggingDoesNotIndexSuccessOrUpstreamFailures(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core))
	logOpenAISessionRouteFailure(ctx, nil, "forward", OpenAISessionRoute{}, 0)
	logOpenAISessionRouteFailure(ctx, &UpstreamFailoverError{StatusCode: 502}, "forward", OpenAISessionRoute{}, 0)
	require.Empty(t, logs.All(), "upstream failures must retain their existing attribution")
}
