package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/sync/singleflight"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

// Negotiated zstd request compression for the Grok sampler JSON routes.
//
// The frozen grok-build source is the authority:
//   - crates/codegen/xai-grok-sampler/src/request_compression.rs owns the
//     64 KiB threshold, zstd level 3 and the 1 MiB offload boundary.
//   - crates/codegen/xai-grok-shell/src/util/config/resolve/request_compression.rs
//     only enables compression toward the one trusted proxy origin whose
//     `/v1/settings` advertised zstd in `accept_request_encodings`, and keeps
//     the operator kill switch.
//   - crates/codegen/xai-grok-cloud-config/src/settings_fetch.rs owns the
//     capability request shape.
//
// The gateway keeps the same semantics without copying the Rust runtime: the
// capability is observed through the existing account/proxy HTTP path with the
// same-owner identity snapshot and cached process-locally with a bounded TTL.
const (
	// grokRequestCompressionMinBytes mirrors MIN_COMPRESS_BYTES.
	grokRequestCompressionMinBytes = 64 * 1024
	// grokRequestCompressionOffloadBytes mirrors OFFLOAD_COMPRESS_BYTES. From
	// here up the encode leaves the request fast path and is bounded by a
	// limiter instead of running unbounded on the request goroutine.
	grokRequestCompressionOffloadBytes = 1024 * 1024
	// grokRequestCompressionZstdLevel mirrors ZSTD_LEVEL (fast, good JSON
	// ratio, 2 MiB window that stays inside the proxy decoder bound).
	grokRequestCompressionZstdLevel = 3
	// grokRequestCompressionWindowSize is the explicit 2 MiB window of level 3.
	grokRequestCompressionWindowSize = 2 << 20
	// grokRequestCompressionMaxLargeEncodes bounds concurrent multi-megabyte
	// encodes. Normal Go scheduling still runs them; this only caps how many
	// may be in flight for one process.
	grokRequestCompressionMaxLargeEncodes = 4

	// grokRequestCompressionCapabilityTTL bounds reuse of one `/v1/settings`
	// observation (positive or negative). There is no existing shared cache for
	// trusted-proxy capabilities, so this stays process-local, exactly as the
	// design allows. Nothing persistent and no background refresh is created.
	grokRequestCompressionCapabilityTTL = 5 * time.Minute
	// grokRequestCompressionSettingsTimeout bounds one capability probe.
	grokRequestCompressionSettingsTimeout = 5 * time.Second
	// grokRequestCompressionSettingsMaxBytes bounds the capability response.
	grokRequestCompressionSettingsMaxBytes = 1 << 20

	// Fixed structured-log stages/codes. They never include request content.
	grokRequestCompressionStageCapability = "capability"
	grokRequestCompressionStageEncode     = "encode"
	grokRequestCompressionCodeFetch       = "settings_fetch_failed"
	grokRequestCompressionCodeStatus      = "settings_status_not_ok"
	grokRequestCompressionCodeShape       = "settings_shape_unknown"
	grokRequestCompressionCodeEncode      = "zstd_encode_failed"
	grokRequestCompressionCodeRebuild     = "plain_body_unavailable"
)

var grokRequestCompressionLargeEncodeLimiter = make(chan struct{}, grokRequestCompressionMaxLargeEncodes)

// grokZstdEncoderPool reuses level-3 encoders. Each pooled encoder is used by
// one goroutine at a time; EncodeAll is safe for that sequential reuse.
var grokZstdEncoderPool = sync.Pool{
	New: func() any {
		encoder, err := zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(grokRequestCompressionZstdLevel)),
			zstd.WithEncoderConcurrency(1),
			zstd.WithWindowSize(grokRequestCompressionWindowSize),
		)
		if err != nil {
			return nil
		}
		return encoder
	},
}

// ---------------------------------------------------------------------------
// Capability cache (process-local, bounded, singleflight-refreshed)
// ---------------------------------------------------------------------------

type grokRequestCompressionCacheKey struct {
	// target is the normalized scheme://host:effective-port/base-path of the
	// exact sampler destination.
	target string
	// owner is the credential-owning account id.
	owner int64
	// proxy identifies the egress configuration; capability never crosses it.
	proxy string
}

func (k grokRequestCompressionCacheKey) flightKey() string {
	return fmt.Sprintf("%s|%d|%s", k.target, k.owner, k.proxy)
}

type grokRequestCompressionCacheEntry struct {
	advertised bool
	expiresAt  time.Time
}

var grokRequestCompressionCapabilityCache = struct {
	mu      sync.Mutex
	entries map[grokRequestCompressionCacheKey]grokRequestCompressionCacheEntry
	flight  singleflight.Group
}{entries: make(map[grokRequestCompressionCacheKey]grokRequestCompressionCacheEntry)}

