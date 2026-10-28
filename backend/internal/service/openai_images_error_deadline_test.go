//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

// ChatGPT image downloads use a separate client, so the shared HTTPUpstream
// transport alone cannot protect their error-body classification and release.
func TestOpenAIImageStatusErrorBoundsNonEOFRejectedDownload(t *testing.T) {
	t.Parallel()
	const diagnostic = `{"error":{"message":"rate limit"}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/next" {
			_, _ = io.WriteString(w, "ready")
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, diagnostic)
		_ = http.NewResponseController(w).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	defer srv.CloseClientConnections()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 1
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	response, err := client.Get(srv.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	done := make(chan error, 1)
	go func() {
		done <- newOpenAIImageStatusError(&req.Response{Response: response}, "image download failed", 8192)
	}()
	select {
	case err := <-done:
		var statusErr *openAIImageStatusError
		require.ErrorAs(t, err, &statusErr)
		require.Equal(t, http.StatusTooManyRequests, statusErr.StatusCode)
		require.Equal(t, diagnostic, string(statusErr.ResponseBody))
		require.Equal(t, "rate limit", statusErr.Message)
	case <-time.After(7 * time.Second):
		t.Fatal("a rejected image download held its worker while reading diagnostics")
	}
	client.Timeout = time.Second
	next, err := client.Get(srv.URL + "/next")
	require.NoError(t, err, "download rejection must release the only transport slot")
	defer next.Body.Close()
	body, err := io.ReadAll(next.Body)
	require.NoError(t, err)
	require.Equal(t, "ready", string(body))
}
