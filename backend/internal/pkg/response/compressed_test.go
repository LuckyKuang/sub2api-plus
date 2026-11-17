//go:build unit

package response

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func serveCompressed(t *testing.T, acceptEncoding string, data any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available?view=catalog", nil)
	if acceptEncoding != "" {
		c.Request.Header.Set("Accept-Encoding", acceptEncoding)
	}
	SuccessCompressed(c, data)
	return recorder
}

func bulkyPayload() map[string]any {
	return map[string]any{"groups": strings.Repeat(`{"model":"gpt-5.4","rate":1}`, 4000)}
}

func TestSuccessCompressedGzipsLargeBodiesForGzipClients(t *testing.T) {
	data := bulkyPayload()
	recorder := serveCompressed(t, "gzip, deflate, br", data)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "gzip", recorder.Header().Get("Content-Encoding"))
	require.Equal(t, "Accept-Encoding", recorder.Header().Get("Vary"))
	reader, err := gzip.NewReader(bytes.NewReader(recorder.Body.Bytes()))
	require.NoError(t, err)
	plain, err := io.ReadAll(reader)
	require.NoError(t, err)
	// Same envelope as the uncompressed Success helper.
	var envelope Response
	require.NoError(t, json.Unmarshal(plain, &envelope))
	require.Equal(t, 0, envelope.Code)
	require.Equal(t, "success", envelope.Message)
	require.Less(t, recorder.Body.Len(), len(plain)/10)
}

func TestSuccessCompressedKeepsPlainBodiesWhenNotAcceptedOrSmall(t *testing.T) {
	for name, tc := range map[string]struct {
		accept string
		data   any
	}{
		"no header":      {"", bulkyPayload()},
		"refused":        {"gzip;q=0, identity", bulkyPayload()},
		"other encoding": {"br", bulkyPayload()},
		"small body":     {"gzip", map[string]any{"groups": []string{}}},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := serveCompressed(t, tc.accept, tc.data)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Empty(t, recorder.Header().Get("Content-Encoding"))
			var envelope Response
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(t, "success", envelope.Message)
		})
	}
}

func TestAcceptsGzip(t *testing.T) {
	require.True(t, acceptsGzip("gzip"))
	require.True(t, acceptsGzip("deflate, GZIP;q=0.5"))
	require.True(t, acceptsGzip("*"))
	require.False(t, acceptsGzip("gzip;q=0"))
	require.False(t, acceptsGzip("gzip; q=0.000"))
	require.False(t, acceptsGzip("identity"))
	require.False(t, acceptsGzip(""))
}
