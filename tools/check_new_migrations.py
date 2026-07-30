#!/usr/bin/env python3
"""Validate migration filenames and changes introduced since a Git base."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "backend/migrations"
NAME_RE = re.compile(r"^(\d{3})(?:[a-z])?_[a-z0-9_]+(?:_notx)?\.sql$")
ZERO_SHA_RE = re.compile(r"^0+$")


def git(*args: str) -> str:
    return subprocess.check_output(
        ["git", *args], cwd=ROOT, text=True, encoding="utf-8"
    )


def validate_added(path: Path, errors: list[str]) -> int | None:
    match = NAME_RE.fullmatch(path.name)
    if not match:
        errors.append(
            f"{path.as_posix()}: expected NNN_description.sql or "
            "NNN_description_notx.sql"
        )
        return None

    content = path.read_text(encoding="utf-8")
    upper = content.upper()
    if path.name.endswith("_notx.sql"):
        if "CONCURRENTLY" not in upper:
            errors.append(f"{path.as_posix()}: _notx migration has no CONCURRENTLY")
        if re.search(r"\b(BEGIN|COMMIT|ROLLBACK)\b", upper):
            errors.append(f"{path.as_posix()}: _notx migration controls transactions")
        for statement in (part.strip() for part in content.split(";")):
            normalized = re.sub(r"--.*?$", "", statement, flags=re.MULTILINE).upper()
            if not normalized.strip():
                continue
            if "CONCURRENTLY" not in normalized:
                errors.append(
                    f"{path.as_posix()}: _notx migration mixes non-concurrent SQL"
                )
            if "CREATE" in normalized and "INDEX" in normalized:
                if "IF NOT EXISTS" not in normalized:
                    errors.append(
                        f"{path.as_posix()}: concurrent CREATE INDEX needs IF NOT EXISTS"
                    )
            elif "DROP" in normalized and "INDEX" in normalized:
                if "IF EXISTS" not in normalized:
                    errors.append(
                        f"{path.as_posix()}: concurrent DROP INDEX needs IF EXISTS"
                    )
            else:
                errors.append(
                    f"{path.as_posix()}: _notx only supports CREATE/DROP INDEX"
                )
    elif "CONCURRENTLY" in upper:
        errors.append(f"{path.as_posix()}: CONCURRENTLY requires _notx.sql")

    return int(match.group(1))


def diff_changes(base: str) -> list[tuple[str, str]]:
    output = git(
        "diff",
        "--name-status",
        "--diff-filter=ADMRT",
        f"{base}...HEAD",
        "--",
        "backend/migrations",
    )
    changes: list[tuple[str, str]] = []
    for line in output.splitlines():
        fields = line.split("\t")
        if len(fields) >= 2:
            changes.append((fields[0], fields[-1]))
    return changes


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", help="Git base SHA/ref for new-file validation")
    args = parser.parse_args()
    errors: list[str] = []

    for path in sorted(MIGRATIONS.glob("*.sql")):
        if not NAME_RE.fullmatch(path.name):
            errors.append(f"{path.relative_to(ROOT).as_posix()}: invalid filename")

    base = (args.base or "").strip()
    if not base or ZERO_SHA_RE.fullmatch(base):
        if errors:
            for error in errors:
                print(f"- {error}", file=sys.stderr)
            return 1
        print("Migration filenames are valid; no Git base was provided.")
        return 0

    try:
        git("rev-parse", "--verify", f"{base}^{{commit}}")
        changes = diff_changes(base)
    except subprocess.CalledProcessError:
        print(f"Cannot resolve migration comparison base {base!r}.", file=sys.stderr)
        return 1

    added: list[Path] = []
    for status, relative in changes:
        if not relative.endswith(".sql"):
            continue
        if status == "A":
            added.append(ROOT / relative)
        else:
            errors.append(f"{relative}: existing migrations may not be {status}")

    base_files = git("ls-tree", "-r", "--name-only", base, "backend/migrations")
    base_prefixes = [
        int(match.group(1))
        for name in base_files.splitlines()
        if (match := NAME_RE.fullmatch(Path(name).name))
    ]
    max_base = max(base_prefixes, default=0)
    new_prefixes: list[int] = []
    for path in added:
        prefix = validate_added(path, errors)
        if prefix is None:
            continue
        new_prefixes.append(prefix)
        if prefix <= max_base:
            errors.append(
                f"{path.relative_to(ROOT).as_posix()}: prefix {prefix:03d} must be "
                f"greater than existing maximum {max_base:03d}"
            )
    if len(new_prefixes) != len(set(new_prefixes)):
        errors.append("new migrations contain duplicate numeric prefixes")

    if errors:
        print("Migration validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print(f"Migration changes are valid ({len(added)} new SQL file(s)).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
