//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/stretchr/testify/require"
)

// Native transports can refresh after the outer handler acquired T0. The
// final response must carry T1's exact grant back to stream-side effects.
func TestHTTP401NativeTransportPublishesFinalSentGrant(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		for _, recovery := range []string{"pre-expiry", "internal-401"} {
			for _, rotated := range []bool{false, true} {
				name := platform + "/" + recovery + map[bool]string{false: "/unchanged", true: "/concurrent-login"}[rotated]
				t.Run(name, func(t *testing.T) {
					selected := cnRecoveryAccount(platform)
					if recovery == "pre-expiry" {
						selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
					}
					repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotRequestCredentialAccount(selected)}}
					oauth := NewCNOAuthService(nil, repo, nil)
					oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
					refreshes := 0
					oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
						refreshes++
						return cnRecoveryResponse(http.StatusOK, `{"access_token":"native-T1","refresh_token":"native-S1","expires_in":3600,"token_type":"Bearer","scope":"agent.default"}`), nil
					})}
					var sentOwner *Account
					var sent []string
					wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
						sent = append(sent, req.Header.Get("Authorization"))
						if recovery == "internal-401" && len(sent) == 1 {
							return cnRecoveryResponse(http.StatusUnauthorized, `{}`), nil
						}
						require.Equal(t, "Bearer native-T1", req.Header.Get("Authorization"))
						sentOwner = snapshotRequestCredentialAccount(repo.account)
						if rotated {
							repo.account = snapshotRequestCredentialAccount(repo.account)
							repo.account.Credentials["access_token"], repo.account.Credentials["refresh_token"] = "new-login-T2", "new-login-S2"
						}
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"rejected\"}}\n\n"))}, nil
					}}
					cfg := rawChatCompletionsTestConfig()
					blocker := &runtimeBlockRecorder{}
					invalidator := &tokenCacheInvalidatorRecorder{}
					rl := NewRateLimitService(repo, nil, cfg, nil, nil)
					rl.SetAccountRuntimeBlocker(blocker)
					rl.SetTokenCacheInvalidator(invalidator)
					gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, cnOAuthService: oauth, httpUpstream: wire, rateLimitService: rl}
					ctx := WithOutboundIdentityScope(context.Background(), nil)
					token, _, err := gateway.GetAccessToken(ctx, selected)
					require.NoError(t, err)
					ctx = withRequestCredentialOwner(ctx, selected, token)
					c := newOpenAIRejectedFieldTestContext(nil)
					finish := bindRequestCredentialOwnerContext(ctx, c)
					defer finish()
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, selected.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{"messages":[]}`))
					require.NoError(t, err)
					req.Header.Set("Authorization", "Bearer "+token)
					response, err := gateway.doOpenAIUpstream(req, "", selected)
					require.NoError(t, err)
					payload, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					require.Contains(t, string(payload), "authentication_error")
					require.Equal(t, 1, refreshes)
					if recovery == "internal-401" {
						require.Equal(t, []string{"Bearer old", "Bearer native-T1"}, sent)
					} else {
						require.Equal(t, []string{"Bearer native-T1"}, sent)
					}
					evidence, ok := requestCredentialOwner(c.Request.Context(), selected)
					require.True(t, ok)
					require.Equal(t, sentOwner.Credentials, evidence.Credentials)
					status, disable := gateway.handleOpenAIStreamTerminalAccountSideEffects(c, selected, []byte(`{"type":"error","error":{"type":"authentication_error","message":"rejected"}}`), "rejected", nil)
					require.Equal(t, http.StatusUnauthorized, status)
					require.True(t, disable)
					require.Equal(t, 1, repo.cooldown.cooldownCalls)
					require.Equal(t, sentOwner.Credentials, repo.cooldown.attempted.Credentials)
					if rotated {
						require.Nil(t, repo.account.TempUnschedulableUntil)
						require.Empty(t, blocker.accounts)
						require.Empty(t, invalidator.accounts)
					} else {
						require.NotNil(t, repo.account.TempUnschedulableUntil)
						require.Len(t, blocker.accounts, 1)
						require.Len(t, invalidator.accounts, 1)
					}
				})
			}
		}
	}
}

func TestHTTP401TransmissionEvidenceFinalizesOncePerOuterAttempt(t *testing.T) {
	selected := newOpenAIAuthRecoveryTestAccount()
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, selected, "old-token")
	firstAttempt := withRequestCredentialOwner(ctx, selected, "old-token")
	newGrant := snapshotRequestCredentialAccount(selected)
	newGrant.Credentials["access_token"], newGrant.Credentials["refresh_token"] = "new-T1", "new-S1"
	request, err := http.NewRequestWithContext(firstAttempt, http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	recordTransmittedRequestCredentialOwner(request, newGrant, "new-T1")
	finalizeTransmittedRequestCredentialOwner(firstAttempt, request)
	newer := snapshotRequestCredentialAccount(newGrant)
	newer.Credentials["access_token"], newer.Credentials["refresh_token"] = "new-T2", "new-S2"
	secondRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	recordTransmittedRequestCredentialOwner(secondRequest, newer, "new-T2")
	finalizeTransmittedRequestCredentialOwner(firstAttempt, secondRequest)
	owner, ok := requestCredentialOwner(firstAttempt, selected)
	require.True(t, ok)
	require.Equal(t, "new-T1", owner.GetOpenAIAccessToken(), "final transmission evidence is immutable")
	secondAttempt := withRequestCredentialOwner(ctx, selected, "new-T2")
	owner, ok = requestCredentialOwner(secondAttempt, selected)
	require.True(t, ok)
	require.Equal(t, "new-T2", owner.GetOpenAIAccessToken())
	owner, ok = requestCredentialOwner(firstAttempt, selected)
	require.True(t, ok)
	require.Equal(t, "new-T1", owner.GetOpenAIAccessToken())
}

func TestHTTP401NativePublicEntrypointsUseActuallySentGeneration(t *testing.T) {
	for _, endpoint := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, phase := range []string{"preflight", "after-output"} {
			for _, rotated := range []bool{false, true} {
				name := endpoint + "/" + phase + map[bool]string{false: "/unchanged", true: "/concurrent-login"}[rotated]
				t.Run(name, func(t *testing.T) {
					selected := cnRecoveryAccount(PlatformKimi)
					selected.Schedulable = true
					selected.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
					repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotRequestCredentialAccount(selected)}}
					oauth := NewCNOAuthService(nil, repo, nil)
					oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
					refreshes := 0
					oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
						refreshes++
						return cnRecoveryResponse(http.StatusOK, fmt.Sprintf(`{"access_token":"native-T%d","refresh_token":"native-S%d","expires_in":3600}`, refreshes, refreshes)), nil
					})}
					var sentOwner *Account
					var sent []string
					wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
						sent = append(sent, req.Header.Get("Authorization"))
						sentOwner = snapshotRequestCredentialAccount(repo.account)
						if rotated && (phase == "after-output" || len(sent) == 2) {
							repo.account = snapshotRequestCredentialAccount(repo.account)
							repo.account.Credentials["access_token"], repo.account.Credentials["refresh_token"] = "new-login-T9", "new-login-S9"
						}
						if phase == "preflight" {
							return nativeSSETestResponse(nativeStructuralPrelude + nativeErrorSSE("authentication_error")), nil
						}
						body := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0] + nativeErrorSSE("authentication_error")
						return nativeSSETestResponse(body), nil
					}}
					cfg := rawChatCompletionsTestConfig()
					blocker := &runtimeBlockRecorder{}
					invalidator := &tokenCacheInvalidatorRecorder{}
					rl := NewRateLimitService(repo, nil, cfg, nil, nil)
					rl.SetAccountRuntimeBlocker(blocker)
					rl.SetTokenCacheInvalidator(invalidator)
					gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, cnOAuthService: oauth, httpUpstream: wire, rateLimitService: rl}
					body := []byte(`{"model":"k3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
					if endpoint == "/v1/responses" {
						body = []byte(`{"model":"k3","input":"hello","stream":true}`)
					}
					c := newOpenAIRejectedFieldTestContext(body)
					var result *OpenAIForwardResult
					var err error
					switch endpoint {
					case "/v1/messages":
						result, err = gateway.ForwardAsAnthropic(context.Background(), c, selected, body, "", "")
					case "/v1/chat/completions":
						result, err = gateway.ForwardAsChatCompletions(context.Background(), c, selected, body, "", "")
					default:
						result, err = gateway.Forward(context.Background(), c, selected, body)
					}
					require.Error(t, err)
					if phase == "preflight" {
						require.Equal(t, []string{"Bearer native-T1", "Bearer native-T2"}, sent)
						var failover *UpstreamFailoverError
						require.ErrorAs(t, err, &failover)
						require.True(t, failover.ShouldRetryNextAccount())
						require.False(t, c.Writer.Written())
						require.Equal(t, 1, repo.fence.casCalls)
						require.Equal(t, sentOwner.Credentials, repo.fence.attempted.Credentials)
						require.Zero(t, repo.fence.unconditionalCalls)
						require.Zero(t, repo.cooldown.cooldownCalls)
					} else {
						require.Equal(t, []string{"Bearer native-T1"}, sent)
						var failover *UpstreamFailoverError
						require.NotErrorAs(t, err, &failover, "actual output cannot be replayed on another account")
						require.True(t, c.Writer.Written())
						require.NotNil(t, result)
						require.True(t, result.UsageIncomplete)
						require.Equal(t, 1, repo.cooldown.cooldownCalls)
						require.Equal(t, sentOwner.Credentials, repo.cooldown.attempted.Credentials)
					}
					if rotated {
						require.Equal(t, StatusActive, repo.account.Status)
						require.True(t, repo.account.Schedulable)
						require.Nil(t, repo.account.TempUnschedulableUntil)
						require.Empty(t, blocker.accounts)
						require.Empty(t, invalidator.accounts)
					} else {
						if phase == "preflight" {
							require.Equal(t, StatusError, repo.account.Status)
							require.False(t, repo.account.Schedulable)
							// Typed credential failures publish durable scheduling
							// invalidation through the CAS repository, rather than
							// the RateLimitService's runtime-block recorder.
						} else {
							require.NotNil(t, repo.account.TempUnschedulableUntil)
							require.Len(t, blocker.accounts, 1)
						}
					}
				})
			}
		}
	}
}

