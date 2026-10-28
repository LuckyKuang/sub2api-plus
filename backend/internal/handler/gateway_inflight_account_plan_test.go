//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/repository"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type accountPlanQuote struct {
	amount float64
	priced bool
}

type accountPlanObservation struct {
	snapshot *service.BillingPricingSnapshot
	public   string
	request  service.InflightEstimateRequest
	key      *service.APIKey
	owner    int64
}

type accountPlanEstimator struct {
	initial      accountPlanQuote
	selected     map[int64]accountPlanQuote
	observations []accountPlanObservation
}

func (e *accountPlanEstimator) observe(ctx context.Context, key *service.APIKey, req service.InflightEstimateRequest, owner int64) {
	public, _ := service.RequestedPublicModelFromContext(ctx)
	e.observations = append(e.observations, accountPlanObservation{
		snapshot: service.BillingPricingSnapshotFromContext(ctx), public: public,
		request: req, key: key, owner: owner,
	})
}

func (e *accountPlanEstimator) EstimateInflightReservation(ctx context.Context, key *service.APIKey, req service.InflightEstimateRequest) (float64, bool) {
	e.observe(ctx, key, req, 0)
	return e.initial.amount, e.initial.priced
}

func (e *accountPlanEstimator) EstimateInflightReservationForAccount(ctx context.Context, key *service.APIKey, req service.InflightEstimateRequest, account *service.Account) (float64, bool) {
	e.observe(ctx, key, req, account.ID)
	quote := e.selected[account.ID]
	return quote.amount, quote.priced
}

// Only the first reserve operation fails; the selected-account reconciliation
// and its eventual release still use the real production Lua scripts.
type accountPlanFirstReserveFailureCache struct {
	service.BillingCache
	service.InflightBalanceReservationCache
	service.InflightBalanceReservationUpdater
	service.InflightBalanceReservationRenewer
	failed atomic.Bool
}

func (c *accountPlanFirstReserveFailureCache) ReserveInflightBalance(ctx context.Context, userID int64, owner string, amount, balance float64, ttl time.Duration) (bool, float64, error) {
	if !c.failed.Swap(true) {
		return false, 0, errors.New("preliminary Redis reserve unavailable")
	}
	return c.InflightBalanceReservationCache.ReserveInflightBalance(ctx, userID, owner, amount, balance, ttl)
}

func newAccountPlanTestEnv(t *testing.T, balance float64, failFirstReserve bool) (*redis.Client, service.BillingCache, *service.BillingCacheService, *service.APIKey, context.Context) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	raw := repository.NewBillingCache(client)
	var cache service.BillingCache = raw
	if failFirstReserve {
		cache = &accountPlanFirstReserveFailureCache{
			BillingCache:                      raw,
			InflightBalanceReservationCache:   raw.(service.InflightBalanceReservationCache),
			InflightBalanceReservationUpdater: raw.(service.InflightBalanceReservationUpdater),
			InflightBalanceReservationRenewer: raw.(service.InflightBalanceReservationRenewer),
		}
	}
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60}
	billing := service.NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	groupID := int64(622)
	key := &service.APIKey{ID: 623, UserID: 621, GroupID: &groupID,
		User:  &service.User{ID: 621, Balance: balance},
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite, RateMultiplier: 2},
	}
	require.NoError(t, raw.SetUserBalance(context.Background(), key.User.ID, balance))
	ctx := service.WithCompositeRouteDecision(context.Background(), service.CompositeRouteDecision{
		Matched: true, PublicModel: "public-alias", TargetPlatform: service.PlatformOpenAI, UpstreamModel: "routed-model",
	})
	ctx = (&service.OpenAIGatewayService{}).WithBillingPricingSnapshot(ctx, key, time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC), "public-alias")
	require.NotNil(t, service.BillingPricingSnapshotFromContext(ctx))
	return client, raw, billing, key, ctx
}

func accountPlanAmounts(t *testing.T, client *redis.Client, userID int64) map[string]string {
	t.Helper()
	amounts, err := client.HGetAll(context.Background(), fmt.Sprintf("billing:inflight_amt:{%d}", userID)).Result()
	require.NoError(t, err)
	return amounts
}

