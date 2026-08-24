#!/usr/bin/env python3
"""Promote pull requests and publish immutable Sub2API Plus releases."""

from __future__ import annotations

import argparse
import json
import re
import shlex
import shutil
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Sequence


ROOT = Path(__file__).resolve().parents[3]
DEFAULT_REMOTE = "origin"
EXPECTED_REPOSITORY = "LuckyKuang/sub2api-plus"
LOCAL_VALIDATION_CONTEXT = "sub2api/local-validation"
FULL_PROFILE = "full"
FINALIZATION_PROFILE = "release-finalization"
VALIDATION_DESCRIPTIONS = {
    FULL_PROFILE: "Platform-container validation passed",
    FINALIZATION_PROFILE: "Deterministic release finalization passed",
}
VALIDATION_MARKER_RE = re.compile(
    r"<!--\s*sub2api-submit-pr:\s*(\{.*?\})\s*-->", re.DOTALL
)
TAG_RE = re.compile(r"^v\d+\.\d+\.\d+\+custom\.(?:00[1-9]|0[1-9]\d|[1-9]\d{2})$")
EXPECTED_SUBJECT_PREFIX = "Sub2API Plus "
EXPECTED_MAIN_WORKFLOWS = frozenset({"CI", "Security Scan"})
RELEASE_ENVIRONMENT = "release"
RELEASE_TAG_POLICY = "v*+custom.*"
RELEASE_TAG_RULESET_REF = f"refs/tags/{RELEASE_TAG_POLICY}"
REQUIRED_RELEASE_TAG_RULES = frozenset({"deletion", "update"})
REQUIRED_PR_STATUS_CONTEXTS = frozenset(
    {
        LOCAL_VALIDATION_CONTEXT,
        "deployment-config",
        "test",
        "frontend",
        "golangci-lint",
        "goreleaser-config",
        "repository-policy",
        "backend-security",
        "frontend-security",
    }
)
PRICING_ASSETS = frozenset({"model-pricing.json", "model-pricing-manifest.json"})
FINALIZATION_ALLOWED_PATHS = frozenset(
    {
        "UPSTREAM.md",
        "README.md",
        "README_CN.md",
        "README_JA.md",
        "deploy/README.md",
    }
)
POLL_SECONDS = 5
DISCOVERY_ATTEMPTS = 12
MERGE_ATTEMPTS = 60
WATCH_SECONDS = 10


class ReleaseCliError(RuntimeError):
    """A release guard failed and the command must stop."""


class PromotionPending(ReleaseCliError):
    """GitHub accepted auto-merge but protected conditions remain pending."""


@dataclass(frozen=True)
class WorkflowRun:
    database_id: int
    url: str
    status: str
    conclusion: str | None


@dataclass(frozen=True)
class ValidationProof:
    base: str
    head: str
    profile: str = FULL_PROFILE
    tag: str | None = None


@dataclass(frozen=True)
class RemoteTag:
    annotated: bool
    object_oid: str
    target: str
    subject: str


@dataclass(frozen=True)
class PullRequest:
    number: int
    state: str
    is_draft: bool
    base_branch: str
    base_oid: str
    head_branch: str
    head_oid: str
    head_owner: str
    merge_state: str
    merge_commit: str | None
    auto_merge_enabled: bool
    body: str
    url: str


def display(command: Sequence[str]) -> str:
    return shlex.join(str(item) for item in command)


def run_command(
    command: Sequence[str],
    *,
    cwd: Path | None = None,
    capture: bool = False,
    timeout: int | None = None,
) -> subprocess.CompletedProcess[str]:
    actual_cwd = ROOT if cwd is None else cwd
    try:
        return subprocess.run(
            [str(item) for item in command],
            cwd=actual_cwd,
            check=False,
            text=True,
            encoding="utf-8",
            errors="replace",
            stdout=subprocess.PIPE if capture else None,
            stderr=subprocess.STDOUT if capture else None,
            timeout=timeout,
        )
    except FileNotFoundError as error:
        raise ReleaseCliError(
            f"required command is unavailable: {error.filename}"
        ) from error


def capture(command: Sequence[str], *, cwd: Path | None = None) -> str:
    result = run_command(command, cwd=cwd, capture=True)
    if result.returncode != 0:
        detail = (result.stdout or "").strip()
        raise ReleaseCliError(
            f"{display(command)} failed with exit code {result.returncode}"
            + (f": {detail[-2000:]}" if detail else "")
        )
    return (result.stdout or "").strip()


def run_step(
    name: str,
    command: Sequence[str],
    *,
    cwd: Path | None = None,
) -> None:
    print(f"\n[{name}]")
    print(f"$ {display(command)}")
    result = run_command(command, cwd=cwd)
    if result.returncode != 0:
        raise ReleaseCliError(f"{name} failed with exit code {result.returncode}")


def require_command(command: str) -> None:
    if shutil.which(command) is None:
        raise ReleaseCliError(f"required command is unavailable: {command}")


def repo_from_remote(url: str) -> str:
    match = re.search(r"github\.com[:/]([^/]+)/([^/]+?)(?:\.git)?$", url.strip())
    if not match:
        raise ReleaseCliError(f"origin is not a GitHub repository URL: {url}")
    return f"{match.group(1)}/{match.group(2)}"


def github_gate(remote: str) -> str:
    require_command("gh")
    version = capture(["gh", "--version"]).splitlines()[0]
    print(f"GitHub CLI: {version}")
    auth = run_command(
        ["gh", "auth", "status", "--hostname", "github.com"], capture=True
    )
    if auth.returncode != 0:
        detail = (auth.stdout or "").strip()
        raise ReleaseCliError(
            "GitHub CLI is missing a valid github.com login. Run gh auth login "
            "and retry."
            + (f"\n{detail[-2000:]}" if detail else "")
        )

    repository = repo_from_remote(capture(["git", "remote", "get-url", remote]))
    if repository != EXPECTED_REPOSITORY:
        raise ReleaseCliError(
            f"origin resolves to {repository}; expected {EXPECTED_REPOSITORY}"
        )
    capture(["gh", "repo", "view", repository, "--json", "nameWithOwner"])
    push_permission = capture(
        ["gh", "api", f"repos/{repository}", "--jq", ".permissions.push"]
    ).lower()
    if push_permission != "true":
        raise ReleaseCliError(
            f"authenticated GitHub account has no push permission for {repository}"
        )
    print(f"GitHub repository: {repository} (push permission confirmed)")
    return repository


