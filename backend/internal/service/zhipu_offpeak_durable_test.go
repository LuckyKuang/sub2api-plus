//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

// This fake supplies crash/ACK boundaries. PostgreSQL fencing and lease
// concurrency are independently tested against the operational tables.
type offPeakMemoryStore struct {
	mu                                               sync.Mutex
	record                                           ZhipuOffPeakRecord
	requests                                         map[string]bool
	beginCount, admissions, releases, renewals, late int
	saveError, renewError                            error
	failACK                                          bool
}

func (s *offPeakMemoryStore) Open(_ context.Context, _ *Account, r ZhipuOffPeakRecord) (ZhipuOffPeakRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID == "" || s.record.Closed || s.record.Retired {
		r.State = "new"
		r.LastUsed = time.Now()
		s.record = r
	}
	return s.record, nil
}

func (s *offPeakMemoryStore) Current(_ context.Context, accountID int64) (ZhipuOffPeakRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID == "" || s.record.AccountID != accountID || s.record.Retired || s.record.Closed {
		return ZhipuOffPeakRecord{}, ErrOffPeakRecordNotFound
	}
	return s.record, nil
}
func (s *offPeakMemoryStore) Terminate(_ context.Context, accountID int64, id, generation string) (ZhipuOffPeakRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID != id || s.record.AccountID != accountID || s.record.Generation != generation {
		return ZhipuOffPeakRecord{}, ErrOffPeakFenceLost
	}
	if !s.record.Retired && !s.record.Closed {
		s.record.Retired = true
		s.record.SettlementPending = s.record.TicketID != ""
		s.record.Fence++
		s.record.LeaseID = ""
		s.record.LeaseUntil = time.Time{}
	}
	return s.record, nil
}
func (s *offPeakMemoryStore) Claim(_ context.Context, id string, settle bool, _ time.Duration, _ time.Duration) (ZhipuOffPeakRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID != id || s.record.Closed {
		if !settle {
			return ZhipuOffPeakRecord{}, ErrOffPeakFenceLost
		}
		return ZhipuOffPeakRecord{}, ErrOffPeakLeaseBusy
	}
	if !settle && s.record.Retired {
		return ZhipuOffPeakRecord{}, ErrOffPeakFenceLost
	}
	if settle && len(s.requests) > 0 {
		return ZhipuOffPeakRecord{}, ErrOffPeakLeaseBusy
	}
	s.record.Fence++
	s.record.LeaseID = "operation"
	if settle {
		s.record.SettlementPending = true
		s.record.Attempts++
	}
	return s.record, nil
}
func (s *offPeakMemoryStore) Save(_ context.Context, r ZhipuOffPeakRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveError != nil {
		return s.saveError
	}
	if s.failACK && r.Closed {
		return errors.New("ACK write unavailable")
	}
	if s.record.ID != r.ID || s.record.Fence != r.Fence || s.record.LeaseID != r.LeaseID {
		return ErrOffPeakFenceLost
	}
	s.record = r
	s.record.LeaseID = ""
	if r.Closed {
		s.record.Snapshot = nil
	}
	return nil
}
func (s *offPeakMemoryStore) BeginTake(_ context.Context, r ZhipuOffPeakRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID != r.ID || s.record.Fence != r.Fence || s.record.Retired || s.record.Closed {
		return ErrOffPeakFenceLost
	}
	s.beginCount++
	s.record.State = "uncertain"
	return nil
}
func (s *offPeakMemoryStore) Admit(_ context.Context, r ZhipuOffPeakRecord, _ *Account, request string, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ID != r.ID || s.record.Fence != r.Fence || s.record.Retired || s.record.Closed {
		return ErrOffPeakFenceLost
	}
	s.record = r
	s.record.LeaseID = ""
	s.admissions++
	if s.requests == nil {
		s.requests = map[string]bool{}
	}
	s.requests[request] = true
	return nil
}
func (s *offPeakMemoryStore) RenewRequest(context.Context, string, string, time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renewals++
	return s.renewError
}
func (s *offPeakMemoryStore) ReleaseRequest(_ context.Context, _ string, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	delete(s.requests, id)
	return nil
}
func (s *offPeakMemoryStore) Due(context.Context, time.Duration, int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Closed || s.record.TicketID == "" || len(s.requests) > 0 {
		return nil, nil
	}
	return []string{s.record.ID}, nil
}
func (s *offPeakMemoryStore) RegisterLateTicket(_ context.Context, r ZhipuOffPeakRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.late++
	s.record.TicketID = r.TicketID
	s.record.Retired = true
	s.record.SettlementPending = true
	return nil
}

