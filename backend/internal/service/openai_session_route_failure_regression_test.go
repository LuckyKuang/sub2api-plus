//go:build unit || !integration

package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type affinityFaultStore struct {
	*fakeOpenAISessionRouteStore
	stage string
}

type affinityContextTransport struct{ *httpUpstreamRecorder }

func (w affinityContextTransport) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return w.httpUpstreamRecorder.Do(req, proxy, accountID, concurrency)
}

func (s *affinityFaultStore) CreateIfAbsent(ctx context.Context, route OpenAISessionRoute) (OpenAISessionRoute, error) {
	if s.stage == "create" {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteStoreUnavailable
	}
	if s.stage == "winner" {
		route.OwnerAccountID = 103
	}
	return s.fakeOpenAISessionRouteStore.CreateIfAbsent(ctx, route)
}
func (s *affinityFaultStore) BeginMigration(ctx context.Context, domain, digest string, generation, candidate int64, lease, reason string, ttl time.Duration) (OpenAISessionRoute, error) {
	if s.stage == "begin" {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteStoreUnavailable
	}
	return s.fakeOpenAISessionRouteStore.BeginMigration(ctx, domain, digest, generation, candidate, lease, reason, ttl)
}
func (s *affinityFaultStore) CommitOwner(ctx context.Context, domain, digest, lease string, candidate int64) (OpenAISessionRoute, error) {
	if s.stage == "commit" {
		return OpenAISessionRoute{}, ErrOpenAISessionRouteStoreUnavailable
	}
	return s.fakeOpenAISessionRouteStore.CommitOwner(ctx, domain, digest, lease, candidate)
}

type affinityCommitWriter struct {
	gin.ResponseWriter
	t      *testing.T
	store  OpenAISessionRouteStore
	writes int
}

func (w *affinityCommitWriter) check(data string) {
	if !strings.Contains(data, "response.completed") && !strings.Contains(data, "output_text") {
		return
	}
	route, err := w.store.Get(context.Background(), "group:0", OpenAISessionRouteDigest("session"))
	require.NoError(w.t, err)
	require.EqualValues(w.t, 102, route.OwnerAccountID, "SQL acceptance must precede downstream semantic bytes")
	w.writes++
}
func (w *affinityCommitWriter) Write(data []byte) (int, error) {
	w.check(string(data))
	return w.ResponseWriter.Write(data)
}
func (w *affinityCommitWriter) WriteString(data string) (int, error) {
	w.check(data)
	return w.ResponseWriter.WriteString(data)
}

