//go:build unit || !integration

package brandidentity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Business contract: routing Host/authority is exempt from the project-token
// policy. Its value and signed declarations must survive real dispatch/redirects.
func TestRoutingHostExceptionReachesServerAndRedirect(t *testing.T) {
	var hosts []string
	const authority = "SuB2ApI.relay.example.test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hosts = append(hosts, r.Host)
		require.Equal(t, "official-client/1.0", r.UserAgent())
		require.Empty(t, r.Header.Get("X-Private"))
		require.Equal(t, "prefer-cache", r.Header.Get("X-Grok-Client-Tool-Cache"))
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://"+authority+"/done", http.StatusFound)
			return
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, "http://"+authority+"/start", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "official-client/1.0")
	req.Header.Set("X-Private", "sub2api")
	req.Header.Set("X-Grok-Client-Tool-Cache", "prefer-cache")
	response, err := WrapClient(&http.Client{Transport: transport}).Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, []string{authority, authority}, hosts)
}

func TestSignedRoutingHostExceptionPreservesDeclarations(t *testing.T) {
	for _, target := range []string{
		"https://sub2api.example.test:8443/request",
		"https://sub2api.example.test/request?X-Amz-SignedHeaders=host%3Bx-extra&X-Amz-Signature=abc",
		"https://sub2api.example.test/request?X-Goog-SignedHeaders=host%3Bx-extra&X-Goog-Signature=abc",
	} {
		req, err := http.NewRequest(http.MethodPost, target, nil)
		require.NoError(t, err)
		req.Host = "sub2api.virtual.example.test:8443"
		req.Header.Set("Authorization", "Signature keyId=neutral,headers=host,signature=abc")
		req.Header.Set("Host", req.Host)
		req.Header.Set("X-Grok-Client-Tool-Cache", "off")
		before := req.Header.Clone()
		require.NoError(t, FilterOutboundRequest(req))
		require.Equal(t, target, req.URL.String())
		require.Equal(t, "sub2api.virtual.example.test:8443", req.Host)
		require.Equal(t, before, req.Header)
	}
}

func TestRoutingHostExceptionPreservesHTTP2AuthorityAndSNI(t *testing.T) {
	const authority = "sub2api.relay.example.test"
	observed := make(chan *http.Request, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r
		w.WriteHeader(http.StatusNoContent)
	}))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{authority},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, key.Public(), key)
	require.NoError(t, err)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	defer transport.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, "https://"+authority+"/request", nil)
	require.NoError(t, err)
	req.Host = "sub2api.virtual.example.test"
	req.Header.Set("User-Agent", "official-client/1.0")
	req.Header.Set("Authorization", "Signature keyId=neutral,headers=host,signature=abc")
	response, err := WrapClient(&http.Client{Transport: transport}).Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	sent := <-observed
	require.Equal(t, 2, sent.ProtoMajor)
	require.Equal(t, "sub2api.virtual.example.test", sent.Host)
	require.Equal(t, authority, sent.TLS.ServerName)
	require.Equal(t, "Signature keyId=neutral,headers=host,signature=abc", sent.Header.Get("Authorization"))
}

func TestHostExceptionDoesNotExemptTrailersOrCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, reason      string
		headers, trailers http.Header
	}{
		{"ordinary trailer", "", nil, http.Header{"X-Extra": {"private-sub2api"}}},
		{"trailer Host is not authority", "", nil, http.Header{"Host": {"sub2api.example.test"}}},
		{"protected trailer", "protected_trailer", nil, http.Header{"Authorization": {"Bearer private-sub2api"}}},
		{"protected header", "protected_header", http.Header{"Cookie": {"session=private-sub2api"}}, nil},
		{"signed trailer", "signed_declaration", http.Header{"Authorization": {"Signature keyId=neutral,headers=x-extra,signature=abc"}}, http.Header{"X-Extra": {"private-sub2api"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://sub2api.example.test", nil)
			require.NoError(t, err)
			req.Header, req.Trailer = tc.headers.Clone(), tc.trailers.Clone()
			err = FilterOutboundRequest(req)
			if tc.reason == "" {
				require.NoError(t, err)
				require.Empty(t, req.Trailer)
				return
			}
			var violation *Violation
			require.ErrorAs(t, err, &violation)
			require.Equal(t, tc.reason, violation.Reason)
			require.NotContains(t, err.Error(), "private-sub2api")
			require.Equal(t, tc.headers, req.Header)
			require.Equal(t, tc.trailers, req.Trailer)
		})
	}
}
