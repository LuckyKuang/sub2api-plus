//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

// OUTBOUND_IDENTITY.md requires one immutable credential owner for the bearer,
// destination and ordinary headers. Exercise real HTTP destinations: a refresh
// must never send the new bearer to the selected row's obsolete relay.
func TestGrokAcquiredOwnerControlsPhysicalTargetAndOverrides(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, operation := range []string{"responses", "chat"} {
			t.Run(kind+"/"+operation, func(t *testing.T) {
				var obsoleteCalls atomic.Int64
				obsolete := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					obsoleteCalls.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
				}))
				defer obsolete.Close()
				captured := make(chan *http.Request, 1)
				current := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					captured <- r.Clone(context.Background())
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{}`)
				}))
				defer current.Close()
				selected := grokCompressionTestAccount(90701)
				selected.Type = kind
				selected.Concurrency = 7
				selected.Credentials["api_key"] = "old-token"
				selected.Credentials["access_token"] = "old-token"
				selected.Credentials["base_url"] = obsolete.URL + "/v1"
				selected.Credentials["header_override_enabled"] = true
				selected.Credentials["header_overrides"] = map[string]any{"X-Relay-Snapshot": "obsolete"}
				owner := snapshotRequestCredentialAccount(selected)
				owner.Concurrency = 2
				owner.Credentials["access_token"] = "new-token"
				owner.Credentials["api_key"] = "new-token"
				owner.Credentials["base_url"] = current.URL + "/v1"
				owner.Credentials["header_overrides"] = map[string]any{"X-Relay-Snapshot": "acquired"}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+operation, nil)
				ctx := WithOutboundIdentityScope(context.Background(), c)
				recordRequestCredentialOwner(ctx, owner, "new-token")
				ctx = withRequestCredentialOwner(ctx, selected, "new-token")
				upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
					require.Equal(t, selected.ID, id)
					require.Equal(t, 7, concurrency, "resource accounting belongs to the selection")
					require.Empty(t, proxy)
					return http.DefaultClient.Do(req)
				}}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				var response *http.Response
				var err error
				if operation == "responses" {
					var request *http.Request
					request, err = svc.buildGrokResponsesRequest(ctx, c, selected, []byte(`{"model":"grok-4.5","input":"hello"}`), "new-token", "", nil)
					require.NoError(t, err)
					response, err = svc.doOpenAIUpstream(request, "", selected)
				} else {
					response, err = svc.sendCCUpstreamRequest(ctx, c, selected, obsolete.URL+"/v1/chat/completions", []byte(`{"model":"grok-4.5","messages":[]}`), false, "new-token", "", "")
				}
				require.NoError(t, err)
				require.NotNil(t, response)
				require.NoError(t, response.Body.Close())
				require.Zero(t, obsoleteCalls.Load(), "no credentials may reach the obsolete destination")
				request := <-captured
				wantPath := "/v1/responses"
				if operation == "chat" {
					wantPath = "/v1/chat/completions"
				}
				require.Equal(t, wantPath, request.URL.Path)
				require.Equal(t, "Bearer new-token", request.Header.Get("Authorization"))
				require.Equal(t, "acquired", request.Header.Get("X-Relay-Snapshot"))
			})
		}
	}
}

func TestGrokCompressionProbeUsesAcquiredPrincipalAndProxy(t *testing.T) {
	resetGrokRequestCompressionCapabilityCacheForTest()
	t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)
	selected := grokCompressionTestAccount(90702)
	selected.Credentials["access_token"] = "old-token"
	selected.Credentials["sub"] = "old-sub"
	selected.Credentials["email"] = "old@example.test"
	selected.ProxyID = grokCompressionPtrInt64(7)
	selected.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "obsolete.example.test", Port: 8080, Status: StatusActive}
	owner := snapshotRequestCredentialAccount(selected)
	owner.Credentials["access_token"] = "new-token"
	owner.Credentials["sub"] = "new-sub"
	owner.Credentials["email"] = "new@example.test"
	owner.Proxy.Host = "acquired.example.test"
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	recordRequestCredentialOwner(ctx, owner, "new-token")
	ctx = withRequestCredentialOwner(ctx, selected, "new-token")
	upstream := &grokCompressionUpstreamStub{settingsBody: `{"accept_request_encodings":["zstd"]}`}
	svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
	request, err := svc.buildGrokResponsesRequest(ctx, nil, selected, grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes), "new-token", "", svc.cfg)
	require.NoError(t, err)
	require.Equal(t, "zstd", request.Header.Get("Content-Encoding"))
	require.Len(t, upstream.settingsRe, 1)
	probe := upstream.settingsRe[0]
	require.Equal(t, "Bearer new-token", probe.Header.Get("Authorization"))
	require.Equal(t, "new-sub", probe.Header.Get("x-userid"))
	require.Equal(t, "new@example.test", probe.Header.Get("x-email"))
	require.Equal(t, "http://acquired.example.test:8080", upstream.proxies[0])
}

// Capability observations, including negative ones, cannot cross a relinked
// grant or a changed proxy state even if account ID and destination stay equal.
func TestGrokCompressionCacheIsolatedByCredentialAndProxyGeneration(t *testing.T) {
	for _, firstAdvertised := range []bool{false, true} {
		for _, change := range []string{"access-token", "principal", "proxy-status"} {
			t.Run(change+"/first="+strconv.FormatBool(firstAdvertised), func(t *testing.T) {
				resetGrokRequestCompressionCapabilityCacheForTest()
				t.Cleanup(resetGrokRequestCompressionCapabilityCacheForTest)
				advertised := `{"accept_request_encodings":["zstd"]}`
				plain := `{"accept_request_encodings":[]}`
				first, second := plain, advertised
				if firstAdvertised {
					first, second = advertised, plain
				}
				upstream := &grokCompressionUpstreamStub{settingsBody: first}
				svc := &OpenAIGatewayService{cfg: grokCompressionEnabledConfig(), httpUpstream: upstream}
				account := grokCompressionTestAccount(90703)
				account.ProxyID = grokCompressionPtrInt64(8)
				account.Proxy = &Proxy{ID: 8, Protocol: "http", Host: "proxy.example.test", Port: 8080, Status: StatusActive}
				body := grokCompressionPaddedJSON(t, grokRequestCompressionMinBytes)
				request, err := svc.buildGrokResponsesRequest(context.Background(), nil, account, body, "access-token", "", svc.cfg)
				require.NoError(t, err)
				require.Equal(t, firstAdvertised, request.Header.Get("Content-Encoding") == "zstd")
				upstream.settingsBody = second
				token := "access-token"
				switch change {
				case "access-token":
					account.Credentials["access_token"] = "rotated-token"
					token = "rotated-token"
				case "principal":
					account.Credentials["sub"] = "different-principal"
				case "proxy-status":
					account.Proxy.Status = StatusDisabled
				}
				request, err = svc.buildGrokResponsesRequest(context.Background(), nil, account, body, token, "", svc.cfg)
				require.NoError(t, err)
				require.Equal(t, !firstAdvertised, request.Header.Get("Content-Encoding") == "zstd")
				require.Equal(t, 2, upstream.settingsCalls, "changed grant/proxy must query capability again")
			})
		}
	}
}
