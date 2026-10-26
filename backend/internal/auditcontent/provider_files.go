package auditcontent

import "encoding/json"

// FileBytes returns only the bounded binary byte count from the gateway's
// sanitized Files audit envelope. Metadata and file contents are never logged.
func FileBytes(protocol string, body []byte) int64 {
	if normalizeProtocol(protocol) != "provider_files" {
		return 0
	}
	var envelope struct {
		File *struct {
			Bytes int64 `json:"bytes"`
		} `json:"file"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.File == nil || envelope.File.Bytes < 0 || envelope.File.Bytes > 64<<20 {
		return 0
	}
	return envelope.File.Bytes
}
