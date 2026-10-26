//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStepFunCapabilityFailuresNeverBecomeSuccess(t *testing.T) {
	for _, profile := range stepCapabilityProfiles {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusOK} {
				t.Run(fmt.Sprintf("%s/%s/status=%d", profile.name, protocol, status), func(t *testing.T) {
					account := stepFunTestAccount(profile.kind, profile.region)
					account.Credentials["account_mode"] = profile.mode
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"upstream_unavailable","message":"upstream failed"}}`))}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					result, recorder, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, false, 1))
					require.Error(t, err, "an explicit provider error remains failure even when HTTP is 200")
					require.Nil(t, result, "an explicit rejection supplies no successful inference usage")
					require.Len(t, upstream.requests, 1, "service adapter does not secretly repeat rejected requests")
					require.NotContains(t, recorder.Body.String(), `"status":"completed"`)
					require.NotContains(t, recorder.Body.String(), `"stop_reason":"end_turn"`)
				})
			}
		}
	}
}

func TestStepFunCapabilityStreamFailureAndTerminationContract(t *testing.T) {
	text := "data: " + `{"id":"chat_step_boundary","object":"chat.completion.chunk","model":"step-3.7-flash","choices":[{"index":0,"delta":{"content":"partial answer"},"finish_reason":null}]}` + "\n\n"
	finish := "data: " + `{"id":"chat_step_boundary","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
	usage := "data: " + `{"id":"chat_step_boundary","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":6,"total_tokens":17}}` + "\n\n"
	for _, profile := range stepCapabilityProfiles {
		for _, protocol := range []string{"chat", "responses", "messages"} {
			for _, scenario := range []struct {
				name, body                                     string
				readError                                      error
				wantText, rawSuccess, bridgeSuccess, wantUsage bool
				cancelled                                      bool
			}{
				{name: "empty_connection"},
				{name: "partial_clean_eof", body: text, wantText: true},
				{name: "partial_read_failure", body: text, readError: io.ErrUnexpectedEOF, wantText: true},
				{name: "error_envelope", body: "data: {\"error\":{\"type\":\"upstream_unavailable\",\"message\":\"upstream failed\"}}\n\ndata: [DONE]\n\n"},
				{name: "error_after_output_and_usage", body: text + usage + "data: {\"error\":{\"type\":\"upstream_unavailable\",\"message\":\"upstream failed\"}}\n\ndata: [DONE]\n\n", wantText: true, wantUsage: true},
				{name: "cancelled_after_usage", body: text + usage, readError: context.Canceled, wantText: true, rawSuccess: true, wantUsage: true, cancelled: true},
				{name: "deadline_after_usage", body: text + usage, readError: context.DeadlineExceeded, wantText: true, rawSuccess: true, wantUsage: true, cancelled: true},
				{name: "complete_without_usage", body: text + finish + "data: [DONE]\n\n", wantText: true, rawSuccess: true, bridgeSuccess: true},
				{name: "missing_done_with_finish", body: text + finish, wantText: true, rawSuccess: true},
				{name: "missing_done_with_usage", body: text + usage, wantText: true, rawSuccess: true, wantUsage: true},
				{name: "complete_exact_usage", body: text + finish + usage + "data: [DONE]\n\n", wantText: true, rawSuccess: true, bridgeSuccess: true, wantUsage: true},
			} {
				t.Run(profile.name+"/"+protocol+"/"+scenario.name, func(t *testing.T) {
					account := stepFunTestAccount(profile.kind, profile.region)
					account.Credentials["account_mode"] = profile.mode
					_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
					var body io.ReadCloser = io.NopCloser(strings.NewReader(scenario.body))
					if scenario.readError != nil {
						body = &errTailReader{data: []byte(scenario.body), err: scenario.readError}
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: NewCNOAuthService(nil, nil, nil)}
					result, recorder, err := stepCapabilityForward(t, svc, ctx, account, protocol, stepCapabilityRequest(protocol, true, 1))
					success := scenario.bridgeSuccess
					if protocol == "chat" {
						success = scenario.rawSuccess
					}
					if success {
						require.NoError(t, err)
						require.NotNil(t, result)
					} else {
						require.Error(t, err)
					}
					require.Len(t, upstream.requests, 1, "delivered output must never be replayed by the adapter")
					if scenario.wantText {
						require.NotNil(t, result)
						require.Contains(t, recorder.Body.String(), "partial answer")
						var failover *UpstreamFailoverError
						require.False(t, errors.As(err, &failover), "semantic output forbids account replay")
					}
					if result != nil {
						require.Equal(t, !success || scenario.cancelled, result.UsageIncomplete, "incomplete results cannot be recorded as complete")
						if scenario.wantUsage {
							require.Equal(t, 11, result.Usage.InputTokens)
							require.Equal(t, 6, result.Usage.OutputTokens)
							wantSource := UsageSourceUpstreamExact
							if !success || scenario.cancelled {
								wantSource = UsageSourcePartial
							}
							require.Equal(t, wantSource, result.UsageSource)
						} else {
							require.Zero(t, result.Usage.InputTokens)
							require.Zero(t, result.Usage.OutputTokens)
							require.Equal(t, UsageSourceUnknown, result.UsageSource)
						}
					}
					if !success || scenario.cancelled {
						require.NotContains(t, recorder.Body.String(), "event: response.completed", "failed streams cannot synthesize a success terminal")
						require.NotContains(t, recorder.Body.String(), "event: message_stop", "failed streams cannot synthesize a success terminal")
					}
				})
			}
		}
	}
}
