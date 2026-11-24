//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexUserErrorHistoryUsesPublicText(t *testing.T) {
	status := 500
	detail := &UserErrorRequestDetail{
		UserErrorRequest: UserErrorRequest{
			Platform:   PlatformOpenAI,
			StatusCode: 502,
			Message:    "upstream secret",
		},
		ErrorBody:          `{"error":{"code":"insufficient_quota","message":"secret","detail":"secret"}}`,
		UpstreamStatusCode: &status,
	}
	ProjectCodexUserErrorDetail(detail, nil)
	require.Nil(t, detail.UpstreamStatusCode)
	require.JSONEq(t, `{"error":{"code":"service_capacity_unavailable","type":"service_unavailable_error","message":"当前服务额度不可用，请联系管理员。"}}`, detail.ErrorBody)
	require.NotContains(t, detail.ErrorBody, "secret")

	item := &UserErrorRequest{Platform: PlatformOpenAI, StatusCode: 429, Message: "secret"}
	ProjectCodexUserErrorListItem(item, nil)
	require.Equal(t, "当前请求受到频率限制，请稍后再试。", item.Message)

	other := &UserErrorRequest{Platform: PlatformAnthropic, StatusCode: 429, Message: "keep"}
	ProjectCodexUserErrorListItem(other, nil)
	require.Equal(t, "keep", other.Message)
}