func grokRequestCompressionCacheLookup(key grokRequestCompressionCacheKey, now time.Time) (bool, bool) {
	grokRequestCompressionCapabilityCache.mu.Lock()
	defer grokRequestCompressionCapabilityCache.mu.Unlock()
	entry, ok := grokRequestCompressionCapabilityCache.entries[key]
	if !ok || !now.Before(entry.expiresAt) {
		if ok {
			delete(grokRequestCompressionCapabilityCache.entries, key)
		}
		return false, false
	}
	return entry.advertised, true
}

func grokRequestCompressionCacheStore(key grokRequestCompressionCacheKey, advertised bool, now time.Time) {
	grokRequestCompressionCapabilityCache.mu.Lock()
	defer grokRequestCompressionCapabilityCache.mu.Unlock()
	// Bound the map: expired entries are dropped on every write, and a hard cap
	// keeps a long-lived process from retaining unbounded owner/target pairs.
	for existing, entry := range grokRequestCompressionCapabilityCache.entries {
		if !now.Before(entry.expiresAt) {
			delete(grokRequestCompressionCapabilityCache.entries, existing)
		}
	}
	if len(grokRequestCompressionCapabilityCache.entries) >= 1024 {
		for existing := range grokRequestCompressionCapabilityCache.entries {
			delete(grokRequestCompressionCapabilityCache.entries, existing)
			break
		}
	}
	grokRequestCompressionCapabilityCache.entries[key] = grokRequestCompressionCacheEntry{
		advertised: advertised,
		expiresAt:  now.Add(grokRequestCompressionCapabilityTTL),
	}
}

// normalizeGrokRequestCompressionTarget returns the exact capability identity
// (scheme, host, effective port and base path) plus the `/v1/settings` URL for
// that same target. The capability key always states the effective port so
// `host` and `host:443` cannot hold two different capability states; the probe
// URL keeps the authority spelling of the validated base URL. Anything that
// cannot be normalized keeps plain JSON.
func normalizeGrokRequestCompressionTarget(baseURL string) (origin, settingsURL string, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "https" && scheme != "http" {
		return "", "", false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", "", false
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	basePath := strings.TrimRight(parsed.EscapedPath(), "/")
	origin = scheme + "://" + host + ":" + port + basePath
	settingsURL = scheme + "://" + parsed.Host + basePath + "/settings"
	return origin, settingsURL, true
}

// applyGrokRequestCompression rewrites a sampler request into its negotiated
// wire form. It is the single owner of the request-owned compression
// declaration: every path calls it, and it always drops a stale declaration
// before deciding. Callers must have completed request auditing and the final
// model/tool/session adaptation, because the encoded bytes are the exact final
// JSON body.
func (s *OpenAIGatewayService) applyGrokRequestCompression(req *http.Request, account *Account, body []byte, settings ...*SettingService) {
	if req == nil {
		return
	}
	// Owned declaration: never inherit one from an override, inbound field or
	// earlier rebuild.
	req.Header.Del("Content-Encoding")
	if s == nil || account == nil || len(body) == 0 {
		return
	}
	if !s.grokRequestCompressionEnabled() {
		return
	}
	if len(body) < grokRequestCompressionMinBytes {
		return
	}
	// Carry the credential owner's Authorization to the capability probe
	// without creating a second credential lookup.
	fetchCtx := withGrokRequestCompressionAuthorization(req.Context(), req.Header.Get("Authorization"))
	if !s.grokRequestCompressionCapabilityAdvertised(fetchCtx, account, settings...) {
		return
	}
	encoded, err := encodeGrokJSONRequest(req.Context(), body)
	if err != nil {
		slog.Warn("grok_request_compression_failed",
			"stage", grokRequestCompressionStageEncode,
			"code", grokRequestCompressionCodeEncode,
			"pre_compression_bytes", len(body),
			"post_compression_bytes", 0,
		)
		return
	}
	slog.Info("grok_request_compression_applied",
		"stage", grokRequestCompressionStageEncode,
		"pre_compression_bytes", len(body),
		"post_compression_bytes", len(encoded),
	)
	req.Header.Set("Content-Encoding", "zstd")
	req.Body = io.NopCloser(bytes.NewReader(encoded))
	req.ContentLength = int64(len(encoded))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(encoded)), nil
	}
	// Keep the final uncompressed JSON beside the request so a destination
	// change can rebuild a plain body without re-running adaptation.
	*req = *req.WithContext(withGrokPlainJSONBody(req.Context(), body))
}

func (s *OpenAIGatewayService) grokRequestCompressionEnabled() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.Gateway.Grok.GrokRequestCompressionEnabled
}

