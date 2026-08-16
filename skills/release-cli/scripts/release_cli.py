#!/usr/bin/env python3
"""Safely prepare and publish immutable Sub2API Plus GitHub releases."""

from __future__ import annotations

import argparse
import json
import re
import shlex
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


ROOT = Path(__file__).resolve().parents[3]
TOOLS = ROOT / "tools"
if str(TOOLS) not in sys.path:
    sys.path.insert(0, str(TOOLS))
import validation_runtime

DEFAULT_REMOTE = "origin"
EXPECTED_REPOSITORY = "LuckyKuang/sub2api-plus"
TAG_RE = re.compile(r"^v\d+\.\d+\.\d+\+custom\.(?:00[1-9]|0[1-9]\d|[1-9]\d{2})$")
EXPECTED_SUBJECT_PREFIX = "Sub2API Plus "
POLL_SECONDS = 5
DISCOVERY_ATTEMPTS = 12
WATCH_SECONDS = 10
PRICING_ASSETS = frozenset(
    {"model-pricing.json", "model-pricing-manifest.json"}
)


class ReleaseCliError(RuntimeError):
    """A release guard failed and the command must stop."""


class ApprovalRequired(ReleaseCliError):
    """A maintainer must complete the protected-environment approval."""


@dataclass(frozen=True)
class WorkflowRun:
    database_id: int
    url: str
    status: str
    conclusion: str | None


def display(command: Sequence[str]) -> str:
    return shlex.join(str(item) for item in command)


def run_command(
    command: Sequence[str],
    *,
    cwd: Path | None = None,
    capture: bool = False,
    timeout: int | None = None,
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
            stderr=subprocess.STDOUT if capture else None,
            timeout=timeout,
        )
    except FileNotFoundError as error:
        raise ReleaseCliError(
            f"required command is unavailable: {error.filename}"
        ) from error


def capture(command: Sequence[str], *, cwd: Path | None = None) -> str:
    result = run_command(command, cwd=cwd, capture=True)
    if result.returncode != 0:
        detail = (result.stdout or "").strip()
        raise ReleaseCliError(
            f"{display(command)} failed with exit code {result.returncode}"
            + (f": {detail[-2000:]}" if detail else "")
        )
    return (result.stdout or "").strip()


def require_command(command: str) -> None:
    if shutil.which(command) is None:
        raise ReleaseCliError(f"required command is unavailable: {command}")


def repo_from_remote(url: str) -> str:
    match = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$", url.strip())
    if not match:
        raise ReleaseCliError(f"origin is not a GitHub repository URL: {url}")
    return f"{match.group(1)}/{match.group(2)}"


def github_gate(remote: str) -> str:
    require_command("gh")
    version = capture(["gh", "--version"]).splitlines()[0]
    print(f"GitHub CLI: {version}")

    auth = run_command(
        ["gh", "auth", "status", "--hostname", "github.com"], capture=True
    )
    if auth.returncode != 0:
        detail = (auth.stdout or "").strip()
        raise ReleaseCliError(
            "GitHub CLI is missing a valid github.com login. Run gh auth login "
            "and retry."
            + (f"\n{detail[-2000:]}" if detail else "")
        )

    remote_url = capture(["git", "remote", "get-url", remote])
    repository = repo_from_remote(remote_url)
    if repository != EXPECTED_REPOSITORY:
        raise ReleaseCliError(
            f"origin resolves to {repository}; expected {EXPECTED_REPOSITORY}"
        )

    capture(["gh", "repo", "view", repository, "--json", "nameWithOwner"])
    push_permission = capture(
        ["gh", "api", f"repos/{repository}", "--jq", ".permissions.push"]
    ).lower()
    if push_permission != "true":
        raise ReleaseCliError(
            f"authenticated GitHub account has no push permission for {repository}"
        )
    print(f"GitHub repository: {repository} (push permission confirmed)")
    return repository


def validate_tag(tag: str) -> None:
    if not TAG_RE.fullmatch(tag):
        raise ReleaseCliError(
            "tag must match vX.Y.Z+custom.NNN with NNN from 001 through 999"
        )


