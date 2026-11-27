#!/usr/bin/env python3
"""Validate and safely push a Sub2API Plus branch."""

from __future__ import annotations

import argparse
import concurrent.futures
from contextlib import ExitStack, nullcontext
import json
import platform
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


ROOT = Path(__file__).resolve().parents[3]
TOOLS = ROOT / "tools"
if str(TOOLS) not in sys.path:
    sys.path.insert(0, str(TOOLS))
import validation_runtime
import release_validation
import validation_checks
import validation_output

DEFAULT_REMOTE = "origin"
EXPECTED_REPOSITORY = "LuckyKuang/sub2api-plus"
LOCAL_VALIDATION_CONTEXT = "sub2api/local-validation"
FULL_PROFILE = "full"
FINALIZATION_PROFILE = "release-finalization"
VALIDATION_PROFILES = (FULL_PROFILE, FINALIZATION_PROFILE)
VALIDATION_DESCRIPTIONS = {
    FULL_PROFILE: "Platform-container validation passed",
    FINALIZATION_PROFILE: "Deterministic release finalization passed",
}
VALIDATION_MARKER_RE = re.compile(
    r"<!--\s*sub2api-submit-pr:\s*(\{.*?\})\s*-->", re.DOTALL
)
GO_VERSION_RE = re.compile(r"^go\s+(\d+\.\d+(?:\.\d+)?)\s*$", re.MULTILINE)
VERSION_RE = re.compile(
    r"\bversion(?:\s*[:=]|\s+)(?:v)?(\d+\.\d+\.\d+)",
    re.IGNORECASE,
)
SCRIPT = Path(__file__).resolve()
OUTPUT_LOCK = threading.Lock()


class PushCliError(RuntimeError):
    """A hard failure that must stop validation or pushing."""


Runtime = validation_runtime.Runtime


@dataclass(frozen=True)
class DeclaredToolchains:
    go: str
    pnpm: str
    node_major_minimum: int
    golangci_lint: str


@dataclass(frozen=True)
class ValidationProof:
    base: str
    head: str
    profile: str = FULL_PROFILE
    tag: str | None = None


ValidationStep = validation_checks.ValidationStep


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
        result = subprocess.run(
            [str(item) for item in command],
            cwd=actual_cwd,
            check=False,
            stdout=subprocess.PIPE if capture else None,
            stderr=(subprocess.STDOUT if merge_stderr else subprocess.PIPE)
            if capture
            else None,
        )
        return subprocess.CompletedProcess(result.args, result.returncode,
                                           validation_runtime.decode_output(result.stdout),
                                           validation_runtime.decode_output(result.stderr))
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


def repository_default_branch(repository: str) -> str:
    branch = capture(
        ["gh", "api", f"repos/{repository}", "--jq", ".default_branch"]
    )
    if not branch or any(char.isspace() for char in branch):
        raise PushCliError("GitHub returned an invalid repository default branch")
    print(f"Default branch: {branch}")
    return branch


def current_branch() -> str:
    branch = capture(["git", "branch", "--show-current"])
    if not branch or branch == "HEAD":
        raise PushCliError("detached HEAD is not pushable; check out a branch first")
    if any(char.isspace() for char in branch):
        raise PushCliError(f"invalid current branch name: {branch}")
    print(f"Current branch: {branch}")
    return branch


def require_working_branch(branch: str, default_branch: str) -> None:
    if branch == default_branch:
        raise PushCliError(
            f"refusing to push repository default branch {default_branch!r}; "
            "create or switch to a working branch first"
        )


def require_no_git_operation() -> None:
    git_dir = Path(capture(["git", "rev-parse", "--git-dir"]))
    if not git_dir.is_absolute():
        git_dir = ROOT / git_dir
    active = [
        name
        for name in (
            "MERGE_HEAD",
            "CHERRY_PICK_HEAD",
            "REVERT_HEAD",
            "rebase-merge",
            "rebase-apply",
        )
        if (git_dir / name).exists()
    ]
    if active:
        raise PushCliError(
            "an unfinished Git operation is present: " + ", ".join(active)
        )


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


