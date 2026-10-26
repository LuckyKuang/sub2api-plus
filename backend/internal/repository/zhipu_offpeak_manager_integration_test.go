//go:build integration

package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type offPeakManagerWireCall struct {
	Operation string
	TaskID    string
	TicketIDs []string
	Auth      zcode.OffPeakAuth
	Proxy     string
	Identity  outboundidentity.Identity
}

// This is the provider protocol port, not a fake manager/store. The manager,
// encryption, snapshots, PostgreSQL transactions and fencing are production.
// Scripted responses do not establish real upstream account entitlement.
type offPeakManagerScriptClient struct {
	mu     sync.Mutex
	calls  []offPeakManagerWireCall
	take   func(string) (*zcode.OffPeakTicket, error)
	status func([]string) ([]zcode.OffPeakTicket, time.Duration, error)
	settle func(string) error
}

func (c *offPeakManagerScriptClient) record(ctx context.Context, operation, task string, ids []string, auth zcode.OffPeakAuth, proxy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	identity, _ := outboundidentity.FromContext(ctx)
	c.calls = append(c.calls, offPeakManagerWireCall{Operation: operation, TaskID: task, TicketIDs: append([]string(nil), ids...), Auth: auth, Proxy: proxy, Identity: identity})
}
func (c *offPeakManagerScriptClient) snapshot() []offPeakManagerWireCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]offPeakManagerWireCall(nil), c.calls...)
}
func (c *offPeakManagerScriptClient) Availability(ctx context.Context, auth zcode.OffPeakAuth, proxy string) (*zcode.OffPeakAvailability, error) {
	c.record(ctx, "availability", "", nil, auth, proxy)
	return nil, errors.New("unexpected availability call")
}
func (c *offPeakManagerScriptClient) TakeTicket(ctx context.Context, task string, auth zcode.OffPeakAuth, proxy string) (*zcode.OffPeakTicket, error) {
	c.record(ctx, "take", task, nil, auth, proxy)
	if c.take == nil {
		return nil, errors.New("unexpected Take after restart")
	}
	return c.take(task)
}
func (c *offPeakManagerScriptClient) TicketStatus(ctx context.Context, ids []string, auth zcode.OffPeakAuth, proxy string) ([]zcode.OffPeakTicket, time.Duration, error) {
	c.record(ctx, "status", "", ids, auth, proxy)
	if c.status == nil {
		return nil, 0, errors.New("unexpected Status")
	}
	return c.status(ids)
}
func (c *offPeakManagerScriptClient) SettleTicket(ctx context.Context, id string, auth zcode.OffPeakAuth, proxy string) error {
	c.record(ctx, "settle", "", []string{id}, auth, proxy)
	if c.settle == nil {
		return errors.New("unexpected settlement")
	}
	return c.settle(id)
}

func newOffPeakManagerIntegrationOwner(t *testing.T) (*service.Account, string) {
	t.Helper()
	_, old, _ := newOffPeakStoreFixture(t)
	credentials := map[string]any{"oauth_provider": "bigmodel", "plan_kind": service.ZhipuPlanOffPeak, "account_mode": service.AccountModeCoding, "zcode_jwt_token": "manager-owner-jwt", "api_key": "manager-owner-jwt", "off_peak_plan_key": "manager-plan-key",
		service.SettingKeyOutboundIdentity: service.OutboundIdentitySelection{Preset: zcode.Preset, Version: "3.16.2", Headers: map[string]string{zcode.HeaderDeviceMid: "7d918214-79ad-4057-bb2b-4ac6bd24a907"}}}
	proxy := mustCreateProxy(t, integrationEntClient, &service.Proxy{Name: "offpeak-manager-route", Protocol: "http", Host: "captured-proxy.example", Port: 8080, Status: service.StatusActive})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=NULL WHERE id=$1", old.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM proxies WHERE id=$1", proxy.ID)
	})
	payload, err := json.Marshal(credentials)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(context.Background(), `UPDATE accounts SET type='apikey',credentials=$2::jsonb,proxy_id=$3 WHERE id=$1`, old.ID, string(payload), proxy.ID)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	owner, err := repo.GetByID(context.Background(), old.ID)
	require.NoError(t, err)
	route, err := owner.StrictProxyURL()
	require.NoError(t, err)
	return owner, route
}

