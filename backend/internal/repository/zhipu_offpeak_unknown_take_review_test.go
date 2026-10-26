//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

// Only local TLS trust is supplied. Request encoding, response parsing and
// identity application are the production protocol client's implementation.
// This fixture uses an explicitly unbound account, so no proxy is substituted.
type offPeakUnknownTakeReviewWireClient struct {
	client *zcode.OffPeakClient
}

func (c offPeakUnknownTakeReviewWireClient) Availability(ctx context.Context, auth zcode.OffPeakAuth, _ string) (*zcode.OffPeakAvailability, error) {
	return c.client.Availability(ctx, auth)
}
func (c offPeakUnknownTakeReviewWireClient) TakeTicket(ctx context.Context, task string, auth zcode.OffPeakAuth, _ string) (*zcode.OffPeakTicket, error) {
	return c.client.TakeTicket(ctx, task, auth)
}
func (c offPeakUnknownTakeReviewWireClient) TicketStatus(ctx context.Context, ids []string, auth zcode.OffPeakAuth, _ string) ([]zcode.OffPeakTicket, time.Duration, error) {
	return c.client.TicketStatus(ctx, ids, auth)
}
func (c offPeakUnknownTakeReviewWireClient) SettleTicket(ctx context.Context, id string, auth zcode.OffPeakAuth, _ string) error {
	return c.client.SettleTicket(ctx, id, auth)
}

func TestZhipuOffPeakManagerPostgresUnknownTakeStateRetainsKnownTicketForRestart(t *testing.T) {
	for _, scenario := range []struct {
		name, stateField string
	}{
		{name: "unknown-state", stateField: `,"state":"future_state"`},
		{name: "missing-state"},
		{name: "empty-state", stateField: `,"state":""`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			exerciseOffPeakUnknownTakeReview(t, scenario.stateField)
		})
	}
}

func exerciseOffPeakUnknownTakeReview(t *testing.T, stateField string) {
	t.Helper()
	owner, _ := newOffPeakManagerIntegrationOwner(t)
	_, err := integrationDB.ExecContext(context.Background(), "UPDATE accounts SET proxy_id=NULL WHERE id=$1", owner.ID)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	owner, err = repo.GetByID(context.Background(), owner.ID)
	require.NoError(t, err)
	route, err := owner.StrictProxyURL()
	require.NoError(t, err)
	require.Empty(t, route)
	const remoteID = "known-id-unknown-take-state"
	var mu sync.Mutex
	var operations []string
	var originalTask string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "Bearer manager-owner-jwt", r.Header.Get("Authorization"))
		require.Equal(t, "manager-plan-key", r.Header.Get("X-Coding-Plan-Api-Key"))
		require.Equal(t, "ZCode/3.16.2", r.UserAgent())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/off-peak/ticket":
			operations = append(operations, "take")
			var body struct {
				TaskID string `json:"task_id"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.NotEmpty(t, body.TaskID)
			originalTask = body.TaskID
			_, _ = io.WriteString(w, `{"code":0,"data":{"ticket_id":"`+remoteID+`"`+stateField+`}}`)
		case "/off-peak/ticket/status":
			operations = append(operations, "status")
			var body struct {
				TicketIDs []string `json:"ticket_ids"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, []string{remoteID}, body.TicketIDs)
			_, _ = io.WriteString(w, `{"code":0,"data":{"tickets":[{"ticket_id":"`+remoteID+`","state":"ready"}]}}`)
		default:
			t.Errorf("unexpected off-peak operation %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	wire := offPeakUnknownTakeReviewWireClient{client: zcode.NewOffPeakClient(server.Client(), server.URL)}
	first := newOffPeakManagerIntegrationManager(t, wire, 2*time.Second)
	ticket, err := first.TicketForRequest(context.Background(), owner)
	require.Error(t, err, "an unknown Take state cannot grant physical model admission")
	require.Nil(t, ticket)
	original := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, remoteID, original.TicketID, "a known remote ticket must remain recoverable even when its admission state is not understood")
	require.Equal(t, "uncertain", original.State)
	require.NotEmpty(t, original.Snapshot)
	require.False(t, original.Retired)
	require.False(t, original.Closed)
	require.Empty(t, original.LeaseID)
	var leases int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_request_leases WHERE ticket_record_id=$1", original.ID).Scan(&leases))
	require.Zero(t, leases)
	mu.Lock()
	require.Equal(t, []string{"take"}, operations, "unknown Take state returns without inventing admission")
	require.Equal(t, originalTask, original.TaskID)
	mu.Unlock()
	first.Stop()

	restarted := newOffPeakManagerIntegrationManager(t, wire, 2*time.Second)
	recovered, err := restarted.TicketForRequest(context.Background(), owner)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.Equal(t, remoteID, recovered.TicketID)
	recovered.Release()
	retained := currentOffPeakManagerRecord(t, owner.ID)
	require.Equal(t, original.ID, retained.ID)
	require.Equal(t, original.TaskID, retained.TaskID)
	require.Equal(t, original.Generation, retained.Generation)
	require.Equal(t, original.Snapshot, retained.Snapshot)
	mu.Lock()
	require.Equal(t, []string{"take", "status"}, operations, "restart revalidates the known original ticket without a second Take")
	mu.Unlock()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT count(*) FROM zhipu_offpeak_tickets WHERE account_id=$1", owner.ID).Scan(&count))
	require.Equal(t, 1, count)
	var _ service.ZhipuOffPeakClient = wire
}
