//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// Requirement: docs/OUTBOUND_IDENTITY.md applies the same source order and
// immutable owner snapshot to discovery, explicit-cache writes and cleanup.
// Capture actual HTTP requests through the production identity finalizer.
func geminiControlWireUpstream(t *testing.T, handler http.HandlerFunc) HTTPUpstream {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	client := &http.Client{Transport: brandidentity.WrapRoundTripper(server.Client().Transport)}
	return &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		wire := req.Clone(req.Context())
		wire.URL.Scheme, wire.URL.Host, wire.Host = target.Scheme, target.Host, target.Host
		return client.Do(wire)
	}}
}

func TestGeminiControlIdentitySourceMatrix(t *testing.T) {
	for _, kind := range []string{"apikey", "gateway", "oauth", "vertex"} {
		for _, source := range []string{"compiled", "global", "account", "invalid_account", "compatible_preset"} {
			if source == "compatible_preset" && (kind == "oauth" || kind == "vertex") {
				continue // These native account types retain the Gemini client family.
			}
			for _, operation := range []string{"list", "get", "create", "patch", "delete"} {
				if kind == "oauth" && operation != "list" && operation != "get" {
					continue // OAuth accounts do not support explicit caching.
				}
				t.Run(kind+"/"+source+"/"+operation, func(t *testing.T) {
					settings := emptyOutboundIdentitySettings()
					wantUA := "GeminiCLI/0.1.5 (Linux; x64)"
					if source != "compiled" {
						settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
						wantUA = "GeminiCLI/3.9.1 (Linux; x64)"
					}
					_, ctx := outboundIdentityTestSettings(t, settings)
					account := &Account{ID: 42, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
					if kind == "gateway" {
						account.Credentials["base_url"] = "https://relay.example.com"
						account.Credentials["explicit_cache_upstream"] = true
					}
					if kind == "oauth" {
						account.Type = AccountTypeOAuth
					}
					if kind == "vertex" {
						account = vertexModelSyncTestAccount(map[string]any{"location": "global"})
					}
					switch source {
					case "account":
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.2"}
						wantUA = "GeminiCLI/3.9.2 (Linux; x64)"
					case "invalid_account":
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "gemini", UserAgent: "inbound/999.0.0"}
					case "compatible_preset":
						account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok", Version: "3.9.2"}
						wantUA = "grok-shell/3.9.2 (linux; x86_64)"
					}
					captured := make(chan http.Header, 1)
					upstreamName := "cachedContents/test"
					if kind == "vertex" {
						upstreamName = "projects/proj/locations/global/cachedContents/test"
					}
					upstream := geminiControlWireUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						captured <- r.Header.Clone()
						w.Header().Set("Content-Type", "application/json")
						if operation == "list" || operation == "get" {
							_, _ = io.WriteString(w, `{"models":[],"publisherModels":[{"name":"publishers/google/models/gemini-3.8-flash"}]}`)
						} else {
							_, _ = io.WriteString(w, `{"name":"`+upstreamName+`","usageMetadata":{"totalTokenCount":100},"expireTime":"2026-10-19T15:00:00Z"}`)
						}
					})
					compat := newGeminiModelsEndpointsTestService(nil, upstream)
					cache := NewGeminiCachedContentService(nil, compat, &config.Config{})
					record := &GeminiCachedContent{UpstreamName: upstreamName}
					var err error
					switch operation {
					case "list":
						_, err = compat.ForwardListModels(ctx, account)
					case "get":
						_, err = compat.ForwardGetModel(ctx, account, "gemini-3.8-flash")
					case "create":
						var req *GeminiCachedContentCreateRequest
						req, err = ParseGeminiCachedContentCreateRequest([]byte(`{"model":"gemini-3.8-flash","contents":[{"role":"user","parts":[{"text":"hello"}]}]}`), time.Now(), time.Hour)
						require.NoError(t, err)
						_, err = cache.CreateUpstream(ctx, account, req, req.Model, "test", time.Hour)
					case "patch":
						_, err = cache.PatchUpstream(ctx, account, record, time.Hour)
					case "delete":
						err = cache.DeleteUpstream(ctx, account, record)
					}
					require.NoError(t, err)
					headers := <-captured
					require.Equal(t, wantUA, headers.Get("User-Agent"))
					for _, key := range []string{"Originator", "Version", "X-Goog-Api-Client", "X-Stainless-Package-Version"} {
						require.Empty(t, headers.Get(key), "the selected preset does not define %s", key)
					}
					if kind == "oauth" || kind == "vertex" {
						require.Equal(t, "Bearer ya29.vertex-token", headers.Get("Authorization"))
						require.Empty(t, headers.Get("x-goog-api-key"))
					} else {
						require.Equal(t, "test-key", headers.Get("x-goog-api-key"))
						require.Empty(t, headers.Get("Authorization"))
					}
				})
			}
		}
	}
}

type geminiControlIdentityTokenCache struct {
	vertexModelsTokenCacheStub
	onGet func(context.Context)
}

func (c *geminiControlIdentityTokenCache) GetAccessToken(ctx context.Context, _ string) (string, error) {
	c.onGet(ctx)
	return "ya29.vertex-token", nil
}

