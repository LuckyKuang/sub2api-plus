//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cnOAuthAdminStub struct {
	AdminService
	mu     sync.Mutex
	writes int
	input  *CreateAccountInput
	update *UpdateAccountInput
	fail   bool
}

func (a *cnOAuthAdminStub) CreateAccount(_ context.Context, in *CreateAccountInput) (*Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writes++
	a.input = in
	if a.fail {
		return nil, errors.New("secret database details")
	}
	return &Account{ID: 42}, nil
}
func (a *cnOAuthAdminStub) UpdateAccount(_ context.Context, _ int64, in *UpdateAccountInput) (*Account, error) {
	a.update = in
	a.writes++
	return &Account{ID: 42}, nil
}

func (a *cnOAuthAdminStub) ClearAccountError(_ context.Context, id int64) (*Account, error) {
	return &Account{ID: id}, nil
}

type cnOAuthDoer func(*http.Request) (*http.Response, error)

func (f cnOAuthDoer) Do(r *http.Request) (*http.Response, error) { return f(r) }
func cnOAuthReady(t *testing.T, s *CNOAuthService, platform string, accountID int64) string {
	t.Helper()
	identity := builtInOutboundIdentity(platform)
	id, err := s.store.Create(context.Background(), &cnoauth.Session{OwnerID: 7, AccountID: accountID, Identity: identity, Flow: &cnoauth.Flow{Platform: platform, Region: "cn", Interval: 2, ExpiresAt: time.Now().Add(time.Minute)}, Grant: &cnoauth.Grant{AccessToken: "private-grant", RefreshToken: "private-refresh", ExpiresAt: time.Now().Add(time.Hour)}})
	require.NoError(t, err)
	return id
}
func TestCNOAuthCompletionOwnerSingleUseAndNoTokenEcho(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			admin := &cnOAuthAdminStub{}
			svc := NewCNOAuthService(nil, nil, admin)
			id := cnOAuthReady(t, svc, platform, 0)
			_, err := svc.Complete(context.Background(), 8, platform, id, CNOAuthCompleteInput{})
			require.Error(t, err)
			_, err = svc.Complete(context.Background(), 7, "other", id, CNOAuthCompleteInput{})
			require.Error(t, err)
			require.Zero(t, admin.writes)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = svc.Complete(context.Background(), 7, platform, id, CNOAuthCompleteInput{Name: "native", GroupIDs: []int64{9}})
				}()
			}
			wg.Wait()
			require.Equal(t, 1, admin.writes)
			require.NoError(t, NormalizeAccountOutboundIdentity(platform, AccountTypeOAuth, admin.input.Credentials))
			require.Equal(t, AccountTypeOAuth, admin.input.Type)
			require.Equal(t, platform, admin.input.Credentials["oauth_provider"])
			if platform == PlatformDeepseek {
				require.NotContains(t, admin.input.Credentials, "refresh_token")
				require.NotContains(t, admin.input.Credentials, "expires_at")
			}
			require.Equal(t, []int64{9}, admin.input.GroupIDs)
			view, err := svc.Complete(context.Background(), 7, platform, id, CNOAuthCompleteInput{})
			require.NoError(t, err)
			require.EqualValues(t, 42, view.AccountID)
			data, err := json.Marshal(view)
			require.NoError(t, err)
			require.NotContains(t, string(data), "private")
			require.NotContains(t, string(data), "token")
		})
	}
}
func TestCNOAuthAmbiguousWriteCannotReplay(t *testing.T) {
	admin := &cnOAuthAdminStub{fail: true}
	svc := NewCNOAuthService(nil, nil, admin)
	id := cnOAuthReady(t, svc, PlatformKimi, 0)
	_, err := svc.Complete(context.Background(), 7, PlatformKimi, id, CNOAuthCompleteInput{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	admin.fail = false
	_, err = svc.Complete(context.Background(), 7, PlatformKimi, id, CNOAuthCompleteInput{})
	require.Error(t, err)
	require.Equal(t, 1, admin.writes)
}
func TestCNOAuthCancelAndRelink(t *testing.T) {
	admin := &cnOAuthAdminStub{}
	repo := &refreshAPIAccountRepo{account: &Account{ID: 42, Platform: PlatformKimi, Type: AccountTypeOAuth, Credentials: map[string]any{"api_key": "obsolete", "model_mapping": map[string]any{"a": "b"}}}}
	svc := NewCNOAuthService(nil, repo, admin)
	id := cnOAuthReady(t, svc, PlatformKimi, 42)
	view, err := svc.Complete(context.Background(), 7, PlatformKimi, id, CNOAuthCompleteInput{})
	require.NoError(t, err)
	require.EqualValues(t, 42, view.AccountID)
	require.NotContains(t, admin.update.Credentials, "api_key")
	require.Contains(t, admin.update.Credentials, "model_mapping")
	id = cnOAuthReady(t, svc, PlatformKimi, 0)
	view, err = svc.Advance(context.Background(), 7, PlatformKimi, id, "", true)
	require.NoError(t, err)
	require.Equal(t, "cancelled", view.Status)
	_, err = svc.Complete(context.Background(), 7, PlatformKimi, id, CNOAuthCompleteInput{})
	require.Error(t, err)
	require.Equal(t, 1, admin.writes)
}
func TestCNOAuthNativeSendAndRefresh(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			region := "global"
			if platform == PlatformDeepseek {
				region = "cn"
			}
			a := &Account{ID: 42, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: region}, &cnoauth.Grant{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute)})}
			// Generic endpoint/protocol/header edits cannot reroute native grants.
			a.Credentials["base_url"] = "https://evil.test"
			a.Credentials["api_protocol"] = APIProtocolResponses
			require.Equal(t, APIProtocolAnthropic, a.GetAPIProtocol())
			require.NotContains(t, a.GetAnthropicProtocolBaseURL(), "evil")
			require.Empty(t, a.GetCodingPlanProvider())
			require.Empty(t, a.GetCNAPIKey())
			repo := &refreshAPIAccountRepo{account: a}
			svc := NewCNOAuthService(nil, repo, nil)
			svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
			refreshes := 0
			svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(r *http.Request) (*http.Response, error) {
				refreshes++
				require.Contains(t, []string{"auth.kimi.ai", "account.minimax.io"}, r.URL.Host)
				require.NotEmpty(t, r.UserAgent())
				require.NoError(t, r.ParseForm())
				require.Equal(t, "refresh", r.Form.Get("refresh_token"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"new","refresh_token":"rotated","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`))}, nil
			})}
			req, err := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer inbound")
			req.Header.Set("x-api-key", "inbound")
			req.Header.Set("x-dsh-auth-token", "inbound")
			req.Header.Set("User-Agent", "untrusted")
			require.NoError(t, svc.prepareRequest(req, a))
			prepareAccountOutboundRequest(req, a)
			require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
			require.Empty(t, req.Header.Get("x-api-key"))
			require.NotEqual(t, "untrusted", req.UserAgent())
			if platform == PlatformDeepseek {
				require.Equal(t, "old", req.Header.Get("x-dsh-auth-token"))
				require.Empty(t, req.Header.Get("Authorization"))
				require.Zero(t, refreshes)
			} else {
				require.Equal(t, "Bearer new", req.Header.Get("Authorization"))
				require.Empty(t, req.Header.Get("x-dsh-auth-token"))
				require.Equal(t, 1, refreshes)
				require.Equal(t, "rotated", repo.account.GetCredential("refresh_token"))
				require.Equal(t, 1, repo.updateCredentialsCalls)
				require.NoError(t, svc.prepareRequest(req, a))
				require.Equal(t, 1, refreshes)
			}
			if platform == PlatformKimi {
				require.Equal(t, "true", req.URL.Query().Get("beta"))
			}
			req.URL.Host = "evil.test"
			require.Error(t, svc.prepareRequest(req, a))
		})
	}
}
func TestCNOAuthSendRejectsChangedOwner(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformKimi, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: PlatformKimi, Region: "cn"}, &cnoauth.Grant{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Minute)})}
	changed := *a
	changed.Type = AccountTypeAPIKey
	svc := NewCNOAuthService(nil, nil, nil)
	svc.refreshAPI = NewOAuthRefreshAPI(&refreshAPIAccountRepo{account: &changed}, nil)
	req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", nil)
	require.Error(t, svc.prepareRequest(req, a))
}

