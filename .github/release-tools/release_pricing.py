#!/usr/bin/env python3
"""Generate and compare immutable pricing assets; only publish mode may upload."""

from __future__ import annotations
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
from urllib.parse import quote

ASSETS = ("model-pricing.json", "model-pricing-manifest.json")


def command(argv):
    result = subprocess.run(argv, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError("pricing asset command failed")
    return result.stdout


def release_metadata(repository, tag, *, allow_missing):
    result = subprocess.run(
        ["gh", "api", f"repos/{repository}/releases/tags/{quote(tag, safe='')}"],
        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    if result.returncode:
        if allow_missing and "HTTP 404" in result.stderr:
            # Distinguish a missing public release from inaccessible repository metadata.
            command(["gh", "api", f"repos/{repository}"])
            return None
        raise RuntimeError("cannot reliably read existing release assets")
    data = json.loads(result.stdout)
    if not isinstance(data, dict) or not isinstance(data.get("assets"), list):
        raise ValueError("invalid release asset response")
    if data.get("draft") or data.get("prerelease"):
        raise ValueError("pricing comparison requires a non-draft stable release")
    return data


def generate(repository, tag, output):
    output.mkdir(parents=True, exist_ok=True)
    shutil.copyfile("backend/resources/model-pricing/model_prices_and_context_window.json",
                    output / ASSETS[0])
    command(["go", "-C", "backend", "run", "./cmd/pricing-manifest-build",
             "--data", str(output / ASSETS[0]), "--repository", repository,
             "--tag", tag, "--manifest", str(output / ASSETS[1])])


def compare(repository, tag, mode, directory, *, upload=True):
    metadata = release_metadata(repository, tag, allow_missing=mode != "tag-rehearsal")
    names = {asset.get("name") for asset in metadata["assets"]} if metadata else set()
    outcomes = {}
    uploads = []
    existing = directory / "existing"
    existing.mkdir(exist_ok=True)
    for name in ASSETS:
        if name in names:
            command(["gh", "release", "download", tag, "--repo", repository,
                     "--pattern", name, "--dir", str(existing)])
            if (existing / name).read_bytes() != (directory / name).read_bytes():
                raise ValueError("Refusing to replace immutable pricing asset: " + name)
            outcomes[name] = "matched"
        else:
            if mode == "tag-rehearsal":
                raise ValueError("published-tag rehearsal requires both pricing assets")
            outcomes[name] = "absent-untested" if mode == "branch-rehearsal" else "new"
            uploads.append(str(directory / name) + "#" + name)
    if mode == "publish" and uploads and upload:
        if os.environ.get("RELEASE_MODE") != "publish":
            raise ValueError("pricing upload requires verified publication mode")
        command(["gh", "release", "upload", tag, "--repo", repository, *uploads])
    (directory / "comparison.json").write_text(json.dumps({
        "mode": mode, "tag": tag, "outcomes": outcomes,
    }, indent=2) + "\n")
    return outcomes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("generate", "compare", "status"))
    parser.add_argument("--repository", required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--mode", choices=("publish", "tag-rehearsal", "branch-rehearsal"), required=True)
    parser.add_argument("--directory", type=Path, default=Path(".release-pricing"))
    args = parser.parse_args()
    if args.action == "generate":
        generate(args.repository, args.tag, args.directory)
    elif args.action == "compare":
        compare(args.repository, args.tag, args.mode, args.directory)
    else:
        data = release_metadata(args.repository, args.tag, allow_missing=True)
        print("true" if data else "false")

if __name__ == "__main__":
    main()
