package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/apicompat"
)

func observeAnthropicSSEOutput(data []byte) apicompat.StreamOutputObservation {
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
		if observation := apicompat.ObserveAnthropicOutput(&event); observation.MeaningfulOutput {
			return observation
		}
	}
	return apicompat.StreamOutputObservation{}
}

type streamOutputTiming struct {
	firstTokenMs    *int
	lastTokenMs     *int
	firstOutputMs   *int
	firstOutputKind string
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
	result.FirstTokenMs = t.firstTokenMs
	result.LastTokenMs = t.lastTokenMs
	result.FirstOutputMs = t.firstOutputMs
	result.FirstOutputKind = t.firstOutputKind
}

func (t *streamOutputTiming) ApplyForwardResult(result *ForwardResult) {
	if t == nil || result == nil {
		return
	}
	result.FirstTokenMs = t.firstTokenMs
	result.LastTokenMs = t.lastTokenMs
	result.FirstOutputMs = t.firstOutputMs
	result.FirstOutputKind = t.firstOutputKind
}