func TestCNOAuthHTTPAdaptersAndProbeUseNativeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax} {
		for _, protocol := range []string{"messages", "chat", "responses", "probe"} {
			t.Run(platform+"/"+protocol, func(t *testing.T) {
				account := &Account{ID: 71, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 1, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: "cn"}, &cnoauth.Grant{AccessToken: "native-token", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})}
				repo := &refreshAPIAccountRepo{account: account}
				oauth := NewCNOAuthService(nil, repo, nil)
				oauth.refreshAPI = NewOAuthRefreshAPI(repo, nil)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg_native","type":"message","role":"assistant","model":"native-model","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`))}}
				if protocol == "chat" || protocol == "responses" {
					upstream.resp.Header.Set("Content-Type", "text/event-stream")
					upstream.resp.Body = io.NopCloser(strings.NewReader(miniAnthropicSSEStream()))
				}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, cnOAuthService: oauth}
				body := []byte(`{"model":"native-model","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				if protocol == "responses" {
					body = []byte(`{"model":"native-model","input":"hello","stream":false}`)
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+protocol, strings.NewReader(string(body)))
				c.Request.Header.Set("User-Agent", "inbound-client/999")
				c.Request.Header.Set("Authorization", "Bearer inbound")
				var err error
				switch protocol {
				case "messages":
					_, err = svc.forwardAnthropicViaNativeAnthropicEndpoint(context.Background(), c, account, body, "")
				case "chat":
					_, err = svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
				case "responses":
					_, err = svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, body, "")
				case "probe":
					tester := &AccountTestService{httpUpstream: upstream, cnOAuthService: oauth}
					req, _ := http.NewRequest(http.MethodPost, account.GetAnthropicProtocolBaseURL()+"/v1/messages", nil)
					_, err = tester.doCNProviderAdaptiveRequest(req, account)
				}
				require.NoError(t, err)
				require.NotNil(t, upstream.lastReq)
				req := upstream.lastReq
				require.Equal(t, account.GetAnthropicProtocolBaseURL()+"/v1/messages", req.URL.Scheme+"://"+req.URL.Host+req.URL.Path)
				require.Equal(t, builtInOutboundIdentity(platform).UserAgent, req.UserAgent())
				require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
				require.Empty(t, req.Header.Get("x-api-key"))
				if platform == PlatformDeepseek {
					require.Equal(t, "native-token", req.Header.Get("x-dsh-auth-token"))
					require.Empty(t, req.Header.Get("Authorization"))
				} else {
					require.Equal(t, "Bearer native-token", req.Header.Get("Authorization"))
				}
				if platform == PlatformMiniMax {
					require.NotEmpty(t, req.Header.Get("X-Mavis-Session-Id"))
					require.NotEmpty(t, req.Header.Get("X-Mavis-Agent-Id"))
					require.Equal(t, "0", req.Header.Get("X-Mavis-Timezone-Offset"))
				}
			})
		}
	}
}

