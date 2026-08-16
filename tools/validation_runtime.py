"""Platform-strict validation runtime for push-cli local validation.

Host processes may probe the selected container runtime, build or inspect the
validation image, and launch an in-container command. Check matrices must not
run on the host.
"""

from __future__ import annotations

import hashlib
import json
import os
import platform
import re
import shlex
import shutil
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Sequence


IN_VALIDATION_ENV = "SUB2API_IN_VALIDATION"
HOST_CHECKED_REMOTE_TAG_ENV = "SUB2API_HOST_CHECKED_REMOTE_TAG"
VALIDATION_MARKER = Path("/etc/sub2api-validation")
IMAGE_NAME = "sub2api-validation"
DOCKERFILE_RELATIVE = Path("deploy/Dockerfile.validation")
CONTAINER_HOME = "/tmp/sub2api-home"
VALIDATION_GIT_NAME = "sub2api-validation"
VALIDATION_GIT_EMAIL = "validation@sub2api.local"
NODE_VERSION = "20.19.4"
GO_VERSION_RE = re.compile(r"^go\s+(\d+\.\d+(?:\.\d+)?)\s*$", re.MULTILINE)
WSL_DEBIAN_UBUNTU_RE = re.compile(r"^(debian|ubuntu)(?:-[\w.]+)?$", re.IGNORECASE)
Capture = Callable[..., str]
OptionalCapture = Callable[..., tuple[bool, str]]
ProbeDocker = Callable[..., tuple[bool, str]]


class ValidationRuntimeError(RuntimeError):
    """A hard failure that must stop validation."""


@dataclass(frozen=True)
class Runtime:
    name: str
    prefix: tuple[str, ...] = ()
    compose_root: str | None = None
    compose_required: bool = True


def display(command: Sequence[str]) -> str:
    return shlex.join(str(item) for item in command)


def in_validation_container() -> bool:
    return (
        os.environ.get(IN_VALIDATION_ENV) == "1"
        and VALIDATION_MARKER.is_file()
    )


def host_os_forbids_in_validation(system_name: str | None = None) -> bool:
    return (system_name or platform.system()) in {"Darwin", "Windows"}


def require_in_validation(*, tool: str) -> None:
    if host_os_forbids_in_validation():
        raise ValidationRuntimeError(
            f"{tool} cannot run on the Darwin/Windows host. "
            "Host validation fallback is forbidden"
        )
    if in_validation_container():
        return
    raise ValidationRuntimeError(
        f"{tool} must run inside the platform validation container. "
        "Host-side execution is forbidden"
    )


def normalize_wsl_list_output(output: str) -> str:
    # wsl.exe -l -v commonly emits UTF-16LE. After a UTF-8 decode that keeps
    # replacement characters, leftover NULs still split tokens.
    cleaned = output.replace("\x00", "").replace("\ufeff", "")
    return cleaned.replace("\r\n", "\n").replace("\r", "\n")


def parse_wsl_distributions(output: str) -> list[tuple[str, str]]:
    distributions: list[tuple[str, str]] = []
    for line in normalize_wsl_list_output(output).splitlines():
        match = re.match(r"^\s*\*?\s*(.*?)\s+(Running|Stopped)\s+2\s*$", line)
        if match and match.group(1):
            name = re.sub(r"\s*\(Default\)\s*$", "", match.group(1).strip())
            if name:
                distributions.append((name, match.group(2)))
    return distributions


def is_debian_or_ubuntu_wsl(name: str) -> bool:
    cleaned = re.sub(r"\s*\(Default\)\s*$", "", name.strip())
    return bool(WSL_DEBIAN_UBUNTU_RE.fullmatch(cleaned))


def probe_docker(
    prefix: Sequence[str] = (),
    *,
    optional_capture: OptionalCapture,
) -> tuple[bool, str]:
    docker_command = [*prefix, "docker"]
    info_ok, info_output = optional_capture([*docker_command, "info"])
    if not info_ok:
        return False, info_output
    compose_ok, compose_output = optional_capture(
        [*docker_command, "compose", "version"]
    )
    if not compose_ok:
        return False, compose_output
    return True, compose_output.splitlines()[0] if compose_output else "Docker Compose"


