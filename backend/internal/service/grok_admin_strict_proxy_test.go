//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type grokAdminStrictDialer struct {
	calls   int
	proxy   string
	headers http.Header
}

func (d *grokAdminStrictDialer) Dial(_ context.Context, _ string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.calls++
	d.proxy, d.headers = proxy, headers.Clone()
	return &grokRealtimeTestConn{msg: []byte(`{"type":"session.created","session":{"id":"session-1"}}`)}, http.StatusSwitchingProtocols, nil, nil
}

// Admin probes are independent production senders. Testing normal inference
// does not establish strict routing for the dashboard's media/voice/WS modes.
func TestGrokAdminProbesRejectBrokenProxyBeforeSend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, mode := range []string{"default", "text", "image", "video", "search", "tts", "stt", "realtime"} {
			for _, binding := range openAIStrictPreflightProxyModes {
				t.Run(accountType+"/"+mode+"/"+binding, func(t *testing.T) {
					account := grokStrictProxyAccount(accountType, binding)
					repo := &mockAccountRepoForGemini{accountsByID: map[int64]*Account{account.ID: account}}
					upstream := &grokAuxiliaryUpstream{err: errors.New("isolated provider unavailable")}
					dialer := &grokAdminStrictDialer{}
					svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{},
						grokTokenProvider: NewGrokTokenProvider(repo, nil), grokWSDialer: dialer}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/7932/test", nil)
					err := svc.TestAccountConnection(c, account.ID, "", "", mode)
					if binding != "valid-bound" && binding != "explicitly-unbound" {
						require.Empty(t, upstream.requests)
						require.Zero(t, dialer.calls)
						grokAuxiliaryAssertSafeError(t, err)
						require.NotContains(t, recorder.Body.String(), "\"success\":true")
						return
					}
					wantProxy := ""
					if binding == "valid-bound" {
						wantProxy = "http://private-proxy-user:private-proxy-password@owner.proxy.example:8080"
					}
					if mode == "realtime" {
						require.NoError(t, err)
						require.Equal(t, 1, dialer.calls)
						require.Equal(t, wantProxy, dialer.proxy)
						require.Equal(t, "Bearer private-grok-token", dialer.headers.Get("Authorization"))
					} else {
						require.Error(t, err, "the isolated responder intentionally fails after observing the exact transport boundary")
						require.Len(t, upstream.requests, 1)
						require.Equal(t, wantProxy, upstream.proxies[0])
						require.Equal(t, "Bearer private-grok-token", upstream.requests[0].Header.Get("Authorization"))
					}
				})
			}
		}
	}
}

func TestGrokAdminProbesRejectBrokenProxyBeforeRefresh(t *testing.T) {
	for _, mode := range []string{"default", "text", "image", "video", "search", "tts", "stt", "realtime"} {
		for _, binding := range openAIStrictPreflightProxyModes[2:] {
			t.Run(mode+"/"+binding, func(t *testing.T) {
				account := grokStrictProxyAccount(AccountTypeOAuth, binding)
				account.Credentials["expires_at"] = time.Now().Add(-time.Minute).Format(time.RFC3339)
				repo := &tokenRefreshAccountRepo{}
				repo.accountsByID = map[int64]*Account{account.ID: account}
				issuer := &tokenRefresherStub{err: errors.New("isolated issuer unavailable")}
				provider := NewGrokTokenProvider(repo, nil)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, nil), issuer)
				upstream := &grokAuxiliaryUpstream{}
				dialer := &grokAdminStrictDialer{}
				svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}, grokTokenProvider: provider, grokWSDialer: dialer}
				err := svc.TestAccountConnection(grokAuxiliaryInbound(http.MethodPost, "/api/v1/admin/accounts/7932/test"), account.ID, "", "", mode)
				require.Zero(t, issuer.calls, "an invalid saved route cannot authorize an OAuth refresh")
				require.Zero(t, repo.updateCalls)
				require.Empty(t, upstream.requests)
				require.Zero(t, dialer.calls)
				grokAuxiliaryAssertSafeError(t, err)
			})
		}
	}
}
