//go:build unit || !integration

package repository

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func writeTestCAPEM(t *testing.T, blockType string) string {
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

func TestBuildCustomCARootPool_AppendsCertificateToSystemRoots(t *testing.T) {
	pool, err := buildCustomCARootPool(writeTestCAPEM(t, "CERTIFICATE"))
	if err != nil {
		t.Fatalf("buildCustomCARootPool: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	// The custom CA must be present while system roots stay available.
	found := false
	for _, subject := range pool.Subjects() {
		if strings.Contains(string(subject), "sub2api-test-ca") {
			found = true
		}
	}
	if !found {
		t.Fatal("custom CA must be appended to the root pool")
	}
	systemPool, sysErr := x509.SystemCertPool()
	if sysErr == nil && systemPool != nil && len(systemPool.Subjects()) > 0 {
		if len(pool.Subjects()) < len(systemPool.Subjects()) {
			t.Fatal("system roots must be preserved alongside the custom CA")
		}
	}
}

// Official normalizes OpenSSL-style TRUSTED CERTIFICATE labels and skips
// non-certificate blocks such as CRLs.
func TestBuildCustomCARootPool_AcceptsTrustedCertificateLabel(t *testing.T) {
	pool, err := buildCustomCARootPool(writeTestCAPEM(t, "TRUSTED CERTIFICATE"))
	if err != nil {
		t.Fatalf("buildCustomCARootPool: %v", err)
	}
	found := false
	for _, subject := range pool.Subjects() {
		if strings.Contains(string(subject), "sub2api-test-ca") {
			found = true
		}
	}
	if !found {
		t.Fatal("TRUSTED CERTIFICATE label must be accepted")
	}
}

func TestBuildCustomCARootPool_RejectsBundleWithoutCertificates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	if err := os.WriteFile(path, []byte("-----BEGIN X509 CRL-----\nnot-a-certificate\n-----END X509 CRL-----\n"), 0o600); err != nil {
		t.Fatalf("write pem: %v", err)
	}
	if _, err := buildCustomCARootPool(path); err == nil {
		t.Fatal("bundle without CERTIFICATE blocks must fail")
	}
}

// CODEX_CA_CERTIFICATE takes precedence over SSL_CERT_FILE, and empty values
// are treated as unset (official custom_ca.rs contract).
func TestLoadCustomCA_EnvPrecedenceAndEmptyUnset(t *testing.T) {
	primary := writeTestCAPEM(t, "CERTIFICATE")
	fallback := writeTestCAPEM(t, "CERTIFICATE")

	t.Setenv(customCAEnvPrimary, primary)
	t.Setenv(customCAEnvFallback, fallback)
	loadCustomCA()
	if customCAErr != nil {
		t.Fatalf("unexpected error: %v", customCAErr)
	}
	if customCAResolved.sourceEnv != customCAEnvPrimary || customCAResolved.path != primary {
		t.Fatalf("resolved = %+v, want primary %s", customCAResolved, primary)
	}

	t.Setenv(customCAEnvPrimary, "  ")
	loadCustomCA()
	if customCAErr != nil {
		t.Fatalf("unexpected error: %v", customCAErr)
	}
	if customCAResolved.sourceEnv != customCAEnvFallback || customCAResolved.path != fallback {
		t.Fatalf("resolved = %+v, want fallback %s", customCAResolved, fallback)
	}

	t.Setenv(customCAEnvPrimary, "")
	t.Setenv(customCAEnvFallback, "")
	loadCustomCA()
	if customCAErr != nil || customCAResolved.pool != nil {
		t.Fatalf("unset env must disable the custom CA: err=%v resolved=%+v", customCAErr, customCAResolved)
	}
}

// A misconfigured bundle fails OpenAI Codex client creation early with a precise
// error instead of silently falling back to system roots. Other providers never
// consult the Codex CA env, so a bad bundle cannot take them down.
func TestGetSharedReqClient_MisconfiguredCAFailsEarly(t *testing.T) {
	sharedReqClients = sync.Map{}
	missing := filepath.Join(t.TempDir(), "missing.pem")
	t.Setenv(customCAEnvPrimary, missing)
	loadCustomCA()
	if customCAErr == nil {
		t.Fatal("missing CA file must record an error")
	}
	_, err := getSharedReqClient(reqClientOptions{Timeout: time.Second, OpenAICodexClient: true})
	if err == nil {
		t.Fatal("OpenAI Codex client creation must fail early on a misconfigured CA bundle")
	}
	if !strings.Contains(err.Error(), customCAEnvPrimary) {
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
