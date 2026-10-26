//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/cnoauth"
	pool "github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// Each case enters its actual service operation. Only the final transport maps
// the validated official destination to a reachable loopback destination. The
// explicit proxy still receives a real HTTP request, including proxy auth.
// A bad route therefore cannot hide an accidental direct send behind a mock.
type auxiliaryWireCapture struct{ route, path, proxyAuth, auth, ua string }
type auxiliaryWireGate struct {
	reached, release chan struct{}
	once             sync.Once
}
type auxiliaryWireNetwork struct {
	mu                sync.Mutex
	destination       *httptest.Server
	directConnections atomic.Int64
	proxies           map[string]*httptest.Server
	captured          []auxiliaryWireCapture
	gate              *auxiliaryWireGate
	failure           string
	failurePath       string
}

const auxiliaryWireJSON = `{"code":"ok","windows_reset":2,"available_count":0,"credits":[],"rate_limit":{"allowed":true,"primary_window":{"used_percent":12,"limit_window_seconds":18000,"reset_after_seconds":3600}},"rate_limit_reset_credits":{"available_count":0},"usages":{"limit_5h":{"used_ratio":0.12,"reset_time":"2099-01-01T00:00:00Z"}},"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":12,"nextResetTime":4070908800000}],"available_balance":12,"userInfo":{"realUserID":"fake-user"}},"base_resp":{"status_code":0},"model_remains":[{"model_name":"MiniMax-M2.7","current_interval_remaining_percent":88},{"model_name":"general","current_interval_remaining_percent":88}],"balance_infos":[{"currency":"USD","total_balance":"12"}],"is_available":true,"config":{"currentPeriod":{"type":"WEEKLY","start":"2026-10-20T00:00:00Z","end":"2026-10-27T00:00:00Z"},"creditUsagePercent":12},"workspaces":[{"workspace_type":0,"workspace_id":42,"op_group_id":"personal-group","has_token_plan":true}],"has_token_plan":true,"should_show":true,"remaining_send_capacity":2,"requires_explicit_confirmation":false}`

