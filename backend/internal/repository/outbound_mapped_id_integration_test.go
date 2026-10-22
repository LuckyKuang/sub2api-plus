//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestOutboundMappedIDRepositoryAtomicAndPersistent(t *testing.T) {
	ctx := context.Background()
	namespace := "test-" + uuid.NewString()
	digest := "6481875d7299874e42501d47b65fe091511a9ef828f82d4d9d82457f2ffb2761"
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM outbound_identity_ids WHERE namespace=$1", namespace)
	})
	var wg sync.WaitGroup
	values := make(chan string, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repo := NewSettingRepository(integrationEntClient).(*settingRepository)
			v, err := repo.ResolveOutboundID(ctx, namespace, digest, uuid.NewString())
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
	require.NotEmpty(t, winner)
	// A new repository instance reads the committed winner rather than using
	// its own random candidate, matching restart and replica behavior.
	resumed, err := NewSettingRepository(integrationEntClient).(*settingRepository).ResolveOutboundID(ctx, namespace, digest, uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, winner, resumed)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM outbound_identity_ids WHERE namespace=$1", namespace).Scan(&count))
	require.Equal(t, 1, count)
}
