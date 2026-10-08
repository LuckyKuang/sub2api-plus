package service

import "github.com/gin-gonic/gin"

const opsSemanticOutputCommittedKey = "ops_semantic_output_committed"

// Timing and output belong to the current credential owner/attempt, never to
// an earlier failed attempt or WebSocket turn.
func resetOpsOpenAIStreamObservation(c *gin.Context) {
	if c == nil {
		return
	}
	c.Set(OpsTimeToFirstTokenMsKey, nil)
	c.Set(opsSemanticOutputCommittedKey, false)
}

func observeOpsOpenAIStream(c *gin.Context, firstToken *int, semanticCommitted bool) {
	if c == nil {
		return
	}
	if firstToken != nil {
		SetOpsLatencyMs(c, OpsTimeToFirstTokenMsKey, int64(*firstToken))
	}
	if semanticCommitted {
		c.Set(opsSemanticOutputCommittedKey, true)
	}
}

func snapshotOpsOpenAIStream(c *gin.Context, event *OpsUpstreamErrorEvent) {
	if c == nil || event == nil {
		return
	}
	if value, ok := c.Get(opsSemanticOutputCommittedKey); ok {
		if committed, ok := value.(bool); ok {
			event.SemanticOutputCommitted = &committed
			if committed {
				event.ReplaySuppressedReason = "semantic_output_committed"
			}
		}
	}
	if value, ok := c.Get(OpsTimeToFirstTokenMsKey); ok {
		if ms, ok := value.(int64); ok && ms >= 0 {
			event.TimeToFirstTokenMs = &ms
		}
	}
}
