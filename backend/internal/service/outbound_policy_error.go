package service

import (
	"errors"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/gin-gonic/gin"
)

// recordOutboundPolicyError attributes a pre-dispatch policy rejection locally.
// The reason is an enum; request headers, tokens and user fields are never logged.
func recordOutboundPolicyError(c *gin.Context, account *Account, err error) bool {
	if !errors.Is(err, brandidentity.ErrBrandedOutboundHeader) {
		return false
	}
	reason := "prohibited_declaration"
	var violation *brandidentity.Violation
	if errors.As(err, &violation) {
		reason = violation.Reason
	}
	if c == nil {
		return true
	}
	setOpsUpstreamError(c, 0, brandidentity.ErrBrandedOutboundHeader.Error(), "")
	event := OpsUpstreamErrorEvent{
		Stage: "outbound_policy", Scope: "gateway", Kind: "local_policy_error",
		Reason: "outbound_policy_" + reason, Message: brandidentity.ErrBrandedOutboundHeader.Error(),
	}
	if account != nil {
		event.Platform, event.AccountID, event.AccountName = account.Platform, account.ID, account.Name
		event.ProxyID, event.ProxyName = opsUpstreamProxyAttribution(account)
	}
	appendOpsUpstreamError(c, event)
	return true
}