def require_clean_worktree(*, allow_untracked: bool = False) -> None:
    status = capture(["git", "status", "--porcelain=v1", "--untracked-files=all"])
    entries = status.splitlines() if status else []
    dirty = [entry for entry in entries if not (allow_untracked and entry.startswith("?? "))]
    if dirty:
        raise ReleaseCliError(
            "release action requires no tracked worktree changes; commit or remove:\n"
            + "\n".join(dirty)
        )
    diff_check = run_command(["git", "diff", "--check"], capture=True)
    if diff_check.returncode != 0:
        raise ReleaseCliError(
            "git diff --check failed:\n" + (diff_check.stdout or "").strip()
        )


def current_head() -> str:
    return capture(["git", "rev-parse", "HEAD"])


def setup_git_transport() -> None:
    run_step("Configure Git transport from GitHub CLI", ["gh", "auth", "setup-git"])


def run_step(
    name: str,
    command: Sequence[str],
    *,
    cwd: Path | None = None,
) -> None:
    print(f"\n[{name}]")
    print(f"$ {display(command)}")
    result = run_command(command, cwd=cwd)
    if result.returncode != 0:
        raise ReleaseCliError(f"{name} failed with exit code {result.returncode}")


def local_tag_details(tag: str, *, required: bool) -> dict[str, str] | None:
    reference = f"refs/tags/{tag}"
    exists = run_command(["git", "show-ref", "--verify", "--quiet", reference])
    if exists.returncode == 1:
        if required:
            raise ReleaseCliError(f"local tag does not exist: {tag}")
        return None
    if exists.returncode != 0:
        raise ReleaseCliError(f"unable to inspect local tag: {tag}")

    object_type = capture(["git", "cat-file", "-t", reference])
    subject = capture(
        ["git", "for-each-ref", "--format=%(contents:subject)", reference]
    )
    target = capture(["git", "rev-list", "-n", "1", tag])
    return {"object_type": object_type, "subject": subject, "target": target}


def require_publishable_local_tag(tag: str) -> str:
    details = local_tag_details(tag, required=True)
    assert details is not None
    if details["object_type"] != "tag":
        raise ReleaseCliError(f"{tag} is not an annotated tag")
    expected_subject = f"{EXPECTED_SUBJECT_PREFIX}{tag}"
    if details["subject"] != expected_subject:
        raise ReleaseCliError(
            f"{tag} subject is {details['subject']!r}; expected {expected_subject!r}"
        )
    head = current_head()
    if details["target"] != head:
        raise ReleaseCliError(
            f"{tag} points to {details['target']}, but HEAD is {head}; "
            "check out the reviewed release commit before publishing"
        )
    return head


def remote_tag_exists(repository: str, tag: str) -> bool:
    output = capture(
        [
            "gh",
            "api",
            f"repos/{repository}/git/matching-refs/tags/{tag}",
        ]
    )
    try:
        refs = json.loads(output)
    except json.JSONDecodeError as error:
        raise ReleaseCliError("GitHub CLI returned an invalid remote-tag result") from error
    if not isinstance(refs, list):
        raise ReleaseCliError("GitHub CLI returned an unexpected remote-tag result")
    return any(
        isinstance(item, dict) and item.get("ref") == f"refs/tags/{tag}"
        for item in refs
    )


def release_details(repository: str, tag: str, *, required: bool) -> dict[str, object] | None:
    result = run_command(
        [
            "gh",
            "release",
            "view",
            tag,
            "--repo",
            repository,
            "--json",
            "tagName,isDraft,isPrerelease,url,assets",
        ],
        capture=True,
    )
    if result.returncode != 0:
        detail = (result.stdout or "").strip()
        if not required and (
            "http 404" in detail.lower() or "release not found" in detail.lower()
        ):
            return None
        raise ReleaseCliError(
            f"unable to inspect GitHub Release {tag}: {detail[-2000:]}"
        )
    try:
        data = json.loads(result.stdout or "{}")
    except json.JSONDecodeError as error:
        raise ReleaseCliError("gh release view returned invalid JSON") from error
    if not isinstance(data, dict):
        raise ReleaseCliError("gh release view returned an unexpected JSON value")
    return data


def optional_capture(
    command: Sequence[str], *, cwd: Path | None = None
) -> tuple[bool, str]:
    try:
        result = run_command(command, cwd=cwd, capture=True)
    except ReleaseCliError as error:
        return False, str(error)
    return result.returncode == 0, (result.stdout or "").strip()


