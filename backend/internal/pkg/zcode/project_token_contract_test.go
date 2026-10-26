//go:build unit

package zcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type projectTokenWire func(*http.Request) (*http.Response, error)

func (f projectTokenWire) Do(r *http.Request) (*http.Response, error) { return f(r) }

// Oracle: ZCode@3c5b2276 packages/shared/src/project-access-token-client.ts.
// OAuth must never copy or reconstruct long-lived project secrets.
func TestOAuthCodingPlansUseProjectAccessTokens(t *testing.T) {
	for _, family := range []string{"bigmodel", "zai"} {
		for _, team := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/team=%v", family, team), func(t *testing.T) {
				calls := []string{}
				c := NewCredentialClient(projectTokenWire(func(r *http.Request) (*http.Response, error) {
					calls = append(calls, r.Method+" "+r.URL.Path)
					payload := `{"code":0,"data":{"organizations":[{"organizationId":"org","projects":[{"projectId":"project"}]}]}}`
					switch {
					case strings.HasSuffix(r.URL.Path, "getCustomerInfo"):
						require.Equal(t, "login-token", r.Header.Get("Authorization"), "customer lookup uses the bare login token in both estates")
					case strings.HasSuffix(r.URL.Path, "/api_keys"):
						expected := "login-token"
						if family == "zai" && !team {
							expected = "Bearer login-token"
						}
						require.Equal(t, expected, r.Header.Get("Authorization"))
						name := "zcode-api-key"
						if team {
							name = "zcode-team-api-key"
						}
						payload = `{"code":"200","data":[{"apiKey":"key-id","name":"` + name + `","keyType":2}]}`
					case strings.HasSuffix(r.URL.Path, "/key-id/access_tokens"):
						require.Equal(t, http.MethodPost, r.Method)
						require.Equal(t, "Bearer login-token", r.Header.Get("Authorization"))
						var body map[string]any
						require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
						require.Equal(t, "zcode", body["clientType"])
						require.NotEmpty(t, body["clientVersion"])
						payload = fmt.Sprintf(`{"code":0,"data":{"accessToken":"short-lived-pat","tokenType":"Bearer","expiresIn":3600,"expiresAt":%d}}`, time.Now().Add(time.Hour).Unix())
					default:
						t.Errorf("forbidden endpoint: %s", r.URL.Path)
						payload = `{"code":500}`
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}, nil
				}), "https://bigmodel.cn", "https://api.z.ai")
				var credential *CodingPlanCredential
				var err error
				if team {
					credential, err = c.ResolveTeamPlanKey(context.Background(), family, "login-token", "org", "project")
				} else {
					credential, err = c.ResolveIndividualCodingPlanKey(context.Background(), family, "login-token")
				}
				require.NoError(t, err)
				require.Equal(t, "short-lived-pat", credential.APIKey)
				require.Equal(t, "org", credential.OrganizationID)
				require.Equal(t, "project", credential.ProjectID)
				require.Len(t, calls, 3)
				require.NotContains(t, strings.Join(calls, " "), "/copy/")
			})
		}
	}
}

func TestProjectTokenRejectsIncompleteOrDisabledIssuance(t *testing.T) {
	for _, body := range []string{
		`{"data":{"accessToken":"pat","tokenType":"Bearer","expiresIn":3600}}`,
		`{"code":0,"data":{"enable":false,"accessToken":"pat","tokenType":"Bearer","expiresIn":3600}}`,
		`{"code":0,"data":{"accessToken":"pat","tokenType":"Other","expiresIn":3600}}`,
		`{"code":0,"data":{"accessToken":"pat","tokenType":"Bearer","expiresIn":3600,"expiresAt":1}}`,
	} {
		c := NewCredentialClient(projectTokenWire(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		}), "https://bigmodel.cn", "")
		_, err := c.IssueProjectAccessToken(context.Background(), "bigmodel", "login", "org", "project", "key")
		require.Error(t, err, "malformed/disabled PAT must never fall back to a Key ID")
	}
}

func TestProjectTokenRequiresPositiveAbsoluteExpiry(t *testing.T) {
	// Official project-access-token.ts requires a finite positive expiresAt;
	// expiresIn alone cannot turn an incomplete issuance into a credential.
	for _, field := range []string{"", `,"expiresAt":null`, `,"expiresAt":0`, `,"expiresAt":-1`, `,"expiresAt":"1900000000"`} {
		t.Run(field, func(t *testing.T) {
			body := `{"code":0,"data":{"accessToken":"sensitive-pat","tokenType":"Bearer","expiresIn":3600` + field + `}}`
			client := NewCredentialClient(projectTokenWire(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			}), "https://bigmodel.cn", "")
			token, err := client.IssueProjectAccessToken(context.Background(), "bigmodel", "login", "org", "project", "key")
			require.Error(t, err)
			require.Nil(t, token)
			require.NotContains(t, err.Error(), "sensitive-pat")
		})
	}
}
