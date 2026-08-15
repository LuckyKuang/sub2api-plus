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
        run_step.assert_not_called()
        probe_docker.assert_not_called()

    def test_installed_apple_containers_not_ready_is_a_hard_failure(self) -> None:
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Darwin"),
            mock.patch.object(push_cli.shutil, "which", return_value="/usr/bin/container"),
            mock.patch.object(
                push_cli,
                "optional_capture",
                side_effect=[
                    (True, "container CLI version 1.2.0"),
                    (False, "runtime is not running"),
                ],
            ),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "mandatory macOS runtime.*fallback is forbidden",
            ):
                push_cli.probe_runtime()

        run_step.assert_not_called()
        probe_docker.assert_not_called()

    def test_installed_apple_containers_with_broken_cli_is_a_hard_failure(self) -> None:
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Darwin"),
            mock.patch.object(push_cli.shutil, "which", return_value="/usr/bin/container"),
            mock.patch.object(
                push_cli,
                "optional_capture",
                return_value=(False, "CLI failed"),
            ),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "mandatory macOS runtime.*fallback is forbidden",
            ):
                push_cli.probe_runtime()

        run_step.assert_not_called()
        probe_docker.assert_not_called()

    def test_absent_apple_containers_does_not_fall_back(self) -> None:
        def which(command: str) -> str | None:
            return "/opt/homebrew/bin/colima" if command == "colima" else None

        with (
            mock.patch.object(push_cli.platform, "system", return_value="Darwin"),
            mock.patch.object(push_cli.shutil, "which", side_effect=which),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "requires Apple Containers.*fallback is forbidden",
            ):
                push_cli.probe_runtime()

        run_step.assert_not_called()
        probe_docker.assert_not_called()

    def test_windows_requires_wsl2_before_any_docker_probe(self) -> None:
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(push_cli.shutil, "which", return_value=None),
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(push_cli.PushCliError, "requires wsl.exe"):
                push_cli.probe_runtime()

        probe_docker.assert_not_called()

    def test_windows_requires_a_running_wsl2_linux_distribution(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(
                push_cli,
                "capture",
                return_value="NAME STATE VERSION\nUbuntu Stopped 2",
            ),
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "Debian/Ubuntu distributions exist but none are running: Ubuntu",
            ):
                push_cli.probe_runtime()

        probe_docker.assert_not_called()

    def test_windows_uses_docker_inside_running_wsl2_linux(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"

        def captured(command: list[str], **_: object) -> str:
            if command == [wsl, "-l", "-v"]:
                return "NAME STATE VERSION\nUbuntu-24.04 Running 2"
            if command == [
                wsl,
                "-d",
                "Ubuntu-24.04",
                "--",
                "wslpath",
                "-a",
                "/repo",
            ]:
                return "/mnt/c/repo"
            self.fail(f"unexpected command: {command}")

        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "capture", side_effect=captured),
            mock.patch.object(
                push_cli,
                "probe_docker",
                return_value=(True, "Docker Compose version v2.40.0"),
            ) as probe_docker,
        ):
            runtime = push_cli.probe_runtime()

        prefix = (wsl, "-d", "Ubuntu-24.04", "--")
        self.assertEqual("wsl2-docker", runtime.name)
        self.assertEqual(prefix, runtime.prefix)
        self.assertEqual("/mnt/c/repo", runtime.compose_root)
        probe_docker.assert_called_once_with(prefix)

    def test_windows_never_falls_back_to_host_docker(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(
                push_cli,
                "capture",
                return_value="NAME STATE VERSION\nUbuntu Running 2",
            ),
            mock.patch.object(
                push_cli,
                "probe_docker",
                return_value=(False, "docker is unavailable"),
            ) as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "Docker and Docker Compose are not usable inside it",
            ):
                push_cli.probe_runtime()

        probe_docker.assert_called_once_with((wsl, "-d", "Ubuntu", "--"))

    def test_windows_rejects_non_debian_ubuntu_distributions(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(
                push_cli,
                "capture",
                return_value=(
                    "NAME STATE VERSION\n"
                    "FedoraLinux-42 Running 2\n"
                    "docker-desktop Running 2"
                ),
            ),
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "requires a WSL2 Debian or Ubuntu",
            ):
                push_cli.probe_runtime()

        probe_docker.assert_not_called()

    def test_windows_ignores_running_fedora_when_ubuntu_is_available(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"

        def captured(command: list[str], **_: object) -> str:
            if command == [wsl, "-l", "-v"]:
                return (
                    "NAME STATE VERSION\n"
                    "FedoraLinux-42 Running 2\n"
                    "Debian Running 2"
                )
            if command == [wsl, "-d", "Debian", "--", "wslpath", "-a", "/repo"]:
                return "/mnt/c/repo"
            self.fail(f"unexpected command: {command}")

        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "capture", side_effect=captured),
            mock.patch.object(
                push_cli,
                "probe_docker",
                return_value=(True, "Docker Compose version v2.40.0"),
            ) as probe_docker,
        ):
            runtime = push_cli.probe_runtime()

        probe_docker.assert_called_once_with((wsl, "-d", "Debian", "--"))
        self.assertEqual("wsl2-docker", runtime.name)
        self.assertEqual("/mnt/c/repo", runtime.compose_root)

    def test_windows_does_not_use_fedora_when_ubuntu_is_stopped(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"
        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(
                push_cli,
                "capture",
                return_value=(
                    "NAME STATE VERSION\n"
                    "FedoraLinux-42 Running 2\n"
                    "Ubuntu-24.04 Stopped 2"
                ),
            ),
            mock.patch.object(push_cli, "probe_docker") as probe_docker,
        ):
            with self.assertRaisesRegex(
                push_cli.PushCliError,
                "none are running: Ubuntu-24.04",
            ):
                push_cli.probe_runtime()

        probe_docker.assert_not_called()

    def test_windows_parses_utf16_default_marker_and_ignores_other_distros(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"
        listing = (
            "\ufeff  N\x00A\x00M\x00E\x00 \x00S\x00T\x00A\x00T\x00E\x00 \x00V\x00E\x00R\x00S\x00I\x00O\x00N\x00\n"
            "* Ubuntu-24.04 (Default)           Running         2\n"
            "  FedoraLinux-42                   Running         2\n"
        )

        def captured(command: list[str], **_: object) -> str:
            if command == [wsl, "-l", "-v"]:
                return listing
            if command == [wsl, "-d", "Ubuntu-24.04", "--", "wslpath", "-a", "/repo"]:
                return "/mnt/c/repo"
            self.fail(f"unexpected command: {command}")

        with (
            mock.patch.object(push_cli.platform, "system", return_value="Windows"),
            mock.patch.object(
                push_cli.shutil,
                "which",
                side_effect=lambda command: wsl if command == "wsl.exe" else None,
            ),
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "capture", side_effect=captured),
            mock.patch.object(
                push_cli,
                "probe_docker",
                return_value=(True, "Docker Compose version v2.40.0"),
            ) as probe_docker,
        ):
            runtime = push_cli.probe_runtime()

        probe_docker.assert_called_once_with((wsl, "-d", "Ubuntu-24.04", "--"))
        self.assertEqual("wsl2-docker", runtime.name)


