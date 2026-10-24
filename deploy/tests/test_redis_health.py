"""Readiness requires authenticated PONG, not redis-cli's default exit code.

Protocol/CLI contract: https://redis.io/docs/latest/commands/ping/ and
https://redis.io/docs/latest/develop/tools/cli/ (-e enables command errors).
Every server here is a foreground child on a private Unix socket, without TCP
or persistence; the test always terminates and waits for its own child.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "deploy/tests/fixtures/bin"
COMPOSE_FILES = (
    "deploy/docker-compose.yml",
    "deploy/docker-compose.local.yml",
    "deploy/docker-compose.dev.yml",
)
# Synthetic credentials intentionally include shell metacharacters/whitespace.
PASSWORD = "test only $:'\" password"


class RedisHealthTests(unittest.TestCase):
    def setUp(self) -> None:
        self.cli = shutil.which("redis-cli")
        self.server = shutil.which("redis-server")
        self.assertIsNotNone(self.cli, "validation image must provide redis-cli")
        self.assertIsNotNone(self.server, "validation image must provide redis-server")
        self.temp = tempfile.TemporaryDirectory(prefix="redis-health-")
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        self.socket = self.path / "redis.sock"
        self.state = self.path / "state"
        self.env_file = self.path / "deployment.env"
        self.reply_log = self.path / "reply"
        self.bin = self.path / "bin"
        self.bin.mkdir()
        wrapper = self.bin / "redis-cli"
        wrapper.write_text("""#!/bin/sh
case "${HEALTH_TEST_REPLY:-}" in
    unexpected) printf 'LOADING\\n'; exit 0 ;;
    stderr) printf 'authentication failed\\n' >&2; printf 'PONG\\n'; exit 0 ;;
    failed) printf 'PONG\\n'; exit 1 ;;
