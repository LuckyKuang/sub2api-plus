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

	takeCalls    int
	statusCalls  int
	settled      []string
	lastAuth     zcode.OffPeakAuth
	availability *zcode.OffPeakAvailability
}

func (s *zhipuOffPeakStub) Availability(context.Context, zcode.OffPeakAuth, string) (*zcode.OffPeakAvailability, error) {
	return s.availability, nil
}

func (s *zhipuOffPeakStub) TakeTicket(_ context.Context, _ string, auth zcode.OffPeakAuth, _ string) (*zcode.OffPeakTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.takeCalls++
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
		return []zcode.OffPeakTicket{{TicketID: "t-1", State: zcode.TicketQueued}}, s.delay, nil
	}
	result := s.statuses[0]
	if len(s.statuses) > 1 {
		s.statuses = s.statuses[1:]
	}
	return result, s.delay, nil
}

func (s *zhipuOffPeakStub) SettleTicket(_ context.Context, ticketID string, _ zcode.OffPeakAuth, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settleEr != nil {
		return s.settleEr
	}
	s.settled = append(s.settled, ticketID)
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
			applyZhipuOffPeakTicketHeader(context.Background(), test.account, headers)
			require.Equal(t, test.want, headers.Get("X-Off-Peak-Ticket-ID"))
		})
	}

	// The provider resolves the ticket from the account's own plan credentials,
	// so a caller-supplied header can never select one.
	headers := http.Header{}
	headers.Set("X-Off-Peak-Ticket-ID", "caller-supplied")
	applyZhipuOffPeakTicketHeader(context.Background(), zhipuOffPeakTestAccount(), headers)
	require.Equal(t, "t-hook", headers.Get("X-Off-Peak-Ticket-ID"))
}

// The unset provider must not panic before startup wiring completes.
func TestZhipuOffPeakTicketHeaderWithoutProvider(t *testing.T) {
	headers := http.Header{}
	applyZhipuOffPeakTicketHeader(context.Background(), &Account{
		ID: 90, Platform: PlatformZhipu, Type: AccountTypeOAuth,
		Credentials: map[string]any{"plan_kind": ZhipuPlanOffPeak},
	}, headers)
	require.Empty(t, headers.Get("X-Off-Peak-Ticket-ID"))
}
