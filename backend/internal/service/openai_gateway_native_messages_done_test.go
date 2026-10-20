//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Anthropic Messages has one successful terminal: message_stop. A bare OpenAI
// [DONE] sentinel must neither complete the request nor hide observed usage.
func TestNativeAnthropicMessagesDoneSentinelCannotReplaceMessageStop(t *testing.T) {
	payload := strings.Split(miniAnthropicSSEStream(), "event: message_stop")[0] + "data: [DONE]\n\n"
	for _, ending := range []string{"eof", "keep_open"} {
		t.Run(ending, func(t *testing.T) {
			var outcome nativeTerminalForwardOutcome
			if ending == "keep_open" {
				result, recorder := forwardNativePipeTest(t, "/v1/messages", true, 1, payload)
				outcome = result
				require.Error(t, outcome.err)
				require.Contains(t, outcome.err.Error(), "timeout")
				require.Equal(t, payload, recorder.Body.String(), "the gateway must not append a successful terminal")
				require.NotContains(t, recorder.Body.String(), "message_stop")
				require.NotContains(t, recorder.Body.String(), "response.completed")
			} else {
				result, err, recorder := forwardNativeSSETest(t, "/v1/messages", true, payload)
				outcome = nativeTerminalForwardOutcome{result: result, err: err}
				require.Equal(t, payload, recorder.Body.String(), "the gateway must not append a successful terminal")
				require.NotContains(t, recorder.Body.String(), "message_stop")
				require.NotContains(t, recorder.Body.String(), "response.completed")
			}
			require.Error(t, outcome.err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, outcome.err, &failover, "a sentinel after output cannot authorize replay")
			require.NotNil(t, outcome.result)
			require.True(t, outcome.result.UsageIncomplete)
			require.Equal(t, 10, outcome.result.Usage.InputTokens)
			require.Equal(t, 5, outcome.result.Usage.OutputTokens)
		})
	}
}
