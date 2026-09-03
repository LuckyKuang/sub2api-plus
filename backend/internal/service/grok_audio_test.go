package service

import (
	"context"
	"io"
	"sync/atomic"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestBuildGrokVoiceURL_UsesAPIDefaultForCLIProxyBase(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": xai.DefaultCLIBaseURL,
		},
	}
	url, err := buildGrokVoiceURL(account, nil, "tts")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/tts", url)

	url, err = buildGrokVoiceURL(account, nil, "realtime")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/realtime", url)
}

func TestBuildGrokVoiceURL_EmptyBaseFallsBackToAPI(t *testing.T) {
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{},
	}
	url, err := buildGrokVoiceURL(account, nil, "stt")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/stt", url)
}

func TestBuildGrokVoiceURL_RequiresEndpoint(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	_, err := buildGrokVoiceURL(account, nil, "  ")
	require.Error(t, err)
}

func TestBuildGrokVoiceURL_EncodesCustomVoicePathSegments(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	got, err := buildGrokVoiceURL(account, nil, "custom-voices/nlbqfwie/audio")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/custom-voices/nlbqfwie/audio", got)

	_, err = buildGrokVoiceURL(account, nil, "custom-voices/../audio")
	require.Error(t, err)
}

func TestForwardGrokVoice_RejectsNonGrok(t *testing.T) {
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &Account{Platform: PlatformOpenAI}, "tts", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestAwaitGrokRealtimeAudioObservedReadsFlagAfterRelayExits(t *testing.T) {
	errCh := make(chan error, 1)
	var observed atomic.Bool
	go func() {
		observed.Store(true)
		errCh <- io.EOF
	}()
	got, err := awaitGrokRealtimeAudioObserved(errCh, &observed)
	require.ErrorIs(t, err, io.EOF)
	require.True(t, got, "audioObserved must be read after the relay returns, not before <-errCh")
}

func TestGrokRealtimeEventHasAudio(t *testing.T) {
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"session.created"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio_transcript.delta","delta":"hi"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":""}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":"abc"}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.output_audio.delta","audio":"abc"}`)))
}

func TestObserveGrokRealtimeUpstreamTurnClassifiesTerminalStatus(t *testing.T) {
	var accepted, completed, failed []string
	observer := &GrokRealtimeTurnObserver{
		Accepted:  func(id string) { accepted = append(accepted, id) },
		Completed: func(id string) { completed = append(completed, id) },
		Failed:    func(id string) { failed = append(failed, id) },
	}

	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.created","response":{"id":"resp-a"}}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.done","response":{"id":"resp-a","status":"completed"}}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.done","response":{"id":"resp-b","status":"failed"}}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.done","response":{"id":"resp-c","status":"cancelled"}}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.done","response":{"id":"resp-d","status":"incomplete"}}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.failed","response":{"id":"resp-e"}}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`{"type":"response.completed","response_id":"resp-f"}`), observer)
	observeGrokRealtimeUpstreamTurn([]byte(`not-json`), observer)

	require.Equal(t, []string{"resp-a"}, accepted)
	require.Equal(t, []string{"resp-a", "resp-f"}, completed)
	require.Equal(t, []string{"resp-b", "resp-c", "resp-d", "resp-e"}, failed)
}

func TestForwardGrokVoice_RejectsUnknownEndpoint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &Account{Platform: PlatformGrok}, "unknown", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}
