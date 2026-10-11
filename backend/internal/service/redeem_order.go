package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	dbent "github.com/LuckyKuang/sub2api-plus/ent"
	infraerrors "github.com/LuckyKuang/sub2api-plus/internal/pkg/errors"
)

const OrderSourcePayment = "payment"
const OrderSourceRedeem = "redeem"

var ErrRedeemOrderRefundDisabled = infraerrors.Forbidden("REDEEM_ORDER_REFUND_DISABLED", "redeem orders cannot be refunded")
var ErrRedeemOrderSnapshotImmutable = infraerrors.BadRequest("REDEEM_ORDER_SNAPSHOT_IMMUTABLE", "order snapshot and subscription entitlement cannot be edited")

func validateOrderSource(source string) error {
	if source != "" && source != OrderSourcePayment && source != OrderSourceRedeem {
		return infraerrors.BadRequest("INVALID_ORDER_SOURCE", "order source must be payment or redeem")
	}
	return nil
}

// freezeRedeemOrder reads one authoritative plan snapshot for an entire batch.
// No exchange rate, fee, or recharge promotion participates in this operation.
func freezeRedeemOrder(ctx context.Context, client *dbent.Client, code *RedeemCode) error {
	if !code.GenerateOrder {
		return nil
	}
	invalid := func(message string) error { return infraerrors.BadRequest("REDEEM_ORDER_INVALID", message) }
	switch code.Type {
	case RedeemTypeBalance:
		code.OrderCurrency = strings.ToUpper(strings.TrimSpace(code.OrderCurrency))
		if code.OrderCurrency == "" {
			code.OrderCurrency = "CNY"
		}
		code.OrderAmount = code.Value
		code.OrderPlanID, code.OrderPlanName = nil, ""
	case RedeemTypeSubscription:
		if code.OrderPlanID == nil || client == nil {
			return invalid("subscription order requires a plan")
		}
		plan, err := client.SubscriptionPlan.Get(ctx, *code.OrderPlanID)
		if err != nil {
			return invalid("subscription plan not found")
		}
		group, err := client.Group.Get(ctx, plan.GroupID)
		if err != nil || group.SubscriptionType != "subscription" || group.Status != StatusActive || group.DeletedAt != nil {
			return invalid("subscription group is unavailable")
		}
		code.OrderAmount, code.OrderCurrency, code.OrderPlanName = plan.Price, strings.ToUpper(strings.TrimSpace(plan.Currency)), plan.Name
		if code.OrderCurrency == "" {
			code.OrderCurrency = "CNY"
		}
		days := psComputeValidityDays(plan.ValidityDays, plan.ValidityUnit)
		if days > MaxValidityDays {
			days = MaxValidityDays
		}
		if days <= 0 {
			return invalid("subscription duration must be positive")
		}
		if (code.GroupID != nil && *code.GroupID != plan.GroupID) || (code.ValidityDays != 0 && code.ValidityDays != days) {
			return invalid("subscription entitlement must match the selected plan")
		}
		code.GroupID, code.ValidityDays = &plan.GroupID, days
	default:
		return invalid("only balance and subscription codes support orders")
	}
	if code.OrderCurrency != "CNY" && code.OrderCurrency != "USD" {
		return invalid("order currency must be CNY or USD")
	}
	if math.IsNaN(code.OrderAmount) || math.IsInf(code.OrderAmount, 0) || code.OrderAmount <= 0 || math.Abs(math.Round(code.OrderAmount*100)-code.OrderAmount*100) > 1e-6 {
		return invalid("order amount must be positive with at most two decimals")
	}
	return nil
}

func createRedeemOrder(ctx context.Context, tx *dbent.Tx, code *RedeemCode, userID int64) error {
	if !code.GenerateOrder {
		return nil
	}
	if (code.Type != RedeemTypeBalance && code.Type != RedeemTypeSubscription) || code.OrderAmount <= 0 || (code.OrderCurrency != "CNY" && code.OrderCurrency != "USD") {
		return infraerrors.BadRequest("REDEEM_ORDER_INVALID", "invalid stored order snapshot")
	}
	user, err := tx.User.Get(ctx, userID)
	if err != nil {
		return err
	}
	number, err := allocateOutTradeNo(ctx, tx)
	if err != nil {
		return err
	}
	now := time.Now()
	b := tx.PaymentOrder.Create().SetUserID(userID).SetUserEmail(user.Email).SetUserName(user.Username).
		SetAmount(code.OrderAmount).SetPayAmount(code.OrderAmount).SetRechargeCode("").
		SetOutTradeNo(number).SetPaymentType("redeem").SetPaymentTradeNo("").
		SetOrderType(code.Type).SetOrderSource(OrderSourceRedeem).SetCurrency(code.OrderCurrency).
		SetPlanName(code.OrderPlanName).SetRedeemCodeID(code.ID).SetNillablePlanID(code.OrderPlanID).
		SetStatus(OrderStatusCompleted).SetExpiresAt(now).SetPaidAt(now).SetCompletedAt(now).SetCreatedAt(now).
		SetClientIP("").SetSrcHost("")
	if code.Type == RedeemTypeSubscription {
		b.SetNillableSubscriptionGroupID(code.GroupID).SetSubscriptionDays(code.ValidityDays)
	}
	order, err := b.Save(ctx)
	if err != nil {
		return fmt.Errorf("create redeem order: %w", err)
	}
	detail, err := json.Marshal(map[string]any{"redeem_code_id": code.ID, "order_source": OrderSourceRedeem, "amount": code.OrderAmount, "currency": code.OrderCurrency})
	if err != nil {
		return err
	}
	_, err = tx.PaymentAuditLog.Create().SetOrderID(strconv.FormatInt(order.ID, 10)).SetAction("REDEEM_ORDER_COMPLETED").SetDetail(string(detail)).SetOperator("system").Save(ctx)
	return err
}