def declared_toolchains() -> DeclaredToolchains:
    package = json.loads((ROOT / "frontend/package.json").read_text(encoding="utf-8"))
    package_manager = package.get("packageManager", "")
    pnpm_match = re.fullmatch(r"pnpm@(.+)", package_manager)
    if not pnpm_match:
        raise PushCliError("frontend/package.json must declare packageManager as pnpm@VERSION")

    go_mod = (ROOT / "backend/go.mod").read_text(encoding="utf-8")
    go_match = GO_VERSION_RE.search(go_mod)
    if not go_match:
        raise PushCliError("backend/go.mod does not declare a Go version")

    node_minimum = re.search(
        r">=\s*(\d+)(?:\.(\d+))?", package.get("engines", {}).get("node", "")
    )
    if not node_minimum:
        raise PushCliError("unable to determine the Node.js minimum version")

    return DeclaredToolchains(
        go=go_match.group(1),
        pnpm=pnpm_match.group(1),
        node_major_minimum=int(node_minimum.group(1)),
        golangci_lint=declared_tool_version("golangci-lint"),
    )


def current_go_version() -> str:
    return capture(["go", "env", "GOVERSION"])


def current_pnpm_version() -> str:
    return capture(["pnpm", "--version"])


def current_node_version() -> str:
    return capture(["node", "--version"])


def current_golangci_lint_version() -> str:
    lint_output = capture(["golangci-lint", "version"])
    lint_match = VERSION_RE.search(lint_output)
    if not lint_match:
        raise PushCliError(f"unable to parse golangci-lint version from {lint_output!r}")
    return lint_match.group(1)


def node_major(version: str) -> int:
    match = re.search(r"(\d+)", version)
    if not match:
        raise PushCliError(f"unable to parse Node.js version from {version!r}")
    return int(match.group(1))


def check_toolchains() -> None:
    declared = declared_toolchains()
    go_actual = current_go_version()
    if go_actual != f"go{declared.go}":
        raise PushCliError(f"Go {declared.go} is required; found {go_actual}")

    pnpm_actual = current_pnpm_version()
    if pnpm_actual != declared.pnpm:
        raise PushCliError(f"pnpm {declared.pnpm} is required; found {pnpm_actual}")

    node_actual = current_node_version()
    if node_major(node_actual) < declared.node_major_minimum:
        raise PushCliError(
            f"Node.js {declared.node_major_minimum}+ is required; found {node_actual}"
        )

    lint_actual = current_golangci_lint_version()
    if lint_actual != declared.golangci_lint:
        raise PushCliError(
            f"golangci-lint {declared.golangci_lint} is required; found {lint_actual}"
        )
    print(
        "Toolchains: "
        f"Go {go_actual}; pnpm {pnpm_actual}; Node.js {node_actual}; "
        f"golangci-lint {lint_actual}"
    )


def probe_docker(prefix: Sequence[str] = ()) -> tuple[bool, str]:
    return validation_runtime.probe_docker(prefix, optional_capture=optional_capture)


def normalize_wsl_list_output(output: str) -> str:
    return validation_runtime.normalize_wsl_list_output(output)


def parse_wsl_distributions(output: str) -> list[tuple[str, str]]:
    return validation_runtime.parse_wsl_distributions(output)


def is_debian_or_ubuntu_wsl(name: str) -> bool:
    return validation_runtime.is_debian_or_ubuntu_wsl(name)


def probe_runtime() -> Runtime:
    try:
        return validation_runtime.probe_runtime(
            root=ROOT,
            capture=capture,
            optional_capture=optional_capture,
            probe_docker_fn=probe_docker,
            which=shutil.which,
            system_name=platform.system(),
        )
    except validation_runtime.ValidationRuntimeError as error:
        raise PushCliError(str(error)) from error


