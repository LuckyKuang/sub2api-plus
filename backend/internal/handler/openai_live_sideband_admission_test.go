//go:build unit || !integration

package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/securityaudit"
	middleware "github.com/LuckyKuang/sub2api-plus/internal/server/middleware"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type liveSidebandAdmissionStore struct {
	service.GatewayCache
	service.LiveCallStore
	record  *service.LiveCallRecord
	lookups atomic.Int32
	claims  atomic.Int32
}

func (s *liveSidebandAdmissionStore) GetLiveCall(context.Context, string) (*service.LiveCallRecord, error) {
	s.lookups.Add(1)
	return s.record, nil
}

func (s *liveSidebandAdmissionStore) ClaimLiveController(context.Context, string, string, string) (bool, error) {
	s.claims.Add(1)
	// This fixture tests admission to an existing call. Declining the controller
	// claim keeps the provider transport/lifecycle in its dedicated service tests.
	return false, nil
}

type liveSidebandAdmissionAccounts struct {
	service.AccountRepository
	reads atomic.Int32
}

func (r *liveSidebandAdmissionAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	r.reads.Add(1)
	return nil, service.ErrAccountNotFound
}

type liveSidebandAdmissionAudit struct {
	mu       sync.Mutex
	kind     securityaudit.DecisionKind
	requests []securityaudit.Request
}

func (*liveSidebandAdmissionAudit) EffectiveMode() securityaudit.Mode {
	return securityaudit.ModeBlocking
}
func (*liveSidebandAdmissionAudit) Enqueue(context.Context, securityaudit.Request) error { return nil }
func (e *liveSidebandAdmissionAudit) Evaluate(_ context.Context, request securityaudit.Request) (*securityaudit.PromptDecision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.requests = append(e.requests, request.Clone())
	return &securityaudit.PromptDecision{Kind: e.kind, AllowNextStage: e.kind == securityaudit.DecisionAllow}, nil
}

func newLiveSidebandAdmissionFixture(t *testing.T, kind securityaudit.DecisionKind, rpm *gatewayRequestRPMCache) (*OpenAIGatewayHandler, *service.APIKey, *liveSidebandAdmissionStore, *liveSidebandAdmissionAccounts, *helperConcurrencyCacheStub, *liveSidebandAdmissionAudit) {
	t.Helper()
	groupID := int64(3)
	key := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, User: &service.User{ID: 7, RPMLimit: 1}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowLive: true, RPMLimit: 10}}
	hash := sha256.Sum256([]byte("call_admitted"))
	store := &liveSidebandAdmissionStore{record: &service.LiveCallRecord{CallID: "call_admitted", CallHash: hex.EncodeToString(hash[:]), AccountID: 11, APIKeyID: key.ID, UserID: key.UserID, GroupID: groupID, LeaseID: "existing-live-owner", Model: "gpt-live-test", Controller: service.LiveControllerObserver, ExpiresAt: time.Now().Add(time.Minute)}}
	accounts := &liveSidebandAdmissionAccounts{}
	slots := &helperConcurrencyCacheStub{}
	concurrency := service.NewConcurrencyService(slots)
	cfg := &config.Config{}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, rpm, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	gateway := service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, store, cfg, nil, concurrency, nil, nil, billing, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, nil, nil, nil, nil, nil, cfg)
	attachDisabledIPAccessControlForWebSocketTest(h)
	audit := &liveSidebandAdmissionAudit{kind: kind}
	h.securityAuditCoordinator = securityaudit.NewCoordinator(nil, audit)
	return h, key, store, accounts, slots, audit
}

func liveSidebandAdmissionRouter(h *OpenAIGatewayHandler, key *service.APIKey) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID, Concurrency: 1})
		c.Next()
	})
	router.GET("/v1/live/:call_id", h.LiveSideband)
	return router
}

