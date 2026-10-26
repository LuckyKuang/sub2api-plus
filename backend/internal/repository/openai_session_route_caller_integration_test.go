//go:build integration

package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type affinityCallerAccounts struct {
	service.AccountRepository
	accounts []service.Account
}

func (r affinityCallerAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for _, a := range r.accounts {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, service.ErrAccountNotFound
}
func (r affinityCallerAccounts) ListSchedulableUngroupedByPlatform(context.Context, string) ([]service.Account, error) {
	return r.accounts, nil
}
func (r affinityCallerAccounts) ListSchedulableByPlatform(context.Context, string) ([]service.Account, error) {
	return r.accounts, nil
}

type affinityCallerWire struct {
	service.HTTPUpstream
	t      *testing.T
	store  service.OpenAISessionRouteStore
	digest string
	calls  atomic.Int32
}

func (w *affinityCallerWire) Do(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
	w.calls.Add(1)
	route, err := w.store.Get(req.Context(), "group:0", w.digest)
	if err != nil {
		return nil, err
	}
	if id != 102 || route.OwnerAccountID != 101 || req.Header.Get("Authorization") != "Bearer candidate" {
		w.t.Errorf("unauthorized wire: account=%d owner=%d", id, route.OwnerAccountID)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_b\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}, nil
}

// V05/V12/V15: separate gateway/store instances compete from the same observed
// generation. The real PostgreSQL lease must admit exactly one upstream write.
func TestOpenAISessionRouteConcurrentCallerTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	session := t.Name() + time.Now().Format("150405.000000000")
	digest := service.OpenAISessionRouteDigest(session)
	store := NewOpenAISessionRouteRepository(integrationDB, integrationRedis, time.Minute)
	_, err := store.CreateIfAbsent(ctx, service.OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: digest, OwnerAccountID: 101})
	require.NoError(t, err)
	defer func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM openai_session_routes WHERE authz_domain='group:0' AND session_digest=$1`, digest)
	}()
	wire := &affinityCallerWire{t: t, store: store, digest: digest}
	accounts := affinityCallerAccounts{accounts: []service.Account{
		{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusError},
		{ID: 102, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"api_key": "candidate"}, Extra: map[string]any{"responses_supported": true}},
	}}
	const attempts = 8
	var gateways [attempts]*service.OpenAIGatewayService
	var selections [attempts]*service.AccountSelectionResult
	for i := range attempts {
		cfg := &config.Config{}
		repo := NewOpenAISessionRouteRepository(integrationDB, integrationRedis, time.Minute)
		gateways[i] = service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, wire, nil, nil, nil, nil, nil, nil, nil, nil, repo)
		selections[i], _, err = gateways[i].SelectAccountWithScheduler(ctx, nil, "", session, "gpt-5.1", nil, service.OpenAIUpstreamTransportAny, false)
		require.NoError(t, err)
	}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	start := make(chan struct{})
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			selection := selections[i]
			defer selection.ReleaseFunc()
			attemptCtx := service.ContextWithSelectionProfitGate(ctx, selection)
			body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body))).WithContext(attemptCtx)
			_, forwardErr := gateways[i].Forward(attemptCtx, c, selection.Account, body)
			if forwardErr == nil {
				accepted.Add(1)
			} else if !service.IsOpenAISessionRouteError(forwardErr) {
				t.Errorf("unexpected attempt error: %v", forwardErr)
			}
		}()
	}
	close(start)
	wg.Wait()
	require.EqualValues(t, 1, wire.calls.Load())
	require.EqualValues(t, 1, accepted.Load())
	route, err := store.Get(ctx, "group:0", digest)
	require.NoError(t, err)
	require.EqualValues(t, 102, route.OwnerAccountID)
	require.EqualValues(t, 2, route.Generation)
}
