//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/codexcli"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/stretchr/testify/require"
)

type codexBridgeHotAccountRepo struct {
	AccountRepository
	owners map[int64]*Account
	flags  map[int64]*atomic.Bool
}

// A probe without an injected gateway must still read the live account flag;
// an acquired enabled snapshot is not authority after the account is disabled.
func TestCodexCLIBridgeProbeFallbackReadsLiveAccountFlag(t *testing.T) {
	owner := enabledCodexCLIExecutionAccount(101, "acquired-token", "workspace")
	flag := &atomic.Bool{}
	rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{101: owner}, flags: map[int64]*atomic.Bool{101: flag}}
	settings := &SettingService{settingRepo: &codexCLIBridgeSettingRepo{readErr: errors.New("private-setting-storage")}}
	writes := 0
	probe := &AccountTestService{accountRepo: rows, settingService: settings,
		httpUpstream: &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			writes++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("accepted"))}, nil
		}},
	}
	for _, enabled := range []bool{false, true, false} {
		flag.Store(enabled)
		req, err := http.NewRequestWithContext(WithOutboundIdentityScope(context.Background(), nil), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"input":"same owner"}`))
		require.NoError(t, err)
		before := writes
		resp, err := probe.doOpenAIAccountTestUpstream(req, "", owner, false)
		if enabled {
			require.ErrorIs(t, err, ErrCodexCLIBridgeSettingUnavailable)
			require.Nil(t, resp)
			require.Equal(t, before, writes)
		} else {
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, before+1, writes)
		}
	}
	require.True(t, CodexCLITaskAccountEnabled(owner))
}

func (r *codexBridgeHotAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	owner := r.owners[id]
	if owner == nil {
		return nil, ErrAccountNotFound
	}
	row := snapshotRequestCredentialAccount(owner)
	row.Credentials[CodexCLIConfigCredentialKey] = map[string]any{"enabled": r.flags[id].Load()}
	// A live settings lookup may only change the transport flag. These newer
	// values must never replace the acquired owner's immutable wire snapshot.
	row.Credentials["access_token"] = "unacquired-live-token"
	row.Credentials["chatgpt_account_id"] = "unacquired-live-workspace"
	row.Credentials["user_agent"] = "codex_cli_rs/0.165.0 (Debian 12; arm64) xterm-256color"
	row.ProxyID = nil
	row.Proxy = nil
	return row, nil
}

// User requirement: either explicit disable restores the normal unified sender
// for the next request; enabling both flags affects already-used accounts too.
// Expectations are an independent transition table. Real local HTTP requests
// prove bearer/workspace/identity/body stay coherent on both transport choices.
func TestCodexCLIBridgeHotSwitchHTTPAndProbeUseCurrentFlags(t *testing.T) {
	for _, path := range []string{"gateway", "probe"} {
		t.Run(path, func(t *testing.T) {
			global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexUserAgent: "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color",
			}}}
			settings := &SettingService{settingRepo: global}
			rows := &codexBridgeHotAccountRepo{owners: make(map[int64]*Account), flags: make(map[int64]*atomic.Bool)}
			contexts := make(map[int64]context.Context)
			selectedRows := make(map[int64]*Account)
			for _, id := range []int64{101, 202} {
				owner := enabledCodexCLIExecutionAccount(id, fmt.Sprintf("acquired-token-%d", id), fmt.Sprintf("acquired-workspace-%d", id))
				owner.Credentials[CodexCLIConfigCredentialKey].(map[string]any)["enabled"] = false
				if id == 101 {
					owner.Credentials["user_agent"] = "codex_vscode/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color"
				} else {
					delete(owner.Credentials, "user_agent")
				}
				proxyID := id + 1000
				owner.ProxyID = &proxyID
				owner.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: fmt.Sprintf("owner-%d.invalid", id), Port: 8080, Status: StatusActive}
				rows.owners[id], rows.flags[id] = owner, &atomic.Bool{}
				selected := owner
				if id == 101 {
					selected = &Account{ID: 1101, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &owner.ID, Concurrency: 7}
				}
				ctx := WithOutboundIdentityScope(context.Background(), nil)
				recordRequestCredentialOwner(ctx, owner, fmt.Sprintf("acquired-token-%d", id))
				contexts[id] = withRequestCredentialOwner(ctx, selected, fmt.Sprintf("acquired-token-%d", id))
				selectedRows[id] = selected
			}
			var expectedID int64
			var wireCalls atomic.Int32
			var bridgeCalls int
			var expectedBody string
			type observation struct {
				header http.Header
				body   string
				err    error
			}
			observations := make(chan observation, 14)
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wireCalls.Add(1)
				body, err := io.ReadAll(r.Body)
				select {
				case observations <- observation{header: r.Header.Clone(), body: string(body), err: err}:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "accepted")
			}))
			defer provider.Close()
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, selectedID int64, concurrency int) (*http.Response, error) {
				require.Equal(t, fmt.Sprintf("http://owner-%d.invalid:8080", expectedID), proxy)
				require.Equal(t, selectedRows[expectedID].ID, selectedID)
				require.Equal(t, selectedRows[expectedID].Concurrency, concurrency)
				return http.DefaultClient.Do(req)
			}}
			gateway := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: rows, settingService: settings, httpUpstream: upstream,
				codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
					bridgeCalls++
					return forward(ctx, req)
				}},
			}
			probe := &AccountTestService{accountRepo: rows, openaiGatewayService: gateway, settingService: settings, httpUpstream: upstream}
			for step, tc := range []struct {
				global, first, second bool
				wantFirst, wantSecond bool
			}{
				{false, true, true, false, false},
				{true, true, true, true, true},
				{false, true, true, false, false},
				{true, false, true, false, true},
				{true, true, false, true, false},
				{true, false, false, false, false},
				{true, true, true, true, true},
			} {
				global.enabled.Store(tc.global)
				rows.flags[101].Store(tc.first)
				rows.flags[202].Store(tc.second)
				for index, id := range []int64{101, 202} {
					expectedID = id
					expectedBody = fmt.Sprintf(`{"input":"step-%d-owner-%d","previous_response_id":"same-owner-continuation"}`, step, id)
					req, err := http.NewRequestWithContext(contexts[id], http.MethodPost, provider.URL, strings.NewReader(expectedBody))
					require.NoError(t, err)
					req.Header.Set("Authorization", fmt.Sprintf("Bearer acquired-token-%d", id))
					req.Header.Set("Chatgpt-Account-Id", fmt.Sprintf("acquired-workspace-%d", id))
					req.Header.Set("User-Agent", "forged-inbound/999")
					gateway.applyOpenAIOutboundIdentity(req.Context(), selectedRows[id], req.Header, true)
					before := bridgeCalls
					var resp *http.Response
					if path == "gateway" {
						resp, err = gateway.doOpenAIUpstream(req, "http://untrusted-route.invalid:1", selectedRows[id])
					} else {
						resp, err = probe.doOpenAIAccountTestUpstream(req, "http://untrusted-route.invalid:1", selectedRows[id], false)
					}
					require.NoError(t, err)
					body, readErr := io.ReadAll(resp.Body)
					require.NoError(t, readErr)
					require.NoError(t, resp.Body.Close())
					require.Equal(t, "accepted", string(body))
					var observed observation
					select {
					case observed = <-observations:
					case <-time.After(5 * time.Second):
						t.Fatal("provider observation missing")
					}
					require.NoError(t, observed.err)
					require.Equal(t, expectedBody, observed.body)
					require.Equal(t, fmt.Sprintf("Bearer acquired-token-%d", id), observed.header.Get("Authorization"))
					require.Equal(t, fmt.Sprintf("acquired-workspace-%d", id), observed.header.Get("Chatgpt-Account-Id"))
					wantUA, wantOriginator := "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) screen-256color", "codex_cli_rs"
					if id == 101 {
						wantUA, wantOriginator = "codex_vscode/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color", "codex_vscode"
					}
					require.Equal(t, wantUA, observed.header.Get("User-Agent"))
					require.Equal(t, wantOriginator, observed.header.Get("Originator"))
					require.Equal(t, "0.165.0", observed.header.Get("Version"))
					want := tc.wantFirst
					if index == 1 {
						want = tc.wantSecond
					}
					require.Equal(t, want, bridgeCalls == before+1, "step=%d owner=%d", step, id)
					require.LessOrEqual(t, bridgeCalls-before, 1)
				}
			}
			require.EqualValues(t, 14, wireCalls.Load(), "one provider write per request; toggles must not replay")
		})
	}
}

// A disable changes the next send, never replays or interrupts the already
// submitted response stream. Real network streaming pins this boundary.
func TestCodexCLIBridgeHotDisablePreservesSubmittedHTTPStream(t *testing.T) {
	for _, source := range []string{"global", "account"} {
		t.Run(source, func(t *testing.T) {
			global := &codexBridgeSwitchRepo{openAIIdentitySettingRepoStub: &openAIIdentitySettingRepoStub{values: map[string]string{}}}
			global.enabled.Store(true)
			owner := enabledCodexCLIExecutionAccount(101, "acquired-access", "acquired-workspace")
			rows := &codexBridgeHotAccountRepo{owners: map[int64]*Account{101: owner}, flags: map[int64]*atomic.Bool{101: {}}}
			rows.flags[101].Store(true)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var writes atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				turn := writes.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: started\n\n")
				w.(http.Flusher).Flush()
				if turn == 1 {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				_, _ = io.WriteString(w, "data: completed\n\n")
			}))
			defer provider.Close()
			defer unblock()
			var bridgeCalls atomic.Int32
			gateway := &OpenAIGatewayService{cfg: &config.Config{}, accountRepo: rows, settingService: &SettingService{settingRepo: global},
				httpUpstream: &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					return http.DefaultClient.Do(req)
				}},
				codexCLIBridgeRuntime: codexCLIBridgeTransportStub{status: codexCLIBridgeReadyFixture(), bridge: func(ctx context.Context, _ codexcli.Status, req *http.Request, forward func(context.Context, *http.Request) (*http.Response, error)) (*http.Response, error) {
					bridgeCalls.Add(1)
					return forward(ctx, req)
				}},
			}
			ctx, cancel := context.WithTimeout(WithOutboundIdentityScope(context.Background(), nil), 5*time.Second)
			defer cancel()
			for turn := 1; turn <= 2; turn++ {
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.URL, strings.NewReader(`{"input":"same session"}`))
				require.NoError(t, err)
				resp, err := gateway.doOpenAIUpstreamPrepared(req, "", owner)
				require.NoError(t, err)
				if turn == 1 {
					if source == "global" {
						global.enabled.Store(false)
					} else {
						rows.flags[101].Store(false)
					}
					unblock()
				}
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, "data: started\n\ndata: completed\n\n", string(body))
			}
			require.EqualValues(t, 2, writes.Load())
			require.EqualValues(t, 1, bridgeCalls.Load())
		})
	}
}