func newOffPeakDurableTestManager(t *testing.T, client ZhipuOffPeakClient, store *offPeakMemoryStore) *ZhipuOffPeakTicketManager {
	t.Helper()
	m := &ZhipuOffPeakTicketManager{client: client, acquireTimeout: 100 * time.Millisecond, settleIdle: time.Millisecond, stopCh: make(chan struct{}), now: time.Now}
	// Tests of durable control use a non-project account to isolate the ticket
	// lifecycle. Existing project-token tests cover same-owner PAT issuance.
	require.NoError(t, m.WithPersistence(store, offPeakPersistenceTestConfig()))
	t.Cleanup(m.Stop)
	return m
}
func offPeakDurableAccount() *Account {
	a := zhipuOffPeakTestAccount()
	a.Type = AccountTypeAPIKey
	return a
}

func TestZhipuOffPeakDurableUncertainTakeNeverReplaysAfterRestart(t *testing.T) {
	store := &offPeakMemoryStore{}
	client := &zhipuOffPeakStub{takeErr: errors.New("response lost after upstream accepted")}
	m := newOffPeakDurableTestManager(t, client, store)
	first, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Nil(t, first)
	require.Equal(t, 1, client.takeCalls)
	require.Equal(t, "uncertain", store.record.State)
	require.Equal(t, 1, store.beginCount, "intent must precede network take")
	m.Stop()
	restarted := newOffPeakDurableTestManager(t, client, store)
	second, err := restarted.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Nil(t, second)
	require.Contains(t, err.Error(), "unknown")
	require.Equal(t, 1, client.takeCalls, "same task_id after unknown result cannot prove the upstream idempotency window")
	require.Zero(t, store.admissions)
}

func TestZhipuOffPeakDurableQueueTimeoutRetainsTicketForNextRequest(t *testing.T) {
	store := &offPeakMemoryStore{}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "queued-original", State: zcode.TicketQueued}}, statuses: [][]zcode.OffPeakTicket{{{TicketID: "queued-original", State: zcode.TicketQueued, Position: zhipuOffPeakIntPtr(8)}}}}
	m := newOffPeakDurableTestManager(t, client, store)
	ticket, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Contains(t, err.Error(), "position 8")
	require.Equal(t, "queued-original", store.record.TicketID)
	client.mu.Lock()
	client.statuses = [][]zcode.OffPeakTicket{{{TicketID: "queued-original", State: zcode.TicketReady}}}
	client.mu.Unlock()
	restarted := newOffPeakDurableTestManager(t, client, store)
	ticket, err = restarted.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	require.Equal(t, "queued-original", ticket.TicketID)
	require.Equal(t, 1, client.takeCalls)
	require.GreaterOrEqual(t, client.statusCalls, 2)
	ticket.Release()
	ticket.Release()
	require.Equal(t, 1, store.releases)
}

func TestZhipuOffPeakDurableTakeCommitFailureCannotAdmit(t *testing.T) {
	store := &offPeakMemoryStore{saveError: errors.New("mapping write unavailable")}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "known-late", State: zcode.TicketReady}}}
	m := newOffPeakDurableTestManager(t, client, store)
	ticket, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Zero(t, store.admissions)
	require.Equal(t, 1, store.late)
	require.Equal(t, "known-late", store.record.TicketID)
	require.True(t, store.record.Retired)
	require.True(t, store.record.SettlementPending)
}

