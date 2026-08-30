Sub2API Plus v0.1.183+custom.004

## Highlights

- Added configurable OpenAI-compatible Content Moderation endpoint pools with priority failover, cooldown, manual pause, and health visibility.
- Improved Prompt Audit and Content Moderation observability and protocol coverage.
- Added Moderation platform attribution and status semantics to audit records.
- Improved asynchronous image task durability, requested/actual image observability, and PostgreSQL-backed ZIP download recovery.

## Changed

- Prompt Guard audits user input only.
- Added text Moderation API strategy guidance and clarified its interaction with global moderation modes.

## Fixed

- Fixed manual Moderation endpoint tests being overwritten by persisted pool configuration.
- Fixed multi-image async edit submission and durable task metadata.
- Fixed completed async image ZIP downloads after Redis task expiry.

## Compatibility and migration

Database migration 237 adds Moderation endpoint attribution and asynchronous image storage/count metadata. Existing completed image tasks can recover actual image counts from stored result data, but tasks completed before storage keys were persisted cannot recover ZIP downloads after Redis expiry.

## Known issues

HTTP image object URLs on a different origin remain blocked by the default CSP. Use an HTTPS image storage or CDN URL for browser previews.

## Upstream baseline

Official release: v0.1.183
Official commit: e8cb019fabf8b55199436229044cbf9aa7a82564
