#!/usr/bin/env python3
"""Run real Chromium/application/database management flows in validation only.

The outer launcher owns the disposable PostgreSQL/Redis containers. This runner
owns a fresh database, temporary processes/CA/data, and reversible hosts entries.
It never imports or replaces an application service, handler, or browser route.
"""
from __future__ import annotations

import argparse
import contextlib
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import tempfile
import time
from urllib.parse import parse_qs, quote, unquote, urlsplit, urlunsplit
from urllib.request import ProxyHandler, build_opener

import validation_runtime


ROOT = Path(__file__).resolve().parents[1]
PROBE_HOSTS = ("ip-api.com", "api64.ipify.org")
PROVIDER_HOSTS = (
    "provider.e2e.invalid", "api.openai.com", "auth.openai.com", "chatgpt.com",
    "api.x.ai", "auth.x.ai", "accounts.x.ai", "grok.com", "cli-chat-proxy.grok.com",
    "us-east-1.api.x.ai", "us-west-2.api.x.ai", "eu-west-1.api.x.ai",
    "api.deepseek.com", "platform.deepseek.com", "api.kimi.ai", "api.kimi.com",
    "auth.kimi.ai", "auth.kimi.com", "account.minimax.cn", "account.minimax.io",
    "agent.minimax.cn", "agent.minimax.io", "api.minimax.chat", "api.minimax.io",
    "platform.stepfun.ai", "platform.stepfun.com", "api.stepfun.ai", "api.stepfun.com",
    "api.z.ai", "bigmodel.cn", "open.bigmodel.cn", "zcode.z.ai", "unpkg.com",
)
APP_PORT = 18090
CONTROL_PORT = 18091
VITE_PORT = 18092
PROXY_BASE_PORT = 18110
REQUIRED_MANAGEMENT_TITLES = tuple(
    f"{provider} {kind}: real browser proxy binding, failure, repair and deletion"
    for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
    for kind in ("apikey", "oauth")
)


def browser_coverage_errors(report: dict, *, grep: str | None = None) -> list[str]:
    """Require actual successful Chromium runs for each applicable business case."""
    expected = {title for title in REQUIRED_MANAGEMENT_TITLES
                if grep is None or re.search(grep, title)}
    errors: list[str] = []
    if not expected:
        errors.append("the filter selects no required browser business scenarios")
    observed: set[str] = set()

    def visit(suites: list[dict]) -> None:
        for suite in suites:
            for spec in suite.get("specs", []):
                title = spec.get("title")
                if title not in expected:
                    errors.append(f"unexpected browser scenario: {title}")
                    continue
                if title in observed:
                    errors.append(f"duplicate browser scenario: {title}")
                observed.add(title)
                tests = spec.get("tests", [])
                if len(tests) != 1:
                    errors.append(f"{title}: expected exactly one Chromium test")
                    continue
                test = tests[0]
                results = test.get("results", [])
                if (test.get("projectName") != "chromium" or
                        test.get("expectedStatus") != "passed" or
                        test.get("status") != "expected" or len(results) != 1 or
                        results[0].get("status") != "passed"):
                    errors.append(f"{title}: missing successful Chromium execution")
            visit(suite.get("suites", []))

    visit(report.get("suites", []))
    for title in sorted(expected - observed):
        errors.append(f"{title}: missing browser scenario")
    if report.get("errors"):
        errors.append("Playwright reported a global error")
    return errors


