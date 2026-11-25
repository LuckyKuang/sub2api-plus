//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexPublicErrorCatalogShape(t *testing.T) {
	require.Len(t, codexPublicErrorCatalog, 76)
	groups := map[string]int{}
	seenKeys := map[string]bool{}
	seenCodes := map[string]string{}
	for _, definition := range codexPublicErrorCatalog {
		require.NotEmpty(t, definition.Key)
		require.False(t, seenKeys[definition.Key], "duplicate key %s", definition.Key)
		seenKeys[definition.Key] = true
		require.Contains(t, []string{"request", "service", "transport", "media"}, definition.Group)
		groups[definition.Group]++
		require.NotEmpty(t, definition.DefaultMessage)
		switch definition.Key {
		case "output_length_limit", "output_content_filtered", "response_cancelled", "response_incomplete":
			require.Empty(t, definition.PublicCode)
			require.Empty(t, definition.ErrorType)
		default:
			require.NotEmpty(t, definition.PublicCode, definition.Key)
			require.NotEmpty(t, definition.ErrorType, definition.Key)
		}
		if definition.PublicCode != "" {
			if previous, exists := seenCodes[definition.PublicCode]; exists {
				// Shared public codes are allowed, but only when the reasons are
				// intentionally distinct (B01/B02 and B07/C08 are the reviewed cases).
				require.NotEqual(t, previous, definition.Key)
			}
			seenCodes[definition.PublicCode] = definition.Key
		}
	}
	require.Equal(t, 24, groups["request"])
	require.Equal(t, 17, groups["service"])
	require.Equal(t, 11, groups["transport"])
	require.Equal(t, 24, groups["media"])
}

func TestCodexPublicErrorCodeResolutionAndFallback(t *testing.T) {
	// The protocol table lists exact wire codes. Case variants and unapproved
	// suffixes are not promoted; only the documented hard-limit pattern is.
	for code, want := range map[string]string{
		"insufficient_quota":             "service_capacity_unavailable",
		"UPSTREAM_UNAVAILABLE":           "",
		"Upstream_Unavailable":           "",
		"upstream_unavailable_extra":     "",
		"upstream_unavailable":           "upstream_unavailable",
		"invalid_stream_id":              "invalid_stream_id",
		"vector_store_timeout":           "vector_store_timeout",
		"response_incomplete":            "response_incomplete",
		"image_content_policy_violation": "image_content_policy_violation",
		"request_policy_denied":          "request_policy_denied",
		"invalid_api_key":                "credential_unavailable",
		"insufficient_scope":             "account_access_unavailable",
		"workspace_spend_limit_exceeded": "service_capacity_unavailable",
	} {
		got, ok := codexPublicErrorKeyFromCode(code)
		if want == "" {
			require.False(t, ok, code)
			continue
		}
		require.True(t, ok, code)
		require.Equal(t, want, got, code)
	}

	for status, want := range map[int]string{
		400: "request_rejected",
		401: "credential_unavailable",
		403: "account_access_unavailable",
		402: "service_capacity_unavailable",
		404: "server_error",
		405: "server_error",
		407: "service_connection_failed",
		408: "response_timeout",
		409: "request_conflict",
		410: "server_error",
		413: "request_too_large",
		415: "unsupported_media_type",
		422: "request_rejected",
		429: "rate_limit_exceeded",
		500: "server_error",
		502: "server_error",
		503: "upstream_unavailable",
		504: "response_timeout",
		529: "model_overloaded",
		0:   "service_error",
		418: "server_error",
	} {
		require.Equal(t, want, codexPublicErrorFallbackKey(status), status)
	}

	// B08 publishes its own code with a service-unavailable type.
	definition, ok := codexPublicErrorByKey("policy_capacity_unavailable")
	require.True(t, ok)
	require.Equal(t, "policy_capacity_unavailable", definition.PublicCode)
	require.Equal(t, "service_unavailable_error", definition.ErrorType)

	// Real 4xx/5xx evidence outranks a conflicting request-level code.
	for _, tc := range []struct {
		status   int
		code     string
		wantKey  string
		wantCode int
	}{
		{401, "budget_reached", "credential_unavailable", 503},
		{403, "budget_reached", "account_access_unavailable", 503},
		{429, "budget_reached", "rate_limit_exceeded", 429},
		{500, "invalid_audio_data", "server_error", 502},
		{503, "policy_capacity_unavailable", "policy_capacity_unavailable", 503},
		{403, "model_not_found", "model_not_found", 403},
		{400, "incomplete_audio", "request_rejected", 400},
	} {
		key, publicStatus := ClassifyCodexPublicFailure(tc.status, []byte(`{"error":{"code":"`+tc.code+`"}}`))
		require.Equal(t, tc.wantKey, key, tc.code)
		require.Equal(t, tc.wantCode, publicStatus, tc.code)
	}
}

func TestCodexPublicErrorMessageProjection(t *testing.T) {
	// A confirmed failure is rebuilt from approved fields. Unapproved
	// extensions, sibling objects and provider diagnostics do not survive; a
	// platform-level param value remains.
	input := `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"code":"insufficient_quota","type":"upstream_error","message":"secret","detail":"secret-detail","param":"input","opaque":7},"usage":{"input_tokens":12,"output_tokens":3}},"item":{"error":{"code":"insufficient_quota","message":"tool data"}}}`
	got := string(normalizeOpenAIErrorMessage([]byte(input), openAIErrorMessageSource{}))
	want := `{"type":"response.failed","response":{"id":"resp_1","status":"failed","error":{"message":"当前服务额度不可用，请联系管理员。","code":"service_capacity_unavailable","type":"service_unavailable_error","param":"input"},"usage":{"input_tokens":12,"output_tokens":3}}}`
	require.JSONEq(t, want, got)
	require.NotContains(t, got, "secret")
	require.NotContains(t, got, "tool data")

	unknown := `{"error":{"code":"vendor_specific_secret","message":"secret","trace":"secret"}}`
	got = string(normalizeOpenAIErrorMessage([]byte(unknown), openAIErrorMessageSource{status: 400}))
	require.JSONEq(t, `{"error":{"message":"本次请求未被接受，请检查请求内容。","code":"request_rejected","type":"invalid_request_error"}}`, got)
	require.False(t, strings.Contains(got, "secret"))
}
