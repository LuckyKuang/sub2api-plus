//go:build unit || !integration

package openai_ws_v2

import (
	"context"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// CHANNEL_PRICING.md: terminal usage replaces provisional classification;
// absent/invalid details use the aggregate price, turns reset and session sums
// include each completed turn exactly once.
func TestRelayCacheWriteTTLTurnIsolationAndReplacement(t *testing.T) {
	client := newPassthroughTestFrameConn(nil, false)
	upstream := newPassthroughTestFrameConn(nil, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	turns := make(chan RelayTurnResult, 3)
	results := make(chan RelayResult, 1)
	go func() {
		result, _ := Relay(ctx, client, upstream,
			[]byte(`{"type":"response.create","model":"gpt-6-luna","input":[]}`),
			RelayOptions{OnTurnComplete: func(turn RelayTurnResult) { turns <- turn }})
		results <- result
	}()
	send := func(payload string) {
		forwarded := len(client.Writes())
		upstream.readCh <- passthroughTestFrame{msgType: coderws.MessageText, payload: []byte(payload)}
		require.Eventually(t, func() bool { return len(client.Writes()) == forwarded+1 }, time.Second, time.Millisecond)
	}
	waitTurn := func() RelayTurnResult {
		select {
		case turn := <-turns:
			return turn
		case <-time.After(time.Second):
			t.Fatal("turn did not complete")
			return RelayTurnResult{}
		}
	}
	nextTurn := func(n int) {
		client.readCh <- passthroughTestFrame{msgType: coderws.MessageText, payload: []byte(`{"type":"response.create","model":"gpt-6-luna","input":[]}`)}
		require.Eventually(t, func() bool { return len(upstream.Writes()) == n }, time.Second, time.Millisecond)
	}
	require.Eventually(t, func() bool { return len(upstream.Writes()) == 1 }, time.Second, time.Millisecond)
	send(`{"type":"response.created","response":{"id":"first","usage":{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":10,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":10}}}}}`)
	send(`{"type":"response.completed","response":{"id":"first","usage":{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":10,"cache_write_tokens_details":{"ttl_30m_tokens":10,"ttl_12h_tokens":0}}}}}`)
	first := waitTurn()
	require.Equal(t, 10, first.Usage.CacheCreationInputTokens)
	require.Equal(t, 10, first.Usage.CacheCreation30mInputTokens)
	require.Zero(t, first.Usage.CacheCreation12hInputTokens)

	nextTurn(2)
	send(`{"type":"response.created","response":{"id":"second","usage":{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":10,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":10}}}}}`)
	send(`{"type":"response.in_progress","response":{"id":"second","usage":{"input_tokens":10,"output_tokens":0,"input_tokens_details":{"cache_write_tokens":10}}}}`)
	send(`{"type":"response.completed","response":{"id":"second"}}`)
	second := waitTurn()
	require.Equal(t, 10, second.Usage.CacheCreationInputTokens)
	require.Zero(t, second.Usage.CacheCreation30mInputTokens)
	require.Zero(t, second.Usage.CacheCreation12hInputTokens)

	nextTurn(3)
	send(`{"type":"response.completed","response":{"id":"third","usage":{"input_tokens":20,"output_tokens":0,"prompt_tokens_details":{"cache_write_tokens":20,"cache_write_tokens_details":{"ttl_30m_tokens":0,"ttl_12h_tokens":20}}}}}`)
	third := waitTurn()
	require.Equal(t, 20, third.Usage.CacheCreationInputTokens)
	require.Zero(t, third.Usage.CacheCreation30mInputTokens)
	require.Equal(t, 20, third.Usage.CacheCreation12hInputTokens)
	cancel()
	select {
	case result := <-results:
		require.Equal(t, 40, result.Usage.CacheCreationInputTokens)
		require.Equal(t, 10, result.Usage.CacheCreation30mInputTokens)
		require.Equal(t, 20, result.Usage.CacheCreation12hInputTokens)
	case <-time.After(time.Second):
		t.Fatal("relay did not stop")
	}
}
