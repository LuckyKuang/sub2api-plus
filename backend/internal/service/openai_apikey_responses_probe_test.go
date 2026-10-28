//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
)

type responsesProbeAccountRepo struct {
	snapshotUpdateAccountRepo
	casCalls            int
	unconditionalWrites int
	casErr              error
}

func (r *responsesProbeAccountRepo) UpdateExtra(ctx context.Context, id int64, extra map[string]any) error {
	r.unconditionalWrites++
	return r.snapshotUpdateAccountRepo.UpdateExtra(ctx, id, extra)
}

func (r *responsesProbeAccountRepo) ApplyCredentialStateMutation(ctx context.Context, owner *Account, mutation CredentialStateMutation) (bool, error) {
	r.casCalls++
	if r.casErr != nil {
		return false, r.casErr
	}
	current, err := r.GetByID(ctx, owner.ID)
	if err != nil {
		return false, err
	}
	if owner.Platform != current.Platform || owner.Type != current.Type ||
		!reflect.DeepEqual(owner.Credentials, current.Credentials) ||
		!reflect.DeepEqual(owner.ProxyID, current.ProxyID) || !reflect.DeepEqual(owner.Proxy, current.Proxy) {
		return false, nil
	}
	if current.Extra == nil {
		current.Extra = map[string]any{}
	}
	for key, value := range mutation.Extra {
		current.Extra[key] = value
	}
	return true, r.snapshotUpdateAccountRepo.UpdateExtra(ctx, owner.ID, mutation.Extra)
}

