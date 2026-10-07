//go:build unit || !integration

package brandidentity

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestStripOutboundHeadersRemovesEveryBrandedNameAndValue(t *testing.T) {
	header := http.Header{
		"X-Sub2API-Trace":         {"internal"},
		GrokClientToolCacheHeader: {"prefer-cache"},
		"X-Grok-Conv-Id":          {"conversation"},
		"User-Agent":              {"sub2api-client/1"},
		"X-Organization":          {"Sub2API Plus"},
		"Authorization":           {"Bearer sub2api-user-value"},
	}

	StripOutboundHeaders(header)

	require.Empty(t, header.Values("X-Sub2API-Trace"))
	require.Empty(t, header.Values(GrokClientToolCacheHeader))
	require.Equal(t, []string{"conversation"}, header.Values("X-Grok-Conv-Id"))
	require.NotEmpty(t, header.Values("User-Agent"), "preserve identity for rejection at send")
	require.Empty(t, header.Values("X-Organization"))
	require.NotEmpty(t, header.Values("Authorization"), "preserve credentials for rejection at send")
}

func TestOutboundPrivacyChecksEveryHeaderAndRedirect(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		for name, values := range r.Header {
			require.NotContains(t, strings.ToLower(name), "sub2api")
			for _, value := range values {
				require.NotContains(t, strings.ToLower(value), "sub2api")
			}
		}
		require.Equal(t, "official-client/1.0", r.UserAgent())
		require.Equal(t, "ordinary", r.Header.Get("X-Keep"))
		if r.URL.Path == "/sub2api-path" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/sub2api-path", nil)
	require.NoError(t, err)
	req.Header["X-Custom"] = []string{"safe", "prefix-SuB2ApI-suffix"}
	req.Header.Set("Prefix-SUB2API-Suffix", "safe")
	req.Header.Set("X-Title", "Sub2API Plus")
	req.Header.Set("User-Agent", "official-client/1.0")
	req.Header.Set("X-Keep", "ordinary")
	response, err := WrapClient(server.Client()).Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 2, calls)
	require.NotEmpty(t, req.Header.Values("X-Custom"), "the wrapper must not mutate its caller's map")
}

func TestOutboundPrivacyRejectsSensitiveAndSignedHeadersBeforeSending(t *testing.T) {
	for _, headers := range []http.Header{
		{"Authorization": {"Bearer private-SuB2ApI-token"}},
		{"X-Api-Key": {"private-sub2api-key"}},
		{"Authorization": {"AWS4-HMAC-SHA256 Credential=key, SignedHeaders=host;x-custom, Signature=abc"}, "X-Custom": {"sub2api"}},
	} {
		req, err := http.NewRequest(http.MethodGet, "https://api.example.com", nil)
		require.NoError(t, err)
		req.Header = headers.Clone()
		require.ErrorIs(t, FilterOutboundRequest(req), ErrBrandedOutboundHeader)
		require.Equal(t, headers, req.Header, "a rejected signed/authenticated request must remain intact")
	}
}

func TestOutboundPrivacyPreservesCleanSignedDeclarations(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=key, SignedHeaders=host;x-custom, Signature=abc")
	req.Header.Set("X-Custom", "signed-value")
	before := req.Header.Clone()
	require.NoError(t, FilterOutboundRequest(req))
	require.Equal(t, before, req.Header)
	req.Host = "sub2api.example.com"
	require.ErrorIs(t, FilterOutboundRequest(req), ErrBrandedOutboundHeader)
}

func TestForbiddenCredentialsCookiesAndSignedHeadersNeverReachServer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(204) }))
	defer server.Close()
	for _, header := range []string{"Authorization", "Cookie", "X-Api-Key", "X-Dsh-Auth-Token", "User-Agent"} {
		t.Run(header, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			req.Header.Set(header, "prefix-SuB2ApI-suffix")
			StripOutboundHeaders(req.Header) // earlier account-header processing must not hide credentials
			_, err = WrapClient(server.Client()).Do(req)
			require.ErrorIs(t, err, ErrBrandedOutboundHeader)
		})
	}
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	jar.SetCookies(endpoint, []*http.Cookie{{Name: "session", Value: "Sub2API-private"}})
	client := server.Client()
	client.Jar = jar
	_, err = WrapClient(client).Get(server.URL)
	require.ErrorIs(t, err, ErrBrandedOutboundHeader, "cookies added by net/http must be inspected at transport time")
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=key, SignedHeaders=host;x-custom, Signature=abc")
	req.Header.Set("X-Custom", "sub2api")
	_, err = WrapClient(server.Client()).Do(req)
	require.ErrorIs(t, err, ErrBrandedOutboundHeader)
	require.Zero(t, calls.Load(), "blocking means no network request, not merely an error after sending")
}

func TestReqClientFiltersSDKDefaultsAndRedirectReferer(t *testing.T) {
	observed := make(chan http.Header, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Clone()
		if r.URL.Path == "/sub2api" {
			http.Redirect(w, r, "/done", http.StatusFound)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	client := WrapReqClient(req.C()).SetCommonHeader("X-SDK-Default", "contains-Sub2API").SetCommonHeader("User-Agent", "official-client/1.0")
	response, err := client.R().SetHeader("X-Request", "SuB2ApI").Get(server.URL + "/sub2api")
	require.NoError(t, err)
	require.Equal(t, 204, response.StatusCode)
	for n := 0; n < 2; n++ {
		sent := <-observed
		require.Equal(t, "official-client/1.0", sent.Get("User-Agent"))
		for name, values := range sent {
			require.NotContains(t, strings.ToLower(name), "sub2api")
			for _, value := range values {
				require.NotContains(t, strings.ToLower(value), "sub2api")
			}
		}
	}
}

type privacyTrackedBody struct {
	*strings.Reader
	closed bool
}

func (b *privacyTrackedBody) Close() error { b.closed = true; return nil }
func TestRejectedOutboundRequestClosesItsBody(t *testing.T) {
	body := &privacyTrackedBody{Reader: strings.NewReader("request body")}
	request, err := http.NewRequest(http.MethodPost, "https://example.com", body)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer sub2api-token")
	_, err = WrapClient(nil).Do(request)
	require.ErrorIs(t, err, ErrBrandedOutboundHeader)
	require.True(t, body.closed, "blocked uploads must release their request resources")
}