func TestAccountInflightPlanCreatesSelectedPaidHoldAfterInitialNoHold(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		initial   accountPlanQuote
		failFirst bool
	}{
		{name: "initial_free", initial: accountPlanQuote{amount: 0, priced: true}},
		{name: "initial_unpriced", initial: accountPlanQuote{priced: false}},
		{name: "initial_reserve_failed_open", initial: accountPlanQuote{amount: 1, priced: true}, failFirst: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client, _, billing, key, original := newAccountPlanTestEnv(t, 10, scenario.failFirst)
			estimator := &accountPlanEstimator{initial: scenario.initial, selected: map[int64]accountPlanQuote{701: {amount: 4, priced: true}}}
			req := tokenInflightEstimate(service.ContentModerationProtocolOpenAIChat, "public-alias", []byte(`{"model":"public-alias","max_completion_tokens":32,"service_tier":"priority","reasoning_effort":"high"}`))
			ctx, done, err := reserveInflightBalanceCtx(original, billing, estimator, key, nil, req)
			require.NoError(t, err)
			defer done()
			require.Nil(t, service.InflightReservationFromContext(ctx))
			require.Empty(t, accountPlanAmounts(t, client, key.User.ID))
			selected := &service.Account{ID: 701, Platform: service.PlatformOpenAI}
			accepted, err := ensureAccountInflightReservationCtx(ctx, selected)
			require.NoError(t, err)
			reservation := service.InflightReservationFromContext(accepted)
			require.NotNil(t, reservation)
			require.Equal(t, float64(4), reservation.Amount())
			amounts := accountPlanAmounts(t, client, key.User.ID)
			require.Len(t, amounts, 1)
			for _, amount := range amounts {
				require.Equal(t, "4", amount)
			}

			// Reconciliation must carry the ingress snapshot, public identity and
			// explicit pricing dimensions into the selected-account estimate.
			require.Len(t, estimator.observations, 2)
			for _, observation := range estimator.observations {
				require.Same(t, service.BillingPricingSnapshotFromContext(original), observation.snapshot)
				require.Equal(t, "public-alias", observation.public)
				require.Equal(t, req, observation.request)
				require.Same(t, key, observation.key)
			}
			require.EqualValues(t, 701, estimator.observations[1].owner)
			require.Same(t, service.BillingPricingSnapshotFromContext(original), service.BillingPricingSnapshotFromContext(accepted))
			done()
			done()
			require.Empty(t, accountPlanAmounts(t, client, key.User.ID), "the original cleanup closure must release a hold created after selection")
			_, err = ensureAccountInflightReservationCtx(accepted, selected)
			require.ErrorIs(t, err, service.ErrInsufficientBalance)
			require.Empty(t, accountPlanAmounts(t, client, key.User.ID), "a closed plan cannot recreate a hold")
		})
	}
}

func TestAccountInflightPlanRejectedIncreasePreservesEveryExistingOwner(t *testing.T) {
	client, raw, billing, key, original := newAccountPlanTestEnv(t, 10, false)
	cache := raw.(service.InflightBalanceReservationCache)
	allowed, _, err := cache.ReserveInflightBalance(original, key.User.ID, "independent-request", 4, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	defer func() { _ = cache.ReleaseInflightBalance(context.Background(), key.User.ID, "independent-request") }()
	estimator := &accountPlanEstimator{
		initial:  accountPlanQuote{amount: 3, priced: true},
		selected: map[int64]accountPlanQuote{701: {amount: 7, priced: true}, 702: {amount: 6, priced: true}, 703: {amount: 5, priced: true}},
	}
	ctx, done, err := reserveInflightBalanceCtx(original, billing, estimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
	require.NoError(t, err)
	defer done()
	reservation := service.InflightReservationFromContext(ctx)
	require.NotNil(t, reservation)
	before := accountPlanAmounts(t, client, key.User.ID)
	require.Len(t, before, 2)
	ownersKey := fmt.Sprintf("billing:inflight:{%d}", key.User.ID)
	ownersBefore, err := client.ZRange(context.Background(), ownersKey, 0, -1).Result()
	require.NoError(t, err)
	_, err = ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 701, Platform: service.PlatformOpenAI})
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.Equal(t, float64(3), reservation.Amount())
	require.Equal(t, before, accountPlanAmounts(t, client, key.User.ID), "rejection must not create a release/reserve gap or alter any owner's amount")
	ownersAfter, err := client.ZRange(context.Background(), ownersKey, 0, -1).Result()
	require.NoError(t, err)
	require.Equal(t, ownersBefore, ownersAfter)

	accepted, err := ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 702, Platform: service.PlatformOpenAI})
	require.NoError(t, err, "ten dollars covers the independent four and this request's final six")
	require.Same(t, reservation, service.InflightReservationFromContext(accepted))
	require.Equal(t, float64(6), reservation.Amount())
	accepted, err = ensureAccountInflightReservationCtx(accepted, &service.Account{ID: 703, Platform: service.PlatformOpenAI})
	require.NoError(t, err)
	require.Equal(t, float64(6), service.InflightReservationFromContext(accepted).Amount(), "a cheaper later account must not lower an already accepted hold")
	done()
	require.Equal(t, map[string]string{"independent-request": "4"}, accountPlanAmounts(t, client, key.User.ID))
}

