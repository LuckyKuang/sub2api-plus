#!/usr/bin/env python3
"""Container-only CI execution; the host handles runtime and metadata only."""

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
from typing import Sequence

import validation_checks
import validation_runtime as runtime


ROOT = Path(os.environ.get("SUB2API_VALIDATION_ROOT", str(Path(__file__).resolve().parents[1]))).resolve()
INPUT_ROOT = Path(os.environ.get("SUB2API_VALIDATION_INPUT_ROOT", str(ROOT))).resolve()
RELEASE_ENV_NAMES = (
    "RELEASE_VERSION", "RELEASE_SHA", "RELEASE_DATE", "RELEASE_TAG", "RELEASE_MODE",
    "GORELEASER_CURRENT_TAG", "GITHUB_REPOSITORY", "GITHUB_REPOSITORY_OWNER",
    "GITHUB_REPO_OWNER", "GITHUB_REPO_OWNER_LOWER", "GITHUB_REPO_NAME",
    "DOCKER_TAG_VERSION", "DOCKERHUB_USERNAME", "TAG_MESSAGE", "RUNNER_TEMP",
    "DRY_RUN", "GH_TOKEN", "GITHUB_TOKEN", "DOCKER_CONFIG",
    "GITHUB_REF", "GITHUB_EVENT_NAME", "DEFAULT_BRANCH", "BUILDX_BUILDER",
)
SHA_RE = re.compile(r"^[0-9a-f]{40}$")


def capture(command: Sequence[str]) -> str:
    result = subprocess.run(command, cwd=ROOT, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, check=True)
    return result.stdout.strip()


def optional_capture(command: Sequence[str]) -> tuple[bool, str]:
    try:
        return True, capture(command)
    except subprocess.CalledProcessError as error:
        return False, error.stdout or ""


def execute(name: str, command: Sequence[str], *, cwd: Path = ROOT) -> None:
    print(f"[{name}]", flush=True)
    result = subprocess.run(command, cwd=cwd)
    if result.returncode:
        raise RuntimeError(f"{name} failed with exit code {result.returncode}")


def selected_runtime() -> runtime.Runtime:
    return runtime.probe_runtime(
        root=ROOT, capture=capture, optional_capture=optional_capture,
        probe_docker_fn=lambda prefix=(): runtime.probe_docker(
            prefix, optional_capture=optional_capture),
    )


def launch(
    argv: Sequence[str], *, integration: bool = False, docker: bool = False,
    env_names: Sequence[str] = (),
) -> None:
    selected = selected_runtime()
    image = runtime.ensure_validation_image(
        selected, root=INPUT_ROOT, optional_capture=optional_capture, run_step=execute,
        allow_build=False,
    )

    def run_step(name: str, command: Sequence[str]) -> None:
        command = list(command)
        if name == "In-container validation" and os.environ.get("GITHUB_ACTIONS") == "true":
            command[command.index(image):command.index(image)] = ["--env", "CI=true"]
        if name == "In-container validation" and env_names:
            for env_name in env_names:
                if env_name not in RELEASE_ENV_NAMES:
                    raise ValueError("unsupported release environment binding")
                index = command.index(image)
                command[index:index] = ["--env", env_name]
        if (integration or docker) and name == "In-container validation":
            # This is confined to an ephemeral, read-only GitHub Linux job.
            # No producer/publication credential is available on its VM.
            if (selected.name != "docker" or platform.system() != "Linux"
                    or os.environ.get("GITHUB_ACTIONS") != "true"):
                raise RuntimeError("CI Docker integration requires an isolated GitHub Linux job")
            sock = Path("/var/run/docker.sock")
            gid = sock.stat().st_gid
            docker_cli = shutil.which("docker")
            if not docker_cli:
                raise RuntimeError("Docker CLI is required for CI integration")
            index = command.index(image)
            command[index:index] = [
                "--mount", f"type=bind,source={sock},target={sock}",
                "--mount", f"type=bind,source={docker_cli},target=/usr/local/bin/docker,readonly",
                "--group-add", str(gid), "--network", "host",
                "--env", "DOCKER_HOST=unix:///var/run/docker.sock",
                "--env", "TESTCONTAINERS_HOST_OVERRIDE=127.0.0.1",
            ]
        execute(name, command)

    runtime.launch_in_validation(
        selected, argv, root=ROOT, capture=capture, run_step=run_step,
        toolchain_root=INPUT_ROOT,
    )