// V04/V05/V12/V14: faults are exercised through selection/admission/Forward,
// with real built HTTP requests and downstream bytes as the side-effect oracle.
func TestCodexRouteFaultsFenceTransportRegression(t *testing.T) {
	for _, stage := range []string{"create", "begin", "commit", "accepted"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			store := &affinityFaultStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore(), stage: stage}
			if stage != "create" {
				_, err := store.fakeOpenAISessionRouteStore.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
				require.NoError(t, err)
			}
			upstream := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
			svc := newOpenAIRejectedFieldTestService(upstream)
			svc.httpUpstream = affinityContextTransport{upstream}
			a := newOpenAIRejectedFieldTestAccount()
			a.ID = 102
			a.Status = StatusActive
			a.Schedulable = true
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError}, *a}}
			svc.cache = &schedulerTestGatewayCache{}
			svc.sessionRouteStore = store
			var releases []int64
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{releasedIDs: &releases})
			selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-6.1-sol", nil, OpenAIUpstreamTransportAny, false)
			if stage == "create" {
				require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
				require.Empty(t, upstream.requests)
				require.Equal(t, []int64{102}, releases)
				return
			}
			require.NoError(t, err)
			defer selection.ReleaseFunc()
			ctx = ContextWithSelectionProfitGate(ctx, selection)
			body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"hello"}`)
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request = c.Request.WithContext(ctx)
			writer := &affinityCommitWriter{ResponseWriter: c.Writer, t: t, store: store}
			c.Writer = writer
			result, err := svc.Forward(ctx, c, selection.Account, body)
			if stage == "accepted" {
				require.NoError(t, err)
				require.Positive(t, writer.writes)
				return
			}
			require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
			require.Zero(t, writer.writes)
			if stage == "begin" {
				require.Empty(t, upstream.requests)
			} else {
				require.Len(t, upstream.requests, 1)
				require.NotNil(t, result)
				require.Positive(t, result.Usage.InputTokens)
			}
			route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
			require.NoError(t, err)
			require.EqualValues(t, 101, route.OwnerAccountID)
		})
	}
}

func TestCodexFirstCreationUsesWinnerRegression(t *testing.T) {
	store := &affinityFaultStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore(), stage: "winner"}
	var released []int64
	svc := &OpenAIGatewayService{sessionRouteStore: store, cache: &schedulerTestGatewayCache{}, accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{
		{ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0},
		{ID: 103, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 100},
	}}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{releasedIDs: &released})}
	selection, _, err := svc.SelectAccountWithScheduler(context.Background(), nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.EqualValues(t, 103, selection.Account.ID)
	require.Equal(t, []int64{102}, released, "losing first candidate must release its slot")
	selection.ReleaseFunc()
}

// V14/V26: cancellation/expiry cannot resurrect a lease; a bounded acceptance
// deadline also cancels requests that otherwise detach for usage draining.
func TestCodexMigrationDeadlineCancelsDetachedTransportRegression(t *testing.T) {
	store := newFakeOpenAISessionRouteStore()
	route, err := store.CreateIfAbsent(context.Background(), OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: "digest", OwnerAccountID: 101})
	require.NoError(t, err)
	a := &openAISessionRouteAttempt{store: store, route: route, accountID: 102}
	ctx := context.WithValue(context.Background(), openAISessionRouteAttemptKey{}, a)
	ctx, finish, err := beginOpenAISessionRouteForward(ctx, &Account{ID: 102}, 10*time.Millisecond)
	require.NoError(t, err)
	defer finish()
	detached, release := detachUpstreamContext(ctx)
	defer release()
	select {
	case <-detached.Done():
		require.ErrorIs(t, context.Cause(detached), ErrOpenAISessionRouteLeaseLost)
	case <-time.After(time.Second):
		t.Fatal("lease loss did not cancel detached transport")
	}
	require.ErrorIs(t, commitOpenAISessionTakeover(ctx), ErrOpenAISessionRouteLeaseLost)
}

// Releasing a pending attempt can race with request cancellation. It must
// cancel detached candidate I/O immediately even after the route is abandoned.
func TestCodexAbandonedMigrationCancelsDetachedTransportRegression(t *testing.T) {
	store := newFakeOpenAISessionRouteStore()
	route, err := store.CreateIfAbsent(context.Background(), OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: "digest", OwnerAccountID: 101})
	require.NoError(t, err)
	a := &openAISessionRouteAttempt{store: store, route: route, accountID: 102}
	ctx := context.WithValue(context.Background(), openAISessionRouteAttemptKey{}, a)
	ctx, finish, err := beginOpenAISessionRouteForward(ctx, &Account{ID: 102}, time.Minute)
	require.NoError(t, err)
	defer finish()
	detached, release := detachUpstreamContext(ctx)
	release()
	require.NoError(t, detached.Err(), "building the request must not cancel it")
	a.close()
	select {
	case <-detached.Done():
		require.ErrorIs(t, context.Cause(detached), ErrOpenAISessionRouteLeaseLost)
	case <-time.After(time.Second):
		t.Fatal("abandonment did not cancel detached candidate I/O")
	}
	route, err = store.Get(context.Background(), "group:0", "digest")
	require.NoError(t, err)
	require.EqualValues(t, 101, route.OwnerAccountID)
	require.Empty(t, route.LeaseID)
}
