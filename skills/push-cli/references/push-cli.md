# Push CLI Reference

## Commands

Run these from the repository root:

    python3 skills/push-cli/scripts/push_cli.py check
    python3 skills/push-cli/scripts/push_cli.py push
    python3 skills/push-cli/scripts/push_cli.py ensure
    python3 skills/push-cli/scripts/push_cli.py watch

check is a read-only push-readiness gate apart from ignored dependency and build
caches. push repeats the gate in the same process before pushing. watch is for
monitoring a run after a local terminal disconnect. ensure aligns the host
toolchain and selected container runtime without requiring a clean worktree or
running the check matrix.

## GitHub CLI Gate

The checker requires all of the following:

    gh --version
    gh auth status --hostname github.com
    gh repo view <owner>/<repo>
    gh api repos/<owner>/<repo> --jq .permissions.push

The repository is derived from the origin remote and is not accepted if it is
not LuckyKuang/sub2api-plus. A failed auth or permission check stops before
toolchain alignment, runtime probing, tests, or Git writes. Environment
alignment and the selected container-runtime probe run before the dirty-worktree
gate so a stale host can be repaired without committing first.

Before a push, the checker runs:

    gh auth setup-git
    git push origin HEAD:<current-branch>

The exact branch name comes from the checked-out branch. It never pushes another
local ref selected by an untrusted argument.

## Local Check Matrix

The matrix mirrors CONTRIBUTING.md, backend/Makefile, and
.github/workflows/backend-ci.yml:

    cd backend && go mod tidy -diff
    python3 skills/push-cli/tests/test_push_cli.py
    cd backend && go test -tags=unit ./...
    cd backend && go test -tags=integration ./...
    cd backend && golangci-lint run ./...
    pnpm --dir frontend install --frozen-lockfile
    pnpm --dir frontend run lint:check
    pnpm --dir frontend run typecheck
    pnpm --dir frontend run test:run
    pnpm --dir frontend run build
    pnpm --dir frontend audit --prod --audit-level=high --json
    python tools/check_pnpm_audit_exceptions.py \
      --audit <temporary-json> \
      --exceptions .github/audit-exceptions.yml

The checker writes audit JSON to a temporary file rather than replacing
frontend/audit.json. A vulnerability exit status is accepted only when the
output is valid audit JSON and every high/critical advisory has a current,
matching exception. Audit execution errors, missing exceptions, and expired
exceptions fail the local gate.

Repository policy and deployment checks:

    python3 tools/test_release_policy.py
    python3 tools/check_openai_codex_identity.py
    python3 tools/check_readme_sync.py
    python3 tools/check_release.py
    bash -n deploy/install.sh
    bash -n deploy/apple-container.sh
    sh deploy/tests/docker-compose-security-test.sh
    sh deploy/tests/docker-runtime-resources-test.sh
    bash deploy/test-caddyfile-cache.sh

When origin/<current-branch> exists, run
python3 tools/check_new_migrations.py --base origin/<current-branch>.

## Runtime Final Gate

When Apple Containers is selected on macOS, `ensure` and the environment probe
only require `container --version` and `container ls`. The repository
lifecycle test runs with the local check matrix, not during environment
alignment:

    bash deploy/tests/apple-container-test.sh

A successful lifecycle test is sufficient for local runtime validation. The
Docker Compose gate is reported as not applicable, not passed. GitHub Actions
remains authoritative for Docker image and Compose behavior.

For WSL2 Debian/Ubuntu Docker and native Linux Docker, the checker first proves
that the selected endpoint answers both:

    docker info
    docker compose version

It then parses the development Compose file without starting or deleting a
user-managed stack:

    docker compose -f deploy/docker-compose.dev.yml config --quiet

The static deployment checks above remain required for every runtime. On a
Docker-based path, a missing daemon, missing Compose plugin, or invalid Compose
file fails the push gate.

## Runtime Rules

On macOS, the order is:

1. require the Apple Containers `container` CLI;
2. require `container --version` and `container ls` to succeed;
3. if Apple Containers is absent or unusable, stop;
4. during check/push only, run the Apple Container lifecycle test with the
   local check matrix.

Apple Containers is the only supported macOS runtime. Absence, an unusable CLI,
or a stopped service is an environment failure. A failed lifecycle test is a
validation failure. Neither case is a fallback condition. Never probe Colima,
Docker Desktop, or any other Docker endpoint on macOS. Never start a
user-managed runtime implicitly.

On Windows, check `wsl.exe -l -v` before any host Docker probe. Require a
running WSL2 Debian or Ubuntu family distribution, then execute `docker info`
and `docker compose version` inside it. Ignore any other running WSL
distribution. Use `wslpath` to translate the repository path before Compose
parsing. Missing WSL2, no running Debian/Ubuntu distribution, or unusable
in-distribution Docker/Compose is a hard failure. Never probe or use a
Windows-host Docker command as a fallback that bypasses the WSL2
Debian/Ubuntu requirement.

On Linux, use the native Docker CLI and daemon. Do not silently use Podman or a
different container implementation.

## Remote Monitoring

After pushing, identify every Actions run matching both the branch and pushed
HEAD SHA. Use GitHub CLI only:

    gh run list --branch <branch> --event push
    gh run watch <run-id> --exit-status

If no matching run appears within the polling window, stop. A completed push
without a discovered Actions run is a hard failure, not a partial success.
Watch every matching run; if any run fails, report its URL and failed
conclusion without retrying. A successful local gate is not a substitute for
successful remote runs.
