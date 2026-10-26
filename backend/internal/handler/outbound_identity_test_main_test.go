//go:build unit

package handler

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// Handler fixtures bypass Wire and its SQL mapping store. Supply the port
// explicitly; production must never fall back to an in-memory store.
func TestMain(m *testing.M) {
	service.SetDefaultOutboundIDStore(&handlerOutboundIDStore{values: map[string]string{}})
	os.Exit(m.Run())
}

type handlerOutboundIDStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *handlerOutboundIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := namespace + ":" + digest
	if value := s.values[key]; value != "" {
		return value, nil
	}
	s.values[key] = candidate
	return candidate, nil
}
