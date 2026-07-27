// Package ip 提供客户端 IP 地址提取工具。
package ip

import (
	"net"
	"net/netip"
	"strings"

	"github.com/gin-gonic/gin"
)

// TrustedProxyConfigurationState describes whether the startup proxy policy is
// safe to use for security decisions. Security-sensitive consumers must share
// this interpretation instead of independently parsing proxy configuration.
type TrustedProxyConfigurationState string

const (
	TrustedProxyStateConfigured    TrustedProxyConfigurationState = "configured"
	TrustedProxyStateNotConfigured TrustedProxyConfigurationState = "not_configured"
	TrustedProxyStateEmpty         TrustedProxyConfigurationState = "empty"
	TrustedProxyStateInvalid       TrustedProxyConfigurationState = "invalid"
)

// TrustedProxyConfiguration is the normalized, validated startup policy. The
// values remain strings because Gin receives the original CIDR/IP values.
type TrustedProxyConfiguration struct {
	State    TrustedProxyConfigurationState
	Values   []string
	Prefixes []netip.Prefix
}

// InspectTrustedProxyConfiguration validates the exact proxy configuration
// used by the HTTP server. Wildcard trust is intentionally rejected: it would
// let any direct client spoof a forwarded source address.
func InspectTrustedProxyConfiguration(configured bool, values []string) TrustedProxyConfiguration {
	result := TrustedProxyConfiguration{}
	if !configured {
		result.State = TrustedProxyStateNotConfigured
		return result
	}
	if len(values) == 0 {
		result.State = TrustedProxyStateEmpty
		return result
	}
	result.Values = make([]string, 0, len(values))
	result.Prefixes = make([]netip.Prefix, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || value == "*" {
			result.State = TrustedProxyStateInvalid
			result.Prefixes = nil
			return result
		}
		if address, err := netip.ParseAddr(value); err == nil {
			address = address.Unmap()
			prefix := netip.PrefixFrom(address, address.BitLen())
			result.Values = append(result.Values, prefix.String())
			result.Prefixes = append(result.Prefixes, prefix)
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
			result.State = TrustedProxyStateInvalid
			result.Values = nil
			result.Prefixes = nil
			return result
		}
		prefix = prefix.Masked()
		result.Values = append(result.Values, prefix.String())
		result.Prefixes = append(result.Prefixes, prefix)
	}
	result.State = TrustedProxyStateConfigured
	return result
}

