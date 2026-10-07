package zcode

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/LuckyKuang/sub2api-plus/internal/pkg/redissession"
)

// RedisSessionPrefix namespaces the shared account-link sessions.
const RedisSessionPrefix = "oauth:session:zhipu"

// OAuthSession is one server-held account-link session.
//
// The poll token is the credential that authorizes the handshake, so the session
// owns it on the server: the admin panel only ever sees the opaque session id.
// The poll result itself is never stored here, which keeps credentials out of
// both the Redis payload and the process-heap snapshot.
type OAuthSession struct {
	Identity outboundidentity.Identity `json:"identity"`
	// State is the state value the platform embedded in the authorization URL.
	// It is informational for the polling flow and required by the fallback
	// code-exchange flow.
	State string `json:"state,omitempty"`
	// Provider is the selected upstream estate (bigmodel or zai).
	Provider string `json:"provider"`
	// PollToken authorizes the handshake. It never leaves the server.
	PollToken string `json:"poll_token"`
	// FlowID identifies the platform-side flow.
	FlowID string `json:"flow_id"`
	// AuthorizeURL is the URL the operator opens in a browser.
	AuthorizeURL string `json:"authorize_url"`
	// ExpiresAt bounds the flow.
	ExpiresAt time.Time `json:"expires_at"`
	// PollIntervalSec is the platform-requested poll cadence.
	PollIntervalSec int64 `json:"poll_interval_sec"`
	// ProxyURL is the egress proxy resolved when the flow started.
	ProxyURL  string    `json:"proxy_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// sessionDTO is the Redis-serializable projection of OAuthSession.
type sessionDTO struct {
	Identity        outboundidentity.Identity `json:"identity"`
	State           string                    `json:"state,omitempty"`
	Provider        string                    `json:"provider"`
	PollToken       string                    `json:"poll_token"`
	FlowID          string                    `json:"flow_id"`
	AuthorizeURL    string                    `json:"authorize_url"`
	ExpiresAt       time.Time                 `json:"expires_at"`
	PollIntervalSec int64                     `json:"poll_interval_sec"`
	ProxyURL        string                    `json:"proxy_url,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
}

func toSessionDTO(session *OAuthSession) sessionDTO {
	return sessionDTO{
		Identity:        session.Identity,
		State:           session.State,
		Provider:        session.Provider,
		PollToken:       session.PollToken,
		FlowID:          session.FlowID,
		AuthorizeURL:    session.AuthorizeURL,
		ExpiresAt:       session.ExpiresAt,
		PollIntervalSec: session.PollIntervalSec,
		ProxyURL:        session.ProxyURL,
		CreatedAt:       session.CreatedAt,
	}
}

func fromSessionDTO(id string, dto sessionDTO) *OAuthSession {
	return &OAuthSession{
		Identity:        dto.Identity,
		State:           dto.State,
		Provider:        dto.Provider,
		PollToken:       dto.PollToken,
		FlowID:          dto.FlowID,
		AuthorizeURL:    dto.AuthorizeURL,
		ExpiresAt:       dto.ExpiresAt,
		PollIntervalSec: dto.PollIntervalSec,
		ProxyURL:        dto.ProxyURL,
		CreatedAt:       dto.CreatedAt,
	}
}

// SessionStore uses Redis as the authoritative store when configured. Redis
// errors never degrade a shared authorization to a replayable local session.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*OAuthSession
	stopOnce sync.Once
	stopCh   chan struct{}
	remote   *redissession.Store
}

// NewSessionStore creates a process-local session store.
func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*OAuthSession),
		stopCh:   make(chan struct{}),
	}
	go store.cleanup()
	return store
}

// NewRedisSessionStore creates a store backed by Redis when a client is given,
// using process-local sessions only when no Redis client is configured.
func NewRedisSessionStore(client *redis.Client) *SessionStore {
	store := NewSessionStore()
	if client != nil {
		store.remote = redissession.New(client, RedisSessionPrefix, SessionTTL)
	}
	return store
}

// Set stores session under sessionID.
func (s *SessionStore) Set(sessionID string, session *OAuthSession) error {
	if s == nil || session == nil || sessionID == "" {
		return errors.New("invalid zhipu authorization session")
	}
	if s.remote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.remote.Set(ctx, sessionID, toSessionDTO(session))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
	return nil
}

// Get returns a live session. Shared sessions are never read from a local cache.
func (s *SessionStore) Get(sessionID string) (*OAuthSession, bool) {
	if s == nil || sessionID == "" {
		return nil, false
	}
	var session *OAuthSession
	var ok bool
	if s.remote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var dto sessionDTO
		found, err := s.remote.Get(ctx, sessionID, &dto)
		if err != nil {
			slog.Warn("zhipu oauth session Redis read failed", "error", err)
			return nil, false
		}
		if found {
			session, ok = fromSessionDTO(sessionID, dto), true
		}
	} else {
		s.mu.RLock()
		session, ok = s.sessions[sessionID]
		s.mu.RUnlock()
	}
	if !ok || session == nil {
		return nil, false
	}
	if !session.ExpiresAt.IsZero() && !time.Now().Before(session.ExpiresAt) {
		s.Delete(sessionID)
		return nil, false
	}
	return session, true
}

// TryConsume claims an authorization once across all replicas. The Redis marker
// remains until expiry, including after an ambiguous account-create response.
func (s *SessionStore) TryConsume(sessionID string) bool {
	if _, ok := s.Get(sessionID); !ok {
		return false
	}
	if s.remote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		consumed, err := s.remote.TryConsume(ctx, sessionID)
		if err != nil {
			slog.Warn("zhipu oauth session Redis consume failed", "error", err)
		}
		return err == nil && consumed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sessionID]; !ok {
		return false
	}
	delete(s.sessions, sessionID)
	return true
}

// Delete drops an unused or expired session.
func (s *SessionStore) Delete(sessionID string) {
	if s == nil || sessionID == "" {
		return
	}
	if s.remote != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.remote.Delete(ctx, sessionID)
		return
	}
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

// Stop ends the cleanup goroutine. It is idempotent.
func (s *SessionStore) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.purgeExpired()
		}
	}
}

func (s *SessionStore) purgeExpired() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, session := range s.sessions {
		if session == nil || (!session.ExpiresAt.IsZero() && now.After(session.ExpiresAt)) {
			delete(s.sessions, id)
		}
	}
}

// AuthorizeState returns the state parameter of an authorization URL, if any.
func AuthorizeState(authorizeURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(authorizeURL))
	if err != nil {
		return ""
	}
	return parsed.Query().Get("state")
}

// MarshalSession exposes the Redis projection for diagnostics and tests.
func MarshalSession(session *OAuthSession) ([]byte, error) {
	if session == nil {
		return nil, nil
	}
	return json.Marshal(toSessionDTO(session))
}
