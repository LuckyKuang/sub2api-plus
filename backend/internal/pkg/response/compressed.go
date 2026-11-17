package response

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// compressMinBytes keeps small envelopes on the plain path; gzip only pays off
// once the body is large enough to dominate transfer time.
const compressMinBytes = 32 << 10

// SuccessCompressed writes the same envelope as Success, gzip-encoded when the
// client accepts it and the body is large. Use it for bulky read-only payloads
// (for example the available-channel catalog, ~1.3 MB of highly repetitive
// JSON that compresses to a few percent); streaming endpoints must not use it.
func SuccessCompressed(c *gin.Context, data any) {
	body, err := json.Marshal(Response{Code: 0, Message: "success", Data: data})
	if err != nil {
		// Fall back to gin's renderer, which reports the marshal failure.
		Success(c, data)
		return
	}
	c.Header("Vary", "Accept-Encoding")
	if len(body) < compressMinBytes || !acceptsGzip(c.GetHeader("Accept-Encoding")) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
		return
	}
	var compressed bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if _, err := writer.Write(body); err != nil || writer.Close() != nil {
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
		return
	}
	c.Header("Content-Encoding", "gzip")
	c.Header("Content-Length", strconv.Itoa(compressed.Len()))
	c.Data(http.StatusOK, "application/json; charset=utf-8", compressed.Bytes())
}

// acceptsGzip reports whether an Accept-Encoding header allows gzip, honouring
// an explicit q=0 refusal.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "gzip" && token != "*" {
			continue
		}
		q := strings.ReplaceAll(strings.ToLower(params), " ", "")
		if q == "q=0" || strings.HasPrefix(q, "q=0.0") && strings.Trim(strings.TrimPrefix(q, "q="), "0.") == "" {
			continue
		}
		return true
	}
	return false
}
