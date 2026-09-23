package service

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
)

// officialCodexAuthPlaneTimeout 是官方 auth 面请求的默认总超时。
const officialCodexAuthPlaneTimeout = 20 * time.Second

// auth-plane 传输层超时，与它替代的 httpclient.buildTransport 保持一致：
// 没有 dial 超时时 Go 会退回零值 net.Dialer（无连接超时），一个黑洞化的代理
// 会让请求挂到 ctx 取消为止——而 agent task 注册的挂死是握着账号锁发生的。
const (
	codexAuthPlaneDialTimeout         = 5 * time.Second
	codexAuthPlaneDialKeepAlive       = 30 * time.Second
	codexAuthPlaneTLSHandshakeTimeout = 5 * time.Second
	codexAuthPlaneResponseHeaderGrace = 15 * time.Second
	codexAuthPlaneIdleConnTimeout     = 90 * time.Second
	codexAuthPlaneMaxIdleConnsPerHost = 10
)

// codexAuthPlaneClients 按「归一化代理 + 生效的 CA bundle + 超时」缓存官方 auth
// 面客户端。这些调用量很小（PAT whoami、agent task 注册），因此不复用共享连接池，
// 也避免把 CA 变体塞进 httpclient 的池键里。sync.Map 本身已保证并发安全。
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
	// proxyurl.Parse 会把 socks5:// 归一成 socks5h://；用归一化值做键，
	// 避免同一有效代理拆出两个连接池。
	trimmed, parsed, parseErr := proxyurl.Parse(proxyURL)
	if parseErr != nil {
		return nil, parseErr
	}
	key := trimmed + "\x00" + timeout.String() + "\x00" + bundle.SourceEnv + "\x00" + bundle.Path
	if cached, ok := codexAuthPlaneClients.Load(key); ok {
		if client, ok := cached.(*http.Client); ok {
			return client, nil
		}
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   codexAuthPlaneDialTimeout,
			KeepAlive: codexAuthPlaneDialKeepAlive,
		}).DialContext,
		TLSHandshakeTimeout:   codexAuthPlaneTLSHandshakeTimeout,
		ResponseHeaderTimeout: codexAuthPlaneResponseHeaderGrace,
		IdleConnTimeout:       codexAuthPlaneIdleConnTimeout,
		MaxIdleConnsPerHost:   codexAuthPlaneMaxIdleConnsPerHost,
		ForceAttemptHTTP2:     true,
	}
	if bundle.Pool != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: bundle.Pool, MinVersion: tls.VersionTLS12}
	}
	if trimmed != "" {
		transport.Proxy = http.ProxyURL(parsed)
	}
	client := &http.Client{Transport: transport, Timeout: timeout}

	actual, _ := codexAuthPlaneClients.LoadOrStore(key, client)
	if cached, ok := actual.(*http.Client); ok {
		return cached, nil
	}
	return client, nil
}
