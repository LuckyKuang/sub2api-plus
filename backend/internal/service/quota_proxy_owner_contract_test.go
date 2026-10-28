//go:build unit

package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type quotaProxyOwnerCase struct{ name, provider, operation, ownerKind string }

func quotaProxyOwnerCases() []quotaProxyOwnerCase {
	return []quotaProxyOwnerCase{
		{"openai-usage", PlatformOpenAI, "usage", "oauth"},
		{"openai-reset", PlatformOpenAI, "reset", "oauth"},
		{"openai-targeted-reset", PlatformOpenAI, "targeted-reset", "oauth"},
		{"openai-agent-usage", PlatformOpenAI, "usage", "agent"},
		{"openai-agent-reset", PlatformOpenAI, "reset", "agent"},
		{"openai-shadow-usage", PlatformOpenAI, "usage", "shadow"},
		{"grok-usage", PlatformGrok, "usage", "oauth"},
		{"grok-billing", PlatformGrok, "billing", "oauth"},
	}
}

type quotaProxyOwnerRepo struct {
	AccountRepository
	accounts map[int64]*Account
	writes   int
}

func (r *quotaProxyOwnerRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	return r.accounts[id], nil
}

func (r *quotaProxyOwnerRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.writes++
	for key, value := range updates {
		r.accounts[id].Extra[key] = value
	}
	return nil
}

func (r *quotaProxyOwnerRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.writes++
	r.accounts[id].Credentials = credentials
	return nil
}

func (r *quotaProxyOwnerRepo) UpdateOpenAIQuotaCredentialsIfUnchanged(ctx context.Context, expected *Account, credentials map[string]any) (bool, error) {
	if !quotaPersistenceOwnerMatches(r.accounts[expected.ID], expected) {
		return false, nil
	}
	return true, r.UpdateCredentials(ctx, expected.ID, credentials)
}

type quotaProxyOwnerProxyRepo struct {
	ProxyRepository
	row   *Proxy
	calls int
}

func (r *quotaProxyOwnerProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	r.calls++
	return r.row, nil
}

type quotaProxyOwnerRefresh struct{ calls int }

func (*quotaProxyOwnerRefresh) CanRefresh(*Account) bool                  { return true }
func (*quotaProxyOwnerRefresh) NeedsRefresh(*Account, time.Duration) bool { return true }
func (*quotaProxyOwnerRefresh) CacheKey(*Account) string                  { return "quota-route-business-contract" }
func (r *quotaProxyOwnerRefresh) Refresh(context.Context, *Account) (map[string]any, error) {
	r.calls++
	return nil, errors.New("quota route test refresh reached provider")
}

type quotaProxyOwnerWireCall struct {
	request *http.Request
	route   string
}

type quotaProxyOwnerWire struct {
	mu        sync.Mutex
	calls     []quotaProxyOwnerWireCall
	factories int
	onFactory func()
	respond   func(*http.Request) (int, string)
}

func (w *quotaProxyOwnerWire) reply(request *http.Request, route string) *http.Response {
	w.mu.Lock()
	w.calls = append(w.calls, quotaProxyOwnerWireCall{request: request.Clone(request.Context()), route: route})
	w.mu.Unlock()
	body := `{"id":"quota-probe","output":[]}`
	switch request.URL.Path {
	case "/backend-api/wham/usage":
		body = `{"plan_type":"pro","rate_limit":{"allowed":true}}`
	case "/backend-api/wham/rate-limit-reset-credits":
		body = `{"available_count":1,"credits":[]}`
	case "/backend-api/wham/rate-limit-reset-credits/consume":
		body = `{"code":"ok","windows_reset":1}`
	case "/v1/billing":
		body = `{"config":{"currentPeriod":{"type":"WEEKLY"},"creditUsagePercent":12}}`
	}
	status := http.StatusOK
	if w.respond != nil {
		status, body = w.respond(request)
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func (w *quotaProxyOwnerWire) factory(route string) (*req.Client, error) {
	w.mu.Lock()
	w.factories++
	w.mu.Unlock()
	if w.onFactory != nil {
		w.onFactory()
	}
	client := req.C().SetTimeout(time.Second)
	client.GetTransport().WrapRoundTripFunc(func(http.RoundTripper) req.HttpRoundTripFunc {
		return func(request *http.Request) (*http.Response, error) { return w.reply(request, route), nil }
	})
	return client, nil
}

func (w *quotaProxyOwnerWire) Do(request *http.Request, route string, _ int64, _ int) (*http.Response, error) {
	return w.reply(request, route), nil
}

func (w *quotaProxyOwnerWire) DoWithTLS(request *http.Request, route string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return w.Do(request, route, id, concurrency)
}

func (w *quotaProxyOwnerWire) snapshot() ([]quotaProxyOwnerWireCall, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]quotaProxyOwnerWireCall(nil), w.calls...), w.factories
}

