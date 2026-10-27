//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/LuckyKuang/sub2api-plus/internal/domain"
	"github.com/LuckyKuang/sub2api-plus/internal/payment"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCreateSubscriptionOrderBillingPolicyEvidenceAndStaleQuote(t *testing.T) {
	// A buyer confirms a particular selling-policy revision. A stale request
	// cannot create an order, allocate a payment or consume a quota. Accepted
	// orders preserve the displayed rule evidence, not a promise of term lock.
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().SetEmail("billing-quote@example.com").SetPasswordHash("fixture").Save(ctx)
	require.NoError(t, err)
	g, err := client.Group.Create().SetName("All-provider plan").SetPlatform(PlatformComposite).
		SetSubscriptionType(SubscriptionTypeSubscription).SetStatus(StatusActive).SetBillingPolicyVersion(7).
		SetBillingRatePolicy(domain.BillingRatePolicy{PlatformMultipliers: map[string]float64{PlatformOpenAI: 1.5, PlatformCline: 0}}).Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().SetGroupID(g.ID).SetName("Monthly").SetPrice(10).Save(ctx)
	require.NoError(t, err)
	personal := .5
	svc := &PaymentService{entClient: client, userGroupRateRepo: &userGroupRateResolverRepoStub{rate: &personal}}
	stale := int64(6)
	req := CreateOrderRequest{UserID: user.ID, PaymentType: payment.TypeAlipay, OrderType: payment.OrderTypeSubscription, PlanID: plan.ID, ExpectedBillingPolicyVersion: &stale}
	_, err = svc.createOrderInTx(ctx, req, &User{ID: user.ID, Email: user.Email}, plan, &PaymentConfig{}, 10, 10, 0, 10, 0, nil)
	require.Error(t, err)
	require.Equal(t, "BILLING_POLICY_CHANGED", infraerrors.FromError(err).Reason)
	require.Zero(t, client.PaymentOrder.Query().CountX(ctx))
	current := int64(7)
	req.ExpectedBillingPolicyVersion = &current
	order, err := svc.createOrderInTx(ctx, req, &User{ID: user.ID, Email: user.Email}, plan, &PaymentConfig{}, 10, 10, 0, 10, 0, nil)
	require.NoError(t, err)
	require.EqualValues(t, 7, order.PricingPolicyVersion)
	require.Equal(t, "current_request_prices", order.PricingSummary["consumption_policy"])
	require.Equal(t, "product_v2", order.PricingSummary["rate_calculation"])
	require.Equal(t, BillingPeakScopeAllMeters, order.PricingSummary["peak_scope"])
	require.Equal(t, "overridden", order.PricingSummary["personal_rate_status"])
	require.Equal(t, .5, order.PricingSummary["personal_rate_multiplier"])
	_, err = client.Group.UpdateOneID(g.ID).SetBillingPolicyVersion(8).Save(ctx)
	require.NoError(t, err)
	stored, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.EqualValues(t, 7, stored.PricingPolicyVersion)
	require.Equal(t, "product_v2", stored.PricingSummary["rate_calculation"], "later policy edits cannot reinterpret purchase audit evidence")
	require.Equal(t, BillingPeakScopeAllMeters, stored.PricingSummary["peak_scope"])
	require.Equal(t, .5, stored.PricingSummary["personal_rate_multiplier"])
	require.NotContains(t, stored.PricingSummary, "credentials")
	require.NotContains(t, stored.PricingSummary, "account_id")

	// The plan may change after input validation but before the transaction.
	// Its old selling price must not be paired with new entitlement evidence.
	_, err = client.SubscriptionPlan.UpdateOneID(plan.ID).SetPrice(20).Save(ctx)
	require.NoError(t, err)
	req.ExpectedBillingPolicyVersion = nil
	_, err = svc.createOrderInTx(ctx, req, &User{ID: user.ID, Email: user.Email}, plan, &PaymentConfig{}, 10, 10, 0, 10, 0, nil)
	require.Error(t, err)
	require.Equal(t, "SUBSCRIPTION_PLAN_CHANGED", infraerrors.FromError(err).Reason)
	require.Equal(t, 1, client.PaymentOrder.Query().CountX(ctx))
}
