"""Maintained full validation commands shared by local submission and CI.

Coverage ownership: docs/CI_VALIDATION.md. CI success is not local proof.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


@dataclass(frozen=True)
class ValidationStep:
    name: str
    command: Sequence[str]
    cwd: Path
    lane: str


def full_steps(
    root: Path, *, python: str, frontend_workers: int = 2, migration_base: str | None = None,
) -> list[ValidationStep]:
    backend = root / "backend"
    steps = [
        ValidationStep(
            "Apple Container lifecycle test",
            ["bash", str(root / "deploy/tests/apple-container-test.sh")],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Go module tidiness",
            ["go", "mod", "tidy", "-diff"],
            backend,
            "backend-tests",
        ),
        ValidationStep(
            "Compress CLI self-tests",
            [python, "skills/compress-cli/tests/test_compress_cli.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Push CLI self-tests",
            [python, "skills/push-cli/tests/test_push_cli.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Release CLI self-tests",
            [python, "skills/release-cli/tests/test_release_cli.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Backend unit tests",
            ["go", "test", "-tags=unit", "./..."],
            backend,
            "backend-tests",
        ),
        ValidationStep(
            "Backend integration tests",
            ["go", "test", "-tags=integration", "./..."],
            backend,
            "backend-tests",
        ),
        ValidationStep(
            "Backend lint",
            ["golangci-lint", "run", "./..."],
            backend,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Frontend frozen install",
            ["pnpm", "--dir", "frontend", "install", "--frozen-lockfile"],
            root,
            "frontend",
        ),
        ValidationStep(
            "Frontend lint",
            ["pnpm", "--dir", "frontend", "run", "lint:check"],
            root,
            "frontend",
        ),
        ValidationStep(
            "Frontend typecheck",
            ["pnpm", "--dir", "frontend", "run", "typecheck"],
            root,
            "frontend",
        ),
        ValidationStep(
            "Frontend tests",
            [
                "pnpm",
                "--dir",
                "frontend",
                "run",
                "test:run",
                f"--maxWorkers={frontend_workers}",
            ],
            root,
            "frontend",
        ),
        ValidationStep(
            "Frontend production build",
            ["pnpm", "--dir", "frontend", "run", "build"],
            root,
            "frontend",
        ),
        ValidationStep(
            "Release policy tests",
            [python, "tools/test_release_policy.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Codex outbound identity",
            [python, "tools/check_openai_codex_identity.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "README synchronization",
            [python, "tools/check_readme_sync.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Go test build tags",
            [python, "tools/check_test_build_tags.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Release metadata sources",
            [python, "tools/check_release.py"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Linux installer syntax",
            ["bash", "-n", "deploy/install.sh"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Apple installer syntax",
            ["bash", "-n", "deploy/apple-container.sh"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Docker Compose security",
            ["sh", "deploy/tests/docker-compose-security-test.sh"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Docker runtime resources",
            ["sh", "deploy/tests/docker-runtime-resources-test.sh"],
            root,
            "backend-lint-policy",
        ),
        ValidationStep(
            "Caddy cache policy",
            ["bash", "deploy/test-caddyfile-cache.sh"],
            root,
            "backend-lint-policy",
        ),
    ]
    steps.extend([
        ValidationStep("Docker Compose gateway environment",
                       ["sh", "deploy/tests/docker-compose-gateway-env-test.sh"], root,
                       "backend-lint-policy"),
        ValidationStep("Go test build-tag regressions",
                       [python, "tools/test_test_build_tags.py"], root, "backend-lint-policy"),
        ValidationStep("CI validation regressions",
                       [python, "tools/test_ci_validation.py"], root, "backend-lint-policy"),
        ValidationStep("Release packaging tests", [python, "-m", "unittest", "discover", "-s",
                       ".github/release-tools", "-p", "test_release_*.py"], root, "backend-lint-policy"),
        ValidationStep("Release image script syntax", ["bash", "-n", ".github/release-tools/release-images.sh"],
                       root, "backend-lint-policy"),
    ])
    if migration_base is not None:
        steps.append(ValidationStep(
            "Migration policy",
            [python, "tools/check_new_migrations.py", "--base", migration_base],
            root, "backend-lint-policy",
        ))
    return steps


# CI adds security/workflow/config coverage; it does not remove any local step.
CI_STEP_NAMES = {
    "deployment-config": (
        "Apple Container lifecycle test", "Linux installer syntax",
        "Apple installer syntax", "Docker Compose security",
        "Docker Compose gateway environment", "Docker runtime resources",
        "Caddy cache policy",
    ),
    "test-unit": ("Go module tidiness", "Backend unit tests"),
    "test-integration": ("Backend integration tests",),
    "frontend": (
        "Frontend frozen install", "Frontend lint", "Frontend typecheck",
        "Frontend tests", "Frontend production build",
    ),
    "golangci-lint": ("Backend lint",),
    "repository-policy": (
        "Compress CLI self-tests", "Push CLI self-tests", "Release CLI self-tests",
        "Release policy tests", "Codex outbound identity", "README synchronization",
        "Go test build tags", "Go test build-tag regressions",
        "CI validation regressions", "Release metadata sources", "Migration policy",
        "Release packaging tests", "Release image script syntax",
    ),
}
CI_LANES = (*CI_STEP_NAMES, "goreleaser-config", "backend-security", "frontend-security")


def ci_steps(root: Path, lane: str, *, python: str, base: str) -> list[ValidationStep]:
    steps = {step.name: step for step in full_steps(
        root, python=python, migration_base=base,
    )}
    return [steps[name] for name in CI_STEP_NAMES[lane]]
