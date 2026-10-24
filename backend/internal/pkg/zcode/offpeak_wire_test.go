//go:build unit

package zcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Official offPeakServerClient fixtures use bare objects; the documented
// gateway envelope is an additional accepted form for the same four endpoints.
func TestOffPeakWireAcceptsBareAndEnvelopeAndUsesPhysicalRequestIDs(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("envelope=%t", wrapped), func(t *testing.T) {
			seen := make(map[string]bool)
			_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) {
				id, err := uuid.Parse(r.Header.Get("x-request-id"))
				require.NoError(t, err)
				require.Equal(t, uuid.Version(4), id.Version())
				require.False(t, seen[id.String()], "each control send owns a fresh ID")
				seen[id.String()] = true
				require.Equal(t, "Bearer plan-jwt", r.Header.Get("Authorization"))
				require.Equal(t, "plan-pat", r.Header.Get("X-Coding-Plan-Api-Key"))
				var body any
				switch {
				case strings.HasSuffix(r.URL.Path, "/availability"):
					body = map[string]any{"can_take_number": true}
				case strings.HasSuffix(r.URL.Path, "/status"):
					body = map[string]any{"next_poll_after": 3, "tickets": []any{map[string]any{"ticket_id": "ticket-1", "state": "ready"}}}
				case strings.HasSuffix(r.URL.Path, "/settle"):
					body = map[string]any{"ticket_id": "ticket-1", "state": "settled"}
				default:
					body = map[string]any{"ticket_id": "ticket-1", "state": "queued", "position": 2}
				}
				if wrapped {
					body = map[string]any{"code": 0, "data": body}
				}
				require.NoError(t, json.NewEncoder(w).Encode(body))
			})
			auth := OffPeakAuth{JWT: "plan-jwt", PlanKey: "plan-pat"}
			availability, err := client.Availability(context.Background(), auth)
			require.NoError(t, err)
			require.True(t, availability.CanTakeNumber)
			ticket, err := client.TakeTicket(context.Background(), "task-1", auth)
			require.NoError(t, err)
			require.Equal(t, "ticket-1", ticket.TicketID)
			require.Equal(t, TicketQueued, ticket.State)
			tickets, delay, err := client.TicketStatus(context.Background(), []string{"ticket-1"}, auth)
			require.NoError(t, err)
			require.Len(t, tickets, 1)
			require.Equal(t, TicketReady, tickets[0].State)
			require.Equal(t, 3*time.Second, delay)
			require.NoError(t, client.SettleTicket(context.Background(), "ticket-1", auth))
			require.Len(t, seen, 4)
		})
	}
}

func TestOffPeakSettleAcceptsEmptySuccessfulResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
			require.NoError(t, client.SettleTicket(context.Background(), "ticket-1", OffPeakAuth{JWT: "jwt"}))
			_, err := client.TakeTicket(context.Background(), "task-1", OffPeakAuth{JWT: "jwt"})
			require.Error(t, err, "an empty success cannot manufacture admission")
		})
	}
}

func TestOffPeakSuccessfulHTTPBusinessRejectionKeepsRetryHints(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       string
		code       int
		nextTakeAt time.Time
		retryAfter time.Duration
	}{
		{"quota", `{"code":3103,"msg":"quota exhausted","next_take_at":1800000000000}`, 3103, time.Unix(1_800_000_000, 0), 0},
		{"nested quota", `{"code":3103,"msg":"quota exhausted","data":{"next_take_at":1800000000000}}`, 3103, time.Unix(1_800_000_000, 0), 0},
		{"queued", `{"code":3105,"msg":"queue busy"}`, 3105, time.Time{}, 7 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.retryAfter != 0 {
					w.Header().Set("Retry-After", "7")
				}
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := client.TakeTicket(context.Background(), "task-1", OffPeakAuth{JWT: "jwt"})
			var failure *OffPeakError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.code, failure.Code)
			require.True(t, tc.nextTakeAt.Equal(failure.NextTakeAt))
			require.Equal(t, tc.retryAfter, failure.RetryAfter)
		})
	}
}

func TestOffPeakSuccessfulHTTPPreservesBusinessRejections(t *testing.T) {
	for _, code := range []any{2007, "2007"} {
		_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"code": code, "msg": "not eligible", "data": map[string]any{"ticket_id": "forbidden", "state": "ready"},
			}))
		})
		ticket, err := client.TakeTicket(context.Background(), "task", OffPeakAuth{JWT: "jwt"})
		require.Nil(t, ticket)
		var failure *OffPeakError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, 2007, failure.Code)
	}
}

func TestControlPreparationReplacesUntrustedRequestIDsEachTime(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://zcode.z.ai/api/v1/off-peak/ticket/availability", nil)
	require.NoError(t, err)
	req.Header["X-REQUEST-ID"] = []string{"caller-id"}
	require.NoError(t, prepareRequest(req))
	first, err := uuid.Parse(req.Header.Get("x-request-id"))
	require.NoError(t, err)
	require.Equal(t, uuid.Version(4), first.Version())
	require.NotContains(t, req.Header, "X-REQUEST-ID")
	require.NoError(t, prepareRequest(req))
	require.NotEqual(t, first.String(), req.Header.Get("x-request-id"))
}
