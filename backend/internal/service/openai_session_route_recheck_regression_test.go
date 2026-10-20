//go:build unit || !integration

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type affinityRecheckFailureRepo struct {
	schedulerTestOpenAIAccountRepo
}

func (r affinityRecheckFailureRepo) GetByID(context.Context, int64) (*Account, error) {
	return nil, errors.New("fixture repository unavailable")
}

// V08: a healthy cached owner and failed authoritative account recheck cannot
// become permission to acquire or send to another account.
func TestCodexCanonicalAccountRecheckFailureStopsSelectionRegression(t *testing.T) {
	ctx := context.Background()
	owner := &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}
	store := newFakeOpenAISessionRouteStore()
	_, err := store.CreateIfAbsent(ctx, OpenAISessionRoute{AuthzDomain: "group:0", SessionDigest: OpenAISessionRouteDigest("session"), OwnerAccountID: 101})
	require.NoError(t, err)
	var acquired []int64
	svc := &OpenAIGatewayService{
		sessionRouteStore:  store,
		cache:              &schedulerTestGatewayCache{},
		accountRepo:        affinityRecheckFailureRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{*owner, {ID: 102, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}}}},
		schedulerSnapshot:  &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{101: owner}}},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquiredIDs: &acquired}),
	}
	selection, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.ErrorIs(t, err, ErrOpenAISessionRouteStoreUnavailable)
	require.Nil(t, selection)
	require.Empty(t, acquired)
	route, err := store.Get(ctx, "group:0", OpenAISessionRouteDigest("session"))
	require.NoError(t, err)
	require.EqualValues(t, 101, route.OwnerAccountID)
	require.Empty(t, route.LeaseID)
}
