//go:build unit || !integration

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type resetIdentityTokenStub struct {
	acquire func(context.Context, *Account)
}

type resetIdentityAccountStub struct{ account *Account }

func (s resetIdentityAccountStub) GetByID(context.Context, int64) (*Account, error) {
	return s.account, nil
}

func (s resetIdentityTokenStub) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	s.acquire(ctx, account)
	return "synthetic-token", nil
}

func TestClaudeResetCreditsOutboundIdentityPriorityAndTransport(t *testing.T) {
	for _, name := range []string{"account", "global", "invalid-account", "default", "invalid-global"} {
		t.Run(name, func(t *testing.T) {
			config := emptyOutboundIdentitySettings()
			if name != "default" {
				config.Profiles["claude"] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.1"}
			}
			svc, ctx := outboundIdentityTestSettings(t, config)
			account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile"}}
			want := builtInOutboundIdentity("claude")
			if name != "default" && name != "invalid-global" {
				var err error
				want, err = buildOutboundIdentity(OutboundIdentitySelection{Preset: "claude", Version: "3.9.1"})
				require.NoError(t, err)
				want.Source = "global"
			}
			switch name {
			case "account":
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.2"}
				var err error
				want, err = buildOutboundIdentity(OutboundIdentitySelection{Preset: "claude", Version: "3.9.2"})
				require.NoError(t, err)
				want.Source = "account"
			case "invalid-account":
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", UserAgent: "inbound/999", Version: "3.9.2"}
			case "invalid-global":
				config.Profiles["claude"] = OutboundIdentitySelection{Preset: "claude", UserAgent: "inbound/999", Version: "3.9.1"}
				repo, ok := svc.settingRepo.(*outboundIdentityTestRepo)
				require.True(t, ok)
				repo.values[SettingKeyOutboundIdentity] = `{"profiles":{"claude":{"preset":"claude","user_agent":"inbound/999","version":"3.9.1"}}}`
				svc.outboundIdentityCache.Store(&cachedOutboundIdentitySettings{settings: config})
			}
			want.AccountID = account.ID
			tokens := resetIdentityTokenStub{acquire: func(tokenCtx context.Context, owner *Account) {
				require.Same(t, account, owner)
				selected, ok := outboundidentity.FromContext(tokenCtx)
				require.True(t, ok)
				require.Equal(t, want, selected)
				// Updates during token acquisition must not change this request's identity.
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.3"}
			}}
			client := &http.Client{Transport: claudeResetCreditTransport{base: brandidentity.WrapRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, claudeResetUsageURL, req.URL.String())
				require.Equal(t, want.UserAgent, req.UserAgent())
				for key, value := range want.Headers {
					require.Equal(t, value, req.Header.Get(key), key)
				}
				require.Empty(t, req.Header.Get("Originator"))
				require.Empty(t, req.Header.Get("Version"))
				require.Equal(t, "Bearer synthetic-token", req.Header.Get("Authorization"))
				require.Equal(t, "oauth-2025-04-20", req.Header.Get("Anthropic-Beta"))
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}))}}
			s := &ClaudeResetCreditService{accounts: resetIdentityAccountStub{account}, tokens: tokens, now: time.Now}
			s.do = func(req *http.Request, proxy string) (*http.Response, error) {
				require.Empty(t, proxy)
				selected, ok := outboundidentity.FromContext(req.Context())
				require.True(t, ok)
				require.Equal(t, want, selected)
				// SDK/default or generic headers are replaced at the final transport boundary.
				req.Header.Set("User-Agent", "sdk/999")
				req.Header.Set("X-Stainless-Package-Version", "999")
				return client.Do(req)
			}
			_, err := s.Query(ctx, account.ID)
			require.NoError(t, err)
		})
	}
}
