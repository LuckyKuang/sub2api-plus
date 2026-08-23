#!/usr/bin/env python3
"""Validate the Sub2API Plus root AGENTS.md contract."""

from __future__ import annotations

import argparse
import re
from pathlib import Path
from typing import Sequence


ROOT = Path(__file__).resolve().parents[3]

REQUIRED_CATEGORIES = (
    "Scope",
    "Sources",
    "Dependencies",
    "Generated Code",
    "Interfaces",
    "Migrations",
    "Configuration",
    "Protocol Documentation",
    "README",
    "Locales",
    "Codex Identity",
    "Security Audit",
    "OpenSpec",
    "Secrets",
    "Documented Commands",
    "Implementation",
    "Design",
    "Verification",
    "Push",
    "Submit PR",
    "Release Promotion",
    "Release Notes",
    "Release Consistency",
    "Release Flow",
    "Publication Safety",
    "Upstream Merge",
    "Local Skill",
    "Skill Trigger",
)

REQUIRED_PATHS = (
    "backend/cmd/server/VERSION",
    "backend/go.mod",
    "frontend/package.json",
    "frontend/pnpm-lock.yaml",
    ".tool-versions",
    "CONTRIBUTING.md",
    "docs/RELEASING.md",
    "UPSTREAM.md",
    "backend/migrations/README.md",
    "deploy/",
    "README.md",
    "README_CN.md",
    "README_JA.md",
    "docs/SECURITY_AUDIT_CONTENT_COVERAGE.md",
    "skills/compress-cli",
    "skills/push-cli",
    "skills/release-cli",
)

PROTECTED_FRAGMENTS = {
    "Codex Identity": (
        "credentials.user_agent > valid global openai_codex_user_agent > compiled default",
        "Empty/invalid candidates fall through only to the next source",
        "Version sync may update only selected identity version declarations and must not change source, client family, Originator, OS, architecture, or terminal fingerprint",
        "Inbound headers, generic overrides, request classification, retries, probes, and upstream merges must not bypass precedence",
        "Keep User-Agent, Originator, and Version coherent",
        "source-priority matrix",
        "all outbound-path tests",
    ),
    "Security Audit": (
        "Ingress and content extraction are immutable security boundaries",
        "Every content-bearing HTTP/WS request or turn",
        "before account selection, billing, concurrency acquisition, upstream writes, or other side effects",
        "API-key/OAuth account type, session affinity",
        "routing, retries, probes, protocol adapters, transforms, request classification, and upstream merges must not bypass or weaken this boundary",
        "Content Moderation and Prompt Audit must consume the same canonical protocol extraction contract",
        "Only explicitly classified control-only frames may be no-content",
        "partial/incomplete extraction hidden by successful sibling content",
        "fail closed whenever a blocking audit mode is active",
        "docs/SECURITY_AUDIT_CONTENT_COVERAGE.md",
        "real-payload semantic tests for both engines",
        "HTTP/WS/account-type side-effect-order tests",
    ),
    "OpenSpec": (
        "cross-cutting public API",
        "security-boundary",
        "multi-module changes",
    ),
    "Secrets": (
        "Never commit credentials, tokens, production configuration, or user data",
    ),
    "Documented Commands": (
        "repository scripts or Make targets",
    ),
    "Push": (
        "skills/push-cli push",
        "Never target the repository default branch",
    ),
    "Submit PR": (
        "skills/push-cli submit-pr",
        "Host-side execution of that matrix is forbidden",
    ),
    "Release Promotion": (
        "skills/release-cli",
        "without admin bypass",
        "Release metadata validation must not repeat the complete local application matrix",
    ),
    "Release Flow": (
        "Never push or commit release changes directly to main",
        "Tag only the actual PR merge commit",
        "separate and resumable",
    ),
    "Publication Safety": (
        "without explicit publication request",
    ),
    "Local Skill": ("skills/compress-cli",),
    "Skill Trigger": ("Use compress-cli", "AGENTS.md"),
}

CATEGORY_RE = re.compile(r"^\|([^:|]+):(.+)$")


def parse_categories(lines: Sequence[str], errors: list[str]) -> dict[str, str]:
    categories: dict[str, str] = {}
    for line_number, line in enumerate(lines[1:], start=2):
        if not line:
            continue
        if not line.startswith("|"):
            errors.append(
                f"line {line_number} must start with '|': {line.strip() or '<blank>'}"
            )
            continue
        match = CATEGORY_RE.fullmatch(line)
        if match is None:
            errors.append(
                f"line {line_number} must use the '|Category:value' format"
            )
            continue
        category = match.group(1).strip()
        value = match.group(2).strip()
        if not category or not value:
            errors.append(f"line {line_number} has an empty category or value")
            continue
        if category in categories:
            errors.append(f"duplicate category '{category}' at line {line_number}")
            continue
        categories[category] = value
    return categories


def validate_source_paths(
    source_value: str,
    *,
    repo_root: Path,
    errors: list[str],
) -> None:
    for field in source_value.split("|"):
        if "=" not in field:
            continue
        label, raw_path = field.split("=", 1)
        source_path = raw_path.strip()
        candidate = Path(source_path)
        if not source_path:
            errors.append(f"source '{label.strip()}' has an empty path")
            continue
        if candidate.is_absolute() or ".." in candidate.parts:
            errors.append(
                f"source '{label.strip()}' must use a repository-relative path: {source_path}"
            )
            continue
        if not (repo_root / candidate).exists():
            errors.append(
                f"source '{label.strip()}' path does not exist: {source_path}"
            )


def validate_agents(path: Path, *, repo_root: Path = ROOT) -> list[str]:
    errors: list[str] = []
    if not path.is_file():
        return [f"file not found: {path}"]

    lines = path.read_text(encoding="utf-8").splitlines()
    if not lines:
        return ["AGENTS.md is empty"]
    if lines[0] != "# AGENTS.md":
        errors.append("first line must be exactly '# AGENTS.md'")

    categories = parse_categories(lines, errors)
    document = "\n".join(lines)

    for category in REQUIRED_CATEGORIES:
        if category not in categories:
            errors.append(f"missing required category '{category}'")

    for category, fragments in PROTECTED_FRAGMENTS.items():
        value = categories.get(category)
        if value is None:
            continue
        for fragment in fragments:
            if fragment not in value:
                errors.append(
                    f"category '{category}' is missing protected content: {fragment}"
                )

    for required_path in REQUIRED_PATHS:
        if required_path not in document:
            errors.append(f"missing required repository path: {required_path}")
        if not (repo_root / required_path).exists():
            errors.append(f"required repository path does not exist: {required_path}")

    source_value = categories.get("Sources")
    if source_value is not None:
        validate_source_paths(source_value, repo_root=repo_root, errors=errors)

    if categories.get("Local Skill") not in (None, "skills/compress-cli"):
        errors.append("category 'Local Skill' must be exactly 'skills/compress-cli'")

    return errors


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Validate the Sub2API Plus AGENTS.md contract."
    )
    subparsers = parser.add_subparsers(dest="action", required=True)
    check = subparsers.add_parser("check", help="validate without modifying files")
    check.add_argument("path", nargs="?", default="AGENTS.md", type=Path)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.action != "check":
        raise AssertionError(f"unsupported action: {args.action}")

    errors = validate_agents(args.path)
    if errors:
        for error in errors:
            print(f"FAIL: {error}")
        return 1
    print(f"PASS: {args.path} matches the Sub2API Plus AGENTS.md contract")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