class DebianUbuntuNameTest(unittest.TestCase):
    def test_accepts_debian_and_ubuntu_family_names(self) -> None:
        for name in (
            "Debian",
            "debian",
            "Ubuntu",
            "Ubuntu-24.04",
            "ubuntu-22.04",
            "Ubuntu-24.04 (Default)",
        ):
            self.assertTrue(push_cli.is_debian_or_ubuntu_wsl(name), name)

    def test_rejects_other_wsl_names(self) -> None:
        for name in ("FedoraLinux-42", "openSUSE-Tumbleweed", "docker-desktop", "kali-linux"):
            self.assertFalse(push_cli.is_debian_or_ubuntu_wsl(name), name)

    def test_parse_strips_nul_bytes_and_default_marker(self) -> None:
        parsed = push_cli.parse_wsl_distributions(
            "* Ubuntu-24.04 (Default)\x00           Running         2\n"
            "  Debian                 Stopped         2\n"
        )
        self.assertEqual(
            [("Ubuntu-24.04", "Running"), ("Debian", "Stopped")],
            parsed,
        )


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
        ):
            runtime = push_cli.Runtime("apple-containers", compose_required=False)
            push_cli.run_local_checks("origin", "feature", runtime)

        names = [call.args[0] for call in run_step.call_args_list]
        self.assertEqual("Apple Container lifecycle test", names[0])
        self.assertIn("Push CLI self-tests", names)
        self.assertIn("Backend unit tests", names)
        self.assertIn("Frontend production build", names)
        self.assertIn("Docker Compose security", names)
        self.assertIn("Docker runtime resources", names)
        audit_check.assert_called_once_with()

    def test_docker_runtime_still_runs_apple_lifecycle_test(self) -> None:
        git_miss = subprocess.CompletedProcess(["git"], 1, "")
        with (
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "run_command", return_value=git_miss),
            mock.patch.object(push_cli, "run_step") as run_step,
            mock.patch.object(push_cli, "run_frontend_security_check"),
        ):
            push_cli.run_local_checks("origin", "feature", push_cli.Runtime("docker"))

        names = [call.args[0] for call in run_step.call_args_list]
        self.assertIn("Apple Container lifecycle test", names)


