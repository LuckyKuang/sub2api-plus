//go:build unit || !integration

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/brandidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

// These are product requirements, not expectations generated from the policy.
var fixedIdentityCases = []struct{ platform, kind, preset string }{
	{"deepseek", "oauth", "deepseek"}, {"deepseek", "apikey", "deepseek"},
	{"kimi", "oauth", "kimi"}, {"kimi", "apikey", "kimi"},
	{"minimax", "oauth", "minimax"}, {"minimax", "apikey", "minimax_apikey"},
	{"zhipu", "oauth", "zcode"}, {"zhipu", "apikey", "zcode"},
	{"stepfun", "oauth", "stepfun"}, {"stepfun", "apikey", "stepfun"},
	{"anthropic", "bedrock", "claude"}, {"anthropic", "service_account", "claude"}, {"gemini", "service_account", "gemini"},
}

func TestNativeIdentityPolicyRejectsEveryForeignFamilyAndDefaultMapping(t *testing.T) {
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	for _, tc := range fixedIdentityCases {
		t.Run(tc.platform+":"+tc.kind, func(t *testing.T) {
			account := &Account{ID: 71, Platform: tc.platform, Type: tc.kind}
			policy := outboundAccountPolicy(tc.platform, tc.kind)
			require.Equal(t, tc.preset, policy.NativePreset)
			require.Equal(t, []string{tc.preset}, policy.AllowedPresets)
			require.False(t, policy.AllowDefaultMapping)
			for _, preset := range outboundPresetNames {
				selection := OutboundIdentitySelection{Preset: preset}
				credentials := map[string]any{outboundIdentityCredential: selection}
				err := NormalizeAccountOutboundIdentity(tc.platform, tc.kind, credentials)
				_, previewErr := svc.PreviewOutboundIdentity(ctx, account, &selection)
				if preset == tc.preset {
					require.NoError(t, err)
					require.NoError(t, previewErr)
				} else {
					requireApplicationErrorReason(t, err, "OUTBOUND_IDENTITY_INVALID")
					require.Error(t, previewErr)
				}
				config := emptyOutboundIdentitySettings()
				config.Defaults[tc.platform+":"+tc.kind] = preset
				requireApplicationErrorReason(t, svc.SetOutboundIdentitySettings(ctx, config), "OUTBOUND_IDENTITY_INVALID")
			}
		})
	}
}

func TestCompatibleIdentityPolicyHasExactlyElevenAdvancedMappings(t *testing.T) {
	expected := []string{"openai:apikey", "openai:upstream", "anthropic:apikey", "anthropic:upstream", "gemini:apikey", "gemini:upstream", "grok:apikey", "grok:upstream", "antigravity:upstream", "typesafe:apikey", "opencode_go:apikey"}
	svc, ctx := outboundIdentityTestSettings(t, emptyOutboundIdentitySettings())
	var actual []string
	for _, policy := range svc.GetOutboundIdentityView(ctx).AccountPolicies {
		if policy.AllowDefaultMapping {
			actual = append(actual, policy.Key)
		}
	}
	require.ElementsMatch(t, expected, actual)
	for _, key := range expected {
		t.Run(key, func(t *testing.T) {
			config := emptyOutboundIdentitySettings()
			config.Defaults[key] = "grok"
			require.NoError(t, svc.SetOutboundIdentitySettings(ctx, config))
			platform, kind, _ := strings.Cut(key, ":")
			account := &Account{ID: 1, Platform: platform, Type: kind}
			identity, ok := outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			require.Equal(t, "grok", identity.Preset)
			account.Credentials = map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "claude"}}
			identity, ok = outboundidentity.FromContext(WithAccountOutboundIdentity(ctx, account))
			require.True(t, ok)
			require.Equal(t, "claude", identity.Preset)
			require.Equal(t, "account", identity.Source)
		})
	}
}

