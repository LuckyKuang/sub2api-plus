//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
)

// zhipuOffPeakStub is a scripted ticket server. Each take and status call is
// answered from a queue so a test can describe an exact admission sequence.
type zhipuOffPeakStub struct {
	mu sync.Mutex

	takes    []*zcode.OffPeakTicket
	takeErr  error
	statuses [][]zcode.OffPeakTicket
	delay    time.Duration
	statusEr error
	settleEr error

	lastTicket   *zcode.OffPeakTicket
	taskIDs      []string
	settleAuths  []zcode.OffPeakAuth
	identities   []outboundidentity.Identity
	takeCalls    int
	statusCalls  int
	settled      []string
	lastAuth     zcode.OffPeakAuth
	availability *zcode.OffPeakAvailability
}

func (s *zhipuOffPeakStub) Availability(context.Context, zcode.OffPeakAuth, string) (*zcode.OffPeakAvailability, error) {
	return s.availability, nil
}

func (s *zhipuOffPeakStub) TakeTicket(ctx context.Context, taskID string, auth zcode.OffPeakAuth, _ string) (*zcode.OffPeakTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, _ := outboundidentity.FromContext(ctx)
	s.identities = append(s.identities, identity)
	s.takeCalls++
	s.taskIDs = append(s.taskIDs, taskID)
	s.lastAuth = auth
	if s.takeErr != nil {
		return nil, s.takeErr
	}
	if len(s.takes) == 0 {
		return &zcode.OffPeakTicket{TicketID: "t-default", State: zcode.TicketQueued}, nil
	}
	ticket := s.takes[0]
	if len(s.takes) > 1 {
		s.takes = s.takes[1:]
	}
	s.lastTicket = ticket
	return ticket, nil
}

func (s *zhipuOffPeakStub) TicketStatus(_ context.Context, _ []string, _ zcode.OffPeakAuth, _ string) ([]zcode.OffPeakTicket, time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCalls++
	if s.statusEr != nil {
		return nil, 0, s.statusEr
	}
	if len(s.statuses) == 0 {
		if s.lastTicket != nil && (s.lastTicket.State == zcode.TicketReady || s.lastTicket.State == zcode.TicketActive) {
			return []zcode.OffPeakTicket{*s.lastTicket}, s.delay, nil
		}
		return []zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketQueued}}, s.delay, nil
	}
	result := s.statuses[0]
	if len(s.statuses) > 1 {
		s.statuses = s.statuses[1:]
	}
	return result, s.delay, nil
}

func (s *zhipuOffPeakStub) SettleTicket(ctx context.Context, ticketID string, auth zcode.OffPeakAuth, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settleEr != nil {
		return s.settleEr
	}
	identity, _ := outboundidentity.FromContext(ctx)
	s.identities = append(s.identities, identity)
	s.settled = append(s.settled, ticketID)
	s.settleAuths = append(s.settleAuths, auth)
	return nil
}

func (s *zhipuOffPeakStub) settledTickets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.settled...)
}

func zhipuOffPeakIntPtr(value int) *int { return &value }

func newZhipuOffPeakTestManager(t *testing.T, client *zhipuOffPeakStub) *ZhipuOffPeakTicketManager {
	t.Helper()
	manager := NewZhipuOffPeakTicketManager(client)
	t.Cleanup(manager.Stop)
	return manager
}

func zhipuOffPeakTestAccount() *Account {
	return &Account{
		ID:       77,
		Platform: PlatformZhipu,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"plan_kind":         ZhipuPlanOffPeak,
			"api_key":           "zcode-jwt",
			"zcode_jwt_token":   "zcode-jwt",
			"off_peak_plan_key": "ak.sk",
		},
	}
}

func zhipuOffPeakTestAuth() zcode.OffPeakAuth {
	return zcode.OffPeakAuth{JWT: "zcode-jwt", PlanKey: "ak.sk"}
}

func TestZhipuOffPeakAcquireReturnsAnImmediatelyReadyTicket(t *testing.T) {
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client)

	ticket, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "t-1", ticket.TicketID)
	require.Equal(t, zcode.TicketReady, ticket.State)
	require.Equal(t, 1, client.takeCalls)
	require.Regexp(t, `^offpeak-[0-9a-f-]{36}$`, client.taskIDs[0])
	// An admitted ticket is served without a status round trip.
	require.Zero(t, client.statusCalls)

	// A second request reuses the same ticket instead of spending more
	// take-number quota.
	again, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "t-1", again.TicketID)
	require.Equal(t, 1, client.takeCalls)
}