func newAuxiliaryWireNetwork(t *testing.T) *auxiliaryWireNetwork {
	t.Helper()
	n := &auxiliaryWireNetwork{proxies: map[string]*httptest.Server{}}
	n.destination = httptest.NewUnstartedServer(n.handler("direct"))
	n.destination.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			n.directConnections.Add(1)
		}
	}
	n.destination.Start()
	t.Cleanup(n.destination.Close)
	for _, name := range []string{"old", "new"} {
		n.proxies[name] = httptest.NewServer(n.handler(name))
		t.Cleanup(n.proxies[name].Close)
	}
	return n
}
func (n *auxiliaryWireNetwork) handler(route string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n.mu.Lock()
		n.captured = append(n.captured, auxiliaryWireCapture{route, r.URL.Path, r.Header.Get("Proxy-Authorization"), r.Header.Get("Authorization"), r.UserAgent()})
		gate, failure, failurePath := n.gate, n.failure, n.failurePath
		n.mu.Unlock()
		if gate != nil {
			gate.once.Do(func() { close(gate.reached); <-gate.release })
		}
		if route != "direct" && (failurePath == "" || r.URL.Path == failurePath) {
			if failure == "timeout" {
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
				return
			}
			if failure == "authentication_required" {
				w.WriteHeader(http.StatusProxyAuthRequired)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-codex-primary-used-percent", "12")
		w.Header().Set("x-codex-primary-window-minutes", "300")
		body := auxiliaryWireJSON
		switch {
		case r.URL.Path == "/api/oauth/token" || r.URL.Path == "/oauth2/token":
			body = `{"access_token":"fake-refreshed-token","refresh_token":"fake-rotated-refresh","expires_in":3600,"scope":"agent.default","token_type":"Bearer"}`
		case r.URL.Path == "/api/auth/z/login":
			body = `{"code":200,"data":{"access_token":"fake-business-token"}}`
		case strings.HasSuffix(r.URL.Path, "/getCustomerInfo"):
			body = `{"code":200,"data":{"organizations":[{"organizationId":"org","organizationName":"默认机构","projects":[{"projectId":"project","projectName":"默认项目"}]}]}}`
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			body = `{"code":200,"data":[{"name":"zcode-api-key","apiKey":"location-id"},{"name":"zcode-team-api-key","keyType":2,"apiKey":"location-id"}]}`
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			body = fmt.Sprintf(`{"code":200,"data":{"enable":true,"accessToken":"fake-project-token","tokenType":"Bearer","expiresIn":3600,"expiresAt":%d}}`, time.Now().Add(time.Hour).Unix())
		}
		if r.URL.Path == "/backend-api/wham/usage" {
			body = strings.Replace(body, `"credits":[]`, `"credits":{"has_credits":false,"unlimited":false,"balance":null}`, 1)
		}
		if r.URL.Path == "/v1/users/me/balance" {
			body = `{"code":0,"status":true,"data":{"available_balance":12}}`
		}
		if r.URL.Path == "/api/monitor/usage/quota/limit" {
			body = `{"code":200,"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":12,"nextResetTime":4070908800000}]}}`
		}
		if strings.HasSuffix(r.URL.Path, "/models") || r.URL.Path == "/models" {
			body = `{"data":[{"id":"deepseek-flash","slug":"deepseek-flash","supported_endpoints":["/responses"],"api_capabilities":{"anthropic_messages":{}},"input_modalities":["text"],"output_modalities":["text"]}],"models":[{"id":"deepseek-flash","slug":"deepseek-flash"}]}`
		}
		io.WriteString(w, body)
	}
}
func (n *auxiliaryWireNetwork) proxy(t *testing.T, name string, id int64) *Proxy {
	t.Helper()
	u, err := url.Parse(n.proxies[name].URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return &Proxy{ID: id, Protocol: "http", Host: u.Hostname(), Port: port, Status: StatusActive}
}
func (n *auxiliaryWireNetwork) reset() { n.mu.Lock(); defer n.mu.Unlock(); n.captured = nil }
func (n *auxiliaryWireNetwork) captures() []auxiliaryWireCapture {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]auxiliaryWireCapture(nil), n.captured...)
}
func (n *auxiliaryWireNetwork) Do(request *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	n.mu.Lock()
	failure := n.failure
	n.mu.Unlock()
	wire := request.Clone(request.Context())
	target, _ := url.Parse(n.destination.URL)
	parsed := *request.URL
	parsed.Scheme, parsed.Host = target.Scheme, target.Host
	wire.URL = &parsed
	wire.Host = target.Host
	transport := &http.Transport{}
	if proxyURL != "" {
		proxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	if failure == "timeout" {
		client.Timeout = 100 * time.Millisecond
	}
	return client.Do(wire)
}
func (n *auxiliaryWireNetwork) DoWithTLS(r *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return n.Do(r, p, id, c)
}

// Privacy requests use the same route as HTTPUpstream after changing only
// the destination at the test seam.
func (n *auxiliaryWireNetwork) privacyFactory(proxy string) (*req.Client, error) {
	client, err := newQuotaRedirectingFactory(n.destination)(proxy)
	if err != nil {
		return nil, err
	}
	if proxy != "" {
		client.SetProxyURL(proxy)
	}
	n.mu.Lock()
	if n.failure == "timeout" {
		client.SetTimeout(100 * time.Millisecond)
	}
	n.mu.Unlock()
	return client, nil
}
func (n *auxiliaryWireNetwork) installProbeClients(t *testing.T, routes ...string) {
	t.Helper()
	for _, route := range routes {
		for _, options := range []pool.Options{
			{ProxyURL: route, Timeout: 15 * time.Second, ResponseHeaderTimeout: 10 * time.Second},
			{ProxyURL: route, Timeout: 30 * time.Second},
			{ProxyURL: route, Timeout: 15 * time.Second},
		} {
			client, err := pool.GetClient(options)
			require.NoError(t, err)
			previous := client.Transport
			currentRoute := route
			client.Transport = auxiliaryRoundTripper(func(r *http.Request) (*http.Response, error) { return n.Do(r, currentRoute, 0, 1) })
			t.Cleanup(func() { client.Transport = previous })
		}
	}
}

type auxiliaryWireReferralClient struct{ network *auxiliaryWireNetwork }

func (c auxiliaryWireReferralClient) send(ctx context.Context, call OpenAIReferralCall, path string) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/referrals/invite"+path, nil)
	if err != nil {
		return err
	}
	for key, value := range call.Headers {
		r.Header.Set(key, value)
	}
	response, err := c.network.Do(r, call.ProxyURL, 0, 1)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return fmt.Errorf("referral HTTP %d", response.StatusCode)
	}
	return nil
}
func (c auxiliaryWireReferralClient) QueryEligibility(ctx context.Context, call OpenAIReferralCall) (*OpenAIReferralEligibility, error) {
	if err := c.send(ctx, call, "/eligibility"); err != nil {
		return nil, err
	}
	count := 2
	confirmation := false
	return &OpenAIReferralEligibility{ShouldShow: true, RemainingSendCapacity: &count, RequiresExplicitConfirmation: &confirmation}, nil
}
func (c auxiliaryWireReferralClient) SendInvite(ctx context.Context, call OpenAIReferralCall, _ string) error {
	return c.send(ctx, call, "")
}

