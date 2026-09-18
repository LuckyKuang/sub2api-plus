//go:build unit || !integration

package openai

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionStore_Stop_Idempotent(t *testing.T) {
	store := NewSessionStore()

	store.Stop()
	store.Stop()

	select {
	case <-store.stopCh:
		// ok
	case <-time.After(time.Second):
		t.Fatal("stopCh 未关闭")
	}
}

func TestSessionStore_Stop_Concurrent(t *testing.T) {
	store := NewSessionStore()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.Stop()
		}()
	}

	wg.Wait()

	select {
	case <-store.stopCh:
		// ok
	case <-time.After(time.Second):
		t.Fatal("stopCh 未关闭")
	}
}

func TestBuildAuthorizationURLForPlatform_OpenAI(t *testing.T) {
	authURL := BuildAuthorizationURLForPlatform("state-1", "challenge-1", DefaultRedirectURI, OAuthPlatformOpenAI)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Parse URL failed: %v", err)
	}
	q := parsed.Query()
	if got := q.Get("client_id"); got != ClientID {
		t.Fatalf("client_id mismatch: got=%q want=%q", got, ClientID)
	}
	if got := q.Get("codex_cli_simplified_flow"); got != "true" {
		t.Fatalf("codex flow mismatch: got=%q want=true", got)
	}
	if got := q.Get("id_token_add_organizations"); got != "true" {
		t.Fatalf("id_token_add_organizations mismatch: got=%q want=true", got)
	}
	if got := q.Get("scope"); got != DefaultScopes {
		t.Fatalf("scope mismatch: got=%q want=%q", got, DefaultScopes)
	}
	if got := q.Get("originator"); got != CodexDefaultOriginator {
		t.Fatalf("originator mismatch: got=%q want=%q", got, CodexDefaultOriginator)
	}
	wantScope := "scope=" + strings.ReplaceAll(url.QueryEscape(DefaultScopes), "+", "%20")
	if !strings.Contains(parsed.RawQuery, wantScope) {
		t.Fatalf("scope should use official percent-encoding, raw=%q", parsed.RawQuery)
	}
}

func TestBuildAuthorizationURL_OfficialQueryOrder(t *testing.T) {
	authURL := BuildAuthorizationURLWithOriginator("state-1", "challenge-1", DefaultRedirectURI, OAuthPlatformOpenAI, CodexDefaultOriginator)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("Parse URL failed: %v", err)
	}
	wantRedirect := strings.ReplaceAll(url.QueryEscape(DefaultRedirectURI), "+", "%20")
	wantScope := strings.ReplaceAll(url.QueryEscape(DefaultScopes), "+", "%20")
	want := strings.Join([]string{
		"response_type=code",
		"client_id=" + ClientID,
		"redirect_uri=" + wantRedirect,
		"scope=" + wantScope,
		"code_challenge=challenge-1",
		"code_challenge_method=S256",
		"id_token_add_organizations=true",
		"codex_cli_simplified_flow=true",
		"state=state-1",
		"originator=" + CodexDefaultOriginator,
	}, "&")
	if parsed.RawQuery != want {
		t.Fatalf("authorize query order mismatch\n got=%q\nwant=%q", parsed.RawQuery, want)
	}
}

func TestEncodeAuthorizationCodeTokenBody_OfficialFieldOrder(t *testing.T) {
	got := EncodeAuthorizationCodeTokenBody("code/1", "http://localhost:1455/auth/callback", ClientID, "verifier")
	wantRedirect := strings.ReplaceAll(url.QueryEscape("http://localhost:1455/auth/callback"), "+", "%20")
	want := "grant_type=authorization_code&code=" + url.QueryEscape("code/1") + "&redirect_uri=" + wantRedirect + "&client_id=" + ClientID + "&code_verifier=verifier"
	if got != want {
		t.Fatalf("token body mismatch\n got=%q\nwant=%q", got, want)
	}
}

func TestGenerateCodeVerifier_OfficialEncoding(t *testing.T) {
	verifier, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("verifier length %d outside RFC 7636 range", len(verifier))
	}
	if _, err := url.ParseQuery("code_verifier=" + verifier); err != nil {
		t.Fatalf("verifier must be query-safe: %v", err)
	}
}

func TestGenerateState_OfficialEncoding(t *testing.T) {
	state, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if stringsContainsAny(state, "+/=") {
		t.Fatalf("state should be base64url without padding: %q", state)
	}
}

func TestDeviceUserCodeResponse_UnmarshalOfficialPayload(t *testing.T) {
	var got DeviceUserCodeResponse
	if err := json.Unmarshal([]byte(`{"device_auth_id":"dev-1","usercode":"ABCD-1234","interval":"5"}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.DeviceAuthID != "dev-1" || got.UserCode != "ABCD-1234" || got.Interval != 5 {
		t.Fatalf("got %+v", got)
	}
}

func TestDeviceUserCodeAndRevokeJSONBodies_OfficialFieldOrder(t *testing.T) {
	got, err := json.Marshal(DeviceUserCodeRequest{ClientID: ClientID})
	if err != nil {
		t.Fatalf("DeviceUserCodeRequest: %v", err)
	}
	want := `{"client_id":"` + ClientID + `"}`
	if string(got) != want {
		t.Fatalf("usercode body mismatch\n got=%s\nwant=%s", got, want)
	}

	got, err = json.Marshal(DeviceTokenPollRequest{DeviceAuthID: "dev-1", UserCode: "ABCD-1234"})
	if err != nil {
		t.Fatalf("DeviceTokenPollRequest: %v", err)
	}
	want = `{"device_auth_id":"dev-1","user_code":"ABCD-1234"}`
	if string(got) != want {
		t.Fatalf("device poll body mismatch\n got=%s\nwant=%s", got, want)
	}

	got, err = json.Marshal(RevokeTokenRequest{Token: "rt", TokenTypeHint: "refresh_token", ClientID: ClientID})
	if err != nil {
		t.Fatalf("RevokeTokenRequest refresh: %v", err)
	}
	want = `{"token":"rt","token_type_hint":"refresh_token","client_id":"` + ClientID + `"}`
	if string(got) != want {
		t.Fatalf("revoke refresh body mismatch\n got=%s\nwant=%s", got, want)
	}

	got, err = json.Marshal(RevokeTokenRequest{Token: "at", TokenTypeHint: "access_token"})
	if err != nil {
		t.Fatalf("RevokeTokenRequest access: %v", err)
	}
	want = `{"token":"at","token_type_hint":"access_token"}`
	if string(got) != want {
		t.Fatalf("revoke access body mismatch\n got=%s\nwant=%s", got, want)
	}
}

func stringsContainsAny(s, chars string) bool {
	for _, r := range chars {
		if containsRune(s, r) {
			return true
		}
	}
	return false
}

func containsRune(s string, r rune) bool {
	for _, cur := range s {
		if cur == r {
			return true
		}
	}
	return false
}