class DeclaredToolchainsTest(unittest.TestCase):
    def test_reads_repository_pins(self) -> None:
        declared = push_cli.declared_toolchains()
        self.assertRegex(declared.go, r"^\d+\.\d+\.\d+$")
        self.assertRegex(declared.pnpm, r"^\d+\.\d+\.\d+$")
        self.assertGreaterEqual(declared.node_major_minimum, 20)
        self.assertRegex(declared.golangci_lint, r"^\d+\.\d+\.\d+$")


class ValidationLaunchTest(unittest.TestCase):
    def test_macos_launch_uses_apple_container_run(self) -> None:
        runtime = push_cli.Runtime("apple-containers", compose_required=False)
        with (
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "SCRIPT", Path("/repo/skills/push-cli/scripts/push_cli.py")),
            mock.patch.object(
                push_cli.validation_runtime,
                "runtime_user",
                return_value="501:20",
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "cache_mounts",
                return_value=[("/cache/go", "/tmp/sub2api-home/go")],
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "validation_image_ref",
                return_value="sub2api-validation:test",
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "in_validation_container",
                return_value=False,
            ),
            mock.patch.object(push_cli, "run_step") as run_step,
        ):
            push_cli.launch_in_validation(runtime, "origin")

        name, command = run_step.call_args.args
        self.assertEqual("In-container validation", name)
        self.assertEqual("container", command[0])
        self.assertIn("run", command)
        self.assertIn("--memory", command)
        self.assertIn("8G", command)
        self.assertIn(
            "type=bind,source=/repo,target=/repo",
            command,
        )
        self.assertNotIn("docker", command)
        self.assertNotIn("go", command)
        self.assertNotIn("pnpm", command)
        self.assertIn("--in-validation", command)

    def test_windows_launch_uses_wsl_docker_run(self) -> None:
        wsl = "C:/Windows/System32/wsl.exe"
        runtime = push_cli.Runtime(
            "wsl2-docker",
            (wsl, "-d", "Ubuntu-24.04", "--"),
            "/mnt/c/repo",
        )
        with (
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "SCRIPT", Path("/repo/skills/push-cli/scripts/push_cli.py")),
            mock.patch.object(
                push_cli.validation_runtime,
                "runtime_user",
                return_value="1000:1000",
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "cache_mounts",
                return_value=[("/tmp/cache/go", "/tmp/sub2api-home/go")],
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "validation_image_ref",
                return_value="sub2api-validation:test",
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "in_validation_container",
                return_value=False,
            ),
            mock.patch.object(push_cli, "run_step") as run_step,
        ):
            push_cli.launch_in_validation(runtime, "origin")

        command = run_step.call_args.args[1]
        self.assertEqual(
            [wsl, "-d", "Ubuntu-24.04", "--", "docker", "run", "--rm", "--cpus", "4", "--memory", "8G"],
            command[:11],
        )
        self.assertIn("/mnt/c/repo:/mnt/c/repo", command)
        self.assertNotIn("go", command)

    def test_linux_launch_uses_docker_run(self) -> None:
        runtime = push_cli.Runtime("docker")
        with (
            mock.patch.object(push_cli, "ROOT", Path("/repo")),
            mock.patch.object(push_cli, "SCRIPT", Path("/repo/skills/push-cli/scripts/push_cli.py")),
            mock.patch.object(
                push_cli.validation_runtime,
                "runtime_user",
                return_value="1000:1000",
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "cache_mounts",
                return_value=[("/cache/go", "/tmp/sub2api-home/go")],
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "validation_image_ref",
                return_value="sub2api-validation:test",
            ),
            mock.patch.object(
                push_cli.validation_runtime,
                "in_validation_container",
                return_value=False,
            ),
            mock.patch.object(push_cli, "run_step") as run_step,
        ):
            push_cli.launch_in_validation(runtime, "origin")

        command = run_step.call_args.args[1]
        self.assertEqual(["docker", "run", "--rm", "--cpus", "4", "--memory", "8G"], command[:7])
        self.assertNotIn("go", command)
        self.assertNotIn("pnpm", command)


