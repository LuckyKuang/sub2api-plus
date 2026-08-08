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


class FrontendSecurityCheckTest(unittest.TestCase):
    def test_vulnerability_exit_runs_exception_checker_with_audit_json(self) -> None:
        audit = {
            "advisories": {
                "1": {
                    "module_name": "xlsx",
                    "severity": "high",
                    "github_advisory_id": "GHSA-example",
                }
            }
        }
        audit_result = subprocess.CompletedProcess(
            ["pnpm", "audit"],
            1,
            stdout=push_cli.json.dumps(audit),
            stderr="node deprecation warning",
        )
        audit_path: Path | None = None

        def verify_exception_check(
            name: str,
            command: list[str],
            cwd: Path,
        ) -> None:
            nonlocal audit_path
            self.assertEqual("Frontend audit exceptions", name)
            self.assertEqual(Path.cwd(), cwd)
            self.assertEqual("--audit", command[2])
            audit_path = Path(command[3])
            with audit_path.open(encoding="utf-8") as handle:
                self.assertEqual(audit, push_cli.json.load(handle))
            self.assertEqual(
                ["--exceptions", ".github/audit-exceptions.yml"],
                command[4:],
            )

        with (
            mock.patch.object(push_cli, "ROOT", Path.cwd()),
            mock.patch.object(push_cli, "run_command", return_value=audit_result) as run_command,
            mock.patch.object(push_cli, "run_step", side_effect=verify_exception_check),
        ):
            push_cli.run_frontend_security_check()

        run_command.assert_called_once_with(
            ["pnpm", "audit", "--prod", "--audit-level=high", "--json"],
            cwd=Path.cwd() / "frontend",
            capture=True,
            merge_stderr=False,
        )
        self.assertIsNotNone(audit_path)
        self.assertFalse(audit_path.exists())

    def test_invalid_audit_json_is_a_hard_failure(self) -> None:
        audit_result = subprocess.CompletedProcess(
            ["pnpm", "audit"],
            1,
            stdout="registry unavailable",
        )
        with mock.patch.object(push_cli, "run_command", return_value=audit_result):
            with self.assertRaisesRegex(push_cli.PushCliError, "invalid JSON"):
                push_cli.run_frontend_security_check()

    def test_audit_error_payload_is_a_hard_failure(self) -> None:
        audit_result = subprocess.CompletedProcess(
            ["pnpm", "audit"],
            1,
            stdout='{"error":{"code":"ERR_PNPM_AUDIT_BAD_RESPONSE"}}',
        )
        with mock.patch.object(push_cli, "run_command", return_value=audit_result):
            with self.assertRaisesRegex(push_cli.PushCliError, "audit error"):
                push_cli.run_frontend_security_check()


class LocalChecksTest(unittest.TestCase):
    def test_static_checks_still_run_for_apple_runtime(self) -> None:
        git_miss = subprocess.CompletedProcess(["git"], 1, "")
        with (
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "run_command", return_value=git_miss),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "run_frontend_security_check") as audit_check,
            mock.patch.object(push_cli, "run_runtime_final_gate") as final_gate,
        ):
            runtime = push_cli.Runtime("apple-containers", compose_required=False)
            push_cli.run_local_checks("origin", "feature", runtime)

        names = [call.args[0] for call in run_step.call_args_list]
        self.assertIn("Backend unit tests", names)
        self.assertIn("Frontend production build", names)
        self.assertIn("Docker Compose security", names)
        self.assertIn("Docker runtime resources", names)
        audit_check.assert_called_once_with()
        final_gate.assert_called_once_with(runtime)


if __name__ == "__main__":
    unittest.main()