// grokRequestCompressionCapabilityAdvertised reports whether the exact sampler
// destination advertised zstd. Unknown, withdrawn, absent, wrong-shape and
// refresh-failure states all resolve to false (plain JSON), and the lookup
// never fails the inference.
func (s *OpenAIGatewayService) grokRequestCompressionCapabilityAdvertised(ctx context.Context, account *Account, settings ...*SettingService) bool {
	if s == nil || account == nil {
		return false
	}
	baseURL, err := grokValidatedSamplerBaseURL(account, s.cfg, settings...)
	if err != nil {
		return false
	}
	origin, settingsURL, ok := normalizeGrokRequestCompressionTarget(baseURL)
	if !ok {
		return false
	}
	// Only the trusted Grok CLI proxy exposes the capability endpoint. Every
	// other destination (api.x.ai, regional hosts, custom relays) stays plain.
	if !isGrokCLIProxyTarget(origin) {
		return false
	}
	key := grokRequestCompressionCacheKey{target: origin, owner: account.ID, proxy: grokRequestCompressionProxyKey(account)}
	now := time.Now()
	if advertised, cached := grokRequestCompressionCacheLookup(key, now); cached {
		return advertised
	}
	value, _, _ := grokRequestCompressionCapabilityCache.flight.Do(key.flightKey(), func() (any, error) {
		refreshAt := time.Now()
		if advertised, cached := grokRequestCompressionCacheLookup(key, refreshAt); cached {
			return advertised, nil
		}
		advertised := s.fetchGrokRequestCompressionCapability(ctx, account, settingsURL)
		// Negative observations are cached too: it keeps a dead or unhelpful
		// `/v1/settings` from being probed on every request while staying
		// bounded by the same TTL.
		grokRequestCompressionCacheStore(key, advertised, refreshAt)
		return advertised, nil
	})
	advertised, _ := value.(bool)
	return advertised
}

func grokRequestCompressionProxyKey(account *Account) string {
	if account == nil || account.ProxyID == nil || account.Proxy == nil {
		return ""
	}
	return account.Proxy.URL()
}

