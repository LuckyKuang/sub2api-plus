#!/usr/bin/env python3
"""Exercise the reviewed official Codex binary against a local Responses fixture.

Run only in the repository validation container. No OpenAI account, API key, or
real provider is used. --archive can reuse an already downloaded release archive;
the complete independent release size and SHA-256 checks always remain active.

This proves the pinned binary's HTTP headers, request model, task JSONL, and
static credential-projection behavior for the controlled provider below. It
does not prove gateway identity-source selection or arbitrary direct-socket
absence. The external proxy trap observes only requests that honor proxy env.
"""

from __future__ import annotations

import argparse
import base64
from contextlib import contextmanager
import gzip
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
from typing import Any, Iterator
import urllib.request
import urllib.parse
import urllib.error

from validation_runtime import ValidationRuntimeError, require_in_validation


# Independent reviewed fixtures from the official rust-v0.165.0 release assets.
# Do not derive expected versions, digests, or wire values from production code.
VERSION = "0.165.0"
SOURCE_COMMIT = "360319d8dee62d87e2d59fc0c21805d79519cfed"
RELEASE_URL = "https://github.com/openai/codex/releases/download/rust-v0.165.0/"
ARTIFACTS = {
    "arm64": {
        "name": "codex-aarch64-unknown-linux-musl.tar.gz",
        "entry": "codex-aarch64-unknown-linux-musl",
        "sha256": "6522e056d0be7f531da915c9a81ae05bc70811b782542499cc0a9776d3abf5d3",
        "size": 107804617,
    },
    "amd64": {
        "name": "codex-x86_64-unknown-linux-musl.tar.gz",
        "entry": "codex-x86_64-unknown-linux-musl",
        "sha256": "be635d3486a5bd060d0fff582ae6d30160280c31344dd85719707fa920e187f0",
        "size": 115837472,
    },
}
MODEL = "gpt-6.1-sol"
PROMPT = "Return exactly wire-ok. Do not run tools."
WORKSPACE_ID = "workspace-wire-fixture"
MAX_BODY = 8 << 20
EXPECTED_USAGE = {
    "input_tokens": 21, "cached_input_tokens": 7, "cache_write_input_tokens": 3,
    "output_tokens": 6, "reasoning_output_tokens": 2,
}
IDENTITIES = (
    {
        "case": "compiled-default",
        "user_agent": "codex_cli_rs/0.165.0 (Ubuntu 24.04; x86_64) xterm-256color",
        "originator": "codex_cli_rs",
        "version": "0.165.0",
    },
    {
        "case": "account-vscode-override",
        "user_agent": "codex_vscode/0.164.0 (Mac OS X 15.1.0; arm64) iTerm.app",
        "originator": "codex_vscode",
        "version": "0.164.0",
    },
    {
        "case": "global-tui-override",
        "user_agent": "codex-tui/0.163.0 (Ubuntu 22.04; x86_64) tmux/3.4",
        "originator": "codex-tui",
        "version": "0.163.0",
    },
)


class WireFailure(RuntimeError):
    """A safe diagnostic code that never contains raw credentials or content."""


def require(condition: bool, code: str) -> None:
    if not condition:
        raise WireFailure(code)


def encode_jwt(claims: dict[str, Any]) -> str:
    def part(value: dict[str, Any]) -> str:
        return base64.urlsafe_b64encode(json.dumps(value).encode()).rstrip(b"=").decode()

    return ".".join((part({"alg": "none", "typ": "JWT"}), part(claims), "synthetic-signature"))


def synthetic_tokens(plan: str) -> tuple[str, str]:
    auth_claims = {"chatgpt_account_id": WORKSPACE_ID, "chatgpt_plan_type": plan}
    access = encode_jwt({
        "sub": "wire-fixture-user", "exp": 4070908800,
        "https://api.openai.com/auth": auth_claims,
    })
    identity = encode_jwt({
        "sub": "wire-fixture-user", "exp": 4070908800,
        "email": "wire-fixture@example.invalid",
        "https://api.openai.com/auth": auth_claims,
    })
    return access, identity


ACCESS_TOKEN, ID_TOKEN = synthetic_tokens("plus")


class FixtureServer(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False

    def __init__(self, handler: type[BaseHTTPRequestHandler], *, reject: bool = False):
        self.records: list[dict[str, Any]] = []
        self.errors: list[str] = []
        self.reject = reject
        self.record_lock = threading.Lock()
        super().__init__(("127.0.0.1", 0), handler)

    def record(self, record: dict[str, Any]) -> None:
        with self.record_lock:
            self.records.append(record)


class QuietHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_: Any) -> None:
        pass

    def reply(self, status: int, body: bytes, content_type: str) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass
        self.close_connection = True