def json_capture(command: Sequence[str], *, description: str) -> object:
    output = capture(command)
    try:
        return json.loads(output)
    except json.JSONDecodeError as error:
        raise ReleaseCliError(f"{description} returned invalid JSON") from error


def repository_settings(repository: str) -> dict[str, object]:
    data = json_capture(
        ["gh", "api", f"repos/{repository}"], description="GitHub repository API"
    )
    if not isinstance(data, dict):
        raise ReleaseCliError("GitHub repository API returned an unexpected value")
    return data


def repository_default_branch(repository: str) -> str:
    branch = repository_settings(repository).get("default_branch")
    if not isinstance(branch, str) or not branch:
        raise ReleaseCliError("GitHub returned an invalid repository default branch")
    return branch


def require_protected_auto_merge(repository: str, default_branch: str) -> None:
    settings = repository_settings(repository)
    if settings.get("allow_auto_merge") is not True:
        raise ReleaseCliError(
            "repository auto-merge is disabled; enable it before PR promotion"
        )
    if settings.get("allow_merge_commit") is not True:
        raise ReleaseCliError(
            "repository merge-commit mode is disabled; enable it before PR promotion"
        )
    data = json_capture(
        ["gh", "api", f"repos/{repository}/rules/branches/{default_branch}"],
        description="GitHub branch rules API",
    )
    if not isinstance(data, list):
        raise ReleaseCliError("GitHub branch rules API returned an unexpected value")
    rule_types = {
        str(rule.get("type"))
        for rule in data
        if isinstance(rule, dict) and rule.get("type")
    }
    required = {"pull_request", "required_status_checks"}
    missing = sorted(required - rule_types)
    if missing:
        raise ReleaseCliError(
            f"{default_branch} lacks required protected rules: " + ", ".join(missing)
        )

    pull_request_rules = [
        rule
        for rule in data
        if isinstance(rule, dict) and rule.get("type") == "pull_request"
    ]
    for rule in pull_request_rules:
        parameters = rule.get("parameters")
        if not isinstance(parameters, dict):
            continue
        allowed = parameters.get("allowed_merge_methods")
        if isinstance(allowed, list) and "merge" not in allowed:
            raise ReleaseCliError(
                f"{default_branch} pull-request rules do not allow merge commits"
            )

    status_rules = [
        rule
        for rule in data
        if isinstance(rule, dict) and rule.get("type") == "required_status_checks"
    ]
    strict = False
    contexts: set[str] = set()
    for rule in status_rules:
        parameters = rule.get("parameters")
        if not isinstance(parameters, dict):
            continue
        strict = strict or parameters.get("strict_required_status_checks_policy") is True
        checks = parameters.get("required_status_checks")
        if not isinstance(checks, list):
            continue
        contexts.update(
            str(check.get("context"))
            for check in checks
            if isinstance(check, dict) and check.get("context")
        )
    if not strict:
        raise ReleaseCliError(
            f"{default_branch} required status checks must require the branch "
            "to be current before merge"
        )
    missing_contexts = sorted(REQUIRED_PR_STATUS_CONTEXTS - contexts)
    if missing_contexts:
        raise ReleaseCliError(
            f"{default_branch} rules do not require all promotion checks: "
            + ", ".join(missing_contexts)
        )


def require_automated_release_environment(repository: str) -> None:
    data = json_capture(
        [
            "gh",
            "api",
            f"repos/{repository}/environments/{RELEASE_ENVIRONMENT}",
        ],
        description="GitHub release-environment API",
    )
    if not isinstance(data, dict) or data.get("name") != RELEASE_ENVIRONMENT:
        raise ReleaseCliError(
            f"GitHub environment {RELEASE_ENVIRONMENT!r} is missing or invalid"
        )
    if data.get("can_admins_bypass") is not False:
        raise ReleaseCliError(
            f"{RELEASE_ENVIRONMENT} environment must disable administrator bypass"
        )

    protection_rules = data.get("protection_rules")
    if not isinstance(protection_rules, list):
        raise ReleaseCliError(
            "GitHub release-environment API returned invalid protection rules"
        )
    rule_types = {
        str(rule.get("type"))
        for rule in protection_rules
        if isinstance(rule, dict) and rule.get("type")
    }
    blocking_rules = sorted(rule_types - {"branch_policy"})
    if blocking_rules:
        raise ReleaseCliError(
            f"{RELEASE_ENVIRONMENT} environment is not automatic; remove blocking "
            "protection rules: " + ", ".join(blocking_rules)
        )
    if "branch_policy" not in rule_types:
        raise ReleaseCliError(
            f"{RELEASE_ENVIRONMENT} environment lacks a deployment tag policy"
        )

    deployment_policy = data.get("deployment_branch_policy")
    if not isinstance(deployment_policy, dict):
        raise ReleaseCliError(
            "GitHub release-environment API returned no deployment policy"
        )
    if (
        deployment_policy.get("protected_branches") is not False
        or deployment_policy.get("custom_branch_policies") is not True
    ):
        raise ReleaseCliError(
            f"{RELEASE_ENVIRONMENT} environment must use only custom deployment "
            "tag policies"
        )

    policies = json_capture(
        [
            "gh",
            "api",
            f"repos/{repository}/environments/{RELEASE_ENVIRONMENT}/"
            "deployment-branch-policies?per_page=100",
        ],
        description="GitHub release deployment-policy API",
    )
    if not isinstance(policies, dict) or not isinstance(
        policies.get("branch_policies"), list
    ):
        raise ReleaseCliError(
            "GitHub release deployment-policy API returned an unexpected value"
        )
    branch_policies = policies["branch_policies"]
    if policies.get("total_count") != len(branch_policies):
        raise ReleaseCliError(
            "GitHub release deployment-policy API returned an incomplete page"
        )
    actual_policies = {
        (str(policy.get("type")), str(policy.get("name")))
        for policy in branch_policies
        if isinstance(policy, dict)
    }
    expected_policies = {("tag", RELEASE_TAG_POLICY)}
    if actual_policies != expected_policies:
        rendered = ", ".join(
            f"{kind}:{name}" for kind, name in sorted(actual_policies)
        )
        raise ReleaseCliError(
            f"{RELEASE_ENVIRONMENT} environment deployment policy must be exactly "
            f"tag:{RELEASE_TAG_POLICY}; found {rendered or '<none>'}"
        )