def probe_runtime() -> validation_runtime.Runtime:
    try:
        return validation_runtime.probe_runtime(
            root=ROOT,
            capture=capture,
            optional_capture=optional_capture,
            probe_docker_fn=lambda prefix=(): validation_runtime.probe_docker(
                prefix, optional_capture=optional_capture
            ),
        )
    except validation_runtime.ValidationRuntimeError as error:
        raise ReleaseCliError(str(error)) from error


def ensure_validation_image(runtime: validation_runtime.Runtime) -> str:
    try:
        return validation_runtime.ensure_validation_image(
            runtime,
            root=ROOT,
            optional_capture=optional_capture,
            run_step=lambda name, command: run_step(name, command),
        )
    except validation_runtime.ValidationRuntimeError as error:
        raise ReleaseCliError(str(error)) from error


def run_preflight(
    tag: str,
    notes_file: Path,
    *,
    create_tag: bool,
    remote: str,
    repository: str,
) -> None:
    if not notes_file.is_file():
        raise ReleaseCliError(f"release notes file does not exist: {notes_file}")
    runtime = probe_runtime()
    ensure_validation_image(runtime)
    if remote_tag_exists(repository, tag):
        raise ReleaseCliError(f"remote tag already exists on {remote}: {tag}")
    notes_in_container = validation_runtime.container_path(
        notes_file.resolve(), runtime, ROOT
    )
    argv = [
        "python3",
        "tools/release_preflight.py",
        "--tag",
        tag,
        "--notes-file",
        notes_in_container,
        "--remote",
        remote,
    ]
    if create_tag:
        argv.append("--create-tag")
    try:
        validation_runtime.launch_in_validation(
            runtime,
            argv,
            root=ROOT,
            capture=capture,
            run_step=lambda name, command: run_step(name, command),
        )
    except validation_runtime.ValidationRuntimeError as error:
        raise ReleaseCliError(str(error)) from error


def find_release_run(repository: str, tag: str, sha: str) -> WorkflowRun:
    for attempt in range(DISCOVERY_ATTEMPTS):
        output = capture(
            [
                "gh",
                "run",
                "list",
                "--repo",
                repository,
                "--workflow",
                "release.yml",
                "--event",
                "push",
                "--limit",
                "50",
                "--json",
                "databaseId,headBranch,headSha,status,conclusion,url",
            ]
        )
        try:
            runs = json.loads(output)
        except json.JSONDecodeError as error:
            raise ReleaseCliError("gh run list returned invalid JSON") from error
        if not isinstance(runs, list):
            raise ReleaseCliError("gh run list returned an unexpected JSON value")
        matches = [
            run
            for run in runs
            if run.get("headBranch") == tag and run.get("headSha") == sha
        ]
        if matches:
            selected = max(matches, key=lambda item: int(item.get("databaseId", 0)))
            try:
                return WorkflowRun(
                    database_id=int(selected["databaseId"]),
                    url=str(selected.get("url", "")),
                    status=str(selected.get("status", "")),
                    conclusion=(
                        str(selected["conclusion"])
                        if selected.get("conclusion") is not None
                        else None
                    ),
                )
            except (KeyError, TypeError, ValueError) as error:
                raise ReleaseCliError("GitHub Actions run has invalid metadata") from error
        if attempt + 1 < DISCOVERY_ATTEMPTS:
            time.sleep(POLL_SECONDS)
    raise ReleaseCliError(
        f"no Release workflow run for tag {tag} at {sha} appeared within "
        f"{DISCOVERY_ATTEMPTS * POLL_SECONDS} seconds"
    )


def workflow_state(repository: str, run_id: int) -> dict[str, object]:
    output = capture(
        [
            "gh",
            "run",
            "view",
            str(run_id),
            "--repo",
            repository,
            "--json",
            "status,conclusion,url,jobs",
        ]
    )
    try:
        data = json.loads(output)
    except json.JSONDecodeError as error:
        raise ReleaseCliError("gh run view returned invalid JSON") from error
    if not isinstance(data, dict):
        raise ReleaseCliError("gh run view returned an unexpected JSON value")
    return data