class MainFlowTest(unittest.TestCase):
    def test_check_probes_environment_before_dirty_worktree(self) -> None:
        order: list[str] = []

        def record(name: str):
            def _inner(*_args: object, **_kwargs: object) -> object:
                order.append(name)
                if name == "github_gate":
                    return "LuckyKuang/sub2api-plus"
                if name == "current_branch":
                    return "feature/new-features-and-fixes"
                if name == "probe_runtime":
                    return push_cli.Runtime("apple-containers", compose_required=False)
                if name == "require_clean_worktree":
                    raise push_cli.PushCliError("worktree is not clean")
                return None

            return _inner

        args = mock.Mock(
            action="check",
            in_validation=False,
            remote="origin",
            repo_root=Path("/repo"),
        )
        with (
            mock.patch.object(push_cli, "parse_args", return_value=args),
            mock.patch.object(push_cli, "github_gate", side_effect=record("github_gate")),
            mock.patch.object(push_cli, "current_branch", side_effect=record("current_branch")),
            mock.patch.object(push_cli, "probe_runtime", side_effect=record("probe_runtime")),
            mock.patch.object(
                push_cli,
                "ensure_validation_image",
                side_effect=record("ensure_validation_image"),
            ),
            mock.patch.object(
                push_cli,
                "require_clean_worktree",
                side_effect=record("require_clean_worktree"),
            ),
            mock.patch.object(push_cli, "launch_in_validation") as launch,
            mock.patch.object(push_cli, "run_local_checks") as local_checks,
        ):
            self.assertEqual(1, push_cli.main())

        self.assertEqual(
            [
                "github_gate",
                "current_branch",
                "probe_runtime",
                "ensure_validation_image",
                "require_clean_worktree",
            ],
            order,
        )
        launch.assert_not_called()
        local_checks.assert_not_called()

    def test_ensure_skips_worktree_and_local_checks(self) -> None:
        args = mock.Mock(
            action="ensure",
            in_validation=False,
            remote="origin",
            repo_root=Path("/repo"),
        )
        with (
            mock.patch.object(push_cli, "parse_args", return_value=args),
            mock.patch.object(push_cli, "github_gate", return_value="LuckyKuang/sub2api-plus"),
            mock.patch.object(push_cli, "current_branch", return_value="feature"),
            mock.patch.object(
                push_cli,
                "probe_runtime",
                return_value=push_cli.Runtime("apple-containers", compose_required=False),
            ) as probe,
            mock.patch.object(push_cli, "ensure_validation_image") as ensure_image,
            mock.patch.object(push_cli, "require_clean_worktree") as clean,
            mock.patch.object(push_cli, "launch_in_validation") as launch,
            mock.patch.object(push_cli, "run_local_checks") as local_checks,
            mock.patch.object(push_cli, "check_toolchains") as check,
        ):
            self.assertEqual(0, push_cli.main())

        probe.assert_called_once_with()
        ensure_image.assert_called_once()
        check.assert_not_called()
        clean.assert_not_called()
        launch.assert_not_called()
        local_checks.assert_not_called()

    def test_host_check_launches_container_instead_of_host_matrix(self) -> None:
        runtime = push_cli.Runtime("docker")
        args = mock.Mock(
            action="check",
            in_validation=False,
            remote="origin",
            repo_root=Path("/repo"),
        )
        with (
            mock.patch.object(push_cli, "parse_args", return_value=args),
            mock.patch.object(push_cli, "github_gate", return_value="LuckyKuang/sub2api-plus"),
            mock.patch.object(push_cli, "current_branch", return_value="feature"),
            mock.patch.object(push_cli, "probe_runtime", return_value=runtime),
            mock.patch.object(push_cli, "ensure_validation_image"),
            mock.patch.object(push_cli, "require_clean_worktree"),
            mock.patch.object(push_cli, "launch_in_validation") as launch,
            mock.patch.object(push_cli, "run_runtime_final_gate") as final_gate,
            mock.patch.object(push_cli, "ensure_clean_after_checks"),
            mock.patch.object(push_cli, "run_local_checks") as local_checks,
            mock.patch.object(push_cli, "check_toolchains") as check,
        ):
            self.assertEqual(0, push_cli.main())

        launch.assert_called_once_with(runtime, "origin")
        final_gate.assert_called_once_with(runtime)
        local_checks.assert_not_called()
        check.assert_not_called()

    def test_in_validation_flag_is_rejected_on_darwin_host(self) -> None:
        args = mock.Mock(
            action="check",
            in_validation=True,
            remote="origin",
            repo_root=Path("/repo"),
        )
        with (
            mock.patch.object(push_cli, "parse_args", return_value=args),
            mock.patch.object(
                push_cli.validation_runtime,
                "host_os_forbids_in_validation",
                return_value=True,
            ),
            mock.patch.object(push_cli, "run_local_checks") as local_checks,
        ):
            self.assertEqual(1, push_cli.main())
        local_checks.assert_not_called()


if __name__ == "__main__":
    unittest.main()
