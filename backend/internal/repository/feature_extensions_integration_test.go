//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	"github.com/LuckyKuang/sub2api-plus/ent/paymentorder"
	"github.com/LuckyKuang/sub2api-plus/ent/redeemcode"
	"github.com/LuckyKuang/sub2api-plus/internal/config"
	"github.com/LuckyKuang/sub2api-plus/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func featureAdminService(client *dbent.Client, codes service.RedeemCodeRepository) service.AdminService {
	groups := NewAdminGroupRepository(client, integrationDB)
	return service.NewAdminService(nil, NewUserRepository(client, integrationDB), groups, nil, nil, nil, codes, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

type featureObservedSubscriptionRepo struct {
	service.UserSubscriptionRepository
	activeReads atomic.Int64
}

func (r *featureObservedSubscriptionRepo) GetActiveByUserIDAndGroupID(ctx context.Context, uid, gid int64) (*service.UserSubscription, error) {
	r.activeReads.Add(1)
	return r.UserSubscriptionRepository.GetActiveByUserIDAndGroupID(ctx, uid, gid)
}

// Approved redemption contract: failed order writes cannot publish success
// side effects. After a committed 30-day renewal, the next cached eligibility
// lookup must see the renewed entitlement, even if another request reads during
// the transaction. Real repositories and the real L1 cache participate here.
func TestRedeemOrdersSubscriptionCacheFollowsCommittedEntitlement(t *testing.T) {
	ctx := context.Background()
	// Failure-injection hooks belong to this test's client, not the suite's
	// shared client; another customer's redemption must remain unaffected.
	db, err := sql.Open("postgres", integrationPostgresDSN)
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	prefix := "rc-" + uuid.NewString()[:20]
	user, err := client.User.Create().SetEmail(prefix + "@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName(prefix).SetSubscriptionType("subscription").Save(ctx)
	require.NoError(t, err)
	start := time.Now().UTC().Truncate(time.Second)
	expiry := start.AddDate(0, 0, 5)
	_, err = client.UserSubscription.Create().SetUserID(user.ID).SetGroupID(group.ID).SetStartsAt(start).SetExpiresAt(expiry).SetStatus("active").Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM payment_audit_logs WHERE order_id IN (SELECT id::text FROM payment_orders WHERE user_id=$1)`, user.ID)
		_, _ = integrationDB.Exec(`DELETE FROM payment_orders WHERE user_id=$1`, user.ID)
		_, _ = integrationDB.Exec(`DELETE FROM redeem_codes WHERE code LIKE $1`, prefix+"%")
		_, _ = integrationDB.Exec(`DELETE FROM user_subscriptions WHERE user_id=$1`, user.ID)
		_, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=$1`, group.ID)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id=$1`, user.ID)
	})
	subRepo := &featureObservedSubscriptionRepo{UserSubscriptionRepository: NewUserSubscriptionRepository(client)}
	subs := service.NewSubscriptionService(NewGroupRepository(client, integrationDB), subRepo, nil, client, &config.Config{
		SubscriptionCache: config.SubscriptionCacheConfig{L1Size: 1024, L1TTLSeconds: 300},
	})
	t.Cleanup(subs.Stop)
	// Wait for cache admission using the public eligibility lookup; no test
	// reaches into the cache or assigns a result that the service should produce.
	require.Eventually(t, func() bool {
		before := subRepo.activeReads.Load()
		sub, readErr := subs.GetActiveSubscription(ctx, user.ID, group.ID)
		return readErr == nil && sub.ExpiresAt.Equal(expiry) && subRepo.activeReads.Load() == before
	}, time.Second, 10*time.Millisecond)
	codes := NewRedeemCodeRepository(client)
	code := &service.RedeemCode{Code: prefix, Type: "subscription", Status: "unused", GroupID: &group.ID, ValidityDays: 30, GenerateOrder: true, OrderAmount: 25, OrderCurrency: "CNY", OrderPlanName: "monthly"}
	require.NoError(t, codes.Create(ctx, code))
	redeem := service.NewRedeemService(codes, NewUserRepository(client, integrationDB), subs, nil, nil, client, nil, nil)
	failOrder := true
	readsBeforeOrder := subRepo.activeReads.Load()
	client.PaymentOrder.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(mctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			// A separate eligibility request can only see committed state.
			sub, readErr := subs.GetActiveSubscription(ctx, user.ID, group.ID)
			if readErr != nil || !sub.ExpiresAt.Equal(expiry) {
				return nil, fmt.Errorf("uncommitted entitlement became visible")
			}
			if failOrder {
				return nil, fmt.Errorf("injected order write failure")
			}
			return next.Mutate(mctx, mutation)
		})
	})
	_, err = redeem.Redeem(ctx, user.ID, code.Code)
	require.ErrorContains(t, err, "injected order write failure")
	require.Equal(t, readsBeforeOrder, subRepo.activeReads.Load(), "rollback must not invalidate a valid cached entitlement")
	stored, err := codes.GetByID(ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, "unused", stored.Status)
	dbSub, err := subRepo.GetByUserIDAndGroupID(ctx, user.ID, group.ID)
	require.NoError(t, err)
	require.True(t, expiry.Equal(dbSub.ExpiresAt), "a rejected order leaves the committed expiration unchanged")
	failOrder = false
	_, err = redeem.Redeem(ctx, user.ID, code.Code)
	require.NoError(t, err)
	sub, err := subs.GetActiveSubscription(ctx, user.ID, group.ID)
	require.NoError(t, err)
	require.True(t, expiry.AddDate(0, 0, 30).Equal(sub.ExpiresAt), "the next eligibility lookup must see the committed renewal")
	orders, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(user.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.Equal(t, 25.0, orders[0].PayAmount)
	require.Equal(t, "CNY", orders[0].Currency)
}

func TestRedeemOrdersAtomicRedemptionAndSnapshot(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "ro-" + uuid.NewString()[:20]
	user, err := client.User.Create().SetEmail(prefix + "@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	otherUser, err := client.User.Create().SetEmail(prefix + "-other@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName(prefix).SetSubscriptionType("subscription").Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM payment_audit_logs WHERE order_id IN (SELECT id::text FROM payment_orders WHERE user_id IN ($1,$2))`, user.ID, otherUser.ID)
		_, _ = integrationDB.Exec(`DELETE FROM payment_orders WHERE user_id IN ($1,$2)`, user.ID, otherUser.ID)
		_, _ = integrationDB.Exec(`DELETE FROM redeem_codes WHERE code LIKE $1 OR group_id=$2`, prefix+"%", group.ID)
		_, _ = integrationDB.Exec(`DELETE FROM user_subscriptions WHERE user_id=$1`, user.ID)
		_, _ = integrationDB.Exec(`DELETE FROM subscription_plans WHERE group_id=$1`, group.ID)
		_, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=$1`, group.ID)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id IN ($1,$2)`, user.ID, otherUser.ID)
	})
	codes := NewRedeemCodeRepository(client)
	users := NewUserRepository(client, integrationDB)
	subs := service.NewSubscriptionService(NewGroupRepository(client, integrationDB), NewUserSubscriptionRepository(client), nil, client, nil)
	redeem := service.NewRedeemService(codes, users, subs, nil, nil, client, nil, nil)
	code := &service.RedeemCode{Code: prefix + "-balance", Type: "balance", Value: 100, GenerateOrder: true, OrderCurrency: "CNY"}
	require.NoError(t, redeem.CreateCode(ctx, code))
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		uid := user.ID
		if i%2 != 0 {
			uid = otherUser.ID
		}
		go func() {
			defer wg.Done()
			_, err := redeem.Redeem(ctx, uid, code.Code)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	updated, err := users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	otherUpdated, err := users.GetByID(ctx, otherUser.ID)
	require.NoError(t, err)
	require.Contains(t, []float64{0, 100}, updated.Balance)
	require.Contains(t, []float64{0, 100}, otherUpdated.Balance)
	require.Equal(t, 100.0, updated.Balance+otherUpdated.Balance, "only one customer's balance may receive the face value")
	order, err := client.PaymentOrder.Query().Where(paymentorder.RedeemCodeIDEQ(code.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "CNY", order.Currency)
	require.Equal(t, 100.0, order.PayAmount)
	require.Equal(t, 100.0, order.Amount)
	winner, err := users.GetByID(ctx, order.UserID)
	require.NoError(t, err)
	require.Equal(t, 100.0, winner.Balance, "the completed order belongs to the customer who received the benefit")
	require.Equal(t, "redeem", order.OrderSource)
	require.Equal(t, "COMPLETED", order.Status)
	require.Zero(t, order.FeeRate)
	require.Zero(t, order.BonusAmount)
	require.NoError(t, codes.Delete(ctx, code.ID))
	_, err = client.PaymentOrder.Query().Where(paymentorder.IDEQ(order.ID)).Only(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetGroupID(group.ID).SetName("Frozen").SetPrice(29).SetOriginalPrice(199).SetCurrency("USD").SetValidityDays(2).SetValidityUnit("week").SetForSale(false).Save(ctx)
	require.NoError(t, err)
	admin := featureAdminService(client, codes)
	batch, err := admin.GenerateRedeemCodes(ctx, &service.GenerateRedeemCodesInput{Count: 2, Type: "subscription", GenerateOrder: true, OrderPlanID: &plan.ID})
	require.NoError(t, err)
	require.Len(t, batch, 2)
	for _, item := range batch {
		require.Equal(t, 29.0, item.OrderAmount)
		require.Equal(t, 14, item.ValidityDays)
		require.Equal(t, "Frozen", item.OrderPlanName)
	}
	t.Cleanup(func() {
		for _, item := range batch {
			_, _ = integrationDB.Exec(`DELETE FROM redeem_codes WHERE id=$1`, item.ID)
		}
	})
	_, err = client.SubscriptionPlan.UpdateOneID(plan.ID).SetName("New price").SetPrice(999).SetCurrency("CNY").SetValidityDays(1).SetValidityUnit("day").Save(ctx)
	require.NoError(t, err)
	require.NoError(t, client.SubscriptionPlan.DeleteOneID(plan.ID).Exec(ctx))
	_, err = redeem.Redeem(ctx, user.ID, batch[0].Code)
	require.NoError(t, err)
	subOrder, err := client.PaymentOrder.Query().Where(paymentorder.RedeemCodeIDEQ(batch[0].ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, 29.0, subOrder.PayAmount)
	require.Equal(t, "USD", subOrder.Currency)
	require.Equal(t, "Frozen", subOrder.PlanName)
	require.Equal(t, 14, *subOrder.SubscriptionDays)
	sub, err := subs.GetActiveSubscription(ctx, user.ID, group.ID)
	require.NoError(t, err)
	require.True(t, sub.StartsAt.AddDate(0, 0, 14).Equal(sub.ExpiresAt), "the customer receives the original two-week entitlement after plan edits and deletion")
	_, err = codes.BatchUpdate(ctx, []int64{batch[1].ID}, service.RedeemCodeBatchUpdateFields{GroupID: service.NullableInt64Update{Set: true, Value: &group.ID}})
	require.ErrorIs(t, err, service.ErrRedeemOrderSnapshotImmutable)
	_, err = client.Group.UpdateOneID(group.ID).SetStatus("disabled").Save(ctx)
	require.NoError(t, err)
	_, err = redeem.Redeem(ctx, user.ID, batch[1].Code)
	require.Error(t, err)
	unused, err := codes.GetByID(ctx, batch[1].ID)
	require.NoError(t, err)
	require.Equal(t, "unused", unused.Status)
	orders, err := client.PaymentOrder.Query().Where(paymentorder.UserIDIn(user.ID, otherUser.ID)).All(ctx)
	require.NoError(t, err)
	require.Len(t, orders, 2)
}

func TestRedeemOrdersFailureRollsBackBenefitsAndConsumption(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	prefix := "rr-" + uuid.NewString()[:20]
	user, err := client.User.Create().SetEmail(prefix + "@example.com").SetPasswordHash("test").Save(ctx)
	require.NoError(t, err)
	codes := NewRedeemCodeRepository(client)
	users := NewUserRepository(client, integrationDB)
	redeem := service.NewRedeemService(codes, users, nil, nil, nil, client, nil, nil)
	code := &service.RedeemCode{Code: prefix, Type: "balance", Value: 100, GenerateOrder: true}
	require.NoError(t, redeem.CreateCode(ctx, code))
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DROP TRIGGER IF EXISTS feature_redeem_audit_failure ON payment_audit_logs`)
		_, _ = integrationDB.Exec(`DROP FUNCTION IF EXISTS feature_redeem_audit_failure()`)
		_, _ = integrationDB.Exec(`DELETE FROM redeem_codes WHERE id=$1`, code.ID)
		_, _ = integrationDB.Exec(`DELETE FROM users WHERE id=$1`, user.ID)
	})
	_, err = integrationDB.Exec(`CREATE FUNCTION feature_redeem_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='REDEEM_ORDER_COMPLETED' THEN RAISE EXCEPTION 'injected audit failure'; END IF; RETURN NEW; END $$`)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`CREATE TRIGGER feature_redeem_audit_failure BEFORE INSERT ON payment_audit_logs FOR EACH ROW EXECUTE FUNCTION feature_redeem_audit_failure()`)
	require.NoError(t, err)
	_, err = redeem.Redeem(ctx, user.ID, code.Code)
	require.ErrorContains(t, err, "injected audit failure")
	got, err := codes.GetByID(ctx, code.ID)
	require.NoError(t, err)
	require.Equal(t, "unused", got.Status)
	updated, err := users.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Zero(t, updated.Balance)
	count, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(user.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	var audits int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM payment_audit_logs WHERE action='REDEEM_ORDER_COMPLETED' AND detail::jsonb->>'redeem_code_id'=$1`, fmt.Sprint(code.ID)).Scan(&audits))
	require.Zero(t, audits)
}

// Approved examples: every face value below credits exactly 100 USD, with no
// FX, fee or bonus. Omitting the order switch creates no order; an enabled
// order uses CNY by default or records the explicitly selected CNY/USD.
func TestRedeemOrdersOptInCurrenciesAndNoPromotions(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	codes := NewRedeemCodeRepository(client)
	users := NewUserRepository(client, integrationDB)
	redeem := service.NewRedeemService(codes, users, nil, nil, nil, client, nil, nil)
	for _, tc := range []struct {
		name, currency, wantCurrency string
		generateOrder                bool
	}{
		{"default no order", "", "", false},
		{"default CNY order", "", "CNY", true},
		{"CNY order", "CNY", "CNY", true},
		{"USD order", "USD", "USD", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := "currency-" + uuid.NewString()
			user, err := client.User.Create().SetEmail(prefix + "@example.com").SetPasswordHash("test").Save(ctx)
			require.NoError(t, err)
			batch, err := featureAdminService(client, codes).GenerateRedeemCodes(ctx, &service.GenerateRedeemCodesInput{Count: 1, Type: "balance", Value: 100, GenerateOrder: tc.generateOrder, OrderCurrency: tc.currency})
			require.NoError(t, err)
			require.Len(t, batch, 1)
			t.Cleanup(func() {
				_, _ = integrationDB.Exec(`DELETE FROM payment_audit_logs WHERE order_id IN (SELECT id::text FROM payment_orders WHERE user_id=$1)`, user.ID)
				_, _ = integrationDB.Exec(`DELETE FROM payment_orders WHERE user_id=$1`, user.ID)
				_, _ = integrationDB.Exec(`DELETE FROM redeem_codes WHERE id=$1`, batch[0].ID)
				_, _ = integrationDB.Exec(`DELETE FROM users WHERE id=$1`, user.ID)
			})
			_, err = redeem.Redeem(ctx, user.ID, batch[0].Code)
			require.NoError(t, err)
			updated, err := users.GetByID(ctx, user.ID)
			require.NoError(t, err)
			require.Equal(t, 100.0, updated.Balance)
			stored, err := codes.GetByID(ctx, batch[0].ID)
			require.NoError(t, err)
			require.Equal(t, "used", stored.Status)
			require.Equal(t, user.ID, *stored.UsedBy)
			orders, err := client.PaymentOrder.Query().Where(paymentorder.UserIDEQ(user.ID)).All(ctx)
			require.NoError(t, err)
			if !tc.generateOrder {
				require.Empty(t, orders)
				return
			}
			require.Len(t, orders, 1)
			require.Equal(t, 100.0, orders[0].Amount)
			require.Equal(t, 100.0, orders[0].PayAmount)
			require.Equal(t, tc.wantCurrency, orders[0].Currency)
			require.Equal(t, "redeem", orders[0].OrderSource)
			require.Equal(t, "balance", orders[0].OrderType)
			require.Equal(t, "COMPLETED", orders[0].Status)
			require.Zero(t, orders[0].FeeRate)
			require.Zero(t, orders[0].BonusAmount)
		})
	}
}

type featureFailCreateRepo struct {
	service.RedeemCodeRepository
	calls int
}

func (r *featureFailCreateRepo) Create(ctx context.Context, code *service.RedeemCode) error {
	r.calls++
	if r.calls == 2 {
		return fmt.Errorf("injected batch failure")
	}
	return r.RedeemCodeRepository.Create(ctx, code)
}
func TestRedeemOrderBatchGenerationIsAtomic(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	before, err := client.RedeemCode.Query().Where(redeemcode.GenerateOrderEQ(true)).Count(ctx)
	require.NoError(t, err)
	repo := &featureFailCreateRepo{RedeemCodeRepository: NewRedeemCodeRepository(client)}
	_, err = featureAdminService(client, repo).GenerateRedeemCodes(ctx, &service.GenerateRedeemCodesInput{Count: 3, Type: "balance", Value: 100, GenerateOrder: true})
	require.Error(t, err)
	after, err := client.RedeemCode.Query().Where(redeemcode.GenerateOrderEQ(true)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