def require_immutable_release_tag_ruleset(repository: str) -> None:
    summaries = json_capture(
        ["gh", "api", f"repos/{repository}/rulesets?per_page=100"],
        description="GitHub rulesets API",
    )
    if not isinstance(summaries, list):
        raise ReleaseCliError("GitHub rulesets API returned an unexpected value")

    candidates: list[dict[str, object]] = []
    for summary in summaries:
        if (
            not isinstance(summary, dict)
            or summary.get("target") != "tag"
            or summary.get("enforcement") != "active"
            or summary.get("source_type") != "Repository"
        ):
            continue
        ruleset_id = summary.get("id")
        if not isinstance(ruleset_id, int):
            raise ReleaseCliError("GitHub returned an invalid tag ruleset ID")
        detail = json_capture(
            ["gh", "api", f"repos/{repository}/rulesets/{ruleset_id}"],
            description="GitHub tag-ruleset API",
        )
        if not isinstance(detail, dict):
            raise ReleaseCliError(
                "GitHub tag-ruleset API returned an unexpected value"
            )
        conditions = detail.get("conditions")
        ref_name = (
            conditions.get("ref_name") if isinstance(conditions, dict) else None
        )
        includes = ref_name.get("include") if isinstance(ref_name, dict) else None
        excludes = ref_name.get("exclude") if isinstance(ref_name, dict) else None
        if (
            isinstance(includes, list)
            and RELEASE_TAG_RULESET_REF in includes
            and isinstance(excludes, list)
            and not excludes
        ):
            candidates.append(detail)

    if not candidates:
        raise ReleaseCliError(
            "no active repository tag ruleset protects " + RELEASE_TAG_RULESET_REF
        )

    def rule_types(detail: dict[str, object]) -> set[str]:
        rules = detail.get("rules")
        if not isinstance(rules, list):
            raise ReleaseCliError("GitHub tag-ruleset API returned invalid rules")
        return {
            str(rule.get("type"))
            for rule in rules
            if isinstance(rule, dict) and rule.get("type")
        }

    for detail in candidates:
        if "creation" in rule_types(detail):
            raise ReleaseCliError(
                f"tag ruleset {detail.get('name')!r} blocks initial release-tag creation"
            )

    protected = []
    for detail in candidates:
        bypass_actors = detail.get("bypass_actors")
        if (
            REQUIRED_RELEASE_TAG_RULES <= rule_types(detail)
            and isinstance(bypass_actors, list)
            and not bypass_actors
        ):
            protected.append(detail)
    if not protected:
        raise ReleaseCliError(
            "custom release tags require an active no-bypass tag ruleset that "
            "blocks updates and deletion"
        )


def require_automated_release_policy(repository: str) -> None:
    require_automated_release_environment(repository)
    require_immutable_release_tag_ruleset(repository)
    print(
        f"Automated release policy: {RELEASE_ENVIRONMENT} environment with "
        f"immutable {RELEASE_TAG_POLICY} tags"
    )


def validate_tag(tag: str) -> None:
    if not TAG_RE.fullmatch(tag):
        raise ReleaseCliError(
            "tag must match vX.Y.Z+custom.NNN with NNN from 001 through 999"
        )


def require_clean_worktree(*, allow_untracked: bool = False) -> None:
    status = capture(["git", "status", "--porcelain=v1", "--untracked-files=all"])
    entries = status.splitlines() if status else []
    dirty = [entry for entry in entries if not (allow_untracked and entry.startswith("?? "))]
    if dirty:
        raise ReleaseCliError(
            "release action requires no tracked worktree changes; commit or remove:\n"
            + "\n".join(dirty)
        )
    diff_check = run_command(["git", "diff", "--check"], capture=True)
    if diff_check.returncode != 0:
        raise ReleaseCliError(
            "git diff --check failed:\n" + (diff_check.stdout or "").strip()
        )


def current_head() -> str:
    return capture(["git", "rev-parse", "HEAD"])


def current_branch() -> str:
    branch = capture(["git", "branch", "--show-current"])
    if not branch:
        raise ReleaseCliError("release finalization requires a checked-out branch")
    return branch


def setup_git_transport() -> None:
    run_step("Configure Git transport from GitHub CLI", ["gh", "auth", "setup-git"])


def fetch_default_branch(remote: str, default_branch: str) -> str:
    run_step(
        f"Fetch {remote}/{default_branch}",
        [
            "git",
            "fetch",
            "--no-tags",
            remote,
            f"+refs/heads/{default_branch}:refs/remotes/{remote}/{default_branch}",
        ],
    )
    return capture(["git", "rev-parse", f"{remote}/{default_branch}"])


def local_tag_details(tag: str, *, required: bool) -> dict[str, str] | None:
    reference = f"refs/tags/{tag}"
    exists = run_command(["git", "show-ref", "--verify", "--quiet", reference])
    if exists.returncode == 1:
        if required:
            raise ReleaseCliError(f"local tag does not exist: {tag}")
        return None
    if exists.returncode != 0:
        raise ReleaseCliError(f"unable to inspect local tag: {tag}")
    return {
        "object_type": capture(["git", "cat-file", "-t", reference]),
        "subject": capture(
            ["git", "for-each-ref", "--format=%(contents:subject)", reference]
        ),
        "target": capture(["git", "rev-list", "-n", "1", tag]),
    }


def require_publishable_local_tag(tag: str) -> str:
    details = local_tag_details(tag, required=True)
    assert details is not None
    if details["object_type"] != "tag":
        raise ReleaseCliError(f"{tag} is not an annotated tag")
    expected_subject = f"{EXPECTED_SUBJECT_PREFIX}{tag}"
    if details["subject"] != expected_subject:
        raise ReleaseCliError(
            f"{tag} subject is {details['subject']!r}; expected {expected_subject!r}"
        )
    return details["target"]


