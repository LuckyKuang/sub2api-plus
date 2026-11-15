Sub2API Plus v0.2.21+custom.002

## Highlights

- Unify the image task center: the console now shows one row per task with user-scoped history, filters, explicit read states and a single generation entry, while per-model-family switches, the nano-banana aliases and immediate switch persistence keep the flow predictable.
- Add a dedicated Image Task panel to admin settings with ordinary/batch switches and an S3-compatible storage target that reuses the backup bucket when asked and can test the connection before saving.
- List only the models a group's accounts actually serve, and hide standard groups whose models are unavailable to users.
- Make subscription adjustments plan-aware with absolute expiry and quota-window reset, and invalidate the models cache whenever accounts change.

## Changed

- Image task history stays readable after an administrator switches new submissions off; only new submissions are affected.
- The admin menu entry follows the capability the server reports, so a hidden entry never stands in for authorization.

## Compatibility and migration

- Application version 0.2.21+custom.002; git tag v0.2.21+custom.002; GHCR image tag v0.2.21-custom.002.
- Same official baseline v0.2.21. No new migrations and no schema change; deployed SQL is untouched.
- Existing deployments keep their current behavior until an administrator enables image tasks and configures a storage target. Reusing the backup S3 settings avoids re-entering credentials.
- No configuration changes are required for other features.

## Known issues

- Enabling an image task switch with an incomplete storage target is rejected; configure the endpoint, bucket and credentials first.
- The storage connection test depends on the server being able to reach the configured endpoint.
- Group model lists are only as current as the account mappings; a stale mapping keeps its last observed models until accounts are refreshed.

## Upstream baseline

Official release: v0.2.21
Official commit: 10ba8f9b4dc2e6023d4a9879ab77b21841babd10
