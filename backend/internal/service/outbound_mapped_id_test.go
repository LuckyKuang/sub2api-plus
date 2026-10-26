//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/pkg/zcode"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Unit services bypass production Wire startup. Supply its persistent-ID port
// explicitly here; production has no process-local fallback.
func TestMain(m *testing.M) {
	SetDefaultOutboundIDStore(newTestOutboundIDStore())
	zcode.SetDeviceIDResolver(func(ctx context.Context) (string, error) {
		return resolveOutboundMappedID(ctx, "zcode-device", "installation-default", 4)
	})
	os.Exit(m.Run())
}

type testOutboundIDStore struct {
	mu     sync.Mutex
	values map[string]string
	err    error
}

func newTestOutboundIDStore() *testOutboundIDStore {
	return &testOutboundIDStore{values: map[string]string{}}
}

func (s *testOutboundIDStore) ResolveOutboundID(_ context.Context, namespace, digest, candidate string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	key := namespace + ":" + digest
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	s.values[key] = candidate
	return candidate, nil
}

func TestOutboundMappedIDOfficialUUIDAndPersistence(t *testing.T) {
	// RFC 9562: v4 is random, v7 has Unix milliseconds in its high 48 bits.
	for _, version := range []int{4, 7} {
		store := newTestOutboundIDStore()
		before := time.Now().Add(-time.Second)
		var wg sync.WaitGroup
		values := make(chan string, 24)
		for range 24 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, err := resolveOutboundMappedID(WithOutboundIDStore(context.Background(), store), "session", "tenant:1/owner:2/conversation", version)
				if err != nil {
					t.Error(err)
					return
				}
				values <- v
			}()
		}
		wg.Wait()
		close(values)
		var winner string
		for v := range values {
			if winner == "" {
				winner = v
			}
			require.Equal(t, winner, v)
		}
		parsed, err := uuid.Parse(winner)
		require.NoError(t, err)
		require.Equal(t, uuid.Version(version), parsed.Version())
		require.Equal(t, uuid.RFC4122, parsed.Variant())
		if version == 7 {
			ms := int64(parsed[0])<<40 | int64(parsed[1])<<32 | int64(parsed[2])<<24 | int64(parsed[3])<<16 | int64(parsed[4])<<8 | int64(parsed[5])
			require.True(t, time.UnixMilli(ms).After(before))
			require.True(t, time.UnixMilli(ms).Before(time.Now().Add(time.Second)))
		}
		// New operation context represents another replica sharing the store.
		resumed, err := resolveOutboundMappedID(WithOutboundIDStore(context.Background(), store), "session", "tenant:1/owner:2/conversation", version)
		require.NoError(t, err)
		require.Equal(t, winner, resumed)
		other, err := resolveOutboundMappedID(WithOutboundIDStore(context.Background(), store), "session", "tenant:2/owner:2/conversation", version)
		require.NoError(t, err)
		require.NotEqual(t, winner, other)
	}
}

func TestOutboundMappedIDStoreFailureDoesNotInventIdentity(t *testing.T) {
	for _, store := range []OutboundIDStore{nil, &testOutboundIDStore{err: errors.New("offline")}} {
		got, err := resolveOutboundMappedID(WithOutboundIDStore(context.Background(), store), "session", "scope", 7)
		require.Empty(t, got)
		require.ErrorIs(t, err, ErrOutboundIDStoreUnavailable)
	}
}
