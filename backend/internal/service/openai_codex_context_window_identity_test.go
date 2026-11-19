//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// Codex 0.170 sends context_window_id in the display form of the allocated
// window (`window003`) whenever the window was not restored from a UUIDv7
// rollout. Turn metadata must carry it as a string, so the tenant mapping has to
// keep the salt and the zero-padded width instead of emitting a UUID.
func TestCodexContextWindowIdentityKeepsDisplayForm(t *testing.T) {
	ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
	account := turnStateAccount(31, "principal-a")
	other := turnStateAccount(32, "principal-b")

	mapped, err := resolveCodexAccountIdentityValue(ctx, account, 7, "context-window", "window003")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^window\d{3}$`), mapped)
	require.NotEqual(t, "window003", mapped)

	stable, err := resolveCodexAccountIdentityValue(ctx, account, 7, "context-window", "window003")
	require.NoError(t, err)
	require.Equal(t, mapped, stable, "the same window must map to the same tenant value")

	otherMapped, err := resolveCodexAccountIdentityValue(ctx, other, 7, "context-window", "window003")
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^window\d{3}$`), otherMapped)
	require.NotEqual(t, mapped, otherMapped, "tenants must not share a window identity")
}

func TestCodexContextWindowIdentityKeepsDigitWidth(t *testing.T) {
	ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
	account := turnStateAccount(33, "principal-a")

	for _, raw := range []string{"window001012", "window003", "ctx000", "window018446744073709551615"} {
		mapped, err := resolveCodexAccountIdentityValue(ctx, account, 7, "context-window", raw)
		require.NoError(t, err)
		require.Len(t, mapped, len(raw), "width must follow the allocation groups: %s", raw)
		require.True(t, regexp.MustCompile(`^[a-z]+\d+$`).MatchString(mapped), mapped)
	}
}

func TestCodexContextWindowIdentityIsUniqueWithinScope(t *testing.T) {
	ctx := context.Background()
	account := turnStateAccount(31, "principal-a")
	seen := make(map[string]string, 1000)
	for i := range 1000 {
		raw := fmt.Sprintf("window%03d", i)
		mapped, err := resolveCodexAccountIdentityValue(ctx, account, 7, "context-window", raw)
		require.NoError(t, err)
		require.NotContains(t, seen, mapped, "input %s collided with %s", raw, seen[mapped])
		seen[mapped] = raw
	}

	for _, raw := range []string{"window001000", "window999999"} {
		mapped, err := resolveCodexAccountIdentityValue(ctx, account, 7, "context-window", raw)
		require.NoError(t, err)
		number, err := strconv.Atoi(mapped[len("window"):])
		require.NoError(t, err)
		require.GreaterOrEqual(t, number, 1000)
		require.LessOrEqual(t, number, 999999)
	}
}

func TestCodexContextWindowIdentityLeavesUUIDv7Shape(t *testing.T) {
	ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
	account := turnStateAccount(34, "principal-a")
	const raw = "01912345-6999-7123-b456-789012345678"

	mapped, err := resolveCodexAccountIdentityValue(ctx, account, 7, "context-window", raw)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), mapped)
	require.NotEqual(t, raw, mapped)
}

func TestCodexContextWindowIdentityMetadataStaysString(t *testing.T) {
	ctx := WithOutboundIDStore(context.Background(), newTestOutboundIDStore())
	account := turnStateAccount(35, "principal-a")

	embedded := map[string]any{
		"thread_id":         "01912345-6789-7abc-8def-0123456789ab",
		"turn_id":           "01912345-7000-7fed-9234-123456789abc",
		"context_window_id": "window003",
		"window_number":     3,
	}
	encoded, err := json.Marshal(embedded)
	require.NoError(t, err)
	body := map[string]any{
		"client_metadata": map[string]any{
			openAIWSTurnMetadataHeader: string(encoded),
		},
	}

	changed, err := applyCodexAccountIdentityClientMetadataMap(ctx, body, account, 7)
	require.NoError(t, err)
	require.True(t, changed)

	clientMetadata := body["client_metadata"].(map[string]any)
	rebuilt := clientMetadata[openAIWSTurnMetadataHeader].(string)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(rebuilt), &decoded))
	windowID, ok := decoded["context_window_id"].(string)
	require.True(t, ok, "context_window_id must stay a JSON string")
	require.Regexp(t, regexp.MustCompile(`^window\d{3}$`), windowID)
	require.NotEqual(t, "window003", windowID)
}

func TestResolveCodexModelFacingIdentityValueRejectsOpaqueValues(t *testing.T) {
	for _, raw := range []string{"", "window", "003", "Window003", "window-003", "window00", "window0003", "window_003"} {
		mapped, ok := resolveCodexModelFacingIdentityValue("scope", raw)
		require.False(t, ok, raw)
		require.Empty(t, mapped)
	}
}