def run_lane(lane: str, *, base: str, tag: str | None = None) -> None:
    runtime.require_in_validation(tool="CI validation")
    if SHA_RE.fullmatch(base) is None:
        raise ValueError("CI validation requires an exact base SHA")
    if lane in validation_checks.CI_STEP_NAMES:
        for step in validation_checks.ci_steps(ROOT, lane, python=sys.executable, base=base):
            command = step.command
            if step.name == "Migration policy" and tag:
                command = [sys.executable, "tools/check_new_migrations.py", "--target-release", tag]
            execute(step.name, command, cwd=step.cwd)
    elif lane == "goreleaser-config":
        version = (ROOT / "backend/cmd/server/VERSION").read_text().strip()
        if re.fullmatch(r"\d+\.\d+\.\d+\+custom\.\d{3}", version) is None:
            raise ValueError("invalid embedded Plus release version")
        # Passed by name to the child, never shell-interpolated.
        env = dict(os.environ, GITHUB_REPO_OWNER="validation",
                   GITHUB_REPO_OWNER_LOWER="validation", GITHUB_REPO_NAME="sub2api-plus",
                   DOCKER_TAG_VERSION="v" + version.replace("+custom.", "-custom."),
                   DOCKERHUB_USERNAME="skip",
                   TAG_MESSAGE="GoReleaser configuration validation only.")
        subprocess.run(["goreleaser", "check"], cwd=ROOT, env=env, check=True)
    elif lane == "frontend-security":
        execute("Frontend frozen install",
                ["pnpm", "--dir", "frontend", "install", "--frozen-lockfile"])
        sys.path.insert(0, str(ROOT / "skills/push-cli/scripts"))
        import push_cli
        push_cli.run_frontend_security_check()
    elif lane == "backend-security":
        execute("Backend vulnerability scan", ["govulncheck", "./..."], cwd=ROOT / "backend")
    elif lane == "finalization":
        if not tag:
            raise ValueError("focused finalization requires a published tag")
        execute("Published release metadata", [
            sys.executable, "tools/check_release.py", "--tag", tag,
            "--require-status", "published", "--mapping-only",
        ])
    else:
        raise ValueError(f"unsupported CI validation lane: {lane}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    identity = commands.add_parser("identity")
    identity.add_argument("--github-output", type=Path)
    commands.add_parser("ensure")
    arbitrary = commands.add_parser("exec")
    arbitrary.add_argument("--docker", action="store_true")
    arbitrary.add_argument("--env", action="append", default=[], choices=RELEASE_ENV_NAMES)
    arbitrary.add_argument("argv", nargs=argparse.REMAINDER)
    lane = commands.add_parser("lane")
    lane.add_argument("--lane", choices=(*validation_checks.CI_LANES, "finalization"), required=True)
    lane.add_argument("--base", required=True)
    lane.add_argument("--tag")
    lane.add_argument("--inside", action="store_true")
    args = parser.parse_args()
    try:
        if args.command == "identity":
            values = {"image": runtime.validation_image_ref(INPUT_ROOT),
                      "image_generation": runtime.validation_image_digest(INPUT_ROOT),
                      "cache_generation": runtime.validation_cache_digest(INPUT_ROOT)}
            if args.github_output:
                with args.github_output.open("a") as output:
                    for key, value in values.items():
                        output.write(f"{key}={value}\n")
            print(json.dumps(values))
        elif args.command == "ensure":
            selected = selected_runtime()
            runtime.cleanup_validation_runtime(
                selected, root=INPUT_ROOT, capture=capture, run_step=execute)
            runtime.ensure_validation_image(
                selected, root=INPUT_ROOT, optional_capture=optional_capture, run_step=execute)
        elif args.command == "exec":
            argv = args.argv[1:] if args.argv[:1] == ["--"] else args.argv
            if not argv:
                raise ValueError("container execution requires a command")
            launch(argv, docker=args.docker, env_names=args.env)
        elif args.inside:
            runtime.require_in_validation(tool="CI validation")
            expected_go = "go" + runtime.declared_validation_pins(ROOT)["GO_VERSION"]
            if capture(["go", "env", "GOVERSION"]) != expected_go:
                raise RuntimeError("validation image does not match the declared Go version")
            run_lane(args.lane, base=args.base, tag=args.tag)
        else:
            argv = ["python3", str(Path(__file__).resolve()), "lane", "--inside",
                    "--lane", args.lane, "--base", args.base]
            if args.tag:
                argv.extend(["--tag", args.tag])
            launch(argv, integration=args.lane == "test-integration")
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        # Never dump command argv: it may contain user-supplied arguments.
        print(f"CI validation failed: {type(error).__name__}: {error if not isinstance(error, subprocess.CalledProcessError) else 'command failed'}",
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
