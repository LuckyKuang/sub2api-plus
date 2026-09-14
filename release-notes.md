Sub2API Plus v0.2.4+custom.005

## Highlights

Usage details now report generation throughput with a request-aware TPS calculation, and account management keeps group assignments compact while preserving access to the complete group list.

## Changed

- Enforced explicit backend test build tags and split the validation lanes for clearer CI coverage.
- Added TPS beneath first-token and total-duration timing in the shared administrator and user usage table.

## Fixed

- Calculated streaming TPS from the generation interval after first-token latency, with total request duration as the fallback for non-streaming requests.
- Limited account group summaries to two rows and exposed every remaining group through a stable overflow control.

## Compatibility and migration

None.

## Known issues

None.

## Upstream baseline

Official release: v0.2.4
Official commit: 5de5e2bed035d43591a2e10e51f420ef6a84eb98