def probe_windows_runtime(
    *,
    root: Path,
    capture: Capture,
    probe_docker_fn: ProbeDocker,
    which: Callable[[str], str | None] = shutil.which,
) -> Runtime:
    wsl = which("wsl.exe") or which("wsl")
    if not wsl:
        raise ValidationRuntimeError("Windows push validation requires wsl.exe")
    status = capture([wsl, "-l", "-v"])
    distributions = parse_wsl_distributions(status)
    supported = [
        (name, state)
        for name, state in distributions
        if is_debian_or_ubuntu_wsl(name)
    ]
    if not supported:
        found = ", ".join(name for name, _ in distributions) or "none"
        raise ValidationRuntimeError(
            "Windows push validation requires a WSL2 Debian or Ubuntu "
            f"distribution; found: {found}. Host Docker fallback is forbidden"
        )

    running = [name for name, state in supported if state == "Running"]
    if not running:
        names = ", ".join(name for name, _ in supported)
        raise ValidationRuntimeError(
            "WSL2 Debian/Ubuntu distributions exist but none are running: "
            f"{names}"
        )

    for distro in running:
        prefix = (wsl, "-d", distro, "--")
        docker_ok, detail = probe_docker_fn(prefix)
        if not docker_ok:
            continue
        linux_root = capture([wsl, "-d", distro, "--", "wslpath", "-a", str(root)])
        print(f"Runtime: WSL2/{distro} with Docker ({detail})")
        return Runtime("wsl2-docker", prefix, linux_root)
    raise ValidationRuntimeError(
        "a running WSL2 Debian/Ubuntu distribution was found, but Docker and "
        "Docker Compose are not usable inside it. Host Docker fallback is forbidden"
    )


def probe_macos_runtime(
    *,
    optional_capture: OptionalCapture,
    which: Callable[[str], str | None] = shutil.which,
) -> Runtime:
    if not which("container"):
        raise ValidationRuntimeError(
            "macOS push validation requires Apple Containers. Install the "
            "container CLI and ensure `container --version` and `container ls` "
            "succeed. Colima/Docker Desktop fallback is forbidden"
        )

    version_ok, version = optional_capture(["container", "--version"])
    if not version_ok:
        detail = version[-500:] if version else "container --version failed"
        raise ValidationRuntimeError(
            "Apple Containers is the mandatory macOS runtime, but its CLI is "
            "not usable; Colima/Docker fallback is forbidden: "
            f"{detail}"
        )
    list_ok, list_output = optional_capture(["container", "ls"])
    if not list_ok:
        detail = list_output[-500:] if list_output else "container ls failed"
        raise ValidationRuntimeError(
            "Apple Containers is the mandatory macOS runtime, but its service "
            "is not ready; start or repair Apple Containers and retry. "
            "Colima/Docker fallback is forbidden: "
            f"{detail}"
        )
    version_label = version.splitlines()[0] if version else "version available"
    print(f"Runtime: Apple Containers ({version_label})")
    return Runtime("apple-containers", compose_required=False)


def probe_runtime(
    *,
    root: Path,
    capture: Capture,
    optional_capture: OptionalCapture,
    probe_docker_fn: ProbeDocker,
    which: Callable[[str], str | None] = shutil.which,
    system_name: str | None = None,
) -> Runtime:
    system = system_name or platform.system()
    if system == "Windows":
        return probe_windows_runtime(
            root=root,
            capture=capture,
            probe_docker_fn=probe_docker_fn,
            which=which,
        )
    if system == "Darwin":
        return probe_macos_runtime(optional_capture=optional_capture, which=which)

    docker_ok, docker_detail = probe_docker_fn()
    if docker_ok:
        print(f"Runtime: Docker ({docker_detail})")
        return Runtime("docker")
    if system == "Linux":
        raise ValidationRuntimeError(
            "Linux validation requires a running Docker daemon. "
            "Host validation fallback is forbidden"
        )
    raise ValidationRuntimeError(f"unsupported host platform: {system}")


def validation_image_digest(root: Path) -> str:
    dockerfile = root / DOCKERFILE_RELATIVE
    if not dockerfile.is_file():
        raise ValidationRuntimeError(f"missing validation Dockerfile: {dockerfile}")
    hasher = hashlib.sha256()
    for relative in (
        DOCKERFILE_RELATIVE,
        Path("backend/go.mod"),
        Path("frontend/package.json"),
        Path(".tool-versions"),
    ):
        path = root / relative
        if not path.is_file():
            raise ValidationRuntimeError(f"missing validation pin file: {path}")
        hasher.update(relative.as_posix().encode("utf-8"))
        hasher.update(b"\0")
        hasher.update(path.read_bytes())
        hasher.update(b"\0")
    return hasher.hexdigest()[:16]


def validation_image_ref(root: Path) -> str:
    return f"{IMAGE_NAME}:{validation_image_digest(root)}"


def declared_tool_version(root: Path, name: str) -> str:
    for line in (root / ".tool-versions").read_text(encoding="utf-8").splitlines():
        fields = line.split()
        if len(fields) == 2 and fields[0] == name:
            return fields[1].removeprefix("v")
    raise ValidationRuntimeError(f".tool-versions does not declare {name}")


