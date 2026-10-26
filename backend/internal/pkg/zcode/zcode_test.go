//go:build unit

package zcode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/stretchr/testify/require"
)

func TestIdentityPinsProductVersionAndAcceptsBothOfficialLines(t *testing.T) {
	t.Setenv(VersionEnv, "")
	require.Equal(t, "3.16.2", DefaultVersion)
	require.Equal(t, "ZCode/3.16.2", UserAgent(DefaultVersion))
	require.Equal(t, "zcode", Preset)

	identity := DefaultIdentity()
	require.Equal(t, Preset, identity.Preset)
	require.Equal(t, "ZCode/3.16.2", identity.UserAgent)
	require.Equal(t, ProductToken, identity.Originator)
	require.Equal(t, DefaultVersion, identity.Version)
	require.Equal(t, "compiled_default", identity.Source)
	require.Equal(t, identity.Version, identity.Headers[HeaderAppVersion])
	require.Equal(t, "Z Code@electron", identity.Headers["X-Title"])
	require.Equal(t, "glm", identity.Headers["X-ZCode-Agent"])
	require.Len(t, identity.Headers, 11)

	// Both official version lines are selectable; the preset declares no floor.
	for _, version := range []string{"3.16.2", "0.16.9"} {
		t.Setenv(VersionEnv, version)
		configured := DefaultIdentity()
		require.Equal(t, "environment", configured.Source, version)
		require.Equal(t, version, configured.Version)
		require.Equal(t, "ZCode/"+version, configured.UserAgent)
	}

	for _, invalid := range []string{"", "v3.14.3", "3.14", "3.14.3.1", "../3", "3.14.3 x", strings.Repeat("1", 70)} {
		require.False(t, IsSupportedVersion(invalid), invalid)
	}
	// The override is trimmed before validation, so surrounding whitespace is not
	// a rejection.
	for _, valid := range []string{"3.16.2", "0.16.9", "3.15.0-rc.1", "10.0.0", " 3.14.3 "} {
		require.True(t, IsSupportedVersion(valid), valid)
	}
}

func TestProviderNormalization(t *testing.T) {
	require.Equal(t, ProviderBigModel, NormalizeProvider(" BigModel "))
	require.Equal(t, ProviderZai, NormalizeProvider("ZAI"))
	require.Empty(t, NormalizeProvider("openai"))
	require.False(t, IsProvider("google"))
	require.Equal(t, []string{ProviderBigModel, ProviderZai}, Providers)
}

func TestParseCallbackAcceptsURLOrBareCode(t *testing.T) {
	code, state := ParseCallback("https://zcode.z.ai/app/oauth/login?code=abc123&state=st-1")
	require.Equal(t, "abc123", code)
	require.Equal(t, "st-1", state)

	// The BigModel callback historically used authCode.
	code, state = ParseCallback("https://bigmodel.cn/callback?authCode=legacy&state=st-2")
	require.Equal(t, "legacy", code)
	require.Equal(t, "st-2", state)

	code, state = ParseCallback("bare-code")
	require.Equal(t, "bare-code", code)
	require.Empty(t, state)

	code, state = ParseCallback("   ")
	require.Empty(t, code)
	require.Empty(t, state)
}

func TestDesktopRedirectURIUsesAppVersionAndDeepLink(t *testing.T) {
	redirect := DesktopRedirectURI("https://zcode.z.ai", "3.16.2")
	parsed, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "/app/oauth/login", parsed.Path)
	require.Equal(t, "zcode://oauth/callback", parsed.Query().Get("redirect"))
	require.Equal(t, "3.16.2", parsed.Query().Get("app_version"))

	// An unsupported version falls back to the compiled pin rather than being
	// injected into the redirect.
	fallback := DesktopRedirectURI("https://zcode.z.ai", "not-a-version")
	parsed, err = url.Parse(fallback)
	require.NoError(t, err)
	require.Equal(t, DefaultVersion, parsed.Query().Get("app_version"))

	// The default origin is used when the override is empty.
	require.Contains(t, DesktopRedirectURI("", DefaultVersion), "https://zcode.z.ai/app/oauth/login")
}

func TestAuthorizeStateExtractsPlatformState(t *testing.T) {
	require.Equal(t, "st-9", AuthorizeState("https://bigmodel.cn/login?appId=zcode&state=st-9"))
	require.Empty(t, AuthorizeState("not a url"))
	require.Empty(t, AuthorizeState("https://bigmodel.cn/login"))
}

