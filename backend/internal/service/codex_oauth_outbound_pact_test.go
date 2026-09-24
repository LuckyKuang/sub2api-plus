//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// PRI-0 golden guard — the Codex platform OAuth outbound MUST NOT change across
// upstream merges. Contract per docs/CODEX_OAUTH_OUTBOUND_PACT.md, cross-checked
// against the official Codex source (codex-rs @ ec4d27ae):
//   - DEFAULT_ORIGINATOR = "codex_cli_rs"; Authorization: Bearer on inference.
//   - WHAM metering answers with User-Agent only — never Originator/Version.
//
// If this test fails after a merge, the Codex OAuth outbound identity regressed;
// fix the merge, or consciously update the golden value AND the pact doc.
func TestPRI0CodexOAuthOutboundContractFrozen(t *testing.T) {
	settingService := &SettingService{settingRepo: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	svc := &OpenAIGatewayService{settingService: settingService}
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"user_agent": testOpenAIAccountUserAgent},
	}

	identity := svc.resolveOpenAIOutboundIdentity(context.Background(), account)
	require.Equal(t, testOpenAIAccountCurrentUserAgent, identity.UserAgent, "canonical Codex UA")
	require.Equal(t, openai.CodexDefaultOriginator, identity.Originator, "default Codex originator is codex_cli_rs")
	require.Equal(t, codexCLIVersion, identity.Version, "effective Codex client version")

	// Responses inference: User-Agent + Originator + Version are all declared.
	infer := http.Header{}
	applyResolvedOpenAIOutboundIdentity(infer, identity, true)
	require.Equal(t, identity.UserAgent, infer.Get("User-Agent"))
	require.Equal(t, identity.Originator, infer.Get("Originator"))
	require.Equal(t, identity.Version, infer.Get("Version"))

	// WHAM metering/credits: User-Agent only — Originator and Version are omitted.
	wham := http.Header{}
	applyResolvedOpenAIOutboundIdentity(wham, identity, false)
	require.Equal(t, identity.UserAgent, wham.Get("User-Agent"))
	require.Empty(t, wham.Get("Originator"), "WHAM must not declare Originator")
	require.Empty(t, wham.Get("Version"), "WHAM must not declare Version")

	// Authorization is attached upstream of this layer (Bearer access token);
	// the identity layer never rewrites it. Guard the canonical originator const.
	require.Equal(t, "codex_cli_rs", openai.CodexDefaultOriginator)
	require.Equal(t, "codex_cli_rs", openai.CodexCLIOriginator)
}
