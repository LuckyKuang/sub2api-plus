//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type codexCLIBridgeTrackedBody struct {
	io.Reader
	closed bool
}

// A stalled bridge error must close its transport within the existing five
// second diagnostic budget, without making the partial status retryable.
func TestCodexCLIBridgeHTTPErrorBodyTimeoutIsTerminal(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	written := make(chan struct{})
	go func() {
		_, _ = io.WriteString(writer, `{"error":{"message":"incomplete"}}`)
		close(written)
	}()
	response := &http.Response{StatusCode: http.StatusTooManyRequests, Body: reader}
	finished := make(chan error, 1)
	go func() {
		_, err := (&OpenAIGatewayService{}).completeCodexCLIBridgeErrorResponse(response, nil)
		finished <- err
	}()
	select {
	case err := <-finished:
		var bridgeErr *codexcli.BridgeError
		require.ErrorAs(t, err, &bridgeErr)
		require.True(t, bridgeErr.RequestSent)
		require.Equal(t, "bridge_response_read_failed", bridgeErr.Code)
	case <-time.After(7 * time.Second):
		_ = reader.Close()
		t.Fatal("bridge error body exceeded its diagnostic read budget")
	}
	<-written
	_, err := writer.Write([]byte("must be closed"))
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

// A fully received provider rejection remains authoritative. The bridge must
// not suppress the existing provider-status failover policy just because the
// request used the official transport hop.
func TestCodexCLIBridgeCompleteHTTPErrorRetainsProviderFailover(t *testing.T) {
	owner := codexBridgeAPIKeyFixture()
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			writes := 0
			gateway := &OpenAIGatewayService{cfg: &config.Config{}, settingService: enabledCodexCLIBridgeTestSettings(), cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector(),
				httpUpstream: &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					writes++
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"provider rejection"}}`))}, nil
				}},
				codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, send func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
					return send(ctx, r)
				}},
			}
			body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
			c := newOpenAIRejectedFieldTestContext(body)
			_, err := gateway.Forward(WithOutboundIdentityScope(t.Context(), nil), c, owner, body)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, status, failover.StatusCode)
			require.Equal(t, 1, writes)
		})
	}
}

func (b *codexCLIBridgeTrackedBody) Close() error {
	b.closed = true
	return nil
}

// docs/CODEX_CLI.md requires complete provider errors to retain their original
// semantics, while local bridge failures must never authorize replay. Exercise
// the transport boundary shared by inference, probes and control-plane sends.
func TestCodexCLIBridgeHTTPErrorBodyCompleteness(t *testing.T) {
	for _, path := range []string{"gateway", "probe", "control-plane"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			for _, scenario := range []string{"complete", "exact-limit", "oversize", "read-failure"} {
				t.Run(path+"/"+http.StatusText(status)+"/"+scenario, func(t *testing.T) {
					owner := codexBridgeAPIKeyFixture()
					payload := `{"error":{"message":"provider rejection"}}`
					if scenario == "exact-limit" || scenario == "oversize" {
						size := 512 << 10 // Independent existing diagnostic byte budget.
						if scenario == "oversize" {
							size++
						}
						payload = strings.Repeat("x", size)
					}
					var reader io.Reader = strings.NewReader(payload)
					if scenario == "read-failure" {
						reader = io.MultiReader(reader, &errReadCloser{err: &codexcli.BridgeError{Code: "bridge_response_read_failed", RequestSent: true}})
					}
					body := &codexCLIBridgeTrackedBody{Reader: reader}
					writes := 0
					forward := func(r *http.Request) (*http.Response, error) {
						writes++
						return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": {"trusted-provider"}}, Body: body, Request: r}, nil
					}
					gateway := &OpenAIGatewayService{settingService: enabledCodexCLIBridgeTestSettings(),
						httpUpstream: &codexModelsHTTPUpstreamStub{do: func(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) { return forward(r) }},
						codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, r *http.Request, send func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
							return send(ctx, r)
						}},
					}
					request, err := http.NewRequestWithContext(WithOutboundIdentityScope(t.Context(), nil), http.MethodPost, "https://compatible.example/v1/responses", strings.NewReader(`{"input":"one attempt"}`))
					require.NoError(t, err)
					var response *http.Response
					switch path {
					case "gateway":
						response, err = gateway.doOpenAIUpstream(request, "", owner)
					case "probe":
						probe := &AccountTestService{openaiGatewayService: gateway, httpUpstream: gateway.httpUpstream}
						response, err = probe.doOpenAIAccountTestUpstream(request, "", owner, false)
					case "control-plane":
						response, err = gateway.doOpenAICodexCLIControlPlane(request, owner, forward)
					}
					require.Equal(t, 1, writes)
					require.NotNil(t, response, "retain the trusted response for credential-owner finalization")
					require.Equal(t, status, response.StatusCode)
					require.Equal(t, "trusted-provider", response.Header.Get("X-Request-Id"))
					require.Equal(t, request.URL.String(), response.Request.URL.String())
					if scenario == "oversize" || scenario == "read-failure" {
						var bridgeErr *codexcli.BridgeError
						require.ErrorAs(t, err, &bridgeErr)
						require.True(t, bridgeErr.RequestSent)
						require.True(t, body.closed, "a terminal diagnostic read must release the provider and CLI resources")
					} else {
						require.NoError(t, err)
						require.True(t, body.closed, "complete diagnostic must release its bridge before provider policy runs")
						got, readErr := io.ReadAll(response.Body)
						require.NoError(t, readErr)
						require.Equal(t, payload, string(got), "complete provider rejection bytes must remain unchanged")
					}
					require.NoError(t, response.Body.Close())
					require.True(t, body.closed)
				})
			}
		}
	}
}
