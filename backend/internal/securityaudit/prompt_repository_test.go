package securityaudit

import (
	"testing"
	"time"
)

func TestShouldStorePromptAuditEvent(t *testing.T) {
	tests := []struct {
		name            string
		storePassEvents bool
		decision        EventDecision
		want            bool
	}{
		{name: "pass disabled", storePassEvents: false, decision: EventPass, want: false},
		{name: "flag disabled", storePassEvents: false, decision: EventFlag, want: true},
		{name: "critical disabled", storePassEvents: false, decision: EventCritical, want: true},
		{name: "pass enabled", storePassEvents: true, decision: EventPass, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStorePromptAuditEvent(tt.decision, tt.storePassEvents); got != tt.want {
				t.Fatalf("shouldStorePromptAuditEvent(%q, %t) = %t, want %t", tt.decision, tt.storePassEvents, got, tt.want)
			}
		})
	}
}

func TestQueueDelayMilliseconds(t *testing.T) {
	created := time.Unix(100, 0).UTC()
	started := created.Add(2345 * time.Millisecond)

	if got := queueDelayMilliseconds(&Job{CreatedAt: created, ProcessingStartedAt: &started}); got != 2345 {
		t.Fatalf("queueDelayMilliseconds() = %d, want 2345", got)
	}
	if got := queueDelayMilliseconds(&Job{CreatedAt: created}); got != 0 {
		t.Fatalf("queueDelayMilliseconds() without claim = %d, want 0", got)
	}
}
