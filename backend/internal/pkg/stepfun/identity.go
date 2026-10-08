// Package stepfun owns the Step-Code client declarations and official endpoints.
package stepfun

import "github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"

const Preset = "stepfun"
const UserAgent = "step (linux 6.8.0-31-generic; x64)"
const SDKVersion = "6.40.0"

// DefaultIdentity follows Step-Code's providers/src/utils/pi-user-agent.ts and
// step/environment.ts. The actual inference UA publishes no product version.
// SDK metadata belongs only to Chat Completions, not fetch-based discovery.
func DefaultIdentity() outboundidentity.Identity {
	return outboundidentity.Identity{
		Preset: Preset, UserAgent: UserAgent, Originator: "step", Source: "compiled_default",
		Headers: map[string]string{"User-Agent": UserAgent},
		Inference: map[string]outboundidentity.WireProfile{"chat_completions": {Headers: map[string]string{
			"X-Step-Client":    "stepcode",
			"X-Stainless-Lang": "js", "X-Stainless-Package-Version": SDKVersion,
			"X-Stainless-OS": "Linux", "X-Stainless-Arch": "x64",
			"X-Stainless-Runtime": "node", "X-Stainless-Runtime-Version": "v22.19.0",
		}}},
	}
}

// BaseURL is versioned; callers must not append another /v1.
func BaseURL(region string, plan bool) string {
	origin := "https://api.stepfun.com"
	if region == "global" {
		origin = "https://api.stepfun.ai"
	}
	if plan {
		return origin + "/step_plan/v1"
	}
	return origin + "/v1"
}