func (c TrustedProxyConfiguration) DirectPeerTrusted(rawIP string) bool {
	if c.State != TrustedProxyStateConfigured {
		return false
	}
	address, err := netip.ParseAddr(NormalizeIP(rawIP))
	if err != nil {
		return false
	}
	address = address.Unmap()
	for _, prefix := range c.Prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// ClientIdentitySource records the only two source classes accepted by the
// global IP policy. Legacy raw forwarded headers intentionally never appear
// here: they remain a compatibility path for non-policy consumers only.
type ClientIdentitySource string

const (
	ClientIdentitySourceDirect           ClientIdentitySource = "direct"
	ClientIdentitySourceTrustedForwarded ClientIdentitySource = "trusted_forwarded"
)

// ClientIdentity is the resolved address a security-sensitive decision may
// use. SafeForEnforcement is deliberately explicit so callers cannot mistake
// an observed-but-incomplete proxy chain for a verified client identity.
type ClientIdentity struct {
	EffectiveIP        string
	DirectPeerIP       string
	Source             ClientIdentitySource
	SafeForEnforcement bool
	FailureReason      string
}

const clientIdentityContextKey = "sub2api.ip_client_identity"

// SetClientIdentity saves the request-scoped security identity so every
// downstream HTTP, auth, and WebSocket handler uses the same decision.
func SetClientIdentity(c *gin.Context, identity ClientIdentity) {
	if c != nil {
		c.Set(clientIdentityContextKey, identity)
	}
}

// ClientIdentityFromContext returns the identity resolved by the global
// middleware. It never manufactures a fallback from untrusted request headers.
func ClientIdentityFromContext(c *gin.Context) (ClientIdentity, bool) {
	if c == nil {
		return ClientIdentity{}, false
	}
	value, ok := c.Get(clientIdentityContextKey)
	if !ok {
		return ClientIdentity{}, false
	}
	identity, ok := value.(ClientIdentity)
	return identity, ok
}

// ClientIdentityResolver binds an HTTP request to the exact trusted-proxy
// configuration applied to Gin at startup.
type ClientIdentityResolver struct {
	trustedProxies TrustedProxyConfiguration
}

func NewClientIdentityResolver(policy TrustedProxyConfiguration) ClientIdentityResolver {
	return ClientIdentityResolver{trustedProxies: policy}
}

// Resolve returns a verified direct identity when no proxy list is configured.
// Once a trusted proxy list is configured, the deployment is explicitly in
// proxy mode: the direct peer must be trusted and Gin must resolve a distinct,
// sanitized downstream source. The final proxy must forward exactly one client
// address in X-Forwarded-For or X-Real-IP. Accepting a multi-hop XFF list here
// would let a CDN egress address become the apparent client when the app only
// trusts its direct Nginx/Caddy peer, causing a ban to affect unrelated users.
// This prevents accidental direct bypasses and shared proxy IP bans in
// reverse-proxy deployments.
func (r ClientIdentityResolver) Resolve(c *gin.Context) ClientIdentity {
	if c == nil || c.Request == nil {
		return ClientIdentity{FailureReason: "request_unavailable"}
	}
	directPeerIP := NormalizeIP(c.Request.RemoteAddr)
	if directPeerIP == "" {
		return ClientIdentity{FailureReason: "direct_peer_unavailable"}
	}

	policy := r.trustedProxies
	switch policy.State {
	case TrustedProxyStateInvalid:
		return ClientIdentity{DirectPeerIP: directPeerIP, FailureReason: "invalid_proxy_config"}
	case TrustedProxyStateConfigured:
		if !policy.DirectPeerTrusted(directPeerIP) {
			return ClientIdentity{DirectPeerIP: directPeerIP, FailureReason: "untrusted_direct_peer"}
		}
		effectiveIP := GetTrustedClientIP(c)
		if effectiveIP == "" || effectiveIP == directPeerIP {
			return ClientIdentity{DirectPeerIP: directPeerIP, FailureReason: "unsafe_proxy_chain"}
		}
		if !hasSanitizedForwardedClientIP(c, effectiveIP) {
			return ClientIdentity{DirectPeerIP: directPeerIP, FailureReason: "forwarded_chain_not_sanitized"}
		}
		return ClientIdentity{
			EffectiveIP:        effectiveIP,
			DirectPeerIP:       directPeerIP,
			Source:             ClientIdentitySourceTrustedForwarded,
			SafeForEnforcement: true,
		}
	case TrustedProxyStateNotConfigured, TrustedProxyStateEmpty:
		// In direct mode forwarding headers are deliberately ignored. A proxy
		// deployment must opt in through server.trusted_proxies instead of
		// relying on a user-controlled header heuristic.
		return ClientIdentity{
			EffectiveIP:        directPeerIP,
			DirectPeerIP:       directPeerIP,
			Source:             ClientIdentitySourceDirect,
			SafeForEnforcement: true,
		}
	default:
		return ClientIdentity{DirectPeerIP: directPeerIP, FailureReason: "proxy_config_unavailable"}
	}
}

// hasSanitizedForwardedClientIP proves that Gin's answer came from one address
// supplied by the direct trusted proxy. It intentionally accepts a single
// X-Real-IP only when X-Forwarded-For is absent, matching Gin's default header
// precedence. Multiple header values and comma-separated lists are rejected:
// the application cannot safely determine whether an untrusted intermediary
// address in such a list is a CDN/shared proxy rather than the actual client.
func hasSanitizedForwardedClientIP(c *gin.Context, effectiveIP string) bool {
	if c == nil || c.Request == nil || NormalizeIP(effectiveIP) == "" {
		return false
	}
	if values, exists := c.Request.Header["X-Forwarded-For"]; exists {
		return singleForwardedHeaderIP(values) == NormalizeIP(effectiveIP)
	}
	if values, exists := c.Request.Header["X-Real-Ip"]; exists {
		return singleForwardedHeaderIP(values) == NormalizeIP(effectiveIP)
	}
	return false
}

func singleForwardedHeaderIP(values []string) string {
	if len(values) != 1 {
		return ""
	}
	value := strings.TrimSpace(values[0])
	if value == "" || strings.Contains(value, ",") {
		return ""
	}
	return NormalizeIP(value)
}

const forwardedIPSettingsKey = "sub2api.forwarded_ip_settings"

type forwardedIPSettings struct {
	trustForwarded bool
	headers        []string
}

// SetForwardedIPSettings snapshots the forwarded-IP mode and custom header list
// for this request.
func SetForwardedIPSettings(c *gin.Context, enabled bool, headers []string) {
	if c == nil {
		return
	}
	c.Set(forwardedIPSettingsKey, forwardedIPSettings{
		trustForwarded: enabled,
		headers:        append([]string(nil), headers...),
	})
}

// SetLegacyForwardedIPTrust records whether raw forwarding headers override
// Gin's server.trusted_proxies chain for this request.
func SetLegacyForwardedIPTrust(c *gin.Context, enabled bool) {
	SetForwardedIPSettings(c, enabled, nil)
}

func requestForwardedIPSettings(c *gin.Context) (forwardedIPSettings, bool) {
	if c == nil {
		return forwardedIPSettings{}, false
	}
	value, ok := c.Get(forwardedIPSettingsKey)
	if !ok {
		return forwardedIPSettings{}, false
	}
	settings, ok := value.(forwardedIPSettings)
	return settings, ok
}

func requestUsesLegacyForwardedIPTrust(c *gin.Context) bool {
	settings, ok := requestForwardedIPSettings(c)
	return !ok || settings.trustForwarded
}

// GetClientIP resolves the client address using the legacy forwarding-header
// precedence used before the trusted-proxy hardening. It remains the
// compatibility path for request metadata and usage/error logs; security-
// sensitive callers must use GetTrustedClientIP or GetSecurityClientIP.
func GetClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if !requestUsesLegacyForwardedIPTrust(c) {
		return GetTrustedClientIP(c)
	}

	settings, _ := requestForwardedIPSettings(c)
	customIP, customFallback := resolveCustomForwardedClientIP(c, settings.headers)
	if customIP != "" {
		return customIP
	}

	// Preserve the historical precedence used by existing reverse-proxy
	// deployments, while skipping an internal proxy address when a public XFF
	// value is available. This covers Docker/Nginx setups that accidentally
	// write the bridge address into X-Real-IP.
	legacyIP, legacyFallback := resolveLegacyForwardedHeaderIP(c)
	if legacyIP != "" {
		return legacyIP
	}
	if customFallback != "" {
		return customFallback
	}
	if legacyFallback != "" {
		return legacyFallback
	}
	return normalizeIP(c.ClientIP())
}

