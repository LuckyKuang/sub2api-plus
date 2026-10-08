//go:build unit

package admin

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

type nativeIdentityImportService struct{ service.AdminService }

func (s *nativeIdentityImportService) ListProxies(context.Context, int, int, string, string, string, string, string) ([]service.Proxy, int64, error) {
	return nil, 0, nil
}

func TestImportNativeIdentityRejectsForeignFamilyThroughRealAccountService(t *testing.T) {
	// Only proxy enumeration is stubbed. Imported accounts pass through the real
	// CreateAccount validation, with no repository available to hide a write.
	svc := service.NewAdminService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := &AccountHandler{adminService: &nativeIdentityImportService{AdminService: svc}}
	skipGroups := true
	for _, platform := range []string{"deepseek", "kimi", "minimax", "zhipu", "stepfun"} {
		for _, kind := range []string{"oauth", "apikey"} {
			t.Run(platform+"/"+kind, func(t *testing.T) {
				mode := "payg"
				if kind == "oauth" {
					mode = "coding"
				}
				result, err := handler.importData(context.Background(), DataImportRequest{
					SkipDefaultGroupBind: &skipGroups,
					Data: DataPayload{Type: dataType, Version: dataVersion, Accounts: []DataAccount{{
						Name: "imported", Platform: platform, Type: kind, Concurrency: 1, Priority: 1,
						Credentials: map[string]any{"api_key": "test-key", "access_token": "test-token", "api_protocol": "chat_completions", "region": "cn", "oauth_region": "cn", "oauth_provider": platform, "account_mode": mode, "outbound_identity": map[string]any{"preset": "grok"}},
					}}},
				})
				require.NoError(t, err)
				require.Zero(t, result.AccountCreated)
				require.Equal(t, 1, result.AccountFailed)
				require.Len(t, result.Errors, 1)
				require.Contains(t, result.Errors[0].Message, "OUTBOUND_IDENTITY_INVALID")
			})
		}
	}
}
