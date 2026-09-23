//go:build unit || !integration

package repository

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return parsed
}

func TestChatGptCloudflareCookieJar_StoresAllowlistedInfraCookies(t *testing.T) {
	jar, err := newChatGptCloudflareCookieJar()
	if err != nil {
		t.Fatalf("newChatGptCloudflareCookieJar: %v", err)
	}
	target := mustParseURL(t, "https://chatgpt.com/backend-api/codex/responses")
	jar.SetCookies(target, []*http.Cookie{
		{Name: "__cf_bm", Value: "infra-1"},
		{Name: "_cfuvid", Value: "infra-2"},
		{Name: "cf_chl_2", Value: "challenge"},
		{Name: "__oailb", Value: "route-1"},
	})

	cookies := jar.Cookies(target)
	if len(cookies) != 4 {
		t.Fatalf("stored cookies = %d, want 4 (%v)", len(cookies), cookies)
	}
	names := map[string]string{}
	for _, cookie := range cookies {
		names[cookie.Name] = cookie.Value
	}
	for name, want := range map[string]string{"__cf_bm": "infra-1", "_cfuvid": "infra-2", "cf_chl_2": "challenge", "__oailb": "route-1"} {
		if names[name] != want {
			t.Fatalf("cookie %s = %q, want %q", name, names[name], want)
		}
	}
}

func TestChatGptCloudflareCookieJar_DropsAccountAndForeignCookies(t *testing.T) {
	jar, err := newChatGptCloudflareCookieJar()
	if err != nil {
		t.Fatalf("newChatGptCloudflareCookieJar: %v", err)
	}
	target := mustParseURL(t, "https://chatgpt.com/backend-api/wham/usage")
	jar.SetCookies(target, []*http.Cookie{
		{Name: "__Secure-next-auth.session-token", Value: "account-session"},
		{Name: "oai-did", Value: "device-id"},
		{Name: "cf_clearance", Value: "allowed"},
	})

	cookies := jar.Cookies(target)
	if len(cookies) != 1 || cookies[0].Name != "cf_clearance" {
		t.Fatalf("only the allowlisted infra cookie must survive, got %v", cookies)
	}
}

func TestChatGptCloudflareCookieJar_IgnoresNonChatgptAndPlainHTTP(t *testing.T) {
	jar, err := newChatGptCloudflareCookieJar()
	if err != nil {
		t.Fatalf("newChatGptCloudflareCookieJar: %v", err)
	}
	jar.SetCookies(mustParseURL(t, "https://auth.openai.com/oauth/token"), []*http.Cookie{{Name: "__cf_bm", Value: "x"}})
	jar.SetCookies(mustParseURL(t, "http://chatgpt.com/backend-api"), []*http.Cookie{{Name: "__cf_bm", Value: "y"}})
	jar.SetCookies(mustParseURL(t, "https://evil.example/"), []*http.Cookie{{Name: "__cf_bm", Value: "z"}})

	if cookies := jar.Cookies(mustParseURL(t, "https://auth.openai.com/oauth/token")); len(cookies) != 0 {
		t.Fatalf("non-chatgpt host must not read cookies, got %v", cookies)
	}
	if cookies := jar.Cookies(mustParseURL(t, "http://chatgpt.com/backend-api")); len(cookies) != 0 {
		t.Fatalf("plain http must not read cookies, got %v", cookies)
	}
	if cookies := jar.Cookies(mustParseURL(t, "https://evil.example/")); len(cookies) != 0 {
		t.Fatalf("foreign host must not read cookies, got %v", cookies)
	}
}

func TestIsAllowedChatgptCookieHost_Subdomains(t *testing.T) {
	for _, host := range []string{"chatgpt.com", "chat.openai.com", "chatgpt-staging.com", "api.chatgpt.com", "x.chatgpt-staging.com"} {
		if !isAllowedChatgptCookieHost(host) {
			t.Fatalf("host %q must be allowed", host)
		}
	}
	for _, host := range []string{"openai.com", "evil-chatgpt.com", "chatgpt.com.evil.example", ""} {
		if isAllowedChatgptCookieHost(host) {
			t.Fatalf("host %q must not be allowed", host)
		}
	}
}