def remote_tag_exists(repository: str, tag: str) -> bool:
    return remote_tag_details(repository, tag) is not None


def remote_tag_details(repository: str, tag: str) -> RemoteTag | None:
    data = json_capture(
        ["gh", "api", f"repos/{repository}/git/matching-refs/tags/{tag}"],
        description="GitHub remote-tag API",
    )
    if not isinstance(data, list):
        raise ReleaseCliError("GitHub remote-tag API returned an unexpected value")
    matches = [
        item
        for item in data
        if isinstance(item, dict) and item.get("ref") == f"refs/tags/{tag}"
    ]
    if not matches:
        return None
    if len(matches) != 1:
        raise ReleaseCliError(f"GitHub returned duplicate exact refs for tag {tag}")
    remote_object = matches[0].get("object")
    if not isinstance(remote_object, dict):
        raise ReleaseCliError(f"GitHub returned invalid ref metadata for tag {tag}")
    object_type = remote_object.get("type")
    object_oid = remote_object.get("sha")
    if not isinstance(object_oid, str) or not re.fullmatch(r"[0-9a-f]{40}", object_oid):
        raise ReleaseCliError(f"GitHub returned an invalid object SHA for tag {tag}")
    if object_type == "commit":
        return RemoteTag(False, object_oid, object_oid, "")
    if object_type != "tag":
        raise ReleaseCliError(
            f"remote tag {tag} points to unsupported object type {object_type!r}"
        )

    tag_object = json_capture(
        ["gh", "api", f"repos/{repository}/git/tags/{object_oid}"],
        description="GitHub annotated-tag API",
    )
    if not isinstance(tag_object, dict) or tag_object.get("tag") != tag:
        raise ReleaseCliError(f"GitHub returned invalid annotated-tag metadata for {tag}")
    target_object = tag_object.get("object")
    if not isinstance(target_object, dict) or target_object.get("type") != "commit":
        raise ReleaseCliError(f"remote annotated tag {tag} does not target a commit")
    target = target_object.get("sha")
    if not isinstance(target, str) or not re.fullmatch(r"[0-9a-f]{40}", target):
        raise ReleaseCliError(f"GitHub returned an invalid target SHA for tag {tag}")
    message = tag_object.get("message")
    if not isinstance(message, str):
        raise ReleaseCliError(f"GitHub returned no annotated-tag message for {tag}")
    subject = next((line.strip() for line in message.splitlines() if line.strip()), "")
    return RemoteTag(True, object_oid, target, subject)


def require_published_remote_tag(repository: str, tag: str) -> RemoteTag:
    details = remote_tag_details(repository, tag)
    if details is None:
        raise ReleaseCliError(f"remote tag is absent: {tag}")
    if not details.annotated:
        raise ReleaseCliError(f"remote tag {tag} is not annotated")
    expected_subject = f"{EXPECTED_SUBJECT_PREFIX}{tag}"
    if details.subject != expected_subject:
        raise ReleaseCliError(
            f"remote tag {tag} subject is {details.subject!r}; "
            f"expected {expected_subject!r}"
        )
    return details


def release_details(
    repository: str,
    tag: str,
    *,
    required: bool,
) -> dict[str, object] | None:
    result = run_command(
        [
            "gh",
            "release",
            "view",
            tag,
            "--repo",
            repository,
            "--json",
            "tagName,isDraft,isPrerelease,url,assets",
        ],
        capture=True,
    )
    if result.returncode != 0:
        if required:
            detail = (result.stdout or "").strip()
            raise ReleaseCliError(
                f"GitHub Release does not exist: {tag}"
                + (f": {detail[-2000:]}" if detail else "")
            )
        return None
    try:
        data = json.loads(result.stdout or "")
    except json.JSONDecodeError as error:
        raise ReleaseCliError("gh release view returned invalid JSON") from error
    if not isinstance(data, dict):
        raise ReleaseCliError("gh release view returned an unexpected value")
    return data


def parse_validation_proof(body: str) -> ValidationProof:
    matches = VALIDATION_MARKER_RE.findall(body)
    if len(matches) != 1:
        raise ReleaseCliError(
            "pull request must contain exactly one submit-pr validation marker"
        )
    try:
        payload = json.loads(matches[0])
    except json.JSONDecodeError as error:
        raise ReleaseCliError("pull-request validation marker is invalid JSON") from error
    if not isinstance(payload, dict):
        raise ReleaseCliError("pull-request validation marker is not an object")
    base, head = payload.get("base"), payload.get("head")
    if not isinstance(base, str) or not re.fullmatch(r"[0-9a-f]{40}", base):
        raise ReleaseCliError("pull-request validation marker has an invalid base SHA")
    if not isinstance(head, str) or not re.fullmatch(r"[0-9a-f]{40}", head):
        raise ReleaseCliError("pull-request validation marker has an invalid head SHA")
    profile = payload.get("profile")
    tag = payload.get("tag")
    if profile == FULL_PROFILE:
        if set(payload) != {"base", "head", "profile"}:
            raise ReleaseCliError("full validation marker has unexpected fields")
        return ValidationProof(base=base, head=head, profile=profile)
    if profile == FINALIZATION_PROFILE:
        if set(payload) != {"base", "head", "profile", "tag"}:
            raise ReleaseCliError(
                "release-finalization validation marker has unexpected fields"
            )
        if not isinstance(tag, str) or TAG_RE.fullmatch(tag) is None:
            raise ReleaseCliError(
                "release-finalization validation marker has an invalid tag"
            )
        return ValidationProof(base=base, head=head, profile=profile, tag=tag)
    raise ReleaseCliError("pull-request validation marker has an invalid profile")


