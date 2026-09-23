package service

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
)

// officialCodexAuthPlaneTimeout 是官方 auth 面请求的默认总超时。
const officialCodexAuthPlaneTimeout = 20 * time.Second

// codexAuthPlaneClients 按「代理 + 生效的 CA bundle + 超时」缓存官方 auth 面客户端。
// 这些调用量很小（PAT whoami、agent task 注册），因此不复用共享连接池，也避免
// 把 CA 变体塞进 httpclient 的池键里。sync.Map 本身已保证并发安全。
var codexAuthPlaneClients sync.Map

// codexAuthPlaneHTTPClient 构造官方 Codex auth 面使用的 HTTP 客户端：应用
// CODEX_CA_CERTIFICATE / SSL_CERT_FILE 自定义根证书（与凭据面同一策略），
// 并绑定代理。bundle 配置错误时 fail early，与官方 custom_ca.rs 一致。
func codexAuthPlaneHTTPClient(proxyURL string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = officialCodexAuthPlaneTimeout
	}
	bundle, err := openai.CodexCARootPool()
	if err != nil {
		return nil, err
	}
	key := proxyURL + "\x00" + timeout.String() + "\x00" + bundle.SourceEnv + "\x00" + bundle.Path
	if cached, ok := codexAuthPlaneClients.Load(key); ok {
		if client, ok := cached.(*http.Client); ok {
			return client, nil
		}
	}

	transport := &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	if bundle.Pool != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: bundle.Pool, MinVersion: tls.VersionTLS12}
	}
	if trimmed, parsed, parseErr := proxyurl.Parse(proxyURL); parseErr != nil {
		return nil, parseErr
	} else if trimmed != "" {
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{Transport: transport, Timeout: timeout}

	actual, _ := codexAuthPlaneClients.LoadOrStore(key, client)
	if cached, ok := actual.(*http.Client); ok {
		return cached, nil
	}
	return client, nil
}
