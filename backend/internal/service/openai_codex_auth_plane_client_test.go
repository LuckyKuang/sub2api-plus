//go:build unit

package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// 官方 auth 面客户端必须带齐传输层超时：缺 dial 超时时 Go 会退回零值
// net.Dialer（无连接超时），黑洞化代理会把请求挂到 ctx 取消，而 agent task
// 注册的挂死是握着账号锁发生的。
func TestCodexAuthPlaneHTTPClient_TransportHasCompleteTimeouts(t *testing.T) {
	client, err := codexAuthPlaneHTTPClient("", 0)
	require.NoError(t, err)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected an *http.Transport")

	require.NotNil(t, transport.DialContext, "a nil DialContext means no connect timeout")

	require.Equal(t, 5*time.Second, transport.TLSHandshakeTimeout)
	require.Equal(t, 15*time.Second, transport.ResponseHeaderTimeout,
		"a missing response-header timeout lets a silent upstream hang the request")
	require.Equal(t, 90*time.Second, transport.IdleConnTimeout)
	require.Equal(t, 10, transport.MaxIdleConnsPerHost)
	// The default request timeout applies when the caller passes none.
	require.Equal(t, 20*time.Second, client.Timeout)
}

// socks5:// and socks5h:// describe the same effective proxy, so they must share
// one client and therefore one connection pool.
func TestCodexAuthPlaneHTTPClient_NormalizedProxySharesClient(t *testing.T) {
	first, err := codexAuthPlaneHTTPClient("socks5://127.0.0.1:1080", 0)
	require.NoError(t, err)
	second, err := codexAuthPlaneHTTPClient("socks5h://127.0.0.1:1080", 0)
	require.NoError(t, err)
	require.Same(t, first, second, "socks5 and socks5h must not mint two pools")

	other, err := codexAuthPlaneHTTPClient("http://127.0.0.1:8080", 0)
	require.NoError(t, err)
	require.NotSame(t, first, other, "different proxies must not share a client")
}

func TestCodexAuthPlaneHTTPClient_RotatedCAUsesNewClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, authPlaneTestCAPEM(t), 0o600))
	t.Setenv(openai.CodexCAEnvPrimary, path)
	t.Setenv(openai.CodexCAEnvFallback, "")

	first, err := codexAuthPlaneHTTPClient("", 0)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(path, authPlaneTestCAPEM(t), 0o600))
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(path, future, future))

	second, err := codexAuthPlaneHTTPClient("", 0)
	require.NoError(t, err)
	require.NotSame(t, first, second, "a rotated CA bundle must not reuse the previous auth-plane client")
}

func authPlaneTestCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sub2api-auth-plane-ca-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