class ProxyTrap(QuietHandler):
    def reject_proxy(self) -> None:
        # Record no headers: even fixture bearer values are unnecessary here.
        self.server.record({"method": self.command, "target": self.path})
        self.reply(502, b'{"error":"external-network-disabled-by-wire-fixture"}', "application/json")

    do_CONNECT = reject_proxy
    do_GET = reject_proxy
    do_POST = reject_proxy
    do_PUT = reject_proxy
    do_DELETE = reject_proxy
    do_HEAD = reject_proxy
    do_OPTIONS = reject_proxy
    do_PATCH = reject_proxy


def sse_fixture() -> bytes:
    message = {
        "id": "msg_wire", "type": "message", "role": "assistant",
        "phase": "final_answer", "status": "completed",
        "content": [{"type": "output_text", "text": "wire-ok", "annotations": []}],
    }
    response = {
        "id": "resp_wire", "object": "response", "created_at": 1,
        "model": MODEL, "status": "completed", "output": [message],
        "usage": {
            "input_tokens": 21, "input_tokens_details": {"cached_tokens": 7, "cache_write_tokens": 3},
            "output_tokens": 6, "output_tokens_details": {"reasoning_tokens": 2},
            "total_tokens": 27,
        },
    }
    events = [
        {"type": "response.created", "response": {"id": "resp_wire", "status": "in_progress"}},
        {"type": "response.output_item.added", "output_index": 0,
         "item": {**message, "status": "in_progress", "content": []}},
        {"type": "response.content_part.added", "item_id": "msg_wire", "output_index": 0,
         "content_index": 0, "part": {"type": "output_text", "text": "", "annotations": []}},
        {"type": "response.output_text.delta", "item_id": "msg_wire", "output_index": 0,
         "content_index": 0, "delta": "wire-ok"},
        {"type": "response.output_text.done", "item_id": "msg_wire", "output_index": 0,
         "content_index": 0, "text": "wire-ok"},
        {"type": "response.content_part.done", "item_id": "msg_wire", "output_index": 0,
         "content_index": 0, "part": message["content"][0]},
        {"type": "response.output_item.done", "output_index": 0, "item": message},
        {"type": "response.completed", "response": response},
    ]
    return "".join(
        "event: " + event["type"] + "\ndata: " + json.dumps(event) + "\n\n"
        for event in events
    ).encode() + b"data: [DONE]\n\n"


class ResponsesFixture(QuietHandler):
    def read_body(self) -> bytes:
        if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
            body = bytearray()
            while True:
                line = self.rfile.readline(128)
                size = int(line.split(b";", 1)[0].strip(), 16)
                require(size >= 0 and len(body) + size <= MAX_BODY, "fixture_request_size")
                if size == 0:
                    while self.rfile.readline(8192).strip():
                        pass
                    return bytes(body)
                data = self.rfile.read(size)
                require(len(data) == size and self.rfile.read(2) == b"\r\n", "fixture_request_chunk")
                body.extend(data)
        size = int(self.headers.get("Content-Length", "0"))
        require(0 <= size <= MAX_BODY, "fixture_request_size")
        body = self.rfile.read(size)
        require(len(body) == size, "fixture_request_truncated")
        return body

    def do_POST(self) -> None:
        record: dict[str, Any] = {
            "method": self.command, "path": self.path,
            "headers": list(self.headers.raw_items()),
        }
        try:
            body = self.read_body()
            encoding = self.headers.get("Content-Encoding", "").lower()
            if encoding == "gzip":
                body = gzip.decompress(body)
            else:
                require(encoding in {"", "identity"}, "fixture_unsupported_content_encoding")
            require(len(body) <= MAX_BODY, "fixture_request_size")
            record["body"] = json.loads(body)
        except (ValueError, OSError, WireFailure):
            self.server.errors.append("fixture_request_decode_failed")
            self.server.record(record)
            self.reply(400, b'{"error":"fixture_request_decode_failed"}', "application/json")
            return
        self.server.record(record)
        if self.path != "/backend-api/codex/responses":
            self.reply(404, b'{"error":"unexpected_endpoint"}', "application/json")
        elif self.server.reject:
            self.reply(401, json.dumps({"error": {
                "code": "token_expired", "type": "invalid_token",
                "message": "Synthetic fixture authentication rejection",
            }}).encode(), "application/json")
        else:
            self.reply(200, sse_fixture(), "text/event-stream")

    def do_GET(self) -> None:
        self.server.record({"method": self.command, "path": self.path, "headers": []})
        self.reply(404, b'{"error":"unexpected_endpoint"}', "application/json")