func TestGeminiControlIdentityTokenAndPaginationSnapshot(t *testing.T) {
	for _, operation := range []string{"list", "get", "create", "patch", "delete"} {
		t.Run(operation, func(t *testing.T) {
			settings := emptyOutboundIdentitySettings()
			settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
			svc, ctx := outboundIdentityTestSettings(t, settings)
			account := vertexModelSyncTestAccount(map[string]any{"location": "global"})
			var tokenIdentity outboundidentity.Identity
			cache := &geminiControlIdentityTokenCache{onGet: func(tokenCtx context.Context) {
				tokenIdentity, _ = outboundidentity.FromContext(tokenCtx)
				settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.7"}
				require.NoError(t, svc.SetOutboundIdentitySettings(ctx, settings))
			}}
			captured := make(chan http.Header, 2)
			upstream := geminiControlWireUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				if operation == "list" || operation == "get" {
					if r.URL.Query().Get("pageToken") == "" {
						_, _ = io.WriteString(w, `{"publisherModels":[],"nextPageToken":"page-2"}`)
					} else {
						_, _ = io.WriteString(w, `{"publisherModels":[{"name":"publishers/google/models/gemini-3.8-flash"}]}`)
					}
				} else {
					_, _ = io.WriteString(w, `{"name":"projects/proj/locations/global/cachedContents/test","usageMetadata":{"totalTokenCount":100}}`)
				}
			})
			compat := newGeminiModelsEndpointsTestService(nil, upstream)
			compat.tokenProvider = NewGeminiTokenProvider(nil, cache, nil)
			service := NewGeminiCachedContentService(nil, compat, &config.Config{})
			record := &GeminiCachedContent{UpstreamName: "projects/proj/locations/global/cachedContents/test"}
			var err error
			switch operation {
			case "list":
				_, err = compat.ForwardListModels(ctx, account)
			case "get":
				_, err = compat.ForwardGetModel(ctx, account, "gemini-3.8-flash")
			case "create":
				req, parseErr := ParseGeminiCachedContentCreateRequest([]byte(`{"model":"gemini-3.8-flash"}`), time.Now(), time.Hour)
				require.NoError(t, parseErr)
				_, err = service.CreateUpstream(ctx, account, req, req.Model, "test", time.Hour)
			case "patch":
				_, err = service.PatchUpstream(ctx, account, record, time.Hour)
			case "delete":
				err = service.DeleteUpstream(ctx, account, record)
			}
			require.NoError(t, err)
			require.Equal(t, "GeminiCLI/3.9.1 (Linux; x64)", tokenIdentity.UserAgent)
			require.Equal(t, account.ID, tokenIdentity.AccountID)
			count := 1
			if operation == "list" || operation == "get" {
				count = 2
			}
			for range count {
				require.Equal(t, "GeminiCLI/3.9.1 (Linux; x64)", (<-captured).Get("User-Agent"), "token acquisition and all pages share one snapshot")
			}
			// A fresh operation sees changed global settings.
			_, err = compat.ForwardListModels(ctx, &Account{ID: 43, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}})
			require.NoError(t, err)
			require.Equal(t, "GeminiCLI/3.9.7 (Linux; x64)", (<-captured).Get("User-Agent"))
		})
	}
}

func TestGeminiControlIdentitySnapshotFailoverAndDetachedCleanup(t *testing.T) {
	settings := emptyOutboundIdentitySettings()
	settings.Profiles["gemini"] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.1"}
	_, ctx := outboundIdentityTestSettings(t, settings)
	ctx = WithOutboundIdentityScope(ctx, nil)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	account := &Account{ID: 42, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "first-key", outboundIdentityCredential: OutboundIdentitySelection{Preset: "gemini", Version: "3.9.2"},
	}}
	other := &Account{ID: 43, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "second-key"}}
	captured := make(chan http.Header, 3)
	upstream := geminiControlWireUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		captured <- r.Header.Clone()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"cachedContents/test","usageMetadata":{"totalTokenCount":100}}`)
	})
	compat := newGeminiModelsEndpointsTestService(nil, upstream)
	cache := NewGeminiCachedContentService(nil, compat, &config.Config{})
	req, err := ParseGeminiCachedContentCreateRequest([]byte(`{"model":"gemini-3.8-flash"}`), time.Now(), time.Hour)
	require.NoError(t, err)
	_, err = cache.CreateUpstream(ctx, account, req, req.Model, "test", time.Hour)
	require.NoError(t, err)
	account.Credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "gemini", Version: "3.9.8"}
	cancel()
	require.NoError(t, cache.DeleteUpstream(ctx, account, &GeminiCachedContent{UpstreamName: "cachedContents/test"}))
	// Failover must not borrow the previous owner's account candidate.
	foreign := WithAccountOutboundIdentity(ctx, account)
	_, err = cache.CreateUpstream(foreign, other, req, req.Model, "other", time.Hour)
	require.NoError(t, err)
	for range 2 {
		headers := <-captured
		require.Equal(t, "GeminiCLI/3.9.2 (Linux; x64)", headers.Get("User-Agent"))
		require.Equal(t, "first-key", headers.Get("x-goog-api-key"))
	}
	headers := <-captured
	require.Equal(t, "GeminiCLI/3.9.1 (Linux; x64)", headers.Get("User-Agent"))
	require.Equal(t, "second-key", headers.Get("x-goog-api-key"))
}