func resolveCustomForwardedClientIP(c *gin.Context, headers []string) (string, string) {
	if c == nil {
		return "", ""
	}
	var fallback string
	for _, header := range headers {
		for _, value := range c.Request.Header.Values(header) {
			for _, candidate := range strings.Split(value, ",") {
				parsed := net.ParseIP(strings.TrimSpace(candidate))
				if parsed == nil {
					continue
				}
				normalized := parsed.String()
				if isPrivateIP(normalized) {
					if fallback == "" {
						fallback = normalized
					}
					continue
				}
				return normalized, fallback
			}
		}
	}
	return "", fallback
}

func resolveLegacyForwardedHeaderIP(c *gin.Context) (string, string) {
	var fallback string
	if forwarded := normalizeIP(c.GetHeader("CF-Connecting-IP")); forwarded != "" {
		fallback = forwarded
		if !isPrivateIP(forwarded) {
			return forwarded, fallback
		}
	}
	if realIP := normalizeIP(c.GetHeader("X-Real-IP")); realIP != "" {
		if fallback == "" {
			fallback = realIP
		}
		if !isPrivateIP(realIP) {
			return realIP, fallback
		}
	}
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		for _, candidate := range ips {
			normalized := normalizeIP(candidate)
			if normalized == "" {
				continue
			}
			if !isPrivateIP(normalized) {
				return normalized, fallback
			}
			if fallback == "" {
				fallback = normalized
			}
		}
	}
	return "", fallback
}

