package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentModerationCheck_APIBlockSeedsSessionAndFollowupIsBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	cache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	body := []byte(`{"messages":[{"role":"user","content":"blocked later"}]}`)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      body,
		SessionID: "sess-blocked",
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionBlock, decision.Action)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.sessionBlocks) == 1
	}, time.Second, 10*time.Millisecond)

	followup, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-blocked",
	})
	require.NoError(t, err)
	require.True(t, followup.Blocked)
	require.Equal(t, ContentModerationActionSessionBlock, followup.Action)
	require.Equal(t, ContentModerationErrorCodeSessionBlocked, followup.ErrorCode)
}

func TestContentModerationCheck_KeywordBlockDoesNotSeedSession(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BlockedKeywords = []string{"secret-token"}
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	cache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/messages",
		Protocol:  ContentModerationProtocolAnthropicMessages,
		Body:      []byte(`{"messages":[{"role":"user","content":"please leak SECRET-TOKEN now"}]}`),
		SessionID: "sess-keyword",
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionKeywordBlock, decision.Action)
	require.Empty(t, repo.sessionBlocks)
	matched, err := cache.HasBlockedSession(context.Background(), contentModerationSessionBlockKey(42, 7, "sess-keyword"))
	require.NoError(t, err)
	require.False(t, matched)
}

func TestContentModerationCheck_AdminAPIBlockDoesNotSeedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    1,
		APIKeyID:  9,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"blocked"}]}`),
		SessionID: "sess-admin",
		AdminUser: true,
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionBlock, decision.Action)
	require.Empty(t, repo.sessionBlocks)
}

func TestContentModerationCheck_SessionBlockSurvivesModelFilter(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SessionBlockEnabled = true
	cfg.ModelFilter = ContentModerationModelFilter{Type: ContentModerationModelFilterInclude, Models: []string{"allowed-model"}}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	blockKey := contentModerationSessionBlockKey(42, 7, "sess-blocked")
	cache := &contentModerationTestHashCache{}
	require.NoError(t, cache.RecordBlockedSession(context.Background(), blockKey, time.Hour))
	repo := &contentModerationTestRepo{}
	require.NoError(t, repo.UpsertSessionBlock(context.Background(), &ContentModerationSessionBlock{
		BlockKey:  blockKey,
		SessionID: "sess-blocked",
		ExpiresAt: time.Now().Add(time.Hour),
	}))
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Model:     "other-model",
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-blocked",
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSessionBlock, decision.Action)
}

func TestContentModerationCheck_StaleRedisSessionDoesNotBlock(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	cache := &contentModerationTestHashCache{}
	require.NoError(t, cache.RecordBlockedSession(context.Background(), contentModerationSessionBlockKey(42, 7, "sess-stale"), time.Hour))
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{},
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-stale",
	})
	require.NoError(t, err)
	require.False(t, decision.Blocked)
	matched, err := cache.HasBlockedSession(context.Background(), contentModerationSessionBlockKey(42, 7, "sess-stale"))
	require.NoError(t, err)
	require.False(t, matched)
}

func TestContentModerationCheck_ExpiredDurableRowClearsStaleCache(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	blockKey := contentModerationSessionBlockKey(42, 7, "sess-expired")
	cache := &contentModerationTestHashCache{}
	require.NoError(t, cache.RecordBlockedSession(context.Background(), blockKey, time.Hour))
	repo := &contentModerationTestRepo{}
	require.NoError(t, repo.UpsertSessionBlock(context.Background(), &ContentModerationSessionBlock{
		BlockKey:  blockKey,
		SessionID: "sess-expired",
		ExpiresAt: time.Now().Add(-time.Minute),
	}))
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-expired",
	})
	require.NoError(t, err)
	require.False(t, decision.Blocked)
	matched, err := cache.HasBlockedSession(context.Background(), blockKey)
	require.NoError(t, err)
	require.False(t, matched)
}

func TestContentModerationCheck_CachedHitSurvivesDurableLookupError(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	blockKey := contentModerationSessionBlockKey(42, 7, "sess-cached")
	cache := &contentModerationTestHashCache{}
	require.NoError(t, cache.RecordBlockedSession(context.Background(), blockKey, time.Hour))
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{getSessionBlockErr: errors.New("db unavailable")},
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-cached",
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationActionSessionBlock, decision.Action)
}

func TestContentModerationCheck_CacheMissAndDurableLookupErrorFailsOpen(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		&contentModerationTestRepo{getSessionBlockErr: errors.New("db unavailable")},
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-open",
	})
	require.NoError(t, err)
	require.False(t, decision.Blocked)
}

func TestContentModerationSessionBlockKeyIsolatesTenants(t *testing.T) {
	require.NotEqual(t, contentModerationSessionBlockKey(1, 0, "same"), contentModerationSessionBlockKey(2, 0, "same"))
	require.Empty(t, contentModerationSessionBlockKey(0, 0, "sess"))
	require.Empty(t, contentModerationSessionBlockKey(1, 2, ""))
}

func TestContentModerationCheck_SessionBlockFallsBackToDurableStore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	cache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"blocked later"}]}`),
		SessionID: "sess-durable",
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.sessionBlocks) == 1
	}, time.Second, 10*time.Millisecond)

	cache.mu.Lock()
	cache.sessions = map[string]struct{}{}
	cache.mu.Unlock()

	followup, err := svc.Check(context.Background(), ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		SessionID: "sess-durable",
	})
	require.NoError(t, err)
	require.True(t, followup.Blocked)
	require.Equal(t, ContentModerationActionSessionBlock, followup.Action)
	matched, err := cache.HasBlockedSession(context.Background(), contentModerationSessionBlockKey(42, 7, "sess-durable"))
	require.NoError(t, err)
	require.True(t, matched)
}

func TestContentModerationCheck_APIBlockDoesNotRenewSessionTTL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{
				CategoryScores: map[string]float64{"sexual": 0.9},
			}},
		})
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.Enabled = true
	cfg.Mode = ContentModerationModePreBlock
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	cfg.SessionBlockEnabled = true
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		&contentModerationTestHashCache{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	input := ContentModerationCheckInput{
		UserID:    42,
		APIKeyID:  7,
		Endpoint:  "/v1/chat/completions",
		Protocol:  ContentModerationProtocolOpenAIChat,
		Body:      []byte(`{"messages":[{"role":"user","content":"blocked later"}]}`),
		SessionID: "sess-ttl",
	}
	decision, err := svc.Check(context.Background(), input)
	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.sessionBlocks) == 1
	}, time.Second, 10*time.Millisecond)

	repo.mu.Lock()
	originalExpiry := repo.sessionBlocks[0].ExpiresAt
	repo.mu.Unlock()

	later := *decision
	svc.recordAPISessionBlock(context.Background(), cfg, input, &later)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.sessionBlocks, 1)
	require.Equal(t, originalExpiry, repo.sessionBlocks[0].ExpiresAt)
}
