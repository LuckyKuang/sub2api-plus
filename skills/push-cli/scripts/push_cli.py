#!/usr/bin/env python3
"""Validate and safely push a Sub2API Plus branch."""

from __future__ import annotations

import argparse
import json
import platform
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


ROOT = Path(__file__).resolve().parents[3]
DEFAULT_REMOTE = "origin"
EXPECTED_REPOSITORY = "LuckyKuang/sub2api-plus"
GO_VERSION_RE = re.compile(r"^go\s+(\d+\.\d+(?:\.\d+)?)\s*$", re.MULTILINE)
VERSION_RE = re.compile(
    r"\bversion(?:\s*[:=]|\s+)(?:v)?(\d+\.\d+\.\d+)",
    re.IGNORECASE,
)


class PushCliError(RuntimeError):
    """A hard failure that must stop validation or pushing."""


@dataclass(frozen=True)
class Runtime:
    name: str
    prefix: tuple[str, ...] = ()
    compose_root: str | None = None
    compose_required: bool = True


def display(command: Sequence[str]) -> str:
    return shlex.join(str(item) for item in command)


def run_command(
    command: Sequence[str],
    *,
    cwd: Path | None = None,
    capture: bool = False,
    merge_stderr: bool = True,
) -> subprocess.CompletedProcess[str]:
    actual_cwd = ROOT if cwd is None else cwd
    try:
        return subprocess.run(
            [str(item) for item in command],
            cwd=actual_cwd,
            check=False,
            text=True,
            encoding="utf-8",
            errors="replace",
            stdout=subprocess.PIPE if capture else None,
            stderr=(subprocess.STDOUT if merge_stderr else subprocess.PIPE)
            if capture
            else None,
        )
    except FileNotFoundError as error:
        raise PushCliError(f"required command is unavailable: {error.filename}") from error


def capture(command: Sequence[str], *, cwd: Path | None = None) -> str:
    result = run_command(command, cwd=cwd, capture=True)
    if result.returncode != 0:
        detail = (result.stdout or "").strip()
        raise PushCliError(
            f"{display(command)} failed with exit code {result.returncode}"
            + (f": {detail[-2000:]}" if detail else "")
        )
    return (result.stdout or "").strip()


def optional_capture(
    command: Sequence[str],
    *,
    cwd: Path | None = None,
) -> tuple[bool, str]:
    try:
        result = run_command(command, cwd=cwd, capture=True)
    except PushCliError as error:
        return False, str(error)
    return result.returncode == 0, (result.stdout or "").strip()


def require_command(command: str) -> None:
    if shutil.which(command) is None:
        raise PushCliError(f"required command is unavailable: {command}")


def repo_from_remote(url: str) -> str:
    match = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$", url.strip())
    if not match:
        raise PushCliError(f"origin is not a GitHub repository URL: {url}")
    return f"{match.group(1)}/{match.group(2)}"


def github_gate(remote: str) -> str:
    require_command("gh")
    version = capture(["gh", "--version"]).splitlines()[0]
    print(f"GitHub CLI: {version}")

    auth = run_command(
        ["gh", "auth", "status", "--hostname", "github.com"],
        capture=True,
    )
    if auth.returncode != 0:
        detail = (auth.stdout or "").strip()
        raise PushCliError(
            "GitHub CLI is missing a valid github.com login. Run "
            "gh auth login and retry."
            + (f"\n{detail[-2000:]}" if detail else "")
        )

    remote_url = capture(["git", "remote", "get-url", remote])
    repository = repo_from_remote(remote_url)
    if repository != EXPECTED_REPOSITORY:
        raise PushCliError(
            f"origin resolves to {repository}; expected {EXPECTED_REPOSITORY}"
        )

    capture(["gh", "repo", "view", repository, "--json", "nameWithOwner"])
    push_permission = capture(
        ["gh", "api", f"repos/{repository}", "--jq", ".permissions.push"]
    ).lower()
    if push_permission != "true":
        raise PushCliError(
            f"authenticated GitHub account has no push permission for {repository}"
        )
    print(f"GitHub repository: {repository} (push permission confirmed)")
    return repository


