package cnoauth

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/outboundidentity"
	"github.com/redis/go-redis/v9"
)

var ErrSession = errors.New("authorization session expired or unavailable")
var ErrBusy = errors.New("authorization session is busy; retry shortly")

type Session struct {
	Flow        *Flow                     `json:"flow"`
	OwnerID     int64                     `json:"owner_id"`
	AccountID   int64                     `json:"account_id,omitempty"`
	ProxyID     *int64                    `json:"proxy_id,omitempty"`
	ProxyURL    string                    `json:"proxy_url,omitempty"`
	Identity    outboundidentity.Identity `json:"identity"`
	Grant       *Grant                    `json:"grant,omitempty"`
	NextPoll    time.Time                 `json:"next_poll"`
	CompletedID int64                     `json:"completed_id,omitempty"`
	Committing  bool                      `json:"committing,omitempty"`
	Cancelled   bool                      `json:"cancelled,omitempty"`
}

// Store serializes state transitions across replicas. Redis errors fail closed;
// local sessions are used only when Redis was not configured at construction.
type Store struct {
	redis *redis.Client
	mu    sync.Mutex
	local map[string][]byte
}

func NewStore(client *redis.Client) *Store { return &Store{redis: client, local: map[string][]byte{}} }

const sessionPrefix = "oauth:cn:"

func (s *Store) Create(ctx context.Context, session *Session) (string, error) {
	if session == nil || session.Flow == nil || time.Until(session.Flow.ExpiresAt) <= 0 {
		return "", ErrSession
	}
	id, err := randomToken()
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return "", err
	}
	if s.redis != nil {
		ttl := time.Until(session.Flow.ExpiresAt)
		if ttl <= 0 {
			return "", ErrSession
		}
		err = s.redis.Set(ctx, sessionPrefix+id, raw, ttl).Err()
		return id, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune()
	if len(s.local) >= 1024 {
		return "", errors.New("too many authorization sessions")
	}
	s.local[id] = raw
	return id, nil
}
func (s *Store) prune() {
	for id, raw := range s.local {
		var v Session
		if json.Unmarshal(raw, &v) != nil || v.Flow == nil || time.Now().After(v.Flow.ExpiresAt) {
			delete(s.local, id)
		}
	}
}
func (s *Store) Update(ctx context.Context, id string, fn func(context.Context, *Session) error) error {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if len(id) != 43 {
		return ErrSession
	}
	if s.redis == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.prune()
		if err := ctx.Err(); err != nil {
			return err
		}
		raw, ok := s.local[id]
		if !ok {
			return ErrSession
		}
		var v Session
		if json.Unmarshal(raw, &v) != nil {
			return ErrSession
		}
		err := fn(ctx, &v)
		if err != nil {
			return err
		}
		raw, err = json.Marshal(&v)
		if err == nil {
			s.local[id] = raw
		}
		return err
	}
	// Upstream requests are bounded to 30s. The operation's total budget is shorter
	// than the lease, so a second worker cannot overlap a still-active exchange.
	lease, err := randomToken()
	if err != nil {
		return err
	}
	key := sessionPrefix + id
	lock := key + ":lock"
	ok, err := s.redis.SetNX(ctx, lock, lease, 60*time.Second).Result()
	if err != nil {
		return ErrSession
	}
	if !ok {
		return ErrBusy
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.redis.Eval(releaseCtx, `if redis.call('GET',KEYS[1]) == ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{lock}, lease).Err()
	}()
	raw, err := s.redis.Get(ctx, key).Bytes()
	if err != nil {
		return ErrSession
	}
	var v Session
	if json.Unmarshal(raw, &v) != nil || v.Flow == nil || time.Now().After(v.Flow.ExpiresAt) {
		return ErrSession
	}
	if err = fn(ctx, &v); err != nil {
		return err
	}
	raw, err = json.Marshal(&v)
	if err != nil {
		return err
	}
	// Never resurrect expired state or publish after losing the lease.
	updated, err := s.redis.Eval(ctx, `if redis.call('GET',KEYS[2]) ~= ARGV[1] or redis.call('EXISTS',KEYS[1]) == 0 then return 0 end redis.call('SET',KEYS[1],ARGV[2],'KEEPTTL'); return 1`, []string{key, lock}, lease, string(raw)).Int()
	if err != nil || updated != 1 {
		return ErrSession
	}
	return nil
}
