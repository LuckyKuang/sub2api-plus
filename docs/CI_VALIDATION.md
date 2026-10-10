# CI validation coverage and migration

This migration keeps the existing required local submission gate. Ordinary and
release-candidate PRs still use push-cli `submit-pr` with its full container
matrix. CI-only submission and a replacement trusted-success context are not
enabled by this coverage change.

## Maintained check definitions

[validation_checks.py](../tools/validation_checks.py) owns commands shared by
local full validation and CI. [ci_validation.py](../tools/ci_validation.py)
launches them through [validation_runtime.py](../tools/validation_runtime.py).
Host launchers inspect metadata, manage images/caches and launch containers;
application, policy and deterministic-tree checks execute inside containers.

| Context / lane | Coverage | Relationship to local full |
| --- | --- | --- |
| `deployment-config` | Installer syntax, Apple lifecycle fixture, Docker security/gateway/resources, Caddy policy | Union of local and CI deployment checks |
| `test` | Full unit suites on Linux amd64 and arm64; Docker-backed integration on amd64 | Same Go commands, additional architecture coverage |
| `frontend` | Frozen install, lint, typecheck, full Vitest, production build and its locale checks | Complete local frontend coverage; critical-only tests cannot substitute |
| `golangci-lint` | Complete backend lint | Same command |
| `repository-policy` | CLI/release/CI regressions, test-tag rules, identity, README, metadata and migrations | Shared full definition including local-only checks |
| `goreleaser-config` | Pinned configuration check with embedded Plus version | Additional CI check |
| `backend-security` | Pinned govulncheck | Additional security coverage |
| `frontend-security` | Production audit and documented exception gate | Same local audit helper; no duplicate audit in frontend CI |

The required `test` context requires both native unit jobs and integration to
succeed. Failed/skipped dependencies cannot pass through an unconditional
aggregator. The full local command inventory also runs CI regression tests and
gateway/test-tag checks formerly owned only by CI.
Release archive, workflow, OCI integrity and immutable pricing regressions and
the image script syntax check are also in both full profiles.

The image resolves Go, Node, pnpm, lint, GoReleaser, govulncheck and PyYAML from repository
declarations. Its identity includes these pins and the Dockerfile. Dependency
generation additionally includes Go and pnpm lock inputs. Remote caches are
separated by architecture and lane; fallback preserves the exact generation.
GitHub ref visibility still applies: prefixes do not guarantee cross-tag reuse
or warm compilation. No validation image is pushed to a registry.
Release frontend builds share the `frontend` cache lane with default-branch CI
under the same exact OS, architecture, toolchain and lock generation. They still
run frozen installation and the complete production build; other release lanes
retain their separate cache ownership.

## Containers and platform coverage

CI uses Docker on Linux and executes unit suites natively on amd64/arm64.
Local checks continue to use Apple Containers on macOS, Docker in supported
WSL2 Debian/Ubuntu, or Docker on Linux, with existing scoped cleanup.

The Apple lifecycle tests use fake runtime fixtures. Running them inside Linux
containers does not claim live Apple Containers/WSL integration. Record any
genuine additional platform requirements and supported-runner evidence before
replacing the local gate.

Docker integration runs on a dedicated ephemeral GitHub Linux VM. Only that
validation lane may mount the VM's Docker socket and matching CLI and use host networking
for testcontainers ports. It has read-only GitHub permissions, no publication
or proof-producer credentials, and no persisted checkout credentials. The socket
grants control over the candidate VM; privileged result production must never
share that VM. Testcontainers and one-shot cleanup remain scoped to the job;
no global prune or live deployment services are used.
Release image packaging uses the socket only in its separate publisher or
read-only rehearsal VM; publisher credentials never enter candidate validation.

The classification action also runs its deterministic-tree checks inside the
validation container. It stages output in a temporary mounted file, copies
metadata to GitHub outputs, and removes the temporary file on success/failure.

## Focused release finalization

Only an independently regenerated deterministic tree for an already verified
published tag may use `release-finalization`. Published tag, Release-workflow
and immutable-asset queries remain mandatory. Repository policy then runs the
focused published mapping check inside the container. Full application,
frontend and vulnerability suites are omitted for this verified profile.
Ordinary release preparation remains full-profile. Preparing/reusing a
toolchain image is not repeating the application matrix.

## CI authority cutover prerequisites

These coverage jobs do not publish local-success or replacement-success
statuses. PR-controlled workflow names alone cannot replace local proof.
Before CI-only submission is activated:

1. Finish coverage/platform accounting and capture successful full/focused CI
   at exact inputs.
2. Establish an approved protected controller/definition, isolated read-only
   candidate jobs and metadata-only privileged result production.
3. Enforce the approved required workflow or exclusively controlled producer
   identity in actual protection. A generic Actions app/check name is insufficient.
4. Bind proof to repo/PR, base/head, tested merge SHA/tree, profile/tag,
   definition/pins, run ID/attempt and all applicable required jobs. Reject
   missing/skipped/failed/foreign/stale evidence.
5. Require the new gate before removing the old one; synchronize CLI proof,
   promotion, repository rules and docs. Submit migration under the old rules.
   Preserve exact actual merged-main CI/Security Scan.

Inspection found an active strict default-branch ruleset with required local,
CI and security contexts and no bypass actors. This is a personal repository
with no configured Actions secrets at inspection. An enforceable exclusive
producer remains a prerequisite, so local full validation remains mandatory.
A maintainer's broad interactive OAuth credential must not be placed in
candidate jobs as a workaround.

## Focused checks

Inside the prescribed validation container:

```bash
python3 tools/test_ci_validation.py
python3 tools/test_release_policy.py
python3 skills/push-cli/tests/test_push_cli.py
```

For host orchestration of a focused local container check:

```bash
python3 tools/ci_validation.py ensure
python3 tools/ci_validation.py exec -- python3 tools/test_ci_validation.py
```

The helper performs no push, PR creation, successful local status or release
publication. Final submission still uses the maintained push-cli entrypoint.
