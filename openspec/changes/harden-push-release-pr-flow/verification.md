## Verification

Verified on 2026-08-16 from `sync/upstream-v0.1.177`.

| Gate | Runtime | Result |
| --- | --- | --- |
| push-cli self-tests | Apple Containers validation image | 36 passed |
| release-cli self-tests | Apple Containers validation image | 15 passed |
| release policy tests | Apple Containers validation image | 20 passed |
| Python compilation | Apple Containers validation image | Passed |
| Full repository matrix | Apple Containers with canonical cache/user mounts | Passed |
| Frontend Vitest | Apple Containers, full matrix | 232 files and 1601 tests passed |
| Actionlint 1.7.12, pinned digest | Apple Containers | Passed |
| Strict OpenSpec validation | Host CLI, specification only | Passed |
| `git diff --check` | Host Git, formatting only | Passed |

The full matrix covered Apple Container lifecycle behavior, Go module
tidiness, backend unit and integration tests, golangci-lint, frozen frontend
install, lint, typecheck, Vitest, production build, release and identity policy,
README synchronization, release metadata, deployment syntax/security, Caddy
behavior, migration policy, and the production dependency audit.

One setup-only retry launched the validation image without the canonical Go,
pnpm, and node_modules cache mounts and exhausted its temporary writable layer.
It failed in `testing.TempDir` with a read-only `/tmp`; rerunning with the exact
`validation_runtime.py` mounts completed successfully. No source change was
made for that environment-only failure.

## External GitHub State

Read-only inspection found repository Auto-merge disabled and no active classic
branch protection or repository ruleset for `main`. This change intentionally
does not mutate those external settings. Until an administrator enables the
documented merge-commit, strict status-check, pull-request, and Auto-merge
prerequisites, `release-cli promote-pr` fails closed before merge mutation.