func TestBuildReqClientKey_CookieJarSeparatesCache(t *testing.T) {
	base := reqClientOptions{ProxyURL: "http://proxy.local:8080", Timeout: 120 * time.Second}
	withJar := base
	withJar.ChatGPTCookieJar = true
	if buildReqClientKey(base) == buildReqClientKey(withJar) {
		t.Fatal("cookie jar flag must separate the shared client cache key")
	}
}

func TestResolveOpenAIOAuthURLOverride(t *testing.T) {
	const env = "CODEX_REFRESH_TOKEN_URL_OVERRIDE"
	const fallback = "https://auth.openai.com/oauth/token"

	t.Setenv(env, "")
	if got := resolveOpenAIOAuthURLOverride(env, fallback); got != fallback {
		t.Fatalf("empty env must fall back, got %q", got)
	}

	t.Setenv(env, "  https://mirror.example/oauth/token  ")
	if got := resolveOpenAIOAuthURLOverride(env, fallback); got != "https://mirror.example/oauth/token" {
		t.Fatalf("valid override must be trimmed and pass through, got %q", got)
	}

	for _, invalid := range []string{"not-a-url", "ftp://mirror.example/token", "/relative/token", "https://"} {
		t.Setenv(env, invalid)
		if got := resolveOpenAIOAuthURLOverride(env, fallback); got != fallback {
			t.Fatalf("invalid override %q must fall back, got %q", invalid, got)
		}
	}
}

func TestNewOpenAIOAuthClient_AppliesURLOverrides(t *testing.T) {
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", "https://mirror.example/oauth/token")
	t.Setenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE", "https://mirror.example/oauth/revoke")
	client, ok := NewOpenAIOAuthClient().(*openaiOAuthService)
	if !ok {
		t.Fatal("unexpected client type")
	}
	if client.tokenURL != "https://mirror.example/oauth/token" {
		t.Fatalf("tokenURL = %q", client.tokenURL)
	}
	if client.revokeURL != "https://mirror.example/oauth/revoke" {
		t.Fatalf("revokeURL = %q", client.revokeURL)
	}
}

// Official revoke.rs derives the revoke endpoint from a configured refresh
// override when no revoke override is set, so a deployment that only overrides
// refresh does not revoke against the production host.
func TestNewOpenAIOAuthClient_DerivesRevokeURLFromRefreshOverride(t *testing.T) {
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", "https://mirror.example/auth/oauth/token?tenant=acme")
	t.Setenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE", "")

	client, ok := NewOpenAIOAuthClient().(*openaiOAuthService)
	if !ok {
		t.Fatal("unexpected client type")
	}
	if client.tokenURL != "https://mirror.example/auth/oauth/token?tenant=acme" {
		t.Fatalf("tokenURL = %q", client.tokenURL)
	}
	if want := "https://mirror.example/oauth/revoke"; client.revokeURL != want {
		t.Fatalf("revokeURL = %q, want %q (path rewritten, query dropped)", client.revokeURL, want)
	}
}

func TestNewOpenAIOAuthClient_NoOverrideKeepsDefaultHosts(t *testing.T) {
	t.Setenv("CODEX_REFRESH_TOKEN_URL_OVERRIDE", "")
	t.Setenv("CODEX_REVOKE_TOKEN_URL_OVERRIDE", "")

	client, ok := NewOpenAIOAuthClient().(*openaiOAuthService)
	if !ok {
		t.Fatal("unexpected client type")
	}
	if client.tokenURL != "https://auth.openai.com/oauth/token" {
		t.Fatalf("tokenURL = %q", client.tokenURL)
	}
	if client.revokeURL != "https://auth.openai.com/oauth/revoke" {
		t.Fatalf("revokeURL = %q", client.revokeURL)
	}
}

