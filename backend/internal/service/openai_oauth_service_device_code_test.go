//go:build unit || !integration

package service

import (
	"context"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type openAIOAuthClientDeviceStub struct {
	openaiOAuthClientStateStub
	started *openai.DeviceUserCodeResponse
}

func (s *openAIOAuthClientDeviceStub) StartDeviceCode(context.Context, string, string) (*openai.DeviceUserCodeResponse, error) {
	return s.started, nil
}

// The official device-code flow has no server-side account binding: the session
// must not carry an AccountID, and the exchange resolves the outbound identity
// from the global/default chain afterwards.
func TestOpenAIOAuthService_StartDeviceCode_SessionHasNoAccountBinding(t *testing.T) {
	client := &openAIOAuthClientDeviceStub{started: &openai.DeviceUserCodeResponse{DeviceAuthID: "dev-1", UserCode: "ABCD-1234", Interval: 5}}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	result, err := svc.StartDeviceCode(context.Background(), nil, "")
	require.NoError(t, err)
	require.Equal(t, "ABCD-1234", result.UserCode)
	require.Equal(t, openai.DeviceVerificationURL, result.VerificationURL)

	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Nil(t, session.AccountID, "device-code sessions must not pre-bind an account")
	require.Equal(t, "dev-1", session.DeviceAuthID)
	require.Equal(t, "ABCD-1234", session.DeviceUserCode)
	require.Equal(t, openai.DeviceCodeRedirectURI, session.RedirectURI)
	require.WithinDuration(t, time.Now(), session.CreatedAt, time.Minute)
}

// Device-code sessions keep the official 15-minute lifetime (Batch 4.4).
func TestOpenAIOAuthService_StartDeviceCode_SessionExpiresAfter15Minutes(t *testing.T) {
	client := &openAIOAuthClientDeviceStub{started: &openai.DeviceUserCodeResponse{DeviceAuthID: "dev-2", UserCode: "WXYZ-5678", Interval: 5}}
	svc := NewOpenAIOAuthService(nil, client)
	defer svc.Stop()

	result, err := svc.StartDeviceCode(context.Background(), nil, "")
	require.NoError(t, err)

	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	session.CreatedAt = time.Now().Add(-16 * time.Minute)
	svc.sessionStore.Set(result.SessionID, session)
	_, ok = svc.sessionStore.Get(result.SessionID)
	require.False(t, ok, "device-code session older than 15 minutes must expire")
}