def ensure_validation_image(runtime: Runtime) -> str:
    try:
        return validation_runtime.ensure_validation_image(
            runtime,
            root=ROOT,
            optional_capture=optional_capture,
            run_step=lambda name, command: run_step(name, command),
        )
    except validation_runtime.ValidationRuntimeError as error:
        raise PushCliError(str(error)) from error


def launch_in_validation(
    runtime: Runtime,
    remote: str,
    *,
    base_ref: str | None = None,
    serial: bool = False,
    exact_head: str | None = None,
    exact_base: str | None = None,
    source_mode: str = "bind",
) -> None:
    repo = validation_runtime.mount_root(runtime, ROOT)
    script = validation_runtime.container_path(SCRIPT, runtime, ROOT)
    argv = [
        "python3",
        script,
        "check",
        "--in-validation",
        "--remote",
        remote,
        "--repo-root",
        repo,
    ]
    if base_ref:
        argv.extend(["--base-ref", base_ref])
    if serial:
        argv.append("--serial")
    if runtime.name == "wsl2-docker" and source_mode == "linux":
        if not exact_head or not exact_base:
            raise PushCliError("Linux validation workspace requires exact base/head inputs")
        argv = ["python3", f"{repo}/tools/validation_workspace.py", "run",
                "--source", repo, "--head", exact_head, "--base", exact_base, "--", *argv]
    try:
        validation_runtime.launch_in_validation(
            runtime,
            argv,
            root=ROOT,
            capture=capture,
            run_step=lambda name, command: run_step(name, command),
            reports=exact_head is not None,
        )
    except validation_runtime.ValidationRuntimeError as error:
        raise PushCliError(str(error)) from error


def run_step(
    name: str,
    command: Sequence[str],
    cwd: Path | None = None,
    *,
    lane: str | None = None,
    capture_output: bool = False,
    queued_seconds: float = 0,
) -> None:
    label = f"{lane}: {name}" if lane else name
    with OUTPUT_LOCK:
        print(f"\n[{label}]")
        print(f"$ {display(command)}")
    started = time.monotonic()
    streamed = capture_output
    if streamed:
        try:
            result = validation_output.stream(command, cwd=cwd or ROOT, lock=OUTPUT_LOCK, label=label)
        except FileNotFoundError as error:
            raise PushCliError(f"required command is unavailable: {error.filename}") from error
    else:
        result = run_command(command, cwd=cwd, capture=False)
    elapsed = time.monotonic() - started
    if validation_output.ACTIVE_REPORT:
        validation_output.ACTIVE_REPORT.step(name, lane, elapsed, result.returncode, queued_seconds)
    with OUTPUT_LOCK:
        outcome = "completed" if result.returncode == 0 else "failed"
        print(f"[{label}] {outcome} in {elapsed:.1f}s")
    if result.returncode != 0:
        raise PushCliError(f"{name} failed with exit code {result.returncode}")


def run_frontend_security_check(*, lane: str | None = None) -> None:
    command = ["pnpm", "audit", "--prod", "--audit-level=high", "--json"]
    label = f"{lane}: Frontend production audit" if lane else "Frontend production audit"
    with OUTPUT_LOCK:
        print(f"\n[{label}]")
        print(f"$ {display(command)}")
    started = time.monotonic()
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
        exception_command = [
            sys.executable,
            "tools/check_pnpm_audit_exceptions.py",
            "--audit",
            str(audit_path),
            "--exceptions",
            ".github/audit-exceptions.yml",
        ]
        if lane is None:
            run_step("Frontend audit exceptions", exception_command, ROOT)
        else:
            run_step(
                "Frontend audit exceptions",
                exception_command,
                ROOT,
                lane=lane,
                capture_output=True,
            )
    finally:
        audit_path.unlink(missing_ok=True)
    with OUTPUT_LOCK:
        print(f"[{label}] completed in {time.monotonic() - started:.1f}s")


