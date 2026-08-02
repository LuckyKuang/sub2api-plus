#!/usr/bin/env python3
"""Run the local release gate before creating or pushing a Git tag."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

import release_docs


ROOT = Path(__file__).resolve().parents[1]
TAG_RE = release_docs.TAG_RE
MINIMUM_PYTHON = (3, 10)
MINIMUM_BASH_MAJOR = 4
REQUIRED_SUBJECT_PREFIX = "Sub2API Plus "


@dataclass(frozen=True)
class Step:
    name: str
    command: tuple[str, ...]
    cwd: Path = ROOT


def platform_command(command: list[str] | tuple[str, ...]) -> tuple[str, ...]:
    executable = (
        "pnpm.cmd"
        if os.name == "nt" and command[0] == "pnpm"
        else command[0]
    )
    return (executable, *command[1:])


def run(
    command: list[str] | tuple[str, ...],
    *,
    cwd: Path = ROOT,
    capture: bool = False,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        platform_command(command),
        cwd=cwd,
        check=False,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
    )


def command_output(
    command: tuple[str, ...],
    *,
    cwd: Path = ROOT,
) -> tuple[str | None, str | None]:
    try:
        result = run(command, cwd=cwd, capture=True)
    except FileNotFoundError:
        return None, f"required command is unavailable: {command[0]}"
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        return None, f"{display_command(command)} failed: {detail or result.returncode}"
    return result.stdout.strip(), None


def git_output(*args: str) -> str:
    output, error = command_output(("git", *args))
    if error is not None:
        raise RuntimeError(error)
    return output or ""


def ensure_clean(notes_file: Path) -> None:
    result = subprocess.run(
        [
            "git",
            "-c",
            "core.quotepath=false",
            "status",
            "--porcelain=v1",
            "-z",
            "--untracked-files=all",
        ],
        cwd=ROOT,
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        raise RuntimeError(result.stderr.decode("utf-8", "replace").strip())

    allowed_note: str | None = None
    try:
        allowed_note = notes_file.resolve().relative_to(ROOT).as_posix()
    except ValueError:
        pass

    dirty: list[str] = []
    entries = result.stdout.decode("utf-8", "surrogateescape").split("\0")
    for entry in entries:
        if not entry:
            continue
        status, path = entry[:2], entry[3:]
        if status == "??" and allowed_note is not None and path == allowed_note:
            continue
        dirty.append(f"{status} {path}")

    if dirty:
        preview = "\n".join(f"  {item}" for item in dirty[:20])
        suffix = "\n  ..." if len(dirty) > 20 else ""
        raise RuntimeError(
            "release preflight requires a clean worktree; commit or remove these "
            f"changes first:\n{preview}{suffix}"
        )


def ensure_tag_absent(tag: str, remote: str) -> None:
    local = run(("git", "show-ref", "--verify", "--quiet", f"refs/tags/{tag}"))
    if local.returncode == 0:
        raise RuntimeError(f"local tag already exists: {tag}")
    if local.returncode not in {0, 1}:
        raise RuntimeError(f"unable to inspect local tag {tag}")

    remote_result = run(
        (
            "git",
            "ls-remote",
            "--exit-code",
            "--tags",
            remote,
            f"refs/tags/{tag}",
            f"refs/tags/{tag}^{{}}",
        ),
        capture=True,
    )
    if remote_result.returncode == 0:
        raise RuntimeError(f"remote tag already exists on {remote}: {tag}")
    if remote_result.returncode != 2:
        detail = (remote_result.stderr or remote_result.stdout or "").strip()
        raise RuntimeError(f"unable to verify tags on remote {remote}: {detail}")


def version_key(tag: str) -> tuple[int, int, int, int] | None:
    return release_docs.version_key(tag)


def docker_tag_version(tag: str) -> str:
    """Convert the validated Git release tag to its OCI-safe form."""
    return tag.replace("+", "-")


def select_previous_release_tag(
    tags: list[str],
    statuses: dict[str, str],
    target: str,
) -> str | None:
    return release_docs.select_previous_release_tag(tags, statuses, target)


def previous_release_tag(target: str) -> str | None:
    statuses = release_docs.parse_upstream_statuses(
        ROOT.joinpath("UPSTREAM.md").read_text(encoding="utf-8")
    )
    tags = git_output("tag", "--merged", "HEAD", "--list").splitlines()
    return select_previous_release_tag(tags, statuses, target)


def parse_version(value: str) -> tuple[int, ...] | None:
    match = re.search(r"(\d+)(?:\.(\d+))?(?:\.(\d+))?", value)
    if not match:
        return None
    return tuple(int(part) for part in match.groups(default="0"))


def declared_tool_version(name: str) -> str | None:
    text = ROOT.joinpath(".tool-versions").read_text(encoding="utf-8")
    for raw_line in text.splitlines():
        fields = raw_line.split()
        if len(fields) == 2 and fields[0] == name:
            return fields[1]
    return None


def check_toolchains() -> tuple[list[str], list[str]]:
    errors: list[str] = []
    detected: list[str] = []

    if sys.version_info[:2] < MINIMUM_PYTHON:
        errors.append(
            f"Python {MINIMUM_PYTHON[0]}.{MINIMUM_PYTHON[1]}+ is required; "
            f"found {sys.version.split()[0]}"
        )
    else:
        detected.append(f"Python {sys.version.split()[0]}")

    go_mod = ROOT.joinpath("backend/go.mod").read_text(encoding="utf-8")
    go_match = re.search(r"^go\s+(\d+\.\d+(?:\.\d+)?)\s*$", go_mod, re.MULTILINE)
    expected_go = parse_version(go_match.group(1)) if go_match else None
    go_output, go_error = command_output(("go", "env", "GOVERSION"), cwd=ROOT / "backend")
    if go_error:
        errors.append(go_error)
    elif expected_go is None:
        errors.append("cannot determine the required Go version from backend/go.mod")
    else:
        actual_go = parse_version(go_output or "")
        if actual_go != expected_go:
            errors.append(
                f"Go {go_match.group(1)} is required; "
                f"found {go_output!r}"
            )
        else:
            detected.append(f"Go {go_output}")

    package = json.loads(
        ROOT.joinpath("frontend/package.json").read_text(encoding="utf-8")
    )
    package_manager = package.get("packageManager", "")
    manager_match = re.fullmatch(r"pnpm@(.+)", package_manager)
    expected_pnpm = manager_match.group(1) if manager_match else None
    pnpm_output, pnpm_error = command_output(("pnpm", "--version"))
    if pnpm_error:
        errors.append(pnpm_error)
    elif expected_pnpm is None:
        errors.append("frontend/package.json must declare packageManager as pnpm@VERSION")
    elif pnpm_output != expected_pnpm:
        errors.append(f"pnpm {expected_pnpm} is required; found {pnpm_output!r}")
    else:
        detected.append(f"pnpm {pnpm_output}")

    node_requirement = package.get("engines", {}).get("node", "")
    node_minimum_match = re.search(r">=\s*(\d+(?:\.\d+){0,2})", node_requirement)
    node_minimum = (
        parse_version(node_minimum_match.group(1)) if node_minimum_match else None
    )
    node_output, node_error = command_output(("node", "--version"))
    if node_error:
        errors.append(node_error)
    elif node_minimum is None:
        errors.append("frontend/package.json must declare a simple Node.js >= minimum")
    else:
        actual_node = parse_version(node_output or "")
        if actual_node is None or actual_node < node_minimum:
            errors.append(
                f"Node.js {node_requirement} is required; found {node_output!r}"
            )
        else:
            detected.append(f"Node.js {node_output}")

    expected_lint = declared_tool_version("golangci-lint")
    if expected_lint is None:
        errors.append(".tool-versions must declare golangci-lint")
    lint_output, lint_error = command_output(("golangci-lint", "version"))
    if lint_error:
        errors.append(lint_error)
    elif expected_lint is not None:
        lint_match = re.search(r"\bversion\s+v?(\d+\.\d+(?:\.\d+)?)", lint_output or "")
        actual_lint = lint_match.group(1) if lint_match else None
        if actual_lint != expected_lint:
            errors.append(
                f"golangci-lint {expected_lint} is required; found "
                f"{lint_output!r}"
            )
        else:
            detected.append(f"golangci-lint {actual_lint}")

    expected_goreleaser = declared_tool_version("goreleaser")
    if expected_goreleaser is None:
        errors.append(".tool-versions must declare goreleaser")
    goreleaser_output, goreleaser_error = command_output(("goreleaser", "--version"))
    if goreleaser_error:
        errors.append(goreleaser_error)
    elif expected_goreleaser is not None:
        goreleaser_match = re.search(
            r"(?:version|v)\s*v?(\d+\.\d+(?:\.\d+)?)",
            goreleaser_output or "",
            re.IGNORECASE,
        )
        actual_goreleaser = goreleaser_match.group(1) if goreleaser_match else None
        if actual_goreleaser != expected_goreleaser.removeprefix("v"):
            errors.append(
                f"goreleaser {expected_goreleaser} is required; found "
                f"{goreleaser_output!r}"
            )
        else:
            detected.append(f"goreleaser {actual_goreleaser}")

    bash_output, bash_error = command_output(("bash", "--version"))
    if bash_error:
        errors.append(bash_error)
    else:
        bash_match = re.search(r"version\s+(\d+)", bash_output or "", re.IGNORECASE)
        bash_major = int(bash_match.group(1)) if bash_match else None
        if bash_major is None or bash_major < MINIMUM_BASH_MAJOR:
            errors.append(
                f"Bash {MINIMUM_BASH_MAJOR}+ is required; found {bash_output!r}"
            )
        else:
            first_line = (bash_output or "").splitlines()[0]
            detected.append(first_line)

    return errors, detected


def display_command(command: tuple[str, ...] | list[str]) -> str:
    if os.name == "nt":
        return subprocess.list2cmdline(command)
    return shlex.join(command)


def notes_digest(notes_file: Path) -> str:
    return hashlib.sha256(notes_file.read_bytes()).hexdigest()


def tag_creation_command(tag: str, commit: str, notes_file: Path) -> tuple[str, ...]:
    return (
        "git",
        "tag",
        "-a",
        tag,
        commit,
        "--cleanup=verbatim",
        "-F",
        str(notes_file),
    )


def verify_created_tag(
    tag: str,
    commit: str,
    expected_subject: str,
    expected_message: str,
) -> None:
    object_type = git_output("cat-file", "-t", f"refs/tags/{tag}")
    if object_type != "tag":
        raise RuntimeError(f"{tag} is not an annotated tag")
    subject = git_output(
        "for-each-ref",
        "--format=%(contents:subject)",
        f"refs/tags/{tag}",
    )
    if subject != expected_subject:
        raise RuntimeError(
            f"{tag} subject is {subject!r}; expected {expected_subject!r}"
        )
    message = git_output(
        "for-each-ref",
        "--format=%(contents)",
        f"refs/tags/{tag}",
    )
    if message != expected_message.strip():
        raise RuntimeError(
            f"{tag} message differs from the validated release notes"
        )
    target = git_output("rev-list", "-n", "1", tag)
    if target != commit:
        raise RuntimeError(f"{tag} points to {target}, expected {commit}")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate a release commit before creating its annotated tag."
    )
    parser.add_argument("--tag", required=True, help="vX.Y.Z+custom.NNN")
    parser.add_argument("--notes-file", required=True, type=Path)
    parser.add_argument("--remote", default="origin")
    parser.add_argument(
        "--create-tag",
        action="store_true",
        help="create the verified local annotated tag after every check passes",
    )
    args = parser.parse_args()

    tag_match = TAG_RE.fullmatch(args.tag)
    if not tag_match or tag_match.group(4) == "000":
        parser.error("--tag must match vX.Y.Z+custom.NNN with NNN from 001 to 999")

    notes_file = args.notes_file.expanduser().resolve()
    if not notes_file.is_file():
        parser.error(f"release notes file does not exist: {notes_file}")
    initial_notes_digest = notes_digest(notes_file)
    first_line = next(
        (
            line.strip()
            for line in notes_file.read_text(encoding="utf-8").splitlines()
            if line.strip()
        ),
        "",
    )
    expected_subject = f"{REQUIRED_SUBJECT_PREFIX}{args.tag}"
    if first_line != expected_subject:
        parser.error(
            f"first non-empty release-notes line must be {expected_subject!r}"
        )

    try:
        git_output("rev-parse", "--show-toplevel")
        ensure_clean(notes_file)
        commit = git_output("rev-parse", "HEAD")
        migration_base = previous_release_tag(args.tag)
    except RuntimeError as error:
        print(f"Preflight stopped: {error}", file=sys.stderr)
        return 1

    toolchain_errors, detected_toolchains = check_toolchains()
    if toolchain_errors:
        print("Preflight stopped: local toolchain requirements are not met:", file=sys.stderr)
        for error in toolchain_errors:
            print(f"- {error}", file=sys.stderr)
        return 1
    print("Toolchains: " + "; ".join(detected_toolchains))

    try:
        ensure_tag_absent(args.tag, args.remote)
    except RuntimeError as error:
        print(f"Preflight stopped: {error}", file=sys.stderr)
        return 1

    python = sys.executable
    steps = [
        Step(
            "Release metadata and notes",
            (
                python,
                "tools/check_release.py",
                "--tag",
                args.tag,
                "--notes-file",
                str(notes_file),
                "--require-status",
                "planned",
            ),
        ),
        Step("README synchronization", (python, "tools/check_readme_sync.py")),
        Step(
            "Migration policy",
            (
                python,
                "tools/check_new_migrations.py",
                *(("--base", migration_base) if migration_base else ()),
            ),
        ),
        Step("Linux installer syntax", ("bash", "-n", "deploy/install.sh")),
        Step(
            "Apple container installer syntax",
            ("bash", "-n", "deploy/apple-container.sh"),
        ),
        Step(
            "Docker Compose deployment security",
            ("sh", "deploy/tests/docker-compose-security-test.sh"),
        ),
        Step(
            "Runtime fallback resources",
            ("sh", "deploy/tests/docker-runtime-resources-test.sh"),
        ),
        Step(
            "Caddy cache deployment test",
            ("bash", "deploy/test-caddyfile-cache.sh"),
        ),
        Step(
            "GoReleaser configuration",
            (
                "env",
                "GITHUB_REPO_OWNER=validation",
                "GITHUB_REPO_OWNER_LOWER=validation",
                "GITHUB_REPO_NAME=sub2api-plus",
                f"DOCKER_TAG_VERSION={docker_tag_version(args.tag)}",
                "DOCKERHUB_USERNAME=skip",
                "TAG_MESSAGE=GoReleaser configuration validation only.",
                "goreleaser",
                "check",
            ),
        ),
        Step(
            "Go module tidiness",
            ("go", "mod", "tidy", "-diff"),
            ROOT / "backend",
        ),
    ]
    if sys.platform == "darwin":
        steps.append(
            Step(
                "Apple container lifecycle test",
                ("bash", "deploy/tests/apple-container-test.sh"),
            )
        )
    steps.extend(
        [
            Step(
                "Backend unit tests",
                ("go", "test", "-tags=unit", "./..."),
                ROOT / "backend",
            ),
            Step(
                "Backend integration tests",
                ("go", "test", "-tags=integration", "./..."),
                ROOT / "backend",
            ),
            Step(
                "Backend lint",
                ("golangci-lint", "run", "./..."),
                ROOT / "backend",
            ),
            Step(
                "Frontend frozen install",
                ("pnpm", "--dir", "frontend", "install", "--frozen-lockfile"),
            ),
            Step(
                "Frontend lint",
                ("pnpm", "--dir", "frontend", "run", "lint:check"),
            ),
            Step(
                "Frontend typecheck",
                ("pnpm", "--dir", "frontend", "run", "typecheck"),
            ),
            Step(
                "Frontend tests",
                ("pnpm", "--dir", "frontend", "run", "test:run"),
            ),
            Step(
                "Frontend production build",
                ("pnpm", "--dir", "frontend", "run", "build"),
            ),
        ]
    )

    print(f"Release preflight for {args.tag} at {commit}")
    if migration_base:
        print(f"Migration comparison base: {migration_base}")
    else:
        print("Migration comparison base: none (no earlier published/historical tag)")

    for index, step in enumerate(steps, start=1):
        print(f"\n[{index}/{len(steps)}] {step.name}")
        print(f"$ {display_command(step.command)}")
        try:
            result = run(step.command, cwd=step.cwd)
        except FileNotFoundError as error:
            print(
                f"Preflight stopped: required command is unavailable: {error.filename}",
                file=sys.stderr,
            )
            return 1
        if result.returncode != 0:
            print(
                f"Preflight stopped: {step.name} failed with exit code "
                f"{result.returncode}.",
                file=sys.stderr,
            )
            return result.returncode or 1

    try:
        ensure_clean(notes_file)
        if git_output("rev-parse", "HEAD") != commit:
            raise RuntimeError("HEAD changed while preflight was running")
        if notes_digest(notes_file) != initial_notes_digest:
            raise RuntimeError("release notes changed while preflight was running")
        ensure_tag_absent(args.tag, args.remote)
    except RuntimeError as error:
        print(f"Preflight stopped after checks: {error}", file=sys.stderr)
        return 1

    tag_command = tag_creation_command(args.tag, commit, notes_file)
    push_command = ("git", "push", args.remote, args.tag)
    if not args.create_tag:
        print("\nRelease preflight passed. No tag was created or pushed.")
        print("To repeat the gate and atomically create the local annotated tag, run:")
        print(
            "  "
            + display_command(
                (
                    sys.executable,
                    "tools/release_preflight.py",
                    "--tag",
                    args.tag,
                    "--notes-file",
                    str(notes_file),
                    "--remote",
                    args.remote,
                    "--create-tag",
                )
            )
        )
        return 0

    result = run(tag_command, capture=True)
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        print(f"Preflight stopped: local tag creation failed: {detail}", file=sys.stderr)
        return result.returncode or 1
    try:
        verify_created_tag(
            args.tag,
            commit,
            expected_subject,
            notes_file.read_text(encoding="utf-8"),
        )
    except RuntimeError as error:
        print(
            "Local tag was created but post-creation verification failed: "
            f"{error}. Inspect it before taking any further action.",
            file=sys.stderr,
        )
        return 1

    print(f"\nCreated and verified local annotated tag {args.tag}.")
    print("No remote tag was pushed. Review, then publish explicitly:")
    print(f"  git show --no-patch {args.tag}")
    print(f"  {display_command(push_command)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
