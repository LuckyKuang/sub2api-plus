//go:build unit || !integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type OpenAIOAuthServiceSuite struct {
	suite.Suite
	ctx      context.Context
	srv      *httptest.Server
	svc      *openaiOAuthService
	received chan url.Values
}

func (s *OpenAIOAuthServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.received = make(chan url.Values, 1)
}

func (s *OpenAIOAuthServiceSuite) TearDownTest() {
	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
	}
}

func (s *OpenAIOAuthServiceSuite) setupServer(handler http.HandlerFunc) {
	s.srv = newLocalTestServer(s.T(), handler)
	s.svc = &openaiOAuthService{
		tokenURL:          s.srv.URL,
		revokeURL:         s.srv.URL,
		deviceAuthAPIBase: s.srv.URL,
	}
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_DefaultRedirectURI() {
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errCh <- "method mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			errCh <- "read body failed"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		wantBody := openai.EncodeAuthorizationCodeTokenBody("code", openai.DefaultRedirectURI, openai.ClientID, "ver")
		if string(raw) != wantBody {
			errCh <- "form body mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-www-form-urlencoded") {
			errCh <- "content-type mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("User-Agent"); got != "" {
			errCh <- "user-agent must be empty on token exchange"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("originator"); got != "" {
			errCh <- "originator must be empty on token exchange"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Version"); got != "" {
			errCh <- "version must be empty on token exchange"
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.ExchangeCode(s.ctx, "code", "ver", "", "", "")
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
	require.Equal(s.T(), "at", resp.AccessToken)
	require.Equal(s.T(), "rt", resp.RefreshToken)
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCodeWithIdentityPairsAndFallsBackUserAgent() {
	requests := make(chan [3]string, 2)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- [3]string{r.Header.Get("User-Agent"), r.Header.Get("Originator"), r.Header.Get("Version")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"bearer","expires_in":3600}`)
	}))

	const validUA = "codex-tui/0.150.0 (Ubuntu 22.4.0; x86_64) xterm-256color (codex-tui; 0.150.0)"
	_, err := s.svc.ExchangeCodeWithIdentity(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "", validUA, "client-controlled", "0.150.0")
	require.NoError(s.T(), err)
	require.Equal(s.T(), [3]string{"", "", ""}, <-requests)

	_, err = s.svc.ExchangeCodeWithIdentity(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "", "Mozilla/5.0", "client-controlled", "9.9.9")
	require.NoError(s.T(), err)
	require.Equal(s.T(), [3]string{"", "", ""}, <-requests)
}

func TestResolveOpenAIOAuthIdentity_LegacyRequiresExplicitResolvedOriginator(t *testing.T) {
	const legacyUA = "codex_sdk_ts/0.150.0 (Ubuntu 24.04; x86_64) xterm-256color"

	userAgent, originator, version := resolveOpenAIOAuthIdentity(legacyUA, "", "0.150.0")
	require.Equal(t, service.DefaultOpenAICodexUserAgent, userAgent)
	require.Equal(t, openai.CodexDefaultOriginator, originator)
	require.Equal(t, service.DefaultOpenAICodexVersion, version)

	userAgent, originator, version = resolveOpenAIOAuthIdentity(legacyUA, "codex_sdk_ts", "0.150.0")
	require.Equal(t, legacyUA, userAgent)
	require.Equal(t, "codex_sdk_ts", originator)
	require.Equal(t, "0.150.0", version)
}

func (s *OpenAIOAuthServiceSuite) TestIdentityRetainsConfiguredOutboundVersionSyntax() {
	requests := make(chan [3]string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- [3]string{r.Header.Get("User-Agent"), r.Header.Get("Originator"), r.Header.Get("Version")}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","expires_in":3600}`)
	}))
	for _, family := range []string{"codex_cli_rs", "codex_exec"} {
		for _, version := range []string{"0.200", "0.2000"} {
			ua := family + "/" + version + " (Ubuntu 24.04; x86_64) terminal"
			_, err := s.svc.ExchangeCodeWithIdentity(s.ctx, "code", "verifier", openai.DefaultRedirectURI, "", "", ua, family, version)
			require.NoError(s.T(), err)
			require.Equal(s.T(), [3]string{"", "", ""}, <-requests)
			_, err = s.svc.RefreshTokenWithClientIDAndIdentity(s.ctx, "refresh", "", "", ua, family, version)
			require.NoError(s.T(), err)
			require.Equal(s.T(), [3]string{ua, family, ""}, <-requests)
			got, _, _ := resolveOpenAIOAuthIdentity(ua, "", version)
			require.Equal(s.T(), service.DefaultOpenAICodexUserAgent, got, "UA-only callers cannot supply a policy-approved historical version")
		}
	}
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_FormFields() {
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			errCh <- "content-type mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errCh <- "json decode failed"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := payload["grant_type"]; got != "refresh_token" {
			errCh <- "grant_type mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := payload["refresh_token"]; got != "rt" {
			errCh <- "refresh_token mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := payload["client_id"]; got != openai.ClientID {
			errCh <- "client_id mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, ok := payload["scope"]; ok {
			errCh <- "scope must be omitted"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("User-Agent"); got != service.DefaultOpenAICodexUserAgent {
			errCh <- "user-agent mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("originator"); got != openai.CodexDefaultOriginator {
			errCh <- "originator mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at2","refresh_token":"rt2","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.NoError(s.T(), err, "RefreshToken")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
	require.Equal(s.T(), "at2", resp.AccessToken)
	require.Equal(s.T(), "rt2", resp.RefreshToken)
}

// TestRefreshToken_DefaultsToOpenAIClientID 验证未指定 client_id 时默认使用 OpenAI ClientID，
// 且只发送一次请求（不再盲猜多个 client_id）。
func (s *OpenAIOAuthServiceSuite) TestRefreshToken_DefaultsToOpenAIClientID() {
	var seenClientIDs []string
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clientID := payload["client_id"]
		seenClientIDs = append(seenClientIDs, clientID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","refresh_token":"rt","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.NoError(s.T(), err, "RefreshToken")
	require.Equal(s.T(), "at", resp.AccessToken)
	// 只发送了一次请求，使用默认的 OpenAI ClientID
	require.Equal(s.T(), []string{openai.ClientID}, seenClientIDs)
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_UseProvidedClientID() {
	const customClientID = "custom-client-id"
	var seenClientIDs []string
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		clientID := payload["client_id"]
		seenClientIDs = append(seenClientIDs, clientID)
		if clientID != customClientID {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at-custom","refresh_token":"rt-custom","token_type":"bearer","expires_in":3600}`)
	}))

	resp, err := s.svc.RefreshTokenWithClientID(s.ctx, "rt", "", customClientID)
	require.NoError(s.T(), err, "RefreshTokenWithClientID")
	require.Equal(s.T(), "at-custom", resp.AccessToken)
	require.Equal(s.T(), "rt-custom", resp.RefreshToken)
	require.Equal(s.T(), []string{customClientID}, seenClientIDs)
}

func (s *OpenAIOAuthServiceSuite) TestNonSuccessStatus_IncludesBody() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad")
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "status 400")
	require.ErrorContains(s.T(), err, "bad")
}

