#!/usr/bin/env python3
"""Update README installation and rollback versions from release metadata."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import release_docs


ROOT = Path(__file__).resolve().parents[1]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--check",
        action="store_true",
        help="check for stale release documentation without writing files",
    )
    args = parser.parse_args()

    try:
        current_tag, rollback_tag, updates = release_docs.generate_release_doc_updates(
            ROOT
        )
    except (OSError, release_docs.ReleaseDocsError) as error:
        print(f"Release documentation update failed: {error}", file=sys.stderr)
        return 1

    changed = [
        path
        for path, updated in updates.items()
        if path.read_text(encoding="utf-8") != updated
    ]
    if args.check:
        if changed:
            print("Release documentation is stale:", file=sys.stderr)
            for path in changed:
                print(f"- {path.relative_to(ROOT)}", file=sys.stderr)
            print(
                "Run: python3 tools/update_release_docs.py",
                file=sys.stderr,
            )
            return 1
    else:
        for path in changed:
            path.write_text(updates[path], encoding="utf-8")

    action = "checked" if args.check else "updated"
    print(
        f"Release documentation {action}: install {current_tag}; "
        f"rollback {rollback_tag}; changed {len(changed)} file(s)."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