def run_local_checks(
    remote: str,
    branch: str,
    runtime: Runtime,
    *,
    base_ref: str | None = None,
    serial: bool = False,
) -> None:
    python = sys.executable
    frontend_workers = 4 if serial else 2
    migration_base = base_ref or f"{remote}/{branch}"
    base_check = run_command(
        ["git", "rev-parse", "--verify", migration_base], capture=True
    )
    steps = validation_checks.full_steps(
        ROOT, python=python, frontend_workers=frontend_workers,
        migration_base=migration_base if base_check.returncode == 0 else None,
    )

    started = time.monotonic()
    if serial:
        for step in steps:
            run_step(step.name, step.command, step.cwd)
        run_frontend_security_check()
    else:
        # Go compilation and whole-program lint each consume several GiB. Run
        # those lanes in order within the 8-GiB VM; frontend remains concurrent.
        lane_groups = (("backend-tests", "backend-lint-policy"), ("frontend",))
        stop_requested = threading.Event()
        memory_slot = threading.Lock()
        memory_steps = {"Backend unit tests", "Backend integration tests", "Backend lint",
                        "Seven-provider required unit regressions",
                        "Seven-provider required integration regressions",
                        "Frontend production build"}

        def run_lane(lane_name: str) -> None:
            lane_steps = [step for step in steps if step.lane == lane_name]
            lane_started = time.monotonic()
            for step in lane_steps:
                if stop_requested.is_set():
                    with OUTPUT_LOCK:
                        print(f"\n[Lane {lane_name}] stopped before {step.name}")
                    return
                try:
                    queued_at = time.monotonic()
                    with memory_slot if step.name in memory_steps else nullcontext():
                        if stop_requested.is_set():
                            return
                        run_step(
                            step.name,
                            step.command,
                            step.cwd,
                            lane=lane_name,
                            capture_output=True,
                            queued_seconds=time.monotonic() - queued_at,
                        )
                except Exception:
                    stop_requested.set()
                    raise
            if lane_name == "frontend" and not stop_requested.is_set():
                try:
                    run_frontend_security_check(lane=lane_name)
                except Exception:
                    stop_requested.set()
                    raise
            with OUTPUT_LOCK:
                print(
                    f"\n[Lane {lane_name}] completed in "
                    f"{time.monotonic() - lane_started:.1f}s"
                )

        failures: list[str] = []
        def run_group(lanes: tuple[str, ...]) -> None:
            for lane_name in lanes:
                run_lane(lane_name)

        with concurrent.futures.ThreadPoolExecutor(
            max_workers=len(lane_groups),
            thread_name_prefix="validation",
        ) as executor:
            futures = {
                executor.submit(run_group, lanes): "/".join(lanes)
                for lanes in lane_groups
            }
            for future in concurrent.futures.as_completed(futures):
                lane_name = futures[future]
                try:
                    future.result()
                except Exception as error:
                    failures.append(f"{lane_name}: {error}")
        if failures:
            raise PushCliError(
                "local validation lane failure(s):\n" + "\n".join(failures)
            )

    mode = "serial" if serial else "parallel"
    print(
        f"\nFull local matrix ({mode}) completed in "
        f"{time.monotonic() - started:.1f}s"
    )


def ensure_clean_after_checks() -> None:
    status = capture(
        ["git", "status", "--porcelain=v1", "--untracked-files=all"]
    )
    if status:
        raise PushCliError(
            "checks created or exposed worktree changes; refusing to push:\n" + status
        )


def run_release_finalization_checks(
    proof: ValidationProof,
    branch: str,
    remote: str,
) -> None:
    if proof.profile != FINALIZATION_PROFILE or proof.tag is None:
        raise PushCliError("release-finalization checks require a typed tag proof")
    result = release_validation.run(
        [
            sys.executable,
            "tools/release_finalization.py",
            "validate",
            "--base",
            proof.base,
            "--head",
            proof.head,
            "--tag",
            proof.tag,
            "--branch",
            branch,
        ],
        root=ROOT,
    )
    if result.stdout:
        print(result.stdout)
    if result.returncode:
        raise PushCliError("deterministic release finalization container validation failed")
    run_step(
        "Verify published release",
        [
            sys.executable,
            "skills/release-cli/scripts/release_cli.py",
            "verify",
            "--tag",
            proof.tag,
            "--remote",
            remote,
        ],
    )