func TestZhipuOffPeakAcquirePollsUntilAdmitted(t *testing.T) {
	client := &zhipuOffPeakStub{
		takes: []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketQueued}},
		statuses: [][]zcode.OffPeakTicket{
			{{TicketID: "t-1", State: zcode.TicketQueued}},
			{{TicketID: "t-1", State: zcode.TicketReady}},
		},
	}
	manager := newZhipuOffPeakTestManager(t, client)

	ticket, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "t-1", ticket.TicketID)
	require.GreaterOrEqual(t, client.statusCalls, 2)
}

func TestZhipuOffPeakAcquireHonoursTheBudgetAndReportsPosition(t *testing.T) {
	client := &zhipuOffPeakStub{
		takes:    []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketQueued, Position: zhipuOffPeakIntPtr(42)}},
		statuses: [][]zcode.OffPeakTicket{{{TicketID: "t-1", State: zcode.TicketQueued, Position: zhipuOffPeakIntPtr(42)}}},
	}
	manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(60 * time.Millisecond)

	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "still queued", "the queue position is reported back to the operator")
}

func TestZhipuOffPeakAcquireKeepsTheTicketWhenStatusPollingFails(t *testing.T) {
	client := &zhipuOffPeakStub{
		takes:    []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketQueued}},
		statusEr: errors.New("temporary network failure"),
	}
	manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(60 * time.Millisecond)

	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Equal(t, 1, client.takeCalls, "a transient status failure must not discard the ticket and re-take")
}

func TestZhipuOffPeakAcquireReTakesThenGivesUp(t *testing.T) {
	client := &zhipuOffPeakStub{
		takes:    []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketNotFound}},
		statuses: [][]zcode.OffPeakTicket{{{TicketID: "t-1", State: zcode.TicketNotFound}}},
	}
	manager := newZhipuOffPeakTestManager(t, client).WithAcquireTimeout(time.Second)

	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Equal(t, 2, client.takeCalls, "one initial take plus one re-take, then the budget is enforced")
}

func TestZhipuOffPeakAcquireClassifiesPlatformRejections(t *testing.T) {
	client := &zhipuOffPeakStub{takeErr: &zcode.OffPeakError{HTTPStatus: http.StatusTooManyRequests, Code: zcode.OffPeakCodeQuotaExhausted, Message: "quota"}}
	manager := newZhipuOffPeakTestManager(t, client)
	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)

	client = &zhipuOffPeakStub{takeErr: &zcode.OffPeakError{HTTPStatus: http.StatusForbidden, Code: zcode.OffPeakCodeNoEligibility}}
	manager = newZhipuOffPeakTestManager(t, client)
	_, err = manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)

	client = &zhipuOffPeakStub{takeErr: errors.New("untyped transport failure")}
	manager = newZhipuOffPeakTestManager(t, client)
	_, err = manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
}

func TestZhipuOffPeakAcquireRequiresAnAccountAndToken(t *testing.T) {
	manager := newZhipuOffPeakTestManager(t, &zhipuOffPeakStub{})
	_, err := manager.Acquire(context.Background(), nil, zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	_, err = manager.Acquire(context.Background(), &Account{ID: 1, Platform: PlatformZhipu}, zcode.OffPeakAuth{}, "")
	require.Error(t, err)
}

func TestZhipuOffPeakSweepSettlesIdleTickets(t *testing.T) {
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client).WithSettleIdle(time.Second)

	// Inject a clock so the idle window is exercised without sleeping.
	now := time.Now()
	var clockMu sync.Mutex
	manager.now = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}

	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)

	// Still inside the idle window: the ticket is kept.
	manager.sweepOnce()
	require.Empty(t, client.settledTickets())

	clockMu.Lock()
	now = now.Add(2 * time.Second)
	clockMu.Unlock()

	manager.sweepOnce()
	require.Equal(t, []string{"t-1"}, client.settledTickets())

	// After settling, the next request takes a fresh ticket.
	client.mu.Lock()
	client.takes = []*zcode.OffPeakTicket{{TicketID: "t-2", State: zcode.TicketReady}}
	client.mu.Unlock()
	ticket, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "t-2", ticket.TicketID)
	clockMu.Lock()
	now = now.Add(2 * time.Second)
	clockMu.Unlock()
	manager.sweepOnce()
	require.Equal(t, []string{"t-1", "t-2"}, client.settledTickets(), "every ticket generation must be retired")
}

func TestZhipuOffPeakSweepRetriesFailedSettles(t *testing.T) {
	client := &zhipuOffPeakStub{
		takes:    []*zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketReady}},
		settleEr: errors.New("off-peak endpoint returned status 503"),
	}
	manager := newZhipuOffPeakTestManager(t, client).WithSettleIdle(time.Nanosecond)
	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)

	// A failed settle is not fatal: the platform reclaims the ticket by timeout
	// and the next cycle retries.
	manager.sweepOnce()
	require.Empty(t, client.settledTickets())
	manager.sweepOnce()
}

