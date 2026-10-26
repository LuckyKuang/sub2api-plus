//go:build unit

package zcode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Official packages/services/src/session/offPeakServerClient.ts request()
// returns bare JSON and unwraps only {code:0,data}; its consumers require these
// concrete admission and quota fields independently of gateway internals.
func TestOffPeakOfficialBareResponseMaintainsAdmissionAndQuota(t *testing.T) {
	_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body string
		switch r.URL.Path {
		case "/off-peak/ticket/availability":
			body = `{"can_take_number":false,"next_take_at":1800000000000}`
		case "/off-peak/ticket":
			body = `{"ticket_id":"official-ticket","state":"queued","position":4}`
		case "/off-peak/ticket/status":
			body = `{"next_poll_after":3,"tickets":[{"ticket_id":"official-ticket","state":"ready","position":null}]}`
		default:
			t.Errorf("unexpected ticket endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, body)
	})
	auth := OffPeakAuth{JWT: "owner-jwt", PlanKey: "owner-pat"}
	t.Run("availability", func(t *testing.T) {
		value, err := client.Availability(context.Background(), auth)
		require.NoError(t, err)
		require.False(t, value.CanTakeNumber)
		require.Equal(t, time.Unix(1800000000, 0), value.NextTakeAt)
	})
	t.Run("take", func(t *testing.T) {
		value, err := client.TakeTicket(context.Background(), "official-task", auth)
		require.NoError(t, err)
		require.Equal(t, "official-ticket", value.TicketID)
		require.Equal(t, TicketQueued, value.State)
		require.NotNil(t, value.Position)
		require.Equal(t, 4, *value.Position)
	})
	t.Run("status", func(t *testing.T) {
		values, delay, err := client.TicketStatus(context.Background(), []string{"official-ticket"}, auth)
		require.NoError(t, err)
		require.Len(t, values, 1)
		require.Equal(t, "official-ticket", values[0].TicketID)
		require.Equal(t, TicketReady, values[0].State)
		require.Nil(t, values[0].Position)
		require.Equal(t, 3*time.Second, delay)
	})
}

// Official request() turns an empty 2xx body into {}, and settleResponseSchema
// accepts that acknowledgement. It cannot manufacture successful admission.
func TestOffPeakOfficialEmptySettleAcknowledgement(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			_, client := offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
			auth := OffPeakAuth{JWT: "owner-jwt", PlanKey: "owner-pat"}
			require.NoError(t, client.SettleTicket(context.Background(), "official-ticket", auth))
			ticket, err := client.TakeTicket(context.Background(), "task", auth)
			require.Error(t, err)
			require.Nil(t, ticket)
		})
	}
}
