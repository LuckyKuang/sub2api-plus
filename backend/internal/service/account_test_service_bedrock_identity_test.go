//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountTestServiceBedrockIdentityIsSigned(t *testing.T) {
	for _, preset := range outboundPresetNames {
		t.Run(preset, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{
				"aws_access_key_id": "test-key", "aws_secret_access_key": "test-secret", "aws_session_token": "test-session", "aws_region": "us-east-1",
				outboundIdentityCredential: OutboundIdentitySelection{Preset: preset},
			}}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(http.StatusOK, `{"content":[{"text":"hello"}]}`)}}
			svc := &AccountTestService{httpUpstream: upstream}
			c, recorder := newTestContext()
			require.NoError(t, svc.testBedrockAccountConnection(c, context.Background(), account, "claude-sonnet-4-5"))
			require.Contains(t, recorder.Body.String(), `"success":true`)
			require.Len(t, upstream.requests, 1)
			req := upstream.requests[0]
			expected := builtInOutboundIdentity(preset)
			require.Equal(t, expected.UserAgent, req.Header.Get("User-Agent"))
			for name, value := range expected.Headers {
				require.Equal(t, value, req.Header.Get(name), name)
			}
			body, err := req.GetBody()
			require.NoError(t, err)
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			require.NoError(t, body.Close())
			stamp, err := time.Parse("20060102T150405Z", req.Header.Get("X-Amz-Date"))
			require.NoError(t, err)
			signer, err := NewBedrockSignerFromAccount(account)
			require.NoError(t, err)
			final := req.Clone(req.Context())
			final.Header.Del("Authorization")
			require.NoError(t, signer.signer.SignHTTP(req.Context(), signer.credentials, final, sha256Hash(payload), "bedrock", "us-east-1", stamp))
			require.Equal(t, final.Header.Get("Authorization"), req.Header.Get("Authorization"), "all final signed declarations must have existed before signing")
		})
	}
}