func (tc quotaProxyOwnerCase) fixture(t *testing.T) (*quotaProxyOwnerRepo, *Account, int64) {
	t.Helper()
	proxyID := int64(71)
	owner := &Account{
		ID: 974, Platform: tc.provider, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 2,
		Credentials: map[string]any{
			"access_token": "quota-owner-access", "refresh_token": "quota-owner-refresh",
			"expires_at":         time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			"chatgpt_account_id": "quota-owner-account", "sub": "quota-owner-user",
		},
		Extra:   map[string]any{"business_marker": "preserve-existing-observation"},
		ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "captured.proxy.example", Port: 8080, Username: "captured-user", Password: "captured-password"},
	}
	if tc.ownerKind == "agent" {
		key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
		der, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		owner.Credentials = map[string]any{
			"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": "quota-owner-runtime",
			"agent_private_key": base64.StdEncoding.EncodeToString(der), "task_id": "quota-owner-task",
			"chatgpt_account_id": "quota-owner-account",
		}
	}
	repo := &quotaProxyOwnerRepo{accounts: map[int64]*Account{owner.ID: owner}}
	requestID := owner.ID
	if tc.ownerKind == "shadow" {
		parentID := owner.ID
		shadow := &Account{ID: 975, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, ParentAccountID: &parentID, QuotaDimension: QuotaDimensionSpark, Extra: map[string]any{"business_marker": "shadow-observation"}}
		repo.accounts[shadow.ID], requestID = shadow, shadow.ID
	}
	return repo, owner, requestID
}

func (tc quotaProxyOwnerCase) query(repo *quotaProxyOwnerRepo, proxies ProxyRepository, wire *quotaProxyOwnerWire, refresh *quotaProxyOwnerRefresh, requestID int64) (any, error) {
	refreshAPI := NewOAuthRefreshAPI(repo, nil)
	if tc.provider == PlatformGrok {
		tokens := NewGrokTokenProvider(repo, nil)
		tokens.SetRefreshAPI(refreshAPI, refresh)
		service := NewGrokQuotaService(repo, proxies, tokens, wire, nil)
		if tc.operation == "billing" {
			return service.ProbeBilling(context.Background(), requestID)
		}
		return service.ProbeUsage(context.Background(), requestID)
	}
	tokens := NewOpenAITokenProvider(repo, nil, nil)
	tokens.SetRefreshAPI(refreshAPI, refresh)
	service := NewOpenAIQuotaService(repo, proxies, tokens, wire.factory)
	switch tc.operation {
	case "reset":
		return service.ResetCredit(context.Background(), requestID)
	case "targeted-reset":
		return service.ResetCreditTargeted(context.Background(), requestID, "quota-credit", "quota-redeem")
	default:
		return service.QueryUsage(context.Background(), requestID)
	}
}