func TestGeneratePollTokenAndSessionIDAreHexAndUnique(t *testing.T) {
	token, err := GeneratePollToken()
	require.NoError(t, err)
	require.Len(t, token, pollTokenBytes*2)

	other, err := GeneratePollToken()
	require.NoError(t, err)
	require.NotEqual(t, token, other)

	sessionID, err := GenerateSessionID()
	require.NoError(t, err)
	require.Len(t, sessionID, sessionIDBytes*2)
}

// handshakeServer builds a fake ZCode platform that records the poll token it
// observes, so the test can prove the credential never reaches the caller.
func handshakeServer(t *testing.T, provider string, polls []string, readyBody map[string]any) (*httptest.Server, *atomic.Int64, *strings.Builder) {
	t.Helper()
	var calls atomic.Int64
	var pollsSeen atomic.Int64
	var seen strings.Builder
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/oauth/cli/init"):
			calls.Add(1)
			body, _ := io.ReadAll(r.Body)
			require.Contains(t, string(body), `"provider":"`+provider+`"`)
			seen.WriteString(r.Header.Get("Authorization") + "\n")
			writeEnvelope(t, w, map[string]any{
				"flow_id":           "flow-1",
				"authorize_url":     "https://bigmodel.cn/login?appId=zcode&state=st-1",
				"expires_at":        time.Now().Add(10 * time.Minute).Unix(),
				"poll_interval_sec": 2,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/oauth/cli/poll/"):
			seen.WriteString(r.Header.Get("Authorization") + "\n")
			calls.Add(1)
			index := int(pollsSeen.Add(1)) - 1
			status := "pending"
			if index < len(polls) {
				status = polls[index]
			}
			if status == FlowStatusReady {
				writeEnvelope(t, w, readyBody)
				return
			}
			writeEnvelope(t, w, map[string]any{"status": status})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &calls, &seen
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}))
}

func TestHandshakeClientInitAndPoll(t *testing.T) {
	bigmodel := map[string]any{
		"access_token":  "bm-access",
		"accessToken":   "bm-camel",
		"refresh_token": "bm-refresh",
	}
	server, calls, seen := handshakeServer(t, ProviderBigModel, []string{FlowStatusPending, FlowStatusReady}, map[string]any{
		"status":   FlowStatusReady,
		"token":    "zcode-jwt",
		"user":     map[string]any{"user_id": "u-1", "name": "Ada", "email": "ada@example.com"},
		"bigmodel": bigmodel,
	})
	client := NewHandshakeClient(server.Client(), server.URL)

	init, err := client.Init(context.Background(), ProviderBigModel, "poll-token-1")
	require.NoError(t, err)
	require.Equal(t, "flow-1", init.FlowID)
	require.Equal(t, "https://bigmodel.cn/login?appId=zcode&state=st-1", init.AuthorizeURL)
	require.Equal(t, int64(2), init.PollIntervalSec)
	require.True(t, init.ExpiresAt.After(time.Now()))

	poll, err := client.Poll(context.Background(), ProviderBigModel, init.FlowID, "poll-token-1")
	require.NoError(t, err)
	require.Equal(t, FlowStatusPending, poll.Status)

	poll, err = client.Poll(context.Background(), ProviderBigModel, init.FlowID, "poll-token-1")
	require.NoError(t, err)
	require.Equal(t, FlowStatusReady, poll.Status)
	require.Equal(t, "zcode-jwt", poll.Ready.ZCodeJWT)
	require.Equal(t, "u-1", poll.Ready.User.ID)
	require.Equal(t, "Ada", poll.Ready.User.DisplayName())
	// The BigModel estate reports a business token directly; the camelCase
	// fallback is only used when the snake_case field is absent.
	require.Equal(t, "bm-access", poll.Ready.Provider.AccessToken)
	require.Equal(t, "bm-refresh", poll.Ready.Provider.RefreshToken)

	require.Equal(t, int64(3), calls.Load(), "one init plus two polls")
	require.Equal(t, 3, strings.Count(seen.String(), "Bearer poll-token-1"), "the poll credential is sent on every handshake call")
}

func TestHandshakeClientRejectsNonHTTPSAuthorizeURLAndInvalidData(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"insecure scheme": {
			"flow_id": "f", "authorize_url": "http://localhost:1/login",
			"expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 1,
		},
		"missing flow id": {
			"flow_id": "", "authorize_url": "https://bigmodel.cn/login",
			"expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 1,
		},
		"zero interval": {
			"flow_id": "f", "authorize_url": "https://bigmodel.cn/login",
			"expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeEnvelope(t, w, data)
			}))
			defer server.Close()
			_, err := NewHandshakeClient(server.Client(), server.URL).Init(context.Background(), ProviderBigModel, "t")
			require.Error(t, err)
		})
	}
}

