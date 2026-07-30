"""Resolve and synchronize release versions embedded in documentation."""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path


TAG_TEXT = r"v\d+\.\d+\.\d+\+custom\.\d{3}"
APPLICATION_VERSION_TEXT = r"\d+\.\d+\.\d+\+custom\.\d{3}"
OCI_TAG_TEXT = r"v\d+\.\d+\.\d+-custom\.\d{3}"
TAG_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)\+custom\.(\d{3})$")
UPSTREAM_ROW_RE = re.compile(
    rf"^\|\s*`({TAG_TEXT})`\s*\|.*\|\s*([a-z]+)\s*\|$",
    re.MULTILINE,
)
BASELINE_STATUSES = frozenset({"published", "historical"})
DOCUMENT_ROLLBACK_STATUSES = frozenset({"published"})

INSTALL_COMMAND_RE = re.compile(rf"(install --version ')({TAG_TEXT})(')")
ROLLBACK_COMMAND_RE = re.compile(rf"(rollback ')({TAG_TEXT})(')")
GIT_TAG_MAPPING_RE = re.compile(
    rf"^(Git/GitHub:\s+)({TAG_TEXT})(\s*)$",
    re.MULTILINE,
)
APPLICATION_MAPPING_RE = re.compile(
    rf"^(Application:\s+)({APPLICATION_VERSION_TEXT})(\s*)$",
    re.MULTILINE,
)
GHCR_IMAGE_RE = re.compile(
    rf"(ghcr\.io/luckykuang/sub2api-plus:)({OCI_TAG_TEXT})()"
)
OCI_EXAMPLE_RE = re.compile(
    rf"(for example `)({OCI_TAG_TEXT})(`)"
)
DOCKER_BUILD_VERSION_RE = re.compile(
    rf"(--build-arg VERSION=)({APPLICATION_VERSION_TEXT})(\s+\\)$",
    re.MULTILINE,
)

GIT_TAG_VALUE = "git-tag"
APPLICATION_VERSION_VALUE = "application-version"
OCI_TAG_VALUE = "oci-tag"


@dataclass(frozen=True)
class CurrentValueRule:
    pattern: re.Pattern[str]
    expected_count: int
    value_type: str
    label: str


@dataclass(frozen=True)
class DocumentRule:
    path: str
    install_commands: int
    rollback_commands: int
    current_values: tuple[CurrentValueRule, ...] = ()


DOCUMENT_RULES = (
    DocumentRule("README.md", 1, 1),
    DocumentRule("README_CN.md", 1, 1),
    DocumentRule("README_JA.md", 1, 1),
    DocumentRule(
        "deploy/README.md",
        2,
        2,
        (
            CurrentValueRule(
                GIT_TAG_MAPPING_RE, 1, GIT_TAG_VALUE, "Git/GitHub release mapping"
            ),
            CurrentValueRule(GHCR_IMAGE_RE, 1, OCI_TAG_VALUE, "GHCR image mapping"),
        ),
    ),
    DocumentRule(
        "deploy/DOCKER.md",
        0,
        0,
        (
            CurrentValueRule(
                GIT_TAG_MAPPING_RE, 1, GIT_TAG_VALUE, "Git/GitHub release mapping"
            ),
            CurrentValueRule(GHCR_IMAGE_RE, 1, OCI_TAG_VALUE, "GHCR image mapping"),
            CurrentValueRule(OCI_EXAMPLE_RE, 1, OCI_TAG_VALUE, "OCI tag example"),
        ),
    ),
    DocumentRule(
        "deploy/APPLE_CONTAINER.md",
        0,
        0,
        (
            CurrentValueRule(
                GIT_TAG_MAPPING_RE, 1, GIT_TAG_VALUE, "Git/GitHub release mapping"
            ),
            CurrentValueRule(
                APPLICATION_MAPPING_RE,
                1,
                APPLICATION_VERSION_VALUE,
                "application version mapping",
            ),
            CurrentValueRule(GHCR_IMAGE_RE, 3, OCI_TAG_VALUE, "Apple/OCI image"),
            CurrentValueRule(
                DOCKER_BUILD_VERSION_RE,
                1,
                APPLICATION_VERSION_VALUE,
                "Docker build version",
            ),
        ),
    ),
    DocumentRule(
        "UPSTREAM.md",
        0,
        0,
        (
            CurrentValueRule(
                GIT_TAG_MAPPING_RE, 1, GIT_TAG_VALUE, "current Git/GitHub version"
            ),
            CurrentValueRule(
                APPLICATION_MAPPING_RE,
                1,
                APPLICATION_VERSION_VALUE,
                "current application version",
            ),
            CurrentValueRule(GHCR_IMAGE_RE, 1, OCI_TAG_VALUE, "current GHCR image"),
        ),
    ),
)