@contextmanager
def serving(handler: type[BaseHTTPRequestHandler], *, reject: bool = False) -> Iterator[FixtureServer]:
    server = FixtureServer(handler, reject=reject)
    worker = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    worker.start()
    try:
        yield server
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=5)


def checked_artifact(archive_option: Path | None, root: Path) -> tuple[Path, dict[str, Any]]:
    arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(platform.machine())
    require(platform.system() == "Linux" and arch in ARTIFACTS, "unsupported_validation_architecture")
    artifact = ARTIFACTS[arch]
    manifest = (Path(__file__).resolve().parents[1] / "backend/internal/codexcli/manifest.go").read_text()
    require(re.search(r'const targetVersion = "0\.165\.0"', manifest) is not None, "manifest_version_changed_review_required")
    for manifest_arch, expected in ARTIFACTS.items():
        block = re.search(r'"linux/' + manifest_arch + r'":\s*\{([^}]+)\}', manifest)
        require(block is not None, "manifest_artifact_missing")
        for value in expected.values():
            require(str(value) in block.group(1), "manifest_artifact_changed_review_required")
    archive = archive_option or root / artifact["name"]
    if archive_option is None:
        request = urllib.request.Request(RELEASE_URL + artifact["name"], headers={"User-Agent": "sub2api-codex-wire-regression"})
        try:
            with urllib.request.urlopen(request, timeout=120) as source, archive.open("xb") as target:
                count = 0
                while chunk := source.read(1 << 20):
                    count += len(chunk)
                    require(count <= artifact["size"], "release_archive_oversized")
                    target.write(chunk)
        except (OSError, urllib.error.URLError) as exc:
            raise WireFailure("release_archive_download_failed") from exc
    require(archive.is_file() and archive.stat().st_size == artifact["size"], "release_archive_size_mismatch")
    digest = hashlib.sha256()
    with archive.open("rb") as source:
        while chunk := source.read(1 << 20):
            digest.update(chunk)
    require(digest.hexdigest() == artifact["sha256"], "release_archive_digest_mismatch")
    binary = root / "codex"
    with tarfile.open(archive, "r:gz") as source:
        entries = [entry for entry in source.getmembers() if entry.name == artifact["entry"]]
        require(len(entries) == 1 and entries[0].isfile(), "release_executable_entry_invalid")
        require(0 < entries[0].size <= 512 << 20, "release_executable_size_invalid")
        member = source.extractfile(entries[0])
        require(member is not None, "release_executable_missing")
        with member, binary.open("xb") as target:
            shutil.copyfileobj(member, target, 1 << 20)
        require(binary.stat().st_size == entries[0].size, "release_executable_truncated")
    binary.chmod(0o700)
    return binary, artifact


def task_environment(home: Path, proxy_port: int, access_token: str) -> dict[str, str]:
    (home / ".tmp").mkdir(mode=0o700)
    proxy = f"http://127.0.0.1:{proxy_port}"
    return {
        "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
        "HOME": str(home), "CODEX_HOME": str(home), "TMPDIR": str(home / ".tmp"),
        "XDG_CONFIG_HOME": str(home / ".config"), "XDG_DATA_HOME": str(home / ".local/share"),
        "XDG_CACHE_HOME": str(home / ".cache"), "TERM": "dumb", "NO_COLOR": "1", "LANG": "C.UTF-8",
        "SUB2API_CODEX_ACCESS_TOKEN": access_token,
        "HTTP_PROXY": proxy, "HTTPS_PROXY": proxy, "ALL_PROXY": proxy,
        "NO_PROXY": "127.0.0.1,localhost",
        "http_proxy": proxy, "https_proxy": proxy, "all_proxy": proxy,
        "no_proxy": "127.0.0.1,localhost",
    }


def run_process(binary: Path, args: list[str], home: Path, work: Path, proxy_port: int, *, prompt: str = "", access_token: str = ACCESS_TOKEN) -> subprocess.CompletedProcess[bytes]:
    process = subprocess.Popen(
        [str(binary), *args], cwd=work, env=task_environment(home, proxy_port, access_token),
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        start_new_session=True,
    )
    try:
        stdout, stderr = process.communicate(prompt.encode(), timeout=90)
    except BaseException:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        raise
    require(len(stdout) <= 8 << 20 and len(stderr) <= 1 << 20, "cli_output_limit_exceeded")
    return subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)


