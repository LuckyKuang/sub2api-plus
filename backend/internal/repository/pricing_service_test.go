package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type PricingServiceSuite struct {
	suite.Suite
	ctx    context.Context
	srv    *httptest.Server
	client *pricingRemoteClient
}

func (s *PricingServiceSuite) SetupTest() {
	s.ctx = context.Background()
	client, ok := NewPricingRemoteClient(testPricingRemoteConfig()).(*pricingRemoteClient)
	require.True(s.T(), ok, "type assertion failed")
	s.client = client
}

func testPricingRemoteConfig() *config.Config {
	return &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
		PricingHosts: []string{"127.0.0.1", "localhost", "pricing.example.test"},
	}}}
}

func (s *PricingServiceSuite) TearDownTest() {
	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
	}
}

func (s *PricingServiceSuite) setupServer(handler http.HandlerFunc) {
	s.srv = newLocalTestServer(s.T(), handler)
}

func (s *PricingServiceSuite) TestFetchPricingJSON_Success() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))

	body, err := s.client.FetchPricingJSON(s.ctx, s.srv.URL+"/ok", 1024)
	require.NoError(s.T(), err, "FetchPricingJSON")
	require.Equal(s.T(), `{"ok":true}`, string(body), "body mismatch")
}

func (s *PricingServiceSuite) TestFetchPricingJSON_NonOKStatus() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	_, err := s.client.FetchPricingJSON(s.ctx, s.srv.URL+"/err", 1024)
	require.Error(s.T(), err, "expected error for non-200 status")
}

func (s *PricingServiceSuite) TestFetchPricingJSON_RejectsOversizedBody() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("123"))
		flusher.Flush()
		_, _ = w.Write([]byte("45"))
	}))

	_, err := s.client.FetchPricingJSON(s.ctx, s.srv.URL+"/large", 4)
	require.ErrorContains(s.T(), err, "exceeds 4 bytes")
}

func (s *PricingServiceSuite) TestFetchPricingJSON_RejectsInvalidSizeLimit() {
	_, err := s.client.FetchPricingJSON(s.ctx, "https://pricing.example.test/data", 0)
	require.ErrorContains(s.T(), err, "size limit must be positive")
}

func (s *PricingServiceSuite) TestFetchPricingJSON_InvalidURL() {
	_, err := s.client.FetchPricingJSON(s.ctx, "://invalid-url", 1024)
	require.Error(s.T(), err, "expected error for invalid URL")
}

func (s *PricingServiceSuite) TestFetchPricingJSON_ContextCancel() {
	started := make(chan struct{})
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithCancel(s.ctx)

	done := make(chan error, 1)
	go func() {
		_, err := s.client.FetchPricingJSON(ctx, s.srv.URL+"/block", 1024)
		done <- err
	}()

	<-started
	cancel()

	err := <-done
	require.Error(s.T(), err)
}

func (s *PricingServiceSuite) TestFetchPricingJSON_BlocksInsecureRedirectEvenWhenGlobalAllowlistIsDisabled() {
	s.setupServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://pricing.example.test/model-pricing.json", http.StatusFound)
	}))

	_, err := s.client.FetchPricingJSON(s.ctx, s.srv.URL+"/redirect", 1024)
	require.ErrorContains(s.T(), err, "pricing redirect blocked")
}

func TestNewPricingRemoteClient_InvalidProxy_NoFallback(t *testing.T) {
	cfg := testPricingRemoteConfig()
	cfg.Update.ProxyURL = "://bad"
	client := NewPricingRemoteClient(cfg)
	_, ok := client.(*pricingRemoteClientError)
	require.True(t, ok, "should return error client when proxy is invalid and fallback disabled")

	_, err := client.FetchPricingJSON(context.Background(), "http://example.com", 1024)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy client init failed")
}

func TestNewPricingRemoteClient_InvalidProxy_WithFallback(t *testing.T) {
	cfg := testPricingRemoteConfig()
	cfg.Update.ProxyURL = "://bad"
	cfg.Security.ProxyFallback.AllowDirectOnError = true
	client := NewPricingRemoteClient(cfg)
	_, ok := client.(*pricingRemoteClient)
	require.True(t, ok, "should fallback to direct client when allowed")
}

func TestPricingServiceSuite(t *testing.T) {
	suite.Run(t, new(PricingServiceSuite))
}
