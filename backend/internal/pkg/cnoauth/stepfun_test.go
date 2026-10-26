//go:build unit

package cnoauth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStepFunRejectsMalformedInitialization(t *testing.T) {
	for _, response := range []map[string]any{
		{"flow_id": "../escape", "poll_interval_sec": 2, "expires_at": 2000000000},
		{"flow_id": "flow", "poll_interval_sec": 0, "expires_at": 2000000000},
		{"flow_id": "flow", "poll_interval_sec": 2, "expires_at": 1},
		{"status": 1, "flow_id": "flow", "poll_interval_sec": 2, "expires_at": 2000000000},
	} {
		c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) { return reply(response), nil })}
		_, err := c.Start(context.Background(), "stepfun", "cn", "")
		require.Error(t, err)
	}
}

func TestStepFunOptionalSessionFailurePreservesModelCredential(t *testing.T) {
	c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) {
		return reply(map[string]any{"state": "ready", "credential": map[string]any{"api_key": "model-key", "login": map[string]any{"access_token": "incomplete"}}}), nil
	})}
	f := &Flow{Platform: "stepfun", Region: "cn", FlowID: "flow", DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute)}
	grant, err := c.Poll(context.Background(), f, "")
	require.NoError(t, err)
	require.Equal(t, "model-key", grant.AccessToken)
	require.Nil(t, grant.StepLogin)
	require.True(t, grant.ExpiresAt.IsZero())
	f.ExpiresAt = time.Now().Add(-time.Second)
	_, err = c.Poll(context.Background(), f, "")
	require.ErrorIs(t, err, ErrExpired)
}

func TestStepFunCancelledResponseCannotDeliverGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{HTTP: doerFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return reply(map[string]any{"state": "ready", "credential": map[string]any{"api_key": "late"}}), nil
	})}
	_, err := c.Poll(ctx, &Flow{Platform: "stepfun", Region: "cn", FlowID: "flow", DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute)}, "")
	require.ErrorIs(t, err, context.Canceled)
}
