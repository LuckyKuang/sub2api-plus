//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type privacyOwnerGuardRepo struct{ auxiliaryOwnerRepo }

func (r *privacyOwnerGuardRepo) ApplyCredentialStateMutation(_ context.Context, owner *Account, mutation CredentialStateMutation) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !sameRequestCredentialGrant(owner, r.account) {
		return false, nil
	}
	r.writes = append(r.writes, cloneRequestCredentialMap(mutation.Extra))
	return true, nil
}

// Account-bound admin operations and detached catalog refreshes are independent
// physical senders. They must reject an invalid binding before even a warm
// cache lookup and must not treat a failed proxy as permission for direct TCP.
func TestAccountControlSendersRequireStrictProxy(t *testing.T) {
	for _, operation := range []string{"privacy", "protocol_catalog"} {
		for _, route := range []string{"missing", "mismatched", "invalid", "timeout", "refused", "authentication_required", "proxy", "unbound"} {
			t.Run(operation+"/"+route, func(t *testing.T) {
				n := newAuxiliaryWireNetwork(t)
				account := auxiliaryCodexAccount("control-token", auxiliaryOwnerUA, 0)
				if operation == "protocol_catalog" {
					account = commandCodeTestAccount(time.Now().UnixNano())
				}
				proxy := n.proxy(t, "old", 81)
				bindingID := proxy.ID
				account.ProxyID, account.Proxy = &bindingID, proxy
				switch route {
				case "missing":
					account.Proxy = nil
				case "mismatched":
					account.Proxy.ID++
				case "invalid":
					account.Proxy.Protocol = "file"
				case "timeout", "authentication_required":
					n.failure = route
				case "refused":
					n.proxies["old"].Close()
				case "unbound":
					account.ProxyID = nil
				}
				repo := &privacyOwnerGuardRepo{auxiliaryOwnerRepo: auxiliaryOwnerRepo{account: snapshotRequestCredentialAccount(account)}}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if operation == "privacy" {
					svc := &adminServiceImpl{accountRepo: repo, proxyRepo: &openAISelectedProxyRepo{proxy: account.Proxy}, privacyClientFactory: n.privacyFactory}
					mode := svc.ForceOpenAIPrivacy(ctx, account)
					if route == "proxy" || route == "unbound" {
						require.Equal(t, PrivacyModeTrainingOff, mode)
						require.NotEmpty(t, repo.writes)
						require.Equal(t, map[string]any{"privacy_mode": PrivacyModeTrainingOff}, repo.writes[0])
					} else {
						require.NotEqual(t, PrivacyModeTrainingOff, mode)
					}
				} else {
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: n}
					protocols := svc.modelCatalogProtocols(ctx, account, "deepseek-flash")
					if route == "proxy" || route == "unbound" {
						require.Equal(t, []string{APIProtocolResponses}, protocols)
					} else {
						require.Empty(t, protocols)
					}
				}
				if route == "unbound" {
					require.Positive(t, n.directConnections.Load())
				} else {
					require.Zero(t, n.directConnections.Load(), "a configured proxy never authorizes a local TCP connection")
				}
				if route == "missing" || route == "mismatched" || route == "invalid" {
					require.Empty(t, n.captures(), "invalid binding must stop before any network request")
					require.Empty(t, repo.writes)
				}
				if route == "proxy" || route == "timeout" || route == "authentication_required" {
					require.NotEmpty(t, n.captures(), "the actual operation must reach the configured proxy")
				}
			})
		}
	}
}

func TestAdminPrivacyRejectsOldOwnerResultAfterProxyEdit(t *testing.T) {
	n := newAuxiliaryWireNetwork(t)
	account := auxiliaryCodexAccount("privacy-old", auxiliaryOwnerUA, 0)
	proxy := n.proxy(t, "old", 82)
	account.ProxyID, account.Proxy = &proxy.ID, proxy
	repo := &privacyOwnerGuardRepo{auxiliaryOwnerRepo: auxiliaryOwnerRepo{account: snapshotRequestCredentialAccount(account)}}
	svc := &adminServiceImpl{accountRepo: repo, privacyClientFactory: n.privacyFactory}
	gate := &auxiliaryWireGate{reached: make(chan struct{}), release: make(chan struct{})}
	n.gate = gate
	var release sync.Once
	unblock := func() { release.Do(func() { close(gate.release) }) }
	t.Cleanup(unblock)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan string, 1)
	go func() { result <- svc.ForceOpenAIPrivacy(ctx, account) }()
	select {
	case <-gate.reached:
	case <-ctx.Done():
		t.Fatal("privacy PATCH did not reach its configured proxy")
	}
	replacement := snapshotRequestCredentialAccount(account)
	replacement.Proxy = n.proxy(t, "new", proxy.ID)
	replacement.Credentials["access_token"] = "privacy-new"
	repo.replace(replacement)
	unblock()
	select {
	case mode := <-result:
		require.Empty(t, mode, "an obsolete PATCH result cannot advertise successor account privacy")
	case <-ctx.Done():
		t.Fatal("privacy PATCH did not finish")
	}
	require.Empty(t, repo.writes)
	require.Equal(t, "Bearer privacy-old", n.captures()[0].auth)
	require.Equal(t, "old", n.captures()[0].route)
	require.Equal(t, PrivacyModeTrainingOff, svc.ForceOpenAIPrivacy(ctx, replacement))
	require.Equal(t, "new", n.captures()[1].route)
	require.Equal(t, "Bearer privacy-new", n.captures()[1].auth)
}

func TestModelProtocolCatalogWarmCacheCannotBypassInvalidBinding(t *testing.T) {
	for _, route := range []string{"missing", "mismatched", "invalid"} {
		t.Run(route, func(t *testing.T) {
			account := commandCodeTestAccount(time.Now().UnixNano())
			id := int64(83)
			account.ProxyID = &id
			proxyURL := ""
			if route != "missing" {
				account.Proxy = &Proxy{ID: id, Protocol: "http", Host: "proxy.invalid", Port: 8080}
				if route == "mismatched" {
					account.Proxy.ID++
				} else {
					account.Proxy.Protocol = "file"
				}
				proxyURL = account.Proxy.URL()
			}
			ctx := context.Background()
			base := buildOpenAIModelsURL(account.GetCNProtocolBaseURL(APIProtocolChatCompletions))
			key := modelProtocolCatalogKey(account, base, modelProtocolCatalogHeaders(ctx, account), proxyURL)
			upstreamModelProtocols.store(key, map[string][]string{"deepseek-flash": {APIProtocolResponses}}, nil, time.Now())
			upstream := &codexModelsHTTPUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				t.Error("invalid proxy must not send")
				return nil, fmt.Errorf("forbidden send")
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			require.Empty(t, svc.modelCatalogProtocols(ctx, account, "deepseek-flash"))
		})
	}
}