func TestZhipuOffPeakDurableSettlementACKMustPersistBeforeClear(t *testing.T) {
	store := &offPeakMemoryStore{}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "same-ticket", State: zcode.TicketReady}}}
	m := newOffPeakDurableTestManager(t, client, store)
	ticket, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	ticket.Release()
	client.settleEr = errors.New("temporary 503")
	m.sweepDurable(m.persistence())
	require.Equal(t, []string{"same-ticket"}, client.settleAttempts)
	require.False(t, store.record.Closed)
	require.True(t, store.record.SettlementPending)
	client.settleEr = nil
	store.failACK = true
	m.sweepDurable(m.persistence())
	require.Equal(t, []string{"same-ticket", "same-ticket"}, client.settleAttempts)
	require.False(t, store.record.Closed, "ACK not committed cannot discard durable intent")
	store.failACK = false
	m.sweepDurable(m.persistence())
	require.True(t, store.record.Closed)
	require.Empty(t, store.record.Snapshot)
	m.sweepDurable(m.persistence())
	require.Len(t, client.settleAttempts, 3, "committed ACK stops dispatch")
	require.Equal(t, 1, client.takeCalls, "settlement recovery never replays a model request or new take")
}

func TestZhipuOffPeakDurableLostHeartbeatPreservesRequestSafetyWindow(t *testing.T) {
	store := &offPeakMemoryStore{renewError: errors.New("database unavailable")}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "still-inflight", State: zcode.TicketReady}}}
	m := newOffPeakDurableTestManager(t, client, store)
	m.persistence().renewInterval = time.Millisecond
	ticket, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	request := ticket.bindRequest(httptest.NewRequest(http.MethodPost, "http://upstream.test/v1/chat/completions", nil))
	response := &http.Response{Body: ioNopBodyForOffPeakTest{strings.NewReader("partial")}}
	holdZhipuOffPeakResponse(request.Context(), ticket, response, nil)
	select {
	case <-request.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("heartbeat loss must cancel physical model request")
	}
	require.NoError(t, response.Body.Close())
	ticket.Release()
	store.mu.Lock()
	releases := store.releases
	remaining := len(store.requests)
	store.mu.Unlock()
	require.Zero(t, releases, "lost heartbeat cannot delete the server-side safety window")
	require.Equal(t, 1, remaining)
	m.sweepDurable(m.persistence())
	require.Empty(t, client.settleAttempts)
}

type ioNopBodyForOffPeakTest struct{ *strings.Reader }

func (ioNopBodyForOffPeakTest) Close() error { return nil }

func TestZhipuOffPeakDurableMissingStatusNeverClearsOrRetakes(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		statuses []zcode.OffPeakTicket
	}{
		{"empty", nil},
		{"missing-ticket", []zcode.OffPeakTicket{{TicketID: "other", State: zcode.TicketExpired}}},
		{"unknown-state", []zcode.OffPeakTicket{{TicketID: "known", State: "future_state"}}},
		{"conflicting-duplicate", []zcode.OffPeakTicket{{TicketID: "known", State: zcode.TicketReady}, {TicketID: "known", State: zcode.TicketExpired}}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store := &offPeakMemoryStore{}
			client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "known", State: zcode.TicketReady}}, statuses: [][]zcode.OffPeakTicket{scenario.statuses}}
			m := newOffPeakDurableTestManager(t, client, store)
			ticket, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
			require.Error(t, err)
			require.Nil(t, ticket)
			require.Equal(t, "known", store.record.TicketID)
			require.False(t, store.record.Closed)
			require.NotEmpty(t, store.record.Snapshot)
			require.Equal(t, 1, client.takeCalls)
			m.sweepDurable(m.persistence())
			require.Empty(t, client.settleAttempts)
			require.False(t, store.record.Closed)
			require.True(t, store.record.SettlementPending)
		})
	}
}

func TestZhipuOffPeakDurableRetentionWipesSnapshotWithoutChangingOwner(t *testing.T) {
	store := &offPeakMemoryStore{}
	client := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "retained", State: zcode.TicketReady}}}
	m := newOffPeakDurableTestManager(t, client, store)
	ticket, err := m.Acquire(context.Background(), offPeakDurableAccount(), zhipuOffPeakTestAuth(), "")
	require.NoError(t, err)
	ticket.Release()
	store.record.RetainUntil = time.Now().Add(-time.Second)
	m.sweepDurable(m.persistence())
	require.True(t, store.record.Closed)
	require.Equal(t, "abandoned", store.record.State)
	require.Equal(t, "retention_expired", store.record.LastError)
	require.Empty(t, store.record.Snapshot)
	require.Empty(t, client.settleAttempts)
}