def waiting_for_approval(state: dict[str, object]) -> bool:
    if state.get("status") == "waiting":
        return True
    jobs = state.get("jobs", [])
    if not isinstance(jobs, list):
        raise ReleaseCliError("GitHub Actions run jobs are not a list")
    for job in jobs:
        if not isinstance(job, dict):
            continue
        if job.get("name") == "Build and publish" and job.get("status") == "waiting":
            return True
    return False


def watch_release(repository: str, tag: str, sha: str) -> None:
    run = find_release_run(repository, tag, sha)
    print(f"Release workflow: {run.url or run.database_id}")

    while True:
        state = workflow_state(repository, run.database_id)
        status = str(state.get("status", "unknown"))
        conclusion = state.get("conclusion")
        url = str(state.get("url") or run.url or "")
        if waiting_for_approval(state):
            raise ApprovalRequired(
                "Release workflow is waiting for protected release-environment "
                f"approval. A maintainer must approve it manually: {url or run.database_id}"
            )
        if status == "completed":
            if conclusion == "success":
                print(f"Release workflow completed successfully: {url or run.database_id}")
                return
            raise ReleaseCliError(
                f"Release workflow completed with {conclusion or 'no conclusion'}: "
                f"{url or run.database_id}"
            )

        print(f"Release workflow status: {status}")
        try:
            watched = run_command(
                [
                    "gh",
                    "run",
                    "watch",
                    str(run.database_id),
                    "--repo",
                    repository,
                    "--exit-status",
                ],
                timeout=WATCH_SECONDS,
            )
        except subprocess.TimeoutExpired:
            continue
        if watched.returncode != 0:
            after_watch = workflow_state(repository, run.database_id)
            if waiting_for_approval(after_watch):
                after_url = str(after_watch.get("url") or url or run.database_id)
                raise ApprovalRequired(
                    "Release workflow is waiting for protected release-environment "
                    f"approval. A maintainer must approve it manually: {after_url}"
                )
            if after_watch.get("status") == "completed":
                continue
            raise ReleaseCliError(
                "gh run watch stopped before the Release workflow completed"
            )


def verify_release(repository: str, tag: str) -> None:
    release = release_details(repository, tag, required=True)
    assert release is not None
    if release.get("tagName") != tag:
        raise ReleaseCliError(
            f"GitHub Release tag is {release.get('tagName')!r}; expected {tag!r}"
        )
    if release.get("isDraft") is True:
        raise ReleaseCliError(f"GitHub Release {tag} is still a draft")
    if release.get("isPrerelease") is True:
        raise ReleaseCliError(f"GitHub Release {tag} is unexpectedly a prerelease")
    assets = release.get("assets", [])
    if not isinstance(assets, list):
        raise ReleaseCliError("GitHub Release assets are not a list")
    names = {
        str(asset.get("name"))
        for asset in assets
        if isinstance(asset, dict) and asset.get("name")
    }
    missing = sorted(PRICING_ASSETS - names)
    if missing:
        raise ReleaseCliError(
            "GitHub Release is missing required immutable pricing assets: "
            + ", ".join(missing)
        )
    print(f"GitHub Release verified: {release.get('url', tag)}")
    print("Required pricing assets: " + ", ".join(sorted(PRICING_ASSETS)))


def inspect(repository: str, tag: str) -> None:
    details = local_tag_details(tag, required=False)
    if details is None:
        print(f"Local tag: absent ({tag})")
        sha = current_head()
    else:
        print(
            f"Local tag: {details['object_type']} at {details['target']}; "
            f"subject: {details['subject']}"
        )
        sha = details["target"]

    remote = "present" if remote_tag_exists(repository, tag) else "absent"
    print(f"Remote tag: {remote} ({tag})")
    release = release_details(repository, tag, required=False)
    if release is None:
        print("GitHub Release: absent")
    else:
        print(f"GitHub Release: {release.get('url', tag)}")
    try:
        run = find_release_run(repository, tag, sha)
    except ReleaseCliError as error:
        print(f"Release workflow: not found ({error})")
    else:
        print(
            f"Release workflow: {run.status}/{run.conclusion or 'pending'} "
            f"{run.url or run.database_id}"
        )