func TestProbeOpenAIAPIKeyResponsesSupportUsesFinalOpenAIIdentity(t *testing.T) {
	updateCalls := make(chan map[string]any, 1)
	account := Account{
		ID:          96,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": "https://compat-upstream.example/v1",
		},
	}
	repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      updateCalls,
	}}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"output":[{"type":"function_call","name":"probe_ping"}]}`)),
	}}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
	}

	svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://compat-upstream.example/v1/responses", upstream.lastReq.URL.String())
	requireOpenAIAPIKeyProbeHeaders(t, upstream.lastReq.Header)
	require.Len(t, updateCalls, 1)
	updates := <-updateCalls
	require.Equal(t, true, updates[openai_compat.ExtraKeyResponsesSupported])
	require.Equal(t, 1, repo.casCalls)
	require.Zero(t, repo.unconditionalWrites)
}

func TestProbeOpenAIAPIKeyResponsesSupportCNProviders(t *testing.T) {
	tests := []struct {
		name        string
		id          int64
		platform    string
		protocol    string
		wantSupport bool
		wantMode    string
	}{
		{name: "deepseek adaptive supports responses", id: 201, platform: PlatformDeepseek, protocol: APIProtocolAdaptive, wantSupport: true, wantMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		{name: "deepseek chat clears forced responses", id: 202, platform: PlatformDeepseek, protocol: APIProtocolChatCompletions, wantSupport: false, wantMode: string(openai_compat.ResponsesSupportModeAuto)},
		{name: "kimi adaptive supports responses", id: 203, platform: PlatformKimi, protocol: APIProtocolAdaptive, wantSupport: true, wantMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		{name: "kimi responses protocol supports responses", id: 205, platform: PlatformKimi, protocol: APIProtocolResponses, wantSupport: true, wantMode: string(openai_compat.ResponsesSupportModeForceResponses)},
		{name: "zhipu adaptive falls back to chat", id: 204, platform: PlatformZhipu, protocol: APIProtocolAdaptive, wantSupport: false, wantMode: string(openai_compat.ResponsesSupportModeAuto)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			updateCalls := make(chan map[string]any, 1)
			account := Account{
				ID: tc.id, Platform: tc.platform, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-test", "api_protocol": tc.protocol},
				Extra: map[string]any{
					openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceResponses),
				},
			}
			repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				updateExtraCalls:      updateCalls,
			}}
			svc := &AccountTestService{accountRepo: repo}

			svc.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)

			require.Len(t, updateCalls, 1)
			updates := <-updateCalls
			require.Equal(t, tc.wantSupport, updates[openai_compat.ExtraKeyResponsesSupported])
			require.Equal(t, tc.wantMode, updates[openai_compat.ExtraKeyResponsesMode])
			require.Equal(t, 1, repo.casCalls)
			require.Zero(t, repo.unconditionalWrites)
		})
	}
}

func TestProbeOpenAIAPIKeyResponsesSupportPreparationFailuresNeverSend(t *testing.T) {
	for _, failure := range []string{"missing-proxy", "preparation-error"} {
		t.Run(failure, func(t *testing.T) {
			account := newResponsesProbeAccount(5001)
			ctx := context.Background()
			if failure == "missing-proxy" {
				proxyID := int64(7)
				account.ProxyID = &proxyID
			} else {
				ctx = WithOutboundPreparationError(ctx, errors.New("identity preparation failed"))
			}
			repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				updateExtraCalls:      make(chan map[string]any, 1),
			}}
			sends := 0
			service := &AccountTestService{accountRepo: repo,
				cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
				httpUpstream: &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					sends++
					return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}},
			}
			service.ProbeOpenAIAPIKeyResponsesSupport(ctx, account.ID)
			require.Zero(t, sends)
			require.Zero(t, repo.casCalls)
			require.Zero(t, repo.unconditionalWrites)
			require.Empty(t, repo.updateExtraCalls)
		})
	}
}

func TestProbeOpenAIAPIKeyResponsesSupportFreezesWireStateAcrossRelink(t *testing.T) {
	account := newResponsesProbeAccount(5002)
	proxyID := int64(8)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "old-proxy.example", Port: 8080}
	account.Credentials["user_agent"] = "codex_vscode/0.165.0 (Ubuntu 24.04; aarch64) vscode"
	account.Credentials["model_mapping"] = map[string]any{"client-model": "gpt-old"}
	account.Credentials["header_override_enabled"] = true
	account.Credentials["header_overrides"] = map[string]any{"X-Grant": "old"}
	repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      make(chan map[string]any, 1),
	}}
	settingsRepo := &modelsManifestSnapshotSettingsRepo{onRead: func() {
		current := &repo.accounts[0]
		current.Credentials["api_key"] = "replacement-key"
		current.Credentials["base_url"] = "https://replacement.example/v1"
		current.Credentials["user_agent"] = DefaultOpenAICodexUserAgent
		modelMapping, ok := current.Credentials["model_mapping"].(map[string]any)
		require.True(t, ok)
		modelMapping["client-model"] = "gpt-new"
		headerOverrides, ok := current.Credentials["header_overrides"].(map[string]any)
		require.True(t, ok)
		headerOverrides["X-Grant"] = "new"
		current.Proxy.Host = "new-proxy.example"
	}}
	var sent *http.Request
	var sentProxy string
	service := &AccountTestService{accountRepo: repo, settingService: NewSettingService(settingsRepo, nil),
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: &codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
			sent, sentProxy = req, proxy
			require.Equal(t, account.ID, id)
			require.Equal(t, 1, concurrency)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"output":[{"type":"function_call"}]}`))}, nil
		}},
	}
	service.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
	require.NotNil(t, sent)
	require.Equal(t, "https://compat-upstream.example/v1/responses", sent.URL.String())
	require.Equal(t, "Bearer sk-test", sent.Header.Get("Authorization"))
	require.Equal(t, "old", getHeaderRaw(sent.Header, "x-grant"))
	require.Equal(t, "codex_vscode/0.165.0 (Ubuntu 24.04; aarch64) vscode", sent.UserAgent())
	require.Equal(t, "codex_vscode", sent.Header.Get("Originator"))
	require.Equal(t, "0.165.0", sent.Header.Get("Version"))
	require.Equal(t, "http://old-proxy.example:8080", sentProxy)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(sent.Body).Decode(&payload))
	require.Equal(t, "gpt-old", payload["model"])
	require.Equal(t, 1, repo.casCalls)
	require.Empty(t, repo.updateExtraCalls, "old grant result must not alter the relinked account")
	require.Zero(t, repo.unconditionalWrites)
}

func TestProbeOpenAIAPIKeyResponsesSupportLateResponseCannotOverwriteNewGrant(t *testing.T) {
	account := newResponsesProbeAccount(5003)
	repo := &responsesProbeAccountRepo{snapshotUpdateAccountRepo: snapshotUpdateAccountRepo{
		stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
		updateExtraCalls:      make(chan map[string]any, 1),
	}}
	service := &AccountTestService{accountRepo: repo,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: &codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			// A replacement grant is committed while the old endpoint returns 404.
			repo.accounts[0].Credentials["api_key"] = "new-grant-key"
			repo.accounts[0].Extra = map[string]any{openai_compat.ExtraKeyResponsesSupported: true}
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}},
	}
	service.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
	require.Equal(t, true, repo.accounts[0].Extra[openai_compat.ExtraKeyResponsesSupported])
	require.Equal(t, 1, repo.casCalls)
	require.Empty(t, repo.updateExtraCalls)
	require.Zero(t, repo.unconditionalWrites)
}

