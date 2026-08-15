package service

import (
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestStreamOutputTimingObserveAt(t *testing.T) {
	t.Parallel()

	startedAt := time.Unix(1_700_000_000, 0)

	t.Run("lifecycle output is ignored", func(t *testing.T) {
		t.Parallel()

		var timing streamOutputTiming
		timing.ObserveAt(startedAt, startedAt.Add(20*time.Millisecond), apicompat.StreamOutputObservation{})

		require.Nil(t, timing.firstTokenMs)
		require.Nil(t, timing.lastTokenMs)
		require.Nil(t, timing.firstOutputMs)
		require.Empty(t, timing.firstOutputKind)
	})

	t.Run("text delta sets first token and first output", func(t *testing.T) {
		t.Parallel()

		var timing streamOutputTiming
		timing.ObserveAt(startedAt, startedAt.Add(25*time.Millisecond), apicompat.StreamOutputObservation{
			MeaningfulOutput: true,
			TokenLikeDelta:   true,
			Kind:             apicompat.StreamOutputText,
		})

		require.Equal(t, 25, requireValue(t, timing.firstTokenMs))
		require.Equal(t, 25, requireValue(t, timing.lastTokenMs))
		require.Equal(t, 25, requireValue(t, timing.firstOutputMs))
		require.Equal(t, "text", timing.firstOutputKind)
	})

	t.Run("aggregate output does not set first token", func(t *testing.T) {
		t.Parallel()

		var timing streamOutputTiming
		timing.ObserveAt(startedAt, startedAt.Add(30*time.Millisecond), apicompat.StreamOutputObservation{
			MeaningfulOutput: true,
			Kind:             apicompat.StreamOutputReasoning,
		})

		require.Nil(t, timing.firstTokenMs)
		require.Nil(t, timing.lastTokenMs)
		require.Equal(t, 30, requireValue(t, timing.firstOutputMs))
		require.Equal(t, "reasoning", timing.firstOutputKind)
	})

	for _, kind := range []apicompat.StreamOutputKind{
		apicompat.StreamOutputImage,
		apicompat.StreamOutputAudio,
	} {
		kind := kind
		t.Run("media output only sets first output "+string(kind), func(t *testing.T) {
			t.Parallel()

			var timing streamOutputTiming
			timing.ObserveAt(startedAt, startedAt.Add(35*time.Millisecond), apicompat.StreamOutputObservation{
				MeaningfulOutput: true,
				Kind:             kind,
			})

			require.Nil(t, timing.firstTokenMs)
			require.Nil(t, timing.lastTokenMs)
			require.Equal(t, 35, requireValue(t, timing.firstOutputMs))
			require.Equal(t, string(kind), timing.firstOutputKind)
		})
	}

	t.Run("later output cannot overwrite first output", func(t *testing.T) {
		t.Parallel()

		var timing streamOutputTiming
		timing.ObserveAt(startedAt, startedAt.Add(10*time.Millisecond), apicompat.StreamOutputObservation{
			MeaningfulOutput: true,
			Kind:             apicompat.StreamOutputImage,
		})
		timing.ObserveAt(startedAt, startedAt.Add(40*time.Millisecond), apicompat.StreamOutputObservation{
			MeaningfulOutput: true,
			TokenLikeDelta:   true,
			Kind:             apicompat.StreamOutputText,
		})
		timing.ObserveAt(startedAt, startedAt.Add(60*time.Millisecond), apicompat.StreamOutputObservation{
			MeaningfulOutput: true,
			TokenLikeDelta:   true,
			Kind:             apicompat.StreamOutputTool,
		})

		require.Equal(t, 40, requireValue(t, timing.firstTokenMs))
		require.Equal(t, 60, requireValue(t, timing.lastTokenMs))
		require.Equal(t, 10, requireValue(t, timing.firstOutputMs))
		require.Equal(t, "image", timing.firstOutputKind)
	})

	t.Run("negative elapsed time is clamped to zero", func(t *testing.T) {
		t.Parallel()

		var timing streamOutputTiming
		timing.ObserveAt(startedAt, startedAt.Add(-time.Millisecond), apicompat.StreamOutputObservation{
			MeaningfulOutput: true,
			TokenLikeDelta:   true,
			Kind:             apicompat.StreamOutputTool,
		})

		require.Zero(t, requireValue(t, timing.firstTokenMs))
		require.Zero(t, requireValue(t, timing.lastTokenMs))
		require.Zero(t, requireValue(t, timing.firstOutputMs))
		require.Equal(t, "tool", timing.firstOutputKind)
	})
}

func TestStreamOutputTimingApplyResults(t *testing.T) {
	t.Parallel()

	firstTokenMs := 20
	lastTokenMs := 80
	firstOutputMs := 5
	timing := &streamOutputTiming{
		firstTokenMs:    &firstTokenMs,
		lastTokenMs:     &lastTokenMs,
		firstOutputMs:   &firstOutputMs,
		firstOutputKind: "image",
	}

	openAIResult := &OpenAIForwardResult{}
	timing.ApplyOpenAIResult(openAIResult)
	require.Equal(t, &firstTokenMs, openAIResult.FirstTokenMs)
	require.Equal(t, &lastTokenMs, openAIResult.LastTokenMs)
	require.Equal(t, &firstOutputMs, openAIResult.FirstOutputMs)
	require.Equal(t, "image", openAIResult.FirstOutputKind)

	forwardResult := &ForwardResult{}
	timing.ApplyForwardResult(forwardResult)
	require.Equal(t, &firstTokenMs, forwardResult.FirstTokenMs)
	require.Equal(t, &lastTokenMs, forwardResult.LastTokenMs)
	require.Equal(t, &firstOutputMs, forwardResult.FirstOutputMs)
	require.Equal(t, "image", forwardResult.FirstOutputKind)

	require.NotPanics(t, func() {
		var nilTiming *streamOutputTiming
		nilTiming.ObserveAt(time.Time{}, time.Time{}, apicompat.StreamOutputObservation{MeaningfulOutput: true})
		nilTiming.ApplyOpenAIResult(openAIResult)
		nilTiming.ApplyForwardResult(forwardResult)
		timing.ApplyOpenAIResult(nil)
		timing.ApplyForwardResult(nil)
	})
}

func requireValue(t *testing.T, value *int) int {
	t.Helper()
	require.NotNil(t, value)
	return *value
}
