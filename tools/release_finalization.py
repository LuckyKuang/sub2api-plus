#!/usr/bin/env python3
"""Classify and verify deterministic post-publication release finalization."""

from __future__ import annotations

import argparse
import os
import re
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


ROOT = Path(__file__).resolve().parents[1]
TAG_TEXT = r"v\d+\.\d+\.\d+\+custom\.(?:00[1-9]|0[1-9]\d|[1-9]\d{2})"
TAG_RE = re.compile(rf"^{TAG_TEXT}$")
MAPPING_ROW_RE = re.compile(
    rf"^(?P<prefix>\|\s*`(?P<tag>{TAG_TEXT})`\s*\|\s*`[^`]+`\s*\|\s*`[0-9a-f]{{40}}`\s*\|\s*)"
    r"(?P<status>[a-z]+)(?P<suffix>\s*\|)$",
    re.MULTILINE,
)
FINALIZATION_PREFIX = "release/finalize-"
FINALIZATION_ALLOWED_PATHS = frozenset(
    {
        "UPSTREAM.md",
        "README.md",
        "README_CN.md",
        "README_JA.md",
        "deploy/README.md",
    }
)
ZERO_SHA = "0" * 40


class ReleaseFinalizationError(RuntimeError):
    """A candidate is not the exact deterministic release finalization."""


@dataclass(frozen=True)
class FinalizationResult:
    tag: str
    base: str
    head: str
    paths: frozenset[str]


def display(command: Sequence[str]) -> str:
    return " ".join(str(item) for item in command)


def run_command(
    root: Path,
    command: Sequence[str],
    *,
    capture: bool = False,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(item) for item in command],
        cwd=root,
        check=False,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
        env=env,
    )


def capture(root: Path, command: Sequence[str]) -> str:
    result = run_command(root, command, capture=True)
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip()
        raise ReleaseFinalizationError(
            f"{display(command)} failed with exit code {result.returncode}"
            + (f": {detail[-2000:]}" if detail else "")
        )
    return (result.stdout or "").strip()


def git_output(root: Path, *args: str) -> str:
    return capture(root, ("git", *args))


def mapping_statuses(text: str) -> dict[str, str]:
    rows: dict[str, str] = {}
    for match in MAPPING_ROW_RE.finditer(text):
        tag = match.group("tag")
        if tag in rows:
            raise ReleaseFinalizationError(f"UPSTREAM.md repeats mapping row {tag}")
        rows[tag] = match.group("status")
    return rows


def transition_tag(base_text: str, head_text: str) -> str:
    base = mapping_statuses(base_text)
    head = mapping_statuses(head_text)
    transitions = [
        tag
        for tag, base_status in base.items()
        if base_status == "planned" and head.get(tag) == "published"
    ]
    if len(transitions) != 1:
        raise ReleaseFinalizationError(
            "release finalization must contain exactly one planned-to-published "
            f"mapping transition; found {len(transitions)}"
        )
    return transitions[0]


def candidate_transition_count(base_text: str, head_text: str) -> int:
    base = mapping_statuses(base_text)
    head = mapping_statuses(head_text)
    return sum(
        base_status == "planned" and head.get(tag) == "published"
        for tag, base_status in base.items()
    )


def finalization_branch(tag: str) -> str:
    if TAG_RE.fullmatch(tag) is None:
        raise ReleaseFinalizationError(f"invalid custom release tag: {tag}")
    return FINALIZATION_PREFIX + tag.removeprefix("v").replace("+", "-")


def tag_from_branch(branch: str) -> str:
    if not branch.startswith(FINALIZATION_PREFIX):
        raise ReleaseFinalizationError(
            f"release-finalization branch must start with {FINALIZATION_PREFIX}"
        )
    suffix = branch.removeprefix(FINALIZATION_PREFIX)
    match = re.fullmatch(
        r"(\d+\.\d+\.\d+)-custom\.(00[1-9]|0[1-9]\d|[1-9]\d{2})",
        suffix,
    )
    if match is None:
        raise ReleaseFinalizationError(
            f"invalid deterministic release-finalization branch: {branch}"
        )
    tag = f"v{match.group(1)}+custom.{match.group(2)}"
    if finalization_branch(tag) != branch:
        raise ReleaseFinalizationError(
            f"release-finalization branch does not round-trip to {tag}"
        )
    return tag


