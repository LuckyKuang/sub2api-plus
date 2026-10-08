package service

import (
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

// Validate the platform's actual authentication/protocol contract at save time.
func validateStepFunCredentials(platform, kind string, credentials map[string]any) error {
	if platform != PlatformStepFun {
		return nil
	}
	invalid := func() error {
		return infraerrors.BadRequest("STEPFUN_CREDENTIALS_INVALID", "StepFun requires an API key or a native Step Plan grant, a valid region, and Chat Completions")
	}
	account := &Account{Platform: platform, Type: kind, Credentials: credentials}
	if kind != AccountTypeAPIKey && kind != AccountTypeOAuth {
		return invalid()
	}
	if protocol := account.GetCredential("api_protocol"); protocol != "" && protocol != APIProtocolChatCompletions {
		return invalid()
	}
	if mode := account.GetCredential("account_mode"); mode != "" && mode != AccountModePayG && mode != AccountModeCoding {
		return invalid()
	}
	regionKey, tokenKey := "region", "api_key"
	if kind == AccountTypeOAuth {
		regionKey, tokenKey = "oauth_region", "access_token"
		if !account.IsDomesticOAuth() || !account.IsCodingPlan() || account.GetCredential("refresh_token") != "" {
			return invalid()
		}
		if raw := account.GetCredential("expires_at"); raw != "" {
			if _, err := time.Parse(time.RFC3339, raw); err != nil {
				return invalid()
			}
		}
	}
	if region := account.GetCredential(regionKey); region != "" && region != "cn" && region != "global" {
		return invalid()
	}
	token := strings.TrimSpace(account.GetCredential(tokenKey))
	if token == "" || len(token) > 16384 || brandidentity.ContainsBrand(token) {
		return invalid()
	}
	for _, c := range token {
		if c < 33 || c > 126 {
			return invalid()
		}
	}
	return nil
}