def pushed_sha() -> str:
    return capture(["git", "rev-parse", "HEAD"])


def fetch_default_branch(remote: str, default_branch: str) -> str:
    run_step(
        f"Fetch {remote}/{default_branch}",
        [
            "git",
            "fetch",
            "--no-tags",
            remote,
            f"+refs/heads/{default_branch}:refs/remotes/{remote}/{default_branch}",
        ],
    )
    return capture(["git", "rev-parse", f"{remote}/{default_branch}"])


def require_latest_base(
    remote: str,
    default_branch: str,
    *,
    profile: str = FULL_PROFILE,
    tag: str | None = None,
) -> ValidationProof:
    base = fetch_default_branch(remote, default_branch)
    head = pushed_sha()
    ancestor = run_command(
        ["git", "merge-base", "--is-ancestor", base, head], capture=True
    )
    if ancestor.returncode == 1:
        raise PushCliError(
            f"current branch does not contain latest {remote}/{default_branch} "
            f"({base}); update the branch before submit-pr"
        )
    if ancestor.returncode != 0:
        raise PushCliError("unable to compare the pull-request base and head")
    return ValidationProof(base=base, head=head, profile=profile, tag=tag)


def require_unchanged_proof(
    proof: ValidationProof,
    remote: str,
    default_branch: str,
) -> None:
    if pushed_sha() != proof.head:
        raise PushCliError("HEAD changed while local validation was running")
    latest_base = fetch_default_branch(remote, default_branch)
    if latest_base != proof.base:
        raise PushCliError(
            f"{remote}/{default_branch} changed from {proof.base} to {latest_base} "
            "while local validation was running; rerun submit-pr"
        )


def validation_marker(proof: ValidationProof) -> str:
    data: dict[str, str] = {
        "base": proof.base,
        "head": proof.head,
        "profile": proof.profile,
    }
    if proof.tag is not None:
        data["tag"] = proof.tag
    payload = json.dumps(data, separators=(",", ":"), sort_keys=True)
    return f"<!-- sub2api-submit-pr: {payload} -->"


def with_validation_marker(body: str, proof: ValidationProof) -> str:
    cleaned = VALIDATION_MARKER_RE.sub("", body).rstrip()
    marker = validation_marker(proof)
    return f"{cleaned}\n\n{marker}\n" if cleaned else f"{marker}\n"


def publish_validation_status(repository: str, proof: ValidationProof) -> None:
    run_step(
        "Publish local-validation commit status",
        [
            "gh",
            "api",
            "--method",
            "POST",
            f"repos/{repository}/statuses/{proof.head}",
            "-f",
            "state=success",
            "-f",
            f"context={LOCAL_VALIDATION_CONTEXT}",
            "-f",
            f"description={VALIDATION_DESCRIPTIONS[proof.profile]}",
        ],
    )


def open_pull_requests(
    repository: str,
    branch: str,
    default_branch: str,
) -> list[dict[str, object]]:
    output = capture(
        [
            "gh",
            "pr",
            "list",
            "--repo",
            repository,
            "--state",
            "open",
            "--head",
            branch,
            "--base",
            default_branch,
            "--json",
            "number,url,isDraft,headRefOid,body",
        ]
    )
    try:
        prs = json.loads(output)
    except json.JSONDecodeError as error:
        raise PushCliError("gh pr list returned invalid JSON") from error
    if not isinstance(prs, list) or any(not isinstance(pr, dict) for pr in prs):
        raise PushCliError("gh pr list returned an unexpected JSON value")
    for pr in prs:
        number = pr.get("number")
        if not isinstance(number, int):
            raise PushCliError("gh pr list returned a pull request without a number")
        base_oid = capture(
            [
                "gh",
                "api",
                f"repos/{repository}/pulls/{number}",
                "--jq",
                ".base.sha",
            ]
        )
        if not re.fullmatch(r"[0-9a-f]{40}", base_oid):
            raise PushCliError(
                f"GitHub returned an invalid base commit for pull request #{number}"
            )
        pr["baseRefOid"] = base_oid
    return prs


