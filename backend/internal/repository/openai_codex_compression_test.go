//go:build unit

package repository

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

// Official Codex 1b6e0d8145 client.rs::responses_request_compression and
// http-client/request.rs: OAuth Responses HTTP is Zstd; API keys are not.
// Capture the production HTTPUpstream wire, not just a compression helper.
func TestCodexOAuthHTTPCompressionWire(t *testing.T) {
	type wireRequest struct {
		header http.Header
		body   []byte
		length int64
	}
	captured := make(chan wireRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured <- wireRequest{r.Header.Clone(), body, r.ContentLength}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client := NewHTTPUpstream(nil)
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	for _, tc := range []struct {
		name, kind, path, authMode string
		compressed                 bool
	}{
		{"oauth", service.AccountTypeOAuth, "/backend-api/codex/responses", "", true},
		{"setup token", service.AccountTypeSetupToken, "/backend-api/codex/responses", "", true},
		{"api key", service.AccountTypeAPIKey, "/v1/responses", "", false},
		{"compact", service.AccountTypeOAuth, "/backend-api/codex/responses/compact", "", false},
		{"models", service.AccountTypeOAuth, "/backend-api/codex/models", "", false},
		{"signed identity", service.AccountTypeOAuth, "/backend-api/codex/responses", "agentIdentity", false},
		{"signed shadow", service.AccountTypeOAuth, "/backend-api/codex/responses", "", false},
	} {
		for _, text := range []string{"hello", strings.Repeat("long context ", 10000)} {
			t.Run(tc.name+"/"+map[bool]string{true: "long", false: "short"}[len(text) > 20], func(t *testing.T) {
				body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"` + text + `"}`)
				req, err := http.NewRequest(http.MethodPost, server.URL+tc.path, bytes.NewReader(body))
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer fixture")
				if strings.HasPrefix(tc.name, "signed") {
					req.Header.Set("Authorization", "AgentAssertion fixture")
				}
				req.Header.Set("User-Agent", "codex_cli_rs/0.163.0 (Ubuntu 24.04; x86_64) xterm-256color")
				req.Header.Set("Originator", "codex_cli_rs")
				req.Header.Set("Version", "0.163.0")
				account := &service.Account{ID: 91, Platform: service.PlatformOpenAI, Type: tc.kind, Credentials: map[string]any{"auth_mode": tc.authMode}}
				service.PrepareCodexRequestCompression(req, account)
				for attempt := 0; attempt < 2; attempt++ {
					if attempt > 0 {
						req.Body, err = req.GetBody()
						require.NoError(t, err)
					}
					resp, err := client.Do(req, "", 91, 1)
					require.NoError(t, err)
					_, err = io.Copy(io.Discard, resp.Body)
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					wire := <-captured
					if tc.compressed {
						require.Equal(t, "zstd", wire.header.Get("Content-Encoding"))
						decoded, err := decoder.DecodeAll(wire.body, nil)
						require.NoError(t, err)
						require.Equal(t, body, decoded, "encoding must preserve every body byte on retries")
						if len(text) > 20 {
							require.Less(t, len(wire.body), len(body))
						}
					} else {
						require.Empty(t, wire.header.Get("Content-Encoding"))
						require.Equal(t, body, wire.body)
					}
					require.Equal(t, int64(len(wire.body)), wire.length)
					require.Equal(t, "codex_cli_rs", wire.header.Get("Originator"))
					require.Equal(t, "0.163.0", wire.header.Get("Version"))
				}
			})
		}
	}
}

func TestCodexCompressionConflictDoesNotSend(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/responses", strings.NewReader(`{"input":"hello"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fixture")
	req.Header.Set("Content-Encoding", "gzip")
	service.PrepareCodexRequestCompression(req, &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth})
	_, err = NewHTTPUpstream(nil).Do(req, "", 91, 1)
	require.ErrorContains(t, err, "compression conflicts")
	require.False(t, called)
}