def declared_validation_pins(root: Path) -> dict[str, str]:
    go_match = GO_VERSION_RE.search(
        (root / "backend/go.mod").read_text(encoding="utf-8")
    )
    if not go_match:
        raise ValidationRuntimeError("backend/go.mod does not declare a Go version")
    package = json.loads((root / "frontend/package.json").read_text(encoding="utf-8"))
    pnpm_match = re.fullmatch(r"pnpm@(.+)", package.get("packageManager", ""))
    if not pnpm_match:
        raise ValidationRuntimeError(
            "frontend/package.json must declare packageManager as pnpm@VERSION"
        )
    return {
        "GO_VERSION": go_match.group(1),
        "NODE_VERSION": NODE_VERSION,
        "PNPM_VERSION": pnpm_match.group(1),
        "GOLANGCI_LINT_VERSION": declared_tool_version(root, "golangci-lint"),
        "GORELEASER_VERSION": declared_tool_version(root, "goreleaser"),
    }


def engine_command(runtime: Runtime) -> list[str]:
    if runtime.name == "apple-containers":
        return ["container"]
    return [*runtime.prefix, "docker"]


def mount_root(runtime: Runtime, root: Path) -> str:
    return runtime.compose_root or str(root)


def container_path(host_path: Path, runtime: Runtime, root: Path) -> str:
    resolved = host_path.resolve()
    try:
        relative = resolved.relative_to(root.resolve())
    except ValueError as error:
        raise ValidationRuntimeError(
            f"path is outside the repository and cannot be mounted: {resolved}"
        ) from error
    return f"{mount_root(runtime, root).rstrip('/')}/{relative.as_posix()}"


def runtime_user(runtime: Runtime, *, capture: Capture) -> str | None:
    if runtime.name == "wsl2-docker":
        uid = capture([*runtime.prefix, "id", "-u"])
        gid = capture([*runtime.prefix, "id", "-g"])
        return f"{uid}:{gid}"
    if hasattr(os, "getuid") and hasattr(os, "getgid"):
        return f"{os.getuid()}:{os.getgid()}"
    return None


def cache_mounts(
    runtime: Runtime,
    *,
    capture: Capture,
) -> list[tuple[str, str]]:
    if runtime.name == "wsl2-docker":
        base = "/tmp/sub2api-validation-cache"
        capture(
            [
                *runtime.prefix,
                "mkdir",
                "-p",
                f"{base}/go/golangci-lint",
                f"{base}/go/xdg-cache",
                f"{base}/pnpm",
                f"{base}/frontend-node-modules",
            ]
        )
        return [
            (f"{base}/go", f"{CONTAINER_HOME}/go"),
            (f"{base}/pnpm", f"{CONTAINER_HOME}/pnpm"),
        ]
    base = Path.home() / ".cache" / "sub2api-validation"
    (base / "go").mkdir(parents=True, exist_ok=True)
    (base / "pnpm").mkdir(parents=True, exist_ok=True)
    (base / "go" / "golangci-lint").mkdir(parents=True, exist_ok=True)
    (base / "go" / "xdg-cache").mkdir(parents=True, exist_ok=True)
    (base / "frontend-node-modules").mkdir(parents=True, exist_ok=True)
    return [
        (str(base / "go"), f"{CONTAINER_HOME}/go"),
        (str(base / "pnpm"), f"{CONTAINER_HOME}/pnpm"),
    ]


def node_modules_overlay(runtime: Runtime, root: Path) -> tuple[str, str]:
    repo = mount_root(runtime, root)
    if runtime.name == "wsl2-docker":
        source = "/tmp/sub2api-validation-cache/frontend-node-modules"
    else:
        source = str(Path.home() / ".cache" / "sub2api-validation" / "frontend-node-modules")
    return source, f"{repo.rstrip('/')}/frontend/node_modules"


def image_inspect_command(runtime: Runtime, image: str) -> list[str]:
    engine = engine_command(runtime)
    if runtime.name == "apple-containers":
        return [*engine, "image", "inspect", image]
    return [*engine, "image", "inspect", image]


def bind_mount_args(runtime: Runtime, source: str, target: str) -> list[str]:
    if runtime.name == "apple-containers":
        return ["--mount", f"type=bind,source={source},target={target}"]
    return ["--volume", f"{source}:{target}"]


