//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func (r *refreshAPIAccountRepo) UpdateOAuthRefreshCredentialsIfUnchanged(ctx context.Context, expectedOwner *Account, credentials map[string]any) (bool, error) {
	id := expectedOwner.ID
	platform := expectedOwner.Platform
	expected := expectedOwner.Credentials
	proxy := expectedOwner.ProxyID
	r.successCASCalls++
	r.lastExpectedCredentials = shallowCopyMap(expected)
	if proxy != nil {
		value := *proxy
		r.lastExpectedProxyID = &value
	} else {
		r.lastExpectedProxyID = nil
	}
	if r.beforeSuccessCAS != nil {
		r.beforeSuccessCAS(r)
	}
	if r.updateErr != nil {
		return false, r.updateErr
	}
	if r.account == nil || r.account.ID != id || r.account.Platform != platform || r.account.Type != AccountTypeOAuth || (!reflect.DeepEqual(r.account.ProxyID, proxy) || !reflect.DeepEqual(r.account.Proxy, expectedOwner.Proxy)) {
		return false, nil
	}
	currentJSON, currentErr := json.Marshal(r.account.Credentials)
	expectedJSON, expectedErr := json.Marshal(expected)
	if currentErr != nil || expectedErr != nil || string(currentJSON) != string(expectedJSON) {
		return false, nil
	}
	return true, r.UpdateCredentials(ctx, id, credentials)
}

type cnRecoveryUpstream struct {
	send func(*http.Request) (*http.Response, error)
}

func (u *cnRecoveryUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.send(req)
}
func (u *cnRecoveryUpstream) DoWithTLS(req *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, id, c)
}
func cnRecoveryResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func cnRecoveryAccount(platform string) *Account {
	return &Account{ID: 42, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: "cn"}, &cnoauth.Grant{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})}
}

func TestCNOAuth401RecoveryUsesSelectedOwnerOnce(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			a := cnRecoveryAccount(platform)
			repo := &refreshAPIAccountRepo{account: a}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
				refreshes++
				require.Equal(t, 1, refreshes)
				return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
			})}
			calls := 0
			var identity string
			upstream := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
				calls++
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.JSONEq(t, `{"messages":[]}`, string(body))
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				if calls == 1 {
					identity = req.UserAgent()
					require.Equal(t, "Bearer old", req.Header.Get("Authorization"))
					return cnRecoveryResponse(401, `{}`), nil
				}
				require.Equal(t, 2, calls)
				require.Equal(t, identity, req.UserAgent())
				require.Equal(t, "Bearer new", req.Header.Get("Authorization"))
				return cnRecoveryResponse(200, `{}`), nil
			}}
			svc := &OpenAIGatewayService{cnOAuthService: oauth, httpUpstream: upstream}
			req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{"messages":[]}`))
			resp, err := svc.doOpenAIUpstream(req, "", a)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			require.Equal(t, 1, refreshes)
			require.Equal(t, 2, calls)
		})
	}
}

func TestCNOAuth401RecoveryReusesConcurrentRotationAndPreservesReauthorization(t *testing.T) {
	for _, stage := range []string{"before-refresh", "during-refresh"} {
		t.Run(stage, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformKimi)
			repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(a)}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			rotate := func() {
				current := snapshotOAuthRefreshAccount(repo.account)
				current.Credentials["access_token"] = "concurrent"
				current.Credentials["refresh_token"] = "new-login"
				repo.account = current
			}
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
				refreshes++
				rotate()
				return cnRecoveryResponse(200, `{"access_token":"obsolete","refresh_token":"obsolete","expires_in":3600}`), nil
			})}
			calls := 0
			req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{}`))
			resp, err := oauth.roundTripNativeOAuth(req, a, func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if stage == "before-refresh" {
						rotate()
					}
					return cnRecoveryResponse(401, `{}`), nil
				}
				require.Equal(t, "Bearer concurrent", req.Header.Get("Authorization"))
				return cnRecoveryResponse(200, `{}`), nil
			})
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode)
			require.Equal(t, 2, calls)
			if stage == "before-refresh" {
				require.Zero(t, refreshes)
			} else {
				require.Equal(t, 1, refreshes)
			}
			require.Equal(t, "new-login", repo.account.GetCredential("refresh_token"))
			require.Zero(t, repo.updateCredentialsCalls)
		})
	}
}

