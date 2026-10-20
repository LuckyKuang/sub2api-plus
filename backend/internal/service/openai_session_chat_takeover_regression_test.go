//go:build unit || !integration

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// V12: a successful no-text Chat terminal needs real usage; an empty choice
// or transport 200 cannot accept a migration. Exercise raw Chat and both
// protocol conversions through their public Forward entrypoints.
func TestCodexChatTerminalTakeoverWireRegression(t *testing.T) {
	for _, endpoint := range []string{"responses", "chat", "messages"} {
		for _, stream := range []bool{false, true} {
			for _, outcome := range []string{"accepted", "empty", "commit-failure"} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", endpoint, stream, outcome), func(t *testing.T) {
					ctx := context.Background()
					store := &affinityFaultStore{fakeOpenAISessionRouteStore: newFakeOpenAISessionRouteStore()}
					if outcome == "commit-failure" {
						store.stage = "commit"
					}
					_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
					require.NoError(t, err)
					usage := `,"usage":{"prompt_tokens":1,"completion_tokens":1}`
					if outcome == "empty" {
						usage = ""
					}
					payload := `{"id":"chatcmpl_b","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]` + usage + `}`
					if stream {
						payload = "data: {\"id\":\"chatcmpl_b\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
							"data: {\"id\":\"chatcmpl_b\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
						if usage != "" {
							payload += "data: {\"id\":\"chatcmpl_b\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n"
						}
						payload += "data: [DONE]\n\n"
					}
					wire := &httpUpstreamRecorder{resp: newOpenAIRejectedFieldTestResponse(http.StatusOK, payload)}
					if stream {
						wire.resp.Header.Set("Content-Type", "text/event-stream")
					}
					svc := newOpenAIRejectedFieldTestService(wire)
					a := *newOpenAIRejectedFieldTestAccount()
					a.ID, a.Status, a.Schedulable = 102, StatusActive, true
					a.Extra = map[string]any{"openai_responses_supported": false}
					svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
					svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError}, a}}
					svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
					selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					defer selected.ReleaseFunc()
					ctx = ContextWithSelectionProfitGate(ctx, selected)
					body := []byte(fmt.Sprintf(`{"model":"gpt-5.1","stream":%v,"input":"complete root"}`, stream))
					if endpoint != "responses" {
						body = []byte(fmt.Sprintf(`{"model":"gpt-5.1","stream":%v,"max_tokens":16,"messages":[{"role":"user","content":"complete root"}]}`, stream))
					}
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request = c.Request.WithContext(ctx)
					switch endpoint {
					case "responses":
						_, err = svc.Forward(ctx, c, selected.Account, body)
					case "chat":
						_, err = svc.ForwardAsChatCompletions(ctx, c, selected.Account, body, "session", "")
					case "messages":
						_, err = svc.ForwardAsAnthropic(ctx, c, selected.Account, body, "session", "")
					}
					if outcome == "accepted" {
						require.NoError(t, err)
						require.True(t, c.Writer.Written())
					} else {
						require.Error(t, err)
						require.False(t, c.Writer.Written(), "unaccepted candidate must not emit a successful terminal")
					}
					require.Len(t, wire.requests, 1)
					require.Equal(t, "/v1/chat/completions", wire.lastReq.URL.Path)
					route, getErr := store.Get(context.Background(), "group:0", OpenAISessionRouteDigest("session"))
					require.NoError(t, getErr)
					wantOwner := int64(101)
					if outcome == "accepted" {
						wantOwner = 102
					}
					require.Equal(t, wantOwner, route.OwnerAccountID)
				})
			}
		}
	}
}
