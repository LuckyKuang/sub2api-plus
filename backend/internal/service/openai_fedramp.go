package service

import (
	"context"
	"net/http"
	"strings"
)

const openAIFedRAMPAPIBaseURL = "https://gov.api.openai.com"
const openAIFedRAMPCodexModelsURL = "https://gov.chatgpt.com/backend-api/codex/models"

// IsOpenAIAPIKeyFedRAMP follows openai/codex 3d1001e8652212529810500facf31f63c1527d24.
// Issuance prefixes are routing hints, never credential validation or entitlement.
func (a *Account) IsOpenAIAPIKeyFedRAMP() bool {
	if a == nil || !a.IsOpenAIApiKey() {
		return false
	}
	key := strings.TrimSpace(a.GetOpenAIApiKey())
	return strings.HasPrefix(key, "sk-proj-fedramp-") || strings.HasPrefix(key, "sk-svcacct-fedramp-")
}

// applyOpenAIRegionalRoutingHeaders binds regional routing to the captured
// credential owner, independently of inbound headers, overrides, and client-family
// selection. OAuth routing is claim-owned; API-key routing uses issuance prefixes.
func applyOpenAIRegionalRoutingHeaders(ctx context.Context, account *Account, headers http.Header) {
	if headers == nil {
		return
	}
	owner, err := requestCredentialAccount(ctx, account)
	if err != nil || owner == nil || !owner.IsOpenAI() {
		return
	}
	for name := range headers {
		if strings.EqualFold(name, "x-openai-fedramp") {
			delete(headers, name)
		}
	}
	if owner.IsOpenAIAPIKeyFedRAMP() || owner.IsChatGPTAccountFedRAMP() {
		headers.Set("x-openai-fedramp", "true")
	}
}