def pull_request_details(repository: str, number: int) -> PullRequest:
    data = json_capture(
        [
            "gh",
            "pr",
            "view",
            str(number),
            "--repo",
            repository,
            "--json",
            "number,state,isDraft,baseRefName,baseRefOid,headRefName,headRefOid,headRepositoryOwner,mergeStateStatus,mergeCommit,autoMergeRequest,body,url",
        ],
        description="gh pr view",
    )
    if not isinstance(data, dict):
        raise ReleaseCliError("gh pr view returned an unexpected value")
    owner = data.get("headRepositoryOwner")
    head_owner = str(owner.get("login", "")) if isinstance(owner, dict) else ""
    merge = data.get("mergeCommit")
    merge_commit = str(merge.get("oid")) if isinstance(merge, dict) and merge.get("oid") else None
    try:
        return PullRequest(
            number=int(data["number"]),
            state=str(data["state"]),
            is_draft=bool(data["isDraft"]),
            base_branch=str(data["baseRefName"]),
            base_oid=str(data["baseRefOid"]),
            head_branch=str(data["headRefName"]),
            head_oid=str(data["headRefOid"]),
            head_owner=head_owner,
            merge_state=str(data.get("mergeStateStatus") or "UNKNOWN"),
            merge_commit=merge_commit,
            auto_merge_enabled=data.get("autoMergeRequest") is not None,
            body=str(data.get("body") or ""),
            url=str(data.get("url") or number),
        )
    except (KeyError, TypeError, ValueError) as error:
        raise ReleaseCliError("gh pr view returned incomplete metadata") from error


def require_local_validation_status(
    repository: str,
    head: str,
    profile: str,
) -> None:
    data = json_capture(
        ["gh", "api", f"repos/{repository}/commits/{head}/status"],
        description="GitHub commit status API",
    )
    if not isinstance(data, dict) or not isinstance(data.get("statuses"), list):
        raise ReleaseCliError("GitHub commit status API returned an unexpected value")
    matching = [
        status
        for status in data["statuses"]
        if isinstance(status, dict) and status.get("context") == LOCAL_VALIDATION_CONTEXT
    ]
    expected_description = VALIDATION_DESCRIPTIONS[profile]
    if (
        not matching
        or matching[0].get("state") != "success"
        or matching[0].get("description") != expected_description
    ):
        raise ReleaseCliError(
            f"{head} has no successful {profile} {LOCAL_VALIDATION_CONTEXT} status"
        )


def require_promotable_pr(
    repository: str,
    pr: PullRequest,
    default_branch: str,
) -> ValidationProof:
    if pr.state != "OPEN":
        raise ReleaseCliError(f"pull request #{pr.number} is not open")
    if pr.is_draft:
        raise ReleaseCliError(f"pull request #{pr.number} is still a draft")
    if pr.base_branch != default_branch:
        raise ReleaseCliError(
            f"pull request #{pr.number} targets {pr.base_branch}, expected {default_branch}"
        )
    expected_owner = repository.split("/", 1)[0]
    if pr.head_owner.lower() != expected_owner.lower():
        raise ReleaseCliError("release pull request must come from the same repository")
    proof = parse_validation_proof(pr.body)
    if proof.head != pr.head_oid:
        raise ReleaseCliError(
            "pull-request head changed after local validation; rerun submit-pr"
        )
    if proof.base != pr.base_oid:
        raise ReleaseCliError(
            "pull-request base changed after local validation; rerun submit-pr"
        )
    require_local_validation_status(repository, pr.head_oid, proof.profile)
    return proof


def run_metadata_preflight(
    tag: str,
    notes_file: Path,
    commit: str,
    *,
    create_tag: bool,
    remote: str,
) -> None:
    command = [
        sys.executable,
        "tools/release_preflight.py",
        "--tag",
        tag,
        "--notes-file",
        str(notes_file),
        "--commit",
        commit,
        "--remote",
        remote,
    ]
    if create_tag:
        command.append("--create-tag")
    run_step("Release metadata preflight", command)


def finalization_metadata_command(tag: str) -> list[str]:
    return [
        sys.executable,
        "tools/check_release.py",
        "--tag",
        tag,
        "--require-status",
        "published",
        "--mapping-only",
    ]


def finalization_tree_command(
    proof: ValidationProof,
    branch: str,
) -> list[str]:
    if proof.tag is None:
        raise ReleaseCliError("release-finalization proof has no tag")
    return [
        sys.executable,
        "tools/release_finalization.py",
        "validate",
        "--base",
        proof.base,
        "--head",
        proof.head,
        "--tag",
        proof.tag,
        "--branch",
        branch,
    ]


def require_required_pr_checks(repository: str, number: int) -> None:
    run_step(
        "Wait for required pull-request checks",
        [
            "gh",
            "pr",
            "checks",
            str(number),
            "--repo",
            repository,
            "--required",
            "--watch",
            "--fail-fast",
        ],
    )


def find_branch_runs(
    repository: str,
    branch: str,
    sha: str,
) -> list[dict[str, object]]:
    for attempt in range(DISCOVERY_ATTEMPTS):
        data = json_capture(
            [
                "gh",
                "run",
                "list",
                "--repo",
                repository,
                "--branch",
                branch,
                "--event",
                "push",
                "--limit",
                "100",
                "--json",
                "databaseId,headSha,headBranch,status,conclusion,url,workflowName",
            ],
            description="gh run list",
        )
        if not isinstance(data, list):
            raise ReleaseCliError("gh run list returned an unexpected value")
        matches = [
            run
            for run in data
            if isinstance(run, dict)
            and run.get("headSha") == sha
            and run.get("headBranch") == branch
        ]
        names = {str(run.get("workflowName")) for run in matches}
        if EXPECTED_MAIN_WORKFLOWS <= names:
            return matches
        if attempt + 1 < DISCOVERY_ATTEMPTS:
            time.sleep(POLL_SECONDS)
    raise ReleaseCliError(
        f"expected main Actions {sorted(EXPECTED_MAIN_WORKFLOWS)} did not appear "
        f"for {branch} at {sha}"
    )


def watch_branch_runs(repository: str, branch: str, sha: str) -> None:
    runs = find_branch_runs(repository, branch, sha)
    for run in runs:
        run_id = str(run.get("databaseId"))
        run_step(
            f"Watch {run.get('workflowName', 'Actions')} at {sha}",
            ["gh", "run", "watch", run_id, "--repo", repository, "--exit-status"],
        )
    print(f"All main-branch Actions passed for {sha}.")


