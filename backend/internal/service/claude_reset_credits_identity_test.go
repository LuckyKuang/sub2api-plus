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
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/claude"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

type resetIdentityTokenStub struct {
	acquire func(context.Context, *Account)
}

func TestClaudeResetCompiledIdentityAndFingerprint(t *testing.T) {
	t.Setenv(claude.CLIVersionEnv, "")
	require.Equal(t, outboundidentity.Identity{
		Preset: "claude", Source: "compiled_default", Originator: "claude-cli",
		UserAgent: "claude-cli/2.1.258 (external, cli)", Version: "2.1.258",
		Headers: map[string]string{
			"User-Agent":                  "claude-cli/2.1.258 (external, cli)",
			"X-App":                       "cli",
			"X-Stainless-Lang":            "js",
			"X-Stainless-Package-Version": "0.94.0",
			"X-Stainless-OS":              "Linux",
			"X-Stainless-Arch":            "x64",
			"X-Stainless-Runtime":         "node",
			"X-Stainless-Runtime-Version": "v24.3.0",
		},
	}, builtInOutboundIdentity("claude"))
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

func TestClaudeResetRedeemRetainsOwnerSnapshotAcrossAllOutboundPaths(t *testing.T) {
	for _, name := range []string{"account", "global", "invalid-account", "empty-account", "default"} {
		t.Run(name, func(t *testing.T) {
			config := emptyOutboundIdentitySettings()
			if name != "default" {
				config.Profiles["claude"] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.1"}
			}
			_, ctx := outboundIdentityTestSettings(t, config)
			account := &Account{ID: 41, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile"}}
			want := builtInOutboundIdentity("claude")
			if name != "default" {
				var err error
				want, err = buildOutboundIdentity(config.Profiles["claude"])
				require.NoError(t, err)
				want.Source = "global"
			}
			switch name {
			case "account":
				selection := OutboundIdentitySelection{Preset: "claude", Version: "3.9.2"}
				account.Credentials[outboundIdentityCredential] = selection
				var err error
				want, err = buildOutboundIdentity(selection)
				require.NoError(t, err)
				want.Source = "account"
			case "invalid-account":
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", UserAgent: "inbound/999"}
			case "empty-account":
				account.Credentials[outboundIdentityCredential] = nil
			}
			want.AccountID = account.ID
			// An unrelated incoming operation cannot lend this credential owner its identity.
			foreign := builtInOutboundIdentity("grok")
			foreign.AccountID = 99
			ctx = outboundidentity.WithIdentity(ctx, foreign)
			f := &redeemFake{claim: `{"result":"reset"}`}
			s, repo, _ := newRedeemService(t, f)
			s.accounts = resetIdentityAccountStub{account}
			acquisitions := 0
			s.tokens = resetIdentityTokenStub{acquire: func(tokenCtx context.Context, owner *Account) {
				acquisitions++
				require.Same(t, account, owner)
				selected, ok := outboundidentity.FromContext(tokenCtx)
				require.True(t, ok)
				require.Equal(t, want, selected)
				// Account edits while obtaining a token cannot switch the ongoing operation.
				account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "claude", Version: "3.9.3"}
			}}
			paths := []string{}
			upstream := s.do
			client := &http.Client{Transport: claudeResetCreditTransport{base: brandidentity.WrapRoundTripper(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				paths = append(paths, req.Method+" "+req.URL.String())
				selected, ok := outboundidentity.FromContext(req.Context())
				require.True(t, ok)
				require.Equal(t, want, selected)
				require.Equal(t, want.UserAgent, req.UserAgent())
				for key, value := range want.Headers {
					require.Equal(t, value, req.Header.Get(key), key)
				}
				require.Equal(t, "cli", req.Header.Get("X-App"))
				require.Equal(t, "0.94.0", req.Header.Get("X-Stainless-Package-Version"))
				require.Equal(t, "Linux", req.Header.Get("X-Stainless-OS"))
				require.Equal(t, "x64", req.Header.Get("X-Stainless-Arch"))
				require.Equal(t, "node", req.Header.Get("X-Stainless-Runtime"))
				require.Equal(t, "v24.3.0", req.Header.Get("X-Stainless-Runtime-Version"))
				require.Empty(t, req.Header.Get("Originator"))
				require.Empty(t, req.Header.Get("Version"))
				require.Empty(t, req.Header.Get("X-Grok-Client-Identifier"))
				require.Empty(t, req.Header.Get("Cookie"))
				return upstream(req, "")
			}))}}
			s.do = func(req *http.Request, proxy string) (*http.Response, error) {
				require.Empty(t, proxy)
				req.Header.Set("User-Agent", "sdk/999")
				req.Header.Set("X-Stainless-Package-Version", "999")
				req.Header.Set("Originator", "inbound")
				req.Header.Set("X-Grok-Client-Identifier", "foreign")
				return client.Do(req)
			}
			out, err := s.Redeem(ctx, account.ID, "snapshot-operation")
			require.NoError(t, err)
			require.Equal(t, ClaudeResetOutcomeReset, out.Outcome)
			require.Equal(t, 2, acquisitions)
			require.Equal(t, []string{
				"GET " + claudeResetProfileURL,
				"GET " + claudeResetUsageURL,
				"POST https://api.anthropic.com/api/organizations/" + redeemTestOrg + "/reset_rate_limits",
				"GET " + claudeResetUsageURL,
			}, paths)
			require.Equal(t, 1, f.postCount())
			// Persisted fences and operator replay results never store OAuth credentials.
			for _, record := range repo.data {
				if record.ResponseBody != nil {
					require.NotContains(t, *record.ResponseBody, "synthetic-token")
					require.NotContains(t, *record.ResponseBody, redeemTestOrg)
				}
			}
		})
	}
}