def image_build_command(runtime: Runtime, root: Path, image: str) -> list[str]:
    dockerfile = container_path(root / DOCKERFILE_RELATIVE, runtime, root)
    context = container_path(root / "deploy", runtime, root)
    command = [
        *engine_command(runtime),
        "build",
        "-t",
        image,
        "-f",
        dockerfile,
    ]
    for key, value in declared_validation_pins(root).items():
        command.extend(["--build-arg", f"{key}={value}"])
    command.append(context)
    return command


def validation_run_command(
    runtime: Runtime,
    argv: Sequence[str],
    *,
    root: Path,
    image: str,
    user: str | None,
    caches: Sequence[tuple[str, str]],
) -> list[str]:
    repo = mount_root(runtime, root)
    command = [
        *engine_command(runtime),
        "run",
        "--rm",
        "--cpus",
        "4",
        "--memory",
        "8G",
        *bind_mount_args(runtime, repo, repo),
        *bind_mount_args(runtime, *node_modules_overlay(runtime, root)),
        "--workdir",
        repo,
        "--env",
        f"{IN_VALIDATION_ENV}=1",
        "--env",
        f"HOME={CONTAINER_HOME}",
        "--env",
        f"GOPATH={CONTAINER_HOME}/go",
        "--env",
        f"GOCACHE={CONTAINER_HOME}/go/cache",
        "--env",
        f"GOMODCACHE={CONTAINER_HOME}/go/pkg/mod",
        "--env",
        f"PNPM_HOME={CONTAINER_HOME}/pnpm",
        "--env",
        f"GOLANGCI_LINT_CACHE={CONTAINER_HOME}/go/golangci-lint",
        "--env",
        "COREPACK_ENABLE_NETWORK=0",
        "--env",
        "COREPACK_ENABLE_DOWNLOAD_PROMPT=0",
        "--env",
        "GOMAXPROCS=4",
        "--env",
        "GOFLAGS=-p=2",
        "--env",
        "GIT_CONFIG_COUNT=3",
        "--env",
        "GIT_CONFIG_KEY_0=safe.directory",
        "--env",
        "GIT_CONFIG_VALUE_0=*",
        "--env",
        "GIT_CONFIG_KEY_1=commit.gpgsign",
        "--env",
        "GIT_CONFIG_VALUE_1=false",
        "--env",
        "GIT_CONFIG_KEY_2=tag.gpgsign",
        "--env",
        "GIT_CONFIG_VALUE_2=false",
        "--env",
        f"GIT_AUTHOR_NAME={VALIDATION_GIT_NAME}",
        "--env",
        f"GIT_AUTHOR_EMAIL={VALIDATION_GIT_EMAIL}",
        "--env",
        f"GIT_COMMITTER_NAME={VALIDATION_GIT_NAME}",
        "--env",
        f"GIT_COMMITTER_EMAIL={VALIDATION_GIT_EMAIL}",
        "--env",
        f"{HOST_CHECKED_REMOTE_TAG_ENV}=1",
    ]
    if user:
        command.extend(["--user", user])
    for source, destination in caches:
        command.extend(bind_mount_args(runtime, source, destination))
    command.append(image)
    command.extend(str(item) for item in argv)
    return command


def ensure_validation_image(
    runtime: Runtime,
    *,
    root: Path,
    optional_capture: OptionalCapture,
    run_step: Callable[[str, Sequence[str]], None],
    allow_build: bool = True,
) -> str:
    if in_validation_container():
        raise ValidationRuntimeError(
            "refusing to build or launch a nested validation container"
        )
    image = validation_image_ref(root)
    inspect = image_inspect_command(runtime, image)
    exists, _ = optional_capture(inspect)
    if exists:
        print(f"Validation image: {image}")
        return image
    if not allow_build:
        raise ValidationRuntimeError(
            f"validation image {image} is missing and rebuild was disabled. "
            "Host validation fallback is forbidden"
        )
    run_step("Build validation image", image_build_command(runtime, root, image))
    exists, detail = optional_capture(inspect)
    if not exists:
        raise ValidationRuntimeError(
            f"validation image build finished, but {image} is still missing: "
            f"{detail[-500:] if detail else 'inspect failed'}. "
            "Host validation fallback is forbidden"
        )
    print(f"Validation image: {image}")
    return image


def launch_in_validation(
    runtime: Runtime,
    argv: Sequence[str],
    *,
    root: Path,
    capture: Capture,
    run_step: Callable[[str, Sequence[str]], None],
) -> None:
    if in_validation_container():
        raise ValidationRuntimeError(
            "refusing to launch a nested validation container"
        )
    image = validation_image_ref(root)
    command = validation_run_command(
        runtime,
        argv,
        root=root,
        image=image,
        user=runtime_user(runtime, capture=capture),
        caches=cache_mounts(runtime, capture=capture),
    )
    run_step("In-container validation", command)
