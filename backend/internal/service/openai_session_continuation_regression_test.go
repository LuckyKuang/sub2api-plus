//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// V20/V21: a new owner needs actual tool-call history. Opaque item references
// and an unrelated call cannot supply it, including through protocol adapters.
// Expected outcomes come from the cross-owner continuity requirement; inspect
// the final transport and durable owner instead of conversion helper results.
func TestCodexContinuationDependencyWireRegression(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, endpoint := range []string{"responses", "responses-shaped-chat", "chat", "messages"} {
			for _, scenario := range []string{"complete", "missing", "partial", "reference", "sibling-response"} {
				if (scenario == "reference" || scenario == "sibling-response") && (endpoint == "chat" || endpoint == "messages") {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%s", kind, endpoint, scenario), func(t *testing.T) {
					ctx := context.Background()
					store := newFakeOpenAISessionRouteStore()
					_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
					require.NoError(t, err)
					wire := &httpUpstreamRecorder{resp: codexLogicalIdentityAuditResponse()}
					svc := newOpenAIRejectedFieldTestService(wire)
					a := newOpenAIRejectedFieldTestAccount()
					if kind == AccountTypeOAuth {
						a = newOpenAIOAuthNamespaceTestAccount()
					}
					a.ID, a.Status, a.Schedulable = 102, StatusActive, true
					svc.sessionRouteStore, svc.cache = store, &schedulerTestGatewayCache{}
					svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{{ID: 101, Platform: PlatformOpenAI, Type: kind, Status: StatusError}, *a}}
					svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
					selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
					require.NoError(t, err)
					defer selected.ReleaseFunc()
					ctx = ContextWithSelectionProfitGate(ctx, selected)
					callID := "call_1"
					if scenario == "partial" {
						callID = "unrelated_call"
					}
					call := fmt.Sprintf(`{"type":"function_call","call_id":%q,"name":"lookup","arguments":"{}"},`, callID)
					if scenario == "missing" {
						call = ""
					}
					if scenario == "reference" {
						call = `{"type":"item_reference","id":"call_1"},`
					}
					body := []byte(`{"model":"gpt-5.1","stream":true,"input":[` + call + `{"type":"function_call_output","call_id":"call_1","output":"result"}]}`)
					if scenario == "sibling-response" {
						body = []byte(`{"model":"gpt-5.1","stream":true,"input":"delta","previous_response_id":"resp_a","response":{}}`)
					}
					if endpoint == "chat" {
						call = fmt.Sprintf(`{"role":"assistant","tool_calls":[{"id":%q,"type":"function","function":{"name":"lookup","arguments":"{}"}}]},`, callID)
						if scenario == "missing" {
							call = ""
						}
						body = []byte(`{"model":"gpt-5.1","stream":true,"messages":[` + call + `{"role":"tool","tool_call_id":"call_1","content":"result"}]}`)
					}
					if endpoint == "messages" {
						call = fmt.Sprintf(`{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":"lookup","input":{}}]},`, callID)
						if scenario == "missing" {
							call = ""
						}
						body = []byte(`{"model":"gpt-5.1","stream":true,"max_tokens":16,"messages":[` + call + `{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"result"}]}]}`)
					}
					c := newOpenAIRejectedFieldTestContext(body)
					c.Request = c.Request.WithContext(ctx)
					switch endpoint {
					case "responses":
						_, err = svc.Forward(ctx, c, selected.Account, body)
					case "chat", "responses-shaped-chat":
						_, err = svc.ForwardAsChatCompletions(ctx, c, selected.Account, body, "session", "")
					case "messages":
						_, err = svc.ForwardAsAnthropic(ctx, c, selected.Account, body, "session", "")
					}
					if scenario == "complete" {
						require.NoError(t, err)
						require.Len(t, wire.requests, 1)
						call := gjson.GetBytes(wire.lastBody, `input.#(type=="function_call")`)
						output := gjson.GetBytes(wire.lastBody, `input.#(type=="function_call_output")`)
						require.Equal(t, "lookup", call.Get("name").String())
						require.Equal(t, "{}", call.Get("arguments").String())
						require.Equal(t, "result", output.Get("output").String())
						require.NotEmpty(t, call.Get("call_id").String())
						require.Equal(t, call.Get("call_id").String(), output.Get("call_id").String(), "the target protocol may rename IDs but must preserve dependency closure")
					} else {
						var failure *UpstreamFailoverError
						require.True(t, errors.As(err, &failure), "%v", err)
						require.Equal(t, 400, failure.ClientStatusCode)
						require.Equal(t, NextAccountStop, failure.NextAccountAction)
						require.Empty(t, wire.requests, "missing tool history must stop before candidate I/O")
					}
					route, getErr := store.Get(context.Background(), "group:0", OpenAISessionRouteDigest("session"))
					require.NoError(t, getErr)
					wantOwner := int64(101)
					if scenario == "complete" {
						wantOwner = 102
					}
					require.Equal(t, wantOwner, route.OwnerAccountID)
				})
			}
		}
	}
}
