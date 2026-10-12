//go:build unit || !integration

package openai_ws_v2

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestRelayTimingTracksEachTurnAndIgnoresNonTokenEvents(t *testing.T) {
	start := time.Unix(100, 0)
	state := &relayState{turnTimingByID: map[string]*relayTurnTiming{
		"a": {startAt: start}, "b": {startAt: start.Add(time.Second)},
	}}
	send := func(ms int, payload string) observedUpstreamEvent {
		return observeUpstreamMessage(state, []byte(payload), start, start.Add(time.Duration(ms)*time.Millisecond), func() time.Time { return start.Add(time.Duration(ms) * time.Millisecond) }, nil)
	}
	send(5, `{"type":"response.created","response":{"id":"a"}}`)
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
	require.Equal(t, 5, *a.firstByte)
	require.Equal(t, 100, *b.firstByte)
	require.Equal(t, 100, *a.firstToken)
	require.Equal(t, 600, *a.lastToken)
	require.Equal(t, 100, *b.firstToken)
	require.Equal(t, 500, *b.lastToken)
}

type receiptClockFrameConn struct {
	FrameConn
	clock *atomic.Int64
}

func (c receiptClockFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	typ, payload, err := c.FrameConn.ReadFrame(ctx)
	if err == nil {
		c.clock.Add(100)
	}
	return typ, payload, err
}

// docs/USAGE_TIMING.md: WS bytes use arrival time before transforms or local
// callbacks. Token timestamps keep their existing observation boundary.
func TestRelayFirstByteUsesReceiptClockBeforeLocalTransform(t *testing.T) {
	start := time.Unix(100, 0)
	var elapsed atomic.Int64
	client := newPassthroughTestFrameConn(nil, false)
	upstream := receiptClockFrameConn{FrameConn: newPassthroughTestFrameConn([]passthroughTestFrame{
		{msgType: coderws.MessageText, payload: []byte(`{"type":"response.created","response":{"id":"audit-turn"}}`)},
		{msgType: coderws.MessageText, payload: []byte(`{"type":"response.output_text.delta","response_id":"audit-turn","delta":"hello"}`)},
		{msgType: coderws.MessageText, payload: []byte(`{"type":"response.completed","response":{"id":"audit-turn","usage":{"input_tokens":1,"output_tokens":1}}}`)},
	}, true), clock: &elapsed}
	var transformed atomic.Bool
	var turn RelayTurnResult
	result, exit := Relay(context.Background(), client, upstream, []byte(`{"type":"response.create","model":"gpt-test","input":[]}`), RelayOptions{
		FirstMessageSent: true, FirstTurnStartedAt: start,
		Now: func() time.Time { return start.Add(time.Duration(elapsed.Load()) * time.Millisecond) },
		TransformClientMessage: func(_ coderws.MessageType, payload []byte) ([]byte, bool, error) {
			if transformed.CompareAndSwap(false, true) {
				elapsed.Add(3000)
			}
			return payload, true, nil
		},
		OnTurnComplete: func(value RelayTurnResult) { turn = value },
	})
	require.Nil(t, exit)
	require.NotNil(t, turn.FirstByteMs)
	require.Equal(t, 100, *turn.FirstByteMs)
	require.Equal(t, 3200, *turn.FirstTokenMs)
	require.Equal(t, 100, *result.FirstByteMs)
}
