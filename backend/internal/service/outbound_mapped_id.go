package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// OutboundIDStore atomically retains the first random ID for a logical scope.
// The digest contains no raw conversation, tenant identifier or credential.
// Records have no scheduling-cache TTL: resumed conversations retain their IDs.
type OutboundIDStore interface {
	ResolveOutboundID(ctx context.Context, namespace, digest, candidate string) (string, error)
}

type outboundIDStoreKey struct{}
type outboundIDStoreBinding struct{ store OutboundIDStore }

var outboundIDRuntime struct {
	sync.RWMutex
	store OutboundIDStore
}

var ErrOutboundIDStoreUnavailable = errors.New("outbound identity mapping unavailable")

func WithOutboundIDStore(ctx context.Context, store OutboundIDStore) context.Context {
	return context.WithValue(ctx, outboundIDStoreKey{}, outboundIDStoreBinding{store: store})
}

// SetDefaultOutboundIDStore installs the application-owned persistent mapping port.
// Startup must supply it before requests are served; there is no fallback store.
func SetDefaultOutboundIDStore(store OutboundIDStore) {
	outboundIDRuntime.Lock()
	defer outboundIDRuntime.Unlock()
	outboundIDRuntime.store = store
}

func resolveOutboundMappedID(ctx context.Context, namespace, scopeKey string, version int) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if namespace == "" || len(namespace) > 64 || strings.ContainsAny(namespace, "\r\n\x00") || scopeKey == "" {
		return "", fmt.Errorf("%w: invalid scope", ErrOutboundIDStoreUnavailable)
	}
	var candidate uuid.UUID
	var err error
	switch version {
	case 4:
		candidate, err = uuid.NewRandom()
	case 7:
		candidate, err = uuid.NewV7()
	default:
		return "", fmt.Errorf("%w: unsupported UUID version", ErrOutboundIDStoreUnavailable)
	}
	if err != nil {
		return "", fmt.Errorf("%w: random source", ErrOutboundIDStoreUnavailable)
	}
	binding, explicit := ctx.Value(outboundIDStoreKey{}).(outboundIDStoreBinding)
	store := binding.store
	if !explicit {
		outboundIDRuntime.RLock()
		store = outboundIDRuntime.store
		outboundIDRuntime.RUnlock()
	}
	if store == nil {
		return "", ErrOutboundIDStoreUnavailable
	}
	sum := sha256.Sum256([]byte("outbound-id:v1\x00" + namespace + "\x00" + scopeKey))
	value, err := store.ResolveOutboundID(ctx, namespace, hex.EncodeToString(sum[:]), candidate.String())
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrOutboundIDStoreUnavailable, err)
	}
	id, err := uuid.Parse(value)
	if err != nil || id.String() != value || int(id.Version()) != version || id.Variant() != uuid.RFC4122 {
		return "", fmt.Errorf("%w: invalid stored UUID", ErrOutboundIDStoreUnavailable)
	}
	return value, nil
}
