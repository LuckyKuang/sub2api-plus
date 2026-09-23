package repository

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/servertiming"

	"github.com/imroc/req/v3"
)

// reqClientOptions 定义 req 客户端的构建参数
type reqClientOptions struct {
	ProxyURL   string        // 代理 URL（支持 http/https/socks5）
	Timeout    time.Duration // 请求超时时间
	ForceHTTP2 bool          // 是否强制使用 HTTP/2
	// ChatGPTCookieJar 启用对齐官方 with_chatgpt_cloudflare_cookie_store 的
	// 过滤型 cookie jar：仅保留 ChatGPT 域名下白名单内的 Cloudflare 基础设施
	// cookie（含 __oailb 路由 cookie）。OAuth 凭据面（刷新/吊销/enrich/wham）
	// 启用；换票/device 等 raw 客户端不启用（官方 raw client 同样无 cookie）。
	// jar 按 options 键（含代理）池化，天然按代理隔离，避免跨出口泄漏。
	ChatGPTCookieJar bool
	// OpenAICodexClient 标记官方 Codex 出站客户端（raw auth / 凭据面 /
	// chatgpt.com backend-api）。自定义 CA（CODEX_CA_CERTIFICATE /
	// SSL_CERT_FILE）只对这类客户端生效：这两个 env 是官方 Codex 专用名，
	// 不应把 Gemini / Grok / GeminiCLI 等其他供应商也拖进同一份 bundle，
	// 更不应因一份通用 SSL_CERT_FILE 配置错误而让全部供应商一起 fail early。
	OpenAICodexClient bool
}

// sharedReqClients 存储按配置参数缓存的 req 客户端实例
//
// 性能优化说明：
// 原实现在每次 OAuth 刷新时都创建新的 req.Client：
// 1. claude_oauth_service.go: 每次刷新创建新客户端
// 2. openai_oauth_service.go: 每次刷新创建新客户端
// 3. gemini_oauth_client.go: 每次刷新创建新客户端
//
// 新实现使用 sync.Map 缓存客户端：
// 1. 相同配置（代理+超时+模拟设置）复用同一客户端
// 2. 复用底层连接池，减少 TLS 握手开销
// 3. LoadOrStore 保证并发安全，避免重复创建
var sharedReqClients sync.Map

// 自定义 CA 环境变量（对齐官方 codex-rs http-client/src/custom_ca.rs）：
// CODEX_CA_CERTIFICATE 优先于 SSL_CERT_FILE，空值视为未设置。配置的 PEM
// bundle 追加到系统根证书（官方 reqwest/rustls 路径同样保留平台根）；bundle
// 不可读或不含证书块时 fail early 返回精确错误，不静默回退系统根。
const (
	customCAEnvPrimary  = "CODEX_CA_CERTIFICATE"
	customCAEnvFallback = "SSL_CERT_FILE"
)

const customCAHint = "If you set CODEX_CA_CERTIFICATE or SSL_CERT_FILE, ensure it points to a PEM file containing one or more CERTIFICATE blocks, or unset it to use system roots."

type customCABundle struct {
	sourceEnv string
	path      string
	pool      *x509.CertPool
}

var (
	customCAMu        sync.Mutex
	customCASignature string
	customCAResolved  customCABundle
	customCAErr       error
)

// loadCustomCA 解析自定义 CA bundle 并按 env 签名缓存：env 不变时零成本，
// env 变化（测试）时重新解析。bundle 配置错误记录在 customCAErr，由
// resolveCustomCABundle 暴露给调用方 fail early。
func loadCustomCA() {
	signature := strings.TrimSpace(os.Getenv(customCAEnvPrimary)) + "\x00" + strings.TrimSpace(os.Getenv(customCAEnvFallback))
	customCAMu.Lock()
	defer customCAMu.Unlock()
	if signature == customCASignature {
		return
	}
	customCASignature = signature
	customCAResolved = customCABundle{}
	customCAErr = nil
	for _, env := range []string{customCAEnvPrimary, customCAEnvFallback} {
		path := strings.TrimSpace(os.Getenv(env))
		if path == "" {
			continue
		}
		customCAResolved = customCABundle{sourceEnv: env, path: path}
		pool, err := buildCustomCARootPool(path)
		if err != nil {
			customCAErr = err
			return
		}
		customCAResolved.pool = pool
		return
	}
}

// resolveCustomCABundle 返回 OpenAI Codex 出站客户端要用的自定义 CA bundle。
// 只有 opts.OpenAICodexClient 为真时才读取 env：CODEX_CA_CERTIFICATE /
// SSL_CERT_FILE 是官方 Codex 专用名，其他供应商的客户端既不注入这份 bundle，
// 也不会因为一份配置错误的通用 SSL_CERT_FILE 而 fail early。
// 读取与解析状态都在 customCAMu 下快照，避免与并发 loadCustomCA 竞争。
func resolveCustomCABundle(opts reqClientOptions) (customCABundle, error) {
	if !opts.OpenAICodexClient {
		return customCABundle{}, nil
	}
	loadCustomCA()
	customCAMu.Lock()
	defer customCAMu.Unlock()
	if customCAErr != nil {
		return customCAResolved, fmt.Errorf("custom CA bundle from %s (%s): %w", customCAResolved.sourceEnv, customCAResolved.path, customCAErr)
	}
	return customCAResolved, nil
}

