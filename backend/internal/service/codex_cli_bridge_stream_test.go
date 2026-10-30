//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type codexCLIBridgeCountingErrorBody struct {
	io.ReadCloser
	reads int
}

type codexCLIBridgeStreamAccountRepo struct {
	*openaiTransportAccountRepoStub
	owner *Account
}

func (r *codexCLIBridgeStreamAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if id != r.owner.ID {
		return nil, ErrAccountNotFound
	}
	return snapshotRequestCredentialAccount(r.owner), nil
}

func (b *codexCLIBridgeCountingErrorBody) Read(p []byte) (int, error) {
	b.reads++
	return b.ReadCloser.Read(p)
}

// Once the official proxy submitted a request, even a failure before the first
// response byte is ambiguous. Every public adapter must preserve that failure,
// avoid account failover/replay and leave the credential owner schedulable.
func TestCodexCLIBridgeSubmittedReadFailureNeverFailsOver(t *testing.T) {
	for _, path := range []string{"responses", "passthrough", "chat-buffered", "chat-streaming", "messages-buffered", "messages-streaming", "ws-http-bridge"} {
		t.Run(path, func(t *testing.T) {
			bridgeErr := &codexcli.BridgeError{Code: "bridge_response_read_failed", RequestSent: true}
			bodyReader := &codexCLIBridgeCountingErrorBody{ReadCloser: &errReadCloser{err: bridgeErr}}
			upstreamCalls, bridgeCalls := 0, 0
			account := enabledCodexCLIExecutionAccount(101, "access", "workspace")
			repo := &codexCLIBridgeStreamAccountRepo{openaiTransportAccountRepoStub: &openaiTransportAccountRepoStub{}, owner: account}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			upstream := &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, ownerID int64, _ int) (*http.Response, error) {
				upstreamCalls++
				require.Equal(t, int64(101), ownerID)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: bodyReader}, nil
			}}
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, settingService: enabledCodexCLIBridgeTestSettings(), httpUpstream: upstream, cache: &stubGatewayCache{}, toolCorrector: NewCodexToolCorrector(),
				codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
					bridgeCalls++
					return forward(ctx, req)
				}},
			}
			ctx := WithOutboundIdentityScope(context.Background(), nil)
			body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
			if path == "passthrough" {
				account.Extra = map[string]any{"openai_passthrough": true}
			}
			stream := path == "chat-streaming" || path == "messages-streaming"
			if path == "chat-buffered" || path == "chat-streaming" {
				body = []byte(`{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}],"stream":false}`)
				if stream {
					body = []byte(`{"model":"gpt-5.1","messages":[{"role":"user","content":"hello"}],"stream":true}`)
				}
			}
			if path == "messages-buffered" || path == "messages-streaming" {
				body = []byte(`{"model":"gpt-5.1","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				if stream {
					body = []byte(`{"model":"gpt-5.1","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"stream":true}`)
				}
			}
			c := newOpenAIRejectedFieldTestContext(body)
			var err error
			switch path {
			case "chat-buffered", "chat-streaming":
				_, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
			case "messages-buffered", "messages-streaming":
				_, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			case "ws-http-bridge":
				body = []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)
				_, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "access", body, len(body), "gpt-5.1", "", "", "", "", 1, func([]byte) error { return nil })
			default:
				_, err = svc.Forward(ctx, c, account, body)
			}
			require.Error(t, err)
			require.Positive(t, bodyReader.reads, "must exercise the actual response reader")
			require.Equal(t, 1, bridgeCalls, "ambiguous submission must not be replayed")
			require.Equal(t, 1, upstreamCalls)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "submitted bridge failure cannot authorize account failover")
			var submitted *codexcli.BridgeError
			require.ErrorAs(t, err, &submitted)
			require.True(t, submitted.RequestSent)
			require.Empty(t, repo.tempUnschedCalls)
		})
	}
}
