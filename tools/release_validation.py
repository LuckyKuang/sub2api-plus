"""Run local release metadata and tree checks in the platform container."""

from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
from contextlib import ExitStack
from pathlib import Path
from typing import Sequence

import validation_runtime


CHECK_SCRIPTS = frozenset({"tools/check_release.py", "tools/release_finalization.py"})


def execute(command: Sequence[str], *, root: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [str(item) for item in command],
        cwd=root,
        check=False,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )


def run(command: Sequence[str], *, root: Path) -> subprocess.CompletedProcess[str]:
    """Preserve check exit status; runtime failures never execute a host check."""
    command = [str(item) for item in command]
    output: list[str] = []

    def optional_capture(argv: Sequence[str]) -> tuple[bool, str]:
        result = execute(argv, root=root)
        return result.returncode == 0, result.stdout or ""

    def capture(argv: Sequence[str]) -> str:
        result = execute(argv, root=root)
        if result.returncode:
            raise subprocess.CalledProcessError(result.returncode, argv, result.stdout)
        return (result.stdout or "").strip()

    def run_step(name: str, argv: Sequence[str]) -> None:
        result = execute(argv, root=root)
        output.append(f"[{name}]\n{result.stdout or ''}")
        if result.returncode:
            raise subprocess.CalledProcessError(result.returncode, argv)

    try:
        if len(command) < 2 or command[1] not in CHECK_SCRIPTS:
            raise ValueError("only release metadata and finalization checks are supported")
        if validation_runtime.in_validation_container():
            validation_runtime.require_in_validation(tool="release validation")
            return execute(command, root=root)
        if os.environ.get(validation_runtime.IN_VALIDATION_ENV) == "1":
            raise validation_runtime.ValidationRuntimeError(
                "release validation requires the platform container marker; "
                "an environment flag alone cannot authorize host checks"
            )

        runtime = validation_runtime.probe_runtime(
            root=root,
            capture=capture,
            optional_capture=optional_capture,
            probe_docker_fn=lambda prefix=(): validation_runtime.probe_docker(
                prefix, optional_capture=optional_capture
            ),
        )
        validation_runtime.ensure_validation_image(
            runtime, root=root, optional_capture=optional_capture, run_step=run_step
        )
        with ExitStack() as resources:
            argv = [
                "python3",
                validation_runtime.container_path(root / command[1], runtime, root),
                *command[2:],
            ]
            if "--notes-file" in argv:
                index = argv.index("--notes-file") + 1
                notes = Path(argv[index]).expanduser()
                notes = (root / notes).resolve() if not notes.is_absolute() else notes.resolve()
                if not notes.is_relative_to(root.resolve()):
                    # Mount only the requested notes, not the user's credentials
                    # directory or the rest of an external parent directory.
                    staging_root = root / "temp"
                    if not staging_root.exists():
                        staging_root.mkdir()
                        resources.callback(staging_root.rmdir)
                    staging = Path(resources.enter_context(tempfile.TemporaryDirectory(
                        prefix="release-validation-", dir=staging_root
                    )))
                    staged_notes = staging / "release-notes.md"
                    shutil.copyfile(notes, staged_notes)
                    notes = staged_notes
                argv[index] = validation_runtime.container_path(notes, runtime, root)
            validation_runtime.launch_in_validation(
                runtime, argv, root=root, capture=capture, run_step=run_step
            )
    except subprocess.CalledProcessError as error:
        if error.output:
            output.append(str(error.output))
        return subprocess.CompletedProcess(command, error.returncode, "\n".join(output))
    except (OSError, ValueError, validation_runtime.ValidationRuntimeError) as error:
        output.append(f"Release container validation failed: {error}")
        return subprocess.CompletedProcess(command, 1, "\n".join(output))
    return subprocess.CompletedProcess(command, 0, "\n".join(output))
