//go:build unit

package admin

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
)

// Admin fixtures bypass Wire. Install the same required ports explicitly;
// production retains durable storage and has no in-memory fallback.
func TestMain(m *testing.M) {
	service.SetDefaultOutboundIDStore(&adminOutboundIDStore{values: map[string]string{}})
	zcode.SetDeviceIDResolver(func(context.Context) (string, error) {
		return "b19657bf-40ba-4923-8498-e575197f3f27", nil
	})
	os.Exit(m.Run())
}

type adminOutboundIDStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *adminOutboundIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := namespace + ":" + digest
	if value := s.values[key]; value != "" {
		return value, nil
	}
	s.values[key] = candidate
	return candidate, nil
}