class ReleaseDocsError(ValueError):
    """Raised when release documentation cannot be updated safely."""


def version_key(tag: str) -> tuple[int, int, int, int] | None:
    match = TAG_RE.fullmatch(tag)
    return tuple(map(int, match.groups())) if match else None


def parse_upstream_statuses(text: str) -> dict[str, str]:
    return dict(UPSTREAM_ROW_RE.findall(text))


def select_previous_release_tag(
    tags: list[str],
    statuses: dict[str, str],
    target: str,
    *,
    eligible_statuses: frozenset[str] = BASELINE_STATUSES,
) -> str | None:
    target_key = version_key(target)
    if target_key is None:
        return None
    candidates: list[tuple[tuple[int, int, int, int], str]] = []
    for tag in tags:
        key = version_key(tag)
        if (
            key is not None
            and key < target_key
            and statuses.get(tag) in eligible_statuses
        ):
            candidates.append((key, tag))
    return max(candidates, default=None)[1] if candidates else None


def resolve_document_versions(root: Path) -> tuple[str, str]:
    version = root.joinpath("backend/cmd/server/VERSION").read_text(
        encoding="utf-8"
    ).strip()
    current_tag = f"v{version}"
    if version_key(current_tag) is None:
        raise ReleaseDocsError(
            f"backend/cmd/server/VERSION contains unsupported version {version!r}"
        )

    statuses = parse_upstream_statuses(
        root.joinpath("UPSTREAM.md").read_text(encoding="utf-8")
    )
    if current_tag not in statuses:
        raise ReleaseDocsError(f"UPSTREAM.md has no mapping row for {current_tag}")

    rollback_tag = select_previous_release_tag(
        list(statuses),
        statuses,
        current_tag,
        eligible_statuses=DOCUMENT_ROLLBACK_STATUSES,
    )
    if rollback_tag is None:
        raise ReleaseDocsError(
            f"UPSTREAM.md has no earlier published release for {current_tag}"
        )
    return current_tag, rollback_tag


def _replace_exact(
    text: str,
    pattern: re.Pattern[str],
    replacement: str,
    expected_count: int,
    *,
    path: str,
    label: str,
) -> str:
    updated, count = pattern.subn(
        lambda match: f"{match.group(1)}{replacement}{match.group(3)}",
        text,
    )
    if count != expected_count:
        raise ReleaseDocsError(
            f"{path} has {count} {label} occurrence(s); expected {expected_count}"
        )
    return updated


def _current_value(current_tag: str, value_type: str) -> str:
    if value_type == GIT_TAG_VALUE:
        return current_tag
    if value_type == APPLICATION_VERSION_VALUE:
        return current_tag.removeprefix("v")
    if value_type == OCI_TAG_VALUE:
        return current_tag.replace("+", "-")
    raise ReleaseDocsError(f"unsupported current-value type {value_type!r}")


def rewrite_document(
    rule: DocumentRule,
    text: str,
    current_tag: str,
    rollback_tag: str,
) -> str:
    updated = _replace_exact(
        text,
        INSTALL_COMMAND_RE,
        current_tag,
        rule.install_commands,
        path=rule.path,
        label="install command",
    )
    updated = _replace_exact(
        updated,
        ROLLBACK_COMMAND_RE,
        rollback_tag,
        rule.rollback_commands,
        path=rule.path,
        label="rollback command",
    )
    for current_value in rule.current_values:
        updated = _replace_exact(
            updated,
            current_value.pattern,
            _current_value(current_tag, current_value.value_type),
            current_value.expected_count,
            path=rule.path,
            label=current_value.label,
        )
    return updated


def generate_release_doc_updates(
    root: Path,
) -> tuple[str, str, dict[Path, str]]:
    current_tag, rollback_tag = resolve_document_versions(root)
    updates: dict[Path, str] = {}
    for rule in DOCUMENT_RULES:
        path = root / rule.path
        text = path.read_text(encoding="utf-8")
        updates[path] = rewrite_document(rule, text, current_tag, rollback_tag)
    return current_tag, rollback_tag, updates


def pending_release_doc_updates(root: Path) -> tuple[str, str, list[Path]]:
    current_tag, rollback_tag, updates = generate_release_doc_updates(root)
    pending = [
        path
        for path, updated in updates.items()
        if path.read_text(encoding="utf-8") != updated
    ]
    return current_tag, rollback_tag, pending