// GetTrustedClientIP 从 Gin 的可信代理解析链提取客户端 IP。
// 该方法依赖 gin.Engine.SetTrustedProxies 配置，不会优先直接信任原始转发头值。
// 适用于 ACL / 风控等安全敏感场景。
func GetTrustedClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return normalizeIP(c.ClientIP())
}

// GetSecurityClientIP returns the address used by security-sensitive paths.
// When legacy forwarded-IP trust is enabled, raw forwarding headers take over
// client-IP resolution. When disabled, Gin's server.trusted_proxies chain is
// authoritative.
func GetSecurityClientIP(c *gin.Context, trustForwarded bool) string {
	if requestSettings, ok := requestForwardedIPSettings(c); ok {
		trustForwarded = requestSettings.trustForwarded
	}
	if trustForwarded {
		return GetClientIP(c)
	}
	return GetTrustedClientIP(c)
}

// normalizeIP 规范化 IP 地址，去除端口号和空格。
func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	// 移除端口号（如 "192.168.1.1:8080" -> "192.168.1.1"）
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	// Keep IPv4 and IPv4-mapped IPv6 addresses in one canonical representation.
	if v4 := parsed.To4(); v4 != nil {
		return v4.String()
	}
	return parsed.String()
}

// NormalizeIP returns a canonical, valid IP address suitable for security
// decisions and persistence. Invalid values intentionally become empty rather
// than creating distinct, attacker-controlled database keys.
func NormalizeIP(value string) string {
	return normalizeIP(value)
}

// NormalizeIPOrCIDR returns the canonical form of either a single IP address
// or a network. Canonical CIDRs prevent semantically identical rules such as
// 192.0.2.9/24 and 192.0.2.0/24 from occupying separate database rows.
func NormalizeIPOrCIDR(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "/") {
		return normalizeIP(value)
	}
	_, network, err := net.ParseCIDR(value)
	if err != nil || network == nil {
		return ""
	}
	return network.String()
}

// NormalizeNonGlobalIPOrCIDR is for deployment-controlled recovery policies.
// A global range would silently disable every block rule, so /0 entries and
// IPv4-mapped IPv6 networks are intentionally rejected here. Regular ACL and
// block-rule helpers keep their broader semantics.
func NormalizeNonGlobalIPOrCIDR(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "/") {
		return normalizeIP(value)
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() {
		return ""
	}
	return prefix.Masked().String()
}

// IsValidIPOrCIDR reports whether value is a single address or a CIDR range.
// It is used by security rule management before a rule is persisted.
func IsValidIPOrCIDR(value string) bool {
	return NormalizeIPOrCIDR(value) != ""
}

// privateNets contains the private/loopback ranges skipped while selecting a
// public address from a legacy X-Forwarded-For chain.
var privateNets []*net.IPNet

func init() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid CIDR: " + cidr)
		}
		privateNets = append(privateNets, block)
	}
}

// CompiledIPRules 表示预编译的 IP 匹配规则。
// PatternCount 记录原始规则数量，用于保留“规则存在但全无效”时的行为语义。
type CompiledIPRules struct {
	CIDRs        []*net.IPNet
	IPs          []net.IP
	PatternCount int
}

// CompileIPRules 将 IP/CIDR 字符串规则预编译为可复用结构。
// 非法规则会被忽略，但 PatternCount 会保留原始规则条数。
func CompileIPRules(patterns []string) *CompiledIPRules {
	compiled := &CompiledIPRules{
		CIDRs:        make([]*net.IPNet, 0, len(patterns)),
		IPs:          make([]net.IP, 0, len(patterns)),
		PatternCount: len(patterns),
	}
	for _, pattern := range patterns {
		normalized := strings.TrimSpace(pattern)
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "/") {
			_, cidr, err := net.ParseCIDR(normalized)
			if err != nil || cidr == nil {
				continue
			}
			compiled.CIDRs = append(compiled.CIDRs, cidr)
			continue
		}
		parsedIP := net.ParseIP(normalized)
		if parsedIP == nil {
			continue
		}
		compiled.IPs = append(compiled.IPs, parsedIP)
	}
	return compiled
}

