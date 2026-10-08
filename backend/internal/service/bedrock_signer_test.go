//go:build unit || !integration

package service

import (
	"context"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBedrockSignerFromAccount_DefaultRegion(t *testing.T) {
	account := &Account{
		Platform: PlatformAnthropic,
		Type:     AccountTypeBedrock,
		Credentials: map[string]any{
			"aws_access_key_id":     "test-akid",
			"aws_secret_access_key": "test-secret",
		},
	}

	signer, err := NewBedrockSignerFromAccount(account)
	require.NoError(t, err)
	require.NotNil(t, signer)
	assert.Equal(t, defaultBedrockRegion, signer.region)
}

func TestFilterBetaTokens(t *testing.T) {
	tokens := []string{"interleaved-thinking-2025-05-14", "tool-search-tool-2025-10-19"}
	filterSet := map[string]struct{}{
		"tool-search-tool-2025-10-19": {},
	}

	assert.Equal(t, []string{"interleaved-thinking-2025-05-14"}, filterBetaTokens(tokens, filterSet))
	assert.Equal(t, tokens, filterBetaTokens(tokens, nil))
	assert.Nil(t, filterBetaTokens(nil, filterSet))
}

func TestBedrockPrivacyFilteringPrecedesSigning(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/test/invoke", nil)
	require.NoError(t, err)
	req.Header.Set("User-Agent", "claude-cli/2.1.119 (external, cli)")
	req.Header.Set("X-Custom", "Sub2API workstation")
	req.Header.Set("X-Keep", "signed-value")
	signer := NewBedrockSigner("test-akid", "test-secret", "", "us-east-1")
	require.NoError(t, signer.SignRequest(context.Background(), req, nil))
	require.Empty(t, req.Header.Get("X-Custom"))
	require.Contains(t, req.Header.Get("Authorization"), "x-keep")
	signed := req.Header.Clone()
	require.NoError(t, brandidentity.FilterOutboundRequest(req))
	require.Equal(t, signed, req.Header)
	req.Header.Set("X-Custom", "sub2api")
	require.ErrorIs(t, brandidentity.FilterOutboundRequest(req), brandidentity.ErrBrandedOutboundHeader)
}
