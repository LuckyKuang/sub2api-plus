// Package minimax pins the MiniMax client identity that MiniMax platform
// accounts can send upstream.
//
// The published MiniMax Code client renders a single product declaration, the
// bare User-Agent value `MiniMaxAgent`, for every managed provider request
// (minimax-code packages/local-runtime-v2/src/service/model-system/resolution/
// model-resolver-helpers.ts). The official family publishes no version segment:
// every occurrence in the upstream source is the bare token, and the client's
// own package version is never part of a wire declaration. This package freezes
// that value in-binary so Sub2API Plus pins one fingerprint instead of tracking
// an upstream release, mirroring the existing pins in internal/pkg/claude,
// internal/pkg/geminicli, internal/pkg/xai, internal/pkg/antigravity and
// internal/pkg/deepseek.
//
// MiniMax is an enumerated versionless client family: the trusted triple keeps
// UserAgent and Originator coherent, while Version is intentionally empty
// because the official client declares none. This is a per-preset exception
// recorded in docs/OUTBOUND_IDENTITY.md, not a general relaxation of the
// version requirement. Inference also retains the pinned SDK declarations. The
// managed MiniMax session headers (X-Mavis-Session-Id / -Agent-Id /
// -Timezone-Offset) are request state owned by the protocol layer and are never
// identity declarations.
package minimax

import (
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
)

const (
	// ProductToken is the MiniMax Code product declaration. It is both the
	// User-Agent value and the trusted triple's client identifier.
	ProductToken = "MiniMaxAgent"

	// Preset is the outbound identity preset key owned by this family.
	Preset = "minimax"
)

// UserAgent returns the published MiniMax Code User-Agent value. The official
// client family declares no version segment, so the token is the complete
// declaration.
func UserAgent() string {
	return ProductToken
}

// DefaultIdentity is shared by settings resolution and standalone protocol
// clients so the fallback has the same complete declarations in both paths.
func DefaultIdentity() outboundidentity.Identity {
	ua := UserAgent()
	return outboundidentity.Identity{
		Preset:     Preset,
		UserAgent:  ua,
		Originator: ProductToken,
		// Version is intentionally empty: the official client family publishes
		// no client version declaration. See docs/OUTBOUND_IDENTITY.md.
		Version: "",
		Source:  "compiled_default",
		Headers: sdkHeaders(ua),
	}
}

// APIKeyPreset is MiniMax Code's BYOK Anthropic SDK profile, distinct from managed login.
const APIKeyPreset = "minimax_apikey"
const SDKVersion = "0.91.1"

func APIKeyIdentity() outboundidentity.Identity {
	ua := "Anthropic/JS " + SDKVersion
	return outboundidentity.Identity{Preset: APIKeyPreset, UserAgent: ua, Originator: "Anthropic", Version: SDKVersion, Source: "compiled_default", Headers: sdkHeaders(ua)}
}

// Pin a supported MiniMax Code Node host fingerprint, independently of the Go
// server and SDK/client version changes. Upstream permits Node >=22.19 <23.
func sdkHeaders(ua string) map[string]string {
	return map[string]string{"User-Agent": ua,
		"X-Stainless-Lang": "js", "X-Stainless-Package-Version": SDKVersion,
		"X-Stainless-OS": "Linux", "X-Stainless-Arch": "arm64",
		"X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.19.0"}
}

// OAuth uses fetch, not the inference SDK. Keep the trusted managed product
// declaration required by the gateway, without claiming an SDK on auth calls.
func ControlIdentity(identity outboundidentity.Identity) outboundidentity.Identity {
	headers := map[string]string{"User-Agent": identity.UserAgent}
	identity.Headers = headers
	return identity
}