func newOffPeakManagerIntegrationManager(t *testing.T, client service.ZhipuOffPeakClient, budget time.Duration) *service.ZhipuOffPeakTicketManager {
	t.Helper()
	manager := service.NewZhipuOffPeakTicketManager(client).WithAcquireTimeout(budget).WithSettleIdle(time.Hour)
	cfg := config.ZhipuOffPeakConfig{PersistenceEnabled: true, ActiveKeyID: "integration-key-v1", SnapshotKeys: `{"integration-key-v1":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("i", 32))) + `"}`, SnapshotRetentionHours: 24}
	require.NoError(t, manager.WithPersistence(NewZhipuOffPeakRepository(integrationDB), cfg))
	t.Cleanup(manager.Stop)
	return manager
}

func currentOffPeakManagerRecord(t *testing.T, ownerID int64) service.ZhipuOffPeakRecord {
	t.Helper()
	var id string
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT id FROM zhipu_offpeak_tickets WHERE account_id=$1 AND NOT retired AND NOT closed", ownerID).Scan(&id))
	return loadOffPeakStoreRecord(t, id)
}

func assertOffPeakManagerCapturedOwner(t *testing.T, calls []offPeakManagerWireCall, route string) {
	t.Helper()
	for _, call := range calls {
		require.Equal(t, zcode.OffPeakAuth{JWT: "manager-owner-jwt", PlanKey: "manager-plan-key"}, call.Auth)
		require.Equal(t, route, call.Proxy)
		require.Equal(t, "zcode", call.Identity.Preset)
		require.Equal(t, "ZCode/3.16.2", call.Identity.UserAgent)
		require.Equal(t, "3.16.2", call.Identity.Version)
	}
}

func TestZhipuOffPeakManagerPostgresQueueTimeoutRestartsWithSameTicketStatus(t *testing.T) {
	owner, route := newOffPeakManagerIntegrationOwner(t)
	position := 11
	firstClient := &offPeakManagerScriptClient{take: func(task string) (*zcode.OffPeakTicket, error) {
		record := currentOffPeakManagerRecord(t, owner.ID)
		require.Equal(t, "uncertain", record.State, "Take intent must already be durable when the provider accepts it")
		require.Equal(t, task, record.TaskID)
		return &zcode.OffPeakTicket{TicketID: "queued-durable-ticket", TaskID: task, State: zcode.TicketQueued, Position: &position}, nil
	}, status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		require.Equal(t, []string{"queued-durable-ticket"}, ids)
		return []zcode.OffPeakTicket{{TicketID: "queued-durable-ticket", State: zcode.TicketQueued, Position: &position}}, 2 * time.Second, nil
	}}
	first := newOffPeakManagerIntegrationManager(t, firstClient, time.Second)
	ticket, err := first.TicketForRequest(context.Background(), owner)
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Equal(t, http.StatusTooManyRequests, infraerrors.Code(err))
	require.Equal(t, "ZHIPU_OFFPEAK_QUEUE_TIMEOUT", infraerrors.Reason(err))
	require.Contains(t, infraerrors.Message(err), "position 11")
	retained := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, "queued-durable-ticket", retained.TicketID)
	require.Equal(t, "queued", retained.State)
	require.Empty(t, retained.LeaseID)
	require.NotEmpty(t, retained.Snapshot)
	first.Stop()
	secondClient := &offPeakManagerScriptClient{status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		require.Equal(t, []string{"queued-durable-ticket"}, ids)
		return []zcode.OffPeakTicket{{TicketID: "queued-durable-ticket", State: zcode.TicketReady}}, 0, nil
	}}
	second := newOffPeakManagerIntegrationManager(t, secondClient, 2*time.Second)
	recovered, err := second.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.Equal(t, "queued-durable-ticket", recovered.TicketID)
	recovered.Release()
	recovered.Release()
	after := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, retained.ID, after.ID)
	require.Equal(t, retained.TaskID, after.TaskID)
	require.Equal(t, retained.Snapshot, after.Snapshot)
	firstCalls, secondCalls := firstClient.snapshot(), secondClient.snapshot()
	require.Len(t, firstCalls, 2)
	require.Equal(t, []string{"take", "status"}, []string{firstCalls[0].Operation, firstCalls[1].Operation})
	require.Len(t, secondCalls, 1)
	require.Equal(t, "status", secondCalls[0].Operation)
	assertOffPeakManagerCapturedOwner(t, append(firstCalls, secondCalls...), route)
	var leases int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", retained.ID).Scan(&leases))
	require.Zero(t, leases)
}