esac
"$HEALTH_TEST_CLI" -s "$FAKE_REDIS_SOCKET" "$@" >"$HEALTH_TEST_REPLY_LOG" 2>&1
result=$?
cat "$HEALTH_TEST_REPLY_LOG"
exit "$result"
""")
        wrapper.chmod(0o755)
        self.env = dict(os.environ,
            PATH=f"{self.bin}:{FIXTURES}:{os.environ['PATH']}",
            SUB2API_ENV_FILE=str(self.env_file),
            FAKE_CONTAINER_STATE=str(self.state),
            FAKE_REDIS_SOCKET=str(self.socket),
            HEALTH_TEST_CLI=str(self.cli),
            HEALTH_TEST_REPLY_LOG=str(self.reply_log),
        )
        # Deployment initialization is real; only the runtime is the existing
        # fixture. Neither this setup nor status may touch a real container.
        self.apple("init", check=True)
        for directory in ("containers", "running"):
            (self.state / directory).mkdir(parents=True, exist_ok=True)
        for name in ("sub2api-apple", "sub2api-apple-postgres", "sub2api-apple-redis"):
            for directory in ("containers", "running"):
                (self.state / directory / name).touch()
        (self.state / "system-running").touch()
        self.process: subprocess.Popen[bytes] | None = None
        self.addCleanup(self.stop_server)

    def apple(self, action: str, *, check: bool = False) -> subprocess.CompletedProcess[str]:
        return subprocess.run(["bash", str(ROOT / "deploy/apple-container.sh"), action],
            env=self.env, capture_output=True, text=True, check=check, timeout=45)

    def stop_server(self) -> None:
        if self.process is not None:
            if self.process.poll() is None:
                self.process.terminate()
            try:
                self.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait(timeout=5)

    def start_server(self, *, password: str = PASSWORD, deny_ping: bool = False) -> None:
        config = self.path / "redis.conf"
        # Redis quoted strings use JSON-compatible escaping for this fixture.
        auth = "nopass" if not password else ">" + password
        config.write_text("\n".join([
            "port 0", f"unixsocket {self.socket}", "unixsocketperm 700",
            'save ""', "appendonly no", "daemonize no",
            "user default on " + json.dumps(auth) + " ~* +@all" + (" -ping" if deny_ping else ""),
        ]) + "\n")
        config.chmod(0o600)
        self.process = subprocess.Popen([str(self.server), str(config)],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for _ in range(100):
            if self.process.poll() is not None:
                self.fail("private Redis exited before accepting connections")
            if self.socket.exists():
                return
            time.sleep(0.02)
        self.fail("private Redis did not create its Unix socket")

    def configure(self, password: str, *, inherited: str = "stale-container-secret") -> None:
        contents = self.env_file.read_text()
        contents = re.sub(r"^REDIS_PASSWORD=.*$", lambda _: "REDIS_PASSWORD=" + password,
                          contents, flags=re.MULTILINE)
        contents += "\nMINIO_ENABLED=false\n"
        self.env_file.write_text(contents)
        self.env["FAKE_REDIS_INHERITED_AUTH"] = inherited
        self.env["REDISCLI_AUTH"] = password

    def assert_health(self, healthy: bool) -> str | None:
        result = self.apple("status")
        apple_reply = self.reply_log.read_text() if self.reply_log.exists() else None
        expected = "healthy" if healthy else "unhealthy"
        self.assertRegex(result.stdout, rf"(?m)^redis\s+{expected}$")
        self.assertEqual(result.returncode, 0 if healthy else 1, result.stdout + result.stderr)
        commands = (self.state / "commands.log").read_text()
        self.assertNotIn(PASSWORD, commands + result.stdout + result.stderr)
        self.assertNotRegex(commands, r"(?m)^(create|delete|stop|start) ")
        for compose in COMPOSE_FILES:
            with self.subTest(compose=compose):
                service = yaml.safe_load((ROOT / compose).read_text())["services"]["redis"]
                health = service["healthcheck"]["test"]
                if health[0] == "CMD-SHELL":
                    command = ["sh", "-c", health[1].replace("$$", "$")]
                else:
                    self.assertEqual(health[0], "CMD")
                    command = health[1:]
                probe = subprocess.run(command, env=self.env, capture_output=True,
                                       text=True, timeout=5)
                self.assertEqual(probe.returncode == 0, healthy, probe.stdout + probe.stderr)
                self.assertNotIn(PASSWORD, probe.stdout + probe.stderr)
        return apple_reply

    def test_selected_password_overrides_stale_exec_environment(self) -> None:
        self.start_server()
        self.configure(PASSWORD)
        reply = self.assert_health(True)
        # A legacy status-only probe would accept NOAUTH with exit 0. Require
        # actual successful authentication at the Redis protocol boundary.
        self.assertEqual(reply, "PONG\n")

    def test_wrong_selected_password_cannot_fall_back_to_inherited_credential(self) -> None:
        self.start_server()
        self.configure("wrong-selected-password", inherited=PASSWORD)
        self.assert_health(False)

    def test_missing_password_is_unhealthy_when_authentication_is_required(self) -> None:
        self.start_server()
        self.configure("")
        self.assert_health(False)

    def test_passwordless_server_clears_stale_authentication(self) -> None:
        self.start_server(password="")
        self.configure("")
        reply = self.assert_health(True)
        self.assertEqual(reply, "PONG\n")

    def test_acl_command_denial_is_unhealthy(self) -> None:
        self.start_server(deny_ping=True)
        self.configure(PASSWORD)
        self.assert_health(False)

    def test_connection_failure_is_unhealthy(self) -> None:
        self.configure(PASSWORD)
        self.assert_health(False)

    def test_unexpected_reply_is_unhealthy_even_with_exit_zero(self) -> None:
        self.configure(PASSWORD)
        self.env["HEALTH_TEST_REPLY"] = "unexpected"
        self.assert_health(False)

    def test_stderr_auth_error_is_unhealthy_even_with_pong_and_exit_zero(self) -> None:
        self.configure(PASSWORD)
        self.env["HEALTH_TEST_REPLY"] = "stderr"
        self.assert_health(False)

    def test_nonzero_exit_is_unhealthy_even_with_pong(self) -> None:
        self.configure(PASSWORD)
        self.env["HEALTH_TEST_REPLY"] = "failed"
        self.assert_health(False)

    def test_legacy_cli_exit_zero_does_not_prove_success(self) -> None:
        self.start_server()
        env = dict(os.environ, REDISCLI_AUTH="wrong-password")
        legacy = subprocess.run([str(self.cli), "-s", str(self.socket), "ping"],
                                env=env, capture_output=True, text=True, timeout=5)
        self.assertEqual(legacy.returncode, 0)
        self.assertIn("NOAUTH", legacy.stdout + legacy.stderr)
        strict = subprocess.run([str(self.cli), "-e", "-s", str(self.socket), "ping"],
                                env=env, capture_output=True, text=True, timeout=5)
        self.assertNotEqual(strict.returncode, 0)


if __name__ == "__main__":
    unittest.main()