func TestCNOAuth401RecoveryFailureScopeAndReplayBoundary(t *testing.T) {
	for _, tc := range []struct {
		name        string
		grantErr    error
		status      int
		replay      bool
		wantScope   GatewayFailureScope
		wantRefresh int
		wantSend    int
	}{
		{"invalid grant", nil, 400, true, GatewayFailureScopeAccount, 1, 1},
		{"shared unavailable", errors.New("network unavailable"), 0, true, GatewayFailureScopeProvider, 1, 1},
		{"rejected after refresh", nil, 200, true, GatewayFailureScopeAccount, 1, 2},
		{"unsafe body", nil, 200, false, "", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := cnRecoveryAccount(PlatformKimi)
			repo := &refreshAPIAccountRepo{account: a}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
				refreshes++
				if tc.grantErr != nil {
					return nil, tc.grantErr
				}
				if tc.status == 400 {
					return cnRecoveryResponse(400, `{"error":"invalid_grant"}`), nil
				}
				return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`), nil
			})}
			req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{}`))
			if !tc.replay {
				req.GetBody = nil
			}
			calls := 0
			resp, err := oauth.roundTripNativeOAuth(req, a, func(*http.Request) (*http.Response, error) { calls++; return cnRecoveryResponse(401, `{}`), nil })
			if tc.wantScope == "" {
				require.NoError(t, err)
				require.Equal(t, 401, resp.StatusCode)
			} else {
				var failure *CredentialFailureError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tc.wantScope, failure.Scope)
				if tc.wantScope == GatewayFailureScopeAccount {
					require.NotNil(t, failure.account)
					require.Equal(t, a.ID, failure.account.ID)
				}
			}
			require.Equal(t, tc.wantRefresh, refreshes)
			require.Equal(t, tc.wantSend, calls)
		})
	}
}

func TestCNOAuth401RecoveryCASMissNeverReplaysRejectedToken(t *testing.T) {
	a := cnRecoveryAccount(PlatformKimi)
	repo := &refreshAPIAccountRepo{account: a}
	oauth := NewCNOAuthService(nil, repo, nil)
	oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	repo.beforeSuccessCAS = func(r *refreshAPIAccountRepo) {
		current := snapshotOAuthRefreshAccount(r.account)
		current.Credentials["model_mapping"] = map[string]any{"old": "new"}
		r.account = current
	}
	oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`), nil
	})}
	req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{}`))
	calls := 0
	_, err := oauth.roundTripNativeOAuth(req, a, func(*http.Request) (*http.Response, error) { calls++; return cnRecoveryResponse(401, `{}`), nil })
	var failure *CredentialFailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
	require.Equal(t, GatewayFailureReason("native_oauth_rejected_token_unchanged"), failure.Reason)
	require.Equal(t, 1, calls)
	require.Zero(t, repo.updateCredentialsCalls)
	require.Equal(t, "old", repo.account.GetCredential("access_token"))
}

func TestCNOAuth401RecoveryNeverAdoptsChangedAuthorizationContext(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		for _, stage := range []string{"before-refresh", "during-refresh"} {
			for _, change := range []string{"new-login", "identity", "proxy", "metadata", "disabled", "legacy-login"} {
				t.Run(platform+"/"+stage+"/"+change, func(t *testing.T) {
					a := cnRecoveryAccount(platform)
					if change == "legacy-login" {
						delete(a.Credentials, domesticOAuthLoginIDCredential)
					}
					repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(a)}
					oauth := NewCNOAuthService(nil, repo, nil)
					oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
					changeOwner := func() {
						current := snapshotOAuthRefreshAccount(repo.account)
						current.Credentials["access_token"] = "different-access"
						current.Credentials["refresh_token"] = "different-refresh"
						switch change {
						case "new-login":
							current.Credentials[domesticOAuthLoginIDCredential] = "another-authorization"
						case "identity":
							current.Credentials[outboundIdentityCredential] = map[string]any{"preset": platform, "version": "9.9.9"}
						case "proxy":
							id := int64(71)
							current.ProxyID, current.Proxy = &id, &Proxy{ID: id, Protocol: "http", Host: "another.example", Port: 8080}
						case "metadata":
							current.Credentials["model_mapping"] = map[string]any{"old": "new"}
						case "disabled":
							current.Status = StatusDisabled
						}
						repo.account = current
					}
					refreshes := 0
					oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
						refreshes++
						changeOwner()
						return cnRecoveryResponse(200, `{"access_token":"obsolete","refresh_token":"obsolete","expires_in":3600}`), nil
					})}
					req, err := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{}`))
					require.NoError(t, err)
					sends := 0
					resp, err := oauth.roundTripNativeOAuth(req, a, func(req *http.Request) (*http.Response, error) {
						sends++
						require.Equal(t, "Bearer old", req.Header.Get("Authorization"))
						if stage == "before-refresh" {
							changeOwner()
						}
						return cnRecoveryResponse(401, `{}`), nil
					})
					var failure *CredentialFailureError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
					require.Nil(t, resp)
					require.Equal(t, 1, sends, "a changed authorization context cannot supply replay credentials")
					if stage == "before-refresh" {
						require.Zero(t, refreshes)
					} else {
						require.Equal(t, 1, refreshes)
					}
					require.Zero(t, repo.updateCredentialsCalls)
					require.Equal(t, "different-access", repo.account.GetCredential("access_token"))
					require.Equal(t, "different-refresh", repo.account.GetCredential("refresh_token"))
				})
			}
		}
	}
}

func TestKimiModelDiscoveryRecovers401WithSameIdentity(t *testing.T) {
	a := cnRecoveryAccount(PlatformKimi)
	repo := &refreshAPIAccountRepo{account: a}
	oauth := NewCNOAuthService(nil, repo, nil)
	oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	refreshes := 0
	oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		refreshes++
		return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600}`), nil
	})}
	calls := 0
	var identity string
	upstream := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https://api.kimi.com/coding/v1/models", req.URL.String())
		require.Empty(t, req.Header.Get("X-Stainless-Package-Version"))
		if calls == 1 {
			identity = req.UserAgent()
			require.Equal(t, "Bearer old", req.Header.Get("Authorization"))
			return cnRecoveryResponse(401, `{}`), nil
		}
		require.Equal(t, identity, req.UserAgent())
		require.Equal(t, "Bearer new", req.Header.Get("Authorization"))
		return cnRecoveryResponse(200, `{"data":[]}`), nil
	}}
	svc := &AccountTestService{cnOAuthService: oauth, httpUpstream: upstream, cfg: cnProbeAllowlistConfig("api.kimi.com")}
	req, err := svc.buildUpstreamModelsRequest(context.Background(), a)
	require.NoError(t, err)
	resp, err := svc.doUpstreamModelsRequest(req, a)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, 1, refreshes)
	require.Equal(t, 2, calls)
}

