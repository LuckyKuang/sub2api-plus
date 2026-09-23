//go:build unit || !integration

package repository

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/servertiming"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func forceHTTPVersion(t *testing.T, client *req.Client) string {
	t.Helper()
	transport := client.GetTransport()
	field := reflect.ValueOf(transport).Elem().FieldByName("forceHttpVersion")
	require.True(t, field.IsValid(), "forceHttpVersion field not found")
	require.True(t, field.CanAddr(), "forceHttpVersion field not addressable")
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().String()
}

func TestGetSharedReqClient_ForceHTTP2SeparatesCache(t *testing.T) {
	sharedReqClients = sync.Map{}
	base := reqClientOptions{
		ProxyURL: "http://proxy.local:8080",
		Timeout:  time.Second,
	}
	clientDefault, err := getSharedReqClient(base)
	require.NoError(t, err)

	force := base
	force.ForceHTTP2 = true
	clientForce, err := getSharedReqClient(force)
	require.NoError(t, err)

	require.NotSame(t, clientDefault, clientForce)
	require.NotEqual(t, buildReqClientKey(base), buildReqClientKey(force))
}

func TestGetSharedReqClient_ReuseCachedClient(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "http://proxy.local:8080",
		Timeout:  2 * time.Second,
	}
	first, err := getSharedReqClient(opts)
	require.NoError(t, err)
	second, err := getSharedReqClient(opts)
	require.NoError(t, err)
	require.Same(t, first, second)
}

func TestGetSharedReqClient_IgnoresNonClientCache(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: " http://proxy.local:8080 ",
		Timeout:  3 * time.Second,
	}
	key := buildReqClientKey(opts)
	sharedReqClients.Store(key, "invalid")

	client, err := getSharedReqClient(opts)
	require.NoError(t, err)

	require.NotNil(t, client)
	loaded, ok := sharedReqClients.Load(key)
	require.True(t, ok)
	require.IsType(t, "invalid", loaded)
}

func TestGetSharedReqClient_ProxyCacheKey(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "  http://proxy.local:8080  ",
		Timeout:  4 * time.Second,
	}
	client, err := getSharedReqClient(opts)
	require.NoError(t, err)

	require.NotNil(t, client)
	require.Equal(t, "http://proxy.local:8080|4s|false|false|false||", buildReqClientKey(opts))
}

func TestGetSharedReqClient_InvalidProxyURL(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "://missing-scheme",
		Timeout:  time.Second,
	}
	_, err := getSharedReqClient(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid proxy URL")
}

func TestGetSharedReqClient_ProxyURLMissingHost(t *testing.T) {
	sharedReqClients = sync.Map{}
	opts := reqClientOptions{
		ProxyURL: "http://",
		Timeout:  time.Second,
	}
	_, err := getSharedReqClient(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "proxy URL missing host")
}

func TestGetSharedReqClient_CustomCAScopedToOpenAICodexClients(t *testing.T) {
	sharedReqClients = sync.Map{}

	notPEM := filepath.Join(t.TempDir(), "not-a-bundle.pem")
	require.NoError(t, os.WriteFile(notPEM, []byte("this is not a PEM bundle"), 0o600))

	t.Setenv("CODEX_CA_CERTIFICATE", notPEM)
	t.Setenv("SSL_CERT_FILE", "")

	// A configured but unusable bundle must fail fast for OpenAI Codex clients,
	// which is the official custom_ca.rs contract.
	_, err := getSharedReqClient(reqClientOptions{ProxyURL: "http://proxy.local:8080", Timeout: time.Second, OpenAICodexClient: true})
	require.Error(t, err)
	require.Contains(t, err.Error(), "CODEX_CA_CERTIFICATE")

	// Other providers must never consult the Codex CA env: a generic
	// SSL_CERT_FILE cannot take down Gemini / Grok / GeminiCLI clients.
	sharedReqClients = sync.Map{}
	client, err := getSharedReqClient(reqClientOptions{ProxyURL: "http://proxy.local:8080", Timeout: time.Second})
	require.NoError(t, err)
	require.NotNil(t, client)

	// The two client kinds must not share a pool entry.
	require.NotEqual(t,
		buildReqClientKey(reqClientOptions{ProxyURL: "http://proxy.local:8080", Timeout: time.Second, OpenAICodexClient: true}),
		buildReqClientKey(reqClientOptions{ProxyURL: "http://proxy.local:8080", Timeout: time.Second}),
	)
}

func TestGetSharedReqClient_NonOpenAIClientIgnoresConfiguredCustomCA(t *testing.T) {
	sharedReqClients = sync.Map{}

	bundle := codexTestCAPEMPath(t, "CERTIFICATE")
	t.Setenv(openai.CodexCAEnvPrimary, bundle)
	t.Setenv(openai.CodexCAEnvFallback, "")

	pool, err := resolveCustomCABundle(reqClientOptions{OpenAICodexClient: true})
	require.NoError(t, err)
	require.NotNil(t, pool.Pool, "OpenAI Codex clients pick up the configured bundle")
	require.Equal(t, openai.CodexCAEnvPrimary, pool.SourceEnv)
	require.Equal(t, bundle, pool.Path)

	pool, err = resolveCustomCABundle(reqClientOptions{})
	require.NoError(t, err)
	require.Nil(t, pool.Pool, "non-Codex clients never read the Codex CA env")
	require.Empty(t, pool.SourceEnv)
	require.Empty(t, pool.Path)
}

func TestCreateOpenAIRawAndCredentialReqClients_Timeout120Seconds(t *testing.T) {
	sharedReqClients = sync.Map{}
	raw, err := createOpenAIRawReqClient("http://proxy.local:8080")
	require.NoError(t, err)
	require.Equal(t, 120*time.Second, raw.GetClient().Timeout)

	sharedReqClients = sync.Map{}
	credential, err := createOpenAICredentialReqClient("http://proxy.local:8080")
	require.NoError(t, err)
	require.Equal(t, 120*time.Second, credential.GetClient().Timeout)
	require.NotSame(t, raw, credential, "raw and credential clients must not share a cookie jar")
}

func TestCreateGeminiReqClient_ForceHTTP2Disabled(t *testing.T) {
	sharedReqClients = sync.Map{}
	client, err := createGeminiReqClient("http://proxy.local:8080")
	require.NoError(t, err)
	require.Equal(t, "", forceHTTPVersion(t, client))
}

func TestInstrumentReqClientRecordsDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	collector := servertiming.New(time.Now())
	ctx := servertiming.WithCollector(context.Background(), collector)
	client := instrumentReqClient(req.C())
	response, err := client.R().SetContext(ctx).Get(server.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, response.StatusCode)

	header := collector.HeaderValue(time.Now(), "bypass")
	require.True(t, strings.Contains(header, "dep_http;dur="), header)
}