func TestQuotaControlRejectsBrokenProxyBeforeCredentialsOrHTTP(t *testing.T) {
	for _, tc := range quotaProxyOwnerCases() {
		for _, failure := range []string{"missing-snapshot", "missing-snapshot-and-row", "mismatched-owner", "malformed-host", "username-without-password", "password-without-username"} {
			t.Run(tc.name+"/"+failure, func(t *testing.T) {
				repo, owner, requestID := tc.fixture(t)
				proxies := &quotaProxyOwnerProxyRepo{row: &Proxy{ID: 71, Protocol: "http", Host: "replacement.proxy.example", Port: 8888}}
				switch failure {
				case "missing-snapshot":
					owner.Proxy = nil
				case "missing-snapshot-and-row":
					owner.Proxy, proxies.row = nil, nil
				case "mismatched-owner":
					owner.Proxy.ID = 72
				case "malformed-host":
					owner.Proxy.Host = "captured.proxy.example/foreign-route"
				case "username-without-password":
					owner.Proxy.Password = ""
				case "password-without-username":
					owner.Proxy.Username = ""
				}
				before, err := json.Marshal(repo.accounts)
				require.NoError(t, err)
				wire, refresh := &quotaProxyOwnerWire{}, &quotaProxyOwnerRefresh{}
				result, err := tc.query(repo, proxies, wire, refresh, requestID)
				assert.Error(t, err, "the captured credential owner does not authorize repairing or replacing this bound route")
				assert.Nil(t, result)
				calls, factories := wire.snapshot()
				assert.Empty(t, calls, "no usage query or credit consumption may reach the provider")
				assert.Zero(t, factories, "route validation precedes client creation")
				assert.Zero(t, refresh.calls)
				assert.Zero(t, proxies.calls)
				assert.Zero(t, repo.writes)
				if err != nil {
					assert.NotContains(t, err.Error(), "captured-password")
					assert.NotContains(t, err.Error(), "quota-owner-access")
				}
				after, marshalErr := json.Marshal(repo.accounts)
				require.NoError(t, marshalErr)
				assert.Equal(t, before, after, "failed routing cannot mutate the captured owner or existing observations")
			})
		}
	}
}

func TestQuotaControlBrokenBindingStopsNearExpiryRefresh(t *testing.T) {
	for _, tc := range []quotaProxyOwnerCase{
		{"openai-usage", PlatformOpenAI, "usage", "oauth"}, {"openai-reset", PlatformOpenAI, "reset", "oauth"},
		{"grok-usage", PlatformGrok, "usage", "oauth"}, {"grok-billing", PlatformGrok, "billing", "oauth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, owner, requestID := tc.fixture(t)
			owner.Proxy.ID = 72
			owner.Credentials["expires_at"] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
			before, err := json.Marshal(repo.accounts)
			require.NoError(t, err)
			proxies, wire, refresh := &quotaProxyOwnerProxyRepo{}, &quotaProxyOwnerWire{}, &quotaProxyOwnerRefresh{}
			_, err = tc.query(repo, proxies, wire, refresh, requestID)
			assert.Error(t, err)
			assert.Zero(t, refresh.calls, "an invalid route must stop before even a due credential refresh")
			calls, factories := wire.snapshot()
			assert.Empty(t, calls)
			assert.Zero(t, factories)
			assert.Zero(t, proxies.calls)
			assert.Zero(t, repo.writes)
			after, marshalErr := json.Marshal(repo.accounts)
			require.NoError(t, marshalErr)
			assert.Equal(t, before, after)
		})
	}
}