func TestCNOAuthRecoveryRejectsChangedProxyOwnerEvenAtSameURL(t *testing.T) {
	for _, nullable := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-url-different-id", true: "nil-to-id"}[nullable], func(t *testing.T) {
			a := cnRecoveryAccount(PlatformKimi)
			firstID, secondID := int64(1), int64(2)
			if !nullable {
				a.ProxyID = &firstID
				a.Proxy = &Proxy{ID: firstID, Protocol: "http", Host: "127.0.0.1", Port: 8080}
			}
			fresh := snapshotOAuthRefreshAccount(a)
			fresh.ProxyID = &secondID
			if !nullable {
				fresh.Proxy = &Proxy{ID: secondID, Protocol: "http", Host: "127.0.0.1", Port: 8080}
			}
			fresh.Credentials["access_token"] = "new-owner-token"
			fresh.Credentials["refresh_token"] = "new-owner-refresh"
			repo := &refreshAPIAccountRepo{account: fresh}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) { refreshes++; return cnRecoveryResponse(200, `{}`), nil })}
			calls := 0
			req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", strings.NewReader(`{}`))
			_, err := oauth.roundTripNativeOAuth(req, a, func(*http.Request) (*http.Response, error) { calls++; return cnRecoveryResponse(401, `{}`), nil })
			var failure *CredentialFailureError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, GatewayFailureScopeProvider, failure.Scope)
			require.Nil(t, failure.account)
			require.Equal(t, 1, calls)
			require.Zero(t, refreshes)
			require.Zero(t, repo.updateCredentialsCalls)
			require.Equal(t, "new-owner-token", repo.account.GetCredential("access_token"))
		})
	}
}

func TestCNOAuthNativeForwardRecoversPreOutputSSE401(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			a := cnRecoveryAccount(platform)
			repo := &refreshAPIAccountRepo{account: a}
			oauth := NewCNOAuthService(nil, repo, nil)
			oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			oauth.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
				refreshes++
				return cnRecoveryResponse(200, `{"access_token":"new","refresh_token":"rotated","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`), nil
			})}
			calls := 0
			var userAgent, device, session string
			upstream := &cnRecoveryUpstream{send: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, req.Method)
				require.Contains(t, req.URL.Path, "/v1/messages")
				if calls == 1 {
					userAgent = req.UserAgent()
					device = req.Header.Get("X-Msh-Device-Id")
					session = req.Header.Get("X-Mavis-Session-Id")
					require.Equal(t, "Bearer old", req.Header.Get("Authorization"))
					return nativeSSETestResponse(nativeStructuralPrelude + nativeErrorSSE("authentication_error")), nil
				}
				require.Equal(t, 2, calls)
				require.Equal(t, "Bearer new", req.Header.Get("Authorization"))
				require.Equal(t, userAgent, req.UserAgent())
				require.Equal(t, device, req.Header.Get("X-Msh-Device-Id"))
				require.Equal(t, session, req.Header.Get("X-Mavis-Session-Id"))
				return nativeSSETestResponse(miniAnthropicSSEStream()), nil
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), cnOAuthService: oauth, httpUpstream: upstream}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			result, err := svc.ForwardAsAnthropic(context.Background(), c, a, []byte(`{"model":"native-model","max_tokens":32,"stream":true,"messages":[{"role":"user","content":"hello"}]}`), "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 2, calls)
			require.Equal(t, 1, refreshes)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "authentication_error")
			require.Contains(t, recorder.Body.String(), "message_stop")
		})
	}
}
