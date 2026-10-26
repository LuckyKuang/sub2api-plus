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
	session := &cnoauth.Session{OwnerID: 7, AccountID: accountID, Identity: identity, Flow: &cnoauth.Flow{Platform: platform, Region: "cn", Interval: 2, ExpiresAt: time.Now().Add(time.Minute)}, Grant: &cnoauth.Grant{AccessToken: "private-grant", RefreshToken: "private-refresh", ExpiresAt: time.Now().Add(time.Hour)}}
	if accountID > 0 {
		account, err := s.accountRepo.GetByID(context.Background(), accountID)
		require.NoError(t, err)
		session.LoginOwner = cnOAuthTestLoginOwner(t, account)
		session.ProxyID = account.ProxyID
		if account.Proxy != nil {
			session.ProxyURL = account.Proxy.URL()
		}
	}
	id, err := s.store.Create(context.Background(), session)
	require.NoError(t, err)
	return id
}

// This fixture encodes the persistence contract independently of the production
// login-owner serializer, so omitted owner fields cannot make both agree.
func cnOAuthTestLoginOwner(t *testing.T, a *Account) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"id": a.ID, "platform": a.Platform, "type": a.Type, "credentials": a.Credentials, "proxy_id": a.ProxyID, "proxy": a.Proxy,
		"status": a.Status, "error_message": a.ErrorMessage, "schedulable": a.Schedulable, "rate_limited_at": a.RateLimitedAt, "rate_limit_reset_at": a.RateLimitResetAt, "overload_until": a.OverloadUntil,
		"temp_unschedulable_until": a.TempUnschedulableUntil, "temp_unschedulable_reason": a.TempUnschedulableReason, "extra": a.Extra})
	require.NoError(t, err)
	return raw
}

func (r *refreshAPIAccountRepo) CompleteCNOAuthLoginIfUnchanged(_ context.Context, expected *Account, credentials map[string]any) (bool, error) {
	current := activeRefreshAPITestAccount(r.account)
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) || current.proxyURLOrEmpty() != expected.proxyURLOrEmpty() {
		return false, nil
	}
	currentJSON, currentErr := json.Marshal(current.Credentials)
	expectedJSON, expectedErr := json.Marshal(expected.Credentials)
	if currentErr != nil || expectedErr != nil || string(currentJSON) != string(expectedJSON) {
		return false, nil
	}
	sameTime := func(a, b *time.Time) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		return a.Equal(*b)
	}
	if current.Status != expected.Status || current.ErrorMessage != expected.ErrorMessage || current.Schedulable != expected.Schedulable || !sameTime(current.RateLimitedAt, expected.RateLimitedAt) || !sameTime(current.RateLimitResetAt, expected.RateLimitResetAt) || !sameTime(current.OverloadUntil, expected.OverloadUntil) || !sameTime(current.TempUnschedulableUntil, expected.TempUnschedulableUntil) || current.TempUnschedulableReason != expected.TempUnschedulableReason {
		return false, nil
	}
	for _, key := range []string{"model_rate_limits", "antigravity_quota_scopes"} {
		if !reflect.DeepEqual(current.Extra[key], expected.Extra[key]) {
			return false, nil
		}
	}
	if r.updateErr != nil {
		return false, r.updateErr
	}
	copy := *current
	copy.Credentials = shallowCopyMap(credentials)
	copy.Status = StatusActive
	copy.ErrorMessage = ""
	copy.RateLimitedAt = nil
	copy.RateLimitResetAt = nil
	copy.OverloadUntil = nil
	copy.TempUnschedulableUntil = nil
	copy.TempUnschedulableReason = ""
	copy.Extra = shallowCopyMap(current.Extra)
	delete(copy.Extra, "model_rate_limits")
	delete(copy.Extra, "antigravity_quota_scopes")
	r.account = &copy
	r.updateCalls++
	r.updateCredentialsCalls++
	return true, nil
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
	require.NotContains(t, repo.account.Credentials, "api_key")
	require.Contains(t, repo.account.Credentials, "model_mapping")
	id = cnOAuthReady(t, svc, PlatformKimi, 0)
	view, err = svc.Advance(context.Background(), 7, PlatformKimi, id, "", true)
	require.NoError(t, err)
	require.Equal(t, "cancelled", view.Status)
	_, err = svc.Complete(context.Background(), 7, PlatformKimi, id, CNOAuthCompleteInput{})
	require.Error(t, err)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Zero(t, admin.writes, "relink uses the atomic repository port")
}
func TestCNOAuthNativeSendAndRefresh(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			region := "global"
			if platform == PlatformDeepseek {
				region = "cn"
			}
			a := &Account{ID: 42, Platform: platform, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: platform, Region: region}, &cnoauth.Grant{AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Minute)})}
			loginID := a.GetCredential(domesticOAuthLoginIDCredential)
			require.NotEmpty(t, loginID)
			// Generic endpoint/protocol/header edits cannot reroute native grants.
			a.Credentials["base_url"] = "https://evil.test"
			a.Credentials["api_protocol"] = APIProtocolResponses
			require.Equal(t, APIProtocolAnthropic, a.GetAPIProtocol())
			require.NotContains(t, a.GetAnthropicProtocolBaseURL(), "evil")
			// Official managed usage clients exist for Kimi and MiniMax Code
			// (matrix-account-client.ts at reviewed commit 5821f7c9).
			if platform == PlatformKimi || platform == PlatformMiniMax {
				require.Equal(t, platform, a.GetCodingPlanProvider())
			} else {
				require.Empty(t, a.GetCodingPlanProvider())
			}
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
				require.Equal(t, loginID, repo.account.GetCredential(domesticOAuthLoginIDCredential), "normal refresh retains the authorization lifetime")
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