// buildCustomCARootPool 把 bundle 中每个可解析的证书块追加到系统根证书池。
// 接受标准 CERTIFICATE 与 OpenSSL TRUSTED CERTIFICATE 标签（对齐官方的 PEM
// 变体规范化），跳过 CRL 等非证书块。
func buildCustomCARootPool(path string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate file %s: %v. %s", path, err, customCAHint)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	added := 0
	for {
		var block *pem.Block
		block, pemBytes = pem.Decode(pemBytes)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" && block.Type != "TRUSTED CERTIFICATE" {
			continue
		}
		certs, parseErr := x509.ParseCertificates(block.Bytes)
		if parseErr != nil || len(certs) == 0 {
			continue
		}
		for _, cert := range certs {
			pool.AddCert(cert)
			added++
		}
	}
	if added == 0 {
		return nil, fmt.Errorf("failed to load CA certificates from %s: no CERTIFICATE block found. %s", path, customCAHint)
	}
	return pool, nil
}

// getSharedReqClient 获取共享的 req 客户端实例
// 性能优化：相同配置复用同一客户端，避免重复创建
func getSharedReqClient(opts reqClientOptions) (*req.Client, error) {
	key := buildReqClientKey(opts)
	if cached, ok := sharedReqClients.Load(key); ok {
		if c, ok := cached.(*req.Client); ok {
			return c, nil
		}
	}

	client := req.C().SetTimeout(opts.Timeout)
	if opts.ForceHTTP2 {
		client = client.EnableForceHTTP2()
	}
	trimmed, _, err := proxyurl.Parse(opts.ProxyURL)
	if err != nil {
		return nil, err
	}
	if trimmed != "" {
		client.SetProxyURL(trimmed)
	}
	caBundle, caErr := resolveCustomCABundle(opts)
	if caErr != nil {
		return nil, caErr
	}
	if caBundle.pool != nil {
		client.SetTLSClientConfig(&tls.Config{RootCAs: caBundle.pool, MinVersion: tls.VersionTLS12})
	}
	if opts.ChatGPTCookieJar {
		jar, jarErr := newChatGptCloudflareCookieJar()
		if jarErr != nil {
			return nil, jarErr
		}
		client.SetCookieJar(jar)
	}
	client = instrumentReqClient(client)

	actual, _ := sharedReqClients.LoadOrStore(key, client)
	if c, ok := actual.(*req.Client); ok {
		return c, nil
	}
	return client, nil
}

func instrumentReqClient(client *req.Client) *req.Client {
	if client == nil {
		return nil
	}
	client.GetTransport().WrapRoundTripFunc(func(rt http.RoundTripper) req.HttpRoundTripFunc {
		filtered := brandidentity.WrapRoundTripper(servertiming.WrapRoundTripper(rt))
		return filtered.RoundTrip
	})
	return client
}

func buildReqClientKey(opts reqClientOptions) string {
	// OpenAI Codex 客户端按生效的 CA bundle 分池（非 Codex 客户端不读 env，
	// 固定为空），避免同一份配置在不同 bundle 下复用同一客户端。
	caSource, caPath := "", ""
	if opts.OpenAICodexClient {
		loadCustomCA()
		customCAMu.Lock()
		caSource, caPath = customCAResolved.sourceEnv, customCAResolved.path
		customCAMu.Unlock()
	}
	return fmt.Sprintf("%s|%s|%t|%t|%t|%s|%s",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.ForceHTTP2,
		opts.ChatGPTCookieJar,
		opts.OpenAICodexClient,
		caSource,
		caPath,
	)
}

// CreatePrivacyReqClient creates the shared HTTP client for ChatGPT
// backend-api auxiliary calls (accounts check, subscription enrich, privacy
// settings, WHAM usage/credits). Official Codex talks to these endpoints with
// its regular HTTP client, so no browser TLS fingerprint is applied; identity
// headers come from the resolved outbound snapshot per request. The client
// retains the allowlisted ChatGPT Cloudflare infrastructure cookies, aligned
// with the official with_chatgpt_cloudflare_cookie_store behavior.
func CreatePrivacyReqClient(proxyURL string) (*req.Client, error) {
	return getSharedReqClient(reqClientOptions{
		ProxyURL:          proxyURL,
		Timeout:           30 * time.Second,
		ChatGPTCookieJar:  true,
		OpenAICodexClient: true,
	})
}
