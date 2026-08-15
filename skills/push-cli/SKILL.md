---
name: push-cli
description: Safely validate and push Sub2API Plus branch commits. Use when the user asks to push code, publish the current branch to GitHub, or verify local CI readiness before a push. Require an installed and authenticated GitHub CLI, align the local environment before any dirty-worktree gate, require Apple Containers on macOS with no Colima/Docker fallback, require usable Docker inside a running WSL2 Debian or Ubuntu distribution on Windows with no host-Docker fallback, run the repository's strict Go, frontend, documentation, deployment, and container checks, push only after every check passes, and monitor the resulting GitHub Actions run.
---

# Push CLI

Use the bundled checker from the repository root:

    python3 skills/push-cli/scripts/push_cli.py check

Align the host environment without running the full check matrix:

    python3 skills/push-cli/scripts/push_cli.py ensure

Run the push operation only for an explicit user request:

    python3 skills/push-cli/scripts/push_cli.py push

To monitor a push after a terminal disconnect or an interrupted local session:

    python3 skills/push-cli/scripts/push_cli.py watch

The checker is intentionally strict. It does not provide a skip-runtime,
skip-test, or unauthenticated fallback mode. Environment alignment happens
before the dirty-worktree gate so a stale toolchain or unusable runtime can
be repaired without committing first.

## Mandatory GitHub CLI Gate

Run the GitHub CLI gate before any repository mutation or local verification:

1. Require gh and verify gh --version.
2. Run gh auth status --hostname github.com.
3. Resolve the origin remote and verify it is the expected GitHub repository.
4. Run gh repo view and confirm the authenticated account can access it.
5. Confirm the account has push permission through gh api.

Stop immediately when gh is missing, unauthenticated, expired, pointed at the
wrong host, unable to read the repository, or missing push permission. Do not run
gh auth login automatically and do not replace GitHub CLI with curl, browser
automation, anonymous API calls, or an unrelated credential.

The script uses gh auth setup-git immediately before an authorized push so the
Git transport uses the authenticated GitHub CLI credential. Git is used for the
actual branch transfer because gh does not replace git push.

## Workflow

1. Run the GitHub CLI gate.
2. Require a non-detached branch.
3. Align and verify the declared Go, pnpm, and golangci-lint versions. Node
   must already meet the minimum major version; do not auto-change Node.
4. Probe the host container runtime using the platform rules below. A failed
   runtime is an environment failure, not a reason to switch implementations.
5. For check and push, require a clean worktree. Commit local changes first.
6. Run all local checks and the selected runtime's final gate.
7. Re-check the worktree. A generated or unexpected file is a failure, not an
   invitation to push it.
8. For push, run gh auth setup-git, then push exactly
   git push origin HEAD:<current-branch>.
9. Find the GitHub Actions run for the pushed SHA and run
   gh run watch <run-id> --exit-status.
10. Report the run URL and stop on any remote failure. Do not retry or amend code
    without a new user request.

ensure performs steps 1-4 and never inspects the worktree or runs the check
matrix. check performs steps 1-7 and never pushes. push performs all steps.
watch performs the GitHub CLI gate and remote monitoring for the current
branch SHA.

## Runtime Selection

### macOS

Apple Containers is the only supported macOS runtime. Require the `container`
CLI, then require both `container --version` and `container ls` to succeed.
That readiness probe is the environment gate. The repository Apple Container
lifecycle test runs later with the local check matrix, not during `ensure`.
A passing lifecycle test satisfies the macOS runtime gate without Docker,
Colima, or Docker Compose.

If Apple Containers is absent or unusable, stop immediately. If the later
lifecycle test fails, treat that as a validation failure, not a reason to
switch runtimes. Do not probe or fall back to Colima, Docker Desktop, or any
other Docker endpoint. Do not start or stop Apple Containers or any
user-managed stack implicitly.

### Windows

Before probing any Windows-host Docker endpoint, require `wsl.exe -l -v` and
select a running WSL2 Debian or Ubuntu family distribution (`Debian`, `Ubuntu`,
`Ubuntu-24.04`, and similar). Ignore Fedora, openSUSE, Kali, docker-desktop, and
any other distribution even if they are running. Run `docker info` and
`docker compose version` inside the selected distribution. Resolve the
repository path with `wslpath` before invoking Docker Compose. This
Debian/Ubuntu WSL2 ordering is mandatory. Stop when WSL2, a running Debian or
Ubuntu distribution, Docker Engine, or the Compose plugin is missing inside that
distribution. Do not fall back to a Windows-host Docker CLI or daemon, and do
not install or start WSL, Linux, Docker Desktop, or Docker Engine implicitly.

### Linux and other hosts

Require a directly reachable Docker daemon and Docker Compose plugin. Unsupported
hosts fail closed instead of claiming that the Docker gate passed.

## Checks

The bundled checker uses commands already defined by the repository:

- Go module tidiness, unit tests, integration tests, and golangci-lint.
- Push CLI self-tests, including the mandatory macOS Apple Containers-only
  runtime and the Windows WSL2 Debian/Ubuntu Docker requirement.
- Frozen pnpm install, frontend lint, typecheck, Vitest, and production build.
- Production pnpm audit and the same high/critical exception policy used by the
  Security Scan workflow.
- Release-policy, OpenAI Codex identity, README synchronization, and migration
  checks when a comparable remote branch exists.
- Installer syntax, Docker Compose security, Docker runtime resources, and Caddy
  cache policy tests.
- The full Apple Container lifecycle test when Apple Containers is selected.
- Docker Compose configuration parsing when a Docker-based runtime is selected.

The Apple Containers path reports the Docker Compose final gate as not
applicable. It must not report that Compose passed. GitHub Actions remains the
authoritative Docker image and Compose validation for that path.

Do not replace these commands with ad hoc equivalents or silently downgrade
tool versions. Local success reduces preventable CI failures but cannot guarantee
cloud-runner success; always monitor the remote Actions run after pushing.

## Safety

- Never push a dirty worktree, detached HEAD, or a branch other than the current
  checked-out branch.
- Never use git push --all, git push --tags, force push, or an unreviewed remote
  name.
- Never print credentials, tokens, environment files, or Docker secrets.
- Treat generated Ent/Wire files and build outputs as validation artifacts. If
  they remain after checks, stop and report them.
- A failed local check or failed GitHub Actions run is a hard stop.

Read references/push-cli.md for the exact repository matrix, runtime behavior,
and failure handling. Use scripts/push_cli.py for deterministic execution.
