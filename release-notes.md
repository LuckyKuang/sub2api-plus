Sub2API Plus v0.1.171+custom.001

## Highlights

- Sync the official v0.1.171 security, CAPTCHA, billing, scheduling, and Codex client compatibility updates.
- Preserve Sub2API Plus outbound identity, OAuth session-sharing, local quota, deployment, and release behavior.

## Changed

- Add Tencent Tianyu and Alibaba CAPTCHA configuration and authentication flows.
- Add group profit-control settings and forward-only database migrations 198 and 199.

## Compatibility and migration

- Apply database migrations 198 and 199 before enabling group profit control.
- Existing custom OpenAI/Codex user-agent configuration remains supported; Codex client-version synchronization now applies to the selected outbound identity.

## Known issues

- CAPTCHA provider credentials and client IDs must be configured before their corresponding authentication gates can be enabled.
- GitHub Release publication and OCI image publication require the separate maintainer approval workflow.

## Upstream baseline

Official release: v0.1.171
Official commit: f0e7a9c7a23a7d02fb159b62fa809621eb0475a6