def promote_pull_request(
    repository: str,
    number: int,
    tag: str,
    notes_file: Path | None,
    remote: str,
) -> str:
    require_clean_worktree()
    default_branch = repository_default_branch(repository)
    require_protected_auto_merge(repository, default_branch)
    pr = pull_request_details(repository, number)
    proof = require_promotable_pr(repository, pr, default_branch)
    if current_head() != proof.head:
        raise ReleaseCliError(
            f"local HEAD does not match pull request head {proof.head}; check it out first"
        )
    latest_base = fetch_default_branch(remote, default_branch)
    if latest_base != proof.base:
        raise ReleaseCliError(
            f"{remote}/{default_branch} changed after local validation; rerun submit-pr"
        )
    setup_git_transport()
    if notes_file is not None:
        if proof.profile != FULL_PROFILE or proof.tag is not None:
            raise ReleaseCliError(
                "release-candidate promotion requires the full validation profile"
            )
        if remote_tag_exists(repository, tag):
            raise ReleaseCliError(f"remote tag already exists: {tag}")
        run_metadata_preflight(
            tag, notes_file, proof.head, create_tag=False, remote=remote
        )
    else:
        if proof.profile != FINALIZATION_PROFILE or proof.tag != tag:
            raise ReleaseCliError(
                "release-finalization promotion requires a matching typed tag proof"
            )
        if pr.head_branch != finalization_branch(tag):
            raise ReleaseCliError(
                "--notes-file is required unless promoting the deterministic "
                "release-finalization branch"
            )
        published_tag = require_published_remote_tag(repository, tag)
        require_release_workflow_success(repository, tag, published_tag.target)
        verify_release(repository, tag)
        run_step(
            "Validate finalized release metadata",
            finalization_metadata_command(tag),
        )
        run_step(
            "Validate deterministic finalization tree",
            finalization_tree_command(proof, pr.head_branch),
        )
    require_required_pr_checks(repository, number)

    latest = pull_request_details(repository, number)
    latest_proof = require_promotable_pr(repository, latest, default_branch)
    if latest_proof != proof:
        raise ReleaseCliError("pull request changed while required checks were running")
    if fetch_default_branch(remote, default_branch) != proof.base:
        raise ReleaseCliError(
            f"{remote}/{default_branch} changed while checks were running; "
            "rerun submit-pr"
        )
    if not latest.auto_merge_enabled:
        run_step(
            "Enable protected GitHub auto-merge",
            [
                "gh",
                "pr",
                "merge",
                str(number),
                "--repo",
                repository,
                "--auto",
                "--merge",
                "--match-head-commit",
                proof.head,
            ],
        )

    merged: PullRequest | None = None
    for _ in range(MERGE_ATTEMPTS):
        state = pull_request_details(repository, number)
        if state.state == "MERGED":
            merged = state
            break
        if state.state != "OPEN":
            raise ReleaseCliError(
                f"pull request #{number} reached unexpected state {state.state}"
            )
        time.sleep(POLL_SECONDS)
    if merged is None:
        raise PromotionPending(
            f"auto-merge is enabled but pull request #{number} is still waiting: {pr.url}"
        )
    if not merged.merge_commit:
        raise ReleaseCliError(f"merged pull request #{number} has no merge commit")
    merge_sha = merged.merge_commit
    fetch_default_branch(remote, default_branch)
    contained = run_command(
        [
            "git",
            "merge-base",
            "--is-ancestor",
            merge_sha,
            f"{remote}/{default_branch}",
        ],
        capture=True,
    )
    if contained.returncode != 0:
        raise ReleaseCliError(
            f"merged commit {merge_sha} is not contained by {remote}/{default_branch}"
        )
    watch_branch_runs(repository, default_branch, merge_sha)
    print(f"Pull request #{number} promoted to {default_branch} at {merge_sha}.")
    return merge_sha


def merged_pr_commit(
    repository: str,
    number: int,
    remote: str,
) -> str:
    default_branch = repository_default_branch(repository)
    pr = pull_request_details(repository, number)
    if pr.state != "MERGED" or not pr.merge_commit:
        raise ReleaseCliError(f"pull request #{number} has not been merged")
    if pr.base_branch != default_branch:
        raise ReleaseCliError(
            f"pull request #{number} did not merge into {default_branch}"
        )
    latest_main = fetch_default_branch(remote, default_branch)
    contained = run_command(
        ["git", "merge-base", "--is-ancestor", pr.merge_commit, latest_main],
        capture=True,
    )
    if contained.returncode != 0:
        raise ReleaseCliError(
            f"pull request merge commit {pr.merge_commit} is not in {remote}/{default_branch}"
        )
    watch_branch_runs(repository, default_branch, pr.merge_commit)
    return pr.merge_commit


def find_release_run(repository: str, tag: str, sha: str) -> WorkflowRun:
    for attempt in range(DISCOVERY_ATTEMPTS):
        data = json_capture(
            [
                "gh",
                "run",
                "list",
                "--repo",
                repository,
                "--workflow",
                "Release",
                "--event",
                "push",
                "--limit",
                "50",
                "--json",
                "databaseId,headSha,headBranch,status,conclusion,url,workflowName",
            ],
            description="gh run list",
        )
        if not isinstance(data, list):
            raise ReleaseCliError("gh run list returned an unexpected value")
        matches = [
            run
            for run in data
            if isinstance(run, dict)
            and run.get("headSha") == sha
            and run.get("headBranch") == tag
            and run.get("workflowName") == "Release"
        ]
        if matches:
            selected = max(matches, key=lambda item: int(item.get("databaseId", 0)))
            try:
                return WorkflowRun(
                    database_id=int(selected["databaseId"]),
                    url=str(selected.get("url") or ""),
                    status=str(selected.get("status") or "unknown"),
                    conclusion=(
                        str(selected["conclusion"])
                        if selected.get("conclusion") is not None
                        else None
                    ),
                )
            except (KeyError, TypeError, ValueError) as error:
                raise ReleaseCliError("GitHub Actions run has invalid metadata") from error
        if attempt + 1 < DISCOVERY_ATTEMPTS:
            time.sleep(POLL_SECONDS)
    raise ReleaseCliError(
        f"no Release workflow run for tag {tag} at {sha} appeared within "
        f"{DISCOVERY_ATTEMPTS * POLL_SECONDS} seconds"
    )