def finalize(repository: str, tag: str) -> None:
    verify_release(repository, tag)
    require_clean_worktree()
    path = ROOT / "UPSTREAM.md"
    content = path.read_text(encoding="utf-8")
    row = re.compile(
        rf"^(\|\s*`{re.escape(tag)}`\s*\|\s*`[^`]+`\s*\|\s*`[0-9a-f]{{40}}`\s*\|\s*)planned(\s*\|)$",
        re.MULTILINE,
    )
    updated, replacements = row.subn(r"\1published\2", content)
    if replacements == 0:
        raise ReleaseCliError(
            f"UPSTREAM.md has no planned mapping row to finalize for {tag}"
        )
    if replacements != 1:
        raise ReleaseCliError(
            f"UPSTREAM.md has {replacements} planned mapping rows for {tag}; "
            "refusing to modify it"
        )
    path.write_text(updated, encoding="utf-8")
    validation = run_command(
        [
            sys.executable,
            "tools/check_release.py",
            "--tag",
            tag,
            "--require-status",
            "published",
        ],
        capture=True,
    )
    if validation.returncode != 0:
        path.write_text(content, encoding="utf-8")
        detail = (validation.stdout or "").strip()
        raise ReleaseCliError(
            "post-publication UPSTREAM.md validation failed; restored the file"
            + (f": {detail[-2000:]}" if detail else "")
        )
    run_step("Stage finalized UPSTREAM.md", ["git", "add", "--", "UPSTREAM.md"])
    print(f"Finalized and staged UPSTREAM.md for {tag}. Commit it with push-cli.")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Safely tag and publish immutable Sub2API Plus GitHub releases."
    )
    parser.add_argument(
        "action",
        choices=("inspect", "validate", "tag", "publish", "monitor", "verify", "finalize"),
    )
    parser.add_argument("--tag", required=True, help="vX.Y.Z+custom.NNN")
    parser.add_argument("--notes-file", type=Path)
    parser.add_argument("--remote", default=DEFAULT_REMOTE)
    parser.add_argument("--repo-root", type=Path, default=ROOT, help=argparse.SUPPRESS)
    return parser.parse_args()


def main() -> int:
    global ROOT
    args = parse_args()
    ROOT = args.repo_root.resolve()
    try:
        repository = github_gate(args.remote)
        validate_tag(args.tag)

        if args.action in {"validate", "tag"}:
            if args.notes_file is None:
                raise ReleaseCliError("--notes-file is required for validate and tag")
            setup_git_transport()
            run_preflight(
                args.tag,
                args.notes_file,
                create_tag=args.action == "tag",
                remote=args.remote,
                repository=repository,
            )
            return 0

        if args.action == "inspect":
            inspect(repository, args.tag)
            return 0

        if args.action == "publish":
            require_clean_worktree(allow_untracked=True)
            sha = require_publishable_local_tag(args.tag)
            if remote_tag_exists(repository, args.tag):
                raise ReleaseCliError(
                    f"remote tag already exists: {args.tag}; immutable tags are never reused"
                )
            if release_details(repository, args.tag, required=False) is not None:
                raise ReleaseCliError(
                    f"GitHub Release already exists for {args.tag}; refusing to republish"
                )
            setup_git_transport()
            run_step("Push exact release tag", ["git", "push", args.remote, args.tag])
            watch_release(repository, args.tag, sha)
            verify_release(repository, args.tag)
            return 0

        if args.action == "monitor":
            local = local_tag_details(args.tag, required=True)
            assert local is not None
            if not remote_tag_exists(repository, args.tag):
                raise ReleaseCliError(f"remote tag is absent: {args.tag}")
            watch_release(repository, args.tag, local["target"])
            return 0

        if args.action == "verify":
            local = local_tag_details(args.tag, required=True)
            assert local is not None
            if not remote_tag_exists(repository, args.tag):
                raise ReleaseCliError(f"remote tag is absent: {args.tag}")
            watch_release(repository, args.tag, local["target"])
            verify_release(repository, args.tag)
            return 0

        if args.action == "finalize":
            finalize(repository, args.tag)
            return 0

        raise ReleaseCliError(f"unsupported action: {args.action}")
    except ApprovalRequired as error:
        print(f"release-cli paused: {error}", file=sys.stderr)
        return 2
    except ReleaseCliError as error:
        print(f"release-cli stopped: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
