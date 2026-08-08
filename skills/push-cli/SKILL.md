---
name: push-cli
description: Safely validate and push Sub2API Plus branch commits. Use when the user asks to push code, publish the current branch to GitHub, or verify local CI readiness before a push. Require an installed and authenticated GitHub CLI, run the repository's strict Go, frontend, documentation, deployment, and container checks, use Apple Containers as the self-sufficient primary macOS runtime with Colima/Docker fallback, use WSL2 Docker on Windows, push only after every check passes, and monitor the resulting GitHub Actions run.
---

# Push CLI

Use the bundled checker from the repository root:

    python3 skills/push-cli/scripts/push_cli.py check

Run the push operation only for an explicit user request:

    python3 skills/push-cli/scripts/push_cli.py push

To monitor a push after a terminal disconnect or an interrupted local session:

    python3 skills/push-cli/scripts/push_cli.py watch

The checker is intentionally strict. It does not provide a skip-runtime,
skip-test, or unauthenticated fallback mode.

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
2. Require a non-detached branch and a clean worktree. Commit local changes first.
3. Resolve the declared Go, Node.js, pnpm, and golangci-lint versions.
4. Probe the host container runtime using the platform rules below.
5. Run all local checks and the selected runtime's final gate.
6. Re-check the worktree. A generated or unexpected file is a failure, not an
   invitation to push it.
7. For push, run gh auth setup-git, then push exactly
   git push origin HEAD:<current-branch>.
8. Find the GitHub Actions run for the pushed SHA and run
   gh run watch <run-id> --exit-status.
9. Report the run URL and stop on any remote failure. Do not retry or amend code
   without a new user request.

check performs steps 1-6 and never pushes. push performs all steps. watch
performs the GitHub CLI gate and remote monitoring for the current branch SHA.

## Runtime Selection

### macOS

Probe Apple Containers first. When its CLI and runtime are ready, run the
repository Apple Container lifecycle test. A passing lifecycle test satisfies
the macOS runtime gate without Docker, Colima, or Docker Compose. If that test
fails, stop immediately; do not hide an Apple deployment regression by falling
back to another runtime.

When Apple Containers is absent or not ready, probe Colima next and use its
Docker endpoint. If Colima is not available, accept another directly reachable
Docker endpoint such as Docker Desktop. Docker-based paths must pass the Compose
final gate. If no supported runtime is ready, stop and report the exact missing
runtime. Do not start or stop Apple Containers, Colima, Docker Desktop, or any
user-managed stack implicitly.

### Windows

Probe wsl.exe -l -v and select a running Linux distribution whose version is 2.
Run docker info and docker compose version inside that distribution. Resolve
the repository path with wslpath before invoking Docker Compose. Stop when WSL2,
a Linux distribution, or a running Docker daemon is missing. Do not install or
start WSL, Linux, Docker Desktop, or Docker Engine implicitly.

### Linux and other hosts

Require a directly reachable Docker daemon and Docker Compose plugin. Unsupported
hosts fail closed instead of claiming that the Docker gate passed.

## Checks

The bundled checker uses commands already defined by the repository:

- Go module tidiness, unit tests, integration tests, and golangci-lint.
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