def default_pr_body(branch: str, proof: ValidationProof) -> str:
    validation = (
        "Platform-container validation matrix passed."
        if proof.profile == FULL_PROFILE
        else f"Deterministic finalization for `{proof.tag}` passed."
    )
    return (
        "## Summary\n\n"
        f"Submit `{branch}` after the repository local-validation gate.\n\n"
        "## Validation\n\n"
        f"- {validation}\n"
    )


def verify_pull_request_proof(repository: str, url: str, proof: ValidationProof) -> None:
    match = re.fullmatch(r"https://github\.com/" + re.escape(repository) + r"/pull/(\d+)", url)
    if not match:
        raise PushCliError("GitHub did not return a pull request in the expected repository")
    try:
        pr = json.loads(capture(["gh", "api", f"repos/{repository}/pulls/{match.group(1)}"]))
        markers = VALIDATION_MARKER_RE.findall(str(pr.get("body") or ""))
        if (pr.get("head", {}).get("sha") != proof.head
                or pr.get("base", {}).get("sha") != proof.base
                or len(markers) != 1
                or json.loads(markers[0]) != json.loads(VALIDATION_MARKER_RE.search(validation_marker(proof)).group(1))
                or (pr.get("state") != "open" and not pr.get("merged"))):
            raise PushCliError("pull-request proof changed before publishing local success")
    except (ValueError, AttributeError, TypeError) as error:
        raise PushCliError("GitHub returned an invalid pull-request proof") from error


def create_or_update_pull_request(
    repository: str,
    branch: str,
    default_branch: str,
    proof: ValidationProof,
    *,
    title: str | None,
    body_file: Path | None,
) -> str:
    prs = open_pull_requests(repository, branch, default_branch)
    if len(prs) > 1:
        raise PushCliError(
            f"multiple open pull requests exist for {branch} -> {default_branch}"
        )
    supplied_body = default_pr_body(branch, proof)
    if body_file is not None:
        try:
            supplied_body = body_file.read_text(encoding="utf-8")
        except OSError as error:
            raise PushCliError(f"cannot read pull-request body {body_file}: {error}") from error
    if prs:
        pr = prs[0]
        if pr.get("headRefOid") != proof.head:
            raise PushCliError("existing pull request head does not match validated HEAD")
        if pr.get("baseRefOid") != proof.base:
            raise PushCliError(
                "existing pull request base does not match the validated default branch"
            )
        body = with_validation_marker(str(pr.get("body") or supplied_body), proof)
        number = str(pr.get("number"))
        with tempfile.TemporaryDirectory(prefix="sub2api-pr-") as directory:
            payload = Path(directory) / "payload.json"
            payload.write_text(json.dumps({"body": body}), encoding="utf-8")
            run_step("Update pull-request validation marker", [
                "gh", "api", "--method", "PATCH", f"repos/{repository}/pulls/{number}",
                "--silent", "--input", str(payload),
            ])
        url = str(pr.get("url") or f"https://github.com/{repository}/pull/{number}")
        verify_pull_request_proof(repository, url, proof)
        print(f"Pull-request proof verified: {url}")
        return url

    pr_title = title or capture(["git", "log", "-1", "--format=%s"])
    body = with_validation_marker(supplied_body, proof)
    with tempfile.TemporaryDirectory(prefix="sub2api-pr-") as directory:
        body_path = Path(directory) / "body.md"
        body_path.write_text(body, encoding="utf-8")
        url = capture([
            "gh",
            "pr",
            "create",
            "--repo",
            repository,
            "--base",
            default_branch,
            "--head",
            branch,
            "--title",
            pr_title,
            "--body-file",
            str(body_path),
        ])
    if not url:
        raise PushCliError("gh pr create did not return a pull-request URL")
    verify_pull_request_proof(repository, url, proof)
    print(f"Pull request created: {url}")
    return url


