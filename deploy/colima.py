"""Local Colima deployment orchestration; all application builds run in Docker."""

from __future__ import annotations

import argparse
import contextlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time


ROOT = Path(__file__).resolve().parents[1]
LABEL = "org.sub2api.stack"
NAME = re.compile(r"[a-z0-9][a-z0-9_-]{0,47}\Z")


class DeploymentError(RuntimeError):
    pass


def read_env(path: Path) -> dict[str, str]:
    if not path.is_file() or path.is_symlink():
        raise DeploymentError("Environment file must be a regular file; run init first")
    metadata = path.stat()
    if metadata.st_uid != os.getuid() or metadata.st_mode & 0o077:
        raise DeploymentError("Environment file must belong to this user and have mode 600")
    values = {}
    for number, line in enumerate(path.read_text().splitlines(), 1):
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or not re.fullmatch(r"[A-Z][A-Z0-9_]*", key):
            raise DeploymentError(f"Invalid literal KEY=value at line {number}")
        if key in values:
            raise DeploymentError(f"Duplicate environment key at line {number}")
        values[key] = value
    return values


def private_json(path: Path, value: dict) -> None:
    descriptor, temporary = tempfile.mkstemp(dir=path.parent, prefix=".record-")
    try:
        with os.fdopen(descriptor, "w") as handle:
            json.dump(value, handle)
            handle.write("\n")
        os.replace(temporary, path)
    finally:
        Path(temporary).unlink(missing_ok=True)