func TestQuotaControlUsesCapturedRouteAndExplicitUnbinding(t *testing.T) {
	for _, tc := range quotaProxyOwnerCases() {
		for _, route := range []string{"captured-bound-route", "saved-unbinding"} {
			t.Run(tc.name+"/"+route, func(t *testing.T) {
				repo, owner, requestID := tc.fixture(t)
				expectedRoute := "http://captured-user:captured-password@captured.proxy.example:8080"
				if route == "saved-unbinding" {
					owner.ProxyID = nil
					expectedRoute = ""
				}
				proxies := &quotaProxyOwnerProxyRepo{row: &Proxy{ID: 71, Protocol: "http", Host: "replacement.proxy.example", Port: 8888}}
				wire, refresh := &quotaProxyOwnerWire{}, &quotaProxyOwnerRefresh{}
				result, err := tc.query(repo, proxies, wire, refresh, requestID)
				require.NoError(t, err)
				require.NotNil(t, result)
				calls, factories := wire.snapshot()
				expectedCalls := 1
				if tc.provider == PlatformOpenAI && tc.operation == "usage" || tc.operation == "billing" {
					expectedCalls = 2
				}
				require.Len(t, calls, expectedCalls)
				if tc.provider == PlatformOpenAI {
					require.Equal(t, 1, factories)
				} else {
					require.Zero(t, factories)
				}
				for _, call := range calls {
					require.Equal(t, expectedRoute, call.route)
					if tc.provider == PlatformOpenAI {
						require.Equal(t, "chatgpt.com", call.request.URL.Host)
						require.Equal(t, "quota-owner-account", call.request.Header.Get("Chatgpt-Account-Id"))
						if tc.ownerKind == "agent" {
							require.True(t, strings.HasPrefix(call.request.Header.Get("Authorization"), "AgentAssertion "))
						} else {
							require.Equal(t, "Bearer quota-owner-access", call.request.Header.Get("Authorization"))
						}
					} else {
						require.Equal(t, "cli-chat-proxy.grok.com", call.request.URL.Host)
						require.Equal(t, "Bearer quota-owner-access", call.request.Header.Get("Authorization"))
					}
				}
				require.Zero(t, proxies.calls)
				require.Zero(t, refresh.calls)
				require.Equal(t, "preserve-existing-observation", owner.Extra["business_marker"])
			})
		}
	}
}

type quotaProxyOwnerTokenCache struct {
	GeminiTokenCache
	onRead         func()
	miss           bool
	writes         int
	reads          int
	cachedToken    string
	afterMissToken string
	lockHeld       bool
}

func (c *quotaProxyOwnerTokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.onRead()
	c.reads++
	if c.reads > 1 && c.afterMissToken != "" {
		return c.afterMissToken, nil
	}
	if c.miss {
		return "", nil
	}
	if c.cachedToken != "" {
		return c.cachedToken, nil
	}
	return "quota-owner-access", nil
}

func (c *quotaProxyOwnerTokenCache) SetAccessToken(context.Context, string, string, time.Duration) error {
	c.writes++
	return nil
}

func (c *quotaProxyOwnerTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return !c.lockHeld, nil
}

func (*quotaProxyOwnerTokenCache) ReleaseRefreshLock(context.Context, string) error { return nil }

type quotaOwnerRotatingRefresh struct {
	calls     int
	onRefresh func()
}

