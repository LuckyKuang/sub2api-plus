package openai_ws_v2

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRelayTimingTracksEachTurnAndIgnoresNonTokenEvents(t *testing.T) {
	start := time.Unix(100, 0)
	state := &relayState{turnTimingByID: map[string]*relayTurnTiming{
		"a": {startAt: start}, "b": {startAt: start.Add(time.Second)},
	}}
	send := func(ms int, payload string) observedUpstreamEvent {
		return observeUpstreamMessage(state, []byte(payload), start, func() time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }, nil)
	}
	send(10, `{"type":"response.audio.delta","response_id":"a","delta":"YQ=="}`)
	send(20, `{"type":"response.output_text.delta","response_id":"a","delta":""}`)
	send(30, `{"type":"response.output_text.done","response_id":"a","text":"aggregate"}`)
	require.Nil(t, state.turnTimingByID["a"].firstTokenMs)
	send(100, `{"type":"response.output_text.delta","response_id":"a","delta":"hello"}`)
	send(600, `{"type":"response.function_call_arguments.delta","response_id":"a","delta":"{}"}`)
	send(1100, `{"type":"response.reasoning_text.delta","response_id":"b","delta":"think"}`)
	send(1500, `{"type":"response.output_text.delta","response_id":"b","delta":"answer"}`)
	a := send(1600, `{"type":"response.completed","response":{"id":"a","usage":{"input_tokens":1,"output_tokens":8}}}`)
	b := send(1700, `{"type":"response.completed","response":{"id":"b","usage":{"input_tokens":1,"output_tokens":9}}}`)
	require.Equal(t, 100, *a.firstToken)
	require.Equal(t, 600, *a.lastToken)
	require.Equal(t, 100, *b.firstToken)
	require.Equal(t, 500, *b.lastToken)
}
