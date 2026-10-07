//go:build unit

package zcode

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIdentityPinsProductVersionAndAcceptsBothOfficialLines(t *testing.T) {
	t.Setenv(VersionEnv, "")
	require.Equal(t, "3.14.3", DefaultVersion)
	require.Equal(t, "ZCode/3.14.3", UserAgent(DefaultVersion))
	require.Equal(t, "zcode", Preset)

	identity := DefaultIdentity()
	require.Equal(t, Preset, identity.Preset)
	require.Equal(t, "ZCode/3.14.3", identity.UserAgent)
	require.Equal(t, ProductToken, identity.Originator)
	require.Equal(t, DefaultVersion, identity.Version)
	require.Equal(t, "compiled_default", identity.Source)
	require.Equal(t, identity.Version, identity.Headers[HeaderAppVersion])
	require.Equal(t, "Z Code@electron", identity.Headers["X-Title"])
	require.Equal(t, "glm", identity.Headers["X-ZCode-Agent"])
	require.Len(t, identity.Headers, 11)

	// Both official version lines are selectable; the preset declares no floor.
	for _, version := range []string{"3.14.3", "0.16.9"} {
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
	for _, valid := range []string{"3.14.3", "0.16.9", "3.15.0-rc.1", "10.0.0", " 3.14.3 "} {
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
	redirect := DesktopRedirectURI("https://zcode.z.ai", "3.14.3")
	parsed, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Equal(t, "https", parsed.Scheme)
	require.Equal(t, "/app/oauth/login", parsed.Path)
	require.Equal(t, "zcode://oauth/callback", parsed.Query().Get("redirect"))
	require.Equal(t, "3.14.3", parsed.Query().Get("app_version"))

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
	store.Set("s-1", session)
	loaded, ok := store.Get("s-1")
	require.True(t, ok)
	require.Equal(t, "secret", loaded.PollToken)
	require.True(t, loaded.TryConsume())
	require.False(t, loaded.TryConsume(), "a session is single-use")

	// Expired sessions are not served.
	expired := &OAuthSession{Provider: ProviderZai, ExpiresAt: time.Now().Add(-time.Second)}
	store.Set("s-2", expired)
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

func TestPickPersonalScopePrefersDefaultsAndSkipsTeamProjects(t *testing.T) {
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

	// Only team projects are visible: no personal scope exists.
	teamOnly := &CustomerInfo{Organizations: []OrganizationInfo{{
		OrganizationID: "org-team",
		Projects:       []ProjectInfo{{ProjectID: "p", ProjectType: json.RawMessage(`2`)}},
	}}}
	organizationID, projectID = PickPersonalScope(teamOnly)
	require.Empty(t, organizationID)
	require.Empty(t, projectID)
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

func TestCredentialClientDerivesPersonalKeyAndJoinsSecret(t *testing.T) {
	var sawCustomerAuth, sawListAuth, sawCopyAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/biz/customer/getCustomerInfo"):
			sawCustomerAuth = r.Header.Get("Authorization")
			writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{
				"organizationId": "org-1", "organizationName": "默认机构",
				"projects": []any{map[string]any{"projectId": "proj-1", "projectName": "默认项目"}},
			}}})
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			sawListAuth = r.Header.Get("Authorization")
			writeEnvelope(t, w, []any{map[string]any{"apiKey": "ak-1", "name": PersonalAPIKeyName}})
		case strings.HasSuffix(r.URL.Path, "/copy/ak-1"):
			sawCopyAuth = r.Header.Get("Authorization")
			writeEnvelope(t, w, map[string]any{"secretKey": "sk-1"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	credential, err := client.ResolveIndividualCodingPlanKey(context.Background(), ProviderBigModel, "bm-token")
	require.NoError(t, err)
	require.Equal(t, "ak-1.sk-1", credential.APIKey)
	require.Equal(t, "org-1", credential.OrganizationID)
	require.Equal(t, "proj-1", credential.ProjectID)

	// BigModel business calls carry the bare token.
	require.Equal(t, "bm-token", sawCustomerAuth)
	require.Equal(t, "bm-token", sawListAuth)
	require.Equal(t, "bm-token", sawCopyAuth)
}

func TestCredentialClientCreatesMissingKeyAndRejectsMissingScope(t *testing.T) {
	var created atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/biz/customer/getCustomerInfo"):
			writeEnvelope(t, w, map[string]any{"organizations": []any{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api_keys"):
			created.Store(true)
			writeEnvelope(t, w, map[string]any{"apiKey": "ak-new", "name": PersonalAPIKeyName})
		case strings.HasSuffix(r.URL.Path, "/copy/ak-new"):
			writeEnvelope(t, w, map[string]any{"secretKey": "sk-new"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	// No personal organization is visible, so the flow must fail before creating
	// anything.
	_, err := client.ResolveIndividualCodingPlanKey(context.Background(), ProviderBigModel, "bm")
	require.ErrorContains(t, err, "no personal organization")
	require.False(t, created.Load())

	_, err = client.ResolveIndividualCodingPlanKey(context.Background(), "unknown", "bm")
	require.Error(t, err)
	_, err = client.ResolveIndividualCodingPlanKey(context.Background(), ProviderBigModel, "  ")
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

func TestCredentialClientZaiUsesBearerOnDerivation(t *testing.T) {
	var seen []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/biz/customer/getCustomerInfo"):
			writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{
				"organizationId": "org-1",
				"projects":       []any{map[string]any{"projectId": "proj-1"}},
			}}})
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			writeEnvelope(t, w, []any{map[string]any{"apiKey": "ak", "name": PersonalAPIKeyName}})
		case strings.HasSuffix(r.URL.Path, "/copy/ak"):
			writeEnvelope(t, w, map[string]any{"secretKey": "sk"})
		}
	}))
	defer server.Close()

	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	credential, err := client.ResolveIndividualCodingPlanKey(context.Background(), ProviderZai, "zai-token")
	require.NoError(t, err)
	require.Equal(t, "ak.sk", credential.APIKey)
	for _, header := range seen {
		require.Equal(t, "Bearer zai-token", header, "Z.ai derivation sends a Bearer-prefixed token")
	}
}

func TestCredentialClientRejectsZaiKeyWithoutSecret(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/biz/customer/getCustomerInfo"):
			writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{
				"organizationId": "org-1",
				"projects":       []any{map[string]any{"projectId": "proj-1"}},
			}}})
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			writeEnvelope(t, w, []any{map[string]any{"apiKey": "ak", "name": PersonalAPIKeyName}})
		default:
			writeEnvelope(t, w, map[string]any{})
		}
	}))
	defer server.Close()

	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	_, err := client.ResolveIndividualCodingPlanKey(context.Background(), ProviderZai, "t")
	require.ErrorContains(t, err, "secret key")

	// BigModel tolerates the missing secret and returns the bare key.
	credential, err := client.ResolveIndividualCodingPlanKey(context.Background(), ProviderBigModel, "t")
	require.NoError(t, err)
	require.Equal(t, "ak", credential.APIKey)
}