func TestDeepSeekOAuthPersistsSelectedEnvironmentAcrossSettingsChange(t *testing.T) {
	config := emptyOutboundIdentitySettings()
	config.Profiles["deepseek"] = OutboundIdentitySelection{Preset: "deepseek", Language: "en-US", Timezone: "Asia/Shanghai"}
	settings, ctx := outboundIdentityTestSettings(t, config)
	admin := &cnOAuthAdminStub{}
	svc := NewCNOAuthService(nil, nil, admin)
	calls := 0
	state := ""
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "en_US", r.Header.Get("X-Client-Locale"))
		require.Equal(t, "28800", r.Header.Get("X-Client-Timezone-Offset"))
		require.Equal(t, "web", r.Header.Get("X-Client-Platform"))
		require.Contains(t, r.Header, "X-Client-Bundle-Id")
		require.Empty(t, r.Header.Get("X-Client-Bundle-Id"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		value := `{"token":"grant"}`
		if calls == 1 {
			require.Equal(t, "en_US", body["locale"])
			state = body["state"].(string)
			value = `{"authorize_id":"authorization","authorize_url":"https://platform.deepseek.com/dsh/authorize?id=authorization","expires_in":300}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"biz_code":0,"biz_data":` + value + `}}`))}, nil
	})}
	view, err := svc.Start(ctx, 7, PlatformDeepseek, "cn", nil, 0)
	require.NoError(t, err)
	require.NoError(t, svc.store.Update(ctx, view.SessionID, func(_ context.Context, session *cnoauth.Session) error {
		require.Equal(t, "en-US", session.Identity.Language)
		require.Equal(t, "Asia/Shanghai", session.Identity.Timezone)
		require.Equal(t, "28800", session.Identity.ControlHeaders["X-Client-Timezone-Offset"])
		return nil
	}))
	config.Profiles["deepseek"] = OutboundIdentitySelection{Preset: "deepseek", Language: "zh-CN", Timezone: "UTC"}
	require.NoError(t, settings.SetOutboundIdentitySettings(ctx, config))
	_, err = svc.Advance(ctx, 7, PlatformDeepseek, view.SessionID, cnoauth.DeepSeekRedirect+"?code=code&state="+state, false)
	require.NoError(t, err)
	_, err = svc.Complete(ctx, 7, PlatformDeepseek, view.SessionID, CNOAuthCompleteInput{Name: "selected"})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	selection, ok := admin.input.Credentials[outboundIdentityCredential].(OutboundIdentitySelection)
	require.True(t, ok)
	require.Equal(t, "en-US", selection.Language)
	require.Equal(t, "Asia/Shanghai", selection.Timezone)
}

