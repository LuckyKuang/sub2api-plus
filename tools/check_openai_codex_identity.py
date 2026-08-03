#!/usr/bin/env python3
"""Reject obsolete upstream Codex identity literals in production Go code."""

from __future__ import annotations

from pathlib import Path
import sys


ROOT = Path(__file__).resolve().parents[1]
SOURCE_ROOT = ROOT / "backend"
FORBIDDEN_LITERALS = ("codex-cli/0.91.0",)


def main() -> int:
    violations: list[str] = []
    for path in SOURCE_ROOT.rglob("*.go"):
        if path.name.endswith("_test.go"):
            continue
        try:
            content = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            violations.append(f"{path.relative_to(ROOT)}: source is not valid UTF-8")
            continue
        for literal in FORBIDDEN_LITERALS:
            if literal in content:
                violations.append(f"{path.relative_to(ROOT)}: forbidden obsolete Codex UA {literal!r}")

    if violations:
        print("OpenAI Codex outbound identity check failed:", file=sys.stderr)
        print("\n".join(violations), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