func matchesCompiledRules(parsedIP net.IP, rules *CompiledIPRules) bool {
	if parsedIP == nil || rules == nil {
		return false
	}
	for _, cidr := range rules.CIDRs {
		if cidr.Contains(parsedIP) {
			return true
		}
	}
	for _, ruleIP := range rules.IPs {
		if parsedIP.Equal(ruleIP) {
			return true
		}
	}
	return false
}

// MatchesCompiledIPRules reports whether a canonical client address matches a
// previously compiled rule set. It keeps CIDR parsing off hot request paths.
func MatchesCompiledIPRules(clientIP string, rules *CompiledIPRules) bool {
	return matchesCompiledRules(net.ParseIP(normalizeIP(clientIP)), rules)
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, block := range privateNets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// MatchesPattern 检查 IP 是否匹配指定的模式（支持单个 IP 或 CIDR）。
// pattern 可以是：
// - 单个 IP: "192.168.1.100"
// - CIDR 范围: "192.168.1.0/24"
func MatchesPattern(clientIP, pattern string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	// 尝试解析为 CIDR
	if strings.Contains(pattern, "/") {
		_, cidr, err := net.ParseCIDR(pattern)
		if err != nil {
			return false
		}
		return cidr.Contains(ip)
	}

	// 作为单个 IP 处理
	patternIP := net.ParseIP(pattern)
	if patternIP == nil {
		return false
	}
	return ip.Equal(patternIP)
}

// MatchesAnyPattern 检查 IP 是否匹配任意一个模式。
func MatchesAnyPattern(clientIP string, patterns []string) bool {
	for _, pattern := range patterns {
		if MatchesPattern(clientIP, pattern) {
			return true
		}
	}
	return false
}

// CheckIPRestriction 检查 IP 是否被 API Key 的 IP 限制允许。
// 返回值：(是否允许, 拒绝原因)
// 逻辑：
// 1. 先检查黑名单，如果在黑名单中则直接拒绝
// 2. 如果白名单不为空，IP 必须在白名单中
// 3. 如果白名单为空，允许访问（除非被黑名单拒绝）
func CheckIPRestriction(clientIP string, whitelist, blacklist []string) (bool, string) {
	return CheckIPRestrictionWithCompiledRules(
		clientIP,
		CompileIPRules(whitelist),
		CompileIPRules(blacklist),
	)
}

// CheckIPRestrictionWithCompiledRules 使用预编译规则检查 IP 是否允许访问。
func CheckIPRestrictionWithCompiledRules(clientIP string, whitelist, blacklist *CompiledIPRules) (bool, string) {
	// 规范化 IP
	clientIP = normalizeIP(clientIP)
	if clientIP == "" {
		return false, "access denied"
	}
	parsedIP := net.ParseIP(clientIP)
	if parsedIP == nil {
		return false, "access denied"
	}

	// 1. 检查黑名单
	if blacklist != nil && blacklist.PatternCount > 0 && matchesCompiledRules(parsedIP, blacklist) {
		return false, "access denied"
	}

	// 2. 检查白名单（如果设置了白名单，IP 必须在其中）
	if whitelist != nil && whitelist.PatternCount > 0 && !matchesCompiledRules(parsedIP, whitelist) {
		return false, "access denied"
	}

	return true, ""
}

// ValidateIPPattern 验证 IP 或 CIDR 格式是否有效。
func ValidateIPPattern(pattern string) bool {
	if strings.Contains(pattern, "/") {
		_, _, err := net.ParseCIDR(pattern)
		return err == nil
	}
	return net.ParseIP(pattern) != nil
}

// ValidateIPPatterns 验证多个 IP 或 CIDR 格式。
// 返回无效的模式列表。
func ValidateIPPatterns(patterns []string) []string {
	var invalid []string
	for _, p := range patterns {
		if !ValidateIPPattern(p) {
			invalid = append(invalid, p)
		}
	}
	return invalid
}