// Once meaningful output exists, terminal errors cannot authorize a replay.
// Repeated account errors apply one cooldown; shared or unknown errors retain
// the observed usage without changing the selected account's health.
func TestHTTP401NativePublicErrorSideEffectsApplyOnceAndRespectSharedFailures(t *testing.T) {
	for _, endpoint := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, scenario := range []struct {
			name         string
			kind         string
			accountError bool
		}{
			{name: "repeated-authentication", kind: "authentication_error", accountError: true},
			{name: "unknown-error", kind: "future_shared_error"},
			{name: "shared-overload", kind: "overloaded_error"},
		} {
			t.Run(endpoint+"/"+scenario.name, func(t *testing.T) {
				selected := cnRecoveryAccount(PlatformKimi)
				selected.Schedulable = true
				repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotRequestCredentialAccount(selected)}}
				oauth := NewCNOAuthService(nil, repo, nil)
				oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
				refreshes := 0
				oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
					refreshes++
					return cnRecoveryResponse(http.StatusOK, `{"access_token":"unexpected-refresh","refresh_token":"unexpected-refresh-grant","expires_in":3600}`), nil
				})}
				// Keep the authoritative 10 input / 5 output usage, followed by
				// two identical errors and a real Anthropic transport terminal.
				stream := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0] +
					nativeErrorSSE(scenario.kind) + nativeErrorSSE(scenario.kind) +
					"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
				var sent []string
				wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
					sent = append(sent, req.Header.Get("Authorization"))
					return nativeSSETestResponse(stream), nil
				}}
				cfg := rawChatCompletionsTestConfig()
				blocker := &runtimeBlockRecorder{}
				invalidator := &tokenCacheInvalidatorRecorder{}
				rl := NewRateLimitService(repo, nil, cfg, nil, nil)
				rl.SetAccountRuntimeBlocker(blocker)
				rl.SetTokenCacheInvalidator(invalidator)
				gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, cnOAuthService: oauth, httpUpstream: wire, rateLimitService: rl}
				body := []byte(`{"model":"k3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
				if endpoint == "/v1/responses" {
					body = []byte(`{"model":"k3","input":"hello","stream":true}`)
				}
				c := newOpenAIRejectedFieldTestContext(body)
				var result *OpenAIForwardResult
				var err error
				switch endpoint {
				case "/v1/messages":
					result, err = gateway.ForwardAsAnthropic(context.Background(), c, selected, body, "", "")
				case "/v1/chat/completions":
					result, err = gateway.ForwardAsChatCompletions(context.Background(), c, selected, body, "", "")
				default:
					result, err = gateway.Forward(context.Background(), c, selected, body)
				}
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover, "actual output cannot be replayed on another account")
				require.Equal(t, []string{"Bearer old"}, sent, "terminal stream errors cannot resend the model request")
				require.Zero(t, refreshes, "terminal stream errors cannot replay through credential recovery")
				require.True(t, c.Writer.Written())
				require.NotNil(t, result)
				require.True(t, result.UsageIncomplete)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 5, result.Usage.OutputTokens)
				require.Zero(t, repo.fence.casCalls, "stream errors cannot permanently quarantine a recoverable or unrelated grant")
				require.Zero(t, repo.fence.unconditionalCalls)
				require.Zero(t, repo.cooldown.unconditionalCalls)
				require.Zero(t, repo.cooldown.unconditionalCooldowns)
				require.Equal(t, StatusActive, repo.account.Status)
				require.True(t, repo.account.Schedulable)
				if scenario.accountError {
					require.Equal(t, 1, repo.cooldown.cooldownCalls, "duplicate authentication frames apply the credential cooldown once")
					require.Equal(t, selected.ID, repo.cooldown.attempted.ID)
					require.Equal(t, selected.Credentials, repo.cooldown.attempted.Credentials)
					require.NotNil(t, repo.account.TempUnschedulableUntil)
					require.Len(t, blocker.accounts, 1)
					require.Len(t, invalidator.accounts, 1)
				} else {
					require.Zero(t, repo.cooldown.cooldownCalls, "shared and unknown errors cannot punish the account")
					require.Nil(t, repo.account.TempUnschedulableUntil)
					require.Empty(t, blocker.accounts)
					require.Empty(t, invalidator.accounts)
				}
			})
		}
	}
}

// A recognized provider authentication envelope remains a failure when the
// native Messages transport returns HTTP 200 with application/json.
func TestHTTP401NativeMessagesBufferedErrorCannotBecomeSuccessfulResult(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		name := map[bool]string{false: "unchanged", true: "concurrent-login"}[rotated]
		t.Run(name, func(t *testing.T) {
			selected := cnRecoveryAccount(PlatformKimi)
			selected.Schedulable = true
			selected.Credentials["access_token"], selected.Credentials["refresh_token"] = "native-json-T1", "native-json-S1"
			repo := &http401RefreshEvidenceRepo{refreshAPIAccountRepo: &refreshAPIAccountRepo{account: snapshotRequestCredentialAccount(selected)}}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				refreshes++
				return cnRecoveryResponse(http.StatusOK, `{"access_token":"unexpected-refresh","refresh_token":"unexpected-refresh-grant","expires_in":3600}`), nil
			})}
			var sent []string
			var sentOwner *Account
			wire := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
				sent = append(sent, req.Header.Get("Authorization"))
				sentOwner = snapshotRequestCredentialAccount(repo.account)
				if rotated {
					repo.account = snapshotRequestCredentialAccount(repo.account)
					repo.account.Credentials["access_token"], repo.account.Credentials["refresh_token"] = "new-login-T2", "new-login-S2"
				}
				resp := cnRecoveryResponse(http.StatusOK, `{"type":"error","error":{"type":"authentication_error","message":"rejected"}}`)
				resp.Header.Set("Content-Type", "application/json")
				return resp, nil
			}}
			cfg := rawChatCompletionsTestConfig()
			blocker := &runtimeBlockRecorder{}
			invalidator := &tokenCacheInvalidatorRecorder{}
			rl := NewRateLimitService(repo, nil, cfg, nil, nil)
			rl.SetAccountRuntimeBlocker(blocker)
			rl.SetTokenCacheInvalidator(invalidator)
			gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, cnOAuthService: oauth, httpUpstream: wire, rateLimitService: rl}
			body := []byte(`{"model":"k3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
			c := newOpenAIRejectedFieldTestContext(body)
			result, err := gateway.ForwardAsAnthropic(context.Background(), c, selected, body, "", "")
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.True(t, failover.ShouldRetryNextAccount())
			require.Nil(t, result, "an authentication error cannot create a billable successful result")
			require.False(t, c.Writer.Written(), "the handler must retain the opportunity to select another account")
			require.Equal(t, []string{"Bearer native-json-T1"}, sent)
			require.Zero(t, refreshes)
			require.Equal(t, 1, repo.cooldown.cooldownCalls)
			require.Equal(t, sentOwner.ID, repo.cooldown.attempted.ID)
			require.Equal(t, sentOwner.Credentials, repo.cooldown.attempted.Credentials)
			require.Zero(t, repo.fence.casCalls, "a recoverable grant receives a cooldown, not permanent quarantine")
			require.Zero(t, repo.fence.unconditionalCalls)
			require.Zero(t, repo.cooldown.unconditionalCalls)
			require.Zero(t, repo.cooldown.unconditionalCooldowns)
			require.Equal(t, StatusActive, repo.account.Status)
			require.True(t, repo.account.Schedulable)
			if rotated {
				require.Equal(t, "new-login-T2", repo.account.GetCredential("access_token"))
				require.Equal(t, "new-login-S2", repo.account.GetCredential("refresh_token"))
				require.Nil(t, repo.account.TempUnschedulableUntil)
				require.Empty(t, blocker.accounts)
				require.Empty(t, invalidator.accounts, "a late JSON error cannot invalidate the new login's token cache")
			} else {
				require.NotNil(t, repo.account.TempUnschedulableUntil)
				require.Len(t, blocker.accounts, 1)
				require.Len(t, invalidator.accounts, 1)
			}
		})
	}
}
