# Payment System Configuration Guide

Sub2API Plus has a built-in payment system that enables user self-service top-up without deploying a separate payment service.

Checkout and subscription-plan group names follow the
[group display translation contract](MODEL_PLAZA.md#group-display-translations).
Changing the interface language updates those names without changing selected
prices, currency conversion, payment methods, recovery state or unsaved input.
Payment expiry displays use elapsed wall-clock time so background-tab throttling
does not extend the order's remaining time.

---

## Table of Contents

- [Supported Payment Methods](#supported-payment-methods)
- [Quick Start](#quick-start)
- [System Settings](#system-settings)
- [Provider Configuration](#provider-configuration)
- [Provider Instance Management](#provider-instance-management)
- [Webhook Configuration](#webhook-configuration)
- [Payment Flow](#payment-flow)
- [Migrating from a Legacy External Payment Service](#migrating-from-a-legacy-external-payment-service)

---

## Supported Payment Methods

| Provider | Payment Methods | Description |
|----------|----------------|-------------|
| **EasyPay** | Alipay, WeChat Pay | Third-party aggregation via EasyPay protocol |
| **Alipay (Direct)** | Desktop QR code, mobile Alipay redirect | Direct integration with Alipay Open Platform, returning desktop QR codes and mobile WAP/app launch links |
| **WeChat Pay (Direct)** | Native QR, H5, MP/JSAPI Pay | Direct integration with WeChat Pay APIv3 with environment-aware routing |
| **Stripe** | Card, Alipay, WeChat Pay, Link, etc. | International payments, multi-currency support |

> Alipay/WeChat Pay direct and EasyPay can both exist as backend provider instances, but the frontend always exposes only two visible buttons: `Alipay` and `WeChat Pay`. Admins choose exactly one source for each visible method: direct or EasyPay. Direct channels connect to payment APIs directly with lower fees; EasyPay aggregates through third-party platforms with easier setup.

> **EasyPay providers**: Configure any provider that implements the EasyPay
> protocol and is suitable for your settlement region and currency. Evaluate the
> provider's security, reliability, fees, terms, and compliance independently.
> Sub2API Plus does not endorse or guarantee third-party payment providers.

---

## Quick Start

1. Go to Admin Dashboard → **Settings** → **Payment Settings** tab
2. Enable **Payment**
3. Configure basic parameters (amount range, timeout, etc.)
4. Add at least one provider instance in **Provider Management**
5. Users can now top up from the frontend

---

## System Settings

Configure the following in Admin Dashboard **Settings → Payment Settings**:

### Basic Settings

| Setting | Description | Default |
|---------|-------------|---------|
| **Enable Payment** | Enable or disable the payment system | Off |
| **Product Name Prefix** | Prefix shown on payment page | - |
| **Product Name Suffix** | Suffix (e.g., "Credits") | - |
| **Minimum Amount** | Minimum single top-up amount | 1 |
| **Maximum Amount** | Maximum single top-up amount (empty = unlimited) | - |
| **Daily Limit** | Per-user daily cumulative limit (empty = unlimited) | - |
| **Order Timeout** | Order timeout in minutes (minimum 1) | 30 |
| **Max Pending Orders** | Maximum concurrent pending orders per user | 3 |
| **Load Balance Strategy** | Strategy for selecting provider instances | Round Robin |
| **Recharge Bonus Tiers** | Recharge incentive tiers (`RECHARGE_BONUS_TIERS`, JSON) | *(empty = incentive disabled)* |
| **Recharge Bonus Mode** | Tier mode: `bonus` (extra credit) or `discount` (reduced payment) | `bonus` |
| **Promotion Notice** | Markdown notice shown above the amount cards (`RECHARGE_BONUS_NOTICE`) | - |

### Recharge Incentives (Bonus / Discount)

Recharge incentives live in the `RECHARGE_BONUS_TIERS`, `RECHARGE_BONUS_MODE` and
`RECHARGE_BONUS_NOTICE` settings. An empty tier list disables the incentive, which
is the default for existing installations. The amount the user enters is matched
against the largest tier whose `min_amount` does not exceed it. Subscription orders
never participate. Amounts and percentages reuse the existing decimal and currency
precision rules, and the promotion notice is sanitized with DOMPurify before it is
rendered on the payment page.

Each setting can also be provided as the same-named deployment environment variable
(see `deploy/.env.example`); unset or blank keeps the value saved in Admin Settings.

### Order Monetary Fields

| Field | Meaning |
|-------|---------|
| `amount` | Total USD credited to the balance, including the free part |
| `bonus_amount` | Free USD part included in `amount` (0 for historical and non-incentivized orders) |
| `pay_amount` | Amount actually collected by the payment channel, including fees, in the order's frozen payment currency |
| `fee_rate` | Fee percentage applied on top of the payment base |

The quote is frozen when the order is created: later changes to the incentive
settings or to the selected provider currency never rewrite an existing order.

### Commission, Refund and Limit Policy

- Affiliate commission accrues on the paid part (`amount - bonus_amount`). The free
  portion never earns commission, and duplicate callbacks or fulfillment retries are
  applied once under the existing idempotency controls.
- Refunds are proportional to actual paid versus credited:
  `gateway refund = pay_amount × credited clawback ÷ amount`, computed on cumulative
  totals with the order currency's precision, so cumulative cash repayment never
  exceeds the original `pay_amount`. Balance is clawed back in step, and a partial
  refund may be followed by the remaining refund; the order only reaches `REFUNDED`
  once the full credited amount has been clawed back.
- Historical orders migrate with `bonus_amount = 0` and keep the previous refund
  behavior; no promotion is applied retroactively.
- Daily limit basis: balance orders are compared using the channel-collected amount
  including fees (`pay_amount`) in the order's frozen payment currency. Credited USD
  and the free bonus never enlarge the payment limit. Subscription orders keep the
  prior plan-price basis. If previously paid balance orders use a different frozen
  currency and no reliable conversion exists, order creation fails with the stable
  `PAYMENT_DAILY_LIMIT_CURRENCY_MISMATCH` configuration error instead of summing
  incompatible units.

### Frontend Visible Method Routing

The current payment UX keeps the frontend method list unified and does not expose provider brands directly:

- **Alipay**: when enabled, this button must be routed to either `Alipay (Direct)` or `EasyPay Alipay`
- **WeChat Pay**: when enabled, this button must be routed to either `WeChat Pay (Direct)` or `EasyPay WeChat`
- Each visible method can route to only one source at a time
- If a visible method is enabled without a selected source, the frontend will not expose that method

### Load Balance Strategies

| Strategy | Description |
|----------|-------------|
| **Round Robin** | Distribute orders to instances in rotation |
| **Least Amount** | Prefer instances with the lowest daily cumulative amount |

### Cancel Rate Limiting

Prevents users from repeatedly creating and canceling orders:

| Setting | Description |
|---------|-------------|
| **Enable Limit** | Toggle |
| **Window Mode** | Sliding / Fixed window |
| **Time Window** | Window duration |
| **Window Unit** | Minutes / Hours |
| **Max Cancels** | Maximum cancellations allowed within the window |

### Help Information

| Setting | Description |
|---------|-------------|
| **Help Image** | Customer service QR code or help image (supports upload) |
| **Help Text** | Instructions displayed on the payment page |

---

## Provider Configuration

Each provider type requires different credentials. Select the type when adding a new provider instance in **Provider Management → Add Provider**.

> **Callback URLs are auto-generated**: When adding a provider, the Notify URL and Return URL are automatically constructed from your site domain. You only need to confirm the domain is correct.

### EasyPay

Compatible with any payment service that implements the EasyPay protocol.

Notifications accept only the standard EasyPay fields (`pid`, `trade_no`,
`out_trade_no`, `type`, `name`, `money`, `trade_status`, `param`, `sign`,
`sign_type`). Unknown fields, including empty ones and order-creation fields
such as `return_url` or `notify_url`, are rejected before fulfillment. A signed
checkout URL cannot be replayed as a payment-success notification. Providers
that add nonstandard fields must use standard notifications; the existing
authenticated upstream order-query reconciliation can recover genuine payments.

For every payment provider, client-supplied query parameters and fragments in
`return_url` are discarded. The canonical internal result-page path and host
validation remain enforced. The server then adds its own `order_id`,
`out_trade_no`, `status`, and signed `resume_token` when configured. Client query
parameters are no longer carried through payment redirects. Existing bonus,
commission, refund, and duplicate-fulfillment rules remain in force.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **Merchant ID (PID)** | EasyPay merchant ID | Yes |
| **Merchant Key (PKey)** | EasyPay merchant secret key | Yes |
| **API Base URL** | EasyPay API base address | Yes |
| **Alipay Channel ID** | Specify Alipay channel (optional) | No |
| **WeChat Channel ID** | Specify WeChat channel (optional) | No |

### Alipay (Direct)

Direct integration with Alipay Open Platform. Mobile flows return an Alipay WAP/app redirect URL. Desktop flows prefer Face-to-Face Precreate QR payloads; if the merchant has not enabled that product, the provider falls back to Computer Website Pay and also returns the cashier URL so the frontend can render a QR code or open the hosted checkout page directly.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **AppID** | Alipay application AppID | Yes |
| **Private Key** | RSA2 application private key | Yes |
| **Alipay Public Key** | Alipay public key | Yes |

### WeChat Pay (Direct)

Direct integration with WeChat Pay APIv3. Supports Native QR code payment, H5 payment, and MP/JSAPI payment inside the WeChat environment.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **AppID** | WeChat Pay AppID | Yes |
| **Merchant ID (MchID)** | WeChat Pay merchant ID | Yes |
| **Merchant API Private Key** | Merchant API private key (PEM format) | Yes |
| **APIv3 Key** | 32-byte APIv3 key | Yes |
| **WeChat Pay Public Key** | WeChat Pay public key (PEM format) | Yes |
| **WeChat Pay Public Key ID** | WeChat Pay public key ID | Yes |
| **Certificate Serial Number** | Merchant certificate serial number | Yes |

### Stripe

International payment platform supporting multiple payment methods and currencies.

| Parameter | Description | Required |
|-----------|-------------|----------|
| **Secret Key** | Stripe secret key (`sk_live_...` or `sk_test_...`) | Yes |
| **Publishable Key** | Stripe publishable key (`pk_live_...` or `pk_test_...`) | Yes |
| **Webhook Secret** | Stripe Webhook signing secret (`whsec_...`) | Yes |

---

## Provider Instance Management

You can create **multiple instances** of the same provider type for load balancing and risk control:

- **Multi-instance load balancing** — Distribute orders via round-robin or least-amount strategy
- **Independent limits** — Each instance can have its own min/max amount and daily limit
- **Independent toggle** — Enable/disable individual instances without affecting others
- **Refund control** — Enable or disable refunds per instance
- **Payment methods** — Each instance can support a subset of payment methods
- **Ordering** — Drag to reorder instances

### Instance Limit Configuration

Each instance supports these limits:

| Limit | Description |
|-------|-------------|
| **Minimum Amount** | Minimum order amount accepted by this instance |
| **Maximum Amount** | Maximum order amount accepted by this instance |
| **Daily Limit** | Daily cumulative transaction limit for this instance |

> During load balancing, instances that exceed their limits are automatically skipped.

---

## Webhook Configuration

Payment callbacks are essential for the payment system to work correctly.

### Callback URL Format

When adding a provider, the system auto-generates callback URLs from your site domain:

| Provider | Callback Path |
|----------|-------------|
| **EasyPay** | `https://your-domain.com/api/v1/payment/webhook/easypay` |
| **Alipay (Direct)** | `https://your-domain.com/api/v1/payment/webhook/alipay` |
| **WeChat Pay (Direct)** | `https://your-domain.com/api/v1/payment/webhook/wxpay` |
| **Stripe** | `https://your-domain.com/api/v1/payment/webhook/stripe` |

> Replace `your-domain.com` with your actual domain. For EasyPay / Alipay / WeChat Pay, the callback URL is auto-filled when adding the provider — no manual configuration needed.

### Stripe Webhook Setup

1. Log in to [Stripe Dashboard](https://dashboard.stripe.com/)
2. Go to **Developers → Webhooks**
3. Add an endpoint with the callback URL
4. Subscribe to events: `payment_intent.succeeded`, `payment_intent.payment_failed`
5. Copy the generated Webhook Secret (`whsec_...`) to your provider configuration

### Important Notes

- Callback URLs must use **HTTPS** (required by Stripe, strongly recommended for others)
- Ensure your firewall allows callback requests from payment platforms
- The system automatically verifies callback signatures to prevent forgery
- Balance top-up is processed automatically upon successful payment — no manual intervention needed

---

## Payment Flow

```
User selects amount and payment method
       │
       ▼
  Create Order (PENDING)
  ├─ Validate amount range, pending order count, daily limit
  ├─ Load balance to select provider instance
  └─ Call provider to get payment info
       │
       ▼
  User completes payment
  ├─ EasyPay     → QR code / H5 redirect
  ├─ Alipay      → Desktop QR payload (Face-to-Face preferred, Website Pay fallback) / mobile Alipay redirect
  ├─ WeChat Pay  → Desktop Native QR / non-WeChat H5 / in-WeChat JSAPI
  └─ Stripe      → Payment Element (card/Alipay/WeChat/etc.)
       │
       ▼
  Webhook callback verified → Order PAID
       │
       ▼
  Auto top-up to user balance → Order COMPLETED
```

### Order Status Reference

| Status | Description |
|--------|-------------|
| `PENDING` | Waiting for user to complete payment |
| `PAID` | Payment confirmed, awaiting balance credit |
| `COMPLETED` | Balance credited successfully |
| `EXPIRED` | Timed out without payment |
| `CANCELLED` | Cancelled by user |
| `FAILED` | Balance credit failed, admin can retry |
| `REFUND_REQUESTED` | Refund requested |
| `REFUNDING` | Refund in progress |
| `REFUNDED` | Refund completed |

### Timeout and Fallback

- Before marking an order as expired, the background job queries the upstream payment status first
- If the user has actually paid but the callback was delayed, the system will reconcile automatically
- The background job runs every 60 seconds to check for timed-out orders

---

## Migrating from a Legacy External Payment Service

If you previously used a legacy external payment service, you can migrate to the built-in payment system:

### Key Differences

| Aspect | Legacy external service | Built-in Payment |
|--------|-----------|-----------------|
| Deployment | Separate service (Next.js + PostgreSQL) | Built into Sub2API Plus, no extra deployment |
| Payment Methods | EasyPay, Alipay, WeChat, Stripe | Same |
| Configuration | Environment variables + separate admin UI | Unified in Sub2API Plus admin dashboard |
| Top-up Integration | Via Admin API callback | Internal processing, more reliable |
| Subscription Plans | Supported | Not yet (planned) |
| Order Management | Separate admin interface | Integrated in Sub2API Plus admin dashboard |

### Migration Steps

1. Enable payment in Sub2API Plus admin dashboard and configure providers (use the same payment credentials)
2. Update webhook callback URLs to Sub2API Plus's callback endpoints
3. Verify that new orders are processed correctly via built-in payment
4. Decommission the legacy external payment service

> **Note**: Historical order data from a legacy external payment service will not be automatically migrated. Keep that service available for a while to access historical records.

## External redeem purchase and redemption orders

DB settings: `payment_redeem_purchase_enabled=false`, `payment_redeem_purchase_url=''`, and `payment_redeem_purchase_mode=new_tab` (`iframe` also supported). The built-in `payment_enabled` switch continues to control recharge/subscription navigation and routes. Both selection views show the optional purchase card at the bottom, including when no payment methods are available; payment and subscription-confirmation phases hide it.

If payment configuration is unavailable, the combined admin settings response keeps the purchase entry disabled, its URL empty and its mode `new_tab`.

Both config APIs validate an absolute HTTP(S) URL without credentials or control characters, require a URL when enabled, and reject unknown modes before saving. New tabs use `noopener noreferrer`; protected `/purchase/redeem-shop` embeds the exact URL without attaching credentials or user identifiers and offers back, redemption and new-tab controls. CSP permits only the saved exact origin when payment, the entry and iframe mode are enabled; both save paths refresh the cache. The third-party page has no payment callback or balance/order side effects.

The iframe sandbox permits scripts, forms, its own origin and payment popups, including popups that escape the sandbox; it does not permit navigation of the enclosing page. Embedding remains subject to the third party's `X-Frame-Options` and `frame-ancestors` policies; use the new-tab control when embedding is unavailable.

Only balance and subscription codes can opt into orders (off by default). Balance currency is CNY/USD, default CNY: a face value of 100 with CNY order currency credits 100 USD and records 100 CNY, without FX, fees or recharge promotions. Subscription codes select an existing plan (including off-sale) and freeze its selling `price`, currency, ID/name, group and effective duration at generation (week × 7, month × 30, capped at 36500 days). Empty currency means CNY; other currencies cannot generate these redemption orders. Ordinary payment currency support is unchanged. Later plan edits/deletion do not change snapshots; unavailable entitlement groups reject redemption.

Consumption, benefits, a COMPLETED order and `REDEEM_ORDER_COMPLETED` audit commit atomically. A unique redeem-code reference prevents duplicates and has no cascading deletion. Orders use `order_source=redeem`, `payment_type=redeem` and retain `order_type=balance|subscription`; historical and ordinary payments use `order_source=payment`. Timestamps are redemption time. No provider, payment URL, fee or bonus is recorded, and ordinary user DTOs never expose raw redeem codes. Internal PAY codes remain opt-out and do not duplicate fulfillment or affiliate rebates. Subscription snapshots cannot be rewritten by ordinary batch editing. Conflicting group/duration inputs are rejected at generation; admin redeem-code list/detail DTOs and CSV exports retain the frozen order fields.

Admin/user order lists and the dashboard support `order_source` filters, defaulting to both sources; unknown sources return 400 (`INVALID_ORDER_SOURCE`). Currency totals remain separate: 100 CNY redemption + 50 CNY payment + 20 USD payment = 3 orders, CNY150, USD20. Redemption does not use actual-payment limits. Every refund boundary, including forced admin refunds, returns `REDEEM_ORDER_REFUND_DISABLED` before provider lookup, deductions or state changes; UI refund controls are hidden.

Subscription redemption invalidates the local entitlement cache and Redis/cross-instance caches only after the outer redemption transaction commits. A failed order or audit write leaves the prior entitlement and its valid cache intact. The next eligibility lookup after successful redemption reads the committed entitlement; cache notification failures are logged without reporting an already committed redemption as failed.
