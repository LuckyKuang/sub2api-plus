#!/usr/bin/env python3
"""Run the focused metadata gate before creating a release tag."""

from __future__ import annotations

import argparse
import hashlib
import shlex
import subprocess
import sys
from pathlib import Path

import release_docs


ROOT = Path(__file__).resolve().parents[1]
TAG_RE = release_docs.TAG_RE
REQUIRED_SUBJECT_PREFIX = "Sub2API Plus "


def run(
    command: list[str] | tuple[str, ...],
    *,
    cwd: Path = ROOT,
    capture: bool = False,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        cwd=cwd,
        check=False,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
    )


def display_command(command: tuple[str, ...] | list[str]) -> str:
    return shlex.join(command)


def git_output(*args: str) -> str:
    result = run(("git", *args), capture=True)
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise RuntimeError(
            f"{display_command(('git', *args))} failed: {detail or result.returncode}"
        )
    return result.stdout.strip()


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
    for entry in result.stdout.decode("utf-8", "surrogateescape").split("\0"):
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
            "release metadata gate requires a clean worktree; commit or remove "
            f"these changes first:\n{preview}{suffix}"
        )


def ensure_tag_absent(tag: str, remote: str) -> None:
    local = run(("git", "show-ref", "--verify", "--quiet", f"refs/tags/{tag}"))
    if local.returncode == 0:
        raise RuntimeError(f"local tag already exists: {tag}")
    if local.returncode != 1:
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
    if git_output("cat-file", "-t", f"refs/tags/{tag}") != "tag":
        raise RuntimeError(f"{tag} is not an annotated tag")
    subject = git_output(
        "for-each-ref", "--format=%(contents:subject)", f"refs/tags/{tag}"
    )
    if subject != expected_subject:
        raise RuntimeError(
            f"{tag} subject is {subject!r}; expected {expected_subject!r}"
        )
    message = git_output(
        "for-each-ref", "--format=%(contents)", f"refs/tags/{tag}"
    )
    if message != expected_message.strip():
        raise RuntimeError(f"{tag} message differs from the validated release notes")
    target = git_output("rev-list", "-n", "1", tag)
    if target != commit:
        raise RuntimeError(f"{tag} points to {target}, expected {commit}")


def run_metadata_check(tag: str, notes_file: Path) -> None:
    command = (
        sys.executable,
        "tools/check_release.py",
        "--tag",
        tag,
        "--notes-file",
        str(notes_file),
        "--require-status",
        "planned",
    )
    print("\n[Release metadata and notes]")
    print(f"$ {display_command(command)}")
    result = run(command)
    if result.returncode != 0:
        raise RuntimeError(
            f"release metadata validation failed with exit code {result.returncode}"
        )


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate release metadata before creating an annotated tag."
    )
    parser.add_argument("--tag", required=True, help="vX.Y.Z+custom.NNN")
    parser.add_argument("--notes-file", required=True, type=Path)
    parser.add_argument("--commit", default="HEAD")
    parser.add_argument("--remote", default="origin")
    parser.add_argument("--create-tag", action="store_true")
    args = parser.parse_args()

    tag_match = TAG_RE.fullmatch(args.tag)
    if not tag_match or tag_match.group(4) == "000":
        parser.error("--tag must match vX.Y.Z+custom.NNN with NNN from 001 to 999")

    notes_file = args.notes_file.expanduser().resolve()
    if not notes_file.is_file():
        parser.error(f"release notes file does not exist: {notes_file}")
    notes = notes_file.read_text(encoding="utf-8")
    first_line = next((line.strip() for line in notes.splitlines() if line.strip()), "")
    expected_subject = f"{REQUIRED_SUBJECT_PREFIX}{args.tag}"
    if first_line != expected_subject:
        parser.error(f"first non-empty release-notes line must be {expected_subject!r}")

    initial_digest = notes_digest(notes_file)
    try:
        ensure_clean(notes_file)
        commit = git_output("rev-parse", f"{args.commit}^{{commit}}")
        head_tree = git_output("rev-parse", "HEAD^{tree}")
        commit_tree = git_output("rev-parse", f"{commit}^{{tree}}")
        if head_tree != commit_tree:
            raise RuntimeError(
                "the checked-out tree differs from the requested tag commit; "
                "check out the promoted pull-request tree first"
            )
        ensure_tag_absent(args.tag, args.remote)
        run_metadata_check(args.tag, notes_file)
        ensure_clean(notes_file)
        if notes_digest(notes_file) != initial_digest:
            raise RuntimeError("release notes changed while validation was running")
        if git_output("rev-parse", "HEAD^{tree}") != head_tree:
            raise RuntimeError("the checked-out tree changed while validation was running")
        ensure_tag_absent(args.tag, args.remote)
    except RuntimeError as error:
        print(f"Release metadata gate stopped: {error}", file=sys.stderr)
        return 1

    if not args.create_tag:
        print(f"Release metadata is ready for {args.tag} at {commit}. No tag was created.")
        return 0

    command = tag_creation_command(args.tag, commit, notes_file)
    result = run(command, capture=True)
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        print(f"Local tag creation failed: {detail}", file=sys.stderr)
        return result.returncode or 1
    try:
        verify_created_tag(args.tag, commit, expected_subject, notes)
    except RuntimeError as error:
        print(
            "Local tag was created but verification failed: " + str(error),
            file=sys.stderr,
        )
        return 1

    print(f"Created and verified local annotated tag {args.tag} at {commit}.")
    print("No remote tag was pushed. Publish it explicitly with release-cli.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