func TestCNOAuthPollingThrottleAndSessionIdentity(t *testing.T) {
	svc := NewCNOAuthService(nil, nil, nil)
	identity := builtInOutboundIdentity(PlatformKimi)
	identity.UserAgent = "kimi-code-cli/9.9.9"
	identity.Version = "9.9.9"
	identity.Headers["X-Msh-Version"] = "9.9.9"
	id, err := svc.store.Create(context.Background(), &cnoauth.Session{OwnerID: 7, Identity: identity, Flow: &cnoauth.Flow{Platform: PlatformKimi, Region: "cn", Interval: 2, ExpiresAt: time.Now().Add(time.Minute)}, NextPoll: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	calls := 0
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, identity.UserAgent, req.UserAgent())
		require.Equal(t, identity.Headers["X-Msh-Device-Id"], req.Header.Get("X-Msh-Device-Id"))
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"slow_down"}`))}, nil
	})}
	_, err = svc.Advance(context.Background(), 8, PlatformKimi, id, "", false)
	require.Error(t, err)
	require.Zero(t, calls)
	view, err := svc.Advance(context.Background(), 7, PlatformKimi, id, "", false)
	require.NoError(t, err)
	require.Equal(t, "pending", view.Status)
	require.Zero(t, calls)
	require.NoError(t, svc.store.Update(context.Background(), id, func(_ context.Context, s *cnoauth.Session) error { s.NextPoll = time.Time{}; return nil }))
	view, err = svc.Advance(context.Background(), 7, PlatformKimi, id, "", false)
	require.NoError(t, err)
	require.Equal(t, 7, view.Interval)
	require.Equal(t, 1, calls)
	_, err = svc.Advance(context.Background(), 7, PlatformKimi, id, "", false)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestCNOAuthUnexpiredSendDoesNotAcquireRefreshLock(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		a := &Account{ID: 1, Platform: platform, Type: AccountTypeOAuth, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: "cn"}, &cnoauth.Grant{AccessToken: "valid", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)})}
		svc := NewCNOAuthService(nil, nil, nil)
		req, _ := http.NewRequest(http.MethodPost, a.GetAnthropicProtocolBaseURL()+"/v1/messages", nil)
		require.NoError(t, svc.prepareRequest(req, a))
		require.Equal(t, "Bearer valid", req.Header.Get("Authorization"))
		delete(a.Credentials, "refresh_token")
		require.Error(t, svc.prepareRequest(req, a))
	}
}