func TestHandshakeClientSurfacesPlatformBusinessError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":3101,"msg":"not eligible"}`))
	}))
	defer server.Close()
	_, err := NewHandshakeClient(server.Client(), server.URL).Init(context.Background(), ProviderZai, "t")
	require.ErrorContains(t, err, "not eligible")
}

func TestHandshakeClientPollRejectsIncompleteReadyPayload(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"missing jwt":   {"status": FlowStatusReady, "user": map[string]any{"user_id": "u"}, "zai": map[string]any{"access_token": "a"}},
		"missing token": {"status": FlowStatusReady, "token": "j", "user": map[string]any{"user_id": "u"}},
		"missing user":  {"status": FlowStatusReady, "token": "j", "zai": map[string]any{"access_token": "a"}},
		"unknown state": {"status": "weird"},
	} {
		t.Run(name, func(t *testing.T) {
			server, _, _ := handshakeServer(t, ProviderZai, []string{FlowStatusReady}, body)
			client := NewHandshakeClient(server.Client(), server.URL)
			_, err := client.Init(context.Background(), ProviderZai, "t")
			require.NoError(t, err)
			_, err = client.Poll(context.Background(), ProviderZai, "flow-1", "t")
			require.Error(t, err)
		})
	}
}

func TestHandshakeClientExchangeCodeUsesProviderAndReturnsZaiToken(t *testing.T) {
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth/token", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		writeEnvelope(t, w, map[string]any{
			"token": "zcode-jwt",
			"user":  map[string]any{"user_id": "u-1"},
			"zai":   map[string]any{"access_token": "zai-oauth"},
		})
	}))
	defer server.Close()

	ready, err := NewHandshakeClient(server.Client(), server.URL).
		ExchangeCode(context.Background(), ProviderZai, "code-1", "https://zcode.z.ai/app/oauth/login", "st-1")
	require.NoError(t, err)
	require.Equal(t, "zcode-jwt", ready.ZCodeJWT)
	require.Equal(t, "zai-oauth", ready.Provider.AccessToken)
	require.Equal(t, ProviderZai, gotBody["provider"])
	require.Equal(t, "code-1", gotBody["code"])
	require.Equal(t, "st-1", gotBody["state"])
	require.Equal(t, "https://zcode.z.ai/app/oauth/login", gotBody["redirect_uri"])
}

func TestSessionStoreLifecycleAndSingleUse(t *testing.T) {
	store := NewSessionStore()
	store.Stop()
	store.Stop() // idempotent

	session := &OAuthSession{
		Provider:        ProviderBigModel,
		PollToken:       "secret",
		FlowID:          "flow",
		ExpiresAt:       time.Now().Add(time.Minute),
		PollIntervalSec: 2,
	}
	require.NoError(t, store.Set("s-1", session))
	loaded, ok := store.Get("s-1")
	require.True(t, ok)
	require.Equal(t, "secret", loaded.PollToken)
	require.True(t, store.TryConsume("s-1"))
	require.False(t, store.TryConsume("s-1"), "a session is single-use")

	// Expired sessions are not served.
	expired := &OAuthSession{Provider: ProviderZai, ExpiresAt: time.Now().Add(-time.Second)}
	require.NoError(t, store.Set("s-2", expired))
	_, ok = store.Get("s-2")
	require.False(t, ok)

	store.Delete("s-1")
	_, ok = store.Get("s-1")
	require.False(t, ok)
	_, ok = store.Get("")
	require.False(t, ok)
}

func TestMarshalSessionOmitsNothingSensitiveBeyondPollToken(t *testing.T) {
	raw, err := MarshalSession(&OAuthSession{
		Provider: ProviderZai, PollToken: "secret", FlowID: "flow",
		ExpiresAt: time.Now(), CreatedAt: time.Now(),
	})
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, "zai", decoded["provider"])
	// The session never carries the poll result, so credentials cannot leak
	// through the shared session backend.
	require.NotContains(t, string(raw), "access_token")
	require.NotContains(t, string(raw), "zcode_jwt")
}