def current_branch() -> str:
    branch = capture(["git", "branch", "--show-current"])
    if not branch or branch == "HEAD":
        raise PushCliError("detached HEAD is not pushable; check out a branch first")
    if any(char.isspace() for char in branch):
        raise PushCliError(f"invalid current branch name: {branch}")
    print(f"Current branch: {branch}")
    return branch


def require_clean_worktree() -> None:
    status = capture(
        ["git", "status", "--porcelain=v1", "--untracked-files=all"]
    )
    if status:
        raise PushCliError(
            "worktree is not clean; commit or remove these paths before pushing:\n"
            + status
        )
    diff_check = run_command(["git", "diff", "--check"], capture=True)
    if diff_check.returncode != 0:
        raise PushCliError(
            "git diff --check failed:\n" + (diff_check.stdout or "").strip()
        )


def declared_tool_version(name: str) -> str:
    tool_file = ROOT / ".tool-versions"
    for line in tool_file.read_text(encoding="utf-8").splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] == name:
            return fields[1].removeprefix("v")
    raise PushCliError(f".tool-versions does not declare {name}")


def check_toolchains() -> None:
    package = json.loads((ROOT / "frontend/package.json").read_text(encoding="utf-8"))
    package_manager = package.get("packageManager", "")
    pnpm_match = re.fullmatch(r"pnpm@(.+)", package_manager)
    if not pnpm_match:
        raise PushCliError("frontend/package.json must declare packageManager as pnpm@VERSION")

    go_mod = (ROOT / "backend/go.mod").read_text(encoding="utf-8")
    go_match = GO_VERSION_RE.search(go_mod)
    if not go_match:
        raise PushCliError("backend/go.mod does not declare a Go version")
    go_actual = capture(["go", "env", "GOVERSION"])
    if go_actual != f"go{go_match.group(1)}":
        raise PushCliError(
            f"Go {go_match.group(1)} is required; found {go_actual}"
        )

    pnpm_actual = capture(["pnpm", "--version"])
    if pnpm_actual != pnpm_match.group(1):
        raise PushCliError(
            f"pnpm {pnpm_match.group(1)} is required; found {pnpm_actual}"
        )

    node_actual = capture(["node", "--version"])
    node_match = re.search(r"(\d+)(?:\.(\d+))?", node_actual)
    node_minimum = re.search(
        r">=\s*(\d+)(?:\.(\d+))?", package.get("engines", {}).get("node", "")
    )
    if not node_match or not node_minimum:
        raise PushCliError("unable to determine the Node.js minimum version")
    if int(node_match.group(1)) < int(node_minimum.group(1)):
        raise PushCliError(
            f"Node.js {node_minimum.group(1)}+ is required; found {node_actual}"
        )

    lint_expected = declared_tool_version("golangci-lint")
    lint_output = capture(["golangci-lint", "version"])
    lint_match = VERSION_RE.search(lint_output)
    if not lint_match or lint_match.group(1) != lint_expected:
        raise PushCliError(
            f"golangci-lint {lint_expected} is required; found {lint_output!r}"
        )
    print(
        "Toolchains: "
        f"Go {go_actual}; pnpm {pnpm_actual}; Node.js {node_actual}; "
        f"golangci-lint {lint_expected}"
    )


def probe_docker(prefix: Sequence[str] = ()) -> tuple[bool, str]:
    docker_command = [*prefix, "docker"]
    info_ok, info_output = optional_capture([*docker_command, "info"])
    if not info_ok:
        return False, info_output
    compose_ok, compose_output = optional_capture(
        [*docker_command, "compose", "version"]
    )
    if not compose_ok:
        return False, compose_output
    return True, compose_output.splitlines()[0] if compose_output else "Docker Compose"


