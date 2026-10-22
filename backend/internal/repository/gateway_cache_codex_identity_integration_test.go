//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheCodexTurnStateProvenanceRealRedis(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	writer := &gatewayCache{rdb: rdb}
	reader := &gatewayCache{rdb: rdb}

	const (
		seed  = "tenant-42\x00private-client-session\x00execution-scope\x00exact-blob-digest"
		owner = "owner-v2:principal-digest-not-an-account-id"
		ttl   = time.Minute
	)
	// The storage contract is SHA-256 of the entire provenance seed, not a raw
	// session or a local row ID. Compute the expected key independently.
	digest := sha256.Sum256([]byte(seed))
	key := "openai_codex_turn_state_origin:" + hex.EncodeToString(digest[:])

	missing, err := reader.GetOpenAICodexTurnStateOrigin(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, missing)
	require.NoError(t, writer.SetOpenAICodexTurnStateOrigin(ctx, seed, owner, ttl))

	storedOwner, err := reader.GetOpenAICodexTurnStateOrigin(ctx, seed)
	require.NoError(t, err)
	require.Equal(t, owner, storedOwner, "a replica must retain the immutable string owner")
	rawOwner, err := rdb.Get(ctx, key).Result()
	require.NoError(t, err)
	require.Equal(t, owner, rawOwner)
	keys := make([]string, 0, 1)
	iter := rdb.Scan(ctx, 0, "openai_codex_turn_state_origin:*", 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	require.NoError(t, iter.Err())
	require.Len(t, keys, 1)
	require.Contains(t, keys[0], key)
	require.NotContains(t, keys[0], "private-client-session")
	require.NotContains(t, keys[0], "execution-scope")
	remaining, err := rdb.PTTL(ctx, key).Result()
	require.NoError(t, err)
	assertTTLWithin(t, remaining, 45*time.Second, ttl)

	otherBlob, err := reader.GetOpenAICodexTurnStateOrigin(ctx, seed+"-different-blob")
	require.NoError(t, err)
	require.Empty(t, otherBlob, "one blob cannot authorize another blob")

	// Real Redis expiry is exercised without waiting for a wall-clock timeout.
	expired, err := rdb.PExpire(ctx, key, 0).Result()
	require.NoError(t, err)
	require.True(t, expired)
	storedOwner, err = reader.GetOpenAICodexTurnStateOrigin(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, storedOwner, "expired provenance must not authorize replay")

	require.NoError(t, rdb.Set(ctx, key, "42", ttl).Err())
	storedOwner, err = reader.GetOpenAICodexTurnStateOrigin(ctx, seed)
	require.NoError(t, err)
	require.Empty(t, storedOwner, "legacy numeric account IDs are not principal evidence")
}

func TestGatewayCacheLiveCallSnapshotRealRedis(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	writer := &gatewayCache{rdb: rdb}
	reader := &gatewayCache{rdb: rdb}
	createdAt := time.UnixMilli(1792600000123)
	callID := "private-live-call"
	callDigest := sha256.Sum256([]byte(callID))
	callHash := hex.EncodeToString(callDigest[:])
	key := "live:call:" + callHash
	record := &service.LiveCallRecord{
		CallID:                callID,
		CallHash:              callHash,
		AccountID:             11,
		APIKeyID:              22,
		UserID:                33,
		GroupID:               44,
		SubscriptionID:        55,
		LeaseID:               "live-lease",
		Model:                 "gpt-live-test",
		CreatedAt:             createdAt,
		ExpiresAt:             createdAt.Add(time.Hour),
		Controller:            service.LiveControllerPending,
		UserAgent:             "inbound-client-agent",
		IPAddress:             "192.0.2.42",
		InboundEndpoint:       "/v1/realtime/calls",
		AttestationCiphertext: "encrypted-attestation",
		OutboundSnapshot: &service.LiveOutboundSnapshot{
			Owner:      "owner-v2:original-upstream-principal",
			SessionID:  "01912345-6789-7abc-8def-0123456789ab",
			ThreadID:   "01912345-7000-7abc-8def-0123456789ab",
			UserAgent:  "codex_cli_rs/0.163.0 (Ubuntu 24.04; x86_64) xterm-256color",
			Originator: "codex_cli_rs",
			Version:    "0.163.0",
			Residency:  "us",
		},
	}
	require.NoError(t, writer.SaveLiveCall(ctx, record, time.Minute))
	loaded, err := reader.GetLiveCall(ctx, callHash)
	require.NoError(t, err)
	require.Equal(t, record, loaded, "all record and snapshot fields survive replica loading")
	remaining, err := rdb.PTTL(ctx, key).Result()
	require.NoError(t, err)
	assertTTLWithin(t, remaining, 45*time.Second, time.Minute)

	assertController := func(controller, controllerOwner string) {
		t.Helper()
		current, loadErr := reader.GetLiveCall(ctx, callHash)
		require.NoError(t, loadErr)
		require.Equal(t, controller, current.Controller)
		require.Equal(t, controllerOwner, current.ControllerOwner)
		require.Equal(t, record.OutboundSnapshot, current.OutboundSnapshot,
			"controller ownership must not replace the credential owner's call snapshot")
	}

	claimed, err := writer.ClaimLiveController(ctx, callHash, service.LiveControllerObserver, "observer-1")
	require.NoError(t, err)
	require.True(t, claimed)
	assertController(service.LiveControllerObserver, "observer-1")
	claimed, err = reader.ClaimLiveController(ctx, callHash, service.LiveControllerObserver, "observer-2")
	require.NoError(t, err)
	require.False(t, claimed, "an existing observer cannot be replaced by another observer")

	claimed, err = writer.ClaimLiveController(ctx, callHash, service.LiveControllerProxy, "proxy-1")
	require.NoError(t, err)
	require.True(t, claimed)
	assertController(service.LiveControllerProxy, "proxy-1")
	claimed, err = reader.ClaimLiveController(ctx, callHash, service.LiveControllerProxy, "proxy-2")
	require.NoError(t, err)
	require.False(t, claimed, "another proxy cannot steal a claimed controller")
	released, err := reader.ReleaseLiveController(ctx, callHash, "proxy-2")
	require.NoError(t, err)
	require.False(t, released, "only the controller owner can release it")
	assertController(service.LiveControllerProxy, "proxy-1")

	released, err = writer.ReleaseLiveController(ctx, callHash, "proxy-1")
	require.NoError(t, err)
	require.True(t, released)
	assertController(service.LiveControllerPending, "")
	closed, err := reader.MarkLiveCallClosed(ctx, callHash, 30*time.Second)
	require.NoError(t, err)
	require.True(t, closed)
	assertController(service.LiveControllerClosed, "")
	remaining, err = rdb.PTTL(ctx, key).Result()
	require.NoError(t, err)
	assertTTLWithin(t, remaining, 15*time.Second, 30*time.Second)
	closed, err = writer.MarkLiveCallClosed(ctx, callHash, time.Minute)
	require.NoError(t, err)
	require.False(t, closed, "closing a call is idempotent across replicas")
	claimed, err = writer.ClaimLiveController(ctx, callHash, service.LiveControllerProxy, "proxy-3")
	require.NoError(t, err)
	require.False(t, claimed, "a closed call cannot be reclaimed")
	assertController(service.LiveControllerClosed, "")

	expired, err := rdb.PExpire(ctx, key, 0).Result()
	require.NoError(t, err)
	require.True(t, expired)
	_, err = reader.GetLiveCall(ctx, callHash)
	require.ErrorIs(t, err, service.ErrLiveCallNotFound)
}
