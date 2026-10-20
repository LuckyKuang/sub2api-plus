//go:build unit || !integration

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// V12: empty output containers do not prove semantic acceptance. Exercise
// buffered JSON and SSE through the actual Responses transport and writer.
func TestCodexResponsesAcceptanceWireRegression(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, pass := range []bool{false, true} {
			for _, outcome := range []string{"empty-shell", "usage", "text"} {
				t.Run(fmt.Sprintf("stream=%v/pass=%v/%s", stream, pass, outcome), func(t *testing.T) {
					ctx := context.Background()
					store := newFakeOpenAISessionRouteStore()
					_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
					require.NoError(t, err)
					output := `[{"type":"message","role":"assistant","content":[]}]`
					// The ordinary JSON parser requires a usage object. Zero
					// counts keep this fixture valid without proving acceptance.
					usage := `,"usage":{"input_tokens":0,"output_tokens":0}`
					if outcome == "usage" {
						usage = `,"usage":{"input_tokens":1,"output_tokens":1}`
					}
					if outcome == "text" {
						output = `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"accepted"}]}]`
					}
					payload := `{"id":"resp_b","status":"completed","output":` + output + usage + `}`
					if stream {
						payload = "data: {\"type\":\"response.completed\",\"response\":" + payload + "}\n\n"
					}
					wire := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(http.StatusOK, payload)}
					if stream {
						wire.resp.Header.Set("Content-Type", "text/event-stream")
					}
					svc := newOpenAIRejectedFieldTestService(wire)
					a := *newOpenAIRejectedFieldTestAccount()
					a.ID, a.Status, a.Schedulable = 102, StatusActive, true
					a.Extra["openai_passthrough"] = pass
					svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
					svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError}, a}}
					svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
					selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					defer selected.ReleaseFunc()
					ctx = ContextWithSelectionProfitGate(ctx, selected)
					body := []byte(fmt.Sprintf(`{"model":"gpt-5.1","stream":%v,"input":"complete root"}`, stream))
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request = c.Request.WithContext(ctx)
					_, err = svc.Forward(ctx, c, selected.Account, body)
					wantOwner := int64(102)
					if outcome == "empty-shell" {
						require.Error(t, err)
						require.False(t, c.Writer.Written(), "an empty output container cannot emit successful acceptance")
						wantOwner = 101
					} else {
						require.NoError(t, err)
						require.True(t, c.Writer.Written())
					}
					require.Len(t, wire.requests, 1)
					route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
					require.NoError(t, err)
					require.Equal(t, wantOwner, route.OwnerAccountID)
				})
			}
		}
	}
}