def managed_config(identity: dict[str, str], fixture_port: int, *, reasoning_effort: str = "low", trusted_project: Path | None = None) -> str:
    headers = {
        "User-Agent": identity["user_agent"], "Originator": identity["originator"],
        "Version": identity["version"], "Chatgpt-Account-Id": WORKSPACE_ID,
    }
    lines = [
        'model_provider = "sub2api_managed_codex"',
        # A different default proves --model reaches the actual upstream body.
        'model = "gpt-6-sol"',
        'model_reasoning_effort = ' + json.dumps(reasoning_effort),
        # protocol/src/openai_models.rs resolves this sentinel to no paid tier,
        # even when a model catalog advertises a tier default.
        'service_tier = "default"',
        'sandbox_mode = "danger-full-access"', 'approval_policy = "never"',
        'cli_auth_credentials_store = "file"', 'check_for_update_on_startup = false',
        'web_search = "disabled"',
        '[analytics]', 'enabled = false', '[feedback]', 'enabled = false',
        '[otel]', 'exporter = "none"', 'trace_exporter = "none"',
        'metrics_exporter = "none"', 'log_user_prompt = false',
        '[features]', 'plugins = false', 'remote_plugin = false', 'plugin_sharing = false',
        'apps = false', 'tool_suggest = false', 'recommended_plugins = false',
        '[cloud.skills]', 'enabled = false',
        '[model_providers.sub2api_managed_codex]',
        'name = "Sub2API managed Codex"',
        f'base_url = "http://127.0.0.1:{fixture_port}/backend-api/codex"',
        'wire_api = "responses"', 'env_key = "SUB2API_CODEX_ACCESS_TOKEN"',
        'requires_openai_auth = false', 'supports_websockets = false',
        'request_max_retries = 0', 'stream_max_retries = 0', 'stream_idle_timeout_ms = 5000',
        'http_headers = { ' + ", ".join(json.dumps(name) + " = " + json.dumps(value) for name, value in headers.items()) + ' }',
    ]
    if trusted_project is not None:
        lines.extend(['[projects.' + json.dumps(str(trusted_project)) + ']', 'trust_level = "trusted"'])
    return "\n".join(lines) + "\n"


def parse_events(output: bytes) -> list[dict[str, Any]]:
    events = []
    try:
        for line in output.splitlines():
            if line.strip():
                event = json.loads(line)
                require(isinstance(event, dict) and isinstance(event.get("type"), str), "cli_invalid_jsonl_event")
                events.append(event)
    except (ValueError, UnicodeError) as exc:
        raise WireFailure("cli_invalid_jsonl") from exc
    return events


def single_header(record: dict[str, Any], name: str, expected: str) -> None:
    values = [value for key, value in record["headers"] if key.lower() == name.lower()]
    require(len(values) == 1 and values[0] == expected, "wire_header_mismatch_" + name.lower())


def safe_proxy_records(records: list[dict[str, Any]]) -> list[dict[str, str]]:
    safe = []
    for record in records:
        method, target = record["method"], record["target"]
        parsed = urllib.parse.urlsplit("//" + target if method == "CONNECT" else target)
        safe.append({"method": method, "host": parsed.hostname or "", "path": parsed.path[:512]})
    return safe


def body_identity_declarations(body: dict[str, Any], identity: dict[str, str]) -> list[str]:
    expected = {
        "user-agent": identity["user_agent"], "originator": identity["originator"],
        "version": identity["version"], "client-version": identity["version"],
    }
    declarations = []
    # Do not mistake arbitrary tool JSON schemas or user text for declarations.
    for scope, fields in (("body", body), ("client_metadata", body.get("client_metadata", {}))):
        if not isinstance(fields, dict):
            continue
        for key, value in fields.items():
            normalized = key.lower().replace("_", "-")
            if normalized in expected and value is not None:
                require(value == expected[normalized], "wire_body_identity_mismatch_" + normalized)
                declarations.append(scope + "." + key)
    return sorted(declarations)