def parse_wsl_distributions(output: str) -> list[tuple[str, str]]:
    distributions: list[tuple[str, str]] = []
    for line in output.splitlines():
        match = re.match(r"^\s*\*?\s*(.*?)\s+(Running|Stopped)\s+2\s*$", line)
        if match and match.group(1):
            distributions.append((match.group(1).strip(), match.group(2)))
    return distributions


def probe_windows_runtime() -> Runtime:
    wsl = shutil.which("wsl.exe") or shutil.which("wsl")
    if not wsl:
        raise PushCliError("Windows push validation requires wsl.exe")
    status = capture([wsl, "-l", "-v"])
    distributions = parse_wsl_distributions(status)
    running = [name for name, state in distributions if state == "Running"]
    if not running:
        if distributions:
            names = ", ".join(name for name, _ in distributions)
            raise PushCliError(
                f"WSL2 Linux distributions exist but none are running: {names}"
            )
        raise PushCliError("no WSL2 Linux distribution was found")

    for distro in running:
        prefix = (wsl, "-d", distro, "--")
        docker_ok, detail = probe_docker(prefix)
        if not docker_ok:
            continue
        linux_root = capture(
            [wsl, "-d", distro, "--", "wslpath", "-a", str(ROOT)]
        )
        print(f"Runtime: WSL2/{distro} with Docker ({detail})")
        return Runtime("wsl2-docker", prefix, linux_root)
    raise PushCliError(
        "a running WSL2 Linux distribution was found, but Docker and Docker "
        "Compose are not usable inside it"
    )


def probe_runtime() -> Runtime:
    system = platform.system()
    if system == "Windows":
        return probe_windows_runtime()

    if system == "Darwin":
        if shutil.which("container"):
            version_ok, version = optional_capture(["container", "--version"])
            list_ok, list_output = optional_capture(["container", "ls"])
            if version_ok and list_ok:
                print(f"Runtime: Apple Containers ({version.splitlines()[0]})")
                run_step(
                    "Apple Container lifecycle test",
                    ["bash", str(ROOT / "deploy/tests/apple-container-test.sh")],
                )
                return Runtime("apple-containers", compose_required=False)
            else:
                print(
                    "Apple Containers detected but not ready; "
                    f"{list_output[-500:]}"
                )

        colima_ready = False
        if shutil.which("colima"):
            colima_ready, colima_status = optional_capture(["colima", "status"])
            if colima_ready:
                print(f"Runtime: Colima ({colima_status.splitlines()[0]})")

        docker_ok, docker_detail = probe_docker()
        if docker_ok:
            label = "colima/docker" if colima_ready else "docker"
            print(f"Docker endpoint: {label} ({docker_detail})")
            return Runtime(label)

        raise PushCliError(
            "Apple Containers is unavailable or not ready, and no usable Colima/"
            "Docker Desktop endpoint was found; start one supported runtime, then retry"
        )

    docker_ok, docker_detail = probe_docker()
    if docker_ok:
        print(f"Runtime: Docker ({docker_detail})")
        return Runtime("docker")
    if system == "Linux":
        raise PushCliError("Linux push validation requires a running Docker daemon")
    raise PushCliError(f"unsupported host platform: {system}")


def run_step(
    name: str,
    command: Sequence[str],
    cwd: Path | None = None,
) -> None:
    print(f"\n[{name}]")
    print(f"$ {display(command)}")
    result = run_command(command, cwd=cwd)
    if result.returncode != 0:
        raise PushCliError(f"{name} failed with exit code {result.returncode}")


