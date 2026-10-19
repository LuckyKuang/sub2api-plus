//go:build unit || !integration

package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestContentModerationSupplierIdentityAcrossRetriesAndKeyRotation(t *testing.T) {
	for _, rotateKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-key", true: "new-key"}[rotateKey], func(t *testing.T) {
			config := emptyOutboundIdentitySettings()

			settings, ctx := outboundIdentityTestSettings(t, config)
			require.NoError(t, settings.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersion, "0.200.1"))
			foreign := builtInOutboundIdentity("claude")
			foreign.AccountID = 77
			ctx = outboundidentity.WithIdentity(ctx, foreign)
			captured := make(chan http.Header, 3)
			updated := make(chan error, 1)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				calls++
				if calls == 1 {
					updated <- settings.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersion, "0.200.2")
					settings.InvalidateOpenAICodexClientVersionCache()
					// Malformed 2xx response retries without freezing the credential.
					_, _ = w.Write([]byte(`{"results":[]}`))
					return
				}
				_, _ = w.Write([]byte(`{"results":[{"flagged":false}]}`))
			}))
			defer server.Close()
			svc := NewContentModerationService(nil, nil, nil, nil, nil, nil, nil, nil)
			cfg := defaultContentModerationConfig()
			cfg.RetryCount = 1
			endpoint := ContentModerationEndpoint{ID: "test", BaseURL: server.URL, Model: "test-model", APIKeys: []string{"key-one"}, TimeoutMS: 1000}
			if rotateKey {
				endpoint.APIKeys = append(endpoint.APIKeys, "key-two")
			}
			result, status, err := svc.callModerationEndpoint(ctx, cfg, endpoint, "hello", false)
			require.NoError(t, err)
			require.NoError(t, <-updated)
			require.Equal(t, http.StatusOK, status)
			require.False(t, result.Flagged)
			first, retry := <-captured, <-captured
			require.Equal(t, "0.200.1", first.Get("Version"))
			require.Equal(t, "Bearer key-one", first.Get("Authorization"))
			if rotateKey {
				require.Equal(t, "0.200.2", retry.Get("Version"))
				require.Equal(t, "Bearer key-two", retry.Get("Authorization"))
			} else {
				require.Equal(t, first.Get("User-Agent"), retry.Get("User-Agent"))
				require.Equal(t, "0.200.1", retry.Get("Version"))
			}
			cfg.BaseURL = server.URL
			_, err = svc.callModerationOnceWithInput(ctx, cfg, "key-one", "hello", &status)
			require.NoError(t, err)
			fresh := <-captured
			require.Equal(t, "0.200.2", fresh.Get("Version"))
			for _, headers := range []http.Header{first, retry, fresh} {
				require.Equal(t, "codex_cli_rs/"+headers.Get("Version")+" (Ubuntu 24.04; x86_64) xterm-256color", headers.Get("User-Agent"))
				require.Equal(t, "codex_cli_rs", headers.Get("Originator"))
				require.Empty(t, headers.Get("X-App"))
				require.Empty(t, headers.Get("x-grok-client-version"))
			}
		})
	}
}
