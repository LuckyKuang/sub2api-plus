//go:build unit

package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codexBridgeSwitchRepo struct {
	*openAIIdentitySettingRepoStub
	enabled atomic.Bool
}

type codexBridgeWSLiveAccountRepo struct {
	AccountRepository
	owner   *Account
	enabled atomic.Bool
}

func (r *codexBridgeWSLiveAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if id != r.owner.ID {
		return nil, ErrAccountNotFound
	}
	row := snapshotRequestCredentialAccount(r.owner)
	row.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": r.enabled.Load()}
	// Live row values must not replace the acquired transport identity.
	row.Credentials["access_token"] = "different-live-access"
	row.Credentials["chatgpt_account_id"] = "different-live-workspace"
	row.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Debian 12; arm64) live-terminal"
	return row, nil
}

// The owner snapshot freezes credentials and identity, but never freezes an
// administrator's current permission to submit the next native generation.
func TestCodexCLIBridgeNativeWSReadsLiveAccountToggleBothDirections(t *testing.T) {
	svc, _ := newResponsesWSInflightTransportTestService()
	settings := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	settings.enabled.Store(true)
	svc.settingService = &SettingService{settingRepo: settings}
	svc.settingService.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: true})
	owner := enabledCodexCLIExecutionAccount(1703, "acquired-access", "acquired-workspace")
	owner.Concurrency = 1
	owner.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
	svc.cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	owner.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) owner-terminal"
	rows := &codexBridgeWSLiveAccountRepo{owner: owner}
	svc.accountRepo = rows
	ctx := WithOutboundIdentityScope(context.Background(), nil)
	ctx = withRequestCredentialOwner(ctx, owner, "acquired-access")
	for _, enabled := range []bool{false, true, false, true} {
		rows.enabled.Store(enabled)
		require.Equal(t, OpenAIUpstreamTransportResponsesWebsocketV2, svc.getOpenAIWSProtocolResolver().Resolve(owner).Transport,
			"protocol eligibility must not freeze the stale selected CLI flag; the owner-aware send gate selects the hop")
		if enabled {
			require.ErrorIs(t, svc.requireCodexUpstreamWS(ctx, owner), errCodexUpstreamWSDisabled)
		} else {
			require.NoError(t, svc.requireCodexUpstreamWS(ctx, owner))
		}
		acquired, err := requestCredentialAccount(ctx, owner)
		require.NoError(t, err)
		require.Equal(t, int64(1703), acquired.ID)
		require.Equal(t, "acquired-access", acquired.GetOpenAIAccessToken())
		require.Equal(t, "acquired-workspace", acquired.GetCredential("chatgpt_account_id"))
		require.Equal(t, "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) owner-terminal", acquired.GetCredential("user_agent"))
	}
}

func (r *codexBridgeSwitchRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == SettingKeyCodexCLIBridgeEnabled {
		if r.enabled.Load() {
			return "true", nil
		}
		return "false", nil
	}
	return r.openAIIdentitySettingRepoStub.GetValue(ctx, key)
}

// Enabling the CLI hop cannot permit reuse, dial or prewarm of a native
// connection, even when a connection was acquired while the hop was disabled.
func TestCodexCLIBridgeEnableDrainsNativePool(t *testing.T) {
	svc, _ := newResponsesWSInflightTransportTestService()
	repo := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
	svc.settingService.settingRepo = repo
	account := enabledCodexCLIExecutionAccount(1701, "owner-access", "owner-workspace")
	account.Concurrency = 1
	pool := newOpenAIWSConnPoolWithPolicy(codexTransportTestConfig(), svc.codexUpstreamWSAllowed)
	defer pool.Close()
	dialer := &openAIWSCountingDialer{}
	pool.setClientDialerForTest(dialer)
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://fixture.invalid/responses"}
	lease, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	repo.enabled.Store(true)
	_, err = pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, errCodexUpstreamWSDisabled)
	require.False(t, lease.conn.isClosed(), "accepted generation may complete")
	require.ErrorIs(t, svc.requireCodexUpstreamWS(context.Background(), account), errCodexUpstreamWSDisabled)
	lease.Release()
	pool.ensureTargetIdleAsync(account.ID)
	pool.runBackgroundCleanupSweep(time.Now())
	require.True(t, lease.conn.isClosed())
	_, err = pool.dialConn(context.Background(), req)
	require.ErrorIs(t, err, errCodexUpstreamWSDisabled)
	dialer.mu.Lock()
	count := dialer.dialCount
	dialer.mu.Unlock()
	require.Equal(t, 1, count)
	require.Equal(t, int64(1701), account.ID)
	require.Equal(t, "owner-access", account.GetOpenAIAccessToken())
}

