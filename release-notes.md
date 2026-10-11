Sub2API Plus v0.2.15+custom.002

## Highlights

- Combine optional V1 active probes with V3 service status. Probe outcomes participate directly in V3 state and incident decisions while users see only the V3 monitor; the separate V1 user monitor remains available only in V1 mode. Probe requests follow the same credential-owner outbound identity contract as platform requests.
- Show deduplicated model status within each platform, without group or monitor names in the user-facing model and incident views. Healthy traffic cannot hide a failed probe.
- Improve the available-channel catalog with at most five cards per row, wider spacing and rounder corners. Service tiers such as Fast/priority, Flex and Ultrafast are collapsed by default and can be expanded independently.
- Add an optional external redeem-code purchase entry in payment settings. Recharge/subscription selection pages show its card at the bottom, with a right-aligned purchase action; operators choose an embedded page or a new tab. The built-in payment switch still controls recharge/subscription navigation.
- Let administrators opt balance and subscription redeem codes into order creation. Redemption atomically grants the benefit and creates a completed order, with separate payment/redemption source labels and filters and the existing balance/subscription order types.

## Changed

- Balance redemption-order currencies are CNY and USD, defaulting to CNY, with no exchange-rate conversion: a face value of 100 and CNY order currency credits 100 USD and records a 100 CNY order.
- Subscription redemption codes freeze the selected plan's price, currency, name, group and duration when generated. Later plan price changes or deletion do not rewrite those snapshots.
- Payment and redemption orders share order management and statistics; totals remain separate per currency. Redemption orders have no provider payment, recharge promotion, fee or refund, including forced administrator refunds.
- New-tab purchases use opener isolation. Embedded purchases use a protected, sandboxed page and the configured origin in CSP, without adding application credentials or user identifiers to the third-party URL.

## Fixed

- Align the purchase button beside the purchase-card title and description, and label it "Go to purchase" / "前往购买".
- Use the shared plan-duration formatter in subscription administration so translated day/week/month labels replace raw translation keys.
- Preserve the disabled purchase entry and default new-tab mode in admin settings when payment configuration is unavailable.
- Invalidate subscription entitlement caches only after the redemption transaction commits; failed order or audit writes leave the code and benefits unchanged.

## Compatibility and migration

Upgrade applies forward-only migrations `282_redeem_order_snapshots.sql` and `283_channel_monitor_v3_probe_sources.sql`. Existing redeem codes default to no order, existing payment orders retain their payment source and currency resolution, and existing V3 observations keep their history and incident IDs as request observations.

Extra V1 probes in V3 and the external purchase entry default to disabled. Existing monitoring mode and the built-in payment navigation gate are preserved. Only balance and subscription codes support optional order creation; internal payment-fulfillment codes remain opted out. Redemption orders are non-refundable and receive no recharge promotions.

## Known issues

Third-party sites can prevent iframe embedding through their own browser security policies; the embedded purchase page provides a new-tab option.

V3 treats a missing probe or one older than five minutes as unknown, so longer probe intervals can leave unknown gaps. A probe targeting this application's gateway can also produce a request observation; the two sources are combined for status, without claiming deduplicated business-request counts across sources.

## Upstream baseline

Official release: v0.2.15
Official commit: f2669c8cf62555cd92389b3f55920e9e6e7c6ff2
