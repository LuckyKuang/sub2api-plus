package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/tlsfingerprint"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
)

// defaultGrokUpstreamUserAgent is the pinned generic Grok shell UA.
// Grok upstream must not forward Claude Code / Codex / browser client UAs.
func defaultGrokUpstreamUserAgent() string {
	return xai.CLIUserAgent(xai.ResolveCLIVersion())
}

// grokSamplerAcceptHeader is the Accept declaration for the operation the
// sampler request actually performs. The frozen grok-build sampler sets
// `Accept: text/event-stream` only on its streaming routes and declares
// application/json for the plain JSON operation
// (crates/codegen/xai-grok-sampler/src/client.rs:1099,1479,1812 vs the
// non-streaming builders). The gateway derives it from the final serialized
// body so the Responses, Chat, Messages and WS-bridge adapters cannot send a
// streaming accept for a non-streaming upstream call, and the upstream
// aggregation path still declares SSE when the upstream body streams.
func grokSamplerAcceptHeader(stream bool) string {
	if stream {
		return "text/event-stream"
	}
	return "application/json"
}

// grokBodyStreamsJSON reports whether the final serialized sampler body asks
// the upstream for a streaming SSE response.
func grokBodyStreamsJSON(body []byte) bool {
	return gjson.GetBytes(body, "stream").Bool()
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