def read_blob(root: Path, commit: str, path: str) -> str:
    return git_output(root, "show", f"{commit}:{path}") + "\n"


def replace_planned_mapping(text: str, tag: str) -> str:
    row = re.compile(
        rf"^(\|\s*`{re.escape(tag)}`\s*\|\s*`[^`]+`\s*\|\s*`[0-9a-f]{{40}}`\s*\|\s*)"
        r"planned(\s*\|)$",
        re.MULTILINE,
    )
    updated, replacements = row.subn(r"\1published\2", text)
    if replacements != 1:
        raise ReleaseFinalizationError(
            f"UPSTREAM.md has {replacements} planned mapping rows for {tag}; expected one"
        )
    return updated


def require_main_merge_topology(root: Path, base: str, head: str) -> None:
    fields = git_output(root, "rev-list", "--parents", "-n", "1", head).split()
    if len(fields) != 3:
        raise ReleaseFinalizationError(
            "focused main finalization requires exactly one two-parent merge commit"
        )
    if fields[1] != base:
        raise ReleaseFinalizationError(
            f"main finalization first parent is {fields[1]}, expected {base}"
        )
    merge_tree = git_output(root, "rev-parse", f"{head}^{{tree}}")
    second_parent_tree = git_output(root, "rev-parse", f"{fields[2]}^{{tree}}")
    if merge_tree != second_parent_tree:
        raise ReleaseFinalizationError(
            "main finalization merge tree differs from the submitted head tree"
        )


def regenerate_expected_tree(
    root: Path,
    *,
    base: str,
    tag: str,
) -> tuple[str, frozenset[str]]:
    with tempfile.TemporaryDirectory(prefix="sub2api-release-finalization-") as temp:
        worktree = Path(temp) / "worktree"
        added = False
        add = run_command(
            root,
            ("git", "worktree", "add", "--detach", str(worktree), base),
            capture=True,
        )
        if add.returncode != 0:
            detail = (add.stderr or add.stdout or "").strip()
            raise ReleaseFinalizationError(
                "unable to create finalization verification worktree"
                + (f": {detail[-2000:]}" if detail else "")
            )
        added = True
        try:
            upstream = worktree / "UPSTREAM.md"
            original = upstream.read_text(encoding="utf-8")
            upstream.write_text(
                replace_planned_mapping(original, tag),
                encoding="utf-8",
            )
            env = dict(os.environ)
            env["PYTHONDONTWRITEBYTECODE"] = "1"
            update = run_command(
                worktree,
                (sys.executable, "tools/update_release_docs.py"),
                capture=True,
                env=env,
            )
            if update.returncode != 0:
                detail = (update.stderr or update.stdout or "").strip()
                raise ReleaseFinalizationError(
                    "release documentation regeneration failed"
                    + (f": {detail[-2000:]}" if detail else "")
                )
            capture(worktree, ("git", "add", "--all"))
            paths = frozenset(
                filter(
                    None,
                    git_output(worktree, "diff", "--cached", "--name-only").splitlines(),
                )
            )
            unexpected = paths - FINALIZATION_ALLOWED_PATHS
            if "UPSTREAM.md" not in paths or unexpected:
                raise ReleaseFinalizationError(
                    "deterministic finalization generated invalid paths: "
                    + (", ".join(sorted(paths)) if paths else "<none>")
                )
            return git_output(worktree, "write-tree"), paths
        finally:
            if added:
                remove = run_command(
                    root,
                    ("git", "worktree", "remove", "--force", str(worktree)),
                    capture=True,
                )
                if remove.returncode != 0:
                    run_command(root, ("git", "worktree", "prune"), capture=True)


