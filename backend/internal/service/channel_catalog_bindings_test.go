//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailableChannelCatalogSkipsUnboundDependencies(t *testing.T) {
	// Available Channels requires an active channel. Unbound account/route
	// configuration is outside this projection and must not fail the page read.
	group := Group{ID: 1, Platform: PlatformComposite, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription}
	svc := newPlazaServiceWithBilling(nil, []Group{group}, nil, nil)
	accounts := &commercialAccountsStub{err: errors.New("unbound account configuration unavailable")}
	svc.SetCommercialCatalogSources(unavailableCommercialRoutes{}, accounts)
	out, err := svc.ListAvailableChannelCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Empty(t, out)
	require.Zero(t, accounts.calls)

	// The same dependency remains required for group quotes and subscriptions;
	// a dependency failure must not be advertised as a valid empty commercial list.
	_, err = svc.ListAvailableCatalog(context.Background(), []Group{group})
	require.ErrorContains(t, err, "unbound account configuration unavailable")
	_, err = svc.ListSaleCatalog(context.Background(), []int64{group.ID})
	require.ErrorContains(t, err, "unbound account configuration unavailable")
}

func TestAvailableChannelCatalogPreservesUnboundSaleOffers(t *testing.T) {
	// Hiding unbound models on the channel page must not remove the configured
	// models a prospective subscriber sees, even without a channel binding.
	group := Group{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformAnthropic, Models: []string{"claude-sale"}}},
	}
	svc := newPlazaServiceWithBilling(nil, []Group{group}, nil, nil)
	out, err := svc.ListAvailableChannelCatalog(context.Background(), []Group{group})
	require.NoError(t, err)
	require.Empty(t, out)
	sale, err := svc.ListSaleCatalog(context.Background(), []int64{1})
	require.NoError(t, err)
	require.Len(t, sale, 1)
	require.Len(t, sale[0].Models, 1)
	require.Equal(t, "claude-sale", sale[0].Models[0].Name)

	// A bound channel still requires its account configuration projection and
	// propagates errors instead of returning incomplete offers.
	svc = newPlazaServiceWithBilling([]Channel{plazaPricedChannel(1, "Bound", []int64{1}, PlatformAnthropic, "claude-sale")}, nil, nil, nil)
	svc.SetCommercialCatalogSources(nil, &commercialAccountsStub{err: errors.New("bound configuration unavailable")})
	out, err = svc.ListAvailableChannelCatalog(context.Background(), []Group{group})
	require.ErrorContains(t, err, "bound configuration unavailable")
	require.Nil(t, out)
}