// The request-path hook must be a no-op for every account that is not an
// off-peak Zhipu account, so the injection cannot leak into other providers.
func TestZhipuOffPeakTicketHeaderAppliesOnlyToOffPeakZhipu(t *testing.T) {
	cases := []struct {
		name    string
		account *Account
		want    string
	}{
		{"off-peak zhipu", zhipuOffPeakTestAccount(), "t-hook"},
		{"coding-plan zhipu", &Account{ID: 78, Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_kind": ZhipuPlanIndividualCodingPlan}}, ""},
		{"other platform", &Account{ID: 79, Platform: PlatformMiniMax, Type: AccountTypeAPIKey, Credentials: map[string]any{"plan_kind": ZhipuPlanOffPeak}}, ""},
	}
	manager := newZhipuOffPeakTestManager(t, &zhipuOffPeakStub{
		takes: []*zcode.OffPeakTicket{{TicketID: "t-hook", State: zcode.TicketReady}},
	})
	SetZhipuOffPeakTicketProvider(manager)

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{}
			require.NoError(t, applyZhipuOffPeakTicketHeader(context.Background(), test.account, headers))
			require.Equal(t, test.want, headers.Get("X-Off-Peak-Ticket-ID"))
		})
	}

	// The provider resolves the ticket from the account's own plan credentials,
	// so a caller-supplied header can never select one.
	headers := http.Header{}
	headers.Set("X-Off-Peak-Ticket-ID", "caller-supplied")
	require.NoError(t, applyZhipuOffPeakTicketHeader(context.Background(), zhipuOffPeakTestAccount(), headers))
	require.Equal(t, "t-hook", headers.Get("X-Off-Peak-Ticket-ID"))
}

func TestZhipuOffPeakUnavailableProviderRejectsAdmission(t *testing.T) {
	previous := zhipuOffPeakProvider.Load()
	SetZhipuOffPeakTicketProvider((*ZhipuOffPeakTicketManager)(nil))
	t.Cleanup(func() {
		if previous != nil {
			zhipuOffPeakProvider.Store(previous)
		}
	})
	headers := http.Header{"X-Off-Peak-Ticket-Id": {"caller-ticket"}}
	require.Error(t, applyZhipuOffPeakTicketHeader(context.Background(), zhipuOffPeakTestAccount(), headers))
	require.Empty(t, headers.Get("X-Off-Peak-Ticket-ID"))
}

func TestZhipuOffPeakSettlementRetainsCredentialOwnerIdentity(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Profiles["zcode"] = OutboundIdentitySelection{Preset: "zcode", Version: "4.1.0"}
	svc, ctx := outboundIdentityTestSettings(t, config)
	account := zhipuOffPeakTestAccount()
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "4.2.0", Headers: map[string]string{"X-Client-Timezone": "Asia/Shanghai"}}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "identity-ticket", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client)
	now := time.Now()
	manager.now = func() time.Time { return now }
	_, err := manager.Acquire(ctx, account, zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	config.Profiles["zcode"] = OutboundIdentitySelection{Preset: "zcode", Version: "4.3.0"}
	require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "4.4.0"}
	_, err = manager.Acquire(ctx, account, zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	now = now.Add(DefaultOffPeakSettleIdle + time.Second)
	manager.sweepOnce()
	require.Equal(t, []string{"identity-ticket"}, client.settledTickets())
	require.Len(t, client.identities, 2)
	require.Equal(t, client.identities[0], client.identities[1])
	require.Equal(t, account.ID, client.identities[1].AccountID)
	require.Equal(t, "4.2.0", client.identities[1].Version)
	require.Equal(t, "Asia/Shanghai", client.identities[1].Headers["X-Client-Timezone"])
}

func TestZhipuOffPeakCredentialChangeSettlesWithOriginalOwner(t *testing.T) {
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "old", State: zcode.TicketReady}, {TicketID: "new", State: zcode.TicketReady}}}
	manager := newZhipuOffPeakTestManager(t, client)
	account := zhipuOffPeakTestAccount()
	auth := zhipuOffPeakTestAuth()
	_, err := manager.Acquire(context.Background(), account, auth, "")
	require.NoError(t, err)
	changed := auth
	changed.JWT = "new-grant"
	ticket, err := manager.Acquire(context.Background(), account, changed, "")
	require.NoError(t, err)
	require.Equal(t, "new", ticket.TicketID)
	require.Equal(t, []string{"old"}, client.settled)
	require.Equal(t, []zcode.OffPeakAuth{auth}, client.settleAuths)
	require.Equal(t, changed, client.lastAuth)
	require.NotEqual(t, client.taskIDs[0], client.taskIDs[1])
}

