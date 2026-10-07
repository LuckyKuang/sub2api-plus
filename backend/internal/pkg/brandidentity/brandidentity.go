package brandidentity

import (
	"errors"
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
	return ContainsBrand(name)
}

// IsLocalControlHeaderName reports whether name is consumed by the gateway and
// must never be forwarded to an upstream service.
func IsLocalControlHeaderName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), GrokClientToolCacheHeader)
}

// StripOutboundHeaders removes project names from every header name and value,
// including multi-value/custom headers, plus gateway-local control headers.
// Credentials and User-Agent are preserved for final rejection, so filtering
// cannot silently send an unauthenticated request or substitute an SDK identity.
func StripOutboundHeaders(h http.Header) {
	for name, values := range h {
		if protectedHeader(name) {
			continue
		}
		if IsReservedHeaderName(name) || IsLocalControlHeaderName(name) {
			delete(h, name)
			continue
		}
		for _, value := range values {
			if ContainsBrand(value) {
				delete(h, name)
				break
			}
		}
	}
}

var ErrBrandedOutboundHeader = errors.New("outbound request contains a prohibited project identifier")

func protectedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "x-api-key", "api-key", "x-dsh-auth-token", "user-agent":
		return true
	}
	return false
}

func prohibitedHeaders(h http.Header) bool {
	for name, values := range h {
		if ContainsBrand(name) || IsLocalControlHeaderName(name) {
			return true
		}
		for _, value := range values {
			if ContainsBrand(value) {
				return true
			}
		}
	}
	return false
}

// FilterOutboundRequest filters optional headers. Signed requests and branded
// credentials fail before sending rather than silently breaking authentication.
func FilterOutboundRequest(req *http.Request) error {
	if req == nil {
		return nil
	}
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	if ContainsBrand(host) {
		return ErrBrandedOutboundHeader
	}
	signed := req.URL != nil && (req.URL.Query().Get("X-Amz-SignedHeaders") != "" || req.URL.Query().Get("X-Goog-SignedHeaders") != "")
	for name, values := range req.Header {
		for _, value := range values {
			if strings.EqualFold(name, "Authorization") && (strings.Contains(value, "SignedHeaders=") || strings.HasPrefix(value, "Signature ")) {
				signed = true
			}
			if protectedHeader(name) && ContainsBrand(value) {
				return ErrBrandedOutboundHeader
			}
		}
	}
	if signed && (prohibitedHeaders(req.Header) || prohibitedHeaders(req.Trailer)) {
		return ErrBrandedOutboundHeader
	}
	for name, values := range req.Trailer {
		for _, value := range values {
			if protectedHeader(name) && ContainsBrand(value) {
				return ErrBrandedOutboundHeader
			}
		}
	}
	StripOutboundHeaders(req.Header)
	StripOutboundHeaders(req.Trailer)
	return nil
}

// WrapClient keeps timeout, redirects and cookie handling while protecting each
// transport attempt, including requests produced by redirects.
func WrapClient(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	copy := *client
	copy.Transport = WrapRoundTripper(client.Transport)
	return &copy
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
	request := req.Clone(req.Context())
	if err := FilterOutboundRequest(request); err != nil {
		// RoundTripper owns closing the body even when it rejects before I/O.
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, err
	}
	return t.base.RoundTrip(request)
}

func (t *roundTripper) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (t *roundTripper) Unwrap() http.RoundTripper { return t.base }