func TestBizCodeAcceptedMirrorsUpstreamEnvelopeRule(t *testing.T) {
	yes := true
	no := false
	cases := []struct {
		code    string
		success *bool
		hasData bool
		want    bool
	}{
		{`0`, nil, false, true},
		{`200`, nil, false, true},
		{`"0"`, nil, false, true},
		{`3101`, nil, true, false},
		{`null`, nil, true, true},
		{``, nil, false, false},
		{`0`, &no, true, false},
		{`null`, &yes, false, true},
		{`0`, &yes, false, true},
	}
	for _, item := range cases {
		require.Equal(t, item.want, bizCodeAccepted(json.RawMessage(item.code), item.success, item.hasData), item.code)
	}
}

func TestPickPersonalScopeUsesCLIDefaultSelection(t *testing.T) {
	info := &CustomerInfo{Organizations: []OrganizationInfo{
		{
			OrganizationID:   "org-team",
			OrganizationName: "Team Org",
			Projects:         []ProjectInfo{{ProjectID: "p-team", ProjectName: "Team", ProjectType: json.RawMessage(`"2"`)}},
		},
		{
			OrganizationID:   "org-other",
			OrganizationName: "Other",
			Projects:         []ProjectInfo{{ProjectID: "p-other", ProjectName: "Other"}},
		},
		{
			OrganizationID:   "org-default",
			OrganizationName: "默认机构 A",
			Projects: []ProjectInfo{
				{ProjectID: "p-a", ProjectName: "A"},
				{ProjectID: "p-default", ProjectName: "默认项目 B"},
			},
		},
	}}
	organizationID, projectID := PickPersonalScope(info)
	require.Equal(t, "org-default", organizationID)
	require.Equal(t, "p-default", projectID)

	// Current CLI default selection also accepts a visible team project.
	teamOnly := &CustomerInfo{Organizations: []OrganizationInfo{{
		OrganizationID: "org-team",
		Projects:       []ProjectInfo{{ProjectID: "p", ProjectType: json.RawMessage(`2`)}},
	}}}
	organizationID, projectID = PickPersonalScope(teamOnly)
	require.Equal(t, "org-team", organizationID)
	require.Equal(t, "p", projectID)
	organizationID, projectID = PickPersonalScope(nil)
	require.Empty(t, organizationID)
	require.Empty(t, projectID)

	require.True(t, HasProject(info, "org-default", "p-default"))
	require.False(t, HasProject(info, "org-default", "missing"))
	require.False(t, HasProject(info, "missing", "p-default"))
	require.False(t, HasProject(nil, "org", "project"))
}

func TestMatchAPIKeyHonoursNameAndKeyType(t *testing.T) {
	keyType := TeamAPIKeyType
	other := 1
	keys := []APIKeySummary{
		{Name: TeamAPIKeyName, KeyType: &other, APIKey: "wrong-type"},
		{Name: "other-name", KeyType: &keyType, APIKey: "wrong-name"},
		{Name: TeamAPIKeyName, KeyType: &keyType, APIKey: "team-key"},
	}
	require.Equal(t, "team-key", matchAPIKey(keys, TeamAPIKeyName, &keyType))
	require.Equal(t, "wrong-type", matchAPIKey(keys, TeamAPIKeyName, nil), "an unconstrained match is name-only, like the personal key lookup")
	require.Equal(t, "", matchAPIKey(keys, "absent", nil))
}

// Independent protocol fixtures from shared/project-access-token-client.ts.
func TestCredentialClientIssuesProjectTokenForBothEstates(t *testing.T) {
	for _, provider := range []string{ProviderBigModel, ProviderZai} {
		t.Run(provider, func(t *testing.T) {
			var paths []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				require.Equal(t, "ZCode/3.16.2", r.UserAgent())
				switch {
				case strings.HasSuffix(r.URL.Path, "/getCustomerInfo"):
					require.Equal(t, "login", r.Header.Get("Authorization"))
					writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{"organizationId": "org", "organizationName": "默认机构", "projects": []any{map[string]any{"projectId": "project", "projectName": "默认项目"}}}}})
				case strings.HasSuffix(r.URL.Path, "/api_keys"):
					auth := "login"
					if provider == ProviderZai {
						auth = "Bearer login"
					}
					require.Equal(t, auth, r.Header.Get("Authorization"))
					writeEnvelope(t, w, []any{map[string]any{"apiKey": "location-id", "name": "zcode-api-key"}})
				case strings.HasSuffix(r.URL.Path, "/location-id/access_tokens"):
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, "Bearer login", r.Header.Get("Authorization"))
					var body map[string]string
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, map[string]string{"clientType": "zcode", "clientVersion": "3.16.2"}, body)
					writeEnvelope(t, w, map[string]any{"accessToken": "short-pat", "tokenType": "Bearer", "expiresIn": 3600, "expiresAt": time.Now().Add(30 * time.Minute).Unix()})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := NewCredentialClient(server.Client(), server.URL, server.URL)
			value, err := client.ResolveIndividualCodingPlanKey(context.Background(), provider, "login")
			require.NoError(t, err)
			require.Equal(t, "short-pat", value.APIKey)
			require.Equal(t, "location-id", value.KeyID)
			require.Equal(t, "org", value.OrganizationID)
			require.Equal(t, "project", value.ProjectID)
			require.WithinDuration(t, time.Now().Add(30*time.Minute), value.ExpiresAt, 2*time.Second)
			require.Len(t, paths, 3)
			for _, path := range paths {
				require.NotContains(t, path, "/copy/")
			}
		})
	}
}