def isolated_environment(source: dict[str, str], database: str, directory: Path) -> dict[str, str]:
    if source.get("SUB2API_E2E_ISOLATED") != "1":
        raise ValueError("SUB2API_E2E_ISOLATED=1 is required for the disposable service environment")
    dsn = source.get("SUB2API_TEST_POSTGRES_DSN", "")
    parsed = urlsplit(dsn)
    if parsed.scheme not in ("postgres", "postgresql") or not parsed.hostname or not parsed.path.strip("/"):
        raise ValueError("SUB2API_TEST_POSTGRES_DSN must be an explicit PostgreSQL URL for the disposable service")
    redis_address = source.get("SUB2API_TEST_REDIS_ADDR", "")
    redis_host, separator, redis_port = redis_address.rpartition(":")
    if not separator or not redis_host or not redis_port.isdigit():
        raise ValueError("SUB2API_TEST_REDIS_ADDR must identify the disposable Redis host and port")
    redis_db = source.get("SUB2API_TEST_REDIS_DB")
    if redis_db is None or not redis_db.isdigit() or int(redis_db) < 1:
        raise ValueError("SUB2API_TEST_REDIS_DB must explicitly identify a nonzero isolated logical database")
    environment = dict(source)
    for key in validation_runtime.PROXY_ENV_NAMES:
        environment.pop(key, None)
    # No ambient production setup/configuration may redirect this process.
    for key in tuple(environment):
        if key.startswith(("DATABASE_", "REDIS_", "ADMIN_", "JWT_")) or key in ("DATA_DIR", "CONFIG_PATH", "CONFIG_FILE"):
            environment.pop(key, None)
    admin_email = "browser-admin@example.test"
    admin_password = "Isolated-browser-" + secrets.token_hex(12)
    query = parse_qs(parsed.query)
    environment.update({
        "SUB2API_TEST_POSTGRES_DSN": urlunsplit(parsed._replace(path="/" + quote(database, safe=""))),
        "AUTO_SETUP": "true", "DATA_DIR": str(directory / "data"),
        "DATABASE_HOST": parsed.hostname, "DATABASE_PORT": str(parsed.port or 5432),
        "DATABASE_USER": unquote(parsed.username or "postgres"),
        "DATABASE_PASSWORD": unquote(parsed.password or ""), "DATABASE_DBNAME": database,
        "DATABASE_SSLMODE": query.get("sslmode", ["disable"])[0],
        "REDIS_HOST": redis_host.strip("[]"), "REDIS_PORT": redis_port,
        "REDIS_USERNAME": source.get("SUB2API_TEST_REDIS_USERNAME", ""),
        "REDIS_PASSWORD": source.get("SUB2API_TEST_REDIS_PASSWORD", ""), "REDIS_DB": redis_db,
        "ADMIN_EMAIL": admin_email, "ADMIN_PASSWORD": admin_password,
        "JWT_SECRET": secrets.token_hex(32), "SERVER_HOST": "127.0.0.1", "SERVER_PORT": str(APP_PORT),
        "SERVER_MODE": "release", "SSL_CERT_FILE": str(directory / "ca.pem"),
        "SUB2API_E2E_BASE_URL": f"http://127.0.0.1:{VITE_PORT}",
        "SUB2API_E2E_FIXTURE_URL": f"http://127.0.0.1:{CONTROL_PORT}",
        "SUB2API_E2E_ADMIN_EMAIL": admin_email, "SUB2API_E2E_ADMIN_PASSWORD": admin_password,
        "SUB2API_E2E_OUTPUT_DIR": str(directory / "browser-results"),
        "VITE_DEV_PROXY_TARGET": f"http://127.0.0.1:{APP_PORT}", "VITE_DEV_PORT": str(VITE_PORT),
    })
    return environment


def wait_http(url: str, processes: list[subprocess.Popen[bytes]], timeout: float = 90) -> None:
    opener = build_opener(ProxyHandler({}))
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if any(process.poll() is not None for process in processes):
            raise RuntimeError("an isolated browser service exited before becoming ready")
        try:
            with opener.open(url, timeout=2) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        time.sleep(0.2)
    raise RuntimeError("an isolated browser service did not become ready")


def stop_process(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=8)
    except subprocess.TimeoutExpired:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)


