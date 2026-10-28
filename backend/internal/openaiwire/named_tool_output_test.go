//go:build unit || !integration

package openaiwire

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Fixtures follow the actual additional-context producer and the protocol's
// named_unpaired_function_call_output_round_trips_without_call_id contract in
// openai/codex 154e8736a51224e12b2d990753622a2cd8118f54.
func TestNamedUnpairedFunctionCallOutputOfficialShapes(t *testing.T) {
	for _, test := range []struct {
		name string
		item string
		want bool
	}{
		{"agent context", `{"type":"function_call_output","name":"notifications","namespace":"agent","output":"untrusted context"}`, true},
		{"protocol namespace", `{"type":"function_call_output","name":"notifications","namespace":"slack","output":"Alice mentioned you."}`, true},
		{"no namespace", `{"type":"function_call_output","name":"context","output":"context"}`, true},
		{"null optional fields", `{"type":"function_call_output","call_id":null,"name":"context","namespace":null,"output":""}`, true},
		{"structured output", `{"type":"function_call_output","name":"context","namespace":"agent","output":[{"type":"input_text","text":"context"},{"type":"future_content","opaque":true}]}`, true},
		{"paired output", `{"type":"function_call_output","call_id":"call_1","name":"context","namespace":"agent","output":"context"}`, false},
		{"ordinary missing call", `{"type":"function_call_output","output":"context"}`, false},
		{"blank call", `{"type":"function_call_output","call_id":"","name":"context","namespace":"agent","output":"context"}`, false},
		{"nonstring call", `{"type":"function_call_output","call_id":42,"name":"context","namespace":"agent","output":"context"}`, false},
		{"blank name", `{"type":"function_call_output","name":" ","namespace":"agent","output":"context"}`, false},
		{"nonstring name", `{"type":"function_call_output","name":42,"namespace":"agent","output":"context"}`, false},
		{"nonstring namespace", `{"type":"function_call_output","name":"context","namespace":{},"output":"context"}`, false},
		{"missing output", `{"type":"function_call_output","name":"context","namespace":"agent"}`, false},
		{"null output", `{"type":"function_call_output","name":"context","namespace":"agent","output":null}`, false},
		{"object output", `{"type":"function_call_output","name":"context","namespace":"agent","output":{"text":"context"}}`, false},
		{"other output type", `{"type":"tool_search_output","name":"context","namespace":"agent","output":"context"}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var item map[string]any
			require.NoError(t, json.Unmarshal([]byte(test.item), &item))
			require.Equal(t, test.want, IsNamedUnpairedFunctionCallOutput(item))
			require.Equal(t, test.want, IsNamedUnpairedFunctionCallOutputJSON(gjson.Parse(test.item)))
		})
	}
}

func TestNamedUnpairedFunctionCallOutputRejectsAmbiguousWireMembers(t *testing.T) {
	for _, item := range []string{
		`{"type":"function_call_output","type":"function_call_output","name":"context","output":"text"}`,
		`{"type":"function_call_output","call_id":null,"call_id":"call_1","name":"context","output":"text"}`,
		`{"type":"function_call_output","name":"context","name":"other","output":"text"}`,
		`{"type":"function_call_output","name":"context","namespace":"agent","namespace":"slack","output":"text"}`,
		`{"type":"function_call_output","name":"context","output":"text","output":null}`,
	} {
		require.False(t, IsNamedUnpairedFunctionCallOutputJSON(gjson.Parse(item)), item)
	}
}