type blockingOffPeakClient struct {
	*zhipuOffPeakStub
	take   func(context.Context) error
	settle func(context.Context) error
}

func (c *blockingOffPeakClient) TakeTicket(ctx context.Context, task string, auth zcode.OffPeakAuth, proxy string) (*zcode.OffPeakTicket, error) {
	if c.take != nil {
		if err := c.take(ctx); err != nil {
			return nil, err
		}
	}
	return c.zhipuOffPeakStub.TakeTicket(ctx, task, auth, proxy)
}
func (c *blockingOffPeakClient) SettleTicket(ctx context.Context, ticket string, auth zcode.OffPeakAuth, proxy string) error {
	if c.settle != nil {
		if err := c.settle(ctx); err != nil {
			return err
		}
	}
	return c.zhipuOffPeakStub.SettleTicket(ctx, ticket, auth, proxy)
}
func TestZhipuOffPeakBudgetIncludesBlockedUpstream(t *testing.T) {
	client := &blockingOffPeakClient{zhipuOffPeakStub: &zhipuOffPeakStub{}, take: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	manager := NewZhipuOffPeakTicketManager(client).WithAcquireTimeout(30 * time.Millisecond)
	defer manager.Stop()
	done := make(chan error, 1)
	go func() {
		_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
		done <- err
	}()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("upstream ignored acquisition budget")
	}
}
func TestZhipuOffPeakCannotReuseTicketDuringSettlement(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	client := &blockingOffPeakClient{zhipuOffPeakStub: &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "old", State: zcode.TicketReady}, {TicketID: "new", State: zcode.TicketReady}}}, settle: func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	manager := NewZhipuOffPeakTicketManager(client).WithAcquireTimeout(30 * time.Millisecond).WithSettleIdle(time.Nanosecond)
	defer manager.Stop()
	_, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	swept := make(chan struct{})
	go func() { manager.sweepOnce(); close(swept) }()
	<-entered
	ticket, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	close(release)
	<-swept
	require.Error(t, err, "waiting for settlement is also bounded by the request budget")
	require.Nil(t, ticket, "must not return a ticket being retired")
	ticket, err = manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "new", ticket.TicketID)
	require.NotEqual(t, client.taskIDs[0], client.taskIDs[1])
}

func TestZhipuOffPeakCachedAdmissionIsRecheckedAndExpiredTicketReplaced(t *testing.T) {
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "expired", State: zcode.TicketReady}, {TicketID: "fresh", State: zcode.TicketReady}}, statuses: [][]zcode.OffPeakTicket{{{TicketID: "expired", State: zcode.TicketExpired}}}}
	manager := newZhipuOffPeakTestManager(t, client)
	first, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "expired", first.TicketID)
	second, err := manager.Acquire(context.Background(), zhipuOffPeakTestAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "fresh", second.TicketID, "local activity cannot keep expired platform admission alive")
	require.Equal(t, 2, client.takeCalls)
	require.Equal(t, 1, client.statusCalls)
}

func TestZhipuOffPeakForwardAndProbeRequireAdmissionBeforeAnySend(t *testing.T) {
	for _, probe := range []bool{false, true} {
		client := &zhipuOffPeakStub{takeErr: errors.New("ticket denied")}
		manager := newZhipuOffPeakTestManager(t, client)
		previous := zhipuOffPeakProvider.Load()
		SetZhipuOffPeakTicketProvider(manager)
		t.Cleanup(func() {
			if previous != nil {
				zhipuOffPeakProvider.Store(previous)
			}
		})
		account := zhipuOffPeakTestAccount()
		req, err := http.NewRequest(http.MethodPost, "https://zcode.z.ai/api/v1/off-peak/anthropic/v1/messages", nil)
		require.NoError(t, err)
		req.Header.Set("X-Off-Peak-Ticket-ID", "caller-ticket")
		// A nil transport intentionally makes any accidental model dispatch fail:
		// the admission error must be returned before that side effect.
		if probe {
			_, err = (&AccountTestService{}).doOpenAIAccountTestUpstream(req, "", account, false)
		} else {
			_, err = (&OpenAIGatewayService{}).doOpenAIUpstream(req, "", account)
		}
		require.ErrorContains(t, err, "ticket denied")
		require.Empty(t, req.Header.Get("X-Off-Peak-Ticket-ID"))
	}
}