func TestLiveSidebandIngressRejectsAuditOrRPMBeforeMappingAndController(t *testing.T) {
	for _, reason := range []string{"audit", "user_rpm", "group_rpm"} {
		t.Run(reason, func(t *testing.T) {
			rpm := &gatewayRequestRPMCache{}
			kind, expectedStatus := securityaudit.DecisionAllow, http.StatusTooManyRequests
			switch reason {
			case "audit":
				kind, expectedStatus = securityaudit.DecisionBlock, http.StatusForbidden
			case "user_rpm":
				rpm.users = map[int64]int{7: 1}
			default:
				rpm.groups = map[[2]int64]int{{7, 3}: 10}
			}
			h, key, store, accounts, slots, audit := newLiveSidebandAdmissionFixture(t, kind, rpm)
			server := httptest.NewServer(liveSidebandAdmissionRouter(h, key))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, response, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/live/call_admitted", nil)
			if conn != nil {
				_ = conn.CloseNow()
			}
			require.Error(t, err, "rejection must happen before WebSocket upgrade")
			require.NotNil(t, response)
			defer func() { _ = response.Body.Close() }()
			require.Equal(t, expectedStatus, response.StatusCode)
			if reason != "audit" {
				retry, err := strconv.Atoi(response.Header.Get("Retry-After"))
				require.NoError(t, err)
				require.GreaterOrEqual(t, retry, 1)
				require.LessOrEqual(t, retry, 60)
			} else {
				require.Empty(t, rpm.users, "audit rejection must precede RPM increment")
				require.Empty(t, rpm.groups)
			}
			require.Zero(t, store.lookups.Load(), "rejected ingress must not resolve an existing call")
			require.Zero(t, store.claims.Load(), "rejected ingress must not take over the controller")
			require.Zero(t, accounts.reads.Load(), "rejected ingress cannot prepare or dial a provider connection")
			require.Zero(t, slots.userAcquireCalls, "sideband reuses its existing Live lease")
			require.Zero(t, slots.accountAcquireCalls)
			audit.mu.Lock()
			require.Len(t, audit.requests, 1)
			require.Equal(t, service.ContentModerationProtocolOpenAILive, audit.requests[0].Protocol)
			require.Empty(t, audit.requests[0].Body)
			audit.mu.Unlock()
		})
	}
}

func TestLiveSidebandAdmittedConnectionUsesExistingCallAndCountsOnce(t *testing.T) {
	rpm := &gatewayRequestRPMCache{}
	h, key, store, accounts, slots, audit := newLiveSidebandAdmissionFixture(t, securityaudit.DecisionAllow, rpm)
	server := httptest.NewServer(liveSidebandAdmissionRouter(h, key))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, response, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/live/call_admitted", nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	_, _, err = conn.Read(ctx)
	require.Error(t, err, "the fixture declines controller ownership after admission")
	require.EqualValues(t, 1, store.lookups.Load())
	require.EqualValues(t, 1, store.claims.Load())
	require.Zero(t, accounts.reads.Load(), "this admission fixture does not claim to exercise provider transport")
	require.Zero(t, slots.userAcquireCalls, "a control connection cannot double-count its persistent Live session")
	require.Zero(t, slots.accountAcquireCalls)
	require.Equal(t, 1, rpm.users[key.UserID])
	require.Equal(t, 1, rpm.groups[[2]int64{key.UserID, *key.GroupID}])
	require.Equal(t, "existing-live-owner", store.record.LeaseID)
	audit.mu.Lock()
	require.Len(t, audit.requests, 1)
	audit.mu.Unlock()
}

func TestLiveSidebandContentFramesAuditWithoutAnotherRPMAdmission(t *testing.T) {
	rpm := &gatewayRequestRPMCache{}
	h, key, _, _, slots, audit := newLiveSidebandAdmissionFixture(t, securityaudit.DecisionAllow, rpm)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/live/call_admitted", nil)
	subject := middleware.AuthSubject{UserID: key.UserID, Concurrency: 1}
	require.True(t, h.admitLiveSideband(c, nil, key, subject))
	checkFrame := h.liveSidebandClientAudit(c, nil, key, subject, "gpt-live-test", nil)
	for _, body := range [][]byte{[]byte(`{"type":"session.update","session":{"instructions":"keep frames audited"}}`), []byte(`{"type":"conversation.item.create","item":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`)} {
		require.NoError(t, checkFrame(c.Request.Context(), coderws.MessageText, body))
	}
	require.NoError(t, checkFrame(c.Request.Context(), coderws.MessageBinary, []byte{0x00, 0x01, 0xff}), "binary control frames must retain the audit pass-through contract")
	require.Equal(t, 1, rpm.users[key.UserID], "content frames belong to the admitted persistent Live session")
	require.Equal(t, 1, rpm.groups[[2]int64{key.UserID, *key.GroupID}])
	require.Zero(t, slots.userAcquireCalls)
	audit.mu.Lock()
	require.Len(t, audit.requests, 4, "connection admission cannot cache away per-frame content audit")
	require.Empty(t, audit.requests[0].Body)
	for _, request := range audit.requests[1:] {
		require.NotEmpty(t, request.Body)
		require.Equal(t, service.ContentModerationProtocolOpenAILive, request.Protocol)
	}
	audit.mu.Unlock()
}