class Deployment:
    def __init__(self, env_file: Path):
        self.env_file = env_file.resolve()
        self.values = read_env(env_file)
        self.profile = self.values.get("COLIMA_PROFILE", "sub2api")
        self.project = self.values.get("COLIMA_PROJECT", "sub2api-colima")
        for value in (self.profile, self.project):
            if not NAME.fullmatch(value):
                raise DeploymentError("Profile/project must use lowercase letters, digits, - or _")
        self.context = "colima" if self.profile == "default" else f"colima-{self.profile}"
        self.minio = self.values.get("MINIO_ENABLED", "false") == "true"
        if self.values.get("MINIO_ENABLED", "false") not in {"true", "false"}:
            raise DeploymentError("MINIO_ENABLED must be true or false")
        for key in ("COLIMA_CPUS", "COLIMA_MEMORY", "COLIMA_DISK"):
            value = self.values.get(key, {"COLIMA_CPUS": "4", "COLIMA_MEMORY": "8", "COLIMA_DISK": "60"}[key])
            if not value.isdigit() or int(value) < 1:
                raise DeploymentError(f"{key} must be a positive integer")
        self.dns = [value.strip() for value in self.values.get("COLIMA_DNS", "").split(",") if value.strip()]
        for address in self.dns:
            try:
                ipaddress.ip_address(address)
            except ValueError as error:
                raise DeploymentError("COLIMA_DNS must contain comma-separated DNS IP addresses") from error
        if self.values.get("COLIMA_RETAIN_PREVIOUS", "false") not in {"true", "false"}:
            raise DeploymentError("COLIMA_RETAIN_PREVIOUS must be true or false")
        self.host = self.values.get("BIND_HOST", "127.0.0.1")
        self.port = self.values.get("SERVER_PORT", "18080")
        self.validate_address(self.host, self.port)
        if self.minio:
            self.validate_address(self.values.get("MINIO_BIND_HOST", "127.0.0.1"), self.values.get("MINIO_API_PORT", "19000"))
            self.validate_address(self.values.get("MINIO_BIND_HOST", "127.0.0.1"), self.values.get("MINIO_CONSOLE_PORT", "19001"))
            if len({self.port, self.values.get("MINIO_API_PORT", "19000"), self.values.get("MINIO_CONSOLE_PORT", "19001")}) != 3:
                raise DeploymentError("Web and MinIO ports must be distinct")
            if len(self.values.get("MINIO_ROOT_USER", "")) < 3 or len(self.values.get("MINIO_ROOT_PASSWORD", "")) < 8:
                raise DeploymentError("MinIO requires a user (3+ characters) and password (8+ characters)")
            bucket = self.values.get("MINIO_BUCKET", "sub2api-images")
            if not re.fullmatch(r"[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]", bucket):
                raise DeploymentError("MINIO_BUCKET must be a valid S3 bucket name")
        if not self.values.get("POSTGRES_PASSWORD") or self.values["POSTGRES_PASSWORD"] == "change_this_secure_password":
            raise DeploymentError("Set POSTGRES_PASSWORD before deploying")
        for key in ("JWT_SECRET", "TOTP_ENCRYPTION_KEY"):
            if not self.values.get(key):
                raise DeploymentError(f"A stable {key} is required; init generates it")
        self.state_dir = Path(os.environ.get("SUB2API_COLIMA_STATE_DIR", str(Path.home() / ".local/state/sub2api/colima"))) / self.profile / self.project
        self.record_path = self.state_dir / "deployment.json"
        self.current = f"sub2api-colima/{self.project}:local-current"
        self.candidate = f"sub2api-colima/{self.project}:local-candidate"
        self.recovery = f"sub2api-colima/{self.project}:local-recovery"
        self.command_env = {key: value for key, value in os.environ.items() if not key.startswith(("DOCKER_", "COMPOSE_"))}
        self.native_env = {key: value for key, value in self.command_env.items()
                           if not key.startswith("COLIMA_") or key == "COLIMA_HOME"}
        # Literal values are passed through the process environment, never sourced
        # or recursively expanded by Compose. Runtime-control keys stay private.
        self.command_env.update({key: value for key, value in self.values.items() if not key.startswith(("DOCKER_", "COMPOSE_"))})
        self.command_env["COLIMA_PROJECT"] = self.project
        self.command_env["COMPOSE_PROFILES"] = "colima-minio" if self.minio else ""
        self.temporary: Path | None = None

    @staticmethod
    def validate_address(host: str, port: str) -> None:
        try:
            ipaddress.IPv4Address(host)
            if not port.isdigit() or not 1025 <= int(port) <= 65535:
                raise ValueError
        except ValueError as error:
            raise DeploymentError("Bind address must be IPv4 and port must be 1025-65535") from error

    def run(self, args: list[str], *, check: bool = True, visible: bool = False, timeout: int = 120) -> subprocess.CompletedProcess:
        try:
            result = subprocess.run(args, cwd=ROOT, env=self.native_env if args[0] == "colima" else self.command_env, text=True,
                                    stdout=None if visible else subprocess.PIPE,
                                    stderr=None if visible else subprocess.PIPE, timeout=timeout)
        except (OSError, subprocess.TimeoutExpired) as error:
            raise DeploymentError(f"Command could not complete: {args[0]}") from error
        if check and result.returncode:
            # Docker/Compose errors may echo interpolated secrets; do not relay them.
            raise DeploymentError(f"Command failed: {args[0]} (exit {result.returncode}); use logs explicitly for service diagnostics")
        return result

    def docker(self, *args: str, **kwargs) -> subprocess.CompletedProcess:
        return self.run(["docker", "--context", self.context, *args], **kwargs)

    def record(self) -> dict:
        if not self.record_path.is_file():
            return {}
        result = json.loads(self.record_path.read_text())
        if result.get("env_file") != str(self.env_file):
            raise DeploymentError("This target belongs to another environment file")
        return result

    def save(self, record: dict) -> None:
        record["env_file"] = str(self.env_file)
        private_json(self.record_path, record)

    @contextlib.contextmanager
    def locked(self):
        self.state_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
        self.state_dir.chmod(0o700)
        lock = self.state_dir / "operation.lock"
        try:
            lock.mkdir()
        except FileExistsError as error:
            raise DeploymentError("Another operation holds this target's lock; inspect before removing a stale lock") from error
        try:
            (lock / "pid").write_text(str(os.getpid()))
            self.record()
            with tempfile.TemporaryDirectory(prefix="sub2api-colima-") as temporary:
                self.temporary = Path(temporary)
                yield
        finally:
            self.temporary = None
            shutil.rmtree(lock)

    def runtime(self, *, start: bool = False) -> None:
        status = self.run(["colima", "status", self.profile, "--json"], check=False)
        if status.returncode and start:
            listed = self.run(["colima", "list", "--json"])
            profiles = [json.loads(line) for line in listed.stdout.splitlines() if line.strip()]
            for profile in profiles:
                if profile.get("name") == self.profile and profile.get("runtime") != "docker":
                    raise DeploymentError("Existing Colima profile does not use Docker; choose another profile")
            print(f"Starting Colima profile {self.profile}")
            self.run(["colima", "start", self.profile, "--runtime", "docker", "--activate=false",
                      "--cpus", self.values.get("COLIMA_CPUS", "4"),
                      "--memory", self.values.get("COLIMA_MEMORY", "8"),
                      "--disk", self.values.get("COLIMA_DISK", "60"),
                      *(["--dns", ",".join(self.dns)] if self.dns else [])], timeout=600)
        elif status.returncode:
            raise DeploymentError(f"Colima profile {self.profile} is stopped; run up explicitly")
        endpoint = self.docker("context", "inspect", self.context, "--format", "{{.Endpoints.docker.Host}}").stdout.strip()
        colima_home = Path(os.environ.get("COLIMA_HOME", str(Path.home() / ".colima"))).expanduser()
        expected = f"unix://{colima_home / self.profile / 'docker.sock'}"
        if endpoint != expected:
            raise DeploymentError("Docker context does not point to the selected local Colima socket")
        self.docker("info", "--format", "{{.ServerVersion}}")
        version = self.docker("compose", "version", "--short").stdout.strip().lstrip("v")
        parts = re.match(r"(\d+)\.(\d+)\.(\d+)", version)
        if not parts or tuple(map(int, parts.groups())) < (2, 30, 0):
            raise DeploymentError("Docker Compose 2.30.0 or newer is required")

    def compose(self, *args: str, image: str | None = None, **kwargs) -> subprocess.CompletedProcess:
        selection = image or self.record().get("current") or self.values.get("COLIMA_SUB2API_IMAGE", "ghcr.io/luckykuang/sub2api-plus:latest")
        self.command_env["COLIMA_SUB2API_IMAGE"] = selection
        files = ["-f", str(ROOT / "deploy/docker-compose.yml"), "-f", str(ROOT / "deploy/docker-compose.colima.yml")]
        if self.temporary is None:
            raise DeploymentError("Compose commands require the target lock")
        raw_env = self.temporary / "application-env.json"
        private_json(raw_env, {"services": {"sub2api": {"env_file": [{"path": str(self.env_file), "format": "raw"}]}}})
        files += ["-f", str(raw_env)]
        if self.minio:
            if self.temporary is None:
                raise DeploymentError("MinIO commands require the target lock")
            override = self.temporary / "minio-app.json"
            host = self.values.get("MINIO_BIND_HOST", "127.0.0.1")
            public_host = "127.0.0.1" if host == "0.0.0.0" else host
            private_json(override, {"services": {"sub2api": {"environment": {
                "IMAGE_STORAGE_ENABLED": "true", "IMAGE_STORAGE_ENDPOINT": "http://minio:9000",
                "IMAGE_STORAGE_REGION": self.values.get("MINIO_REGION", "us-east-1"),
                "IMAGE_STORAGE_BUCKET": self.values.get("MINIO_BUCKET", "sub2api-images"),
                "IMAGE_STORAGE_ACCESS_KEY_ID": self.values["MINIO_ROOT_USER"],
                "IMAGE_STORAGE_SECRET_ACCESS_KEY": self.values["MINIO_ROOT_PASSWORD"],
                "IMAGE_STORAGE_FORCE_PATH_STYLE": "true",
                "IMAGE_STORAGE_PUBLIC_BASE_URL": f"http://{public_host}:{self.values.get('MINIO_API_PORT', '19000')}/{self.values.get('MINIO_BUCKET', 'sub2api-images')}",
            }}}})
            files += ["-f", str(override)]
        return self.docker("compose", "--project-name", self.project, "--project-directory", str(ROOT / "deploy"),
                           "--env-file", str(self.env_file), *files, *args, **kwargs)

    def inspect(self, kind: str, name: str) -> dict | None:
        result = self.docker(kind, "inspect", name, check=False)
        if result.returncode:
            return None
        return json.loads(result.stdout)[0]

    def ownership(self) -> None:
        resources = [("container", f"{self.project}-{name}") for name in ("app", "postgres", "redis", "minio")]
        resources += [("volume", f"{self.project}_{name}") for name in ("sub2api_data", "postgres_data", "redis_data", "minio_data")]
        resources += [("network", f"{self.project}_sub2api-network")]
        for kind, name in resources:
            resource = self.inspect(kind, name)
            if resource is None:
                continue
            labels = resource.get("Config", {}).get("Labels", {}) if kind == "container" else resource.get("Labels", {})
            if labels.get(LABEL) != "colima" or labels.get("com.docker.compose.project") != self.project:
                raise DeploymentError(f"Refusing to adopt unowned {kind}: {name}")

    def image_id(self, reference: str) -> str:
        return self.docker("image", "inspect", reference, "--format", "{{.Id}}").stdout.strip()

    def prepare(self, *, build: bool, pull: bool) -> str:
        record = self.record()
        if record.get("recovery_needed"):
            raise DeploymentError("A previous replacement failed; preserve its record and resolve recovery before another deployment")
        source = self.values.get("COLIMA_SUB2API_IMAGE", "ghcr.io/luckykuang/sub2api-plus:latest")
        record["candidate_source"] = {"image": source}
        created_reference = None
        if build:
            print("Building application candidate in Colima Docker (running Web is preserved)")
            version = (ROOT / "backend/cmd/server/VERSION").read_text().strip()
            commit = self.run(["git", "rev-parse", "HEAD"]).stdout.strip()
            dirty = bool(self.run(["git", "status", "--porcelain"]).stdout.strip())
            self.docker("build", "--tag", self.candidate, "--file", str(ROOT / "Dockerfile"),
                        "--build-arg", f"VERSION={version}", "--build-arg", f"COMMIT={commit}", str(ROOT), timeout=3600)
            source = self.candidate
            created_reference = self.candidate
            record["candidate_source"] = {"commit": commit, "dirty": dirty, "version": version}
        elif pull:
            print("Pulling configured application candidate (running Web is preserved)")
            self.docker("pull", source, timeout=900)
            self.docker("image", "tag", source, self.candidate)
            created_reference = source
        elif record.get("current"):
            source = record["current"]
            record["candidate_source"] = record.get("source", {"image": source})
        elif self.docker("image", "inspect", source, check=False).returncode:
            print("Pulling initial application image")
            self.docker("pull", source, timeout=900)
            created_reference = source
        candidate_id = self.image_id(source)
        record["candidate"] = candidate_id
        record["owned_images"] = sorted(set(record.get("owned_images", []) + [candidate_id]))
        if created_reference:
            record.setdefault("owned_references", {})[created_reference] = candidate_id
        self.save(record)
        return candidate_id

    def port_preflight(self) -> None:
        checks = [("app", self.host, self.port)]
        if self.minio:
            checks += [("minio", self.values.get("MINIO_BIND_HOST", "127.0.0.1"), self.values.get(key, default))
                       for key, default in (("MINIO_API_PORT", "19000"), ("MINIO_CONSOLE_PORT", "19001"))]
        for service, host, port in checks:
            existing = self.inspect("container", f"{self.project}-{service}")
            binding_key = "8080/tcp" if service == "app" else ("9000/tcp" if port == self.values.get("MINIO_API_PORT", "19000") else "9001/tcp")
            if existing and existing.get("State", {}).get("Running") and any(item.get("HostIp") == host and item.get("HostPort") == port
                                for item in existing.get("HostConfig", {}).get("PortBindings", {}).get(binding_key, []) or []):
                continue
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
                # A stopped published service may leave connections in TIME_WAIT.
                # Allow that normal restart case while still rejecting listeners.
                probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
                try:
                    probe.bind((host, int(port)))
                except OSError as error:
                    raise DeploymentError(f"Published port {port} is occupied") from error

    def health(self) -> None:
        deadline = time.monotonic() + 180
        host = "127.0.0.1" if self.host == "0.0.0.0" else self.host
        while time.monotonic() < deadline:
            app = self.inspect("container", f"{self.project}-app")
            internal = bool(app and app.get("State", {}).get("Running")) and not self.compose(
                "exec", "-T", "sub2api", "wget", "-q", "-T", "5", "-O", "/dev/null", "http://localhost:8080/health",
                check=False, timeout=10).returncode
            external = not self.run(["curl", "--fail", "--silent", "--output", "/dev/null", "--max-time", "5",
                                     f"http://{host}:{self.port}/health"], check=False, timeout=10).returncode
            if internal and external:
                print(f"Health: internal=healthy host-port=healthy http://{host}:{self.port}")
                return
            time.sleep(min(2, max(0, deadline - time.monotonic())))
        raise DeploymentError("Web readiness failed (internal and published-port checks required)")

    def dependencies(self) -> None:
        print("Starting dependencies without recreating existing containers")
        names = ["postgres", "redis"] + (["minio"] if self.minio else [])
        # --no-recreate preserves existing healthy dependencies and their configuration.
        self.compose("up", "--detach", "--no-recreate", "--wait", "--wait-timeout", "180", *names, timeout=240)
        if self.minio:
            self.compose("exec", "-T", "minio", "sh", "-ec",
                         'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; '
                         'mc mb --ignore-existing "local/$1" >/dev/null; mc anonymous set download "local/$1" >/dev/null',
                         "sh", self.values.get("MINIO_BUCKET", "sub2api-images"))

    def status(self) -> None:
        self.compose("ps", visible=True)
        for service in ["postgres", "redis"] + (["minio"] if self.minio else []):
            container = self.inspect("container", f"{self.project}-{service}") or {}
            state = container.get("State", {})
            if not state.get("Running") or state.get("Health", {}).get("Status") != "healthy":
                raise DeploymentError(f"Dependency is not healthy: {service}")
        self.health()

    def replace(self, image: str) -> None:
        print(f"Replacing Web with immutable image {image}")
        self.compose("stop", "--timeout", "30", "sub2api")
        self.compose("rm", "--force", "sub2api")
        self.compose("up", "--detach", "--no-deps", "--pull", "never", "sub2api", image=image)
        self.health()

    def cleanup_images(self, record: dict) -> None:
        keep = {record.get("current_id")}
        if self.values.get("COLIMA_RETAIN_PREVIOUS", "false") == "true":
            keep.add(record.get("previous_id"))
        containers = self.docker("container", "ls", "--all", "--quiet").stdout.split()
        used = {(self.inspect("container", identifier) or {}).get("Image") for identifier in containers}
        retained = []
        incomplete = False
        for identifier in record.get("owned_images", []):
            if identifier in keep or identifier in used:
                retained.append(identifier)
                continue
            details = self.inspect("image", identifier)
            if details is None:
                continue
            tags = set(details.get("RepoTags") or [])
            for reference, owned_id in list(record.get("owned_references", {}).items()):
                if owned_id != identifier:
                    continue
                if reference in tags and self.image_id(reference) == identifier:
                    self.docker("image", "rm", reference)
                    tags.remove(reference)
                record["owned_references"].pop(reference)
            if tags:
                retained.append(identifier)
                print(f"Cleanup: retained image with unowned references {identifier}")
            elif self.inspect("image", identifier) and self.docker("image", "rm", identifier, check=False).returncode:
                retained.append(identifier)
                incomplete = True
                print(f"Cleanup incomplete: unable to remove owned image {identifier}")
        record["owned_images"] = retained
        self.save(record)
        if incomplete:
            raise DeploymentError("Application is healthy, but owned image cleanup is incomplete")

    def deploy(self, *, build: bool = False, pull: bool = False) -> None:
        self.runtime(start=True)
        self.ownership()
        self.port_preflight()
        self.compose("config", "--quiet")
        previous = self.inspect("container", f"{self.project}-app")
        old_id = previous.get("Image") if previous else None
        candidate_id = self.prepare(build=build, pull=pull)
        self.dependencies()
        record = self.record()
        if old_id and old_id != candidate_id:
            record["previous_id"] = old_id
        record["recovery_needed"] = True
        if old_id and old_id != candidate_id:
            self.docker("image", "tag", old_id, self.recovery)
        self.save(record)
        try:
            self.replace(candidate_id)
        except DeploymentError:
            if old_id:
                print("Replacement failed; attempting one recovery with the previous image")
                try:
                    self.replace(old_id)
                    record["recovery_needed"] = False
                    record["current"] = old_id
                    record["current_id"] = old_id
                    self.save(record)
                    print("Recovery: previous application is healthy; deployment still failed")
                except DeploymentError:
                    print("Recovery failed; images and recovery record retained")
            else:
                print("First deployment failed; persistent data and candidate retained")
                record["recovery_needed"] = False
                self.save(record)
            raise
        self.docker("image", "tag", candidate_id, self.current)
        record.update(current=self.current, current_id=candidate_id, recovery_needed=False,
                      source=record.pop("candidate_source", {"image": self.values.get("COLIMA_SUB2API_IMAGE", "ghcr.io/luckykuang/sub2api-plus:latest")}))
        record["owned_images"] = sorted(set(record.get("owned_images", []) + [candidate_id]))
        self.save(record)
        # Remove only our temporary aliases; other tags are never force-removed.
        for alias in (self.candidate, self.recovery):
            if alias == self.recovery and self.values.get("COLIMA_RETAIN_PREVIOUS", "false") == "true":
                continue
            if self.docker("image", "inspect", alias, check=False).returncode == 0:
                self.docker("image", "rm", alias)
        self.cleanup_images(record)
        print(f"Deployed image={candidate_id}; Web containers=1; source={json.dumps(record['source'])}")


