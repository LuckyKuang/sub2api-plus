package brandidentity

import (
	"net/http"
	"strings"
)

const (
	brandToken                = "sub2api"
	GrokClientToolCacheHeader = "X-Grok-Client-Tool-Cache"
)

// ContainsBrand reports whether s includes the project protocol token.
func ContainsBrand(s string) bool {
	return strings.Contains(strings.ToLower(s), brandToken)
}

// IsReservedHeaderName reports whether name is a project-specific protocol header.
func IsReservedHeaderName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "x-sub2api-")
}

// IsLocalControlHeaderName reports whether name is consumed by the gateway and
// must never be forwarded to an upstream service.
func IsLocalControlHeaderName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), GrokClientToolCacheHeader)
}

// StripOutboundHeaders removes reserved project headers and branded values from
// system identity headers. Arbitrary custom header values remain untouched.
func StripOutboundHeaders(h http.Header) {
	for name, values := range h {
		if IsReservedHeaderName(name) || IsLocalControlHeaderName(name) {
			delete(h, name)
			continue
		}
		switch strings.ToLower(name) {
		case "user-agent", "referer", "origin":
			for _, value := range values {
				if ContainsBrand(value) {
					delete(h, name)
					break
				}
			}
		}
	}
}

func FilterOutboundRequest(req *http.Request) {
	if req != nil {
		StripOutboundHeaders(req.Header)
	}
}

type roundTripper struct {
	base http.RoundTripper
}

func WrapRoundTripper(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(*roundTripper); ok {
		return base
	}
	return &roundTripper{base: base}
}

func (t *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	FilterOutboundRequest(req)
	return t.base.RoundTrip(req)
}