def actions_watch_event(repository: str, branch: str, default_branch: str) -> str:
    output = capture(
        [
            "gh",
            "pr",
            "list",
            "--repo",
            repository,
            "--state",
            "open",
            "--head",
            branch,
            "--base",
            default_branch,
            "--json",
            "number",
        ]
    )
    try:
        prs = json.loads(output)
    except json.JSONDecodeError as error:
        raise PushCliError("gh pr list returned invalid JSON") from error
    if not isinstance(prs, list):
        raise PushCliError("gh pr list returned an unexpected JSON value")
    if prs:
        return "pull_request"
    return "push"


def find_actions_runs(
    repository: str,
    branch: str,
    sha: str,
    event: str,
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
                event,
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
        f"no GitHub Actions {event} run for {branch} at {sha} appeared within 30 seconds"
    )


def watch_actions(repository: str, branch: str) -> None:
    sha = pushed_sha()
    event = actions_watch_event(
        repository,
        branch,
        repository_default_branch(repository),
    )
    runs = find_actions_runs(repository, branch, sha, event)
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
        choices=("check", "push", "submit-pr", "watch", "ensure", "clean"),
        help="prepare the runtime, check locally, push quickly, submit a validated PR, watch branch Actions, or manually purge validation resources in the selected runtime/profile",
    )
    parser.add_argument(
        "--in-validation",
        action="store_true",
        help=argparse.SUPPRESS,
    )
    parser.add_argument(
        "--repo-root",
        type=Path,
        default=ROOT,
        help=argparse.SUPPRESS,
    )
    parser.add_argument("--remote", default=DEFAULT_REMOTE)
    parser.add_argument("--base-ref", help=argparse.SUPPRESS)
    parser.add_argument("--title", help="pull-request title for submit-pr")
    parser.add_argument("--body-file", type=Path, help="pull-request body for submit-pr")
    parser.add_argument(
        "--profile",
        choices=VALIDATION_PROFILES,
        default=FULL_PROFILE,
        help="validation profile for submit-pr",
    )
    parser.add_argument(
        "--tag",
        help="published custom tag required by the release-finalization profile",
    )
    parser.add_argument(
        "--serial",
        action="store_true",
        help="run the full validation matrix serially for diagnostics or benchmarking",
    )
    parser.add_argument(
        "--yes",
        action="store_true",
        help="confirm deleting Sub2API validation images, caches, and containers in the selected runtime/profile (only for clean)",
    )
    parser.add_argument("--wsl-workspace", choices=("linux", "bind"), default="linux",
                        help="WSL full validation source storage; bind is the diagnostic comparison mode")
    return parser.parse_args()