def assert_reviewed_task_tools(body: dict[str, Any], label: str) -> dict[str, Any]:
    # Independent fixed-version contract: rust-v0.165.0 models-manager/models.json
    # marks gpt-6.1-sol use_responses_lite=true. core/src/client.rs puts tool
    # declarations in a developer additional_tools input item; codex-api/common.rs
    # omits the root tools field. protocol/src/models.rs defines the item schema.
    items = [item for item in body["input"] if isinstance(item, dict) and item.get("type") == "additional_tools"]
    shape = {
        "root_tools_present": "tools" in body,
        "root_tools_type": type(body.get("tools")).__name__,
        "input_item_types": [item.get("type", "missing") for item in body["input"] if isinstance(item, dict)],
        "additional_tools_items": len(items),
        "additional_tools_shapes": [{"keys": sorted(item), "tools_type": type(item.get("tools")).__name__,
                                      "tool_count": len(item["tools"]) if isinstance(item.get("tools"), list) else None}
                                     for item in items],
    }
    valid = "tools" not in body and len(items) == 1
    if valid:
        declarations = items[0].get("tools")
        valid = items[0].get("role") == "developer" and isinstance(declarations, list) and bool(declarations)
        if valid:
            # tool_spec.rs defines these wire variants. Web search is disabled
            # by this fixture; function/custom specs are grouped into namespaces.
            valid = all(isinstance(tool, dict) and tool.get("type") in {"namespace", "tool_search"}
                        for tool in declarations)
            for tool in declarations:
                if not valid:
                    break
                if tool["type"] == "namespace":
                    nested = tool.get("tools")
                    valid = isinstance(tool.get("name"), str) and bool(tool["name"])
                    valid = valid and isinstance(nested, list) and bool(nested)
                    valid = valid and all(isinstance(entry, dict) and entry.get("type") in {"function", "custom"}
                                          and isinstance(entry.get("name"), str) and bool(entry["name"])
                                          for entry in nested)
    if not valid:
        print(json.dumps({"case": label, "safe_task_tool_shape": shape}), file=sys.stderr)
        raise WireFailure(label + ":reviewed_additional_tools_contract_mismatch")
    return shape


