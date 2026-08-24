#!/usr/bin/env python3
"""Verify successful GitHub Actions evidence for an exact branch and commit."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from collections.abc import Iterable, Sequence


SHA_RE = re.compile(r"^[0-9a-f]{40}$")
DEFAULT_WORKFLOWS = ("CI", "Security Scan")


class WorkflowProvenanceError(RuntimeError):
    """The required immutable workflow evidence is absent or unsuccessful."""


def matching_runs(
    runs: Iterable[object],
    *,
    branch: str,
    sha: str,
) -> list[dict[str, object]]:
    return [
        run
        for run in runs
        if isinstance(run, dict)
        and run.get("event") == "push"
        and run.get("headBranch") == branch
        and run.get("headSha") == sha
    ]


def require_successful_workflows(
    runs: Iterable[object],
    *,
    branch: str,
    sha: str,
    workflows: Sequence[str] = DEFAULT_WORKFLOWS,
) -> list[dict[str, object]]:
    if not branch:
        raise WorkflowProvenanceError("workflow provenance requires a branch")
    if SHA_RE.fullmatch(sha) is None:
        raise WorkflowProvenanceError(
            "workflow provenance requires a 40-character lowercase commit SHA"
        )

    matches = matching_runs(runs, branch=branch, sha=sha)
    failures: list[str] = []
    for workflow in workflows:
        workflow_runs = [run for run in matches if run.get("workflowName") == workflow]
        if not workflow_runs:
            failures.append(f"{workflow}: missing")
            continue
        for run in workflow_runs:
            status = str(run.get("status") or "unknown")
            conclusion = str(run.get("conclusion") or "pending")
            if status != "completed" or conclusion != "success":
                failures.append(f"{workflow}: {status}/{conclusion}")

    if failures:
        raise WorkflowProvenanceError(
            f"required {branch} workflow evidence failed for {sha}: "
            + "; ".join(failures)
        )
    return matches


def list_branch_runs(repository: str, branch: str) -> list[object]:
    result = subprocess.run(
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
            "workflowName,event,headBranch,headSha,status,conclusion,url,databaseId",
        ],
        check=False,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise WorkflowProvenanceError(
            "unable to query GitHub Actions workflow provenance"
            + (f": {detail[-2000:]}" if detail else "")
        )
    try:
        data = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise WorkflowProvenanceError(
            "GitHub Actions workflow provenance returned invalid JSON"
        ) from error
    if not isinstance(data, list):
        raise WorkflowProvenanceError(
            "GitHub Actions workflow provenance returned an unexpected value"
        )
    return data


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Require successful push workflows for an exact branch commit."
    )
    parser.add_argument("--repository", required=True)
    parser.add_argument("--branch", required=True)
    parser.add_argument("--sha", required=True)
    parser.add_argument(
        "--workflow",
        action="append",
        dest="workflows",
        help="required workflow name; repeat for more than one",
    )
    args = parser.parse_args()

    try:
        runs = list_branch_runs(args.repository, args.branch)
        matches = require_successful_workflows(
            runs,
            branch=args.branch,
            sha=args.sha,
            workflows=tuple(args.workflows or DEFAULT_WORKFLOWS),
        )
    except WorkflowProvenanceError as error:
        print(f"Workflow provenance failed: {error}", file=sys.stderr)
        return 1

    names = sorted({str(run.get("workflowName")) for run in matches})
    print(
        f"Workflow provenance passed for {args.branch}@{args.sha}: "
        + ", ".join(names)
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