func TestNativeIdentityStoredForeignCandidatesNeverReachWire(t *testing.T) {
	expectedUA := map[string]string{
		"deepseek": "deepseek-harness/0.2.0-rc.2 (+https://github.com/deepseek-ai/deepseek-harness)",
		"kimi":     "kimi-code-cli/2.1.1", "minimax": "MiniMaxAgent",
		"minimax_apikey": "Anthropic/JS 0.91.1", "zcode": "ZCode/3.14.3",
		"stepfun": "step (linux 6.8.0-31-generic; x64)",
	}
	for _, tc := range fixedIdentityCases[:10] {
		t.Run(tc.platform+":"+tc.kind, func(t *testing.T) {
			// Simulate a stale database before cleanup. The global and account
			// candidates both try to select another product with extra fields.
			raw, err := json.Marshal(OutboundIdentitySettings{Defaults: map[string]string{tc.platform + ":" + tc.kind: "grok"}})
			require.NoError(t, err)
			svc := NewSettingService(&outboundIdentityTestRepo{values: map[string]string{SettingKeyOutboundIdentity: string(raw)}}, nil)
			ctx := outboundidentity.WithResolver(context.Background(), svc.resolveOutboundIdentityKey)
			account := &Account{ID: 71, Platform: tc.platform, Type: tc.kind, Credentials: map[string]any{
				outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok", Version: "3.9.1"},
			}}
			captured := make(chan http.Header, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			client := &http.Client{Transport: brandidentity.WrapRoundTripper(server.Client().Transport)}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/models", nil)
			require.NoError(t, err)
			request.Header.Set("User-Agent", "Sub2API-caller")
			request.Header.Set("X-Grok-Client-Version", "99.0.0")
			request.Header.Set("Authorization", "Bearer test-credential")
			prepareAccountOutboundRequest(request, account)
			response, err := client.Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			headers := <-captured
			require.Equal(t, expectedUA[tc.preset], headers.Get("User-Agent"))
			require.Empty(t, headers.Get("X-Grok-Client-Version"))
			require.Equal(t, "Bearer test-credential", headers.Get("Authorization"))
			for name, values := range headers {
				require.NotContains(t, strings.ToLower(name), "sub2api")
				for _, value := range values {
					require.NotContains(t, strings.ToLower(value), "sub2api")
				}
			}
			standalone, ok := outboundidentity.FromContext(WithStandaloneOutboundIdentity(ctx, tc.platform))
			require.True(t, ok)
			want := tc.preset
			if tc.platform == "minimax" {
				want = "minimax_apikey"
			}
			require.Equal(t, want, standalone.Preset, "independent monitor credentials use the API-key identity")
		})
	}
}

func TestNativeIdentityBatchRejectsForeignFamilyBeforeAnyWrite(t *testing.T) {
	for _, tc := range fixedIdentityCases {
		t.Run(tc.platform+":"+tc.kind, func(t *testing.T) {
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
				{ID: 1, Platform: "openai", Type: "apikey"}, {ID: 2, Platform: tc.platform, Type: tc.kind},
			}}
			svc := &adminServiceImpl{accountRepo: repo}
			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
				AccountIDs: []int64{1, 2}, Credentials: map[string]any{outboundIdentityCredential: OutboundIdentitySelection{Preset: "grok"}},
			})
			require.Nil(t, result)
			requireApplicationErrorReason(t, err, "OUTBOUND_IDENTITY_INVALID")
			require.Zero(t, repo.bulkUpdateCalls)
		})
	}
}

type nativeIdentityWriteRepo struct {
	AccountRepository
	account *Account
	writes  int
}

func (r *nativeIdentityWriteRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *nativeIdentityWriteRepo) Create(context.Context, *Account) error { r.writes++; return nil }
func (r *nativeIdentityWriteRepo) Update(context.Context, *Account) error { r.writes++; return nil }

func TestNativeIdentityCreateAndUpdateRejectBeforePersistence(t *testing.T) {
	for _, tc := range fixedIdentityCases {
		for _, operation := range []string{"create", "update"} {
			t.Run(tc.platform+":"+tc.kind+"/"+operation, func(t *testing.T) {
				credentials := map[string]any{"api_key": "test-key", "access_token": "test-token"}
				if tc.platform == "stepfun" {
					credentials = stepFunTestAccount(tc.kind, "cn").Credentials
				}
				credentials[outboundIdentityCredential] = OutboundIdentitySelection{Preset: "grok"}
				repo := &nativeIdentityWriteRepo{account: &Account{ID: 1, Platform: tc.platform, Type: tc.kind, Credentials: map[string]any{}}}
				svc := &adminServiceImpl{accountRepo: repo}
				var err error
				if operation == "create" {
					_, err = svc.CreateAccount(context.Background(), &CreateAccountInput{Platform: tc.platform, Type: tc.kind, Credentials: credentials, SkipDefaultGroupBind: true})
				} else {
					_, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Credentials: credentials})
				}
				requireApplicationErrorReason(t, err, "OUTBOUND_IDENTITY_INVALID")
				require.Zero(t, repo.writes)
			})
		}
	}
}

func (r *nativeIdentityWriteRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}

func TestNativeIdentityAuthTypeChangeWithoutCredentialsIsValidated(t *testing.T) {
	for _, change := range []struct{ from, to, preset string }{
		{"oauth", "apikey", "minimax"}, {"apikey", "oauth", "minimax_apikey"},
	} {
		t.Run(change.from+"/"+change.to, func(t *testing.T) {
			repo := &nativeIdentityWriteRepo{account: &Account{ID: 1, Platform: "minimax", Type: change.from, Credentials: map[string]any{
				outboundIdentityCredential: OutboundIdentitySelection{Preset: change.preset},
			}}}
			svc := &adminServiceImpl{accountRepo: repo}
			_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Type: change.to})
			requireApplicationErrorReason(t, err, "OUTBOUND_IDENTITY_INVALID")
			require.Zero(t, repo.writes)
		})
	}
}
