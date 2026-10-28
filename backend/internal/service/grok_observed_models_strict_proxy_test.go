//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type grokObservedStrictAccountRepo struct {
	AccountRepository
	account      *Account
	legacyWrites int
	casCalls     int
	ownerReads   int
	writes       int
	updates      map[string]any
	done         chan struct{}
	doneOnce     sync.Once
}

func (r *grokObservedStrictAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.legacyWrites++
	r.finish()
	return nil
}

// The independent fake models the atomic, complete credential/proxy-owner CAS.
// A successful discovery cannot turn a stale login into fresh model metadata.
func (r *grokObservedStrictAccountRepo) UpdateCNControlExtraIfUnchanged(_ context.Context, expected *Account, updates map[string]any) (bool, error) {
	defer r.finish()
	r.casCalls++
	current := r.account
	if current == nil || expected == nil || current.ID != expected.ID || current.Platform != expected.Platform || current.Type != expected.Type || !reflect.DeepEqual(current.ProxyID, expected.ProxyID) || current.ProxyID != nil && !sameTestCNProxySnapshot(current.Proxy, expected.Proxy) {
		return false, nil
	}
	currentRoute, currentRouteErr := current.StrictProxyURL()
	expectedRoute, expectedRouteErr := expected.StrictProxyURL()
	if currentRouteErr != nil || expectedRouteErr != nil || currentRoute != expectedRoute {
		return false, nil
	}
	currentJSON, currentErr := json.Marshal(current.Credentials)
	expectedJSON, expectedErr := json.Marshal(expected.Credentials)
	if currentErr != nil || expectedErr != nil || string(currentJSON) != string(expectedJSON) {
		return false, nil
	}
	r.writes++
	r.updates = updates
	return true, nil
}

func (r *grokObservedStrictAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.ownerReads++
	defer r.finish()
	if r.account == nil || r.account.ID != id {
		return nil, ErrAccountNotFound
	}
	return snapshotRequestCredentialAccount(r.account), nil
}

func (r *grokObservedStrictAccountRepo) CompareAndSwapGrokObservedModels(ctx context.Context, owner *Account, expected, next json.RawMessage) (bool, error) {
	var current any
	if r.account != nil && r.account.Extra != nil {
		current = r.account.Extra[grokObservedModelsExtraKey]
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		return false, err
	}
	if !jsonValuesEqual(encoded, expected) {
		return false, nil
	}
	var value any
	if err := json.Unmarshal(next, &value); err != nil {
		return false, err
	}
	return r.UpdateCNControlExtraIfUnchanged(ctx, owner, map[string]any{grokObservedModelsExtraKey: value})
}

func (r *grokObservedStrictAccountRepo) finish() {
	if r.done != nil {
		r.doneOnce.Do(func() { close(r.done) })
	}
}

type grokObservedStrictProxyRepo struct {
	ProxyRepository
	calls int
	row   *Proxy
}

func (r *grokObservedStrictProxyRepo) GetByID(context.Context, int64) (*Proxy, error) {
	r.calls++
	return r.row, nil
}

type grokObservedStrictUpstream struct {
	HTTPUpstream
	calls                int
	request              *http.Request
	proxy                string
	account, concurrency int
	beforeReply          func()
}

