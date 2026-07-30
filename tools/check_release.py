#!/usr/bin/env python3
"""Validate release metadata and optional release notes."""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

import release_docs


ROOT = Path(__file__).resolve().parents[1]
TAG_RE = re.compile(r"^v(\d+\.\d+\.\d+\+custom\.(\d{3}))$")
REQUIRED_NOTE_SECTIONS = (
    "Highlights",
    "Compatibility and migration",
    "Known issues",
    "Upstream baseline",
)
ALLOWED_STATUSES = {"planned", "published", "historical", "withdrawn", "invalid"}
INSTALLER_VERSION_FRAGMENTS = (
    r'[[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+\+custom\.[0-9]{3}$ ]]',
    r"grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+(\+custom\.[0-9]{3})?'",
)


def fail(message: str, errors: list[str]) -> None:
    errors.append(message)


def validate_required_status(
    tag: str,
    actual: str,
    required: str | None,
    errors: list[str],
) -> None:
    if required is not None and actual != required:
        fail(
            f"{tag} is marked {actual!r} in UPSTREAM.md; expected {required!r}",
            errors,
        )


def validate_release_documentation(root: Path, errors: list[str]) -> None:
    try:
        _, _, stale_release_docs = release_docs.pending_release_doc_updates(root)
    except (OSError, release_docs.ReleaseDocsError) as error:
        fail(f"cannot validate release documentation: {error}", errors)
        return

    for path in stale_release_docs:
        fail(
            f"{path.relative_to(root)} has stale release-version examples",
            errors,
        )
    if stale_release_docs:
        fail("Run: python tools/update_release_docs.py", errors)


def parse_upstream(tag: str, errors: list[str]) -> tuple[str, str, str] | None:
    text = (ROOT / "UPSTREAM.md").read_text(encoding="utf-8")
    row_re = re.compile(
        rf"^\|\s*`{re.escape(tag)}`\s*\|\s*`([^`]+)`\s*\|\s*`([0-9a-f]{{40}})`"
        rf"\s*\|\s*([a-z]+)\s*\|$",
        re.MULTILINE,
    )
    match = row_re.search(text)
    if not match:
        fail(f"UPSTREAM.md has no mapping row for {tag}", errors)
        return None
    official_tag, official_commit, status = match.groups()
    if status not in ALLOWED_STATUSES:
        fail(f"UPSTREAM.md uses unsupported status {status!r} for {tag}", errors)
    if status in {"invalid", "withdrawn"}:
        fail(f"{tag} is marked {status} in UPSTREAM.md", errors)
    return official_tag, official_commit, status


def section_body(notes: str, heading_end: int) -> str:
    next_heading = re.search(r"^##\s+", notes[heading_end:], re.MULTILINE)
    end = heading_end + next_heading.start() if next_heading else len(notes)
    return notes[heading_end:end].strip()


def validate_notes(
    notes: str,
    tag: str,
    official_tag: str,
    official_commit: str,
    errors: list[str],
    *,
    require_subject: bool,
) -> None:
    if not notes.strip():
        fail("release notes are empty", errors)
        return

    if require_subject:
        subject = next(
            (line.strip() for line in notes.splitlines() if line.strip()),
            "",
        )
        expected_subject = f"Sub2API Plus {tag}"
        if subject != expected_subject:
            fail(
                "first non-empty release-notes line is "
                f"{subject!r}; expected {expected_subject!r}",
                errors,
            )

    matches: list[re.Match[str]] = []
    section_matches: dict[str, re.Match[str]] = {}
    for section in REQUIRED_NOTE_SECTIONS:
        found = list(
            re.finditer(
                rf"^##\s+{re.escape(section)}\s*$",
                notes,
                re.MULTILINE,
            )
        )
        if not found:
            fail(f"release notes are missing '## {section}'", errors)
            continue
        if len(found) > 1:
            fail(f"release notes contain duplicate '## {section}' headings", errors)
        match = found[0]
        matches.append(match)
        section_matches[section] = match
        if not section_body(notes, match.end()):
            fail(f"release-note section '## {section}' is empty", errors)

    if len(matches) == len(REQUIRED_NOTE_SECTIONS):
        positions = [match.start() for match in matches]
        if positions != sorted(positions):
            fail("required release-note sections are out of order", errors)

    upstream_heading = section_matches.get("Upstream baseline")
    if upstream_heading is not None:
        upstream_body = section_body(notes, upstream_heading.end())
        if f"Official release: {official_tag}" not in upstream_body:
            fail(
                "the '## Upstream baseline' section does not name official "
                f"release {official_tag}",
                errors,
            )
        if f"Official commit: {official_commit}" not in upstream_body:
            fail(
                "the '## Upstream baseline' section does not name official "
                f"commit {official_commit}",
                errors,
            )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", help="release tag; defaults to the embedded version")
    parser.add_argument(
        "--require-status",
        choices=sorted(ALLOWED_STATUSES),
        help="require the UPSTREAM.md row to have this exact status",
    )
    notes_group = parser.add_mutually_exclusive_group()
    notes_group.add_argument("--notes-file", type=Path)
    notes_group.add_argument("--notes-env")
    args = parser.parse_args()

    errors: list[str] = []
    embedded = (
        ROOT.joinpath("backend/cmd/server/VERSION")
        .read_text(encoding="utf-8")
        .strip()
    )
    tag = args.tag or f"v{embedded}"
    match = TAG_RE.fullmatch(tag)
    if not match:
        fail(f"invalid release tag {tag!r}; expected vX.Y.Z+custom.NNN", errors)
        version = tag.removeprefix("v")
    else:
        version, iteration = match.groups()
        if iteration == "000":
            fail("custom iteration must be between 001 and 999", errors)

    if embedded != version:
        fail(
            f"backend/cmd/server/VERSION is {embedded!r}, expected {version!r}",
            errors,
        )

    expected_arg = f"ARG VERSION={version}"
    for relative in ("Dockerfile", "backend/Dockerfile"):
        lines = ROOT.joinpath(relative).read_text(encoding="utf-8").splitlines()
        version_args = [
            line.strip()
            for line in lines
            if line.strip().startswith("ARG VERSION=")
        ]
        if not version_args:
            fail(f"{relative} has no ARG VERSION", errors)
        for value in version_args:
            if value != expected_arg:
                fail(f"{relative} contains {value!r}, expected {expected_arg!r}", errors)

    installer = ROOT.joinpath("deploy/install.sh").read_text(encoding="utf-8")
    for fragment in INSTALLER_VERSION_FRAGMENTS:
        if fragment not in installer:
            fail(
                "deploy/install.sh does not enforce the canonical three-digit "
                f"custom version pattern: {fragment}",
                errors,
            )

    mapping = parse_upstream(tag, errors)
    if mapping is not None:
        validate_required_status(tag, mapping[2], args.require_status, errors)

    validate_release_documentation(ROOT, errors)

    notes: str | None = None
    require_subject = False
    if args.notes_file:
        try:
            notes = args.notes_file.read_text(encoding="utf-8")
        except OSError as error:
            fail(f"cannot read release notes {args.notes_file}: {error}", errors)
        require_subject = True
    elif args.notes_env:
        notes = os.environ.get(args.notes_env, "")
    if notes is not None and mapping is not None:
        validate_notes(
            notes,
            tag,
            mapping[0],
            mapping[1],
            errors,
            require_subject=require_subject,
        )

    if errors:
        print("Release validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print(f"Release metadata is consistent for {tag}.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