func TestDeriveOpenAIOAuthRevokeURL(t *testing.T) {
	if got := deriveOpenAIOAuthRevokeURL(""); got != "" {
		t.Fatalf("empty refresh URL must not derive, got %q", got)
	}
	if got := deriveOpenAIOAuthRevokeURL("not-a-url"); got != "" {
		t.Fatalf("invalid refresh URL must not derive, got %q", got)
	}
	if got := deriveOpenAIOAuthRevokeURL("/relative/token"); got != "" {
		t.Fatalf("relative refresh URL must not derive, got %q", got)
	}
	if got := deriveOpenAIOAuthRevokeURL("https://auth.openai.com/oauth/token"); got != "" {
		t.Fatalf("the official default refresh URL must not derive, got %q", got)
	}
	if got, want := deriveOpenAIOAuthRevokeURL("https://gw.internal/oauth/token"), "https://gw.internal/oauth/revoke"; got != want {
		t.Fatalf("derived = %q, want %q", got, want)
	}
	if got, want := deriveOpenAIOAuthRevokeURL("http://127.0.0.1:9/token"), "http://127.0.0.1:9/oauth/revoke"; got != want {
		t.Fatalf("derived = %q, want %q", got, want)
	}
}

// The custom-CA policy now lives in pkg/openai so both the credential-plane
// pool and the official auth-plane clients share one implementation. These
// tests pin the exported contract: standard and OpenSSL labels are accepted,
// a bundle without certificates fails, and a misconfigured bundle fails client
// creation early for Codex clients only.

// codexTestCAPEMPath 写一份单证书 PEM bundle 到临时目录并返回路径。
func codexTestCAPEMPath(t *testing.T, blockType string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sub2api-test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	return path
}

// codexTestCA 生成一张自签 CA 与由它签发的 localhost 服务端证书，用于功能性
// 验证自定义根证书确实进入了 pool（替代已废弃的 CertPool.Subjects()）。
type codexTestCA struct {
	CertPEM []byte
	KeyPEM  []byte
	Leaf    tls.Certificate
}

func newCodexTestCA(t *testing.T) codexTestCA {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "sub2api-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
	if err != nil {
		t.Fatalf("create ca certificate: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca certificate: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, leafKey.Public(), caKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}
	return codexTestCA{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER}),
		Leaf: tls.Certificate{
			Certificate: [][]byte{leafDER, caDER},
			PrivateKey:  leafKey,
		},
	}
}

