package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
)

var openAIWSUnknownCredentialGeneration atomic.Uint64

// A socket authenticates at its handshake. A later attempt must never reuse it
// with evidence for a different grant, even when the bearer string is unchanged.
// Existing protocol/identity compatibility remains independent of this digest.
func openAIWSCredentialGeneration(ctx context.Context, req openAIWSAcquireRequest) string {
	owner := req.Account
	authorization := strings.TrimSpace(firstOpenAIWSHeaderValue(req.Headers, "Authorization"))
	// A credential shadow does not carry its parent's auth_mode. The trusted
	// authentication builder has already emitted its signed authorization here.
	agentIdentity := owner != nil && (owner.IsOpenAIAgentIdentity() ||
		(owner.Platform == PlatformOpenAI && strings.HasPrefix(authorization, "AgentAssertion ")))
	if requestCredentialOwnerBound(ctx, req.Account) {
		acquiredOwner, known := requestCredentialOwner(ctx, req.Account)
		if known {
			owner = acquiredOwner
		} else if !agentIdentity {
			// Ambiguous acquisition cannot borrow another attempt's socket.
			return "unknown:" + strconv.FormatUint(openAIWSUnknownCredentialGeneration.Add(1), 10)
		}
	}
	if agentIdentity || owner != nil && owner.IsOpenAIAgentIdentity() {
		// Agent Identity signs each dial. Its existing task generation and pool
		// invalidation own authorization; signature bytes cannot gate reuse.
		authorization = ""
	}
	if authorization == "" && (owner == nil || len(owner.Credentials) == 0) {
		return ""
	}
	var ownerID int64
	var platform, kind string
	var credentials map[string]any
	if owner != nil {
		ownerID, platform, kind, credentials = owner.ID, owner.Platform, owner.Type, owner.Credentials
	}
	encoded, err := json.Marshal(struct {
		OwnerID                      int64
		Platform, Type               string
		Credentials                  map[string]any
		Authorization, URL, ProxyURL string
	}{ownerID, platform, kind, credentials, authorization, stringsTrim(req.WSURL), stringsTrim(req.ProxyURL)})
	if err != nil {
		return "unknown:" + strconv.FormatUint(openAIWSUnknownCredentialGeneration.Add(1), 10)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
