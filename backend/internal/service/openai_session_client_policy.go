package service

import (
	"bytes"
	"context"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type openAIClientRestrictionKey struct{}

type openAIClientRestrictionRequest struct {
	client *gin.Context
	body   []byte
}

// CodexClientRestrictionError is a request/account policy denial, not an
// upstream failure or permission to migrate an existing conversation owner.
type CodexClientRestrictionError struct {
	Result CodexClientRestrictionDetectionResult
}

func (e *CodexClientRestrictionError) Error() string {
	return CodexClientRestrictionMessage(e.Result)
}

// WithOpenAIClientRestriction captures ingress evidence for durable selection.
// Call after security audit and before selection; later account transforms must
// not change which clients a restricted account permits.
func WithOpenAIClientRestriction(ctx context.Context, c *gin.Context, body []byte) context.Context {
	if c == nil || c.Request == nil {
		return ctx
	}
	client := c.Copy()
	client.Request = c.Request.Clone(ctx)
	return context.WithValue(ctx, openAIClientRestrictionKey{}, openAIClientRestrictionRequest{
		client: client,
		body:   bytes.Clone(body),
	})
}

func (s *OpenAIGatewayService) checkOpenAIClientRestriction(ctx context.Context, account *Account) error {
	request, ok := ctx.Value(openAIClientRestrictionKey{}).(openAIClientRestrictionRequest)
	if !ok {
		return nil
	}
	result := s.EvaluateCodexClientRestriction(request.client, account, request.body)
	if !result.Enabled || result.Matched {
		return nil
	}
	logger.FromContext(ctx).Info("openai.account_client_profile_ineligible",
		zap.Int64("account_id", account.ID), zap.String("reason", result.Reason), zap.String("profile", result.Profile))
	return &CodexClientRestrictionError{Result: result}
}