// requirePoolTrustsTestCA 用一个由测试 CA 签发的 TLS 服务端验证 pool：只有
// 该 CA 真的在 pool 里，握手才会成功。ca 必须与写入 pool 的那份 bundle 同一张。
func requirePoolTrustsTestCA(t *testing.T, pool *x509.CertPool, ca codexTestCA) {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{ca.Leaf}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	defer server.Close()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("a pool containing the custom CA must complete the handshake: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
}

func TestCodexCARootPool_AppendsCertificateToSystemRoots(t *testing.T) {
	ca := newCodexTestCA(t)
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, ca.CertPEM, 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	t.Setenv(openai.CodexCAEnvPrimary, path)
	t.Setenv(openai.CodexCAEnvFallback, "")

	bundle, err := openai.CodexCARootPool()
	if err != nil {
		t.Fatalf("CodexCARootPool: %v", err)
	}
	if bundle.SourceEnv != openai.CodexCAEnvPrimary || bundle.Path != path {
		t.Fatalf("resolved = %+v, want primary %s", bundle, path)
	}
	requirePoolTrustsTestCA(t, bundle.Pool, ca)

	// System roots stay available: a bundle pool must still be non-nil and usable
	// by the TLS stack for unrelated hosts (verified structurally, without
	// network access).
	systemPool, sysErr := x509.SystemCertPool()
	if sysErr == nil && systemPool != nil {
		if bundle.Pool == systemPool {
			t.Fatal("the custom bundle must be a copy, not the shared system pool")
		}
	}
}

func TestCodexCARootPool_AcceptsTrustedCertificateLabel(t *testing.T) {
	ca := newCodexTestCA(t)
	// Wrap the same DER in the OpenSSL TRUSTED CERTIFICATE label the official
	// normalizer accepts.
	path := filepath.Join(t.TempDir(), "trusted.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "TRUSTED CERTIFICATE", Bytes: ca.Leaf.Certificate[1]}), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	t.Setenv(openai.CodexCAEnvPrimary, path)
	t.Setenv(openai.CodexCAEnvFallback, "")

	bundle, err := openai.CodexCARootPool()
	if err != nil {
		t.Fatalf("CodexCARootPool: %v", err)
	}
	requirePoolTrustsTestCA(t, bundle.Pool, ca)
}

func TestCodexCARootPool_RejectsBundleWithoutCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte("-----BEGIN X509 CRL-----\nnot-a-certificate\n-----END X509 CRL-----\n"), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	t.Setenv(openai.CodexCAEnvPrimary, path)
	t.Setenv(openai.CodexCAEnvFallback, "")

	if _, err := openai.CodexCARootPool(); err == nil {
		t.Fatal("bundle without CERTIFICATE blocks must fail")
	}
}

// CODEX_CA_CERTIFICATE takes precedence over SSL_CERT_FILE, and empty values
// are treated as unset (official custom_ca.rs contract).
func TestCodexCARootPool_EnvPrecedenceAndEmptyUnset(t *testing.T) {
	primary := codexTestCAPEMPath(t, "CERTIFICATE")
	fallback := codexTestCAPEMPath(t, "CERTIFICATE")

	t.Setenv(openai.CodexCAEnvPrimary, primary)
	t.Setenv(openai.CodexCAEnvFallback, fallback)
	bundle, err := openai.CodexCARootPool()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.SourceEnv != openai.CodexCAEnvPrimary || bundle.Path != primary {
		t.Fatalf("resolved = %+v, want primary %s", bundle, primary)
	}

	t.Setenv(openai.CodexCAEnvPrimary, "  ")
	bundle, err = openai.CodexCARootPool()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bundle.SourceEnv != openai.CodexCAEnvFallback || bundle.Path != fallback {
		t.Fatalf("resolved = %+v, want fallback %s", bundle, fallback)
	}

	t.Setenv(openai.CodexCAEnvPrimary, "")
	t.Setenv(openai.CodexCAEnvFallback, "")
	bundle, err = openai.CodexCARootPool()
	if err != nil || bundle.Pool != nil {
		t.Fatalf("unset env must disable the custom CA: err=%v resolved=%+v", err, bundle)
	}
}

// A misconfigured bundle fails OpenAI Codex client creation early with a precise
// error instead of silently falling back to system roots. Other providers never
// consult the Codex CA env, so a bad bundle cannot take them down.
func TestGetSharedReqClient_MisconfiguredCAFailsEarly(t *testing.T) {
	sharedReqClients = sync.Map{}
	missing := filepath.Join(t.TempDir(), "missing.pem")
	t.Setenv(openai.CodexCAEnvPrimary, missing)
	t.Setenv(openai.CodexCAEnvFallback, "")
	if _, err := openai.CodexCARootPool(); err == nil {
		t.Fatal("missing CA file must record an error")
	}
	_, err := getSharedReqClient(reqClientOptions{Timeout: time.Second, OpenAICodexClient: true})
	if err == nil {
		t.Fatal("OpenAI Codex client creation must fail early on a misconfigured CA bundle")
	}
	if !strings.Contains(err.Error(), openai.CodexCAEnvPrimary) {
		t.Fatalf("error must name the source env, got %v", err)
	}

	sharedReqClients = sync.Map{}
	client, err := getSharedReqClient(reqClientOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("non-Codex clients must ignore the Codex CA env, got %v", err)
	}
	if client == nil {
		t.Fatal("expected a client for non-Codex options")
	}
}
