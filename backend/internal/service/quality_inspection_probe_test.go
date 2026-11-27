//go:build unit

package service

import (
	"context"
	"testing"
)

func TestClassifyQualityProbeError(t *testing.T) {
	cases := []struct {
		name       string
		message    string
		wantCode   string
		wantClass  string
		wantStatus string
	}{
		{"rate limited", "Chat Completions API returned 429: too many requests", "HTTP_429", QualityFailureUpstream, QualityStatusQuota},
		{"rate limit wording", "rate limit exceeded", "HTTP_429", QualityFailureUpstream, QualityStatusQuota},
		{"insufficient quota", "insufficient quota for this model", "QUOTA_EXHAUSTED", QualityFailureUpstream, QualityStatusQuota},
		{"chinese quota", "账户额度不足，请充值", "QUOTA_EXHAUSTED", QualityFailureUpstream, QualityStatusQuota},
		{"unauthorized", "401 Unauthorized", "HTTP_401", QualityFailureLocal, QualityStatusError},
		{"forbidden", "403 forbidden", "HTTP_403", QualityFailureLocal, QualityStatusError},
		{"timeout", "context deadline exceeded (timeout)", "REQUEST_FAILED", QualityFailureUpstream, QualityStatusError},
		{"connection reset", "connection reset by peer", "REQUEST_FAILED", QualityFailureUpstream, QualityStatusError},
		{"empty answer", "模型返回空回答", "REQUEST_FAILED", QualityFailureUpstream, QualityStatusError},
		{"server error", "upstream returned 503", "HTTP_503", QualityFailureUpstream, QualityStatusError},
		{"unclassified", "unexpected local configuration problem", "REQUEST_FAILED", QualityFailureLocal, QualityStatusError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, class, status := classifyQualityProbeError(tc.message)
			if code != tc.wantCode || class != tc.wantClass || status != tc.wantStatus {
				t.Fatalf("got (%s, %s, %s), want (%s, %s, %s)",
					code, class, status, tc.wantCode, tc.wantClass, tc.wantStatus)
			}
		})
	}
}

func TestQualityProbe_NilTester(t *testing.T) {
	probe := NewQualityProbe(nil)
	if _, err := probe.Run(context.Background(), 1, "m", "p"); err == nil {
		t.Fatal("nil tester must return an error instead of panicking")
	}
}

func TestProbeRecorder_TracksFirstContentOffset(t *testing.T) {
	recorder := newProbeRecorder()
	if _, err := recorder.WriteString(`data: {"type":"test_start"}` + "\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := recorder.WriteString(`data: {"type":"content","text":"hi"}` + "\n\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	offset := recorder.FirstEventOffset()
	if offset < 0 {
		t.Fatal("content event offset not found")
	}
	if _, ok := recorder.OffsetTime(offset); !ok {
		t.Fatal("offset must map to a write timestamp")
	}
	empty := newProbeRecorder()
	if empty.FirstEventOffset() != -1 {
		t.Fatal("empty recorder must report -1")
	}
	if _, ok := empty.OffsetTime(0); ok {
		t.Fatal("empty recorder has no timestamps")
	}
}