func (s *OpenAIOAuthServiceSuite) TestRequestError_ClosedServer() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	s.srv.Close()

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "request failed")
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_RequestErrorWithoutProxyReturnsProxyHint() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	s.srv.Close()

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")

	require.Error(s.T(), err)
	require.Equal(s.T(), "OPENAI_OAUTH_PROXY_REQUIRED", infraerrors.Reason(err))
	require.Contains(s.T(), infraerrors.Message(err), "no proxy is configured")
}

func (s *OpenAIOAuthServiceSuite) TestContextCancel() {
	started := make(chan struct{})
	block := make(chan struct{})
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-block
	}))

	ctx, cancel := context.WithCancel(s.ctx)

	done := make(chan error, 1)
	go func() {
		_, err := s.svc.ExchangeCode(ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
		done <- err
	}()

	<-started
	cancel()
	close(block)

	err := <-done
	require.Error(s.T(), err)
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_UsesProvidedRedirectURI() {
	want := "http://localhost:9999/cb"
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.PostForm.Get("redirect_uri"); got != want {
			errCh <- "redirect_uri mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"bearer","expires_in":1}`)
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", want, "", "")
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_UseProvidedClientID() {
	wantClientID := "custom-exchange-client-id"
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.PostForm.Get("client_id"); got != wantClientID {
			errCh <- "client_id mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"bearer","expires_in":1}`)
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", wantClientID)
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func (s *OpenAIOAuthServiceSuite) TestTokenURL_CanBeOverriddenWithQuery() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.received <- r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"at","token_type":"bearer","expires_in":1}`)
	}))
	s.svc.tokenURL = s.srv.URL + "?x=1"

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.NoError(s.T(), err, "ExchangeCode")
	select {
	case <-s.received:
	default:
		require.Fail(s.T(), "expected server to receive request")
	}
}

func (s *OpenAIOAuthServiceSuite) TestExchangeCode_SuccessButInvalidJSON() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "not-valid-json")
	}))

	_, err := s.svc.ExchangeCode(s.ctx, "code", "ver", openai.DefaultRedirectURI, "", "")
	require.Error(s.T(), err, "expected error for invalid JSON response")
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_NonSuccessStatus() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized")
	}))

	_, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.Error(s.T(), err, "expected error for non-2xx status")
	require.ErrorContains(s.T(), err, "status 401")
	require.Equal(s.T(), "OPENAI_OAUTH_REFRESH_PERMANENT", infraerrors.Reason(err))
}

func (s *OpenAIOAuthServiceSuite) TestRefreshToken_ExpiredRefreshIsPermanent() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"refresh_token_expired"}}`)
	}))

	_, err := s.svc.RefreshToken(s.ctx, "rt", "")
	require.Error(s.T(), err)
	require.Equal(s.T(), "OPENAI_OAUTH_REFRESH_PERMANENT", infraerrors.Reason(err))
}