func TestCredentialClientRejectsMissingScopeBeforeCreatingKey(t *testing.T) {
	var created atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getCustomerInfo") {
			writeEnvelope(t, w, map[string]any{"organizations": []any{}})
			return
		}
		created.Store(true)
		w.WriteHeader(500)
	}))
	defer server.Close()
	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	_, err := client.ResolveIndividualCodingPlanKey(context.Background(), ProviderBigModel, "login")
	require.Error(t, err)
	require.False(t, created.Load())
	_, err = client.ResolveIndividualCodingPlanKey(context.Background(), "unknown", "login")
	require.Error(t, err)
	_, err = client.ResolveIndividualCodingPlanKey(context.Background(), ProviderBigModel, "")
	require.Error(t, err)
}

func TestCredentialClientExchangesZaiBusinessToken(t *testing.T) {
	var body map[string]string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/auth/z/login", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeEnvelope(t, w, map[string]any{"access_token": "business-token", "expires_in": 3600})
	}))
	defer server.Close()

	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	token, err := client.ExchangeZaiBusinessToken(context.Background(), "oauth-token")
	require.NoError(t, err)
	require.Equal(t, "business-token", token)
	require.Equal(t, "oauth-token", body["token"])

	_, err = client.ExchangeZaiBusinessToken(context.Background(), " ")
	require.Error(t, err)
}

func TestCredentialClientProjectTokenRejectsDisabledAndMalformed(t *testing.T) {
	for _, data := range []map[string]any{
		{"enable": false, "accessToken": "pat", "tokenType": "Bearer", "expiresIn": 3600, "expiresAt": time.Now().Add(time.Hour).Unix()},
		{"accessToken": "pat", "tokenType": "Basic", "expiresIn": 3600, "expiresAt": time.Now().Add(time.Hour).Unix()},
		{"accessToken": "pat", "tokenType": "Bearer", "expiresIn": 0, "expiresAt": time.Now().Add(time.Hour).Unix()},
		{"accessToken": "pat", "tokenType": "Bearer", "expiresIn": 3600, "expiresAt": 1},
		{},
	} {
		t.Run(fmt.Sprint(data), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NotContains(t, r.URL.Path, "/copy/")
				writeEnvelope(t, w, data)
			}))
			defer server.Close()
			client := NewCredentialClient(server.Client(), server.URL, server.URL)
			_, err := client.IssueProjectAccessToken(context.Background(), ProviderBigModel, "login", "org", "project", "id")
			require.Error(t, err)
		})
	}
}

func TestCredentialClientProjectTokenEnvelopeAndPinnedVersion(t *testing.T) {
	// shared/project-access-token-response.ts requires an explicit success code;
	// project-access-token-client.ts carries the selected product version.
	for _, tc := range []struct {
		code   string
		valid  bool
		status int
	}{
		{`0`, true, 0}, {`200`, true, 0}, {`"0"`, true, 0}, {`"200"`, true, 0},
		{``, false, 0}, {`null`, false, 0}, {`404`, false, 404}, {`"404"`, false, 404},
	} {
		t.Run("code="+tc.code, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/api/biz/v1/organization/org/projects/project/api_keys/saved-id/access_tokens", r.URL.Path)
				require.Equal(t, "Bearer login", r.Header.Get("Authorization"))
				var body map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, map[string]string{"clientType": "zcode", "clientVersion": "4.2.0"}, body)
				payload := map[string]any{"data": map[string]any{"accessToken": "short-pat", "tokenType": "Bearer", "expiresIn": 3600, "expiresAt": time.Now().Add(time.Hour).Unix()}}
				if tc.code != "" {
					payload["code"] = json.RawMessage(tc.code)
				}
				require.NoError(t, json.NewEncoder(w).Encode(payload))
			}))
			defer server.Close()
			client := NewCredentialClient(server.Client(), server.URL, server.URL)
			identity := DefaultIdentity()
			identity.Version, identity.UserAgent = "4.2.0", "ZCode/4.2.0"
			identity.Headers["User-Agent"], identity.Headers[HeaderAppVersion] = "ZCode/4.2.0", "4.2.0"
			value, err := client.IssueProjectAccessToken(outboundidentity.WithIdentity(context.Background(), identity), ProviderBigModel, "login", "org", "project", "saved-id")
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, "short-pat", value.Token)
			} else {
				require.Error(t, err)
				if tc.status != 0 {
					var status *BusinessHTTPError
					require.ErrorAs(t, err, &status)
					require.Equal(t, tc.status, status.Status)
				}
			}
		})
	}
}