func (*quotaOwnerRotatingRefresh) CanRefresh(*Account) bool                  { return true }
func (*quotaOwnerRotatingRefresh) NeedsRefresh(*Account, time.Duration) bool { return true }
func (*quotaOwnerRotatingRefresh) CacheKey(account *Account) string {
	return OpenAITokenCacheKey(account)
}
func (r *quotaOwnerRotatingRefresh) Refresh(_ context.Context, account *Account) (map[string]any, error) {
	r.calls++
	credentials := shallowCopyMap(account.Credentials)
	credentials["access_token"] = "same-owner-rotated-access"
	credentials["refresh_token"] = "same-owner-rotated-refresh"
	credentials["expires_at"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	if r.onRefresh != nil {
		r.onRefresh()
	}
	return credentials, nil
}

func TestOpenAIQuotaTokenOwnerRejectsRelinkBeforeAdoptionOrRefresh(t *testing.T) {
	for _, operation := range []string{"usage", "reset"} {
		for _, phase := range []string{"cache-version-relink", "refresh-reread-relink", "refresh-in-flight-relink", "same-owner-rotation", "cache-hit-replacement-token", "same-owner-cache-positive", "refresh-lock-wait-replacement-token", "legacy-lock-wait-replacement-token"} {
			t.Run(operation+"/"+phase, func(t *testing.T) {
				tc := quotaProxyOwnerCase{provider: PlatformOpenAI, operation: operation, ownerKind: "oauth"}
				repo, owner, requestID := tc.fixture(t)
				owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color"
				owner.Credentials["_token_version"] = int64(1)
				if phase != "cache-version-relink" && phase != "cache-hit-replacement-token" && phase != "same-owner-cache-positive" {
					owner.Credentials["expires_at"] = time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
				}
				relink := func() {
					credentials := shallowCopyMap(owner.Credentials)
					credentials["access_token"] = "replacement-access"
					credentials["refresh_token"] = "replacement-refresh"
					credentials["chatgpt_account_id"] = "replacement-account"
					credentials["user_agent"] = "codex-tui/0.158.0 (Mac OS X 14.0; arm64) iTerm"
					credentials["_token_version"] = int64(2)
					owner.Credentials = credentials
				}
				cache := &quotaProxyOwnerTokenCache{miss: true, onRead: func() {
					if phase == "cache-version-relink" || phase == "refresh-reread-relink" || phase == "cache-hit-replacement-token" {
						relink()
					}
				}}
				if phase == "cache-hit-replacement-token" {
					cache.miss, cache.cachedToken = false, "replacement-access"
				}
				if phase == "same-owner-cache-positive" {
					cache.miss = false
				}
				if phase == "refresh-lock-wait-replacement-token" || phase == "legacy-lock-wait-replacement-token" {
					cache.afterMissToken, cache.lockHeld = "unowned-cached-access", true
				}
				refresh := &quotaOwnerRotatingRefresh{}
				if phase == "refresh-in-flight-relink" {
					refresh.onRefresh = relink
				}
				tokens := NewOpenAITokenProvider(repo, cache, nil)
				tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), refresh)
				if phase == "refresh-lock-wait-replacement-token" {
					tokens.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), refresh)
				}
				if phase == "legacy-lock-wait-replacement-token" {
					tokens.SetRefreshAPI(nil, nil)
				}
				wire := &quotaProxyOwnerWire{}
				service := NewOpenAIQuotaService(repo, nil, tokens, wire.factory)
				var err error
				if operation == "reset" {
					_, err = service.ResetCredit(context.Background(), requestID)
				} else {
					_, err = service.QueryUsage(context.Background(), requestID)
				}
				calls, factories := wire.snapshot()
				if phase == "same-owner-rotation" || phase == "same-owner-cache-positive" || phase == "refresh-lock-wait-replacement-token" || phase == "legacy-lock-wait-replacement-token" {
					require.NoError(t, err)
					bearer := "Bearer quota-owner-access"
					if phase == "same-owner-rotation" {
						bearer = "Bearer same-owner-rotated-access"
						require.Equal(t, 1, refresh.calls)
					} else {
						require.Zero(t, refresh.calls)
					}
					require.NotEmpty(t, calls)
					for _, call := range calls {
						require.Equal(t, bearer, call.request.Header.Get("Authorization"))
						require.Equal(t, "quota-owner-account", call.request.Header.Get("Chatgpt-Account-Id"))
						require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color", call.request.UserAgent())
						require.Equal(t, "http://captured-user:captured-password@captured.proxy.example:8080", call.route)
					}
					if phase == "same-owner-rotation" {
						require.Equal(t, "same-owner-rotated-refresh", owner.GetCredential("refresh_token"))
					} else {
						require.Equal(t, "quota-owner-refresh", owner.GetCredential("refresh_token"))
					}
					return
				}
				require.Error(t, err, "a relink cannot authorize adopting or refreshing replacement credentials")
				require.Empty(t, calls)
				require.Zero(t, factories)
				require.Zero(t, repo.writes, "an in-flight rotation cannot overwrite a replacement login")
				require.Zero(t, cache.writes, "a rejected owner's token cannot populate the account cache")
				require.Equal(t, "replacement-account", owner.GetCredential("chatgpt_account_id"))
				require.Equal(t, "replacement-refresh", owner.GetCredential("refresh_token"))
				if phase == "refresh-in-flight-relink" {
					require.Equal(t, 1, refresh.calls)
				} else {
					require.Zero(t, refresh.calls, "the replacement owner must be rejected before issuer I/O")
				}
			})
		}
	}
}