type auxiliaryWireOperation struct {
	name    string
	account func() *Account
	execute func(context.Context, *auxiliaryOwnerRepo, *auxiliaryWireNetwork) error
	// Independent MiniMax control endpoints share a signer but each must retain
	// the operation's proxy even after earlier control calls have succeeded.
	failurePaths           []string
	successfulPaths        []string
	rejectStaleObservation bool
}

// The service adapter keeps protocol handling in the real ZCode client. Its
// HTTP client is the same route-keyed shared client used by the repository port.
type auxiliaryZhipuWireClient struct{ zhipuOAuthStubClient }

func (auxiliaryZhipuWireClient) credentials(proxy string) (*zcode.CredentialClient, error) {
	client, err := pool.GetClient(pool.Options{ProxyURL: proxy, Timeout: zcode.CredentialRequestTimeout})
	if err != nil {
		return nil, err
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return zcode.NewCredentialClient(&clone, "", ""), nil
}
func (c auxiliaryZhipuWireClient) ExchangeZaiBusinessToken(ctx context.Context, token, proxy string) (string, error) {
	client, err := c.credentials(proxy)
	if err != nil {
		return "", err
	}
	return client.ExchangeZaiBusinessToken(ctx, token)
}
func (c auxiliaryZhipuWireClient) ResolveIndividualCodingPlanKey(ctx context.Context, provider, token, proxy string) (*zcode.CodingPlanCredential, error) {
	client, err := c.credentials(proxy)
	if err != nil {
		return nil, err
	}
	return client.ResolveIndividualCodingPlanKey(ctx, provider, token)
}
func (c auxiliaryZhipuWireClient) ResolveTeamPlanKey(ctx context.Context, provider, token, org, project, proxy string) (*zcode.CodingPlanCredential, error) {
	client, err := c.credentials(proxy)
	if err != nil {
		return nil, err
	}
	return client.ResolveTeamPlanKey(ctx, provider, token, org, project)
}
func (c auxiliaryZhipuWireClient) IssueProjectAccessToken(ctx context.Context, provider, token, org, project, key, proxy string) (*zcode.ProjectAccessToken, error) {
	client, err := c.credentials(proxy)
	if err != nil {
		return nil, err
	}
	return client.IssueProjectAccessToken(ctx, provider, token, org, project, key)
}

func auxiliaryWireOperations() []auxiliaryWireOperation {
	codex := func() *Account { return auxiliaryCodexAccount("fake-wire", auxiliaryOwnerUA, 0) }
	openaiService := func(repo *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) *OpenAIQuotaService {
		svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, nil, nil), n.privacyFactory)
		svc.referralClient = auxiliaryWireReferralClient{n}
		return svc
	}
	operations := []auxiliaryWireOperation{
		{name: "codex-probe", account: codex, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			_, err := (&AccountUsageService{accountRepo: r, openAIQuotaService: openaiService(r, n)}).probeOpenAICodexSnapshot(ctx, a)
			return err
		}},
		{name: "wham-usage-credit-details", account: codex, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			_, err := openaiService(r, n).QueryUsage(ctx, a.ID)
			return err
		}, failurePaths: []string{"/backend-api/wham/usage", "/backend-api/wham/rate-limit-reset-credits"}},
		{name: "wham-reset", account: codex, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			_, err := openaiService(r, n).ResetCredit(ctx, a.ID)
			return err
		}},
		{name: "referral-query", account: codex, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			_, err := openaiService(r, n).QueryReferralEligibility(ctx, a.ID)
			return err
		}},
		{name: "referral-send", account: codex, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			_, err := openaiService(r, n).SendReferralInvite(ctx, a.ID, OpenAIReferralSendRequest{Email: "wire@example.test", ProgramID: openAIReferralConsumer, Confirmed: true})
			return err
		}, failurePaths: []string{"/backend-api/referrals/invite/eligibility", "/backend-api/referrals/invite"}},
	}
	for _, platform := range []string{PlatformKimi, PlatformMiniMax, PlatformZhipu} {
		for _, auth := range []string{"api_key", "oauth"} {
			p, kind := platform, auth
			operation := auxiliaryWireOperation{name: p + "-quota-" + kind, rejectStaleObservation: true, account: func() *Account {
				if kind == "oauth" {
					if p == PlatformZhipu {
						a, _ := zhipuNativeCredentialAccount("bigmodel", zhipuNativeCredentialCase{plan: ZhipuPlanIndividualCodingPlan})
						return a
					}
					return cnRecoveryAccount(p)
				}
				return codingAccount(p)
			}}
			if p == PlatformMiniMax && kind == "oauth" {
				operation.failurePaths = []string{"/v1/api/user/info", "/matrix/api/v1/user/get_user_extra_info", "/matrix/api/v1/commerce/get_membership_info", miniMaxOAuthQuotaPath}
			}
			operation.execute = func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
				a, _ := r.GetByID(ctx, 0)
				svc := NewCNProviderQuotaService(r, nil, n, cnProbeAllowlistConfig("api.kimi.com", "api.kimi.com.cn", "api.minimaxi.com", "api.minimax.io", "agent.minimaxi.com", "www.minimaxi.com", "open.bigmodel.cn"))
				svc.cnOAuthService = NewCNOAuthService(nil, r, nil)
				if p == PlatformZhipu && kind == "oauth" {
					svc.projectTokens = NewZhipuProjectTokenManager(&projectTokenTestClient{}, nil)
				}
				result, err := svc.QueryUsageForAccount(ctx, a)
				if err == nil && result == nil {
					return fmt.Errorf("quota response missing")
				}
				if err == nil && !result.Success {
					return fmt.Errorf("quota response failed: %s", result.Error)
				}
				return err
			}
			operations = append(operations, operation)
		}
	}
	for _, platform := range []string{PlatformKimi, PlatformDeepseek} {
		p := platform
		operations = append(operations, auxiliaryWireOperation{name: p + "-balance", rejectStaleObservation: true, account: func() *Account { return paygAccount(p) }, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			result, err := NewCNProviderBalanceService(r, nil, n, nil).QueryBalanceForAccount(ctx, a)
			if err == nil && (result == nil || !result.Success) {
				return fmt.Errorf("balance response failed")
			}
			return err
		}})
	}
	for _, billing := range []bool{false, true} {
		isBilling := billing
		name := "grok-probe"
		if billing {
			name = "grok-billing"
		}
		operations = append(operations, auxiliaryWireOperation{name: name, account: func() *Account { return healthyGrokQuotaOAuthAccount(883007) }, execute: func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
			a, _ := r.GetByID(ctx, 0)
			svc := NewGrokQuotaService(r, nil, NewGrokTokenProvider(r, nil), n, nil)
			var result *GrokQuotaProbeResult
			var err error
			if isBilling {
				result, err = svc.ProbeBilling(ctx, a.ID)
			} else {
				result, err = svc.ProbeUsage(ctx, a.ID)
			}
			if err == nil && (result == nil || result.StatusCode >= 400 || result.ProbeError != "") {
				return fmt.Errorf("grok probe failed")
			}
			return err
		}})
	}
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformDeepseek, PlatformKimi, PlatformMiniMax, PlatformStepFun, PlatformZhipu} {
		for _, auth := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
			if platform == PlatformMiniMax && auth == AccountTypeOAuth {
				continue
			} // explicit unsupported contract tested separately
			p, kind := platform, auth
			operation := auxiliaryWireOperation{name: p + "-models-" + kind, account: func() *Account {
				if kind == AccountTypeAPIKey {
					return &Account{ID: 883009, Platform: p, Type: kind, Status: StatusActive, Concurrency: 1, Credentials: map[string]any{"api_key": "fake-model-key", "api_protocol": "chat_completions"}}
				}
				switch p {
				case PlatformOpenAI:
					return auxiliaryCodexAccount("fake-model-token", auxiliaryOwnerUA, 0)
				case PlatformGrok:
					return healthyGrokQuotaOAuthAccount(883009)
				case PlatformStepFun:
					a := stepFunTestAccount(kind, "cn")
					a.Status = StatusActive
					return a
				case PlatformZhipu:
					a, _ := zhipuNativeCredentialAccount("bigmodel", zhipuNativeCredentialCase{plan: ZhipuPlanIndividualCodingPlan})
					return a
				default:
					return cnRecoveryAccount(p)
				}
			}}
			operation.execute = func(ctx context.Context, r *auxiliaryOwnerRepo, n *auxiliaryWireNetwork) error {
				account, _ := r.GetByID(ctx, 0)
				oauth := NewCNOAuthService(nil, r, nil)
				var tokens *ZhipuProjectTokenManager
				if p == PlatformZhipu && kind == AccountTypeOAuth {
					tokens = NewZhipuProjectTokenManager(&projectTokenTestClient{}, nil)
				}
				svc := &AccountTestService{projectTokens: tokens, accountRepo: r, httpUpstream: n, cnOAuthService: oauth, grokTokenProvider: NewGrokTokenProvider(r, nil), cfg: upstreamModelSyncTestConfig()}
				models, err := svc.FetchUpstreamSupportedModels(ctx, account)
				if err == nil && (len(models) != 1 || models[0] != "deepseek-flash") {
					return fmt.Errorf("live model response did not retain the advertised model")
				}
				return err
			}
			path := "/v1/models"
			switch p {
			case PlatformOpenAI:
				if kind == AccountTypeOAuth {
					path = "/backend-api/codex/models"
				}
			case PlatformDeepseek:
				if kind == AccountTypeOAuth {
					path = "/models"
				}
			case PlatformKimi:
				if kind == AccountTypeOAuth {
					path = "/coding/v1/models"
				}
			case PlatformStepFun:
				if kind == AccountTypeOAuth {
					path = "/step_plan/v1/models"
				}
			case PlatformZhipu:
				path = "/api/paas/v4/models"
				if kind == AccountTypeOAuth {
					path = "/api/coding/paas/v4/models"
				}
			}
			operation.successfulPaths = []string{path}
			operations = append(operations, operation)
		}
	}
	for _, platform := range []string{PlatformKimi, PlatformMiniMax} {
		p := platform
		path := "/api/oauth/token"
		if p == PlatformMiniMax {
			path = "/oauth2/token"
		}
		operations = append(operations, auxiliaryWireOperation{
			name: p + "-native-token-refresh", account: func() *Account { return cnRecoveryAccount(p) }, successfulPaths: []string{path},
			execute: func(ctx context.Context, repo *auxiliaryOwnerRepo, _ *auxiliaryWireNetwork) error {
				account, _ := repo.GetByID(ctx, 0)
				credentials, err := (&CNTokenRefresher{platform: p, client: &cnoauth.Client{}}).Refresh(ctx, account)
				if err == nil && (credentials["access_token"] != "fake-refreshed-token" || credentials["refresh_token"] != "fake-rotated-refresh") {
					return fmt.Errorf("native refresh did not return the provider's rotation")
				}
				return err
			},
		})
	}
	for _, estate := range []string{zcode.ProviderBigModel, zcode.ProviderZai} {
		for _, mode := range []string{"saved-location", "discover-personal", "discover-team"} {
			provider, routeMode := estate, mode
			paths := []string{"/api/biz/v1/organization/org/projects/project/api_keys/location-id/access_tokens"}
			if routeMode != "saved-location" {
				paths = append([]string{"/api/biz/v1/organization/org/projects/project/api_keys"}, paths...)
			}
			if routeMode == "discover-personal" || routeMode == "discover-team" {
				paths = append([]string{"/api/biz/customer/getCustomerInfo"}, paths...)
			}
			if provider == zcode.ProviderZai {
				paths = append([]string{"/api/auth/z/login"}, paths...)
			}
			operations = append(operations, auxiliaryWireOperation{
				name: "zhipu-project-token-" + provider + "-" + routeMode, successfulPaths: paths, failurePaths: paths,
				account: func() *Account {
					account := projectTokenTestAccount(883012)
					account.Credentials["oauth_provider"] = provider
					account.Credentials["zhipu_organization"], account.Credentials["zhipu_project"] = "org", "project"
					if routeMode == "saved-location" {
						account.Credentials["zhipu_project_key_id"] = "location-id"
					}
					if routeMode == "discover-team" {
						account.Credentials["plan_kind"] = ZhipuPlanTeamCodingPlan
					}
					return account
				},
				execute: func(ctx context.Context, repo *auxiliaryOwnerRepo, _ *auxiliaryWireNetwork) error {
					account, _ := repo.GetByID(ctx, 0)
					tokens := NewZhipuProjectTokenManager(&auxiliaryZhipuWireClient{}, nil)
					credential, err := tokens.credential(ctx, account, false)
					if err == nil && (credential == nil || credential.APIKey != "fake-project-token") {
						return fmt.Errorf("project token did not use actual provider issuance")
					}
					return err
				},
			})
		}
	}
	for i := range operations {
		operation := &operations[i]
		switch operation.name {
		case "codex-probe":
			operation.successfulPaths = []string{"/backend-api/codex/responses"}
		case "wham-usage-credit-details":
			operation.successfulPaths = []string{"/backend-api/wham/usage", "/backend-api/wham/rate-limit-reset-credits"}
		case "wham-reset":
			operation.successfulPaths = []string{"/backend-api/wham/rate-limit-reset-credits/consume"}
		case "referral-query":
			operation.successfulPaths = []string{"/backend-api/referrals/invite/eligibility"}
		case "referral-send":
			operation.successfulPaths = []string{"/backend-api/referrals/invite/eligibility", "/backend-api/referrals/invite"}
		case "kimi-quota-api_key", "kimi-quota-oauth":
			operation.successfulPaths = []string{"/coding/v1/usages"}
		case "minimax-quota-api_key":
			operation.successfulPaths = []string{"/v1/api/openplatform/coding_plan/remains"}
		case "minimax-quota-oauth":
			operation.successfulPaths = []string{"/v1/api/user/info", "/matrix/api/v1/user/get_user_extra_info", "/matrix/api/v1/commerce/get_membership_info", "/v1/api/openplatform/coding_plan/remains"}
		case "zhipu-quota-api_key", "zhipu-quota-oauth":
			operation.successfulPaths = []string{"/api/monitor/usage/quota/limit"}
		case "kimi-balance":
			operation.successfulPaths = []string{"/v1/users/me/balance"}
		case "deepseek-balance":
			operation.successfulPaths = []string{"/user/balance"}
		case "grok-probe":
			operation.successfulPaths = []string{"/v1/responses"}
		case "grok-billing":
			operation.successfulPaths = []string{"/v1/billing", "/v1/billing"}
		}
	}
	return operations
}