func TestZhipuOffPeakManagerPostgresAcceptedTakeResponseLossDoesNotRetakeAfterRestart(t *testing.T) {
	owner, route := newOffPeakManagerIntegrationOwner(t)
	acceptedTask := ""
	firstClient := &offPeakManagerScriptClient{take: func(task string) (*zcode.OffPeakTicket, error) {
		acceptedTask = task
		record := currentOffPeakManagerRecord(t, owner.ID)
		require.Equal(t, "uncertain", record.State)
		return nil, errors.New("response lost after platform accepted ticket")
	}}
	first := newOffPeakManagerIntegrationManager(t, firstClient, 2*time.Second)
	ticket, err := first.TicketForRequest(context.Background(), owner)
	require.Error(t, err)
	require.Nil(t, ticket)
	require.Equal(t, http.StatusBadGateway, infraerrors.Code(err))
	unknown := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, "uncertain", unknown.State)
	require.Empty(t, unknown.TicketID)
	require.Equal(t, acceptedTask, unknown.TaskID)
	require.Empty(t, unknown.LeaseID)
	first.Stop()
	secondClient := &offPeakManagerScriptClient{}
	second := newOffPeakManagerIntegrationManager(t, secondClient, 2*time.Second)
	recovered, err := second.TicketForRequest(context.Background(), owner)
	require.Error(t, err)
	require.Nil(t, recovered)
	require.Equal(t, "ZHIPU_OFFPEAK_TAKE_UNCERTAIN", infraerrors.Reason(err))
	require.Empty(t, secondClient.snapshot(), "an uncertain persisted Take cannot be retried even with the same task ID after restart")
	require.Len(t, firstClient.snapshot(), 1)
	assertOffPeakManagerCapturedOwner(t, firstClient.snapshot(), route)
	after := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, unknown.ID, after.ID)
	require.Equal(t, "uncertain", after.State)
	require.Equal(t, unknown.TaskID, after.TaskID)
}

func TestZhipuOffPeakManagerPostgresConfirmedExpiryCreatesOneNewTaskAndTicket(t *testing.T) {
	owner, route := newOffPeakManagerIntegrationOwner(t)
	firstClient := &offPeakManagerScriptClient{take: func(task string) (*zcode.OffPeakTicket, error) {
		return &zcode.OffPeakTicket{TicketID: "expiring-ticket", TaskID: task, State: zcode.TicketReady}, nil
	}, status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		return []zcode.OffPeakTicket{{TicketID: "expiring-ticket", State: zcode.TicketReady}}, 0, nil
	}}
	first := newOffPeakManagerIntegrationManager(t, firstClient, 2*time.Second)
	ticket, err := first.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	ticket.Release()
	original := currentOffPeakManagerRecord(t, owner.ID)
	first.Stop()
	secondClient := &offPeakManagerScriptClient{take: func(task string) (*zcode.OffPeakTicket, error) {
		require.NotEqual(t, original.TaskID, task)
		return &zcode.OffPeakTicket{TicketID: "fresh-ticket", TaskID: task, State: zcode.TicketReady}, nil
	}, status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		if ids[0] == "expiring-ticket" {
			return []zcode.OffPeakTicket{{TicketID: "expiring-ticket", State: zcode.TicketExpired}}, 0, nil
		}
		require.Equal(t, []string{"fresh-ticket"}, ids)
		return []zcode.OffPeakTicket{{TicketID: "fresh-ticket", State: zcode.TicketReady}}, 0, nil
	}}
	second := newOffPeakManagerIntegrationManager(t, secondClient, 2*time.Second)
	recovered, err := second.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, "fresh-ticket", recovered.TicketID)
	recovered.Release()
	old := loadOffPeakStoreRecord(t, original.ID)
	require.True(t, old.Closed)
	require.Equal(t, "expired", old.State)
	require.Empty(t, old.Snapshot)
	current := currentOffPeakManagerRecord(t, owner.ID)
	require.NotEqual(t, original.ID, current.ID)
	require.NotEqual(t, original.TaskID, current.TaskID)
	calls := secondClient.snapshot()
	require.Len(t, calls, 3)
	require.Equal(t, []string{"status", "take", "status"}, []string{calls[0].Operation, calls[1].Operation, calls[2].Operation})
	require.Equal(t, []string{"expiring-ticket"}, calls[0].TicketIDs)
	require.Equal(t, []string{"fresh-ticket"}, calls[2].TicketIDs)
	assertOffPeakManagerCapturedOwner(t, append(firstClient.snapshot(), calls...), route)
}

