#!/usr/bin/env python3
"""Validate migration filenames and changes introduced since a Git base."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

import release_docs


ROOT = Path(__file__).resolve().parents[1]
MIGRATIONS = ROOT / "backend/migrations"
NAME_RE = re.compile(r"^(\d{3})(?:[a-z])?_[a-z0-9_]+(?:_notx)?\.sql$")
ZERO_SHA_RE = re.compile(r"^0+$")

# These independently committed feature, official-agent and Gemini migrations
# predate their main merges. Their deployed filenames cannot be renumbered.
# Pin both path and original Git blob; no other old or duplicate prefix is exempt.
HISTORICAL_MERGED_MIGRATIONS = {
    "backend/migrations/287_channel_monitor_stream_probe.sql":
        "89bda598c1d0183e1f98e7b191de7aa3123b1e7b",
    "backend/migrations/288_openai_session_routes.sql":
        "e0e9c2ed9a502a142bb5278fddd2c4ebfbbefc11",
    "backend/migrations/287_gemini_cached_contents.sql":
        "d3cbda27dfd25d1004f54982021e9152437d224c",
    "backend/migrations/288_gemini_cached_contents_channel_usage.sql":
        "c72f097a7303166d68cdb7b57b0d77bd7737db3e",
    "backend/migrations/289_gemini_cached_contents_mutation_lease.sql":
        "e0970efebf75d763271032b7157c6a2eafa2b8ad",
    "backend/migrations/290_codex_native_identity_and_manual_versions.sql":
        "2a26822f3224fefb23ececa1b5af7a5fe0367450",
    "backend/migrations/291_pin_grok_native_agent.sql":
        "18678cb403110e47a06ae063f0521b61d1bd4252",
    "backend/migrations/292_deepseek_harness_anonymous_user.sql":
        "4859310b1844919553b774e1b8837f860120f04a",
    "backend/migrations/293_zcode_project_tokens_memory_only.sql":
        "20aed0f087e2906485d353f33b79e63fb78a1238",
    "backend/migrations/294_outbound_identity_ids.sql":
        "765a7fe50a291d4e18a74e01cb96ad0c44dd790d",
    "backend/migrations/295_channel_monitor_v2_model_visibility.sql":
        "ae727c64fd1ba4e28a87e4544f9a63aebbb3c765",
    "backend/migrations/295_enable_gemini_image_generation_groups.sql":
        "ae37f206fce3632de6738b3fc83e7fc45c862580",
}


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


def resolve_release_base(
    target: str,
    *,
    tags: list[str] | None = None,
    statuses: dict[str, str] | None = None,
) -> str:
    if release_docs.version_key(target) is None:
        raise ValueError(
            f"invalid target release {target!r}; expected vX.Y.Z+custom.NNN"
        )
    if statuses is None:
        statuses = release_docs.parse_upstream_statuses(
            ROOT.joinpath("UPSTREAM.md").read_text(encoding="utf-8")
        )
    if target not in statuses:
        raise ValueError(f"UPSTREAM.md has no mapping row for {target}")
    if tags is None:
        tags = git("tag", "--merged", "HEAD", "--list").splitlines()
    base = release_docs.select_previous_release_tag(tags, statuses, target)
    if base is None:
        raise ValueError(
            f"no earlier published or historical release tag is available for {target}"
        )
    return base


def main() -> int:
    parser = argparse.ArgumentParser()
    comparison = parser.add_mutually_exclusive_group()
    comparison.add_argument("--base", help="Git base SHA/ref for new-file validation")
    comparison.add_argument(
        "--target-release",
        help="release tag whose previous published/historical tag is the Git base",
    )
    args = parser.parse_args()
    errors: list[str] = []

    for path in sorted(MIGRATIONS.glob("*.sql")):
        if not NAME_RE.fullmatch(path.name):
            errors.append(f"{path.relative_to(ROOT).as_posix()}: invalid filename")

    base = (args.base or "").strip()
    if args.target_release:
        try:
            base = resolve_release_base(args.target_release)
        except (OSError, subprocess.CalledProcessError, ValueError) as error:
            print(f"Cannot resolve migration comparison base: {error}", file=sys.stderr)
            return 1
        print(f"Migration comparison base for {args.target_release}: {base}")
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
    new_migrations: list[tuple[Path, int]] = []
    historical_count = 0
    for path in added:
        prefix = validate_added(path, errors)
        if prefix is None:
            continue
        relative = path.relative_to(ROOT).as_posix()
        if expected_blob := HISTORICAL_MERGED_MIGRATIONS.get(relative):
            actual_blob = git("hash-object", f"--path={relative}", str(path)).strip()
            if actual_blob != expected_blob:
                errors.append(f"{relative}: historical migration differs from pinned content")
            else:
                historical_count += 1
                max_base = max(max_base, prefix)
            continue
        new_migrations.append((path, prefix))
    for path, prefix in new_migrations:
        if prefix <= max_base:
            errors.append(
                f"{path.relative_to(ROOT).as_posix()}: prefix {prefix:03d} must be "
                f"greater than existing maximum {max_base:03d}"
            )
    new_prefixes = [prefix for _, prefix in new_migrations]
    if len(new_prefixes) != len(set(new_prefixes)):
        errors.append("new migrations contain duplicate numeric prefixes")

    if errors:
        print("Migration validation failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print(
        f"Migration changes are valid ({len(new_prefixes)} new SQL file(s), "
        f"{historical_count} unchanged historical file(s))."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