func TestProbeOpenAIAPIKeyResponsesSupportPersistenceFailureHasNoFallback(t *testing.T) {
	for _, cause := range []string{"missing-port", "repository-error", "cn-missing-port", "cn-repository-error"} {
		t.Run(cause, func(t *testing.T) {
			account := newResponsesProbeAccount(5004)
			if strings.HasPrefix(cause, "cn-") {
				account.Platform = PlatformDeepseek
				account.Credentials["api_protocol"] = APIProtocolAdaptive
			}
			base := snapshotUpdateAccountRepo{
				stubOpenAIAccountRepo: stubOpenAIAccountRepo{accounts: []Account{account}},
				updateExtraCalls:      make(chan map[string]any, 1),
			}
			var repository AccountRepository = &base
			if strings.HasSuffix(cause, "repository-error") {
				repository = &responsesProbeAccountRepo{snapshotUpdateAccountRepo: base, casErr: errors.New("unavailable")}
			}
			service := &AccountTestService{accountRepo: repository,
				cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
				httpUpstream: &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}},
			}
			service.ProbeOpenAIAPIKeyResponsesSupport(context.Background(), account.ID)
			require.Empty(t, base.updateExtraCalls)
		})
	}
}

func TestDecideResponsesProbeSupport(t *testing.T) {
	fnCall := []byte(`{"output":[{"type":"reasoning"},{"type":"function_call","name":"probe_ping"}]}`)
	reasoningOnly := []byte(`{"output":[{"type":"reasoning"}]}`)

	cases := []struct {
		name   string
		status int
		body   []byte
		want   bool
	}{
		// Endpoint clearly absent on third-party OpenAI-compatible upstreams.
		{"404 endpoint absent", 404, fnCall, false},
		{"405 method not allowed", 405, fnCall, false},
		// 2xx: tool capability is judged by presence of a function_call output item.
		{"200 with function_call", 200, fnCall, true},
		// Volcengine Ark coding/v3 × kimi-k2.6: reasoning only, no function_call.
		{"200 reasoning only", 200, reasoningOnly, false},
		{"200 invalid json", 200, []byte("not-json"), false},
		{"200 no output field", 200, []byte(`{"status":"completed"}`), false},
		// Non-2xx (other than 404/405): endpoint exists, capability undecidable -> conservative true.
		{"400 conservative true", 400, reasoningOnly, true},
		{"401 conservative true", 401, nil, true},
		{"500 conservative true", 500, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, decideResponsesProbeSupport(tc.status, tc.body))
		})
	}
}

func TestResponsesProbeBodyHasFunctionCall(t *testing.T) {
	require.True(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"function_call"}]}`)))
	require.True(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"reasoning"},{"type":"function_call"}]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[{"type":"reasoning"}]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{"output":[]}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`{}`)))
	require.False(t, responsesProbeBodyHasFunctionCall([]byte(`garbage`)))
}

func TestSelectResponsesProbeModel(t *testing.T) {
	// No model_mapping -> fall back to DefaultTestModel (OpenAI official APIKey).
	require.Equal(t, openai.DefaultTestModel, selectResponsesProbeModel(&Account{}))

	// model_mapping values are upstream models; pick first by sort for reproducibility.
	acct := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"client-b": "zeta-model",
			"client-a": "alpha-model",
		},
	}}
	require.Equal(t, "alpha-model", selectResponsesProbeModel(acct))

	// Wildcard / blank upstream values are skipped.
	acctWild := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{
			"a": "*",
			"b": "  ",
			"c": "real-model",
		},
	}}
	require.Equal(t, "real-model", selectResponsesProbeModel(acctWild))

	// Only wildcard mappings -> DefaultTestModel.
	acctAllWild := &Account{Credentials: map[string]any{
		"model_mapping": map[string]any{"a": "gpt-*"},
	}}
	require.Equal(t, openai.DefaultTestModel, selectResponsesProbeModel(acctAllWild))
}
