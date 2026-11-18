package handler

import "github.com/LuckyKuang/sub2api-plus/internal/openaiwire"

func normalizeCodexPageContext(body []byte) ([]byte, bool) {
	return openaiwire.NormalizeCodexPageContext(body)
}

// normalizeCodexUntrustedContext keeps the upstream name for the same contract:
// Codex external context is folded into untrusted user text, never a tool call.
func normalizeCodexUntrustedContext(body []byte) ([]byte, bool) {
	return openaiwire.NormalizeCodexPageContext(body)
}