def verify_network_observation() -> None:
    """A deliberate direct TCP/request proves counters before a clean reset."""
    with socket.create_connection(("127.0.0.2", 80), timeout=3) as connection:
        connection.sendall(b"GET /json/ HTTP/1.1\r\nHost: ip-api.com\r\nAuthorization: fixture-canary\r\nConnection: close\r\n\r\n")
        response = connection.recv(4096)
        if b"502" not in response.split(b"\r\n", 1)[0]:
            raise RuntimeError("the real direct sentinel did not reject the canary request")
    opener = build_opener(ProxyHandler({}))
    with opener.open(f"http://127.0.0.1:{CONTROL_PORT}/control/state", timeout=3) as response:
        state = json.load(response)
    if state["direct_connections"] < 1 or state["credentials_leaked"] is not True:
        raise RuntimeError("direct-connection or credential-leak observations are not live")
    from urllib.request import Request
    with opener.open(Request(f"http://127.0.0.1:{CONTROL_PORT}/control/reset", method="POST"), timeout=3):
        pass


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--grep", help="optional focused Playwright title filter")
    arguments = parser.parse_args()
    validation_runtime.require_in_validation(tool="seven-provider real browser regression")
    if os.geteuid() != 0:
        raise ValueError("the isolated browser container must run as root to bind local sentinels and restore /etc/hosts")
    if os.environ.get("SUB2API_E2E_ISOLATED") != "1":
        raise ValueError("only an explicitly disposable PostgreSQL/Redis environment is accepted")
    with tempfile.TemporaryDirectory(prefix="sub2api-browser-") as temporary:
        directory = Path(temporary)
        (directory / "data").mkdir()
        database = "sub2api_browser_" + secrets.token_hex(8)
        environment = isolated_environment(dict(os.environ), database, directory)
        original_environment = dict(os.environ)
        for key in validation_runtime.PROXY_ENV_NAMES:
            original_environment.pop(key, None)
        fixture_binary = directory / "browser-fixture"
        app_binary = directory / "server"
        subprocess.run(["go", "build", "-tags=e2e", "-o", str(fixture_binary), "./cmd/test-browser-fixture"], cwd=ROOT / "backend", env=original_environment, check=True)
        subprocess.run(["go", "build", "-o", str(app_binary), "./cmd/server"], cwd=ROOT / "backend", env=original_environment, check=True)
        subprocess.run(["pnpm", "exec", "tsc", "--noEmit", "--project", "e2e/tsconfig.json"], cwd=ROOT / "frontend", env=environment, check=True)
        # The fixture accepts only its generated database namespace. Connecting
        # the application to the base integration database is never necessary.
        hosts = Path("/etc/hosts")
        previous_hosts: bytes | None = None
        database_created = False
        processes: list[subprocess.Popen[bytes]] = []
        logs = []
        try:
            subprocess.run([str(fixture_binary), "-init-database", database], env=original_environment, check=True)
            database_created = True
            previous_hosts = hosts.read_bytes()
            with hosts.open("ab") as output:
                output.write(("\n127.0.0.2 " + " ".join(PROBE_HOSTS) + "\n127.0.0.3 " + " ".join(PROVIDER_HOSTS) + "\n").encode())

            def launch(name: str, command: list[str], cwd: Path) -> subprocess.Popen[bytes]:
                stream = (directory / f"{name}.log").open("wb")
                logs.append(stream)
                process = subprocess.Popen(command, cwd=cwd, env=environment, stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
                processes.append(process)
                return process

            launch("fixture", [str(fixture_binary), "-control-port", str(CONTROL_PORT), "-proxy-base-port", str(PROXY_BASE_PORT), "-ca-dir", str(directory), "-hosts", ",".join(PROBE_HOSTS + PROVIDER_HOSTS)], ROOT)
            wait_http(f"http://127.0.0.1:{CONTROL_PORT}/control/info", processes)
            verify_network_observation()
            launch("application", [str(app_binary)], ROOT / "backend")
            wait_http(f"http://127.0.0.1:{APP_PORT}/health", processes, timeout=180)
            launch("vite", ["pnpm", "exec", "vite", "--host", "127.0.0.1", "--strictPort"], ROOT / "frontend")
            wait_http(f"http://127.0.0.1:{VITE_PORT}/login", processes)
            report_path = directory / "browser-report.json"
            environment["PLAYWRIGHT_JSON_OUTPUT_FILE"] = str(report_path)
            command = ["pnpm", "test:e2e", "--reporter=list,json"]
            if arguments.grep:
                command.extend(["--grep", arguments.grep])
            result = subprocess.run(command, cwd=ROOT / "frontend", env=environment, check=False)
            if not report_path.is_file():
                print("Browser coverage failed: missing Playwright JSON report", flush=True)
                return 1
            errors = browser_coverage_errors(json.loads(report_path.read_text()), grep=arguments.grep)
            for error in errors:
                print(f"Browser coverage failed: {error}", flush=True)
            if not errors and result.returncode == 0:
                count = sum(arguments.grep is None or re.search(arguments.grep, title) is not None
                            for title in REQUIRED_MANAGEMENT_TITLES)
                print(f"Browser coverage: {count} required scenarios passed", flush=True)
            return result.returncode or int(bool(errors))
        except BaseException:
            # Logs contain only disposable data; redact known credentials before
            # returning startup diagnostics and remove all files on exit.
            for path in directory.glob("*.log"):
                content = path.read_text(errors="replace")[-6000:]
                for key in ("ADMIN_PASSWORD", "DATABASE_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET"):
                    secret = environment.get(key)
                    if secret:
                        content = content.replace(secret, "[redacted]")
                print(f"{path.stem} startup diagnostics:\n{content}", flush=True)
            raise
        finally:
            try:
                for process in reversed(processes):
                    stop_process(process)
                for stream in logs:
                    stream.close()
            finally:
                try:
                    if previous_hosts is not None:
                        hosts.write_bytes(previous_hosts)
                finally:
                    if database_created:
                        subprocess.run([str(fixture_binary), "-drop-database", database], env=original_environment, check=True)


if __name__ == "__main__":
    raise SystemExit(main())