def run_case(binary: Path, root: Path, proxy: FixtureServer, identity: dict[str, str], *, reject: bool = False, plan: str = "plus", execution_auth_mode: str = "apikey-projection") -> dict[str, Any]:
    expected_effort = "high" if plan == "business" else "low"
    label = identity["case"] + ("-" + plan if plan != "plus" else "") + ("-401" if reject else "")
    case_root = root / label
    ancestor = case_root / "trusted-git-ancestor" if plan == "business" else None
    home = case_root / "home"
    work = ancestor / "private-task" / "workspace" if ancestor is not None else case_root / "workspace"
    home.mkdir(mode=0o700, parents=True)
    work.mkdir(mode=0o700, parents=True)
    ancestor_config: Path | None = None
    ancestor_config_bytes: bytes | None = None
    if ancestor is not None:
        initialized = subprocess.run(
            ["git", "init", "--quiet", str(ancestor)],
            env={"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
                 "HOME": str(home), "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull},
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False,
        )
        require(initialized.returncode == 0 and (ancestor / ".git" / "HEAD").is_file(), label + ":ancestor_git_fixture_failed")
        project_directory = ancestor / ".codex"
        project_directory.mkdir(mode=0o700)
        ancestor_config = project_directory / "config.toml"
        # rust-v0.165.0 codex-rs/config/src/loader/mod.rs: project layers override
        # HOME config, and default marker discovery accepts .git with HEAD.
        # Use valid, project-allowed fields: the differing saved effort proves
        # exclusion independently from the auxiliary-request proxy trap.
        ancestor_config_bytes = (
            'model_reasoning_effort = "low"\ncli_auth_credentials_store = "keyring"\n'
            '[features]\nplugins = true\nremote_plugin = true\nplugin_sharing = true\n'
            'apps = true\ntool_suggest = true\nrecommended_plugins = true\n'
            '[cloud.skills]\nenabled = true\n'
        ).encode()
        ancestor_config.write_bytes(ancestor_config_bytes)
        ancestor_config.chmod(0o600)
    access_token, id_token = synthetic_tokens(plan)
    canonical_auth_bytes = (json.dumps({
        "auth_mode": "chatgpt", "OPENAI_API_KEY": None,
        "tokens": {"access_token": access_token, "id_token": id_token,
                   "refresh_token": "synthetic-refresh-must-never-be-used", "account_id": WORKSPACE_ID},
    }) + "\n").encode()
    canonical_home = case_root / "account"
    canonical_home.mkdir(mode=0o700)
    canonical_file = canonical_home / "auth.json"
    canonical_file.write_bytes(canonical_auth_bytes)
    canonical_file.chmod(0o600)
    auth_bytes = canonical_auth_bytes
    if execution_auth_mode == "apikey-projection":
        auth_bytes = (json.dumps({"auth_mode": "apikey", "OPENAI_API_KEY": access_token}) + "\n").encode()
    auth_file = home / "auth.json"
    auth_file.write_bytes(auth_bytes)
    auth_file.chmod(0o600)
    proxy_count = len(proxy.records)
    with serving(ResponsesFixture, reject=reject) as fixture:
        config = home / "config.toml"
        config.write_text(managed_config(identity, fixture.server_port, reasoning_effort=expected_effort, trusted_project=ancestor))
        config.chmod(0o600)
        # Exact loader/mod.rs discover_project_root returns None for [], so
        # find_project_root falls back to this fresh private cwd. The CLI flag
        # has precedence over HOME and excludes trusted ancestor project files.
        process = run_process(
            binary, ["exec", "--json", "--ephemeral", "--skip-git-repo-check", "-c", "project_root_markers=[]", "--model", MODEL, "-"],
            home, work, proxy.server_port, prompt=PROMPT, access_token=access_token,
        )
        require(auth_file.read_bytes() == auth_bytes, label + ":auth_snapshot_modified")
        require(canonical_file.read_bytes() == canonical_auth_bytes, label + ":canonical_oauth_snapshot_modified")
        if ancestor_config is not None:
            require(ancestor_config.read_bytes() == ancestor_config_bytes, label + ":ancestor_config_fixture_modified")
        require(not fixture.errors, label + ":fixture_decode_failed")
        if len(proxy.records) != proxy_count:
            print(json.dumps({"case": label, "proxy_observed_requests": safe_proxy_records(proxy.records[proxy_count:])}), file=sys.stderr)
            raise WireFailure(label + ":external_proxy_attempt")
        if len(fixture.records) != 1:
            diagnostic = process.stderr.decode("utf-8", errors="replace")
            for value in (access_token, id_token, "synthetic-refresh-must-never-be-used", PROMPT, str(root)):
                diagnostic = diagnostic.replace(value, "[fixture-redacted]")
            diagnostic = re.sub(r"[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]+", "[fixture-token]", diagnostic)
            print(json.dumps({"case": label, "cli_exit_code": process.returncode,
                              "fixture_request_count": len(fixture.records),
                              "sanitized_fixture_stderr": diagnostic[:2400]}), file=sys.stderr)
            raise WireFailure(label + ":unexpected_request_count_" + str(len(fixture.records)))
        record = fixture.records[0]
        require(record["method"] == "POST" and record["path"] == "/backend-api/codex/responses", label + ":unexpected_endpoint")
        for name, value in (
            ("User-Agent", identity["user_agent"]), ("Originator", identity["originator"]),
            ("Version", identity["version"]), ("Chatgpt-Account-Id", WORKSPACE_ID),
            ("Authorization", "Bearer " + access_token),
        ):
            single_header(record, name, value)
        body = record.get("body")
        require(isinstance(body, dict), label + ":missing_request_body")
        require(body.get("model") == MODEL, label + ":final_wire_model_mismatch")
        require(body.get("service_tier") is None, label + ":standard_task_requested_paid_service_tier")
        # Both levels are advertised for this fixed model in the reviewed
        # models-manager/models.json. The saved root account setting must reach
        # the actual wire; no command-line effort override is supplied.
        require(isinstance(body.get("reasoning"), dict)
                and body["reasoning"].get("effort") == expected_effort,
                label + ":saved_account_reasoning_effort_mismatch")
        require(body.get("stream") is True and body.get("store") is False, label + ":unexpected_responses_flags")
        require(isinstance(body.get("input"), list) and PROMPT in json.dumps(body["input"]), label + ":task_prompt_missing")
        tool_shape = assert_reviewed_task_tools(body, label)
        single_header(record, "X-OpenAI-Internal-Codex-Responses-Lite", "true")
        declarations = body_identity_declarations(body, identity)
        events = parse_events(process.stdout)
        event_types = [event["type"] for event in events]
        require("thread.started" in event_types and "turn.started" in event_types, label + ":task_start_events_missing")
        if reject:
            require(process.returncode != 0, label + ":unexpected_success")
            require(any(kind in event_types for kind in ("turn.failed", "error")), label + ":task_failure_event_missing")
            require("turn.completed" not in event_types, label + ":rejected_task_completed")
            usage: dict[str, Any] | None = None
        else:
            require(process.returncode == 0, label + ":cli_exit_" + str(process.returncode))
            completed = [event for event in events if event["type"] == "turn.completed"]
            require(len(completed) == 1, label + ":completion_event_missing_or_duplicate")
            usage = completed[0].get("usage")
            require(isinstance(usage, dict), label + ":task_usage_missing")
            for name, value in EXPECTED_USAGE.items():
                require(usage.get(name) == value, label + ":task_usage_mismatch_" + name)
            final_messages = [event.get("item", {}) for event in events
                              if event["type"] == "item.completed" and event.get("item", {}).get("type") == "agent_message"]
            require(bool(final_messages) and final_messages[-1].get("text") == "wire-ok", label + ":task_final_message_mismatch")
        return {
            "case": label, "request_count": len(fixture.records), "exit_code": process.returncode,
            "user_agent": identity["user_agent"], "originator": identity["originator"], "version": identity["version"],
            "trusted_headers_single_and_exact": True, "synthetic_dotted_bearer_exact": True,
            "final_wire_model": body["model"], "body_identity_declarations": declarations,
            "wire_reasoning_effort": body["reasoning"]["effort"],
            "wire_service_tier": body.get("service_tier"),
            "task_tool_shape": tool_shape,
            "jsonl_event_types": event_types, "task_usage": usage,
            "auth_snapshot_unchanged": True, "proxy_observed_external_attempts": 0,
            "canonical_oauth_plan": plan, "execution_auth_mode": execution_auth_mode,
            "account_config_reasoning_effort": expected_effort,
            "project_root_markers_override": [],
            "trusted_ancestor_project_config_excluded": True if ancestor is not None else None,
            "canonical_oauth_snapshot_unchanged": True,
        }


def run_transparent_bridge_case(binary: Path, root: Path) -> dict[str, Any]:
    """Independent exact-release proof; no agent loop or actual OAuth is used."""
    nonce = "bridge_fixture_0123456789abcdef"
    wire_body = b'{"model":"fixture-model","input":[{"role":"user","content":"unchanged"}],"tools":[{"type":"function","name":"caller_tool","parameters":{}}],"future":{"keep":true}}'
    wire_response = b'event: response.completed\ndata: {"type":"response.completed","response":{"id":"resp_bridge","status":"completed","future":true}}\n\n'
    captured: list[dict[str, Any]] = []

    class BridgeUpstream(QuietHandler):
        def do_POST(self) -> None:
            captured.append({"path": self.path, "authorization_ok": self.headers.get("Authorization") == "Bearer " + nonce,
                             "identity": self.headers.get("User-Agent"), "originator": self.headers.get("Originator"),
                             "version": self.headers.get("Version"), "body": self.rfile.read(int(self.headers["Content-Length"]))})
            self.reply(401 if self.server.reject else 200, wire_response, "text/event-stream")

    directory = root / "transparent-bridge"
    directory.mkdir(mode=0o700)
    info = directory / "server.json"
    with serving(BridgeUpstream) as target:
        process = subprocess.Popen([str(binary), "responses-api-proxy", "--server-info", str(info),
                                    "--upstream-url", f"http://127.0.0.1:{target.server_port}/trusted-owner"],
                                   cwd=directory, env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": str(directory),
                                                      "CODEX_HOME": str(directory), "NO_PROXY": "*", "no_proxy": "*"},
                                   stdin=subprocess.PIPE, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   start_new_session=True)
        try:
            process.stdin.write((nonce + "\n").encode())
            process.stdin.close()
            deadline = time.monotonic() + 10
            while not info.exists() and process.poll() is None and time.monotonic() < deadline:
                time.sleep(0.01)
            require(info.exists() and process.poll() is None, "transparent_bridge_startup_failed")
            status = json.loads(info.read_text())
            require(status["pid"] == process.pid and 0 < status["port"] < 65536, "transparent_bridge_invalid_startup")
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            for identity in IDENTITIES:
                request = urllib.request.Request(f'http://127.0.0.1:{status["port"]}/v1/responses', wire_body,
                                                 {"User-Agent": identity["user_agent"], "Originator": identity["originator"],
                                                  "Version": identity["version"], "Authorization": "Bearer not-provider-secret"}, method="POST")
                with opener.open(request, timeout=10) as response:
                    require(response.status == 200 and response.read() == wire_response, "transparent_bridge_response_changed")
                got = captured[-1]
                require(got == {"path": "/trusted-owner", "authorization_ok": True, "identity": identity["user_agent"],
                                "originator": identity["originator"], "version": identity["version"], "body": wire_body},
                        "transparent_bridge_request_changed")
            target.reject = True
            before = len(captured)
            try:
                opener.open(request, timeout=10)
                raise WireFailure("transparent_bridge_401_lost")
            except urllib.error.HTTPError as error:
                require(error.code == 401 and error.read() == wire_response, "transparent_bridge_401_changed")
            require(len(captured) == before + 1, "transparent_bridge_replayed_401")
            before = len(captured)
            try:
                opener.open(f'http://127.0.0.1:{status["port"]}/v1/responses', timeout=10)
                raise WireFailure("transparent_bridge_get_allowed")
            except urllib.error.HTTPError as error:
                require(error.code == 403, "transparent_bridge_get_not_forbidden")
            require(len(captured) == before, "transparent_bridge_forwarded_forbidden_request")
            return {"mode": "official-transparent-responses-http", "identities": len(IDENTITIES), "body_unchanged": True,
                    "sse_unchanged": True, "terminal_401_no_replay": True, "non_post_forbidden": True}
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)


