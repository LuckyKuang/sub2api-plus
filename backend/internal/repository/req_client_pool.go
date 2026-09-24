package repository

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
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

// resolveCustomCABundle 返回 OpenAI Codex 出站客户端要用的自定义 CA bundle。
// 只有 opts.OpenAICodexClient 为真时才读取 env：CODEX_CA_CERTIFICATE /
// SSL_CERT_FILE 是官方 Codex 专用名，其他供应商的客户端既不注入这份 bundle，
// 也不会因为一份配置错误的通用 SSL_CERT_FILE 而 fail early。
// 读取与解析状态都在 pkg/openai 的锁内快照，避免与并发解析竞争。
func resolveCustomCABundle(opts reqClientOptions) (openai.CodexCABundle, error) {
	if !opts.OpenAICodexClient {
		return openai.CodexCABundle{}, nil
	}
	bundle, err := openai.CodexCARootPool()
	if err != nil {
		return bundle, fmt.Errorf("custom CA bundle from %s (%s): %w", bundle.SourceEnv, bundle.Path, err)
	}
	return bundle, nil
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
	if caBundle.Pool != nil {
		client.SetTLSClientConfig(&tls.Config{RootCAs: caBundle.Pool, MinVersion: tls.VersionTLS12})
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
	caSource, caPath, caIdentity := "", "", ""
	if opts.OpenAICodexClient {
		if bundle, err := openai.CodexCARootPool(); err == nil {
			caSource, caPath, caIdentity = bundle.SourceEnv, bundle.Path, bundle.Identity
		}
	}
	return fmt.Sprintf("%s|%s|%t|%t|%t|%s|%s|%s",
		strings.TrimSpace(opts.ProxyURL),
		opts.Timeout.String(),
		opts.ForceHTTP2,
		opts.ChatGPTCookieJar,
		opts.OpenAICodexClient,
		caSource,
		caPath,
		caIdentity,
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
