package service

import (
	"errors"
	"net"
	"strings"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/proxyurl"
)

// StrictProxyURL resolves only the account's captured binding. A missing or
// invalid bound proxy is an error; only an unbound account authorizes direct
// egress. Expiry fallback changes the saved binding in its owning workflow and
// is never selected while preparing an in-flight provider request.
func (a *Account) StrictProxyURL() (string, error) {
	if a == nil {
		return "", errors.New("credential owner is unavailable")
	}
	if a.ProxyID == nil {
		return "", nil
	}
	p := a.Proxy
	if *a.ProxyID <= 0 || p == nil || p.ID != *a.ProxyID {
		return "", errors.New("configured proxy snapshot is unavailable")
	}
	if p.Host == "" || strings.TrimSpace(p.Host) != p.Host || strings.ContainsAny(p.Host, " /\\?#@[]\t\r\n") || (strings.Contains(p.Host, ":") && net.ParseIP(p.Host) == nil) || p.Port <= 0 || p.Port > 65535 {
		return "", errors.New("configured proxy address is invalid")
	}
	if (p.Username == "") != (p.Password == "") {
		return "", errors.New("configured proxy authentication is incomplete")
	}
	if strings.TrimSpace(p.Protocol) != p.Protocol {
		return "", errors.New("configured proxy protocol is invalid")
	}
	route, parsed, err := proxyurl.Parse(p.URL())
	if err != nil || parsed == nil {
		return "", errors.New("configured proxy URL is invalid")
	}
	return route, nil
}
