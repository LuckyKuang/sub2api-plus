//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOpenAICodexResidency(t *testing.T) {
	off, err := NormalizeOpenAICodexResidency("  ")
	require.NoError(t, err)
	require.Equal(t, "off", off)
	us, err := NormalizeOpenAICodexResidency("US")
	require.NoError(t, err)
	require.Equal(t, "us", us)
	_, err = NormalizeOpenAICodexResidency("eu")
	require.Error(t, err)
}

func TestApplyOpenAIOutboundIdentity_Residency(t *testing.T) {
	usRepo := &settingRepoStub{values: map[string]string{SettingKeyOpenAICodexResidency: "us"}}
	usSvc := &OpenAIGatewayService{settingService: NewSettingService(usRepo, &config.Config{})}
	headers := http.Header{}
	headers.Set(openai.CodexResidencyHeader, "eu")
	usSvc.applyOpenAIOutboundIdentity(context.Background(), nil, headers, true)
	require.Equal(t, "us", headers.Get(openai.CodexResidencyHeader))

	offRepo := &settingRepoStub{values: map[string]string{SettingKeyOpenAICodexResidency: "off"}}
	offSvc := &OpenAIGatewayService{settingService: NewSettingService(offRepo, &config.Config{})}
	headers = http.Header{}
	headers.Set(openai.CodexResidencyHeader, "us")
	offSvc.applyOpenAIOutboundIdentity(context.Background(), nil, headers, true)
	require.Empty(t, headers.Get(openai.CodexResidencyHeader))

	headers = http.Header{}
	usSvc.applyOpenAIOutboundIdentity(context.Background(), nil, headers, false)
	require.Empty(t, headers.Get(openai.CodexResidencyHeader), "non-Codex protocol requests do not gain residency")
}

func TestQuotaWHAMAppliesResidencyWithoutOriginator(t *testing.T) {
	repo := &settingRepoStub{values: map[string]string{SettingKeyOpenAICodexResidency: "us"}}
	quota := &OpenAIQuotaService{openAIIdentityResolver: &OpenAIGatewayService{settingService: NewSettingService(repo, &config.Config{})}}
	headers := map[string]string{"authorization": "Bearer token"}
	quota.applyOpenAIOutboundIdentity(context.Background(), nil, headers)
	require.Equal(t, openai.CodexResidencyUS, headers[openai.CodexResidencyHeader])
	_, hasOriginator := headers["Originator"]
	require.False(t, hasOriginator)
}