func TestStepAccountSessionRefreshCannotRotateModelKey(t *testing.T) {
	a := &Account{ID: 42, Platform: PlatformStepFun, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: PlatformStepFun, Region: "global"}, &cnoauth.Grant{AccessToken: "static-model-key"})}
	for key, value := range map[string]any{"step_account_access_token": "account-access", "step_account_refresh_token": "account-refresh", "step_account_device_id": "device", "step_account_app_id": "app", "step_account_expires_at": time.Now().Add(-time.Minute).Format(time.RFC3339)} {
		a.Credentials[key] = value
	}
	repo := &refreshAPIAccountRepo{account: a}
	svc := NewCNOAuthService(nil, repo, nil)
	svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "https://platform.stepfun.ai/passport/proto.api.passport.v1.PassportService/RefreshToken", r.URL.String())
		require.Equal(t, "Oasis-Token=account-access...account-refresh", r.Header.Get("Cookie"))
		require.Equal(t, "app", r.Header.Get("Oasis-appID"))
		require.Equal(t, "device", r.Header.Get("Oasis-Webid"))
		require.Equal(t, "web", r.Header.Get("Oasis-Platform"))
		require.Equal(t, "1", r.Header.Get("Connect-Protocol-Version"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"accessToken":{"raw":"renewed-account","mode":2,"duration":1800}}`))}, nil
	})}
	fresh, err := svc.RefreshAccount(context.Background(), a, true)
	require.NoError(t, err)
	require.Equal(t, "static-model-key", fresh.GetCredential("access_token"))
	require.Equal(t, "renewed-account", fresh.GetCredential("step_account_access_token"))
	require.Equal(t, "account-refresh", fresh.GetCredential("step_account_refresh_token"))
	require.Empty(t, fresh.GetCredential("refresh_token"))
	require.Empty(t, fresh.GetCredential("expires_at"))
}

func TestStepAccountSessionRefreshRejectsIncompleteOrInactiveSession(t *testing.T) {
	for _, phase := range []string{"selected", "reread"} {
		for _, missing := range []string{"step_account_access_token", "step_account_refresh_token", "step_account_device_id", "step_account_app_id"} {
			t.Run(phase+"/"+missing, func(t *testing.T) {
				selected := &Account{ID: 42, Platform: PlatformStepFun, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{
					"oauth_provider": PlatformStepFun, "oauth_region": "global", "access_token": "static-model-key",
					"step_account_access_token": "account-access", "step_account_refresh_token": "account-refresh", "step_account_device_id": "device", "step_account_app_id": "app",
				}}
				current := snapshotOAuthRefreshAccount(selected)
				delete(current.Credentials, missing)
				if phase == "selected" {
					selected = current
				}
				assertStepAccountSessionRefreshRejectedWithoutSideEffects(t, selected, current)
			})
		}
	}
	t.Run("inactive", func(t *testing.T) {
		selected := &Account{ID: 42, Platform: PlatformStepFun, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{
			"oauth_provider": PlatformStepFun, "oauth_region": "global", "access_token": "static-model-key",
			"step_account_access_token": "account-access", "step_account_refresh_token": "account-refresh", "step_account_device_id": "device", "step_account_app_id": "app",
		}}
		current := snapshotOAuthRefreshAccount(selected)
		current.Status = StatusDisabled
		assertStepAccountSessionRefreshRejectedWithoutSideEffects(t, selected, current)
	})
}

func assertStepAccountSessionRefreshRejectedWithoutSideEffects(t *testing.T, selected, current *Account) {
	t.Helper()
	before := snapshotOAuthRefreshAccount(current)
	repo := &refreshAPIAccountRepo{account: current}
	svc := NewCNOAuthService(nil, repo, nil)
	svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	calls := 0
	svc.client = &cnoauth.Client{HTTP: cnOAuthDoer(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected Passport refresh")
	})}
	fresh, err := svc.RefreshAccount(context.Background(), selected, true)
	require.Error(t, err, "Passport refresh requires a complete session on an active account")
	require.Nil(t, fresh)
	require.Zero(t, calls, "invalid account sessions must not reach the issuer")
	require.Zero(t, repo.updateCalls, "rejection must not persist credentials")
	require.Equal(t, before.Credentials, repo.account.Credentials, "the model key and existing session material must remain unchanged")
	require.Equal(t, before.Status, repo.account.Status)
}

func TestStepRelinkWithoutOptionalAccountSessionClearsPreviousOwnerSession(t *testing.T) {
	old := &Account{ID: 42, Platform: PlatformStepFun, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: PlatformStepFun, Region: "cn"}, &cnoauth.Grant{AccessToken: "old-model-key", StepLogin: &cnoauth.StepLogin{AccessToken: "old-account-access", RefreshToken: "old-account-refresh", DeviceID: "old-device", AppID: "old-app", ExpiresAt: time.Now().Add(time.Hour)}})}
	old.Credentials["model_mapping"] = map[string]any{"selected": "step-3.5-flash"}
	repo := &refreshAPIAccountRepo{account: old}
	admin := &cnOAuthAdminStub{}
	svc := NewCNOAuthService(nil, repo, admin)
	svc.refreshAPI = NewOAuthRefreshAPI(repo, nil)
	id, err := svc.store.Create(context.Background(), &cnoauth.Session{OwnerID: 7, AccountID: old.ID, LoginOwner: cnOAuthTestLoginOwner(t, old), Identity: builtInOutboundIdentity(PlatformStepFun), Flow: &cnoauth.Flow{Platform: PlatformStepFun, Region: "cn", ExpiresAt: time.Now().Add(time.Minute)}, Grant: &cnoauth.Grant{AccessToken: "new-model-key"}})
	require.NoError(t, err)
	_, err = svc.Complete(context.Background(), 7, PlatformStepFun, id, CNOAuthCompleteInput{})
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	for _, key := range []string{"step_account_access_token", "step_account_refresh_token", "step_account_device_id", "step_account_app_id", "step_account_expires_at"} {
		require.NotContains(t, repo.account.Credentials, key, "relink cannot retain another login's optional account session")
	}
	require.Equal(t, "new-model-key", repo.account.Credentials["access_token"])
	require.Equal(t, old.Credentials["model_mapping"], repo.account.Credentials["model_mapping"])
	current := repo.account
	repo.account = current
	require.False(t, NewCNTokenRefresher(PlatformStepFun).CanRefresh(current))
	_, err = svc.RefreshAccount(context.Background(), current, true)
	require.Error(t, err, "account-session refresh requires a fresh account login")
	for _, tc := range []struct{ method, path string }{{http.MethodPost, "/chat/completions"}, {http.MethodGet, "/usage"}} {
		req, err := http.NewRequest(tc.method, "https://api.stepfun.com/step_plan/v1"+tc.path, nil)
		require.NoError(t, err)
		require.NoError(t, svc.prepareRequest(req, current))
		require.Equal(t, "Bearer new-model-key", req.Header.Get("Authorization"), "model inference and Plan usage use the independent new model credential")
	}
}

func TestStepRelinkWithAccountSessionReplacesAllPreviousOwnerSessionFields(t *testing.T) {
	old := &Account{ID: 42, Platform: PlatformStepFun, Type: AccountTypeOAuth, Status: StatusActive, Credentials: cnOAuthCredentials(&cnoauth.Flow{Platform: PlatformStepFun, Region: "cn"}, &cnoauth.Grant{AccessToken: "old-model-key", StepLogin: &cnoauth.StepLogin{AccessToken: "old-account-access", RefreshToken: "old-account-refresh", DeviceID: "old-device", AppID: "old-app", ExpiresAt: time.Now().Add(time.Minute)}})}
	repo := &refreshAPIAccountRepo{account: old}
	admin := &cnOAuthAdminStub{}
	svc := NewCNOAuthService(nil, repo, admin)
	freshSession := &cnoauth.StepLogin{AccessToken: "new-account-access", RefreshToken: "new-account-refresh", DeviceID: "new-device", AppID: "new-app", ExpiresAt: time.Now().Add(time.Hour)}
	id, err := svc.store.Create(context.Background(), &cnoauth.Session{OwnerID: 7, AccountID: old.ID, LoginOwner: cnOAuthTestLoginOwner(t, old), Identity: builtInOutboundIdentity(PlatformStepFun), Flow: &cnoauth.Flow{Platform: PlatformStepFun, Region: "cn", ExpiresAt: time.Now().Add(time.Minute)}, Grant: &cnoauth.Grant{AccessToken: "new-model-key", StepLogin: freshSession}})
	require.NoError(t, err)
	_, err = svc.Complete(context.Background(), 7, PlatformStepFun, id, CNOAuthCompleteInput{})
	require.NoError(t, err)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	for key, expected := range map[string]any{"step_account_access_token": "new-account-access", "step_account_refresh_token": "new-account-refresh", "step_account_device_id": "new-device", "step_account_app_id": "new-app", "step_account_expires_at": freshSession.ExpiresAt.UTC().Format(time.RFC3339)} {
		require.Equal(t, expected, repo.account.Credentials[key], "every account-session field must come from the new grant")
		require.NotEqual(t, old.Credentials[key], repo.account.Credentials[key])
	}
	require.Equal(t, "new-model-key", repo.account.Credentials["access_token"])
}