func TestAuxiliarySendersProxyLifecycleUsesCommittedRouteForFreshOperation(t *testing.T) {
	for _, operation := range auxiliaryWireOperations() {
		t.Run(operation.name, func(t *testing.T) {
			network := newAuxiliaryWireNetwork(t)
			oldProxy, newProxy := network.proxy(t, "old", 7), network.proxy(t, "new", 7)
			authenticated := *newProxy
			authenticated.Username = "operator"
			authenticated.Password = "fake-password"
			network.installProbeClients(t, "", oldProxy.URL(), newProxy.URL(), authenticated.URL())
			account := operation.account()
			repo := &auxiliaryOwnerRepo{account: account}
			run := func(route string) {
				t.Helper()
				network.reset()
				require.NoError(t, operation.execute(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()), repo, network))
				captures := network.captures()
				require.NotEmpty(t, captures)
				paths := make([]string, 0, len(captures))
				for _, capture := range captures {
					paths = append(paths, capture.path)
				}
				expected := append([]string(nil), operation.successfulPaths...)
				sort.Strings(paths)
				sort.Strings(expected)
				require.Equal(t, expected, paths, "all documented physical operation calls must execute")
				for _, capture := range captures {
					require.Equal(t, route, capture.route, "every physical control/quota call must use the same committed route")
				}
			}
			// Creating and binding a proxy changes the very next operation.
			run("direct")
			account.ProxyID = &oldProxy.ID
			account.Proxy = oldProxy
			repo.replace(account)
			run("old")
			// Idle endpoint and credential edits retain the row's binding.
			account.Proxy = newProxy
			repo.replace(account)
			run("new")
			account.Proxy = &authenticated
			repo.replace(account)
			run("new")
			for _, capture := range network.captures() {
				require.NotEmpty(t, capture.proxyAuth, "proxy credential edits must reach the next real request")
			}
			// The complete logical operation retains its first snapshot across a barrier.
			account.Proxy = oldProxy
			repo.replace(account)
			network.reset()
			repo.mu.Lock()
			writesBefore := len(repo.writes)
			repo.mu.Unlock()
			gate := &auxiliaryWireGate{reached: make(chan struct{}), release: make(chan struct{})}
			network.mu.Lock()
			network.gate = gate
			network.mu.Unlock()
			done := make(chan error, 1)
			go func() {
				done <- operation.execute(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()), repo, network)
			}()
			select {
			case <-gate.reached:
			case <-time.After(3 * time.Second):
				close(gate.release)
				t.Fatal("operation did not reach its configured proxy")
			}
			account.Proxy = newProxy
			repo.replace(account)
			close(gate.release)
			completedErr := <-done
			if operation.rejectStaleObservation {
				require.Error(t, completedErr, "a completed physical probe cannot certify a replaced route's observation")
				repo.mu.Lock()
				writesAfter := len(repo.writes)
				repo.mu.Unlock()
				require.Equal(t, writesBefore, writesAfter, "the old route's late observation cannot persist onto the replacement route")
			} else {
				require.NoError(t, completedErr)
			}
			for _, capture := range network.captures() {
				require.Equal(t, "old", capture.route, "in-flight edits cannot split a logical operation's route")
			}
			network.mu.Lock()
			network.gate = nil
			network.mu.Unlock()
			run("new")
			// Switching IDs is distinct from editing the existing proxy row.
			other := *newProxy
			other.ID = 8
			account.ProxyID = &other.ID
			account.Proxy = &other
			repo.replace(account)
			run("new")
			// A committed explicit unbind is direct; deleting a bound proxy is failure.
			account.ProxyID = nil
			account.Proxy = nil
			repo.replace(account)
			run("direct")
			account.ProxyID = &oldProxy.ID
			repo.replace(account)
			network.reset()
			require.Error(t, operation.execute(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()), repo, network))
			require.Empty(t, network.captures(), "missing bound proxy must fail before any local/direct network request")
			account.Proxy = oldProxy
			repo.replace(account)
			run("old")
		})
	}
}

