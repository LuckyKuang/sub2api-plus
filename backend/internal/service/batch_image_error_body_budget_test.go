//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchImageProviderErrorsBoundNonEOF429AndPreserveTypedStatus(t *testing.T) {
	for _, provider := range []string{"gemini", "vertex"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			canceled := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/next" {
					_, _ = io.WriteString(w, "complete")
					return
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{"message":"limited","status":"RESOURCE_EXHAUSTED"}}`)
				_ = http.NewResponseController(w).Flush()
				<-r.Context().Done()
				close(canceled)
			}))
			defer srv.Close()
			defer srv.CloseClientConnections()
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.MaxConnsPerHost = 1
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			req, err := http.NewRequestWithContext(context.WithoutCancel(t.Context()), http.MethodDelete, srv.URL+"/held", nil)
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				if provider == "gemini" {
					done <- NewGeminiBatchHTTPClient(srv.URL, client).doNoBody(req)
				} else {
					done <- doVertexNoBody(client, req)
				}
			}()
			select {
			case err := <-done:
				if provider == "gemini" {
					var typed *GeminiAPIError
					require.ErrorAs(t, err, &typed)
					require.Equal(t, http.StatusTooManyRequests, typed.StatusCode)
					require.Equal(t, "RESOURCE_EXHAUSTED", typed.Code)
					require.Equal(t, "limited", typed.Message)
				} else {
					var typed *VertexAPIError
					require.ErrorAs(t, err, &typed)
					require.Equal(t, http.StatusTooManyRequests, typed.StatusCode)
					require.Equal(t, "RESOURCE_EXHAUSTED", typed.Code)
					require.Equal(t, "limited", typed.Message)
				}
			case <-time.After(7 * time.Second):
				t.Fatal("the batch provider error retained its worker beyond the diagnostic budget")
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("the batch provider retained the rejected network response")
			}
			require.NoError(t, req.Context().Err(), "error reads cannot cancel a later worker attempt")
			nextCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			next, err := http.NewRequestWithContext(nextCtx, http.MethodGet, srv.URL+"/next", nil)
			require.NoError(t, err)
			response, err := client.Do(next)
			require.NoError(t, err, "the batch client's only connection must be available again")
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, "complete", string(body))
		})
	}
}