// A completed native passthrough turn must not authorize the next native turn
// after global CLI permission changes. Reconnect preserves the selected owner.
func TestCodexCLIBridgeEnableStopsNativePassthroughNextTurn(t *testing.T) {
	for _, switchSource := range []string{"global", "account"} {
		for _, switchTiming := range []string{"afterTurn", "beforeTurn"} {
			t.Run(switchSource+"/"+switchTiming, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				upstream := newStagedPassthroughConn()
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				svc := newPassthroughLifecycleService(cfg, upstream)
				repo := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
				svc.settingService = &SettingService{settingRepo: repo}
				svc.settingService.storeCodexTransportSettings(CodexTransportSettings{WSEnabled: true})
				// The lifecycle server passes sk-test as the acquired bearer. Its OAuth
				// fixture must own that exact token so the identity boundary can establish
				// the credential owner rather than rejecting ambiguous acquired evidence.
				account := enabledCodexCLIExecutionAccount(1702, "sk-test", "owner-workspace")
				account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
				account.Extra = map[string]any{"responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
				liveAccount := &codexBridgeWSLiveAccountRepo{owner: account}
				liveAccount.enabled.Store(switchSource == "global")
				repo.enabled.Store(switchSource == "account")
				svc.accountRepo = liveAccount
				require.True(t, svc.codexUpstreamWSAllowed(ctx, account))
				require.Equal(t, OpenAIUpstreamTransportResponsesWebsocketV2, svc.getOpenAIWSProtocolResolver().Resolve(account).Transport)
				require.Equal(t, OpenAIWSIngressModePassthrough, account.ResolveOpenAIResponsesWebSocketV2Mode(cfg.Gateway.OpenAIWS.IngressModeDefault))
				enableBridge := func() {
					if switchSource == "global" {
						repo.enabled.Store(true)
					} else {
						liveAccount.enabled.Store(true)
					}
				}
				admissionEvents := make(chan string, 4)
				server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(_ *gin.Context) *OpenAIWSIngressHooks {
					return &OpenAIWSIngressHooks{
						BeforeRequest: func(turn int, _ []byte, _ string) error {
							if turn == 2 {
								admissionEvents <- "audit"
							}
							return nil
						},
						BeforeTurn: func(turn int) error {
							if turn == 2 {
								admissionEvents <- "admission"
								if switchTiming == "beforeTurn" {
									enableBridge()
								}
							}
							return nil
						},
						AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
							if turn == 1 && switchTiming == "afterTurn" {
								enableBridge()
							}
						},
					}
				})
				defer server.Close()
				client := dialPassthroughLifecycleClient(t, server)
				defer client.CloseNow()
				requirePassthroughUpstreamWrite(t, upstream, time.Second)
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_owner","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
				_, err := readPassthroughLifecycleFrame(t, client, time.Second)
				require.NoError(t, err)
				// The adapter may close immediately after the first turn or after reading
				// this continuation; neither path may submit it or replay the first turn.
				_ = client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_owner","input":"continue"}`))
				// Read the close frame so the peer acknowledges the server's close
				// handshake. Waiting only for serverErr would leave coder/websocket.Close
				// blocked waiting for a client response, obscuring the immediate reconnect.
				_, closeReadErr := readPassthroughLifecycleFrame(t, client, time.Second)
				var wireClose coderws.CloseError
				require.ErrorAs(t, closeReadErr, &wireClose)
				require.Equal(t, coderws.StatusServiceRestart, wireClose.Code)
				select {
				case err := <-serverErr:
					var closeErr *OpenAIWSClientCloseError
					require.ErrorAs(t, err, &closeErr)
					require.Equal(t, coderws.StatusServiceRestart, closeErr.StatusCode())
				case <-ctx.Done():
					t.Fatal("native passthrough continued after enabling CLI bridge")
				}
				select {
				case payload := <-upstream.writes:
					t.Fatalf("native continuation or replay after enabling bridge: %s", payload)
				default:
				}
				require.Equal(t, int64(1702), account.ID)
				require.Equal(t, "sk-test", account.GetOpenAIAccessToken())
				if switchTiming == "beforeTurn" {
					require.Len(t, admissionEvents, 2)
					require.Equal(t, "audit", <-admissionEvents, "audit must precede concurrency admission")
					require.Equal(t, "admission", <-admissionEvents, "this regression changes permission inside BeforeTurn")
				}

			})
		}
	}
}
