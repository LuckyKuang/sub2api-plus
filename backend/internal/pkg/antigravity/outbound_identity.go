package antigravity

import (
	"maps"
	"net/http"
	"os"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

// PrivacyAPIClient is the SDK declaration of the two privacy endpoints. It is
// independent of the Antigravity client version and is not an inference header.
const PrivacyAPIClient = "gl-node/22.21.1"

// DefaultIdentity is shared by settings resolution and standalone protocol
// clients so the fallback has the same complete declarations in both paths.
func DefaultIdentity() outboundidentity.Identity {
	version := GetDefaultUserAgentVersion()
	source := "compiled_default"
	if configured := NormalizeUserAgentVersion(os.Getenv(AntigravityUserAgentVersionEnv)); configured != "" {
		version = configured
		source = "environment"
	}
	ua := BuildUserAgent(version)
	return outboundidentity.Identity{
		Preset: "antigravity", UserAgent: ua, Originator: "antigravity",
		Version: version, Source: source, Headers: map[string]string{"User-Agent": ua},
	}
}

func applyPrivacyIdentity(req *http.Request) {
	identity, ok := outboundidentity.Default(req.Context(), "antigravity")
	if !ok {
		identity = DefaultIdentity()
	}
	if identity.Preset == "antigravity" {
		identity.Headers = maps.Clone(identity.Headers)
		if identity.Headers == nil {
			identity.Headers = make(map[string]string)
		}
		identity.Headers["X-Goog-Api-Client"] = PrivacyAPIClient
	}
	// Keep endpoint declarations on the request snapshot, so another finalizer
	// preserves them without changing the caller's identity for other endpoints.
	*req = *req.WithContext(outboundidentity.WithIdentity(req.Context(), identity))
	outboundidentity.ApplyContext(req)
}
