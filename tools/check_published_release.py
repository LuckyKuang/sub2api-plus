#!/usr/bin/env python3
"""Read-only CI verification for an already published immutable release."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RELEASE_CLI = ROOT / "skills" / "release-cli" / "scripts"
if str(RELEASE_CLI) not in sys.path:
    sys.path.insert(0, str(RELEASE_CLI))

import release_cli


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Verify a published release without requesting mutation rights."
    )
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    args = parser.parse_args()

    if args.repository != release_cli.EXPECTED_REPOSITORY:
        parser.error(
            f"--repository must be {release_cli.EXPECTED_REPOSITORY}"
        )
    try:
        release_cli.validate_tag(args.tag)
        published = release_cli.require_published_remote_tag(
            args.repository,
            args.tag,
        )
        release_cli.require_release_workflow_success(
            args.repository,
            args.tag,
            published.target,
        )
        release_cli.verify_release(args.repository, args.tag)
    except release_cli.ReleaseCliError as error:
        print(f"Published release verification failed: {error}", file=sys.stderr)
        return 1

    print(
        f"Published release verified: {args.tag} at {published.target}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
