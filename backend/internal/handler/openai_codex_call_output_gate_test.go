//go:build unit

package handler

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/openaiwire"
	"github.com/stretchr/testify/require"
)

func TestCodexCallOutputGateSkipsDeclaredToolsWithoutCandidate(t *testing.T) {
	body := []byte(`{"model":"gpt-5","tools":[{"type":"function","name":"automation_update"},{"type":"function","name":"create_thread"}],` +
		`"input":[{"type":"function_call","call_id":"c1","name":"shell"},{"type":"function_call_output","call_id":"c1","output":"ok"}]}`)
	require.False(t, openaiwire.MayContainCodexCallOutputCandidate(body, "automation_update"))
	require.False(t, openaiwire.MayContainCodexCallOutputCandidate(body, "create_thread", "send_message_to_thread"))
	for name, normalize := range map[string]func([]byte) ([]byte, bool){
		"untrusted":  normalizeCodexUntrustedContext,
		"automation": normalizeCodexAutomationBootstrap,
		"delegation": normalizeCodexDelegationBootstrap,
	} {
		t.Run(name, func(t *testing.T) {
			// Plus 的归一化器会解析一次 payload 后原样返回，因此只断言语义不变，
			// 不沿用上游的零分配断言（那要求零拷贝实现，与本地实现方式不同）。
			got, changed := normalize(body)
			require.False(t, changed, "non-candidate body must not be rewritten")
			require.Equal(t, body, got)
		})
	}
}

// Whenever the ungated bootstrap path rewrites a body, the gate must admit it.
func TestCodexCallOutputGateAdmitsEveryRewrite(t *testing.T) {
	u := func(hex string) string { return `\` + "u" + hex }
	output := mustJSON(t, codexAutomationBootstrap("wiki", "never", automationBootstrapPrompt))
	items := []string{
		`{"type":"function_call_output","namespace":"codex_app","name":"automation_update","output":` + output + `}`,
		`{"type":"function_call_output","namespace":"codex_app","name":"` + u("0061") + `utomation_update","output":` + output + `}`,
		`{"` + u("0074") + `ype":"function_call_output","namespace":"codex_app","name":"automation_update","output":` + output + `}`,
		`{"type":"function_call_output","namespace":"codex_app","name":"automation_update","name":"other","output":` + output + `}`,
		`{"type":"function_call_output","call_id":"c","output":"ok"}`,
		`{"type":"message","role":"user","content":"automation_update function_call_output caf` + u("00e9") + `"}`,
		`{"type":"function_call","call_id":"c","name":"automation_update"}`,
	}
	rng := rand.New(rand.NewSource(11))
	rewrites := 0
	for i := 0; i < 3000; i++ {
		var parts []string
		for n := rng.Intn(3); n >= 0; n-- {
			parts = append(parts, items[rng.Intn(len(items))])
		}
		body := []byte(`{"model":"gpt-5","tools":[{"type":"function","name":"automation_update"}],"input":[` + strings.Join(parts, ",") + `]}`)
		_, rewritten := openaiwire.NormalizeCodexAutomationBootstrap(body)
		if rewritten {
			rewrites++
			require.True(t, openaiwire.MayContainCodexCallOutputCandidate(body, "automation_update"), "%s", body)
		}
	}
	require.Positive(t, rewrites)
}
