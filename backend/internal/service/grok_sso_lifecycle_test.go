//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

type grokSSOLateClient struct {
	GrokOAuthClient
	cancel context.CancelFunc
}

func (c grokSSOLateClient) ConvertSSOToBuild(context.Context, string, string) (*xai.TokenResponse, error) {
	c.cancel()
	return &xai.TokenResponse{AccessToken: "late-access", RefreshToken: "late-refresh"}, nil
}

// A canceled login/import request cannot publish credentials from a late
// successful issuer response, even if the transport completes concurrently.
func TestGrokSSOCanceledConversionCannotPublishLateCredentials(t *testing.T) {
	for _, operation := range []string{"validate", "batch-import"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			svc := NewGrokOAuthService(nil, grokSSOLateClient{cancel: cancel})
			t.Cleanup(svc.Stop)
			var info *GrokTokenInfo
			var err error
			if operation == "validate" {
				info, err = svc.ValidateSSOToken(ctx, "private-web-cookie", nil)
			} else {
				info, err = svc.ConvertFromSSO(ctx, "private-web-cookie", nil)
			}
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, info)
			require.Nil(t, svc.BuildAccountCredentials(info))
		})
	}
}