def workflow_state(repository: str, run_id: int) -> dict[str, object]:
    data = json_capture(
        [
            "gh",
            "run",
            "view",
            str(run_id),
            "--repo",
            repository,
            "--json",
            "status,conclusion,url,jobs",
        ],
        description="gh run view",
    )
    if not isinstance(data, dict):
        raise ReleaseCliError("gh run view returned an unexpected value")
    return data


def waiting_for_release_gate(state: dict[str, object]) -> bool:
    if state.get("status") == "waiting":
        return True
    jobs = state.get("jobs", [])
    if not isinstance(jobs, list):
        raise ReleaseCliError("GitHub Actions run jobs are not a list")
    return any(
        isinstance(job, dict)
        and job.get("name") == "Build and publish"
        and job.get("status") == "waiting"
        for job in jobs
    )


def watch_release(repository: str, tag: str, sha: str) -> None:
    run = find_release_run(repository, tag, sha)
    print(f"Release workflow: {run.url or run.database_id}")
    while True:
        state = workflow_state(repository, run.database_id)
        status = str(state.get("status", "unknown"))
        conclusion = state.get("conclusion")
        url = str(state.get("url") or run.url or "")
        if waiting_for_release_gate(state):
            raise ReleaseCliError(
                "Release workflow unexpectedly reached a waiting environment gate; "
                "the automated release policy drifted after tag publication. Restore "
                f"the release environment policy and rerun monitor: {url or run.database_id}"
            )
        if status == "completed":
            if conclusion == "success":
                print(f"Release workflow completed successfully: {url or run.database_id}")
                return
            raise ReleaseCliError(
                f"Release workflow completed with {conclusion or 'no conclusion'}: "
                f"{url or run.database_id}"
            )
        try:
            watched = run_command(
                [
                    "gh",
                    "run",
                    "watch",
                    str(run.database_id),
                    "--repo",
                    repository,
                    "--exit-status",
                ],
                timeout=WATCH_SECONDS,
            )
        except subprocess.TimeoutExpired:
            continue
        if watched.returncode != 0:
            after = workflow_state(repository, run.database_id)
            if waiting_for_release_gate(after):
                raise ReleaseCliError(
                    "Release workflow unexpectedly reached a waiting environment gate; "
                    "restore the automated release policy and rerun monitor: "
                    f"{after.get('url') or url or run.database_id}"
                )
            if after.get("status") == "completed":
                continue
            raise ReleaseCliError(
                "gh run watch stopped before the Release workflow completed"
            )


def require_release_workflow_success(repository: str, tag: str, sha: str) -> None:
    run = find_release_run(repository, tag, sha)
    state = workflow_state(repository, run.database_id)
    if state.get("status") != "completed" or state.get("conclusion") != "success":
        raise ReleaseCliError(
            f"Release workflow is not successfully completed: {run.url or run.database_id}"
        )


def verify_release(repository: str, tag: str) -> None:
    release = release_details(repository, tag, required=True)
    assert release is not None
    if release.get("tagName") != tag:
        raise ReleaseCliError(
            f"GitHub Release tag is {release.get('tagName')!r}; expected {tag!r}"
        )
    if release.get("isDraft") is True:
        raise ReleaseCliError(f"GitHub Release {tag} is still a draft")
    if release.get("isPrerelease") is True:
        raise ReleaseCliError(f"GitHub Release {tag} is unexpectedly a prerelease")
    assets = release.get("assets", [])
    if not isinstance(assets, list):
        raise ReleaseCliError("GitHub Release assets are not a list")
    names = {
        str(asset.get("name"))
        for asset in assets
        if isinstance(asset, dict) and asset.get("name")
    }
    missing = sorted(PRICING_ASSETS - names)
    if missing:
        raise ReleaseCliError(
            "GitHub Release is missing required immutable pricing assets: "
            + ", ".join(missing)
        )
    print(f"GitHub Release verified: {release.get('url', tag)}")


def inspect(repository: str, tag: str, pr_number: int | None) -> None:
    if pr_number is not None:
        pr = pull_request_details(repository, pr_number)
        print(
            f"Pull request #{pr.number}: {pr.state} {pr.head_branch}@{pr.head_oid} "
            f"-> {pr.base_branch}@{pr.base_oid} {pr.url}"
        )
    details = local_tag_details(tag, required=False)
    if details is None:
        print(f"Local tag: absent ({tag})")
        local_sha = current_head()
    else:
        print(
            f"Local tag: {details['object_type']} at {details['target']}; "
            f"subject: {details['subject']}"
        )
        local_sha = details["target"]
    remote = remote_tag_details(repository, tag)
    if remote is None:
        print("Remote tag: absent")
        sha = local_sha
    else:
        kind = "annotated" if remote.annotated else "lightweight"
        print(f"Remote tag: {kind} at {remote.target}; subject: {remote.subject}")
        sha = remote.target
    release = release_details(repository, tag, required=False)
    print(f"GitHub Release: {release.get('url', tag) if release else 'absent'}")
    try:
        run = find_release_run(repository, tag, sha)
    except ReleaseCliError as error:
        print(f"Release workflow: not found ({error})")
    else:
        print(
            f"Release workflow: {run.status}/{run.conclusion or 'pending'} "
            f"{run.url or run.database_id}"
        )


def finalization_branch(tag: str) -> str:
    return "release/finalize-" + tag.removeprefix("v").replace("+", "-")


