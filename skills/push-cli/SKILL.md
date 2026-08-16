---
name: push-cli
description: Safely validate and push Sub2API Plus branch commits. Use when the user asks to push code, publish the current branch to GitHub, or verify local CI readiness before a push. Require an installed and authenticated GitHub CLI, probe the platform container runtime before any dirty-worktree gate, and run every check matrix inside that runtime. On macOS require Apple Containers with no Colima/Docker fallback. On Windows require Docker inside a running WSL2 Debian or Ubuntu distribution with no host-Docker fallback. On Linux require Docker. Never validate on the host. Push only after every check passes, and monitor the resulting GitHub Actions run.
---

# Push CLI

Use the bundled checker from the repository root:

    python3 skills/push-cli/scripts/push_cli.py check

Prepare the platform runtime and validation image without running the check
matrix:

    python3 skills/push-cli/scripts/push_cli.py ensure

Run the push operation only for an explicit user request:

    python3 skills/push-cli/scripts/push_cli.py push

To monitor a push after a terminal disconnect or an interrupted local session:

    python3 skills/push-cli/scripts/push_cli.py watch

The checker is intentionally strict. It does not provide a skip-runtime,
skip-test, host-toolchain, or unauthenticated fallback mode. Host processes
may only run GitHub CLI, git, the runtime probe, validation-image
build/inspect, the selected runtime's Compose parse, and the authorized
push. Every Go, frontend, Python, installer, and lifecycle check runs inside
the validation container.

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
3. Probe the platform container runtime using the rules below. A failed
   runtime is an environment failure, not a reason to switch implementations.
4. Ensure the repository validation image exists. Build it with the selected
   runtime when missing. Never install Go, pnpm, or golangci-lint on the host.
5. For check and push, require a clean worktree. Commit local changes first.
6. Re-exec the check matrix inside the validation container.
7. On Docker-based runtimes, parse the development Compose file with that
   runtime. On Apple Containers, report the Compose gate as not applicable.
8. Re-check the worktree. A generated or unexpected file is a failure, not an
   invitation to push it.
9. For push, run gh auth setup-git, then push exactly
   git push origin HEAD:<current-branch>.
10. Find the GitHub Actions run for the pushed SHA and run
    gh run watch <run-id> --exit-status.
11. Report the run URL and stop on any remote failure. Do not retry or amend
    code without a new user request.

ensure performs steps 1-4 and never inspects the worktree or runs the check
matrix. check performs steps 1-8 and never pushes. push performs all steps.
watch performs the GitHub CLI gate and remote monitoring for the current
branch SHA.

## Runtime Selection

### macOS

Apple Containers is the only supported macOS runtime. Require the `container`
CLI, then require both `container --version` and `container ls` to succeed.
That readiness probe is the environment gate. The check matrix then runs with
`container run` against `deploy/Dockerfile.validation`. Host-side Go, pnpm,
Node, and Docker are forbidden.

If Apple Containers is absent or unusable, stop immediately. If the later
in-container matrix fails, treat that as a validation failure, not a reason to
switch runtimes. Do not probe or fall back to Colima, Docker Desktop, or any
other Docker endpoint. Do not start or stop Apple Containers or any
user-managed stack implicitly.

### Windows

Before probing any Windows-host Docker endpoint, require `wsl.exe -l -v` and
select a running WSL2 Debian or Ubuntu family distribution (`Debian`, `Ubuntu`,
`Ubuntu-24.04`, and similar). Ignore Fedora, openSUSE, Kali, docker-desktop, and
any other distribution even if they are running. Run `docker info` and
`docker compose version` inside the selected distribution. Resolve the
repository path with `wslpath` before invoking Docker. The check matrix then
runs with `wsl -d <distro> -- docker run`. Entering the WSL shell without
Docker, or using a Windows-host Docker CLI, is forbidden.

Stop when WSL2, a running Debian or Ubuntu distribution, Docker Engine, or the
Compose plugin is missing inside that distribution. Do not install or start
WSL, Linux, Docker Desktop, or Docker Engine implicitly.

### Linux

Require a directly reachable Docker daemon and Docker Compose plugin. The
check matrix runs with `docker run`. Host-side `go test`, `pnpm`, and
`golangci-lint` are forbidden. Unsupported hosts fail closed instead of
claiming that the Docker gate passed.

## Checks

The bundled checker uses commands already defined by the repository. They
execute only inside the validation image:

- Go module tidiness, unit tests, integration tests, and golangci-lint.
- Push CLI self-tests, including the mandatory macOS Apple Containers-only
  runtime, the Windows WSL2 Debian/Ubuntu Docker requirement, and the Linux
  Docker requirement.
- Frozen pnpm install, frontend lint, typecheck, Vitest, and production build.
- Production pnpm audit and the same high/critical exception policy used by the
  Security Scan workflow.
- Release-policy, OpenAI Codex identity, README synchronization, and migration
  checks when a comparable remote branch exists.
- Installer syntax, Docker Compose security, Docker runtime resources, Caddy
  cache policy tests, and the Apple Container lifecycle fixture test.

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
- Never run the check matrix on the host, even when host toolchains already
  match.
- Treat generated Ent/Wire files and build outputs as validation artifacts. If
  they remain after checks, stop and report them.
- A failed local check or failed GitHub Actions run is a hard stop.

Read references/push-cli.md for the exact repository matrix, runtime behavior,
and failure handling. Use scripts/push_cli.py for deterministic execution.
