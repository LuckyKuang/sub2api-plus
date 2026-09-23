//go:build unit

package service

import (
	"net/http"
	"testing"
	"time"

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
