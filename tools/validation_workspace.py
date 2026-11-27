#!/usr/bin/env python3
"""Exact-commit, disposable Linux workspace for WSL container validation."""

from __future__ import annotations

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import shutil
import statistics
import subprocess
import tempfile
import time
from typing import Iterator, Sequence

import validation_runtime


def git(root: Path, *args: str) -> str:
    result = subprocess.run(["git", "-C", str(root), *args], check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return result.stdout.decode("utf-8").strip()


@contextmanager
def snapshot(source: Path, head: str, base: str) -> Iterator[Path]:
    """Check out the exact proof inputs; all writable state dies with the container.

    Object sharing is read-only. The clone has its own index, refs and worktree;
    it never copies uncommitted source or reuses a historical writable snapshot.
    """
    source = source.resolve()
    if git(source, "rev-parse", "HEAD") != head or git(source, "status", "--porcelain=v1", "--untracked-files=all"):
        raise RuntimeError("validation source must be clean and match the recorded HEAD")
    branch = git(source, "branch", "--show-current")
    if not branch:
        raise RuntimeError("validation source must be on a working branch")
    # Go's default build identity includes absolute source paths. Keep the path
    # stable across containers while creating a new private tree every time.
    # Different sources (including nested test fixtures) get separate locations.
    identity = hashlib.sha256(os.fsencode(source)).hexdigest()[:16]
    directory = Path(tempfile.gettempdir()) / f"sub2api-validation-source-{identity}"
    directory.mkdir(mode=0o700)  # Existing/active/stale paths must never be reused.
    try:
        target = directory / "repo"
        subprocess.run(["git", "clone", "--quiet", "--shared", "--no-checkout",
                        "--", str(source), str(target)], check=True)
        # Checkout conversion is part of the source representation. In
        # particular, preserve Windows CRLF inputs and deterministic cache IDs
        # instead of measuring different bytes on the two filesystems.
        for name, default in (("core.autocrlf", "false"), ("core.eol", "native")):
            value = git(source, "config", "--get", "--default", default, name)
            git(target, "config", name, value)
        git(target, "checkout", "--quiet", "-B", branch, head)
        git(target, "cat-file", "-e", f"{base}^{{commit}}")
        if git(target, "rev-parse", "HEAD") != head or git(target, "status", "--porcelain=v1"):
            raise RuntimeError("snapshot checkout differs from the validation proof")
        modules = source / "frontend/node_modules"
        if modules.is_dir():
            # The launcher mounts this dependency generation on Linux storage.
            # Sharing dependencies does not share source/build state.
            (target / "frontend/node_modules").symlink_to(modules, target_is_directory=True)
        print(json.dumps({"event": "validation_workspace", "head": head, "base": base,
                          "tree": git(target, "rev-parse", "HEAD^{tree}"),
                          "storage": "container-linux-filesystem"}), flush=True)
        yield target
    finally:
        shutil.rmtree(directory)


def scan(root: Path, paths: Sequence[str]) -> dict[str, object]:
    started = time.monotonic()
    digest = hashlib.sha256()
    byte_count = 0
    for relative in paths:
        path = root / relative
        content = os.readlink(path).encode() if path.is_symlink() else path.read_bytes()
        byte_count += len(content)
        digest.update(relative.encode())
        digest.update(b"\0")
        digest.update(content)
    return {"seconds": time.monotonic() - started, "files": len(paths),
            "bytes": byte_count, "sha256": digest.hexdigest()}


def benchmark(source: Path, head: str, base: str, *, rounds: int = 3) -> dict[str, object]:
    if rounds < 3:
        raise ValueError("I/O comparison requires at least three paired rounds")
    paths = git(source, "ls-files", "-z").rstrip("\0").split("\0")
    measurements: dict[str, list[dict[str, object]]] = {"bind": [], "linux": []}
    with snapshot(source, head, base) as target:
        preparation_started = time.monotonic()
        representation_files = 0
        # A clean Windows tree can contain a mix of LF and CRLF even when
        # core.autocrlf=true. Match those representations only for the I/O
        # experiment; reject every difference beyond checkout line endings.
        for relative in paths:
            original, checked_out = source / relative, target / relative
            if original.is_symlink() or checked_out.is_symlink():
                if not (original.is_symlink() and checked_out.is_symlink()
                        and os.readlink(original) == os.readlink(checked_out)):
                    raise RuntimeError("I/O comparison contains differing symlinks")
                continue
            content, candidate = original.read_bytes(), checked_out.read_bytes()
            if content != candidate:
                if b"\0" in content or content.replace(b"\r\n", b"\n") != candidate.replace(b"\r\n", b"\n"):
                    raise RuntimeError("I/O comparison differs beyond checkout line endings")
                checked_out.write_bytes(content)
                representation_files += 1
        preparation_seconds = time.monotonic() - preparation_started
        for index in range(rounds):
            # Alternate order to reduce the advantage of warming the other side.
            order = (("bind", source), ("linux", target))
            for name, root in order if index % 2 == 0 else reversed(order):
                measurement = scan(root, paths)
                measurements[name].append(measurement)
                print(json.dumps({"event": "io_sample", "storage": name,
                                  "round": index + 1, **measurement}), flush=True)
            pair = [measurements[name][-1]["sha256"] for name in ("bind", "linux")]
            if pair[0] != pair[1]:
                raise RuntimeError("I/O comparison source content changed or differs")
    digests = {item["sha256"] for samples in measurements.values() for item in samples}
    if len(digests) != 1:
        raise RuntimeError("I/O comparison source content changed or differs")
    medians = {name: statistics.median(float(item["seconds"]) for item in samples)
               for name, samples in measurements.items()}
    return {"head": head, "base": base, "rounds": rounds, "samples": measurements,
            "median_seconds": medians, "bind_to_linux_ratio": medians["bind"] / medians["linux"],
            "preparation_seconds": preparation_seconds,
            "checkout_representation_files": representation_files,
            "cache_scope": "parity preparation reads both trees before repeated reads; OS caches are not flushed"}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("run", "benchmark"))
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--head", required=True)
    parser.add_argument("--base", required=True)
    parser.add_argument("--report", type=Path)
    args, argv = parser.parse_known_args()
    validation_runtime.require_in_validation(tool="Validation workspace")
    source = args.source.resolve()
    if args.action == "benchmark":
        if argv:
            parser.error("benchmark does not accept a child command")
        report = benchmark(source, args.head, args.base)
        print(json.dumps(report, sort_keys=True), flush=True)
        if args.report:
            args.report.parent.mkdir(parents=True, exist_ok=True)
            args.report.write_text(json.dumps(report, indent=2) + "\n")
        return 0
    argv = argv[1:] if argv[:1] == ["--"] else argv
    if not argv:
        parser.error("run requires a container command")
    with snapshot(source, args.head, args.base) as target:
        prefix = str(source) + "/"
        command = [str(target) if item == str(source) else
                   str(target) + "/" + item[len(prefix):] if item.startswith(prefix) else item
                   for item in argv]
        return subprocess.run(command, cwd=target).returncode


if __name__ == "__main__":
    raise SystemExit(main())
