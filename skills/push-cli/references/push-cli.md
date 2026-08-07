# Push CLI Reference

## Commands

Run these from the repository root:

    python3 skills/push-cli/scripts/push_cli.py check
    python3 skills/push-cli/scripts/push_cli.py push
    python3 skills/push-cli/scripts/push_cli.py watch

check is a read-only push-readiness gate apart from ignored dependency and build
caches. push repeats the gate in the same process before pushing. watch is for
monitoring a run after a local terminal disconnect.

## GitHub CLI Gate

The checker requires all of the following:

    gh --version
    gh auth status --hostname github.com
    gh repo view <owner>/<repo>
    gh api repos/<owner>/<repo> --jq .permissions.push

The repository is derived from the origin remote and is not accepted if it is
not LuckyKuang/sub2api-plus. A failed auth or permission check stops before
toolchain detection, tests, Docker access, or Git writes.

Before a push, the checker runs:

    gh auth setup-git
    git push origin HEAD:<current-branch>

The exact branch name comes from the checked-out branch. It never pushes another
local ref selected by an untrusted argument.

## Local Check Matrix

The matrix mirrors CONTRIBUTING.md, backend/Makefile, and
.github/workflows/backend-ci.yml:

    cd backend && go mod tidy -diff
    cd backend && go test -tags=unit ./...
    cd backend && go test -tags=integration ./...
    cd backend && golangci-lint run ./...
    pnpm --dir frontend install --frozen-lockfile
    pnpm --dir frontend run lint:check
    pnpm --dir frontend run typecheck
    pnpm --dir frontend run test:run
    pnpm --dir frontend run build

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

## Docker Final Gate

The checker first proves that the selected endpoint answers both:

    docker info
    docker compose version

It then parses the development Compose file without starting or deleting a
user-managed stack:

    docker compose -f deploy/docker-compose.dev.yml config --quiet

The static deployment checks above remain required even when the Compose parser
passes. A missing daemon, missing Compose plugin, or invalid Compose file fails
the push gate.

## Runtime Rules

On macOS, the order is:

1. container --version and container ls;
2. Apple Container lifecycle test;
3. running Colima and its Docker endpoint;
4. another directly reachable Docker endpoint.

Apple Containers cannot satisfy the Docker endpoint requirement by themselves.
If Apple Containers is usable but Docker is not, report the native test result
and fail the strict push gate.

On Windows, use a running WSL2 Linux distribution and execute Docker commands
inside it. Use wslpath to translate the repository path before Compose parsing.
Do not use a Windows Docker command that bypasses the WSL2 requirement.

On Linux, use the native Docker CLI and daemon. Do not silently use Podman or a
different container implementation.

## Remote Monitoring

After pushing, identify every Actions run matching both the branch and pushed
HEAD SHA. Use GitHub CLI only:

    gh run list --branch <branch> --event push
    gh run watch <run-id> --exit-status

If no matching run appears within the polling window, report that the push
succeeded but CI discovery is incomplete. Watch every matching run; if any run
fails, report its URL and failed conclusion without retrying. A successful local
gate is not a substitute for successful remote runs.
