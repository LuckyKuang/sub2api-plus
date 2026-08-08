from __future__ import annotations

import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "push_cli.py"
SPEC = importlib.util.spec_from_file_location("push_cli_under_test", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"unable to load {SCRIPT}")
push_cli = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = push_cli
SPEC.loader.exec_module(push_cli)


class ProbeRuntimeTest(unittest.TestCase):
    def test_ready_apple_containers_is_self_sufficient(self) -> None:
        def optional(command: list[str], **_: object) -> tuple[bool, str]:
            if command == ["container", "--version"]:
                return True, "container CLI version 1.2.0"
            if command == ["container", "ls"]:
                return True, ""
            self.fail(f"unexpected command: {command}")

        with (
            mock.patch.object(push_cli.platform, "system", return_value="Darwin"),
            mock.patch.object(push_cli.shutil, "which", return_value="/usr/bin/container"),
            mock.patch.object(push_cli, "optional_capture", side_effect=optional),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            runtime = push_cli.probe_runtime()

        self.assertEqual("apple-containers", runtime.name)
        self.assertFalse(runtime.compose_required)
        run_step.assert_called_once()
        probe_docker.assert_not_called()

    def test_apple_lifecycle_failure_does_not_fall_back(self) -> None:
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Darwin"),
            mock.patch.object(push_cli.shutil, "which", return_value="/usr/bin/container"),
            mock.patch.object(
                push_cli,
                "optional_capture",
                side_effect=[
                    (True, "container CLI version 1.2.0"),
                    (True, ""),
                ],
            ),
            mock.patch.object(
                push_cli,
                "run_step",
                side_effect=push_cli.PushCliError("lifecycle failed"),
            ),
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(push_cli.PushCliError, "lifecycle failed"):
                push_cli.probe_runtime()

        probe_docker.assert_not_called()

    def test_unavailable_apple_containers_falls_back_to_colima(self) -> None:
        def which(command: str) -> str | None:
            return "/opt/homebrew/bin/colima" if command == "colima" else None

        with (
            mock.patch.object(push_cli.platform, "system", return_value="Darwin"),
            mock.patch.object(push_cli.shutil, "which", side_effect=which),
            mock.patch.object(
                push_cli,
                "optional_capture",
                return_value=(True, "colima is running"),
            ),
            mock.patch.object(
                push_cli,
                "probe_docker",
                return_value=(True, "Docker Compose version v2.40.0"),
            ),
        ):
            runtime = push_cli.probe_runtime()

        self.assertEqual("colima/docker", runtime.name)
        self.assertTrue(runtime.compose_required)


class RuntimeFinalGateTest(unittest.TestCase):
    def test_apple_runtime_does_not_invoke_docker(self) -> None:
        with mock.patch.object(push_cli, "run_step") as run_step:
            push_cli.run_runtime_final_gate(
                push_cli.Runtime("apple-containers", compose_required=False)
            )

        run_step.assert_not_called()

    def test_docker_runtime_runs_compose_parser(self) -> None:
        with mock.patch.object(push_cli, "run_step") as run_step:
            push_cli.run_runtime_final_gate(push_cli.Runtime("docker"))

        run_step.assert_called_once_with(
            "Docker Compose final gate",
            [
                "docker",
                "compose",
                "-f",
                "deploy/docker-compose.dev.yml",
                "config",
                "--quiet",
            ],
        )


class LocalChecksTest(unittest.TestCase):
    def test_static_checks_still_run_for_apple_runtime(self) -> None:
        git_miss = subprocess.CompletedProcess(["git"], 1, "")
        with (
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "run_command", return_value=git_miss),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "run_runtime_final_gate") as final_gate,
        ):
            runtime = push_cli.Runtime("apple-containers", compose_required=False)
            push_cli.run_local_checks("origin", "feature", runtime)

        names = [call.args[0] for call in run_step.call_args_list]
        self.assertIn("Backend unit tests", names)
        self.assertIn("Frontend production build", names)
        self.assertIn("Docker Compose security", names)
        self.assertIn("Docker runtime resources", names)
        final_gate.assert_called_once_with(runtime)


if __name__ == "__main__":
    unittest.main()