func TestOpenAIQuotaCapturesRouteBeforeTokenLookup(t *testing.T) {
	for _, operation := range []string{"usage", "reset"} {
		t.Run(operation, func(t *testing.T) {
			tc := quotaProxyOwnerCase{provider: PlatformOpenAI, operation: operation, ownerKind: "oauth"}
			repo, owner, requestID := tc.fixture(t)
			owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color"
			cache := &quotaProxyOwnerTokenCache{onRead: func() {
				owner.Proxy.Host = "edited-during-token.proxy.example"
				owner.Proxy.Password = "replacement-password"
				owner.Credentials["user_agent"] = "codex-tui/0.158.0 (Mac OS X 14.0; arm64) iTerm"
			}}
			wire := &quotaProxyOwnerWire{}
			service := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), wire.factory)
			var err error
			if operation == "reset" {
				_, err = service.ResetCredit(context.Background(), requestID)
			} else {
				_, err = service.QueryUsage(context.Background(), requestID)
			}
			require.NoError(t, err)
			calls, _ := wire.snapshot()
			require.NotEmpty(t, calls)
			for _, call := range calls {
				require.Equal(t, "http://captured-user:captured-password@captured.proxy.example:8080", call.route,
					"a later proxy edit cannot replace the route already captured for this credential operation")
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color", call.request.UserAgent(),
					"a later identity edit cannot accompany an earlier owner's bearer and route")
				require.Empty(t, call.request.Header.Get("Originator"))
				require.Empty(t, call.request.Header.Get("Version"))
			}
			require.Equal(t, "edited-during-token.proxy.example", owner.Proxy.Host)
		})
	}
}

func TestOpenAIQuotaCredentialOwnerSnapshotDoesNotAdoptReplacementAgent(t *testing.T) {
	for _, operation := range []string{"usage", "reset"} {
		t.Run(operation, func(t *testing.T) {
			tc := quotaProxyOwnerCase{provider: PlatformOpenAI, operation: operation, ownerKind: "oauth"}
			repo, owner, requestID := tc.fixture(t)
			owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color"
			agentCase := quotaProxyOwnerCase{provider: PlatformOpenAI, ownerKind: "agent"}
			_, replacement, _ := agentCase.fixture(t)
			replacement.Credentials["chatgpt_account_id"] = "replacement-agent-account"
			replacement.Credentials["agent_runtime_id"] = "replacement-agent-runtime"
			replacement.Credentials["task_id"] = "replacement-agent-task"
			replacement.Credentials["user_agent"] = "codex-tui/0.158.0 (Mac OS X 14.0; arm64) iTerm"
			cache := &quotaProxyOwnerTokenCache{onRead: func() { owner.Credentials = replacement.Credentials }}
			wire := &quotaProxyOwnerWire{}
			service := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), wire.factory)
			var err error
			if operation == "reset" {
				_, err = service.ResetCredit(context.Background(), requestID)
			} else {
				_, err = service.QueryUsage(context.Background(), requestID)
			}
			require.NoError(t, err)
			calls, _ := wire.snapshot()
			require.NotEmpty(t, calls)
			for _, call := range calls {
				require.Equal(t, "Bearer quota-owner-access", call.request.Header.Get("Authorization"), "an OAuth operation cannot switch to a replacement login's AgentAssertion")
				require.Equal(t, "quota-owner-account", call.request.Header.Get("Chatgpt-Account-Id"))
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color", call.request.UserAgent())
			}
		})
	}
}