func TestAccountInflightPlanDoneBeforeSelectionCannotCreateHold(t *testing.T) {
	client, _, billing, key, original := newAccountPlanTestEnv(t, 10, false)
	estimator := &accountPlanEstimator{initial: accountPlanQuote{priced: true}, selected: map[int64]accountPlanQuote{701: {amount: 4, priced: true}}}
	ctx, done, err := reserveInflightBalanceCtx(original, billing, estimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
	require.NoError(t, err)
	done()
	done()
	_, err = ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 701, Platform: service.PlatformOpenAI})
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.Empty(t, accountPlanAmounts(t, client, key.User.ID))
	require.Len(t, estimator.observations, 1, "a completed handler cannot start another selected-account estimation")
}

func TestAccountInflightPlanAsyncBillingRetainsLateCreatedHold(t *testing.T) {
	client, _, billing, key, original := newAccountPlanTestEnv(t, 10, false)
	estimator := &accountPlanEstimator{initial: accountPlanQuote{priced: false}, selected: map[int64]accountPlanQuote{701: {amount: 4, priced: true}}}
	ctx, done, err := reserveInflightBalanceCtx(original, billing, estimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
	require.NoError(t, err)
	defer done()
	accepted, err := ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 701, Platform: service.PlatformOpenAI})
	require.NoError(t, err)
	reservation := service.InflightReservationFromContext(accepted)
	require.NotNil(t, reservation)
	ran := false
	task, abandon := wrapUsageRecordTaskContext(accepted, func(taskCtx context.Context) {
		ran = true
		require.Same(t, service.BillingPricingSnapshotFromContext(original), service.BillingPricingSnapshotFromContext(taskCtx))
		require.Len(t, accountPlanAmounts(t, client, key.User.ID), 1, "the hold must still exist while the billing task runs")
	})
	defer abandon()
	done()
	done()
	require.Len(t, accountPlanAmounts(t, client, key.User.ID), 1, "handler completion cannot release the billing task's acquired reference")
	require.Equal(t, float64(4), reservation.Amount())
	task(context.Background())
	require.True(t, ran)
	abandon()
	require.Empty(t, accountPlanAmounts(t, client, key.User.ID))
}

func TestAccountInflightPlanMissingOwnerRejectsEvenWithoutHigherEstimate(t *testing.T) {
	client, raw, billing, key, original := newAccountPlanTestEnv(t, 10, false)
	estimator := &accountPlanEstimator{initial: accountPlanQuote{amount: 3, priced: true}, selected: map[int64]accountPlanQuote{701: {amount: 3, priced: true}}}
	ctx, done, err := reserveInflightBalanceCtx(original, billing, estimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
	require.NoError(t, err)
	defer done()
	for owner := range accountPlanAmounts(t, client, key.User.ID) {
		require.NoError(t, raw.(service.InflightBalanceReservationCache).ReleaseInflightBalance(context.Background(), key.User.ID, owner))
	}
	_, err = ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 701, Platform: service.PlatformOpenAI})
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.Empty(t, accountPlanAmounts(t, client, key.User.ID))
}