func TestCredentialClientResolvesTeamPlanKeyWithScopeHeaders(t *testing.T) {
	var teamHeaders []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordTeam := func() {
			teamHeaders = append(teamHeaders, r.Header.Get("bigmodel-organization")+"/"+r.Header.Get("bigmodel-project"))
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/biz/customer/getCustomerInfo"):
			writeEnvelope(t, w, map[string]any{"organizations": []any{map[string]any{
				"organizationId": "org-team",
				"projects":       []any{map[string]any{"projectId": "proj-team", "projectType": 2}},
			}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/api_keys"):
			recordTeam()
			writeEnvelope(t, w, map[string]any{"apiKey": "ak-team"})
		case strings.HasSuffix(r.URL.Path, "/api_keys"):
			recordTeam()
			writeEnvelope(t, w, []any{})
		case strings.HasSuffix(r.URL.Path, "/copy/ak-team"):
			recordTeam()
			writeEnvelope(t, w, map[string]any{"secretKey": "sk-team"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewCredentialClient(server.Client(), server.URL, server.URL)
	credential, err := client.ResolveTeamPlanKey(context.Background(), ProviderBigModel, "bm", "org-team", "proj-team")
	require.NoError(t, err)
	require.Equal(t, "ak-team.sk-team", credential.APIKey)
	require.Len(t, teamHeaders, 3, "list, create and copy all carry the team scope")
	for _, header := range teamHeaders {
		require.Equal(t, "org-team/proj-team", header)
	}

	// A project the account cannot see is rejected before any key is created.
	_, err = client.ResolveTeamPlanKey(context.Background(), ProviderBigModel, "bm", "org-team", "other")
	require.ErrorContains(t, err, "not visible")
	_, err = client.ResolveTeamPlanKey(context.Background(), ProviderBigModel, "bm", "", "proj-team")
	require.Error(t, err)
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
	auth := OffPeakAuth{JWT: "jwt", PlanKey: "ak.sk", TeamOrg: "org", TeamProj: "proj", RequestID: "req-1"}
	headers := auth.Headers()
	require.Equal(t, "Bearer jwt", headers["Authorization"])
	require.Equal(t, "ak.sk", headers["X-Coding-Plan-Api-Key"])
	require.Equal(t, "org", headers["bigmodel-organization"])
	require.Equal(t, "proj", headers["bigmodel-project"])
	require.Equal(t, "req-1", headers["x-request-id"])

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