func TestOpenAIQuotaCredentialOwnerSnapshotKeepsCapturedAgentAssertion(t *testing.T) {
	for _, operation := range []string{"usage", "reset"} {
		t.Run(operation, func(t *testing.T) {
			tc := quotaProxyOwnerCase{provider: PlatformOpenAI, operation: operation, ownerKind: "agent"}
			repo, owner, requestID := tc.fixture(t)
			owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color"
			wire := &quotaProxyOwnerWire{onFactory: func() {
				owner.Credentials = map[string]any{"access_token": "replacement-oauth-access", "chatgpt_account_id": "replacement-oauth-account", "user_agent": "codex-tui/0.158.0 (Mac OS X 14.0; arm64) iTerm"}
			}}
			service := NewOpenAIQuotaService(repo, nil, nil, wire.factory)
			var err error
			if operation == "reset" {
				_, err = service.ResetCredit(context.Background(), requestID)
			} else {
				_, err = service.QueryUsage(context.Background(), requestID)
			}
			require.NoError(t, err)
			calls, _ := wire.snapshot()
			require.NotEmpty(t, calls)
			for _, call := range calls {
				require.True(t, strings.HasPrefix(call.request.Header.Get("Authorization"), "AgentAssertion "))
				require.Equal(t, "quota-owner-task", decodeAgentAssertionTask(t, call.request.Header.Get("Authorization")))
				require.Equal(t, "quota-owner-account", call.request.Header.Get("Chatgpt-Account-Id"))
				require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; aarch64) xterm-256color", call.request.UserAgent())
			}
		})
	}
}

func TestOpenAIQuotaCredentialOwnerSnapshotRejectsTaskRecoveryAfterRelink(t *testing.T) {
	for _, operation := range []string{"usage", "reset"} {
		t.Run(operation, func(t *testing.T) {
			tc := quotaProxyOwnerCase{provider: PlatformOpenAI, operation: operation, ownerKind: "agent"}
			repo, owner, requestID := tc.fixture(t)
			agentCase := quotaProxyOwnerCase{provider: PlatformOpenAI, ownerKind: "agent"}
			_, replacement, _ := agentCase.fixture(t)
			replacement.Credentials["agent_runtime_id"] = "replacement-runtime"
			replacement.Credentials["chatgpt_account_id"] = "replacement-account"
			replacement.Credentials["task_id"] = "replacement-task"
			registerCalls := 0
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				registerCalls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"task_id":"forbidden-replacement-registration"}`)
			}))
			defer registry.Close()
			oldBase := openAIAgentIdentityAuthAPIBaseURL
			openAIAgentIdentityAuthAPIBaseURL = registry.URL
			defer func() { openAIAgentIdentityAuthAPIBaseURL = oldBase }()
			wire := &quotaProxyOwnerWire{respond: func(*http.Request) (int, string) {
				owner.Credentials = replacement.Credentials
				return http.StatusUnauthorized, `{"error":{"code":"invalid_task_id"}}`
			}}
			service := NewOpenAIQuotaService(repo, nil, nil, wire.factory)
			var err error
			if operation == "reset" {
				_, err = service.ResetCredit(context.Background(), requestID)
			} else {
				_, err = service.QueryUsage(context.Background(), requestID)
			}
			require.Error(t, err)
			calls, _ := wire.snapshot()
			require.Len(t, calls, 1, "a replacement credential owner cannot authorize replaying the original operation")
			require.Equal(t, "quota-owner-task", decodeAgentAssertionTask(t, calls[0].request.Header.Get("Authorization")))
			require.Zero(t, registerCalls)
			require.Zero(t, repo.writes)
			require.Equal(t, "replacement-task", owner.GetCredential("task_id"))
		})
	}
}