func TestAuxiliarySendersProxyFailureNeverDialsDirectDestination(t *testing.T) {
	for _, operation := range auxiliaryWireOperations() {
		paths := operation.failurePaths
		if len(paths) == 0 {
			paths = []string{""}
		}
		for pathIndex, path := range paths {
			for _, failure := range []string{"timeout", "refused", "authentication_required"} {
				if failure == "refused" && pathIndex > 0 {
					continue
				}
				t.Run(operation.name+"/"+path+"/"+failure, func(t *testing.T) {
					network := newAuxiliaryWireNetwork(t)
					proxy := network.proxy(t, "old", 7)
					network.failure, network.failurePath = failure, path
					network.installProbeClients(t, "", proxy.URL())
					account := operation.account()
					account.ProxyID = &proxy.ID
					account.Proxy = proxy
					repo := &auxiliaryOwnerRepo{account: account}
					if failure == "refused" {
						network.proxies["old"].Close()
					}
					ctx, cancel := context.WithTimeout(WithOutboundIDStore(context.Background(), newTestOutboundIDStore()), 200*time.Millisecond)
					defer cancel()
					err := operation.execute(ctx, repo, network)
					// WHAM credit detail reads are deliberately partial-success; the usage
					// result may survive a detail failure, but that detail cannot go direct.
					if operation.name != "wham-usage-credit-details" || !strings.HasSuffix(path, "/rate-limit-reset-credits") {
						require.Error(t, err)
					}
					require.Zero(t, network.directConnections.Load(), "proxy failure must not open even one direct destination connection")
					for _, capture := range network.captures() {
						require.NotEqual(t, "direct", capture.route, "configured proxy failure must never dial the reachable host destination")
					}
				})
			}
		}
	}
}