def validate_finalization(
    root: Path,
    *,
    base: str,
    head: str,
    expected_tag: str | None = None,
    branch: str | None = None,
    require_main_merge: bool = False,
) -> FinalizationResult:
    base_commit = git_output(root, "rev-parse", f"{base}^{{commit}}")
    head_commit = git_output(root, "rev-parse", f"{head}^{{commit}}")
    ancestor = run_command(
        root,
        ("git", "merge-base", "--is-ancestor", base_commit, head_commit),
        capture=True,
    )
    if ancestor.returncode != 0:
        raise ReleaseFinalizationError(
            f"finalization head {head_commit} does not contain base {base_commit}"
        )
    if require_main_merge:
        require_main_merge_topology(root, base_commit, head_commit)

    base_text = read_blob(root, base_commit, "UPSTREAM.md")
    head_text = read_blob(root, head_commit, "UPSTREAM.md")
    tag = transition_tag(base_text, head_text)
    if expected_tag is not None and tag != expected_tag:
        raise ReleaseFinalizationError(
            f"finalization changed {tag}, expected published tag {expected_tag}"
        )
    if branch is not None and branch != finalization_branch(tag):
        raise ReleaseFinalizationError(
            f"finalization branch is {branch}, expected {finalization_branch(tag)}"
        )

    expected_tree, paths = regenerate_expected_tree(root, base=base_commit, tag=tag)
    head_tree = git_output(root, "rev-parse", f"{head_commit}^{{tree}}")
    if expected_tree != head_tree:
        changed = git_output(root, "diff", "--name-only", base_commit, head_commit)
        raise ReleaseFinalizationError(
            "finalization head tree is not the deterministic generated result; "
            f"changed paths: {changed or '<none>'}"
        )
    return FinalizationResult(tag=tag, base=base_commit, head=head_commit, paths=paths)


def write_github_output(path: Path | None, *, profile: str, tag: str = "") -> None:
    if path is None:
        return
    with path.open("a", encoding="utf-8") as output:
        output.write(f"profile={profile}\n")
        output.write(f"tag={tag}\n")


def classify(
    root: Path,
    *,
    base: str | None,
    head: str | None,
    branch: str,
    default_branch: str,
    event: str,
) -> FinalizationResult | None:
    if not base or not head or base == ZERO_SHA:
        return None
    if event not in {"push", "pull_request"}:
        return None

    base_text = read_blob(root, base, "UPSTREAM.md")
    head_text = read_blob(root, head, "UPSTREAM.md")
    transitions = candidate_transition_count(base_text, head_text)
    is_named_finalization = branch.startswith(FINALIZATION_PREFIX)
    is_main_push = event == "push" and branch == default_branch

    if not is_named_finalization and not is_main_push:
        if transitions:
            raise ReleaseFinalizationError(
                "planned-to-published release metadata may change only on the "
                "deterministic finalization branch"
            )
        return None
    if not transitions and not is_named_finalization:
        return None

    expected_tag = tag_from_branch(branch) if is_named_finalization else None
    return validate_finalization(
        root,
        base=base,
        head=head,
        expected_tag=expected_tag,
        branch=branch if is_named_finalization else None,
        require_main_merge=is_main_push,
    )


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Classify or strictly validate release finalization commits."
    )
    parser.add_argument("action", choices=("classify", "validate"))
    parser.add_argument("--base", required=True)
    parser.add_argument("--head", required=True)
    parser.add_argument("--tag")
    parser.add_argument("--branch", default="")
    parser.add_argument("--default-branch", default="main")
    parser.add_argument("--event", default="pull_request")
    parser.add_argument("--main-merge", action="store_true")
    parser.add_argument("--github-output", type=Path)
    args = parser.parse_args()

    try:
        if args.action == "validate":
            result = validate_finalization(
                ROOT,
                base=args.base,
                head=args.head,
                expected_tag=args.tag,
                branch=args.branch or None,
                require_main_merge=args.main_merge,
            )
        else:
            result = classify(
                ROOT,
                base=args.base,
                head=args.head,
                branch=args.branch,
                default_branch=args.default_branch,
                event=args.event,
            )
    except (OSError, ReleaseFinalizationError) as error:
        print(f"Release finalization validation failed: {error}", file=sys.stderr)
        return 1

    if result is None:
        write_github_output(args.github_output, profile="full")
        print("Validation profile: full")
        return 0

    write_github_output(
        args.github_output,
        profile="release-finalization",
        tag=result.tag,
    )
    print(
        f"Validation profile: release-finalization ({result.tag}); "
        f"paths: {', '.join(sorted(result.paths))}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