def run_go_transparent_bridge(binary: Path) -> None:
    environment = os.environ.copy()
    environment["SUB2API_CODEX_BRIDGE_BINARY"] = str(binary.resolve())
    repository = Path(__file__).resolve().parents[1]
    result = subprocess.run(["go", "test", "-C", str(repository / "backend"), "-tags=unit", "./internal/codexcli",
                             "-run", "^TestCodexBridgeOfficialBinaryWire$", "-count=1", "-v"],
                            env=environment, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=180)
    output = result.stdout.decode("utf-8", errors="replace")
    # A real binary must be exercised. A missing fixture, skipped case or build
    # failure cannot silently reduce this gate to the proxy-only Python check.
    require(result.returncode == 0 and "--- PASS: TestCodexBridgeOfficialBinaryWire" in output
            and "--- SKIP:" not in output, "official_go_bridge_wire_failed")


def main() -> int:
    require_in_validation(tool="Official Codex CLI wire regression")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, help="reuse an archive; exact release size and SHA-256 are still mandatory")
    parser.add_argument("--execution-auth-mode", choices=("chatgpt", "apikey-projection"), default="apikey-projection",
                        help="default: static execution projection; chatgpt is a diagnostic mode expected to expose uncontrolled native bootstrap; the canonical OAuth fixture stays outside CODEX_HOME")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="sub2api-codex-wire-") as temporary:
        root = Path(temporary)
        binary, artifact = checked_artifact(args.archive, root)
        bridge_case = run_transparent_bridge_case(binary, root)
        run_go_transparent_bridge(binary)
        bridge_case["gateway_nonce_relay_wire"] = True
        with serving(ProxyTrap) as proxy:
            version_home, version_work = root / "version-home", root / "version-work"
            version_home.mkdir(mode=0o700)
            version_work.mkdir(mode=0o700)
            process = run_process(binary, ["--version"], version_home, version_work, proxy.server_port)
            require(process.returncode == 0 and process.stdout.strip() == b"codex-cli 0.165.0", "official_binary_version_mismatch")
            require(not proxy.records, "version_probe_external_proxy_attempt")
            cases = [run_case(binary, root, proxy, identity, execution_auth_mode=args.execution_auth_mode) for identity in IDENTITIES]
            cases.append(run_case(binary, root, proxy, IDENTITIES[0], reject=True, execution_auth_mode=args.execution_auth_mode))
            cases.append(run_case(binary, root, proxy, IDENTITIES[0], plan="business", execution_auth_mode=args.execution_auth_mode))
            cases.append(run_case(binary, root, proxy, IDENTITIES[0], plan="enterprise", reject=True, execution_auth_mode=args.execution_auth_mode))
            require(not proxy.records, "external_proxy_attempt")
        print(json.dumps({
            "result": "passed", "official_version": VERSION, "official_source_commit": SOURCE_COMMIT,
            "release_asset": artifact["name"], "release_sha256": artifact["sha256"],
            "release_size": artifact["size"], "cases": cases, "transparent_bridge": bridge_case,
            "scope": {
                "provider": "sub2api_managed_codex", "mode": "exec-task-jsonl-over-http-responses",
                "native_oauth_refresh": False, "automatic_task_replay": False,
                "execution_auth_mode": args.execution_auth_mode,
                "canonical_snapshot_fixture": "outside CODEX_HOME; production may retain the canonical grant only in its database",
                "disabled": ["startup-update", "analytics", "feedback", "web-search", "otel-export",
                             "plugins", "remote-plugin", "plugin-sharing", "apps", "tool-suggest",
                             "recommended-plugins", "cloud-skills"],
                "fresh_state_without_configured": ["MCP-servers", "plugins", "hooks"],
                "proxy_trap_limit": "observes proxy-aware requests only; does not prove absence of arbitrary direct sockets",
                "identity_priority_limit": "supplied snapshots only; gateway source selection requires its independent Go regressions",
                "provider_limit": "local protocol fixture; no real provider entitlement, live model, or service acceptance is established",
            },
        }, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (WireFailure, ValidationRuntimeError) as error:
        print(json.dumps({"result": "failed", "code": str(error)}), file=sys.stderr)
        raise SystemExit(1)
    except (OSError, ValueError, tarfile.TarError, subprocess.TimeoutExpired) as error:
        # Never expose raw subprocess stderr, provider content, or bearer values.
        print(json.dumps({"result": "failed", "code": "harness_exception_" + type(error).__name__}), file=sys.stderr)
        raise SystemExit(1)