func (u *grokObservedStrictUpstream) Do(req *http.Request, proxy string, account int64, concurrency int) (*http.Response, error) {
	u.calls++
	u.request, u.proxy, u.account, u.concurrency = req, proxy, int(account), concurrency
	if u.beforeReply != nil {
		u.beforeReply()
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"grok-4.3"},{"id":"grok-4.3-fast"}]}`))}, nil
}

func grokObservedBusinessAccount(id int64) *Account {
	proxyID := id - 1
	return &Account{ID: id, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 3,
		Credentials: map[string]any{
			"access_token": "private-observed-models-owner-token", "base_url": "https://models.owner.example/v1",
			"grant_metadata":    map[string]any{"subject": "captured-subject"},
			"outbound_identity": map[string]any{"preset": "grok", "user_agent": "grok-shell/1.0.45 (linux; x86_64)", "version": "1.0.45"},
		},
		Extra: map[string]any{"management_marker": "preserve"}, ProxyID: &proxyID,
		Proxy: &Proxy{ID: proxyID, Protocol: "http", Host: "captured.proxy.example", Port: 8080,
			Username: "private-observed-proxy-user", Password: "private-observed-proxy-password", Status: StatusActive}}
}

// Background /v1/models discovery has its own sender. A saved binding permits
// only the captured route; neither mutable proxy lookup nor failed hydration
// can replace that owner or authorize direct egress and model-schema writes.
func TestGrokObservedModelsSyncRejectsBrokenProxyBeforeLookupHTTPOrSchemaWrite(t *testing.T) {
	t.Setenv("XAI_GROK_CLI_VERSION", "")
	for _, mode := range []string{
		"valid-bound", "explicitly-unbound", "missing-bound-snapshot", "missing-bound-snapshot-and-row",
		"mismatched-bound-owner", "malformed-bound-host", "username-without-password", "password-without-username",
	} {
		t.Run(mode, func(t *testing.T) {
			account := grokObservedBusinessAccount(7932)
			proxies := &grokObservedStrictProxyRepo{row: &Proxy{ID: *account.ProxyID, Protocol: "http", Host: "replacement.proxy.example", Port: 9090}}
			switch mode {
			case "explicitly-unbound":
				account.ProxyID = nil // The stale hydrated association cannot undo saved unbinding.
			case "missing-bound-snapshot":
				account.Proxy = nil
			case "missing-bound-snapshot-and-row":
				account.Proxy, proxies.row = nil, nil
			case "mismatched-bound-owner":
				account.Proxy.ID++
			case "malformed-bound-host":
				account.Proxy.Host = "captured.proxy.example/foreign-route"
			case "username-without-password":
				account.Proxy.Password = ""
			case "password-without-username":
				account.Proxy.Username = ""
			}
			credentialsBefore, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			extraBefore, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			repo := &grokObservedStrictAccountRepo{account: account}
			upstream := &grokObservedStrictUpstream{}
			svc := NewGrokQuotaService(repo, proxies, nil, upstream, &config.Config{})
			syncErr := svc.syncGrokObservedModels(context.Background(), account)
			if mode == "valid-bound" || mode == "explicitly-unbound" {
				require.NoError(t, syncErr)
				require.Equal(t, 1, upstream.calls)
				require.Zero(t, proxies.calls, "discovery must not reread a mutable replacement route")
				if mode == "valid-bound" {
					require.Equal(t, "http://private-observed-proxy-user:private-observed-proxy-password@captured.proxy.example:8080", upstream.proxy)
				} else {
					require.Empty(t, upstream.proxy)
				}
				require.Equal(t, int(account.ID), upstream.account)
				require.Equal(t, 3, upstream.concurrency)
				require.Equal(t, http.MethodGet, upstream.request.Method)
				require.Equal(t, "https://models.owner.example/v1/models", upstream.request.URL.String())
				require.Equal(t, "Bearer private-observed-models-owner-token", upstream.request.Header.Get("Authorization"))
				require.Equal(t, "grok-shell/1.0.45 (linux; x86_64)", upstream.request.UserAgent())
				require.Equal(t, "grok-shell", upstream.request.Header.Get("X-Grok-Client-Identifier"))
				require.Equal(t, "1.0.45", upstream.request.Header.Get("X-Grok-Client-Version"))
				require.Equal(t, 1, repo.casCalls)
				require.Equal(t, 1, repo.writes)
				observation, ok := repo.updates[grokObservedModelsExtraKey].(map[string]any)
				require.True(t, ok)
				require.Equal(t, []any{"grok-4.3", "grok-4.3-fast"}, observation["models"])
				require.Equal(t, "upstream_v1_models", observation["source"])
				require.NotEmpty(t, observation["fetched_at"])
			} else {
				assert.Zero(t, upstream.calls, "reject broken bindings before sending the captured bearer")
				assert.Zero(t, proxies.calls, "failed hydration cannot adopt a mutable replacement proxy")
				assert.Zero(t, repo.writes, "a rejected discovery must not publish model-schema observations")
				assert.Zero(t, repo.casCalls, "broken bindings must be rejected before model-schema persistence")
				require.Error(t, syncErr)
				for _, secret := range []string{"private-observed-models-owner-token", "private-observed-proxy-user", "private-observed-proxy-password"} {
					require.NotContains(t, syncErr.Error(), secret)
				}
			}
			require.Zero(t, repo.legacyWrites, "observed models require complete owner-conditional persistence")
			credentialsAfter, err := json.Marshal(account.Credentials)
			require.NoError(t, err)
			extraAfter, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			require.Equal(t, credentialsBefore, credentialsAfter)
			require.Equal(t, extraBefore, extraAfter)
		})
	}
}

func TestGrokObservedModelsSyncRejectsLateCredentialAndProxyOwnerChanges(t *testing.T) {
	for _, mutation := range []string{"unchanged", "login-in-place", "replacement-login", "nested-credentials", "saved-unbinding", "saved-rebinding", "same-row-host", "same-row-password", "same-row-status", "same-row-expiry"} {
		t.Run(mutation, func(t *testing.T) {
			account := grokObservedBusinessAccount(7942)
			repo := &grokObservedStrictAccountRepo{account: account}
			upstream := &grokObservedStrictUpstream{beforeReply: func() {
				switch mutation {
				case "login-in-place":
					account.Credentials["access_token"] = "replacement-login-token"
				case "replacement-login":
					replacement := grokObservedBusinessAccount(account.ID)
					replacement.Credentials["access_token"] = "replacement-login-token"
					repo.account = replacement
				case "nested-credentials":
					account.Credentials["grant_metadata"].(map[string]any)["subject"] = "replacement-subject"
				case "saved-unbinding":
					account.ProxyID = nil
				case "saved-rebinding":
					*account.ProxyID = 9991
					account.Proxy.ID = 9991
				case "same-row-host":
					account.Proxy.Host = "replacement.proxy.example"
				case "same-row-password":
					account.Proxy.Password = "replacement-proxy-password"
				case "same-row-status":
					account.Proxy.Status = "inactive"
				case "same-row-expiry":
					expires := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
					account.Proxy.ExpiresAt = &expires
				}
			}}
			svc := NewGrokQuotaService(repo, nil, nil, upstream, &config.Config{})
			syncErr := svc.syncGrokObservedModels(context.Background(), account)
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, "Bearer private-observed-models-owner-token", upstream.request.Header.Get("Authorization"))
			require.Equal(t, "http://private-observed-proxy-user:private-observed-proxy-password@captured.proxy.example:8080", upstream.proxy)
			require.Equal(t, 1, repo.ownerReads, "a late reply must recheck the complete captured owner before persistence")
			if mutation == "unchanged" {
				require.Equal(t, 1, repo.casCalls)
			} else {
				require.Zero(t, repo.casCalls)
			}
			require.Zero(t, repo.legacyWrites, "account-ID writes would publish observations into a replacement login")
			if mutation == "unchanged" {
				require.NoError(t, syncErr)
				require.Equal(t, 1, repo.writes)
				require.Contains(t, repo.updates, grokObservedModelsExtraKey)
			} else {
				require.Zero(t, repo.writes, "the successful HTTP reply belongs to the owner that sent it")
				require.Nil(t, repo.updates)
			}
			require.Equal(t, "preserve", repo.account.Extra["management_marker"])
			require.NotContains(t, repo.account.Extra, grokObservedModelsExtraKey)
		})
	}
}

// The settings callback is a synchronous scheduling barrier. All owner data
// must already be frozen when it runs, before the detached goroutine exists.
func TestGrokObservedModelsScheduleFreezesDeepOwnerBeforeBackgroundWork(t *testing.T) {
	for _, mutation := range []string{"login", "nested-credentials", "proxy-binding", "proxy-host", "proxy-password"} {
		t.Run(mutation, func(t *testing.T) {
			account := grokObservedBusinessAccount(7952)
			delete(account.Credentials, "outbound_identity")
			repo := &grokObservedStrictAccountRepo{account: account, done: make(chan struct{})}
			upstream := &grokObservedStrictUpstream{}
			svc := NewGrokQuotaService(repo, nil, nil, upstream, &config.Config{})
			entered, release, scheduled := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var resolverOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			ctx := outboundidentity.WithResolver(context.Background(), func(context.Context, string) outboundidentity.Identity {
				resolverOnce.Do(func() { close(entered) })
				<-release
				return outboundidentity.Identity{Preset: "grok", Source: "global", UserAgent: "grok-shell/1.0.45 (linux; x86_64)", Originator: "grok-shell", Version: "1.0.45",
					Headers: map[string]string{"User-Agent": "grok-shell/1.0.45 (linux; x86_64)", "X-Grok-Client-Identifier": "grok-shell", "X-Grok-Client-Version": "1.0.45"}}
			})
			go func() {
				svc.scheduleGrokObservedModelsSync(ctx, account)
				close(scheduled)
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("model discovery did not reach its scheduling barrier")
			}
			switch mutation {
			case "login":
				account.Credentials["access_token"] = "replacement-login-token"
			case "nested-credentials":
				account.Credentials["grant_metadata"].(map[string]any)["subject"] = "replacement-subject"
			case "proxy-binding":
				*account.ProxyID = 9992
				account.Proxy.ID = 9992
			case "proxy-host":
				account.Proxy.Host = "replacement.proxy.example"
			case "proxy-password":
				account.Proxy.Password = "replacement-proxy-password"
			}
			unblock()
			select {
			case <-scheduled:
			case <-time.After(2 * time.Second):
				t.Fatal("model discovery schedule did not return after its barrier was released")
			}
			require.Eventually(t, func() bool {
				_, inFlight := grokObservedModelsFlight.Load(account.ID)
				return !inFlight
			}, 2*time.Second, time.Millisecond, "detached discovery must finish and release its flight")
			select {
			case <-repo.done:
			default:
				t.Fatal("successful model discovery did not reach owner-conditional persistence")
			}
			require.Equal(t, 1, upstream.calls)
			require.Equal(t, "Bearer private-observed-models-owner-token", upstream.request.Header.Get("Authorization"))
			require.Equal(t, "grok-shell/1.0.45 (linux; x86_64)", upstream.request.UserAgent())
			require.Equal(t, "http://private-observed-proxy-user:private-observed-proxy-password@captured.proxy.example:8080", upstream.proxy)
			require.Equal(t, 1, repo.ownerReads)
			require.Zero(t, repo.casCalls)
			require.Zero(t, repo.legacyWrites)
			require.Zero(t, repo.writes, "scheduled discovery must not publish into its newly edited owner")
			require.Nil(t, repo.updates)
		})
	}
}