func TestAccountInflightPlanUnpricedSelectionStillRequiresItsExistingLiveHold(t *testing.T) {
	for _, alive := range []bool{true, false} {
		t.Run(fmt.Sprintf("owner_alive_%t", alive), func(t *testing.T) {
			client, raw, billing, key, original := newAccountPlanTestEnv(t, 10, false)
			estimator := &accountPlanEstimator{
				initial:  accountPlanQuote{priced: false},
				selected: map[int64]accountPlanQuote{701: {amount: 3, priced: true}, 702: {priced: false}},
			}
			ctx, done, err := reserveInflightBalanceCtx(original, billing, estimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
			require.NoError(t, err)
			defer done()
			accepted, err := ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 701, Platform: service.PlatformOpenAI})
			require.NoError(t, err)
			reservation := service.InflightReservationFromContext(accepted)
			require.NotNil(t, reservation)
			if !alive {
				for owner := range accountPlanAmounts(t, client, key.User.ID) {
					require.NoError(t, raw.(service.InflightBalanceReservationCache).ReleaseInflightBalance(context.Background(), key.User.ID, owner))
				}
			}
			// Retry contexts can retain the plan from before the paid account
			// created its hold. The plan must still enforce and attach that hold.
			retried, err := ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 702, Platform: service.PlatformOpenAI})
			if alive {
				require.NoError(t, err)
				require.Same(t, reservation, service.InflightReservationFromContext(retried))
				require.Equal(t, float64(3), reservation.Amount())
				require.Len(t, accountPlanAmounts(t, client, key.User.ID), 1)
			} else {
				require.ErrorIs(t, err, service.ErrInsufficientBalance, "unpriced fail-open cannot bypass an explicitly lost existing owner")
				require.Empty(t, accountPlanAmounts(t, client, key.User.ID))
			}
		})
	}
}

func TestAccountInflightPlanFailClosedSelectedUnpricedPreservesOtherOwners(t *testing.T) {
	client, raw, billing, key, original := newAccountPlanTestEnv(t, 10, false)
	cache := raw.(service.InflightBalanceReservationCache)
	allowed, _, err := cache.ReserveInflightBalance(original, key.User.ID, "independent-request", 2, 10, time.Minute)
	require.NoError(t, err)
	require.True(t, allowed)
	defer func() { _ = cache.ReleaseInflightBalance(context.Background(), key.User.ID, "independent-request") }()
	estimator := &accountPlanEstimator{
		initial:  accountPlanQuote{amount: 3, priced: true},
		selected: map[int64]accountPlanQuote{701: {priced: false}},
	}
	// Configure fail-closed independently, before the request begins. The
	// initial estimate is known, while this selected account is unpriceable.
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, TTLSeconds: 60, FailClosedOnUnpriced: true}
	strictBilling := service.NewBillingCacheService(raw, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(strictBilling.Stop)
	ctx, done, err := reserveInflightBalanceCtx(original, strictBilling, estimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
	require.NoError(t, err)
	defer done()
	reservation := service.InflightReservationFromContext(ctx)
	require.NotNil(t, reservation)
	before := accountPlanAmounts(t, client, key.User.ID)
	require.Len(t, before, 2)
	_, err = ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 701, Platform: service.PlatformOpenAI})
	require.ErrorIs(t, err, service.ErrInsufficientBalance)
	require.Equal(t, before, accountPlanAmounts(t, client, key.User.ID), "unpriced rejection cannot release either this request's old hold or another request's hold")
	require.Equal(t, float64(3), reservation.Amount())
	done()
	require.Equal(t, map[string]string{"independent-request": "2"}, accountPlanAmounts(t, client, key.User.ID))
	// The permissive service also remains independently usable; the stricter
	// request must not mutate shared cache configuration or another service.
	require.False(t, billing.InflightReservationFailClosedOnUnpriced())
}

