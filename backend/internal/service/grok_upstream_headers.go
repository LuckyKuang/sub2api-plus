package service

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

// defaultGrokUpstreamUserAgent is the pinned generic Grok shell UA.
// Grok upstream must not forward Claude Code / Codex / browser client UAs.
func defaultGrokUpstreamUserAgent() string {
	return xai.CLIUserAgent(xai.ResolveCLIVersion())
}

func applyDefaultGrokUpstreamHeaders(req *http.Request) {
	if req == nil {
		return
	}
	// Render the trusted request/global snapshot. Inbound client declarations
	// (Claude Code, Codex, curl, etc.) are never an identity source.
	identity, ok := outboundidentity.Default(req.Context(), "grok")
	if !ok {
		identity = builtInOutboundIdentity("grok")
	}
	identity.Apply(req.Header)
}

func applyGrokTLSProfileHeaders(req *http.Request, profile *tlsfingerprint.Profile) {
	// The TLS profile owns no HTTP identity fields; render the trusted snapshot here.
	applyDefaultGrokUpstreamHeaders(req)
	_ = profile
}

// openAITLSFingerprintRuntime is the resolved TLS fingerprint routing result
// used by OpenAI/Grok outbound header application. Defined here so Grok header
// helpers compile even when the full OpenAI TLS router is not present on HEAD.
type openAITLSFingerprintRuntime struct {
	Profile            *tlsfingerprint.Profile
	UpstreamUserAgent  string
	UpstreamOriginator string
	Matched            bool
}

func applyGrokRuntimeHeaders(req *http.Request, _ openAITLSFingerprintRuntime) {
	applyDefaultGrokUpstreamHeaders(req)
}

// resolveGrokUpstreamUserAgent always returns the pinned Grok CLI User-Agent.
// Inbound client UAs (Claude Code, Codex, browsers, libraries) are never forwarded.
func resolveGrokUpstreamUserAgent(_ *gin.Context) string {
	return defaultGrokUpstreamUserAgent()
}
