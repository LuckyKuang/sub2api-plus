#!/usr/bin/env python3
"""Require backend tests to declare a known Go build tag."""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BACKEND = ROOT / "backend"
BUILD_LINE = re.compile(r"^//go:build\s+(.+)$")
FUNC_TEST = re.compile(r"^func Test", re.M)
ALLOWED = {
    "unit",
    "integration",
    "unit || integration",
    "integration || unit",
    "unit || !integration",
    "e2e",
    "!e2e",
    "embed",
    "!unit",
    "!darwin",
}


def build_constraint(text: str) -> str | None:
    for line in text.splitlines()[:20]:
        stripped = line.strip()
        if stripped == "":
            continue
        match = BUILD_LINE.fullmatch(stripped)
        if match is not None:
            return " ".join(match.group(1).split())
        if stripped.startswith("//"):
            continue
        break
    return None


def violations_for(root: Path) -> list[str]:
    problems: list[str] = []
    for path in sorted(root.rglob("*_test.go")):
        text = path.read_text(encoding="utf-8")
        rel = path.relative_to(root).as_posix()
        constraint = build_constraint(text)
        if constraint is None:
            problems.append(f"{rel}: missing //go:build constraint")
            continue
        if constraint not in ALLOWED:
            problems.append(f"{rel}: unsupported //go:build {constraint}")
            continue
        if FUNC_TEST.search(text) is None:
            continue
        if constraint in {"!unit", "!darwin", "embed"}:
            continue
    return problems


def main() -> int:
    problems = violations_for(BACKEND)
    if problems:
        print("backend test files must declare a known //go:build tag:", file=sys.stderr)
        for problem in problems:
            print(f"  {problem}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
