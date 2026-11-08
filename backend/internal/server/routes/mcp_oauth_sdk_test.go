//go:build unit || !integration

package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// The pinned Go SDK supplies Streamable HTTP. Its unreleased browser OAuth
// handler is not part of v1.4.1: exercise the production consent/code flow via
// the HTTP fixture and the supported oauth2 transport for real token refresh.
func TestMCPOAuthOfficialGoSDKInteroperability(t *testing.T) {
	f := newMCPOAuthHTTPFixture(t, service.RoleUser)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	discovered := f.request(t, http.MethodGet, "/.well-known/oauth-authorization-server", "", "", nil, false)
	require.Equal(t, http.StatusOK, discovered.status)
	var metadata struct {
		TokenEndpoint string `json:"token_endpoint"`
	}
	require.NoError(t, json.Unmarshal(discovered.body, &metadata))
	require.Equal(t, f.server.URL+"/api/v1/mcp/oauth/token", metadata.TokenEndpoint)
	registered, issued := f.authorize(t, service.MCPScopeUser, true)
	ctx = context.WithValue(ctx, oauth2.HTTPClient, f.server.Client())
	config := oauth2.Config{ClientID: registered.ClientID, Endpoint: oauth2.Endpoint{TokenURL: metadata.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
	initial := &oauth2.Token{AccessToken: issued.AccessToken, RefreshToken: issued.RefreshToken, TokenType: "Bearer", Expiry: time.Now().Add(time.Minute)}
	source := config.TokenSource(ctx, initial)
	client := mcp.NewClient(&mcp.Implementation{Name: "oauth-interop-test", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: f.resource, HTTPClient: oauth2.NewClient(ctx, source)}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	require.Contains(t, names, "user_get_api_key")
	require.NotContains(t, names, "user_create_api_key")
	require.NotContains(t, names, "admin_get_dashboard")
	called, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "user_get_api_key", Arguments: map[string]any{"id": 9}})
	require.NoError(t, err)
	require.False(t, called.IsError)
	encoded, err := json.Marshal(called)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "owner-key")
	require.NotContains(t, string(encoded), "must-not-leak")
	oldAccess, oldRefresh := initial.AccessToken, initial.RefreshToken
	require.NotEmpty(t, oldRefresh)
	// oauth2 caches the returned token pointer. Expire that cache entry to drive
	// the SDK's real refresh request without sleeping for the access lifetime.
	initial.Expiry = time.Now().Add(-time.Minute)
	_, err = session.ListTools(ctx, nil)
	require.NoError(t, err)
	rotated, err := source.Token()
	require.NoError(t, err)
	require.NotEqual(t, oldAccess, rotated.AccessToken)
	require.NotEqual(t, oldRefresh, rotated.RefreshToken)
	grants, err := f.oauth.ListGrants(ctx, 7)
	require.NoError(t, err)
	require.Len(t, grants, 1, "refresh must reuse the existing browser consent")
	_, err = f.oauth.Authenticate(ctx, oldAccess)
	require.NoError(t, err, "ordinary refresh preserves in-flight access tokens")
}