func TestCredentialClientCreatesTeamLocationWithCodingPlanUsage(t *testing.T) {
	var created bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getCustomerInfo"):
			require.Equal(t, "login", r.Header.Get("Authorization"))
			writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{"organizationId": "org", "projects": []any{map[string]any{"projectId": "project", "projectType": 2}}}}})
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			require.Equal(t, "login", r.Header.Get("Authorization"))
			require.Equal(t, "org", r.Header.Get("bigmodel-organization"))
			require.Equal(t, "project", r.Header.Get("bigmodel-project"))
			if r.Method == http.MethodGet {
				writeEnvelope(t, w, []any{})
				return
			}
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, map[string]any{"name": "zcode-team-api-key", "keyType": float64(2), "usageScene": float64(1)}, body)
			created = true
			writeEnvelope(t, w, map[string]any{"apiKey": "id", "name": "zcode-team-api-key", "keyType": 2})
		case strings.HasSuffix(r.URL.Path, "/id/access_tokens"):
			require.Equal(t, "Bearer login", r.Header.Get("Authorization"))
			writeEnvelope(t, w, map[string]any{"accessToken": "pat", "tokenType": "Bearer", "expiresIn": 3600, "expiresAt": time.Now().Add(time.Hour).Unix()})
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	value, err := client.ResolveTeamPlanKey(context.Background(), ProviderBigModel, "login", "org", "project")
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "pat", value.APIKey)
	_, err = client.ResolveTeamPlanKey(context.Background(), ProviderBigModel, "login", "org", "other")
	require.Error(t, err)
}

func TestCredentialClientDoesNotCreateDuplicateMalformedPersonalLocation(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		writeEnvelope(t, w, []any{map[string]any{"name": "zcode-api-key"}})
	}))
	defer server.Close()
	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	_, err := client.findOrCreateAPIKey(context.Background(), server.URL, "login", nil, PersonalAPIKeyName, nil)
	require.Error(t, err)
	require.Zero(t, writes.Load())
}

func TestResolveOriginsFallsBackOnInvalidOverrides(t *testing.T) {
	bigModelOrigin, zaiOrigin := ResolveBusinessOrigins()
	require.Equal(t, DefaultBigModelBusinessOrigin, bigModelOrigin)
	require.Equal(t, DefaultZaiBusinessOrigin, zaiOrigin)

	t.Setenv(EnvBigModelBusinessOrigin, "http://insecure.example")
	t.Setenv(EnvZaiBusinessOrigin, "https://zai.example/")
	bigModelOrigin, zaiOrigin = ResolveBusinessOrigins()
	require.Equal(t, DefaultBigModelBusinessOrigin, bigModelOrigin, "a non-https override is ignored")
	require.Equal(t, "https://zai.example", zaiOrigin)

	t.Setenv(EnvHandshakeBaseURL, "https://handshake.example/")
	require.Equal(t, "https://handshake.example", ResolveHandshakeBaseURL())
	t.Setenv(EnvHandshakeBaseURL, "javascript:alert(1)")
	require.Equal(t, DefaultHandshakeBaseURL, ResolveHandshakeBaseURL())
}

func TestCredentialClientOriginsAreExposed(t *testing.T) {
	client := NewCredentialClient(nil, "", "")
	require.Equal(t, DefaultBigModelBusinessOrigin, client.BigModelOrigin())
	require.Equal(t, DefaultZaiBusinessOrigin, client.ZaiOrigin())
	require.Equal(t,
		DefaultBigModelBusinessOrigin+"/api/biz/v1/organization/org/projects/proj/api_keys",
		ProjectAPIKeysURL(client.BigModelOrigin(), "org", "proj"))
}

