//go:build unit

package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCreateAPIKeyRequestNumericLimits(t *testing.T) {
	positiveExpiry := 1
	require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{
		Quota: 1e100, RateLimit5h: 1e100, ExpiresInDays: &positiveExpiry,
	}))
	require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{}))

	invalidExpiry := 0
	tests := []CreateAPIKeyRequest{
		{Quota: -1},
		{Quota: math.NaN()},
		{Quota: math.Inf(1)},
		{RateLimit5h: -1},
		{RateLimit1d: math.NaN()},
		{RateLimit7d: math.Inf(-1)},
		{ExpiresInDays: &invalidExpiry},
	}
	for _, req := range tests {
		require.Error(t, validateCreateAPIKeyRequest(req))
	}
}

func TestValidateUpdateAPIKeyRequestNumericLimits(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(1)
	require.NoError(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{Quota: &zero, RateLimit7d: &large}))

	for _, req := range []UpdateAPIKeyRequest{
		{Quota: &negative},
		{RateLimit5h: &nan},
		{RateLimit1d: &inf},
		{RateLimit7d: &negative},
	} {
		require.Error(t, validateUpdateAPIKeyRequest(req))
	}
}

func TestCompositeConversionConfigurationValidation(t *testing.T) {
	for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions", "/v1/messages"} {
		require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{ConversionEnabled: true, DefaultClient: "custom", DefaultEndpoint: endpoint}))
	}
	for _, endpoint := range []string{"", "/v1beta", "/v1/systemone"} {
		require.Error(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{ConversionEnabled: true, DefaultClient: "custom", DefaultEndpoint: endpoint}))
		require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{DefaultClient: "custom", DefaultEndpoint: endpoint}), "legacy display preferences remain editable in native mode")
	}
}