func TestZhipuOffPeakManagerPostgresSettledTicketDoesNotRestoreTakeQuota(t *testing.T) {
	owner, route := newOffPeakManagerIntegrationOwner(t)
	firstClient := &offPeakManagerScriptClient{take: func(task string) (*zcode.OffPeakTicket, error) {
		return &zcode.OffPeakTicket{TicketID: "settled-quota-ticket", TaskID: task, State: zcode.TicketReady}, nil
	}, status: func([]string) ([]zcode.OffPeakTicket, time.Duration, error) {
		return []zcode.OffPeakTicket{{TicketID: "settled-quota-ticket", State: zcode.TicketReady}}, 0, nil
	}}
	first := newOffPeakManagerIntegrationManager(t, firstClient, 2*time.Second)
	ticket, err := first.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	ticket.Release()
	first.Stop()
	old := currentOffPeakManagerRecord(t, owner.ID)
	nextTake := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	secondClient := &offPeakManagerScriptClient{status: func(ids []string) ([]zcode.OffPeakTicket, time.Duration, error) {
		require.Equal(t, []string{"settled-quota-ticket"}, ids)
		return []zcode.OffPeakTicket{{TicketID: "settled-quota-ticket", State: zcode.TicketSettled}}, 0, nil
	}, take: func(task string) (*zcode.OffPeakTicket, error) {
		require.NotEqual(t, old.TaskID, task)
		return nil, &zcode.OffPeakError{HTTPStatus: 429, Code: zcode.OffPeakCodeQuotaExhausted, NextTakeAt: nextTake}
	}}
	second := newOffPeakManagerIntegrationManager(t, secondClient, 2*time.Second)
	recovered, err := second.TicketForRequest(context.Background(), owner)
	require.Error(t, err)
	require.Nil(t, recovered)
	require.Equal(t, http.StatusTooManyRequests, infraerrors.Code(err))
	require.Equal(t, "ZHIPU_OFFPEAK_QUOTA_EXHAUSTED", infraerrors.Reason(err))
	require.Contains(t, infraerrors.Message(err), "2030-01-01T00:00:00Z")
	calls := secondClient.snapshot()
	require.Len(t, calls, 2)
	require.Equal(t, "status", calls[0].Operation)
	require.Equal(t, "take", calls[1].Operation)
	terminal := loadOffPeakStoreRecord(t, old.ID)
	require.True(t, terminal.Closed)
	require.Equal(t, "settled", terminal.State)
	current := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, "new", current.State)
	require.Empty(t, current.TicketID)
	require.False(t, current.Closed)
	assertOffPeakManagerCapturedOwner(t, append(firstClient.snapshot(), calls...), route)
	var leases int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", current.ID).Scan(&leases))
	require.Zero(t, leases, "settlement does not create fresh model admission when Take returns 3103")
}
