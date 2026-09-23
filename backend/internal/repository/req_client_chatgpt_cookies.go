package repository

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
)

// ChatGPT Cloudflare 基础设施 cookie 白名单（对齐官方 codex-rs
// http-client/src/chatgpt_cloudflare_cookies.rs）：
// Cloudflare 服务端 cookie + __oailb 路由 cookie。仅这些非敏感基础设施
// cookie 可以进入 jar；账号、会话、鉴权 cookie 一律不存储。
var chatgptCloudflareAllowedCookieNames = map[string]struct{}{
	"__cf_bm":         {},
	"__cflb":          {},
	"__cfruid":        {},
	"__cfseq":         {},
	"__cfwaitingroom": {},
	"__oailb":         {},
	"_cfuvid":         {},
	"cf_clearance":    {},
	"cf_ob_info":      {},
	"cf_use_ob":       {},
}

// isAllowedChatgptCloudflareCookieName 对齐官方 is_allowed_cloudflare_cookie_name：
// 白名单精确匹配 + cf_chl_ 前缀（Cloudflare challenge 流程 cookie）。
func isAllowedChatgptCloudflareCookieName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if _, ok := chatgptCloudflareAllowedCookieNames[name]; ok {
		return true
	}
	return strings.HasPrefix(name, "cf_chl_")
}

// isAllowedChatgptCookieHost 对齐官方 chatgpt_hosts.rs：ChatGPT 域名（含子域）
// 才参与 cookie 存储；wss 与 https 同作用域（由调用方归一化 scheme）。
func isAllowedChatgptCookieHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if port := strings.LastIndex(host, ":"); port >= 0 && !strings.Contains(host[port:], "]") {
		host = host[:port]
	}
	switch host {
	case "chatgpt.com", "chat.openai.com", "chatgpt-staging.com":
		return true
	}
	return strings.HasSuffix(host, ".chatgpt.com") || strings.HasSuffix(host, ".chatgpt-staging.com")
}

// isChatgptCookieURL 仅 https（wss 握手由调用方转换为 https）且主机在白名单内。
func isChatgptCookieURL(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	return isAllowedChatgptCookieHost(u.Host)
}

// chatgptCloudflareCookieJar 是带过滤的 cookie jar：只保留 ChatGPT 域名下
// 白名单内的 Cloudflare 基础设施 cookie，其余 Set-Cookie 静默丢弃。
// 与官方一致，账号/会话/鉴权 cookie 永不进入 jar。
type chatgptCloudflareCookieJar struct {
	mu  sync.Mutex
	jar *cookiejar.Jar
}

// newChatGptCloudflareCookieJar 创建过滤型 cookie jar。
func newChatGptCloudflareCookieJar() (*chatgptCloudflareCookieJar, error) {
	inner, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &chatgptCloudflareCookieJar{jar: inner}, nil
}

// SetCookies 只存储 ChatGPT 主机下白名单内的 cookie。
func (j *chatgptCloudflareCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if j == nil || j.jar == nil || u == nil || !isChatgptCookieURL(u) {
		return
	}
	allowed := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		if isAllowedChatgptCloudflareCookieName(cookie.Name) {
			allowed = append(allowed, cookie)
		}
	}
	if len(allowed) == 0 {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.jar.SetCookies(u, allowed)
}

// Cookies 返回 ChatGPT 主机下已存储的基础设施 cookie；非白名单主机返回空。
func (j *chatgptCloudflareCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if j == nil || j.jar == nil || u == nil || !isChatgptCookieURL(u) {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.jar.Cookies(u)
}