// offPeakServer is a scripted ticket server. The ticket client only accepts
// https origins, so the test serves TLS.
func offPeakServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *OffPeakClient) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server, NewOffPeakClient(server.Client(), server.URL)
}

func TestOffPeakTicketAuthHeadersCarryThePlanSnapshot(t *testing.T) {
	auth := OffPeakAuth{JWT: "jwt", PlanKey: "ak.sk", TeamOrg: "org", TeamProj: "proj"}
	headers := auth.Headers()
	require.Equal(t, "Bearer jwt", headers["Authorization"])
	require.Equal(t, "ak.sk", headers["X-Coding-Plan-Api-Key"])
	require.Equal(t, "org", headers["bigmodel-organization"])
	require.Equal(t, "proj", headers["bigmodel-project"])
	require.NotContains(t, headers, "x-request-id", "correlation IDs belong to physical sends, not credentials")

	// A half-specified team scope is omitted rather than sent incomplete.
	partial := OffPeakAuth{JWT: "jwt", TeamOrg: "org"}.Headers()
	require.NotContains(t, partial, "bigmodel-organization")
}

func TestOffPeakTakeTicketParsesStatePositionAndPollDelay(t *testing.T) {
	var seenAuth, seenBody string
	_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/off-peak/ticket", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		seenAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		seenBody = string(raw)
		writeEnvelope(t, w, map[string]any{
			"ticket_id": "t-1", "state": "queued", "position": 7, "next_poll_after": 5,
		})
	})

	ticket, err := client.TakeTicket(context.Background(), "task-1", OffPeakAuth{JWT: "jwt", PlanKey: "k"})
	require.NoError(t, err)
	require.Equal(t, "t-1", ticket.TicketID)
	require.Equal(t, TicketQueued, ticket.State)
	require.Equal(t, 7, *ticket.Position)
	require.Equal(t, 5*time.Second, ticket.NextPollAfter, "next_poll_after is reported in seconds")
	require.False(t, ticket.Admitted())
	require.False(t, ticket.Terminal())
	require.Equal(t, "Bearer jwt", seenAuth)
	require.Contains(t, seenBody, `"task_id":"task-1"`)

	_, err = client.TakeTicket(context.Background(), "  ", OffPeakAuth{JWT: "jwt"})
	require.Error(t, err)
}

func TestOffPeakTakeTicketAcceptsNullPositionAndReadyState(t *testing.T) {
	_, client := offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, map[string]any{"ticket_id": "t-2", "state": "ready", "position": nil})
	})
	ticket, err := client.TakeTicket(context.Background(), "task", OffPeakAuth{JWT: "jwt"})
	require.NoError(t, err)
	require.True(t, ticket.Admitted())
	require.Nil(t, ticket.Position)

	_, client = offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, map[string]any{"state": "ready"})
	})
	_, err = client.TakeTicket(context.Background(), "task", OffPeakAuth{JWT: "jwt"})
	require.Error(t, err, "a ticket without an id is rejected")
}

