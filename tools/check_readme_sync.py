#!/usr/bin/env python3
"""Validate the shared structure, semantics, and links of the README files."""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
READMES = (ROOT / "README.md", ROOT / "README_CN.md", ROOT / "README_JA.md")
EXPECTED_SECTIONS = (
    "notice",
    "overview",
    "features",
    "quick-start",
    "deployment",
    "providers",
    "release-tags",
    "documentation",
    "license",
)
EXPECTED_CAPABILITIES = (
    "openai,anthropic,gemini,antigravity,grok,async-images,sora-unavailable"
)
EXPECTED_RELEASE_FORMAT = "vX.Y.Z+custom.NNN|vX.Y.Z-custom.NNN"
REQUIRED_LINKS = (
    "docs/README.md",
    "docs/RELEASING.md",
    "docs/providers/GROK.md",
    "docs/providers/SORA.md",
    "docs/providers/ANTIGRAVITY.md",
    "docs/protocols/OPENAI_RESPONSES.md",
    "docs/ASYNC_IMAGE_TASKS.md",
    "deploy/README.md",
    "deploy/DOCKER.md",
    "deploy/EDGE_SECURITY.md",
    "UPSTREAM.md",
    "CONTRIBUTING.md",
    "SECURITY.md",
)
SECTION_RE = re.compile(r"<!--\s*readme-section:([a-z0-9-]+)\s*-->")
CAPABILITY_RE = re.compile(r"<!--\s*readme-capabilities:([^>]+?)\s*-->")
RELEASE_FORMAT_RE = re.compile(r"<!--\s*readme-release-format:([^>]+?)\s*-->")
LINK_RE = re.compile(r"!?\[[^\]]*]\(([^)]+)\)")


def local_link_target(readme: Path, raw_target: str) -> Path | None:
    target = raw_target.strip().split(maxsplit=1)[0].strip("<>")
    if not target or target.startswith(("#", "http://", "https://", "mailto:")):
        return None
    path_text = target.split("#", 1)[0]
    if not path_text:
        return None
    return (readme.parent / path_text).resolve()


def main() -> int:
    errors: list[str] = []

    for readme in READMES:
        text = readme.read_text(encoding="utf-8")
        sections = tuple(SECTION_RE.findall(text))
        if sections != EXPECTED_SECTIONS:
            errors.append(
                f"{readme.name}: section IDs are {sections}, expected {EXPECTED_SECTIONS}"
            )

        capabilities = CAPABILITY_RE.findall(text)
        if capabilities != [EXPECTED_CAPABILITIES]:
            errors.append(
                f"{readme.name}: capability contract is {capabilities}, "
                f"expected one {EXPECTED_CAPABILITIES!r}"
            )

        release_formats = RELEASE_FORMAT_RE.findall(text)
        if release_formats != [EXPECTED_RELEASE_FORMAT]:
            errors.append(
                f"{readme.name}: release-format contract is {release_formats}, "
                f"expected one {EXPECTED_RELEASE_FORMAT!r}"
            )

        for required in REQUIRED_LINKS:
            if f"]({required})" not in text:
                errors.append(f"{readme.name}: missing required link {required}")

        for raw_target in LINK_RE.findall(text):
            target = local_link_target(readme, raw_target)
            if target is not None and not target.exists():
                errors.append(f"{readme.name}: broken local link {raw_target}")

    if errors:
        print("README synchronization check failed:", file=sys.stderr)
        for error in errors:
            print(f"- {error}", file=sys.stderr)
        return 1

    print("README structures, semantic contracts, and local links are synchronized.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
