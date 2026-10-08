//go:build unit

package stepfun

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOfficialStepCodeIdentityAndRegionBaselines(t *testing.T) {
	identity := DefaultIdentity()
	require.Equal(t, "step (linux 6.8.0-31-generic; x64)", identity.UserAgent)
	require.Equal(t, "step", identity.Originator)
	require.Empty(t, identity.Version)
	headers := http.Header{"User-Agent": {"inbound"}, "X-Step-Client": {"foreign"}, "Originator": {"codex"}, "Version": {"1.2.3"}}
	identity.ForProtocol("chat_completions").Apply(headers)
	require.Equal(t, "stepcode", headers.Get("X-Step-Client"))
	require.Equal(t, "6.40.0", headers.Get("X-Stainless-Package-Version"))
	require.Equal(t, "Linux", headers.Get("X-Stainless-OS"))
	require.Equal(t, "x64", headers.Get("X-Stainless-Arch"))
	require.Empty(t, headers.Get("Originator"))
	require.Empty(t, headers.Get("Version"))
	identity.Apply(headers)
	require.Len(t, headers, 1, "fetch-based discovery must not inherit SDK or inference attribution")
	require.Equal(t, "https://api.stepfun.com/v1", BaseURL("cn", false))
	require.Equal(t, "https://api.stepfun.ai/v1", BaseURL("global", false))
	require.Equal(t, "https://api.stepfun.com/step_plan/v1", BaseURL("cn", true))
	require.Equal(t, "https://api.stepfun.ai/step_plan/v1", BaseURL("global", true))
}
