package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

func observeAnthropicSSEOutput(data []byte) apicompat.StreamOutputObservation {
	var result apicompat.StreamOutputObservation
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var event apicompat.AnthropicStreamEvent
		if json.Unmarshal([]byte(payload), &event) != nil {
			continue
		}
		observation := apicompat.ObserveAnthropicOutput(&event)
		if !result.MeaningfulOutput {
			result = observation
		} else {
			result.TokenLikeDelta = result.TokenLikeDelta || observation.TokenLikeDelta
		}
	}
	return result
}

type streamOutputTiming struct {
	onFirstOutput   func(int, string)
	firstByteMs     *int
	firstTokenMs    *int
	lastTokenMs     *int
	firstOutputMs   *int
	firstOutputKind string
}

type liveOutputObserverKey struct{}

func WithLiveOutputObserver(ctx context.Context, observer func(int, int, string)) context.Context {
	return context.WithValue(ctx, liveOutputObserverKey{}, observer)
}

func newStreamOutputTiming(ctx context.Context, turn ...int) streamOutputTiming {
	if ctx == nil {
		return streamOutputTiming{}
	}
	observer, _ := ctx.Value(liveOutputObserverKey{}).(func(int, int, string))
	logicalTurn := 0
	if len(turn) > 0 {
		logicalTurn = turn[0]
	}
	return streamOutputTiming{onFirstOutput: func(ms int, kind string) {
		if observer != nil {
			observer(logicalTurn, ms, kind)
		}
	}}
}

func requestStreamOutputTiming(c *gin.Context, turn ...int) streamOutputTiming {
	if c == nil || c.Request == nil {
		return streamOutputTiming{}
	}
	return newStreamOutputTiming(c.Request.Context(), turn...)
}

func (t *streamOutputTiming) Observe(startedAt time.Time, observation apicompat.StreamOutputObservation) {
	t.ObserveAt(startedAt, time.Now(), observation)
}

func (t *streamOutputTiming) ObserveAt(startedAt, observedAt time.Time, observation apicompat.StreamOutputObservation) {
	if t == nil || !observation.MeaningfulOutput {
		return
	}
	elapsed := int(observedAt.Sub(startedAt).Milliseconds())
	if elapsed < 0 {
		elapsed = 0
	}
	if t.firstOutputMs == nil {
		value := elapsed
		t.firstOutputMs = &value
		t.firstOutputKind = string(observation.Kind)
		if t.onFirstOutput != nil {
			t.onFirstOutput(elapsed, t.firstOutputKind)
		}
	}
	if observation.TokenLikeDelta {
		value := elapsed
		if t.firstTokenMs == nil {
			t.firstTokenMs = &value
		}
		t.lastTokenMs = laterTokenMs(t.lastTokenMs, elapsed)
	}
}

func laterTokenMs(current *int, candidate int) *int {
	if current == nil || candidate >= *current {
		value := candidate
		return &value
	}
	return current
}

func (t *streamOutputTiming) ApplyOpenAIResult(result *OpenAIForwardResult) {
	if t == nil || result == nil {
		return
	}
	if t.firstByteMs != nil {
		result.FirstByteMs = t.firstByteMs
	}
	result.FirstTokenMs = t.firstTokenMs
	result.LastTokenMs = t.lastTokenMs
	result.FirstOutputMs = t.firstOutputMs
	result.FirstOutputKind = t.firstOutputKind
}

func (t *streamOutputTiming) ApplyForwardResult(result *ForwardResult) {
	if t == nil || result == nil {
		return
	}
	if t.firstByteMs != nil {
		result.FirstByteMs = t.firstByteMs
	}
	result.FirstTokenMs = t.firstTokenMs
	result.LastTokenMs = t.lastTokenMs
	result.FirstOutputMs = t.firstOutputMs
	result.FirstOutputKind = t.firstOutputKind
}

// ObserveFrame is used only after the frame has been attributed to this turn.
func (t *streamOutputTiming) ObserveFrame(start, at time.Time) {
	if t == nil || t.firstByteMs != nil {
		return
	}
	ms := int(at.Sub(start).Milliseconds())
	if ms < 0 {
		ms = 0
	}
	t.firstByteMs = &ms
}
