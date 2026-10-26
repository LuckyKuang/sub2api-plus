//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/stretchr/testify/require"
)

type projectTokenTestClient struct {
	zhipuOAuthStubClient
	mu      sync.Mutex
	calls   int
	err     error
	entered chan struct{}
	release chan struct{}
}

func (c *projectTokenTestClient) ResolveIndividualCodingPlanKey(ctx context.Context, provider, login, proxy string) (*zcode.CodingPlanCredential, error) {
	c.mu.Lock()
	c.calls++
	n, err, entered, release := c.calls, c.err, c.entered, c.release
	c.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &zcode.CodingPlanCredential{APIKey: fmt.Sprintf("pat-%s-%d", login, n), KeyID: "location", OrganizationID: "org", ProjectID: "project", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (c *projectTokenTestClient) IssueProjectAccessToken(ctx context.Context, provider, login, org, project, key, proxy string) (*zcode.ProjectAccessToken, error) {
	value, err := c.ResolveIndividualCodingPlanKey(ctx, provider, login, proxy)
	if err != nil {
		return nil, err
	}
	return &zcode.ProjectAccessToken{Token: value.APIKey, ExpiresAt: value.ExpiresAt}, nil
}

func projectTokenTestAccount(id int64) *Account {
	return &Account{ID: id, Platform: PlatformZhipu, Type: AccountTypeOAuth, Credentials: map[string]any{"oauth_provider": "bigmodel", "oauth_access_token": "login", "access_token": "business", "plan_kind": ZhipuPlanIndividualCodingPlan, "zhipu_project_kind": "personal", "api_key": "forbidden-long-key"}}
}
func newProjectTokenTestService(t *testing.T, c *projectTokenTestClient) *ZhipuProjectTokenManager {
	t.Helper()
	return NewZhipuProjectTokenManager(c, nil)
}
func projectTokenTestResolve(m *ZhipuProjectTokenManager, ctx context.Context, a *Account, rejected string) (*zcode.CodingPlanCredential, error) {
	m.reject(ctx, a, rejected)
	return m.credential(ctx, a, false)
}

// ZCode project-access-token.ts: owner single flight, memory-only material,
// two-minute+jitter early refresh and no fallback on disabled/auth failures.
func TestZhipuProjectTokenCacheOwnersRefreshAndRejection(t *testing.T) {
	c := &projectTokenTestClient{}
	s := newProjectTokenTestService(t, c)
	a := projectTokenTestAccount(1)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := projectTokenTestResolve(s, context.Background(), a, "")
			require.NoError(t, err)
			if v != nil {
				require.Equal(t, "pat-login-1", v.APIKey)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, c.calls)
	require.Equal(t, "forbidden-long-key", a.GetCredential("api_key"), "cache must never persist a PAT")
	_, err := projectTokenTestResolve(s, context.Background(), projectTokenTestAccount(2), "")
	require.NoError(t, err)
	require.Equal(t, 2, c.calls)
	// A rejected old token rotates only its owner; a late 401 cannot discard a
	// newer PAT already issued by another subscriber.
	v, err := projectTokenTestResolve(s, context.Background(), a, "pat-login-1")
	require.NoError(t, err)
	require.Equal(t, "pat-login-3", v.APIKey)
	v, err = projectTokenTestResolve(s, context.Background(), a, "pat-login-1")
	require.NoError(t, err)
	require.Equal(t, "pat-login-3", v.APIKey)
	require.Equal(t, 3, c.calls)
	entry := s.state(context.Background(), a, false)
	entry.mu.Lock()
	entry.refreshAt = time.Now().Add(-time.Second)
	entry.mu.Unlock()
	c.err = &zcode.BusinessHTTPError{Status: 503}
	v, err = projectTokenTestResolve(s, context.Background(), a, "")
	require.NoError(t, err)
	require.Equal(t, "pat-login-3", v.APIKey)
	entry.mu.Lock()
	entry.refreshAt = time.Now().Add(-time.Second)
	entry.mu.Unlock()
	c.err = &zcode.BusinessHTTPError{Status: 403}
	_, err = projectTokenTestResolve(s, context.Background(), a, "")
	require.Error(t, err, "authorization errors cannot reuse the cached PAT or long key")
	entry.mu.Lock()
	require.Nil(t, entry.value, "a rejected refresh discards old material")
	entry.value = &zcode.CodingPlanCredential{APIKey: "expired", ExpiresAt: time.Now().Add(-time.Second)}
	entry.mu.Unlock()
	c.err = &zcode.BusinessHTTPError{Status: 503}
	_, err = projectTokenTestResolve(s, context.Background(), a, "")
	require.Error(t, err, "expired material cannot be reused")
}

func TestZhipuProjectTokenCancellationAndLoginGeneration(t *testing.T) {
	c := &projectTokenTestClient{entered: make(chan struct{}, 4), release: make(chan struct{})}
	s := newProjectTokenTestService(t, c)
	a := projectTokenTestAccount(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := projectTokenTestResolve(s, ctx, a, ""); done <- err }()
	<-c.entered
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	// A new credential generation supersedes the pending old result.
	replacement := projectTokenTestAccount(1)
	replacement.Credentials["oauth_access_token"] = "new-login"
	newer := make(chan error, 1)
	go func() {
		v, err := projectTokenTestResolve(s, context.Background(), replacement, "")
		if err == nil && v.APIKey != "pat-new-login-2" {
			err = errors.New("wrong owner token")
		}
		newer <- err
	}()
	<-c.entered
	close(c.release)
	require.NoError(t, <-newer)
	v, err := projectTokenTestResolve(s, context.Background(), replacement, "")
	require.NoError(t, err)
	require.Equal(t, "pat-new-login-2", v.APIKey)
	c.mu.Lock()
	calls := c.calls
	c.mu.Unlock()
	require.Equal(t, 2, calls)
}

func TestZhipuProjectTokenRejectedDuringTransientRefresh(t *testing.T) {
	// ZCode project-access-token.ts falls back only to current.value. A model
	// 401 that arrives during issuance removes that fallback for every waiter.
	c := &projectTokenTestClient{}
	s := newProjectTokenTestService(t, c)
	a := projectTokenTestAccount(1)
	first, err := projectTokenTestResolve(s, context.Background(), a, "")
	require.NoError(t, err)
	entry := s.state(context.Background(), a, false)
	entry.mu.Lock()
	entry.refreshAt = time.Now().Add(-time.Second)
	entry.mu.Unlock()
	c.mu.Lock()
	c.err = &zcode.BusinessHTTPError{Status: 503}
	c.entered = make(chan struct{}, 1)
	c.release = make(chan struct{})
	entered, release := c.entered, c.release
	c.mu.Unlock()
	type result struct {
		value *zcode.CodingPlanCredential
		err   error
	}
	results := make(chan result, 2)
	resolve := func(rejected string) {
		value, err := projectTokenTestResolve(s, context.Background(), a, rejected)
		results <- result{value, err}
	}
	go resolve("")
	<-entered
	go resolve(first.APIKey)
	require.Eventually(t, func() bool {
		entry.mu.Lock()
		defer entry.mu.Unlock()
		return entry.value == nil
	}, time.Second, time.Millisecond, "the model rejection must discard the in-flight refresh fallback")
	close(release)
	for range 2 {
		got := <-results
		require.Error(t, got.err, "a transient failure must not resurrect a rejected PAT")
		require.Nil(t, got.value)
	}
}

func TestZhipuProjectTokenEvery401InvalidatesAttempt(t *testing.T) {
	// Every explicit 401 rejects its PAT, including the final retry and bodies
	// that cannot be replayed. Rejection itself must not initiate extra I/O.
	for _, replayable := range []bool{true, false} {
		t.Run(fmt.Sprintf("replayable=%t", replayable), func(t *testing.T) {
			c := &projectTokenTestClient{}
			linked := newProjectTokenTestService(t, c)
			oauth := linked
			a := projectTokenTestAccount(1)
			req, err := http.NewRequest(http.MethodPost, "https://open.bigmodel.cn/api/anthropic/v1/messages", strings.NewReader(`{"messages":[]}`))
			require.NoError(t, err)
			if !replayable {
				req.GetBody = nil
			}
			var auth []string
			resp, err := oauth.RoundTrip(req, a, func(r *http.Request) (*http.Response, error) {
				auth = append(auth, r.Header.Get("Authorization"))
				return &http.Response{StatusCode: 401, Body: http.NoBody}, nil
			})
			require.NoError(t, err)
			require.Equal(t, 401, resp.StatusCode)
			want := []string{"Bearer pat-login-1"}
			if replayable {
				want = append(want, "Bearer pat-login-2")
			}
			require.Equal(t, want, auth)
			require.Equal(t, len(want), c.calls)
			value, err := projectTokenTestResolve(linked, req.Context(), a, "")
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("pat-login-%d", len(want)+1), value.APIKey, "the next request must not reuse the final rejected PAT")
		})
	}
}

func TestZhipuProjectTokenLinkedModelDiscovery(t *testing.T) {
	// The official-agent plan requires discovery to reach the same final PAT
	// resolver as inference, for both estates and personal/team credential owners.
	for _, provider := range []string{"bigmodel", "zai"} {
		for _, plan := range []string{ZhipuPlanIndividualCodingPlan, ZhipuPlanTeamCodingPlan} {
			t.Run(provider+"/"+plan, func(t *testing.T) {
				client := &projectTokenTestClient{}
				client.businessToken = "zai-business"
				linked := newProjectTokenTestService(t, client)
				account := projectTokenTestAccount(1)
				account.Credentials["oauth_provider"] = provider
				account.Credentials["plan_kind"] = plan
				account.Credentials["zhipu_organization"] = "org"
				account.Credentials["zhipu_project"] = "project"
				account.Credentials["zhipu_project_key_id"] = "location"
				account.Credentials["base_url"] = "https://attacker.test"
				if plan == ZhipuPlanTeamCodingPlan {
					account.Credentials[credKeyHeaderOverrideEnabled] = true
					account.Credentials[credKeyHeaderOverrides] = map[string]any{"bigmodel-organization": "org", "bigmodel-project": "project"}
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"glm-5.3"}]}`))}}
				svc := &AccountTestService{httpUpstream: upstream, projectTokens: linked, cfg: &config.Config{}}
				ids, err := svc.FetchUpstreamSupportedModels(context.Background(), account)
				require.NoError(t, err)
				require.Equal(t, []string{"glm-5.3"}, ids)
				target, auth := "https://open.bigmodel.cn/api/coding/paas/v4/models", "Bearer pat-login-1"
				if provider == "zai" {
					target, auth = "https://api.z.ai/api/coding/paas/v4/models", "Bearer pat-zai-business-1"
				}
				require.Equal(t, target, upstream.lastReq.URL.String())
				require.Equal(t, auth, upstream.lastReq.Header.Get("Authorization"))
				require.Equal(t, "ZCode/3.16.2", upstream.lastReq.UserAgent())
				require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
				if plan == ZhipuPlanTeamCodingPlan {
					require.Equal(t, []string{"org"}, headerValuesEqualFold(upstream.lastReq.Header, "bigmodel-organization"))
					require.Equal(t, []string{"project"}, headerValuesEqualFold(upstream.lastReq.Header, "bigmodel-project"))
				}
				require.Equal(t, 1, client.calls)
			})
		}
	}
}

func TestZhipuProjectTokenFinalTicket401InvalidatesAttempt(t *testing.T) {
	client := &projectTokenTestClient{}
	linked := newProjectTokenTestService(t, client)
	account := projectTokenTestAccount(1)
	account.Credentials["plan_kind"] = ZhipuPlanOffPeak
	ctx := WithAccountOutboundIdentity(context.Background(), account)
	identity, _ := outboundidentity.FromContext(ctx)
	holder := &zhipuOffPeakHolder{account: account, identity: identity, auth: zcode.OffPeakAuth{JWT: "plan-jwt"}}
	manager := &ZhipuOffPeakTicketManager{projectTokens: linked}
	var keys []string
	err := manager.withProjectAuth(ctx, holder, func(auth zcode.OffPeakAuth) error {
		keys = append(keys, auth.PlanKey)
		return &zcode.OffPeakError{HTTPStatus: 401}
	})
	require.Error(t, err)
	require.Equal(t, []string{"pat-login-1", "pat-login-2"}, keys)
	require.Equal(t, 2, client.calls, "rejection cannot issue a third PAT within the failed operation")
	value, err := projectTokenTestResolve(linked, ctx, account, "")
	require.NoError(t, err)
	require.Equal(t, "pat-login-3", value.APIKey)
}

func TestZhipuProjectTokenZaiBusinessRefreshFailure(t *testing.T) {
	// The official cache allows transient I/O fallback only while the PAT is
	// unexpired. A failed business-token exchange follows that same rule.
	c := &projectTokenTestClient{}
	c.businessToken = "business-login"
	s := newProjectTokenTestService(t, c)
	a := projectTokenTestAccount(1)
	a.Credentials["oauth_provider"] = zcode.ProviderZai
	value, err := projectTokenTestResolve(s, context.Background(), a, "")
	require.NoError(t, err)
	require.Equal(t, "pat-business-login-1", value.APIKey)
	refresh := func() {
		entry := s.state(context.Background(), a, false)
		entry.mu.Lock()
		entry.refreshAt = time.Now().Add(-time.Second)
		entry.mu.Unlock()
	}
	refresh()
	c.businessTokenErr = &zcode.BusinessHTTPError{Status: 503}
	value, err = projectTokenTestResolve(s, context.Background(), a, "")
	require.NoError(t, err)
	require.Equal(t, "pat-business-login-1", value.APIKey)
	refresh()
	c.businessTokenErr = &zcode.BusinessHTTPError{Status: 403}
	_, err = projectTokenTestResolve(s, context.Background(), a, "")
	require.Error(t, err)
	require.Equal(t, 1, c.calls, "failed exchanges cannot initiate project issuance")
	require.Equal(t, 3, c.businessCalls)
}

func TestZhipuProjectTokenInferenceProbeAnd401Replay(t *testing.T) {
	for _, probe := range []bool{false, true} {
		for _, status := range []int{401, 403} {
			t.Run(fmt.Sprintf("probe=%t/status=%d", probe, status), func(t *testing.T) {
				c := &projectTokenTestClient{}
				linked := newProjectTokenTestService(t, c)
				oauth := linked
				a := projectTokenTestAccount(1)
				// The real sending wrapper must replace the long key, preserve the original
				// body and retry only explicit 401. Final identity remains ZCode's snapshot.
				upstream := &projectTokenWireUpstream{status: status, t: t}
				req, _ := http.NewRequest(http.MethodPost, "https://open.bigmodel.cn/api/anthropic/v1/messages", strings.NewReader(`{"model":"glm-5.3","messages":[]}`))
				req.Header.Set("Authorization", "Bearer forbidden-long-key")
				var resp *http.Response
				var err error
				if probe {
					svc := &AccountTestService{httpUpstream: upstream, projectTokens: oauth}
					resp, err = svc.doOpenAIAccountTestUpstream(req, "", a, false)
				} else {
					svc := &OpenAIGatewayService{httpUpstream: upstream, projectTokens: oauth}
					resp, err = svc.doOpenAIUpstream(req, "", a)
				}
				require.NoError(t, err)
				require.NotNil(t, resp)
				_ = resp.Body.Close()
				expected := 1
				if status == 401 {
					expected = 2
				}
				require.Len(t, upstream.auth, expected)
				require.Equal(t, "Bearer pat-login-1", upstream.auth[0])
				if expected == 2 {
					require.Equal(t, "Bearer pat-login-2", upstream.auth[1])
				}
				require.Equal(t, expected, c.calls)
			})
		}
	}
	// Manually entered API keys never initiate login or project-token exchange.
	a := projectTokenTestAccount(1)
	a.Type = AccountTypeAPIKey
	req, _ := http.NewRequest(http.MethodGet, "https://api.z.ai/v1/models", nil)
	req.Header.Set("Authorization", "Bearer manual")
	_, err := (*ZhipuProjectTokenManager)(nil).RoundTrip(req, a, func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer manual", r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200}, nil
	})
	require.NoError(t, err)
}

func TestZhipuProjectTokenDestinationAndOffPeakIsolation(t *testing.T) {
	c := &projectTokenTestClient{}
	linked := newProjectTokenTestService(t, c)
	oauth := linked
	a := projectTokenTestAccount(1)
	for _, target := range []string{"https://attacker.test/api/anthropic/v1/messages", "https://api.z.ai/api/anthropic/v1/messages", "https://open.bigmodel.cn.evil.test/api/anthropic/v1/messages"} {
		req, _ := http.NewRequest(http.MethodPost, target, nil)
		_, err := oauth.RoundTrip(req, a, func(*http.Request) (*http.Response, error) {
			t.Fatal("untrusted destination must not be sent")
			return nil, nil
		})
		require.ErrorContains(t, err, "destination_rejected")
		require.Zero(t, c.calls)
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}}
	svc := &AccountTestService{httpUpstream: upstream, projectTokens: oauth}
	req, _ := http.NewRequest(http.MethodGet, "https://models.dev/api.json", nil)
	resp, err := svc.doUpstreamModelsRequest(req, a)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Empty(t, upstream.lastReq.Header.Get("Authorization"))
	require.Zero(t, c.calls)
	// Personal off-peak material contains a resolved org/project too. That alone
	// must not switch it to the team key. JWT and PAT have separate wire roles.
	a.Credentials["plan_kind"] = ZhipuPlanOffPeak
	a.Credentials["api_key"] = "zcode-jwt"
	a.Credentials["zcode_jwt_token"] = "zcode-jwt"
	a.Credentials["zhipu_organization"] = "org"
	a.Credentials["zhipu_project"] = "project"
	a.Credentials["zhipu_project_key_name"] = "zcode-api-key"
	require.Equal(t, "zcode-jwt", a.GetOpenAIProtocolAPIKey())
	req, _ = http.NewRequest(http.MethodPost, "https://zcode.z.ai/api/v1/off-peak/anthropic/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer forbidden-override")
	req.Header["authorization"] = []string{"Bearer raw-inbound"}
	req.Header["x-api-key"] = []string{"raw-key"}
	req.Header["X-CODING-PLAN-API-KEY"] = []string{"raw-plan"}
	require.NoError(t, oauth.Prepare(req, a))
	require.Equal(t, "Bearer zcode-jwt", req.Header.Get("Authorization"))
	require.NotContains(t, req.Header, "authorization")
	require.NotContains(t, req.Header, "x-api-key")
	require.NotContains(t, req.Header, "X-CODING-PLAN-API-KEY")
	require.Equal(t, "pat-login-1", req.Header.Get("X-Coding-Plan-Api-Key"))
	require.Equal(t, 1, c.calls)
	require.False(t, zhipuTeamProject(a))
	a.Credentials["zhipu_project_key_name"] = "zcode-team-api-key"
	a.Credentials["zhipu_project_kind"] = "team"
	require.True(t, zhipuTeamProject(a))
}

func TestZhipuProjectTokenOffPeakTeamScope(t *testing.T) {
	// ZCode off-peak-request-auth.ts adds team scope only for a team plan.
	// Personal PATs also resolve org/project IDs; those do not imply team scope.
	for _, team := range []bool{false, true} {
		t.Run(fmt.Sprintf("team=%t", team), func(t *testing.T) {
			client := &projectTokenTestClient{}
			linked := newProjectTokenTestService(t, client)
			account := projectTokenTestAccount(1)
			account.Credentials["plan_kind"] = ZhipuPlanOffPeak
			account.Credentials["zcode_jwt_token"] = "plan-jwt"
			account.Credentials["zhipu_organization"] = "org"
			account.Credentials["zhipu_project"] = "project"
			account.Credentials["zhipu_project_key_id"] = "location"
			account.Credentials["zhipu_project_key_name"] = zcode.PersonalAPIKeyName
			if team {
				account.Credentials["zhipu_project_key_name"] = zcode.TeamAPIKeyName
				account.Credentials["zhipu_project_kind"] = "team"
			}
			// Generic header overrides are unavailable to linked OAuth accounts;
			// stale/inbound scope headers cannot replace the PAT owner's scope.
			req, err := http.NewRequest(http.MethodPost, "https://zcode.z.ai/api/v1/off-peak/anthropic/v1/messages", nil)
			require.NoError(t, err)
			req.Header["BIGMODEL-ORGANIZATION"] = []string{"untrusted"}
			req.Header["bigmodel-project"] = []string{"untrusted"}
			require.NoError(t, linked.Prepare(req, account))
			org, project := "", ""
			if team {
				org, project = "org", "project"
			}
			require.Equal(t, org, req.Header.Get("bigmodel-organization"))
			require.Equal(t, project, req.Header.Get("bigmodel-project"))
			require.NotContains(t, req.Header, "BIGMODEL-ORGANIZATION")
			require.NotContains(t, req.Header, "bigmodel-project")
			tickets := &zhipuOffPeakStub{takes: []*zcode.OffPeakTicket{{TicketID: "ticket", State: zcode.TicketReady}}}
			manager := NewZhipuOffPeakTicketManager(tickets)
			t.Cleanup(manager.Stop)
			manager.projectTokens = linked
			ticket, err := manager.TicketForRequest(context.Background(), account)
			if ticket != nil {
				ticket.Release()
			}
			require.NoError(t, err)
			require.Equal(t, zcode.OffPeakAuth{JWT: "plan-jwt", PlanKey: "pat-login-1", TeamOrg: org, TeamProj: project}, tickets.lastAuth)
		})
	}
}

type rotatingProjectTicketClient struct {
	zhipuOffPeakStub
	statusAuths  []zcode.OffPeakAuth
	rejectStatus bool
}

func (c *rotatingProjectTicketClient) TicketStatus(ctx context.Context, ids []string, auth zcode.OffPeakAuth, proxy string) ([]zcode.OffPeakTicket, time.Duration, error) {
	c.statusAuths = append(c.statusAuths, auth)
	if c.rejectStatus {
		c.rejectStatus = false
		return nil, 0, &zcode.OffPeakError{HTTPStatus: http.StatusUnauthorized}
	}
	return c.zhipuOffPeakStub.TicketStatus(ctx, ids, auth, proxy)
}

func TestZhipuProjectTokenOffPeakStatusAndDetachedSettlementRenew(t *testing.T) {
	// ZCode off-peak-request-auth.ts resolves PATs for every call and retries
	// explicit 401 once. A ticket retains its original credential owner, while
	// rotating a PAT alone does not spend another take-number quota.
	c := &projectTokenTestClient{}
	service := newProjectTokenTestService(t, c)
	tickets := &rotatingProjectTicketClient{zhipuOffPeakStub: zhipuOffPeakStub{
		takes: []*zcode.OffPeakTicket{{TicketID: "stable-ticket", State: zcode.TicketReady}},
	}}
	manager := NewZhipuOffPeakTicketManager(tickets)
	t.Cleanup(manager.Stop)
	manager.projectTokens = service
	now := time.Now()
	manager.now = func() time.Time { return now }
	a := projectTokenTestAccount(77)
	a.Credentials["plan_kind"] = ZhipuPlanOffPeak
	a.Credentials["zcode_jwt_token"] = "plan-jwt"
	a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "4.2.0"}
	_, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	ctx = WithOutboundIdentityScope(ctx, nil)
	id, err := manager.TicketForRequest(ctx, a)
	require.NoError(t, err)
	require.Equal(t, "stable-ticket", id.TicketID)
	id.Release()
	require.Equal(t, zcode.OffPeakAuth{JWT: "plan-jwt", PlanKey: "pat-login-1"}, tickets.lastAuth)
	refresh := func() {
		t.Helper()
		entry := service.state(context.Background(), a, false)
		entry.mu.Lock()
		entry.refreshAt = time.Now().Add(-time.Second)
		entry.mu.Unlock()
	}
	refresh()
	tickets.rejectStatus = true
	id, err = manager.TicketForRequest(ctx, a)
	require.NoError(t, err)
	require.Equal(t, "stable-ticket", id.TicketID)
	id.Release()
	require.Equal(t, []zcode.OffPeakAuth{
		{JWT: "plan-jwt", PlanKey: "pat-login-2"},
		{JWT: "plan-jwt", PlanKey: "pat-login-3"},
	}, tickets.statusAuths)
	require.Equal(t, 1, tickets.takeCalls)
	refresh()
	// Mutating the live account must not relabel the old ticket's cleanup.
	a.Credentials["oauth_access_token"] = "replacement-login"
	a.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "zcode", Version: "4.4.0"}
	now = now.Add(DefaultOffPeakSettleIdle + time.Second)
	manager.sweepOnce()
	require.Equal(t, []string{"stable-ticket"}, tickets.settled)
	require.Equal(t, []zcode.OffPeakAuth{{JWT: "plan-jwt", PlanKey: "pat-login-4"}}, tickets.settleAuths)
	require.Equal(t, 4, c.calls)
	require.Len(t, tickets.identities, 2)
	require.Equal(t, "4.2.0", tickets.identities[1].Version)
	require.Equal(t, int64(77), tickets.identities[1].AccountID)
	identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, a))
	require.True(t, ok)
	require.Equal(t, "4.2.0", identity.Version)
}

type projectTokenWireUpstream struct {
	httpUpstreamRecorder
	status int
	auth   []string
	t      *testing.T
}

func (u *projectTokenWireUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	require.True(u.t, HTTPUpstreamRedirectsDisabled(req.Context()), "linked credentials must never follow a redirect beyond their validated destination")
	u.auth = append(u.auth, req.Header.Get("Authorization"))
	body, err := io.ReadAll(req.Body)
	require.NoError(u.t, err)
	requireZCodeUnsessionedBusinessBody(u.t, req, body, `{"model":"glm-5.3","messages":[]}`)
	require.Contains(u.t, req.UserAgent(), "ZCode/3.16.2")
	require.Equal(u.t, "3.16.2", req.Header.Get("X-ZCode-App-Version"))
	status := 200
	if len(u.auth) == 1 {
		status = u.status
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}
func (u *projectTokenWireUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestZhipuProjectTokenFailuresSeparateGrantFromSharedDependency(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		scope     GatewayFailureScope
		permanent bool
	}{
		{"login401", &zcode.BusinessHTTPError{Status: 401}, GatewayFailureScopeAccount, true},
		{"permission403", &zcode.BusinessHTTPError{Status: 403}, GatewayFailureScopeAccount, true},
		{"rate429", &zcode.BusinessHTTPError{Status: 429}, GatewayFailureScopeProvider, false},
		{"service503", &zcode.BusinessHTTPError{Status: 503}, GatewayFailureScopeProvider, false},
		{"malformed", errors.New("malformed provider response"), GatewayFailureScopeProvider, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := projectTokenTestAccount(42)
			client := &projectTokenTestClient{err: tc.err}
			manager := newProjectTokenTestService(t, client)
			_, err := projectTokenTestResolve(manager, context.Background(), a, "")
			var failure *CredentialFailureError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.scope, failure.Scope)
			require.Equal(t, tc.permanent, failure.Permanent)
			require.ErrorIs(t, err, tc.err)
			if tc.permanent {
				require.NotNil(t, failure.account)
				require.Equal(t, a.Credentials, failure.account.Credentials)
			} else {
				require.Nil(t, failure.account)
			}
		})
	}
}