def initialize(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    text = (ROOT / "deploy/.env.example").read_text()
    replacements = {"BIND_HOST": "127.0.0.1", "SERVER_PORT": "18080", "MINIO_API_PORT": "19000", "MINIO_CONSOLE_PORT": "19001",
                    "POSTGRES_PASSWORD": secrets.token_hex(32), "REDIS_PASSWORD": secrets.token_hex(32),
                    "JWT_SECRET": secrets.token_hex(32), "TOTP_ENCRYPTION_KEY": secrets.token_hex(32), "MINIO_ROOT_PASSWORD": secrets.token_hex(32)}
    for key, value in replacements.items():
        text = re.sub(rf"^{key}=.*$", f"{key}={value}", text, flags=re.MULTILINE)
    # The shared example repeats some legacy keys; emit one final definition.
    seen = set()
    lines = []
    for line in reversed(text.splitlines()):
        key = line.partition("=")[0]
        if re.fullmatch(r"[A-Z][A-Z0-9_]*", key) and "=" in line:
            if key in seen:
                continue
            seen.add(key)
        lines.append(line)
    text = "\n".join(reversed(lines)) + "\n"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "w") as handle:
        handle.write(text)
    print(f"Created {path}; review configuration before up")


def main() -> int:
    parser = argparse.ArgumentParser(description="Deploy Sub2API Plus in an explicitly targeted macOS Colima Docker profile")
    parser.add_argument("command", choices=["init", "up", "build", "upgrade", "status", "logs", "restart", "down", "disk-usage"])
    parser.add_argument("service", nargs="?", choices=["app", "postgres", "redis", "minio"])
    parser.add_argument("--build", action="store_true", help="up only: build the current source before replacement")
    parser.add_argument("--follow", "-f", action="store_true", help="logs only")
    args = parser.parse_args()
    if (args.build and args.command != "up") or (args.follow and args.command != "logs") or (args.service and args.command != "logs"):
        parser.error("Options are not valid for this command")
    if sys.platform != "darwin":
        raise DeploymentError("colima.sh manages macOS deployments only")
    path = Path(os.environ.get("SUB2API_ENV_FILE", str(ROOT / "deploy/.env.colima.local"))).expanduser().absolute()
    if args.command == "init":
        initialize(path)
        return 0
    deployment = Deployment(path)
    with deployment.locked():
        if args.command in {"up", "upgrade"}:
            deployment.deploy(build=args.build, pull=args.command == "upgrade")
        elif args.command == "build":
            deployment.runtime(start=True)
            deployment.ownership()
            identifier = deployment.prepare(build=True, pull=False)
            print(f"Candidate prepared: {identifier}; running Web unchanged")
        else:
            deployment.runtime()
            deployment.ownership()
            if args.command == "status":
                deployment.status()
            elif args.command == "logs":
                if not args.service:
                    parser.error("logs requires a service")
                deployment.compose("logs", "--tail", "100", *(["--follow"] if args.follow else []),
                                   "sub2api" if args.service == "app" else args.service, visible=True, timeout=86400)
            elif args.command == "down":
                deployment.compose("stop", "--timeout", "30")
                print("Services stopped; persistent data and Colima VM retained")
            elif args.command == "restart":
                deployment.dependencies()
                deployment.compose("restart", "sub2api")
                deployment.health()
            elif args.command == "disk-usage":
                deployment.docker("system", "df", visible=True)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (DeploymentError, FileExistsError) as error:
        print(f"[ERROR] {error}", file=sys.stderr)
        raise SystemExit(1)
