#!/usr/bin/env python3
"""Stage workflow validation inputs separately from historical application files."""
from pathlib import Path
import argparse
import shutil

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tooling", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    root = Path.cwd()
    for source, destination in [
        (root / "backend/go.mod", "backend/go.mod"),
        (root / "backend/go.sum", "backend/go.sum"),
        (root / "frontend/package.json", "frontend/package.json"),
        (root / "frontend/pnpm-lock.yaml", "frontend/pnpm-lock.yaml"),
        (args.tooling / "validation/Dockerfile.validation", "deploy/Dockerfile.validation"),
        (args.tooling / "validation/tool-versions", ".tool-versions"),
        (args.tooling / "release-tools/requirements-release.txt",
         ".github/release-tools/requirements-release.txt"),
    ]:
        target = args.output / destination
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, target)

if __name__ == "__main__":
    main()
