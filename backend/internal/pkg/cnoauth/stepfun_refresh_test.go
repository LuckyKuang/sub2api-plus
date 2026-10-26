//go:build unit

package cnoauth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStepAccountRefreshFailureDoesNotDestroyStoredSession(t *testing.T) {
	original := &StepLogin{AccessToken: "access", RefreshToken: "long-lived-refresh", DeviceID: "device", AppID: "app"}
	for _, tc := range []struct {
		body    map[string]any
		expired bool
	}{
		{map[string]any{"accessToken": map[string]any{"raw": "anonymous", "mode": 1}}, true},
		{map[string]any{"accessToken": map[string]any{"raw": "drift"}}, false},
		{map[string]any{"future": "schema"}, false},
	} {
		c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) { return reply(tc.body), nil })}
		_, err := c.RefreshStepLogin(context.Background(), "cn", "", original)
		require.Error(t, err)
		if tc.expired {
			require.ErrorIs(t, err, ErrStepAccountSessionExpired)
		} else {
			require.NotErrorIs(t, err, ErrStepAccountSessionExpired)
		}
		require.Equal(t, "long-lived-refresh", original.RefreshToken)
		require.Equal(t, "access", original.AccessToken)
	}
}