func TestAccountInflightPlanNewAdmissionDoesNotInheritParentHoldOrClosedPlan(t *testing.T) {
	for _, parentState := range []string{"completed", "billing_pending"} {
		for _, admission := range []string{"free_quote", "unpriced_quote", "subscription", "disabled"} {
			t.Run(parentState+"/"+admission, func(t *testing.T) {
				client, raw, billing, key, original := newAccountPlanTestEnv(t, 10, false)
				parentEstimator := &accountPlanEstimator{initial: accountPlanQuote{amount: 3, priced: true}}
				parent, parentDone, err := reserveInflightBalanceCtx(original, billing, parentEstimator, key, nil, service.InflightEstimateRequest{Model: "public-alias"})
				require.NoError(t, err)
				defer parentDone()
				parentHold := service.InflightReservationFromContext(parent)
				require.NotNil(t, parentHold)
				billingDone := func() {}
				if parentState == "billing_pending" {
					billingDone = parentHold.Acquire()
				}
				defer billingDone()
				parentDone()
				parentAmounts := accountPlanAmounts(t, client, key.User.ID)
				if parentState == "completed" {
					require.Empty(t, parentAmounts)
				} else {
					require.Len(t, parentAmounts, 1)
				}

				// A fallback group captures its own price snapshot while deriving
				// tracing metadata from the completed request's context. Its
				// admission must not inherit that request's hold or closed plan.
				fallbackGroupID := int64(624)
				fallbackKey := *key
				fallbackKey.GroupID = &fallbackGroupID
				fallbackKey.Group = &service.Group{ID: fallbackGroupID, Platform: service.PlatformComposite, RateMultiplier: 1}
				var subscription *service.UserSubscription
				if admission == "subscription" {
					fallbackKey.Group.SubscriptionType = service.SubscriptionTypeSubscription
					subscription = &service.UserSubscription{ID: 625}
				}
				fallbackBilling := billing
				if admission == "disabled" {
					cfg := &config.Config{}
					fallbackBilling = service.NewBillingCacheService(raw, nil, nil, nil, nil, nil, cfg, nil)
					t.Cleanup(fallbackBilling.Stop)
				}
				fallback := service.WithCompositeRouteDecision(service.WithoutBillingPricingSnapshot(parent), service.CompositeRouteDecision{
					Matched: true, PublicModel: "fallback-public", TargetPlatform: service.PlatformOpenAI, UpstreamModel: "fallback-upstream",
				})
				fallback = (&service.OpenAIGatewayService{}).WithBillingPricingSnapshot(fallback, &fallbackKey, time.Date(2026, 10, 27, 12, 1, 0, 0, time.UTC), "fallback-public")
				fallbackSnapshot := service.BillingPricingSnapshotFromContext(fallback)
				require.NotSame(t, service.BillingPricingSnapshotFromContext(parent), fallbackSnapshot)
				estimator := &accountPlanEstimator{
					initial:  accountPlanQuote{priced: admission != "unpriced_quote"},
					selected: map[int64]accountPlanQuote{702: {amount: 2, priced: true}},
				}
				ctx, done, err := reserveInflightBalanceCtx(fallback, fallbackBilling, estimator, &fallbackKey, subscription, service.InflightEstimateRequest{Model: "fallback-public"})
				require.NoError(t, err)
				defer done()
				require.Nil(t, service.InflightReservationFromContext(ctx), "a new admission without an initial hold must mask its parent's handle")
				accepted, err := ensureAccountInflightReservationCtx(ctx, &service.Account{ID: 702, Platform: service.PlatformOpenAI})
				require.NoError(t, err, "a completed parent plan cannot reject the new request's account selection")
				require.Same(t, fallbackSnapshot, service.BillingPricingSnapshotFromContext(accepted))
				if admission == "subscription" || admission == "disabled" {
					require.Nil(t, service.InflightReservationFromContext(accepted))
					require.Empty(t, estimator.observations, "inactive balance admission must not estimate or reuse the parent's estimator")
					require.Equal(t, parentAmounts, accountPlanAmounts(t, client, key.User.ID))
				} else {
					childHold := service.InflightReservationFromContext(accepted)
					require.NotNil(t, childHold)
					require.NotSame(t, parentHold, childHold)
					require.Equal(t, float64(2), childHold.Amount())
					for _, observation := range estimator.observations {
						require.Same(t, fallbackSnapshot, observation.snapshot)
						require.Equal(t, "fallback-public", observation.public)
					}
					require.Len(t, accountPlanAmounts(t, client, key.User.ID), len(parentAmounts)+1, "the child must acquire its own independent owner")
				}
				done()
				done()
				require.Equal(t, parentAmounts, accountPlanAmounts(t, client, key.User.ID), "child cleanup cannot release the parent's pending billing hold")
				billingDone()
				require.Empty(t, accountPlanAmounts(t, client, key.User.ID))
			})
		}
	}
}