def main() -> int:
    global ROOT
    validation_output.configure_console()
    args = parse_args()
    ROOT = args.repo_root.resolve()
    sessions = ExitStack()
    try:
        if args.in_validation:
            if validation_runtime.host_os_forbids_in_validation():
                raise PushCliError(
                    "check --in-validation cannot run on the Darwin/Windows host. "
                    "Host validation fallback is forbidden"
                )
            if not validation_runtime.in_validation_container():
                raise PushCliError(
                    "check --in-validation is only valid inside the platform "
                    "validation container. Host validation fallback is forbidden"
                )
            if args.action != "check":
                raise PushCliError("in-container execution only supports check")
            branch = current_branch()
            check_toolchains()
            with validation_output.report_scope(ROOT, base=args.base_ref):
                run_local_checks(
                    args.remote,
                    branch,
                    Runtime("in-validation"),
                    base_ref=args.base_ref,
                    serial=args.serial,
                )
            print("\nIn-container push checks passed.")
            return 0

        if args.action == "clean" and not args.yes:
            print(
                "push-cli clean 需要 --yes 确认;该操作会删除所选运行时/profile 的 "
                "Sub2API 验证镜像、缓存与容器。",
                file=sys.stderr,
            )
            return 1

        repository = github_gate(args.remote)
        default_branch = repository_default_branch(repository)
        if args.action == "clean":
            runtime = probe_runtime()
            sessions.enter_context(validation_runtime.runtime_session(runtime))
            validation_runtime.purge_validation_resources(
                runtime,
                root=ROOT,
                capture=capture,
                run_step=run_step,
            )
            print("\n已删除所选运行时/profile 的 Sub2API 验证镜像、缓存与容器。")
            return 0

        if args.action != "submit-pr" and (
            args.profile != FULL_PROFILE or args.tag is not None
        ):
            raise PushCliError("--profile and --tag are valid only for submit-pr")
        if args.serial and args.action not in {"check", "submit-pr"}:
            raise PushCliError("--serial is valid only for check or submit-pr")
        if args.profile == FULL_PROFILE and args.tag is not None:
            raise PushCliError("the full validation profile does not accept --tag")
        if args.profile == FINALIZATION_PROFILE and args.tag is None:
            raise PushCliError("release-finalization submit-pr requires --tag")
        if args.profile == FINALIZATION_PROFILE and args.serial:
            raise PushCliError("release-finalization does not run the full serial matrix")
        if args.action == "ensure":
            runtime = probe_runtime()
            sessions.enter_context(validation_runtime.runtime_session(runtime))
            validation_runtime.cleanup_validation_runtime(
                runtime,
                root=ROOT,
                capture=capture,
                run_step=run_step,
            )
            ensure_validation_image(runtime)
            print("\nValidation runtime and image are ready. No checks were run.")
            return 0

        branch = current_branch()
        if args.action == "watch":
            watch_actions(repository, branch)
            return 0

        if args.action in {"push", "submit-pr"}:
            require_working_branch(branch, default_branch)
            require_no_git_operation()
            require_clean_worktree()

        if args.action == "push":
            push_branch(args.remote, branch)
            print(
                f"\nPushed {branch} without local validation. "
                "Use submit-pr for the final validated pull-request submission."
            )
            return 0

        proof = None
        base_ref = args.base_ref
        if args.action == "submit-pr":
            proof = require_latest_base(
                args.remote,
                default_branch,
                profile=args.profile,
                tag=args.tag,
            )
            base_ref = f"{args.remote}/{default_branch}"
        else:
            require_clean_worktree()
        if proof is not None and proof.profile == FINALIZATION_PROFILE:
            run_release_finalization_checks(proof, branch, args.remote)
        else:
            runtime = probe_runtime()
            sessions.enter_context(validation_runtime.runtime_session(runtime))
            validation_runtime.cleanup_validation_runtime(
                runtime,
                root=ROOT,
                capture=capture,
                run_step=run_step,
            )
            ensure_validation_image(runtime)
            launch_in_validation(
                runtime,
                args.remote,
                base_ref=proof.base if proof else capture(["git", "rev-parse", base_ref or f"{args.remote}/{default_branch}"]),
                serial=args.serial,
                exact_head=proof.head if proof else capture(["git", "rev-parse", "HEAD"]),
                exact_base=proof.base if proof else capture(["git", "rev-parse", base_ref or f"{args.remote}/{default_branch}"]),
                source_mode=args.wsl_workspace,
            )
        ensure_clean_after_checks()
        profile = proof.profile if proof is not None else FULL_PROFILE
        print(f"\nLocal {profile} checks passed. No branch was pushed.")

        if args.action == "submit-pr":
            assert proof is not None
            require_unchanged_proof(proof, args.remote, default_branch)
            push_branch(args.remote, branch)
            create_or_update_pull_request(
                repository,
                branch,
                default_branch,
                proof,
                title=args.title,
                body_file=args.body_file,
            )
            publish_validation_status(repository, proof)
        return 0
    except (PushCliError, validation_runtime.ValidationRuntimeError) as error:
        print(f"push-cli stopped: {error}", file=sys.stderr)
        return 1
    finally:
        sessions.close()


if __name__ == "__main__":
    raise SystemExit(main())