def finalize(repository: str, tag: str, remote: str) -> None:
    published_tag = require_published_remote_tag(repository, tag)
    require_release_workflow_success(repository, tag, published_tag.target)
    verify_release(repository, tag)
    require_clean_worktree()

    default_branch = repository_default_branch(repository)
    fetch_default_branch(remote, default_branch)
    branch = finalization_branch(tag)
    exists = run_command(
        ["git", "show-ref", "--verify", "--quiet", f"refs/heads/{branch}"]
    )
    if exists.returncode == 0:
        raise ReleaseCliError(
            f"local finalization branch already exists: {branch}; inspect it before retrying"
        )
    if exists.returncode != 1:
        raise ReleaseCliError(f"unable to inspect local branch {branch}")
    run_step(
        "Create release-finalization branch",
        ["git", "switch", "--create", branch, "--no-track", f"{remote}/{default_branch}"],
    )

    path = ROOT / "UPSTREAM.md"
    content = path.read_text(encoding="utf-8")
    row = re.compile(
        rf"^(\|\s*`{re.escape(tag)}`\s*\|\s*`[^`]+`\s*\|\s*`[0-9a-f]{{40}}`\s*\|\s*)planned(\s*\|)$",
        re.MULTILINE,
    )
    updated, replacements = row.subn(r"\1published\2", content)
    if replacements != 1:
        raise ReleaseCliError(
            f"UPSTREAM.md has {replacements} planned mapping rows for {tag}; expected one"
        )
    path.write_text(updated, encoding="utf-8")
    validation = run_command(
        finalization_metadata_command(tag),
        capture=True,
    )
    if validation.returncode != 0:
        path.write_text(content, encoding="utf-8")
        detail = (validation.stdout or "").strip()
        raise ReleaseCliError(
            "post-publication metadata validation failed; restored UPSTREAM.md"
            + (f": {detail[-2000:]}" if detail else "")
        )
    run_step(
        "Synchronize release documentation",
        [sys.executable, "tools/update_release_docs.py"],
    )
    run_step(
        "Stage finalized release metadata",
        ["git", "add", "--", *sorted(FINALIZATION_ALLOWED_PATHS)],
    )
    staged = frozenset(
        capture(["git", "diff", "--cached", "--name-only"]).splitlines()
    )
    unexpected = staged - FINALIZATION_ALLOWED_PATHS
    if "UPSTREAM.md" not in staged or unexpected:
        raise ReleaseCliError(
            "finalization staged invalid paths: "
            f"{', '.join(sorted(staged)) if staged else '<none>'}"
        )
    subject = f"docs(release): mark {tag} published"
    run_step("Commit release finalization", ["git", "commit", "-m", subject])
    run_step(
        "Submit release-finalization pull request",
        [
            sys.executable,
            "skills/push-cli/scripts/push_cli.py",
            "submit-pr",
            "--remote",
            remote,
            "--title",
            subject,
            "--profile",
            FINALIZATION_PROFILE,
            "--tag",
            tag,
        ],
    )
    print(
        f"Release finalization submitted from {branch}. Promote its PR through "
        "release-cli after required Actions pass."
    )


def require_notes_file(args: argparse.Namespace, action: str) -> Path:
    if args.notes_file is None:
        raise ReleaseCliError(f"--notes-file is required for {action}")
    return args.notes_file


def require_pr_number(args: argparse.Namespace, action: str) -> int:
    if args.pr is None:
        raise ReleaseCliError(f"--pr is required for {action}")
    return args.pr


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Promote pull requests and publish immutable releases."
    )
    parser.add_argument(
        "action",
        choices=(
            "inspect",
            "promote-pr",
            "validate",
            "tag",
            "publish",
            "monitor",
            "verify",
            "finalize",
        ),
    )
    parser.add_argument("--tag", required=True, help="vX.Y.Z+custom.NNN")
    parser.add_argument("--pr", type=int, help="release pull-request number")
    parser.add_argument("--notes-file", type=Path)
    parser.add_argument("--remote", default=DEFAULT_REMOTE)
    parser.add_argument("--repo-root", type=Path, default=ROOT, help=argparse.SUPPRESS)
    return parser.parse_args()


def main() -> int:
    global ROOT
    args = parse_args()
    ROOT = args.repo_root.resolve()
    try:
        repository = github_gate(args.remote)
        validate_tag(args.tag)

        if args.action == "inspect":
            inspect(repository, args.tag, args.pr)
            return 0

        if args.action == "promote-pr":
            promote_pull_request(
                repository,
                require_pr_number(args, "promote-pr"),
                args.tag,
                args.notes_file,
                args.remote,
            )
            return 0

        if args.action in {"validate", "tag"}:
            commit = merged_pr_commit(
                repository,
                require_pr_number(args, args.action),
                args.remote,
            )
            setup_git_transport()
            run_metadata_preflight(
                args.tag,
                require_notes_file(args, args.action),
                commit,
                create_tag=args.action == "tag",
                remote=args.remote,
            )
            return 0

        if args.action == "publish":
            require_clean_worktree(allow_untracked=True)
            target = require_publishable_local_tag(args.tag)
            default_branch = repository_default_branch(repository)
            latest_main = fetch_default_branch(args.remote, default_branch)
            contained = run_command(
                ["git", "merge-base", "--is-ancestor", target, latest_main],
                capture=True,
            )
            if contained.returncode != 0:
                raise ReleaseCliError(
                    f"tag target {target} is not contained by {args.remote}/{default_branch}"
                )
            if remote_tag_exists(repository, args.tag):
                raise ReleaseCliError(
                    f"remote tag already exists: {args.tag}; immutable tags are never reused"
                )
            if release_details(repository, args.tag, required=False) is not None:
                raise ReleaseCliError(
                    f"GitHub Release already exists for {args.tag}; refusing to republish"
                )
            require_automated_release_policy(repository)
            setup_git_transport()
            run_step("Push exact release tag", ["git", "push", args.remote, args.tag])
            print(
                f"Published tag {args.tag}. Run monitor to observe the Release workflow."
            )
            return 0

        if args.action == "finalize":
            finalize(repository, args.tag, args.remote)
            return 0
        published_tag = require_published_remote_tag(repository, args.tag)
        if args.action == "monitor":
            watch_release(repository, args.tag, published_tag.target)
            return 0
        if args.action == "verify":
            require_release_workflow_success(
                repository, args.tag, published_tag.target
            )
            verify_release(repository, args.tag)
            return 0
        raise ReleaseCliError(f"unsupported action: {args.action}")
    except PromotionPending as error:
        print(f"release-cli paused: {error}", file=sys.stderr)
        return 2
    except ReleaseCliError as error:
        print(f"release-cli stopped: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
