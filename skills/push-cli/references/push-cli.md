# Push CLI Reference

## Commands

Run these from the repository root:

    python3 skills/push-cli/scripts/push_cli.py check
    python3 skills/push-cli/scripts/push_cli.py push
    python3 skills/push-cli/scripts/push_cli.py ensure
    python3 skills/push-cli/scripts/push_cli.py watch

check is a read-only push-readiness gate apart from ignored dependency and build
caches. It probes the platform runtime on the host, then re-executes the matrix
inside the validation container. push repeats that gate in the same process
before pushing. watch is for monitoring a run after a local terminal disconnect.
ensure prepares the selected container runtime and validation image without
requiring a clean worktree or running the check matrix.

## GitHub CLI Gate

The checker requires all of the following:

    gh --version
    gh auth status --hostname github.com
    gh repo view <owner>/<repo>
    gh api repos/<owner>/<repo> --jq .permissions.push

The repository is derived from the origin remote and is not accepted if it is
not LuckyKuang/sub2api-plus. A failed auth or permission check stops before
runtime probing, image build, tests, or Git writes.

Before a push, the checker runs:

    gh auth setup-git
    git push origin HEAD:<current-branch>

The exact branch name comes from the checked-out branch. It never pushes another
local ref selected by an untrusted argument.

## Validation Image

The matrix runs in the image built from `deploy/Dockerfile.validation`. The
image pins Go, Node, pnpm, golangci-lint, goreleaser, Python, and Bash to the
repository declarations. Host toolchain installers are not used.

Host-side `ensure` and the first half of `check`/`push` only:

1. probe the selected runtime;
2. inspect or build `sub2api-validation:<dockerfile-digest>`;
3. launch `container run` / `wsl … docker run` / `docker run`.

A missing runtime or failed image build is a hard failure. Host validation
fallback is forbidden.

## In-container Check Matrix

The matrix mirrors CONTRIBUTING.md, backend/Makefile, and
.github/workflows/backend-ci.yml, and always runs with
`SUB2API_IN_VALIDATION=1`:

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
    bash deploy/tests/apple-container-test.sh

When origin/<current-branch> exists, run
python3 tools/check_new_migrations.py --base origin/<current-branch>.

The Apple Container lifecycle test is a fixture script. It runs on every
platform inside the validation image and is not a substitute for launching
that image through Apple Containers, WSL2 Docker, or Linux Docker.

## Runtime Final Gate

When Apple Containers is selected on macOS, `ensure` and the environment probe
only require `container --version` and `container ls`. The check matrix then
runs with `container run`. The Docker Compose gate is reported as not
applicable, not passed. GitHub Actions remains authoritative for Docker image
and Compose behavior.

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
4. build or inspect the validation image with `container build`/`image inspect`;
5. during check/push only, run the matrix with `container run`.

Apple Containers is the only supported macOS runtime. Absence, an unusable CLI,
a stopped service, or a failed image launch is an environment or validation
failure. Neither case is a fallback condition. Never probe Colima, Docker
Desktop, or any other Docker endpoint on macOS. Never start a user-managed
runtime implicitly.

On Windows, check `wsl.exe -l -v` before any host Docker probe. Require a
running WSL2 Debian or Ubuntu family distribution, then execute `docker info`
and `docker compose version` inside it. Ignore any other running WSL
distribution. Use `wslpath` to translate the repository path before image
build, Compose parsing, and `docker run`. Missing WSL2, no running
Debian/Ubuntu distribution, or unusable in-distribution Docker/Compose is a
hard failure. Never probe or use a Windows-host Docker command as a fallback
that bypasses the WSL2 Debian/Ubuntu requirement. Never run the matrix in the
WSL shell outside Docker.

On Linux, use the native Docker CLI and daemon for probe, image build, Compose
parse, and `docker run`. Do not silently use Podman, a different container
implementation, or host-side Go/pnpm.

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