func TestOffPeakTicketStatusAndSettle(t *testing.T) {
	var settledPath string
	_, client := offPeakServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/off-peak/ticket/status":
			raw, _ := io.ReadAll(r.Body)
			require.Contains(t, string(raw), `"ticket_ids":["t-1","t-2"]`)
			writeEnvelope(t, w, map[string]any{
				"next_poll_after": 30,
				"tickets": []any{
					map[string]any{"ticket_id": "t-1", "state": "active"},
					map[string]any{"ticket_id": "t-2", "state": "settled"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/settle"):
			settledPath = r.URL.Path
			writeEnvelope(t, w, map[string]any{"ticket_id": "t-1", "state": "settled"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	tickets, delay, err := client.TicketStatus(context.Background(), []string{"t-1", "t-1", "t-2"}, OffPeakAuth{JWT: "jwt"})
	require.NoError(t, err)
	require.Len(t, tickets, 2, "duplicate ids are collapsed")
	require.Equal(t, TicketActive, tickets[0].State)
	require.True(t, tickets[0].Admitted())
	require.True(t, tickets[1].Terminal())
	require.Equal(t, 30*time.Second, delay)

	require.NoError(t, client.SettleTicket(context.Background(), "t-1", OffPeakAuth{JWT: "jwt"}))
	require.Equal(t, "/off-peak/ticket/t-1/settle", settledPath)
	require.Error(t, client.SettleTicket(context.Background(), "", OffPeakAuth{JWT: "jwt"}))
	_, _, err = client.TicketStatus(context.Background(), nil, OffPeakAuth{JWT: "jwt"})
	require.Error(t, err)
}

func TestOffPeakAvailabilityRejectsADirtyUnavailableResponse(t *testing.T) {
	_, client := offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, map[string]any{"can_take_number": true})
	})
	availability, err := client.Availability(context.Background(), OffPeakAuth{JWT: "jwt"})
	require.NoError(t, err)
	require.True(t, availability.CanTakeNumber)

	_, client = offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, map[string]any{"can_take_number": false, "next_take_at": time.Now().Add(time.Hour).Unix()})
	})
	availability, err = client.Availability(context.Background(), OffPeakAuth{JWT: "jwt"})
	require.NoError(t, err)
	require.False(t, availability.CanTakeNumber)
	require.True(t, availability.NextTakeAt.After(time.Now()))

	// The contract requires next_take_at whenever the quota is unavailable.
	_, client = offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, map[string]any{"can_take_number": false})
	})
	_, err = client.Availability(context.Background(), OffPeakAuth{JWT: "jwt"})
	require.Error(t, err)

	_, client = offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelope(t, w, map[string]any{})
	})
	_, err = client.Availability(context.Background(), OffPeakAuth{JWT: "jwt"})
	require.Error(t, err)
}

func TestOffPeakErrorsCarryTheRetryHint(t *testing.T) {
	_, client := offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":3105,"message":"queued"}`))
	})
	_, err := client.TakeTicket(context.Background(), "task", OffPeakAuth{JWT: "jwt"})
	require.Error(t, err)
	var offPeakErr *OffPeakError
	require.True(t, errors.As(err, &offPeakErr))
	require.Equal(t, OffPeakCodeQueued, offPeakErr.Code)
	require.Equal(t, http.StatusTooManyRequests, offPeakErr.HTTPStatus)
	require.Equal(t, 12*time.Second, offPeakErr.RetryAfter)

	_, client = offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":3103,"msg":"quota","data":{"next_take_at":` +
			strconv.FormatInt(time.Now().Add(30*time.Minute).Unix(), 10) + `}}`))
	})
	_, err = client.TakeTicket(context.Background(), "task", OffPeakAuth{JWT: "jwt"})
	require.True(t, errors.As(err, &offPeakErr))
	require.Equal(t, OffPeakCodeQuotaExhausted, offPeakErr.Code)
	require.True(t, offPeakErr.NextTakeAt.After(time.Now()))

	// A business code on HTTP 200 is still a rejection.
	_, client = offPeakServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":3101,"msg":"not eligible"}`))
	})
	_, err = client.TakeTicket(context.Background(), "task", OffPeakAuth{JWT: "jwt"})
	require.True(t, errors.As(err, &offPeakErr))
	require.Equal(t, OffPeakCodeNoEligibility, offPeakErr.Code)
	require.Contains(t, offPeakErr.Error(), "not eligible")
}

func TestResolveOffPeakBaseURLFallsBackOnInvalidOverride(t *testing.T) {
	t.Setenv(EnvOffPeakBaseURL, "")
	require.Equal(t, DefaultOffPeakBaseURL, ResolveOffPeakBaseURL())
	t.Setenv(EnvOffPeakBaseURL, "http://insecure.example/")
	require.Equal(t, DefaultOffPeakBaseURL, ResolveOffPeakBaseURL(), "a non-https override is ignored")
	t.Setenv(EnvOffPeakBaseURL, "https://offpeak.example/")
	require.Equal(t, "https://offpeak.example", ResolveOffPeakBaseURL())
}

func TestOAuthSessionSerializationPreservesIdentityAndWireProfiles(t *testing.T) {
	original := &OAuthSession{Provider: ProviderBigModel, Identity: DefaultIdentity(), ExpiresAt: time.Now().Add(time.Minute)}
	raw, err := MarshalSession(original)
	require.NoError(t, err)
	var dto sessionDTO
	require.NoError(t, json.Unmarshal(raw, &dto))
	restored := fromSessionDTO("session", dto)
	require.Equal(t, original.Identity, restored.Identity)
	require.Equal(t, original.Identity.ForProtocol("anthropic"), restored.Identity.ForProtocol("anthropic"))
	require.Equal(t, ControlIdentity(original.Identity), ControlIdentity(restored.Identity))
}
