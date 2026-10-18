"""Requirement-based lifecycle regressions. No host runtime is accessed."""

from __future__ import annotations

import importlib.util
import json
import os
import socket
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("colima_deployment", Path(__file__).resolve().parents[1] / "colima.py")
colima = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(colima)


class DockerFixture:
    """Independent model of CLI effects, not an alternate deployment algorithm."""
    def __init__(self, deployment):
        self.deployment = deployment
        self.calls = []
        self.images = {"sha256:old": "sha256:old", "sha256:new": "sha256:new",
                       deployment.current: "sha256:old", "example/sub2api:test": "sha256:new"}
        self.containers = {}
        self.fail = None

    def container(self, service, image="sha256:old"):
        return {"Image": image, "Config": {"Labels": {"org.sub2api.stack": "colima", "com.docker.compose.project": self.deployment.project}},
                "State": {"Running": True, "Health": {"Status": "healthy"}}, "HostConfig": {"PortBindings": {"8080/tcp": [{"HostIp": "127.0.0.1", "HostPort": "18080"}]}}}

    def __call__(self, args, **kwargs):
        self.calls.append(args)
        output, code = "", 0
        if args[:2] == ["colima", "status"]:
            output = '{"runtime":"docker"}'
        elif args[0] == "curl":
            pass
        elif args[:3] == ["git", "rev-parse", "HEAD"]:
            output = "abc123"
        elif args[:2] == ["git", "status"]:
            output = " M deploy/colima.py"
        else:
            assert args[:3] == ["docker", "--context", self.deployment.context], args
            command = args[3:]
            if self.fail and command[:len(self.fail)] == self.fail:
                return subprocess.CompletedProcess(args, 1, "", "private-failure-secret")
            if command[:2] == ["context", "inspect"]:
                output = f"unix://{Path.home()}/.colima/{self.deployment.profile}/docker.sock"
            elif command[0] == "info":
                output = "28.0.0"
            elif command[:2] == ["image", "inspect"]:
                image = self.images.get(command[2])
                code = 0 if image else 1
                tags = [name for name, identifier in self.images.items() if identifier == image and not name.startswith("sha256:")]
                output = image if "--format" in command else json.dumps([{"Id": image, "RepoTags": tags}])
            elif command[:2] == ["image", "tag"]:
                self.images[command[3]] = self.images[command[2]]
            elif command[:2] == ["image", "rm"]:
                self.images.pop(command[2], None)
            elif command[0] == "build":
                self.images[self.deployment.candidate] = "sha256:new"
            elif command[0] == "pull":
                self.images[command[1]] = "sha256:new"
            elif command[:2] == ["container", "inspect"]:
                item = self.containers.get(command[2])
                code, output = (0, json.dumps([item])) if item else (1, "")
            elif command[:2] == ["container", "ls"]:
                output = "\n".join(self.containers)
            elif command[0] in {"volume", "network"}:
                code = 1
            elif command[0] == "compose":
                if command[1] == "version":
                    output = "5.6.0"
                else:
                    # Strip options in the maintained invocation, then model
                    # individual Compose effects in their actual command order.
                    index = 1
                    while command[index] in {"--project-name", "--project-directory", "--env-file", "-f"}:
                        index += 2
                    operation = command[index:]
                    if operation[0] == "rm":
                        self.containers.pop(f"{self.deployment.project}-app", None)
                    elif operation[0] == "up":
                        if "--no-deps" in operation:
                            name = f"{self.deployment.project}-app"
                            assert name not in self.containers, "replacement must remove old Web first"
                            self.containers[name] = self.container("app", kwargs["env"]["COLIMA_SUB2API_IMAGE"])
                        else:
                            for service in ("postgres", "redis"):
                                self.containers.setdefault(f"{self.deployment.project}-{service}", self.container(service))
                    elif operation[0] not in {"config", "stop", "exec", "ps", "restart"}:
                        raise AssertionError(operation)
            else:
                raise AssertionError(command)
        return subprocess.CompletedProcess(args, code, output, "")


class ColimaDeploymentTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.directory = Path(temporary.name)
        self.env = self.directory / "deployment.env"
        self.env.write_text("POSTGRES_PASSWORD=database-secret\nJWT_SECRET=jwt-secret\nTOTP_ENCRYPTION_KEY=totp-secret\nCOLIMA_SUB2API_IMAGE=example/sub2api:test\n")
        self.env.chmod(0o600)
        patcher = mock.patch.dict(os.environ, {"SUB2API_COLIMA_STATE_DIR": str(self.directory / "state")})
        patcher.start()
        self.addCleanup(patcher.stop)
        self.deployment = colima.Deployment(self.env)
        self.fixture = DockerFixture(self.deployment)
        runner = mock.patch.object(colima.subprocess, "run", side_effect=self.fixture)
        runner.start()
        self.addCleanup(runner.stop)

    def old_stack(self):
        for service in ("app", "postgres", "redis"):
            self.fixture.containers[f"sub2api-colima-{service}"] = self.fixture.container(service)
        self.deployment.state_dir.mkdir(parents=True)
        self.deployment.save({"current": self.deployment.current, "current_id": "sha256:old", "owned_images": ["sha256:old"]})

    def test_init_private_distinct_secrets_and_no_overwrite(self):
        target = self.directory / "fresh.env"
        colima.initialize(target)
        self.assertEqual(0o600, target.stat().st_mode & 0o777)
        values = colima.read_env(target)
        self.assertEqual("127.0.0.1", values["BIND_HOST"])
        self.assertEqual("18080", values["SERVER_PORT"])
        self.assertEqual(5, len({values[key] for key in ("POSTGRES_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "TOTP_ENCRYPTION_KEY", "MINIO_ROOT_PASSWORD")}))
        original = target.read_bytes()
        with self.assertRaises(FileExistsError):
            colima.initialize(target)
        self.assertEqual(original, target.read_bytes())

    def test_reject_insecure_env_before_runtime_access(self):
        self.env.chmod(0o644)
        with self.assertRaisesRegex(colima.DeploymentError, "mode 600"):
            colima.Deployment(self.env)
        self.assertEqual([], self.fixture.calls)

    def test_literal_values_never_evaluate_shell_or_expand_dollars(self):
        with self.env.open("a") as handle:
            handle.write('ADMIN_PASSWORD=$(touch /tmp/forbidden)`whoami`$VALUE\n')
        deployment = colima.Deployment(self.env)
        self.assertEqual('$(touch /tmp/forbidden)`whoami`$VALUE', deployment.command_env["ADMIN_PASSWORD"])

    def test_colima_cli_gets_explicit_profile_without_application_env(self):
        with mock.patch.object(colima.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "", "")) as run:
            self.deployment.run(["colima", "status", self.deployment.profile])
        environment = run.call_args.kwargs["env"]
        self.assertNotIn("COLIMA_PROFILE", environment)
        self.assertNotIn("POSTGRES_PASSWORD", environment)
        self.assertNotIn("JWT_SECRET", environment)

    def test_invalid_configuration_does_not_touch_runtime(self):
        for extra in ("COLIMA_PROJECT=../other\n", "SERVER_PORT=0\n", "BIND_HOST=localhost\n", "MINIO_ENABLED=yes\n", "COLIMA_CPUS=-1\n", "COLIMA_DNS=not-an-ip\n"):
            with self.subTest(extra=extra):
                original = self.env.read_text()
                self.env.write_text(original + extra)
                with self.assertRaises(colima.DeploymentError):
                    colima.Deployment(self.env)
                self.env.write_text(original)
        self.assertEqual([], self.fixture.calls)

    def test_target_lock_serializes_and_cleans_temporary_files(self):
        with self.deployment.locked():
            temporary = self.deployment.temporary
            with self.assertRaisesRegex(colima.DeploymentError, "lock"):
                with self.deployment.locked():
                    self.fail("second mutation entered")
        self.assertFalse(temporary.exists())
        self.assertFalse((self.deployment.state_dir / "operation.lock").exists())

    def test_target_cannot_adopt_another_env_file(self):
        self.old_stack()
        other = self.directory / "another.env"
        other.write_text(self.env.read_text())
        other.chmod(0o600)
        with self.assertRaisesRegex(colima.DeploymentError, "another environment"):
            with colima.Deployment(other).locked():
                self.fail("adopted other env")

    def test_unowned_container_refused_before_image_preparation(self):
        self.fixture.containers["sub2api-colima-app"] = {"Config": {"Labels": {}}}
        with self.deployment.locked(), self.assertRaisesRegex(colima.DeploymentError, "unowned"):
            self.deployment.deploy(pull=True)
        self.assertFalse(any("pull" in command for command in self.fixture.calls))

    def test_pull_failure_keeps_old_web_and_dependencies(self):
        self.old_stack()
        previous = dict(self.fixture.containers)
        self.fixture.fail = ["pull"]
        with self.deployment.locked(), self.assertRaises(colima.DeploymentError):
            self.deployment.deploy(pull=True)
        self.assertEqual(previous, self.fixture.containers)
        self.assertFalse(any("stop" in command or "rm" in command for command in self.fixture.calls))

    def test_build_failure_keeps_old_web(self):
        self.old_stack()
        self.fixture.fail = ["build"]
        with self.deployment.locked(), self.assertRaises(colima.DeploymentError):
            self.deployment.deploy(build=True)
        self.assertEqual("sha256:old", self.fixture.containers["sub2api-colima-app"]["Image"])

    def test_success_replaces_one_web_preserves_dependencies_and_records_source(self):
        self.old_stack()
        postgres = self.fixture.containers["sub2api-colima-postgres"]
        redis = self.fixture.containers["sub2api-colima-redis"]
        with self.deployment.locked():
            self.deployment.deploy(build=True)
        self.assertEqual("sha256:new", self.fixture.containers["sub2api-colima-app"]["Image"])
        self.assertIs(postgres, self.fixture.containers["sub2api-colima-postgres"])
        self.assertIs(redis, self.fixture.containers["sub2api-colima-redis"])
        record = self.deployment.record()
        self.assertEqual({"commit": "abc123", "dirty": True, "version": (colima.ROOT / "backend/cmd/server/VERSION").read_text().strip()}, record["source"])
        self.assertFalse(record["recovery_needed"])
        self.assertEqual(1, sum(name.endswith("-app") for name in self.fixture.containers))
        self.assertFalse(any("prune" in command or "--volumes" in command or ("context" in command and "use" in command) for command in self.fixture.calls))

    def test_failed_health_recovers_once_and_still_returns_failure(self):
        self.old_stack()
        with self.deployment.locked(), mock.patch.object(self.deployment, "health", side_effect=[colima.DeploymentError("host failed"), None]) as health:
            with self.assertRaisesRegex(colima.DeploymentError, "host failed"):
                self.deployment.deploy(pull=True)
        self.assertEqual(2, health.call_count)
        self.assertEqual("sha256:old", self.fixture.containers["sub2api-colima-app"]["Image"])
        self.assertFalse(self.deployment.record()["recovery_needed"])

    def test_failed_recovery_retains_evidence_and_does_not_retry_again(self):
        self.old_stack()
        with self.deployment.locked(), mock.patch.object(self.deployment, "health", side_effect=colima.DeploymentError("unhealthy")) as health:
            with self.assertRaises(colima.DeploymentError):
                self.deployment.deploy(pull=True)
        self.assertEqual(2, health.call_count)
        self.assertTrue(self.deployment.record()["recovery_needed"])
        self.assertIn(self.deployment.recovery, self.fixture.images)

    def test_cleanup_never_removes_image_used_by_stopped_other_project(self):
        self.old_stack()
        self.fixture.containers["another-project"] = {"Image": "sha256:old", "State": {"Running": False}}
        with self.deployment.locked():
            self.deployment.deploy(pull=True)
        self.assertFalse(any(command[-3:] == ["image", "rm", "sha256:old"] for command in self.fixture.calls))

    def test_cleanup_preserves_unowned_tag_even_without_containers(self):
        self.old_stack()
        self.fixture.images["another-project:keep"] = "sha256:old"
        with self.deployment.locked():
            self.deployment.deploy(pull=True)
        self.assertEqual("sha256:old", self.fixture.images["another-project:keep"])
        self.assertFalse(any(command[-3:] == ["image", "rm", "sha256:old"] for command in self.fixture.calls))

    def test_cleanup_failure_leaves_new_healthy_web_without_recovery(self):
        self.old_stack()
        self.fixture.fail = ["image", "rm"]
        with self.deployment.locked(), self.assertRaises(colima.DeploymentError):
            self.deployment.deploy(pull=True)
        self.assertEqual("sha256:new", self.fixture.containers["sub2api-colima-app"]["Image"])
        self.assertFalse(self.deployment.record()["recovery_needed"])

    def test_read_only_status_cannot_start_stopped_vm(self):
        self.fixture.fail = ["unused"]
        with mock.patch.object(self.deployment, "run", return_value=subprocess.CompletedProcess([], 1, "", "")) as run:
            with self.assertRaisesRegex(colima.DeploymentError, "stopped"):
                self.deployment.runtime()
        self.assertEqual(1, run.call_count)

    def test_status_cannot_hide_failed_dependency_behind_healthy_web(self):
        self.old_stack()
        self.fixture.containers["sub2api-colima-redis"]["State"]["Health"]["Status"] = "unhealthy"
        with self.deployment.locked(), self.assertRaisesRegex(colima.DeploymentError, "redis"):
            self.deployment.status()

    def test_runtime_rejects_redirected_context_before_docker_info(self):
        with mock.patch.object(self.deployment, "docker", return_value=subprocess.CompletedProcess([], 0, "tcp://remote:2375", "")) as docker:
            with self.assertRaisesRegex(colima.DeploymentError, "selected local"):
                self.deployment.runtime()
        self.assertEqual(1, docker.call_count)

    def test_port_preflight_rejects_an_unrelated_listener(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            self.deployment.port = str(listener.getsockname()[1])
            with self.assertRaisesRegex(colima.DeploymentError, "occupied"):
                self.deployment.port_preflight()

    def test_port_preflight_allows_connections_left_by_a_stopped_service(self):
        with socket.socket() as listener:
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            address = listener.getsockname()
            with socket.create_connection(address) as client:
                accepted, _ = listener.accept()
                accepted.close()
                self.assertEqual(b"", client.recv(1))
        self.deployment.port = str(address[1])
        self.deployment.port_preflight()

    def test_stopped_profile_start_preserves_global_context_and_uses_configured_dns(self):
        self.deployment.dns = ["192.0.2.53", "2001:db8::53"]
        original = self.fixture.__call__
        def stopped(args, **kwargs):
            if args[:2] == ["colima", "status"]:
                return subprocess.CompletedProcess(args, 1, "", "")
            if args[:2] == ["colima", "list"]:
                return subprocess.CompletedProcess(args, 0, "", "")
            if args[:2] == ["colima", "start"]:
                return subprocess.CompletedProcess(args, 0, "", "")
            return original(args, **kwargs)
        with mock.patch.object(colima.subprocess, "run", side_effect=stopped) as run:
            self.deployment.runtime(start=True)
        command = next(call.args[0] for call in run.call_args_list if call.args[0][:2] == ["colima", "start"])
        self.assertIn("--activate=false", command)
        self.assertEqual("192.0.2.53,2001:db8::53", command[command.index("--dns") + 1])

    def test_optional_minio_credentials_are_private_and_application_uses_service_dns(self):
        with self.env.open("a") as handle:
            handle.write("MINIO_ENABLED=true\nMINIO_ROOT_USER=storage-admin\nMINIO_ROOT_PASSWORD=minio-private\n")
        deployment = colima.Deployment(self.env)
        with deployment.locked():
            with mock.patch.object(deployment, "docker"):
                deployment.compose("config", "--quiet")
            override = deployment.temporary / "minio-app.json"
            self.assertEqual(0o600, override.stat().st_mode & 0o777)
            environment = json.loads(override.read_text())["services"]["sub2api"]["environment"]
            self.assertEqual("http://minio:9000", environment["IMAGE_STORAGE_ENDPOINT"])
            self.assertEqual("minio-private", environment["IMAGE_STORAGE_SECRET_ACCESS_KEY"])
            self.assertEqual("http://127.0.0.1:19000/sub2api-images", environment["IMAGE_STORAGE_PUBLIC_BASE_URL"])
            self.assertEqual("colima-minio", deployment.command_env["COMPOSE_PROFILES"])
        self.assertFalse(override.exists())

    def test_retention_policy_keeps_only_one_predecessor(self):
        self.old_stack()
        self.deployment.values["COLIMA_RETAIN_PREVIOUS"] = "true"
        with self.deployment.locked():
            self.deployment.deploy(pull=True)
            self.deployment.deploy()
        self.assertEqual({"sha256:old", "sha256:new"}, set(self.deployment.record()["owned_images"]))
        self.assertEqual("sha256:old", self.deployment.record()["previous_id"])
        self.assertIn(self.deployment.recovery, self.fixture.images)

    def test_repeated_up_preserves_last_successful_source_selection(self):
        self.old_stack()
        with self.deployment.locked():
            self.deployment.deploy(build=True)
            source = self.deployment.record()["source"]
            self.deployment.deploy()
        self.assertEqual(source, self.deployment.record()["source"])
        self.assertEqual(1, sum("build" in command for command in self.fixture.calls))

    def test_dual_health_requires_published_endpoint(self):
        clock = iter((0, 0, 179, 181))
        self.old_stack()
        with (
            self.deployment.locked(),
            mock.patch.object(colima.time, "monotonic", side_effect=lambda: next(clock, 181)),
            mock.patch.object(colima.time, "sleep"),
            mock.patch.object(self.deployment, "run", wraps=self.deployment.run) as run,
        ):
            original = self.fixture.__call__
            def failed_host(args, **kwargs):
                if args[0] == "curl":
                    return subprocess.CompletedProcess(args, 1, "", "")
                return original(args, **kwargs)
            with mock.patch.object(colima.subprocess, "run", side_effect=failed_host):
                with self.assertRaisesRegex(colima.DeploymentError, "published-port"):
                    self.deployment.health()
        self.assertTrue(any(call.args[0][0] == "curl" for call in run.call_args_list))

    def test_command_failure_does_not_echo_private_diagnostics(self):
        self.fixture.fail = ["pull"]
        with self.assertRaises(colima.DeploymentError) as error:
            self.deployment.docker("pull", "example/sub2api:test")
        self.assertNotIn("private-failure-secret", str(error.exception))


if __name__ == "__main__":
    unittest.main()
