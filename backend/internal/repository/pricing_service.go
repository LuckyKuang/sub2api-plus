package repository

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/httpclient"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/LuckyKuang/sub2api-plus/internal/util/urlvalidator"
)

type pricingRemoteClient struct {
	httpClient *http.Client
}

// pricingRemoteClientError 代理初始化失败时的错误占位客户端
// 所有请求直接返回初始化错误，禁止回退到直连
type pricingRemoteClientError struct {
	err error
}

func (c *pricingRemoteClientError) FetchPricingJSON(_ context.Context, _ string, _ int64) ([]byte, error) {
	return nil, c.err
}

// NewPricingRemoteClient 创建定价数据远程客户端。
//
// Pricing is financial input, so redirects use the dedicated pricing allowlist
// even when the broader upstream allowlist remains in compatibility mode.
// proxyURL 为空时直连，支持 http/https/socks5/socks5h 协议。
// 代理配置失败时行为由 allowDirectOnProxyError 控制：
//   - false（默认）：返回错误占位客户端，禁止回退到直连
//   - true：回退到直连（仅限管理员显式开启）
func NewPricingRemoteClient(cfg *config.Config) service.PricingRemoteClient {
	proxyURL := ""
	allowDirectOnProxyError := false
	if cfg != nil {
		proxyURL = cfg.Update.ProxyURL
		allowDirectOnProxyError = cfg.Security.ProxyFallback.AllowDirectOnError
	}

	// 安全说明：httpclient.GetClient 的错误链（url.Parse / proxyutil）不含明文代理凭据，
	// 但仍通过 slog 仅在服务端日志记录，不会暴露给 HTTP 响应。
	sharedClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  30 * time.Second,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy client init failed, all requests will fail", "service", "pricing", "error", err)
			return &pricingRemoteClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		sharedClient = &http.Client{Timeout: 30 * time.Second}
	}
	clientCopy := *sharedClient
	clientCopy.CheckRedirect = pricingRedirectChecker(cfg)
	return &pricingRemoteClient{
		httpClient: &clientCopy,
	}
}

func pricingRedirectChecker(cfg *config.Config) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, _ []*http.Request) error {
		if cfg == nil {
			return fmt.Errorf("pricing redirect blocked: configuration is unavailable")
		}
		_, err := urlvalidator.ValidateHTTPSURL(request.URL.String(), urlvalidator.ValidationOptions{
			AllowedHosts:     cfg.Security.URLAllowlist.PricingHosts,
			RequireAllowlist: true,
			AllowPrivate:     false,
		})
		if err != nil {
			return fmt.Errorf("pricing redirect blocked: %w", err)
		}
		return nil
	}
}

func (c *pricingRemoteClient) FetchPricingJSON(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("pricing response size limit must be positive")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("pricing response exceeds %d bytes", maxBytes)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("pricing response exceeds %d bytes", maxBytes)
	}
	return body, nil
}