def run_runtime_final_gate(runtime: Runtime) -> None:
    if not runtime.compose_required:
        print("\n[Docker Compose final gate]")
        print(
            "not applicable: Apple Containers is the selected macOS runtime; "
            "Docker image and Compose behavior remain covered by GitHub Actions"
        )
        return

    compose_path = "deploy/docker-compose.dev.yml"
    if runtime.compose_root:
        compose_path = f"{runtime.compose_root}/deploy/docker-compose.dev.yml"
    run_step(
        "Docker Compose final gate",
        [
            *runtime.prefix,
            "docker",
            "compose",
            "-f",
            compose_path,
            "config",
            "--quiet",
        ],
    )


def run_frontend_security_check() -> None:
    command = ["pnpm", "audit", "--prod", "--audit-level=high", "--json"]
    print("\n[Frontend production audit]")
    print(f"$ {display(command)}")
    result = run_command(
        command,
        cwd=ROOT / "frontend",
        capture=True,
        merge_stderr=False,
    )
    output = result.stdout or ""
    if result.returncode not in (0, 1):
        detail = (result.stderr or output).strip()
        raise PushCliError(
            f"Frontend production audit failed with exit code {result.returncode}"
            + (f": {detail[-2000:]}" if detail else "")
        )
    try:
        audit = json.loads(output)
    except json.JSONDecodeError as error:
        raise PushCliError("Frontend production audit returned invalid JSON") from error
    if not isinstance(audit, dict) or audit.get("error"):
        raise PushCliError("Frontend production audit returned an audit error")

    with tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        suffix=".json",
        delete=False,
    ) as audit_file:
        json.dump(audit, audit_file)
        audit_path = Path(audit_file.name)
    try:
        run_step(
            "Frontend audit exceptions",
            [
                sys.executable,
                "tools/check_pnpm_audit_exceptions.py",
                "--audit",
                str(audit_path),
                "--exceptions",
                ".github/audit-exceptions.yml",
            ],
            ROOT,
        )
    finally:
        audit_path.unlink(missing_ok=True)


def run_local_checks(remote: str, branch: str, runtime: Runtime) -> None:
    python = sys.executable
    backend = ROOT / "backend"
    steps: list[tuple[str, Sequence[str], Path]] = [
        ("Go module tidiness", ["go", "mod", "tidy", "-diff"], backend),
        ("Backend unit tests", ["go", "test", "-tags=unit", "./..."], backend),
        (
            "Backend integration tests",
            ["go", "test", "-tags=integration", "./..."],
            backend,
        ),
        ("Backend lint", ["golangci-lint", "run", "./..."], backend),
        (
            "Frontend frozen install",
            ["pnpm", "--dir", "frontend", "install", "--frozen-lockfile"],
            ROOT,
        ),
        (
            "Frontend lint",
            ["pnpm", "--dir", "frontend", "run", "lint:check"],
            ROOT,
        ),
        (
            "Frontend typecheck",
            ["pnpm", "--dir", "frontend", "run", "typecheck"],
            ROOT,
        ),
        (
            "Frontend tests",
            ["pnpm", "--dir", "frontend", "run", "test:run"],
            ROOT,
        ),
        (
            "Frontend production build",
            ["pnpm", "--dir", "frontend", "run", "build"],
            ROOT,
        ),
        (
            "Release policy tests",
            [python, "tools/test_release_policy.py"],
            ROOT,
        ),
        (
            "Codex outbound identity",
            [python, "tools/check_openai_codex_identity.py"],
            ROOT,
        ),
        ("README synchronization", [python, "tools/check_readme_sync.py"], ROOT),
        ("Release metadata sources", [python, "tools/check_release.py"], ROOT),
        ("Linux installer syntax", ["bash", "-n", "deploy/install.sh"], ROOT),
        (
            "Apple installer syntax",
            ["bash", "-n", "deploy/apple-container.sh"],
            ROOT,
        ),
        (
            "Docker Compose security",
            ["sh", "deploy/tests/docker-compose-security-test.sh"],
            ROOT,
        ),
        (
            "Docker runtime resources",
            ["sh", "deploy/tests/docker-runtime-resources-test.sh"],
            ROOT,
        ),
        (
            "Caddy cache policy",
            ["bash", "deploy/test-caddyfile-cache.sh"],
            ROOT,
        ),
    ]

    base_ref = f"{remote}/{branch}"
    base_check = run_command(["git", "rev-parse", "--verify", base_ref], capture=True)
    if base_check.returncode == 0:
        steps.append(
            (
                "Migration policy",
                [python, "tools/check_new_migrations.py", "--base", base_ref],
                ROOT,
            )
        )

    for name, command, cwd in steps:
        run_step(name, command, cwd)

    run_frontend_security_check()
    run_runtime_final_gate(runtime)