// fetchGrokRequestCompressionCapability performs the capability probe on the
// existing account HTTP path: same proxy, same TLS/transport policy and the
// same-owner identity snapshot. It never creates an independent client with a
// default User-Agent, and it never touches sampler declarations.
func (s *OpenAIGatewayService) fetchGrokRequestCompressionCapability(ctx context.Context, account *Account, settingsURL string) bool {
	if s == nil || s.httpUpstream == nil || account == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fetchCtx, cancel := context.WithTimeout(ctx, grokRequestCompressionSettingsTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, settingsURL, nil)
	if err != nil {
		slog.Warn("grok_request_compression_capability_unavailable",
			"stage", grokRequestCompressionStageCapability,
			"code", grokRequestCompressionCodeFetch)
		return false
	}
	// The credential owner's bearer is the same one the sampler request uses.
	if authorization, ok := ctx.Value(grokRequestCompressionAuthorizationKey{}).(string); ok && strings.TrimSpace(authorization) != "" {
		req.Header.Set("Authorization", strings.TrimSpace(authorization))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-XAI-Token-Auth", xai.CLITokenAuth)
	if userID := strings.TrimSpace(account.GetCredential("sub")); userID != "" {
		req.Header.Set("x-userid", userID)
	}
	if email := strings.TrimSpace(account.GetCredential("email")); email != "" {
		req.Header.Set("x-email", email)
	}
	// Same-owner identity snapshot and the Grok transport profile, applied by
	// the shared account preparation boundary.
	req = prepareAccountOutboundRequest(req, account)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		slog.Warn("grok_request_compression_capability_unavailable",
			"stage", grokRequestCompressionStageCapability,
			"code", grokRequestCompressionCodeFetch)
		return false
	}
	if resp == nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		slog.Warn("grok_request_compression_capability_unavailable",
			"stage", grokRequestCompressionStageCapability,
			"code", grokRequestCompressionCodeStatus,
			"status_code", resp.StatusCode)
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, grokRequestCompressionSettingsMaxBytes))
	if err != nil {
		slog.Warn("grok_request_compression_capability_unavailable",
			"stage", grokRequestCompressionStageCapability,
			"code", grokRequestCompressionCodeFetch)
		return false
	}
	var payload struct {
		AcceptRequestEncodings []string `json:"accept_request_encodings"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("grok_request_compression_capability_unavailable",
			"stage", grokRequestCompressionStageCapability,
			"code", grokRequestCompressionCodeShape,
			"response_bytes", len(body))
		return false
	}
	for _, encoding := range payload.AcceptRequestEncodings {
		// The frozen enum only accepts the exact token `zstd`; anything else
		// deserializes to its forward-compat Unknown variant.
		if strings.TrimSpace(encoding) == "zstd" {
			return true
		}
	}
	return false
}

// encodeGrokJSONRequest compresses the final request JSON at level 3. Bodies at
// or above the offload boundary acquire a bounded encode slot first; the encode
// itself still runs on the caller's own Go goroutine.
func encodeGrokJSONRequest(ctx context.Context, body []byte) ([]byte, error) {
	if len(body) >= grokRequestCompressionOffloadBytes {
		select {
		case grokRequestCompressionLargeEncodeLimiter <- struct{}{}:
			defer func() { <-grokRequestCompressionLargeEncodeLimiter }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// Indirection so the fixed stage/code fallback path stays testable without
	// depending on a zstd failure mode that does not otherwise occur.
	return grokJSONBodyEncoder(body)
}

var grokJSONBodyEncoder = zstdEncodeGrokJSONBody

func zstdEncodeGrokJSONBody(body []byte) ([]byte, error) {
	encoder, _ := grokZstdEncoderPool.Get().(*zstd.Encoder)
	if encoder == nil {
		return nil, fmt.Errorf("zstd encoder unavailable")
	}
	defer grokZstdEncoderPool.Put(encoder)
	return encoder.EncodeAll(body, make([]byte, 0, len(body)/2+64)), nil
}

// ---------------------------------------------------------------------------
// Plain-body rebuild for destination changes
// ---------------------------------------------------------------------------

// grokPlainJSONBodyContextKey carries the final uncompressed JSON of a
// compression-owned request so the shared transport can rebuild a plain body
// when the destination changes, without re-running model/tool/session
// adaptation.
type grokPlainJSONBodyContextKey struct{}

// grokRequestCompressionAuthorizationKey carries the credential owner's
// Authorization header value from the sampler request to the capability probe.
type grokRequestCompressionAuthorizationKey struct{}

// WithGrokPlainJSONBody records the final uncompressed JSON for a request the
// gateway has encoded. It is the producing half of the request-compression
// contract between the gateway builders and the shared transport: the transport
// consumes it through RebuildGrokPlainRequest when the destination changes
// (api.x.ai compatibility fallback, cross-origin redirect).
func WithGrokPlainJSONBody(ctx context.Context, body []byte) context.Context {
	return withGrokPlainJSONBody(ctx, body)
}

func withGrokPlainJSONBody(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, grokPlainJSONBodyContextKey{}, body)
}

func withGrokRequestCompressionAuthorization(ctx context.Context, authorization string) context.Context {
	return context.WithValue(ctx, grokRequestCompressionAuthorizationKey{}, authorization)
}

func grokPlainJSONBodyFromContext(ctx context.Context) ([]byte, bool) {
	if ctx == nil {
		return nil, false
	}
	body, ok := ctx.Value(grokPlainJSONBodyContextKey{}).([]byte)
	return body, ok && len(body) > 0
}

// GrokRequestCompressionOwned reports whether the gateway owns a zstd request
// declaration on this request. The shared transport uses it to decide whether a
// destination change must rebuild a plain body.
func GrokRequestCompressionOwned(req *http.Request) bool {
	if req == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(req.Header.Get("Content-Encoding")), "zstd")
}

// RebuildGrokPlainRequest replaces a compression-owned body with the recorded
// final JSON and removes the compression declaration, so the request can be
// sent to a destination that never advertised zstd. It is a no-op for a request
// that carries no gateway compression. When the plain body is unavailable the
// caller must keep its existing transport error semantics instead of sending
// bytes the destination may not be able to decode.
func RebuildGrokPlainRequest(req *http.Request) error {
	if !GrokRequestCompressionOwned(req) {
		return nil
	}
	plain, ok := grokPlainJSONBodyFromContext(req.Context())
	if !ok {
		return fmt.Errorf("grok request compression: %s", grokRequestCompressionCodeRebuild)
	}
	req.Header.Del("Content-Encoding")
	req.Body = io.NopCloser(bytes.NewReader(plain))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(plain)), nil
	}
	req.ContentLength = int64(len(plain))
	return nil
}

// GrokRequestOriginChanged reports whether a redirect hop leaves the origin
// (scheme, host and effective port) of the previous hop.
func GrokRequestOriginChanged(from, to *http.Request) bool {
	if from == nil || from.URL == nil || to == nil || to.URL == nil {
		return true
	}
	return !strings.EqualFold(grokRequestOrigin(from.URL), grokRequestOrigin(to.URL))
}

func grokRequestOrigin(u *url.URL) string {
	if u == nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return scheme + "://" + host + ":" + port
}
