#!/usr/bin/env python3
"""Focused tests for release-cli state and mutation boundaries."""

from __future__ import annotations

import argparse
import importlib.util
import json
import re
import subprocess
import sys
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "release_cli.py"
SPEC = importlib.util.spec_from_file_location("release_cli_under_test", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
release_cli = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = release_cli
SPEC.loader.exec_module(release_cli)

TAG = "v1.2.3+custom.009"
BASE = "a" * 40
HEAD = "b" * 40
MERGE = "c" * 40
REPOSITORY = "LuckyKuang/sub2api-plus"
ROOT = Path(__file__).resolve().parents[3]


def marker(base: str = BASE, head: str = HEAD) -> str:
    payload = json.dumps({"base": base, "head": head}, separators=(",", ":"))
    return f"<!-- sub2api-submit-pr: {payload} -->"


def protected_rules(
    *,
    strict: bool = True,
    contexts: frozenset[str] = release_cli.REQUIRED_PR_STATUS_CONTEXTS,
) -> list[dict[str, object]]:
    return [
        {
            "type": "pull_request",
            "parameters": {"allowed_merge_methods": ["merge"]},
        },
        {
            "type": "required_status_checks",
            "parameters": {
                "strict_required_status_checks_policy": strict,
                "required_status_checks": [
                    {"context": context} for context in sorted(contexts)
                ],
            },
        },
    ]


def pull_request(
    *,
    state: str = "OPEN",
    base: str = BASE,
    head: str = HEAD,
    merge: str | None = None,
    auto_merge: bool = False,
) -> object:
    return release_cli.PullRequest(
        number=17,
        state=state,
        is_draft=False,
        base_branch="main",
        base_oid=base,
        head_branch="release/candidate",
        head_oid=head,
        head_owner="LuckyKuang",
        merge_state="CLEAN",
        merge_commit=merge,
        auto_merge_enabled=auto_merge,
        body=marker(base, head),
        url="https://github.com/LuckyKuang/sub2api-plus/pull/17",
    )


class ValidationProofTest(unittest.TestCase):
    def test_marker_requires_exact_base_and_head(self) -> None:
        proof = release_cli.parse_validation_proof(marker())
        self.assertEqual(release_cli.ValidationProof(BASE, HEAD), proof)

    def test_duplicate_marker_is_rejected(self) -> None:
        with self.assertRaisesRegex(release_cli.ReleaseCliError, "exactly one"):
            release_cli.parse_validation_proof(marker() + "\n" + marker())

    def test_changed_pr_head_is_rejected(self) -> None:
        pr = pull_request(head="d" * 40)
        pr = release_cli.PullRequest(**{**pr.__dict__, "body": marker(BASE, HEAD)})
        with self.assertRaisesRegex(release_cli.ReleaseCliError, "head changed"):
            release_cli.require_promotable_pr(REPOSITORY, pr, "main")


class RepositoryPolicyTest(unittest.TestCase):
    def test_required_contexts_are_emitted_by_pull_request_workflows(self) -> None:
        workflow_job_re = re.compile(r"^  ([a-zA-Z0-9_-]+):\n", re.MULTILINE)
        workflow_jobs: set[str] = set()
        for name in ("backend-ci.yml", "security-scan.yml"):
            workflow = ROOT.joinpath(".github", "workflows", name).read_text(
                encoding="utf-8"
            )
            workflow_jobs.update(workflow_job_re.findall(workflow))

        expected = release_cli.REQUIRED_PR_STATUS_CONTEXTS - {
            "sub2api/local-validation"
        }
        self.assertEqual(expected - workflow_jobs, set())

    def test_auto_merge_must_be_enabled(self) -> None:
        with mock.patch.object(
            release_cli,
            "repository_settings",
            return_value={"allow_auto_merge": False},
        ):
            with self.assertRaisesRegex(release_cli.ReleaseCliError, "disabled"):
                release_cli.require_protected_auto_merge(REPOSITORY, "main")

    def test_merge_commit_mode_must_be_enabled(self) -> None:
        with mock.patch.object(
            release_cli,
            "repository_settings",
            return_value={"allow_auto_merge": True, "allow_merge_commit": False},
        ):
            with self.assertRaisesRegex(release_cli.ReleaseCliError, "merge-commit"):
                release_cli.require_protected_auto_merge(REPOSITORY, "main")

    def test_required_rules_must_be_present(self) -> None:
        with (
            mock.patch.object(
                release_cli,
                "repository_settings",
                return_value={"allow_auto_merge": True, "allow_merge_commit": True},
            ),
            mock.patch.object(
                release_cli,
                "json_capture",
                return_value=[{"type": "pull_request"}],
            ),
        ):
            with self.assertRaisesRegex(
                release_cli.ReleaseCliError, "required_status_checks"
            ):
                release_cli.require_protected_auto_merge(REPOSITORY, "main")

    def test_branch_must_require_current_head_and_complete_matrix(self) -> None:
        incomplete = release_cli.REQUIRED_PR_STATUS_CONTEXTS - {"backend-security"}
        with (
            mock.patch.object(
                release_cli,
                "repository_settings",
                return_value={"allow_auto_merge": True, "allow_merge_commit": True},
            ),
            mock.patch.object(
                release_cli,
                "json_capture",
                return_value=protected_rules(strict=False, contexts=incomplete),
            ),
        ):
            with self.assertRaisesRegex(release_cli.ReleaseCliError, "current"):
                release_cli.require_protected_auto_merge(REPOSITORY, "main")

        with (
            mock.patch.object(
                release_cli,
                "repository_settings",
                return_value={"allow_auto_merge": True, "allow_merge_commit": True},
            ),
            mock.patch.object(
                release_cli,
                "json_capture",
                return_value=protected_rules(contexts=incomplete),
            ),
        ):
            with self.assertRaisesRegex(release_cli.ReleaseCliError, "backend-security"):
                release_cli.require_protected_auto_merge(REPOSITORY, "main")

    def test_complete_protected_policy_is_accepted(self) -> None:
        with (
            mock.patch.object(
                release_cli,
                "repository_settings",
                return_value={"allow_auto_merge": True, "allow_merge_commit": True},
            ),
            mock.patch.object(
                release_cli,
                "json_capture",
                return_value=protected_rules(),
            ),
        ):
            release_cli.require_protected_auto_merge(REPOSITORY, "main")


class RemoteTagTest(unittest.TestCase):
    def test_annotated_remote_tag_resolves_exact_target_and_subject(self) -> None:
        tag_object = "d" * 40
        with mock.patch.object(
            release_cli,
            "json_capture",
            side_effect=[
                [
                    {
                        "ref": f"refs/tags/{TAG}",
                        "object": {"type": "tag", "sha": tag_object},
                    }
                ],
                {
                    "tag": TAG,
                    "message": f"Sub2API Plus {TAG}\n\nRelease notes",
                    "object": {"type": "commit", "sha": MERGE},
                },
            ],
        ):
            details = release_cli.require_published_remote_tag(REPOSITORY, TAG)

        self.assertEqual(
            release_cli.RemoteTag(True, tag_object, MERGE, f"Sub2API Plus {TAG}"),
            details,
        )

    def test_lightweight_remote_tag_is_rejected(self) -> None:
        with mock.patch.object(
            release_cli,
            "json_capture",
            return_value=[
                {
                    "ref": f"refs/tags/{TAG}",
                    "object": {"type": "commit", "sha": MERGE},
                }
            ],
        ):
            with self.assertRaisesRegex(release_cli.ReleaseCliError, "not annotated"):
                release_cli.require_published_remote_tag(REPOSITORY, TAG)


class PromotionTest(unittest.TestCase):
    def test_promote_uses_native_auto_merge_and_waits_for_merge_sha(self) -> None:
        candidate = pull_request()
        merged = pull_request(state="MERGED", merge=MERGE, auto_merge=True)
        proof = release_cli.ValidationProof(BASE, HEAD)
        completed = subprocess.CompletedProcess([], 0, "")
        with (
            mock.patch.object(release_cli, "require_clean_worktree"),
            mock.patch.object(release_cli, "repository_default_branch", return_value="main"),
            mock.patch.object(release_cli, "require_protected_auto_merge"),
            mock.patch.object(
                release_cli,
                "pull_request_details",
                side_effect=[candidate, candidate, merged],
            ),
            mock.patch.object(release_cli, "require_promotable_pr", return_value=proof),
            mock.patch.object(release_cli, "current_head", return_value=HEAD),
            mock.patch.object(release_cli, "fetch_default_branch", return_value=BASE),
            mock.patch.object(release_cli, "setup_git_transport"),
            mock.patch.object(release_cli, "remote_tag_exists", return_value=False),
            mock.patch.object(release_cli, "run_metadata_preflight"),
            mock.patch.object(release_cli, "require_required_pr_checks"),
            mock.patch.object(release_cli, "run_step") as run_step,
            mock.patch.object(release_cli, "run_command", return_value=completed),
            mock.patch.object(release_cli, "watch_branch_runs") as watch,
        ):
            result = release_cli.promote_pull_request(
                REPOSITORY, 17, TAG, Path("release-notes.md"), "origin"
            )

        self.assertEqual(MERGE, result)
        merge_commands = [call.args[1] for call in run_step.call_args_list]
        self.assertIn(
            [
                "gh",
                "pr",
                "merge",
                "17",
                "--repo",
                REPOSITORY,
                "--auto",
                "--merge",
                "--match-head-commit",
                HEAD,
            ],
            merge_commands,
        )
        self.assertFalse(any("--admin" in command for command in merge_commands))
        watch.assert_called_once_with(REPOSITORY, "main", MERGE)

    def test_finalize_promotion_requires_published_tag_and_no_notes(self) -> None:
        final_branch = release_cli.finalization_branch(TAG)
        candidate = release_cli.PullRequest(
            **{**pull_request().__dict__, "head_branch": final_branch}
        )
        proof = release_cli.ValidationProof(BASE, HEAD)
        with (
            mock.patch.object(release_cli, "require_clean_worktree"),
            mock.patch.object(release_cli, "repository_default_branch", return_value="main"),
            mock.patch.object(release_cli, "require_protected_auto_merge"),
            mock.patch.object(release_cli, "pull_request_details", return_value=candidate),
            mock.patch.object(release_cli, "require_promotable_pr", return_value=proof),
            mock.patch.object(release_cli, "current_head", return_value=HEAD),
            mock.patch.object(release_cli, "fetch_default_branch", return_value=BASE),
            mock.patch.object(release_cli, "setup_git_transport"),
            mock.patch.object(
                release_cli,
                "require_published_remote_tag",
                side_effect=release_cli.ReleaseCliError(
                    "release-finalization promotion requires the published remote tag"
                ),
            ),
        ):
            with self.assertRaisesRegex(release_cli.ReleaseCliError, "published remote tag"):
                release_cli.promote_pull_request(REPOSITORY, 17, TAG, None, "origin")


class MainFlowTest(unittest.TestCase):
    def args(self, action: str) -> argparse.Namespace:
        return argparse.Namespace(
            action=action,
            tag=TAG,
            pr=None,
            notes_file=None,
            remote="origin",
            repo_root=Path("/repo"),
        )

    def test_publish_only_pushes_tag(self) -> None:
        args = self.args("publish")
        completed = subprocess.CompletedProcess([], 0, "")
        with (
            mock.patch.object(release_cli, "parse_args", return_value=args),
            mock.patch.object(release_cli, "github_gate", return_value=REPOSITORY),
            mock.patch.object(release_cli, "require_clean_worktree"),
            mock.patch.object(release_cli, "require_publishable_local_tag", return_value=MERGE),
            mock.patch.object(release_cli, "repository_default_branch", return_value="main"),
            mock.patch.object(release_cli, "fetch_default_branch", return_value=MERGE),
            mock.patch.object(release_cli, "run_command", return_value=completed),
            mock.patch.object(release_cli, "remote_tag_exists", return_value=False),
            mock.patch.object(release_cli, "release_details", return_value=None),
            mock.patch.object(release_cli, "setup_git_transport"),
            mock.patch.object(release_cli, "run_step") as run_step,
            mock.patch.object(release_cli, "watch_release") as watch,
            mock.patch.object(release_cli, "verify_release") as verify,
        ):
            self.assertEqual(0, release_cli.main())

        run_step.assert_called_once_with(
            "Push exact release tag", ["git", "push", "origin", TAG]
        )
        watch.assert_not_called()
        verify.assert_not_called()

    def test_verify_does_not_watch_workflow(self) -> None:
        args = self.args("verify")
        details = release_cli.RemoteTag(
            True, "d" * 40, MERGE, f"Sub2API Plus {TAG}"
        )
        with (
            mock.patch.object(release_cli, "parse_args", return_value=args),
            mock.patch.object(release_cli, "github_gate", return_value=REPOSITORY),
            mock.patch.object(
                release_cli, "require_published_remote_tag", return_value=details
            ),
            mock.patch.object(release_cli, "require_release_workflow_success") as success,
            mock.patch.object(release_cli, "verify_release") as verify,
            mock.patch.object(release_cli, "watch_release") as watch,
        ):
            self.assertEqual(0, release_cli.main())

        success.assert_called_once_with(REPOSITORY, TAG, MERGE)
        verify.assert_called_once_with(REPOSITORY, TAG)
        watch.assert_not_called()


class FinalizationTest(unittest.TestCase):
    def test_branch_name_is_deterministic_and_oci_safe(self) -> None:
        self.assertEqual(
            "release/finalize-1.2.3-custom.009",
            release_cli.finalization_branch(TAG),
        )


if __name__ == "__main__":
    unittest.main()