def ensure_clean_after_checks() -> None:
    status = capture(
        ["git", "status", "--porcelain=v1", "--untracked-files=all"]
    )
    if status:
        raise PushCliError(
            "checks created or exposed worktree changes; refusing to push:\n" + status
        )


def pushed_sha() -> str:
    return capture(["git", "rev-parse", "HEAD"])


def find_actions_runs(
    repository: str,
    branch: str,
    sha: str,
) -> list[dict[str, object]]:
    for _ in range(10):
        output = capture(
            [
                "gh",
                "run",
                "list",
                "--repo",
                repository,
                "--branch",
                branch,
                "--event",
                "push",
                "--limit",
                "50",
                "--json",
                "databaseId,headSha,status,conclusion,url,workflowName,headBranch",
            ]
        )
        try:
            runs = json.loads(output)
        except json.JSONDecodeError as error:
            raise PushCliError("gh run list returned invalid JSON") from error
        matches = [
            item
            for item in runs
            if item.get("headSha") == sha and item.get("headBranch") == branch
        ]
        if matches:
            return matches
        time.sleep(3)
    raise PushCliError(
        f"no GitHub Actions push run for {branch} at {sha} appeared within 30 seconds"
    )


def watch_actions(repository: str, branch: str) -> None:
    sha = pushed_sha()
    runs = find_actions_runs(repository, branch, sha)
    for run in runs:
        run_id = str(run["databaseId"])
        print(
            f"GitHub Actions run: {run_id} "
            f"({run.get('workflowName', 'unknown')}) {run.get('url', '')}"
        )
        result = run_command(
            ["gh", "run", "watch", run_id, "--repo", repository, "--exit-status"]
        )
        if result.returncode != 0:
            raise PushCliError(
                f"GitHub Actions run failed: {run.get('url', '') or run_id}"
            )
    print(f"All {len(runs)} GitHub Actions runs passed.")


def push_branch(remote: str, branch: str) -> None:
    run_step("Configure Git transport from GitHub CLI", ["gh", "auth", "setup-git"])
    run_step(
        "Push current branch",
        ["git", "push", remote, f"HEAD:{branch}"],
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run strict Sub2API Plus checks and push the current branch."
    )
    parser.add_argument(
        "action",
        choices=("check", "push", "watch"),
        help="check locally, push after checking, or watch the current branch run",
    )
    parser.add_argument(
        "--repo-root",
        type=Path,
        default=ROOT,
        help=argparse.SUPPRESS,
    )
    parser.add_argument("--remote", default=DEFAULT_REMOTE)
    return parser.parse_args()


def main() -> int:
    global ROOT
    args = parse_args()
    ROOT = args.repo_root.resolve()
    try:
        repository = github_gate(args.remote)
        branch = current_branch()
        require_clean_worktree()
        if args.action == "watch":
            watch_actions(repository, branch)
            return 0

        check_toolchains()
        runtime = probe_runtime()
        run_local_checks(args.remote, branch, runtime)
        ensure_clean_after_checks()
        print("\nLocal push checks passed. No branch was pushed.")

        if args.action == "push":
            push_branch(args.remote, branch)
            watch_actions(repository, branch)
        return 0
    except PushCliError as error:
        print(f"push-cli stopped: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
