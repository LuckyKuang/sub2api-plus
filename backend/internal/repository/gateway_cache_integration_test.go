//go:build integration

package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type GatewayCacheSuite struct {
	IntegrationRedisSuite
	cache service.GatewayCache
}

func (s *GatewayCacheSuite) SetupTest() {
	s.IntegrationRedisSuite.SetupTest()
	s.cache = NewGatewayCache(s.rdb)
}

func (s *GatewayCacheSuite) TestGetSessionAccountID_Missing() {
	_, err := s.cache.GetSessionAccountID(s.ctx, 1, "nonexistent")
	require.True(s.T(), errors.Is(err, service.ErrStickySessionNotFound), "expected ErrStickySessionNotFound for missing session")
}

func (s *GatewayCacheSuite) TestSetAndGetSessionAccountID() {
	sessionID := "s1"
	accountID := int64(99)
	groupID := int64(1)
	sessionTTL := 1 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, sessionTTL), "SetSessionAccountID")

	sid, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.NoError(s.T(), err, "GetSessionAccountID")
	require.Equal(s.T(), accountID, sid, "session id mismatch")
}

func (s *GatewayCacheSuite) TestSessionAccountID_TTL() {
	sessionID := "s2"
	accountID := int64(100)
	groupID := int64(1)
	sessionTTL := 1 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, sessionTTL), "SetSessionAccountID")

	sessionKey := buildSessionKey(groupID, sessionID)
	ttl, err := s.rdb.TTL(s.ctx, sessionKey).Result()
	require.NoError(s.T(), err, "TTL sessionKey after Set")
	s.AssertTTLWithin(ttl, 1*time.Second, sessionTTL)
}

func (s *GatewayCacheSuite) TestRefreshSessionTTL() {
	sessionID := "s3"
	accountID := int64(101)
	groupID := int64(1)
	initialTTL := 1 * time.Minute
	refreshTTL := 3 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, initialTTL), "SetSessionAccountID")

	require.NoError(s.T(), s.cache.RefreshSessionTTL(s.ctx, groupID, sessionID, refreshTTL), "RefreshSessionTTL")

	sessionKey := buildSessionKey(groupID, sessionID)
	ttl, err := s.rdb.TTL(s.ctx, sessionKey).Result()
	require.NoError(s.T(), err, "TTL after Refresh")
	s.AssertTTLWithin(ttl, 1*time.Second, refreshTTL)
}

func (s *GatewayCacheSuite) TestRefreshSessionTTL_MissingKey() {
	// RefreshSessionTTL on a missing key should not error (no-op)
	err := s.cache.RefreshSessionTTL(s.ctx, 1, "missing-session", 1*time.Minute)
	require.NoError(s.T(), err, "RefreshSessionTTL on missing key should not error")
}

func (s *GatewayCacheSuite) TestDeleteSessionAccountID() {
	sessionID := "openai:s4"
	accountID := int64(102)
	groupID := int64(1)
	sessionTTL := 1 * time.Minute

	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, accountID, sessionTTL), "SetSessionAccountID")
	require.NoError(s.T(), s.cache.DeleteSessionAccountID(s.ctx, groupID, sessionID), "DeleteSessionAccountID")

	_, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.True(s.T(), errors.Is(err, service.ErrStickySessionNotFound), "expected ErrStickySessionNotFound after delete")
}

func (s *GatewayCacheSuite) TestCompareAndDeleteSessionAccountID() {
	// docs/providers/ANTIGRAVITY.md: a failed committed stream must never
	// erase the newer account binding installed by concurrent failover.
	const sessionID = "antigravity:compare-delete"
	const groupID int64 = 1
	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, 5, time.Minute))
	observed, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(5), observed)
	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, groupID, sessionID, 6, time.Minute))

	removed, err := s.cache.CompareAndDeleteSessionAccountID(s.ctx, groupID, sessionID, observed)
	require.NoError(s.T(), err)
	require.False(s.T(), removed)
	current, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(6), current)

	// An equal binding in another group is outside this release's scope.
	require.NoError(s.T(), s.cache.SetSessionAccountID(s.ctx, 2, sessionID, 6, time.Minute))
	removed, err = s.cache.CompareAndDeleteSessionAccountID(s.ctx, groupID, sessionID, 6)
	require.NoError(s.T(), err)
	require.True(s.T(), removed)
	_, err = s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.ErrorIs(s.T(), err, service.ErrStickySessionNotFound)
	current, err = s.cache.GetSessionAccountID(s.ctx, 2, sessionID)
	require.NoError(s.T(), err)
	require.Equal(s.T(), int64(6), current)

	removed, err = s.cache.CompareAndDeleteSessionAccountID(s.ctx, groupID, sessionID, 6)
	require.NoError(s.T(), err)
	require.False(s.T(), removed, "an absent binding is a no-op")

	// Corrupt cache values must also survive a mismatched release.
	key := buildSessionKey(groupID, sessionID)
	require.NoError(s.T(), s.rdb.Set(s.ctx, key, "invalid", time.Minute).Err())
	removed, err = s.cache.CompareAndDeleteSessionAccountID(s.ctx, groupID, sessionID, 6)
	require.NoError(s.T(), err)
	require.False(s.T(), removed)
	require.Equal(s.T(), "invalid", s.rdb.Get(s.ctx, key).Val())
}

func (s *GatewayCacheSuite) TestGetSessionAccountID_CorruptedValue() {
	sessionID := "corrupted"
	groupID := int64(1)
	sessionKey := buildSessionKey(groupID, sessionID)

	// Set a non-integer value
	require.NoError(s.T(), s.rdb.Set(s.ctx, sessionKey, "not-a-number", 1*time.Minute).Err(), "Set invalid value")

	_, err := s.cache.GetSessionAccountID(s.ctx, groupID, sessionID)
	require.Error(s.T(), err, "expected error for corrupted value")
	require.False(s.T(), errors.Is(err, service.ErrStickySessionNotFound), "expected parsing error, not a miss")
}

func TestGatewayCacheSuite(t *testing.T) {
	suite.Run(t, new(GatewayCacheSuite))
}