func TestNewOpenAIOAuthClient_DefaultTokenURL(t *testing.T) {
	client := NewOpenAIOAuthClient()
	svc, ok := client.(*openaiOAuthService)
	require.True(t, ok)
	require.Equal(t, openai.TokenURL, svc.tokenURL)
	require.Equal(t, openai.RevokeURL, svc.revokeURL)
	require.Equal(t, openai.DeviceAuthAPIBase, svc.deviceAuthAPIBase)
}

func (s *OpenAIOAuthServiceSuite) TestRevokeToken_JSONBodyAndIdentityHeaders() {
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			errCh <- "method mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if ct := r.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			errCh <- "content-type mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("User-Agent"); got != service.DefaultOpenAICodexUserAgent {
			errCh <- "user-agent mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("originator"); got != openai.CodexDefaultOriginator {
			errCh <- "originator mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Version"); got != "" {
			errCh <- "version must be empty"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errCh <- "json decode failed"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload["token"] != "rt" || payload["token_type_hint"] != "refresh_token" || payload["client_id"] != openai.ClientID {
			errCh <- "body mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	err := s.svc.RevokeToken(s.ctx, "rt", "refresh_token", "", "", "", "")
	require.NoError(s.T(), err)
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func (s *OpenAIOAuthServiceSuite) TestRevokeToken_AccessTokenOmitsClientID() {
	errCh := make(chan string, 1)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			errCh <- "json decode failed"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if payload["token"] != "at" || payload["token_type_hint"] != "access_token" {
			errCh <- "body mismatch"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, ok := payload["client_id"]; ok {
			errCh <- "client_id must be omitted for access_token revoke"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	err := s.svc.RevokeToken(s.ctx, "at", "access_token", openai.ClientID, "", "", "")
	require.NoError(s.T(), err)
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func (s *OpenAIOAuthServiceSuite) TestStartAndPollDeviceCode() {
	errCh := make(chan string, 2)
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "" {
			errCh <- "user-agent must be empty on device-code " + r.URL.Path
		}
		if got := r.Header.Get("originator"); got != "" {
			errCh <- "originator must be empty on device-code " + r.URL.Path
		}
		if got := r.Header.Get("Version"); got != "" {
			errCh <- "version must be empty on device-code " + r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, openai.DeviceAuthUserCodePath):
			_, _ = io.WriteString(w, `{"device_auth_id":"dev-1","user_code":"WXYZ-9876","interval":"7"}`)
		case strings.HasSuffix(r.URL.Path, openai.DeviceAuthTokenPath):
			_, _ = io.WriteString(w, `{"authorization_code":"auth-code","code_challenge":"chal","code_verifier":"ver"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	started, err := s.svc.StartDeviceCode(s.ctx, "", "")
	require.NoError(s.T(), err)
	require.Equal(s.T(), "dev-1", started.DeviceAuthID)
	require.Equal(s.T(), "WXYZ-9876", started.UserCode)
	require.Equal(s.T(), int64(7), started.Interval)

	polled, pending, err := s.svc.PollDeviceCode(s.ctx, "", started.DeviceAuthID, started.UserCode)
	require.NoError(s.T(), err)
	require.False(s.T(), pending)
	require.Equal(s.T(), "auth-code", polled.AuthorizationCode)
	require.Equal(s.T(), "ver", polled.CodeVerifier)
	select {
	case msg := <-errCh:
		require.Fail(s.T(), msg)
	default:
	}
}

func TestOpenAIOAuthServiceSuite(t *testing.T) {
	suite.Run(t, new(OpenAIOAuthServiceSuite))
}
