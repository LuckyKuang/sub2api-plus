#!/usr/bin/env python3
"""Regression tests for release-policy helpers."""

from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import check_release
import check_new_migrations
import check_published_release
import release_docs
import release_finalization
import release_preflight
import release_validation
import validation_runtime
import workflow_provenance


ROOT = Path(__file__).resolve().parents[1]
TAG = "v1.2.3+custom.009"
OFFICIAL_TAG = "v1.2.3"
OFFICIAL_COMMIT = "a" * 40


def valid_notes() -> str:
    return f"""Sub2API Plus {TAG}

## Highlights

One useful change.

## Compatibility and migration

No migration is required.

## Known issues

No known release blockers.

## Upstream baseline

Official release: {OFFICIAL_TAG}
Official commit: {OFFICIAL_COMMIT}
"""


class ReleaseNotesTests(unittest.TestCase):
    def validate(self, notes: str) -> list[str]:
        errors: list[str] = []
        check_release.validate_notes(
            notes,
            TAG,
            OFFICIAL_TAG,
            OFFICIAL_COMMIT,
            errors,
            require_subject=True,
        )
        return errors

    def test_valid_notes_pass(self) -> None:
        self.assertEqual(self.validate(valid_notes()), [])

    def test_wrong_subject_fails(self) -> None:
        notes = valid_notes().replace(
            f"Sub2API Plus {TAG}",
            "Sub2API Plus wrong-version",
            1,
        )
        self.assertTrue(
            any("first non-empty release-notes line" in error for error in self.validate(notes))
        )

    def test_duplicate_required_heading_fails(self) -> None:
        notes = valid_notes() + "\n## Highlights\n\nDuplicated.\n"
        self.assertTrue(
            any("duplicate '## Highlights'" in error for error in self.validate(notes))
        )

    def test_upstream_identifiers_must_be_in_upstream_section(self) -> None:
        notes = valid_notes().replace(
            f"Official release: {OFFICIAL_TAG}\nOfficial commit: {OFFICIAL_COMMIT}",
            "Baseline recorded below.",
        )
        notes = notes.replace(
            "## Upstream baseline",
            f"Official release: {OFFICIAL_TAG}\n"
            f"Official commit: {OFFICIAL_COMMIT}\n\n"
            "## Upstream baseline",
            1,
        )
        errors = self.validate(notes)
        self.assertTrue(any("does not name official release" in error for error in errors))
        self.assertTrue(any("does not name official commit" in error for error in errors))


class ReleaseContainerTests(unittest.TestCase):
    command = [sys.executable, "tools/check_release.py", "--tag", TAG]

    def test_validation_container_forwards_proxy_names_without_values(self) -> None:
        runtime = validation_runtime.Runtime("linux-docker", (), None)
        with mock.patch.dict(
            os.environ,
            {
                "HTTP_PROXY": "http://proxy-user:proxy-secret@example.invalid:8080",
                "https_proxy": "http://lowercase.example.invalid:8080",
                "HTTPS_PROXY": "",
                "NO_PROXY": "",
                "http_proxy": "",
                "no_proxy": "",
            },
            clear=True,
        ):
            argv = validation_runtime.validation_run_command(
                runtime,
                self.command,
                root=ROOT,
                image="sub2api-validation:test",
                user=None,
                caches=[],
            )
        self.assertIn("HTTP_PROXY", argv)
        self.assertIn("https_proxy", argv)
        self.assertNotIn("HTTPS_PROXY", argv)
        self.assertNotIn("proxy-secret", " ".join(argv))

    def test_unavailable_runtime_never_runs_check_on_host(self) -> None:
        with (
            mock.patch.dict(os.environ, {validation_runtime.IN_VALIDATION_ENV: ""}),
            mock.patch.object(validation_runtime, "in_validation_container", return_value=False),
            mock.patch.object(validation_runtime, "probe_runtime", side_effect=validation_runtime.ValidationRuntimeError("WSL2 Docker unavailable")),
            mock.patch.object(release_validation, "execute") as execute,
        ):
            result = release_validation.run(self.command, root=ROOT)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("WSL2 Docker unavailable", result.stdout)
        execute.assert_not_called()

    def test_flag_without_container_marker_is_rejected(self) -> None:
        with (
            mock.patch.dict(os.environ, {validation_runtime.IN_VALIDATION_ENV: "1"}),
            mock.patch.object(validation_runtime, "in_validation_container", return_value=False),
            mock.patch.object(release_validation, "execute") as execute,
        ):
            result = release_validation.run(self.command, root=ROOT)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("marker", result.stdout)
        execute.assert_not_called()

    def test_existing_container_runs_check_without_nested_runtime(self) -> None:
        completed = subprocess.CompletedProcess(self.command, 3, "metadata rejected")
        with (
            mock.patch.object(validation_runtime, "in_validation_container", return_value=True),
            mock.patch.object(validation_runtime, "require_in_validation") as guard,
            mock.patch.object(validation_runtime, "probe_runtime") as probe,
            mock.patch.object(release_validation, "execute", return_value=completed) as execute,
        ):
            result = release_validation.run(self.command, root=ROOT)
        self.assertEqual(result.returncode, 3)
        guard.assert_called_once_with(tool="release validation")
        execute.assert_called_once_with(self.command, root=ROOT)
        probe.assert_not_called()

    def test_container_failure_preserves_status_and_cleans_runtime(self) -> None:
        selected = validation_runtime.Runtime("wsl2-docker", ("wsl.exe", "-d", "Debian", "--"), "/mnt/c/repo")
        with (
            mock.patch.dict(os.environ, {validation_runtime.IN_VALIDATION_ENV: ""}),
            mock.patch.object(validation_runtime, "in_validation_container", return_value=False),
            mock.patch.object(validation_runtime, "probe_runtime", return_value=selected),
            mock.patch.object(validation_runtime, "ensure_validation_image"),
            mock.patch.object(validation_runtime, "runtime_user", return_value="1000:1000"),
            mock.patch.object(validation_runtime, "cache_mounts", return_value=[]),
            mock.patch.object(validation_runtime, "cleanup_validation_runtime") as cleanup,
            mock.patch.object(release_validation, "execute", return_value=subprocess.CompletedProcess([], 7, "bad metadata")) as execute,
        ):
            result = release_validation.run(self.command, root=ROOT)
        self.assertEqual(result.returncode, 7)
        self.assertIn("bad metadata", result.stdout)
        cleanup.assert_called_once()
        argv = execute.call_args.args[0]
        self.assertEqual(argv[:6], ["wsl.exe", "-d", "Debian", "--", "docker", "run"])
        self.assertIn("--rm", argv)
        self.assertIn("PYTHONDONTWRITEBYTECODE=1", argv)
        self.assertEqual(argv[-4:], ["python3", "/mnt/c/repo/tools/check_release.py", "--tag", TAG])

    def test_external_notes_are_staged_and_removed_even_on_failure(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "repo with spaces"
            root.mkdir()
            notes = Path(temp) / "external notes.md"
            content = valid_notes()
            notes.write_text(content, encoding="utf-8")
            selected = validation_runtime.Runtime("wsl2-docker", (), "/mnt/c/repo with spaces")
            staged: list[Path] = []

            def launch(runtime, argv, **kwargs):
                argument = argv[argv.index("--notes-file") + 1]
                relative = argument.removeprefix("/mnt/c/repo with spaces/")
                stage = root / relative
                self.assertEqual(stage.read_text(encoding="utf-8"), content)
                self.assertNotEqual(stage, notes)
                staged.append(stage)
                raise subprocess.CalledProcessError(4, ["docker", "run"], "rejected")

            with (
                mock.patch.dict(os.environ, {validation_runtime.IN_VALIDATION_ENV: ""}),
                mock.patch.object(validation_runtime, "in_validation_container", return_value=False),
                mock.patch.object(validation_runtime, "probe_runtime", return_value=selected),
                mock.patch.object(validation_runtime, "ensure_validation_image"),
                mock.patch.object(validation_runtime, "launch_in_validation", side_effect=launch),
            ):
                result = release_validation.run([*self.command, "--notes-file", str(notes)], root=root)
            self.assertEqual(result.returncode, 4)
            self.assertTrue(staged)
            self.assertFalse(staged[0].exists())
            self.assertFalse((root / "temp").exists())
            self.assertEqual(notes.read_text(encoding="utf-8"), content)

    def test_runner_cannot_launch_publication_commands(self) -> None:
        with mock.patch.object(release_validation, "execute") as execute:
            result = release_validation.run(
                [sys.executable, "skills/release-cli/scripts/release_cli.py", "publish", "--tag", TAG],
                root=ROOT,
            )
        self.assertNotEqual(result.returncode, 0)
        execute.assert_not_called()

    def test_metadata_failure_prevents_local_tag_creation(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            notes = Path(temp) / "notes.md"
            notes.write_text(valid_notes(), encoding="utf-8")
            with (
                mock.patch.object(sys, "argv", ["release_preflight.py", "--tag", TAG, "--notes-file", str(notes), "--create-tag"]),
                mock.patch.object(release_preflight, "ensure_clean"),
                mock.patch.object(release_preflight, "ensure_tag_absent"),
                mock.patch.object(release_preflight, "git_output", side_effect=["a" * 40, "b" * 40, "b" * 40]),
                mock.patch.object(release_validation, "run", return_value=subprocess.CompletedProcess([], 1, "metadata rejected")) as check,
                mock.patch.object(release_preflight, "run") as mutate,
            ):
                self.assertEqual(release_preflight.main(), 1)
        check.assert_called_once()
        mutate.assert_not_called()

    def test_successful_preflight_verifies_tag_against_release_notes(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            notes_file = Path(temp) / "notes.md"
            notes = valid_notes()
            notes_file.write_text(notes, encoding="utf-8")
            commit = "a" * 40
            tree = "b" * 40
            with (
                mock.patch.object(
                    sys,
                    "argv",
                    [
                        "release_preflight.py",
                        "--tag",
                        TAG,
                        "--notes-file",
                        str(notes_file),
                        "--create-tag",
                    ],
                ),
                mock.patch.object(release_preflight, "ensure_clean"),
                mock.patch.object(release_preflight, "ensure_tag_absent"),
                mock.patch.object(
                    release_preflight,
                    "git_output",
                    side_effect=[commit, tree, tree, tree],
                ),
                mock.patch.object(release_preflight, "run_metadata_check"),
                mock.patch.object(
                    release_preflight,
                    "run",
                    return_value=subprocess.CompletedProcess([], 0, ""),
                ),
                mock.patch.object(release_preflight, "verify_created_tag") as verify,
            ):
                self.assertEqual(release_preflight.main(), 0)

        verify.assert_called_once_with(
            TAG,
            commit,
            f"Sub2API Plus {TAG}",
            notes,
        )


class ReleaseBaselineTests(unittest.TestCase):
    def test_finalization_regenerates_exact_tree_and_removes_temporary_worktree(self) -> None:
        current_tag = "v" + ROOT.joinpath("backend/cmd/server/VERSION").read_text().strip()
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            paths = [rule.path for rule in release_docs.DOCUMENT_RULES]
            paths.extend([
                "backend/cmd/server/VERSION", "UPSTREAM.md",
                "tools/update_release_docs.py", "tools/release_docs.py",
            ])
            for relative in paths:
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(ROOT.joinpath(relative).read_text(encoding="utf-8"), encoding="utf-8")
            upstream = root / "UPSTREAM.md"
            original = upstream.read_text(encoding="utf-8")
            planned = re.sub(
                rf"^(\|\s*`{re.escape(current_tag)}`.*\|\s*)published(\s*\|)$",
                r"\1planned\2", original, flags=re.MULTILINE,
            )
            upstream.write_text(planned, encoding="utf-8")

            def git(*args):
                return subprocess.check_output(
                    ["git", "-C", str(root), *args], text=True, stderr=subprocess.STDOUT
                ).strip()

            git("init", "--quiet")
            git("config", "user.email", "fixture@example.invalid")
            git("config", "user.name", "Release fixture")
            git("config", "commit.gpgsign", "false")
            git("add", ".")
            git("commit", "--quiet", "-m", "base fixture")
            base = git("rev-parse", "HEAD")
            upstream.write_text(release_finalization.replace_planned_mapping(planned, current_tag), encoding="utf-8")
            git("add", "UPSTREAM.md")
            git("commit", "--quiet", "-m", "finalization fixture")
            head = git("rev-parse", "HEAD")
            result = release_finalization.validate_finalization(
                root, base=base, head=head, expected_tag=current_tag,
                branch=release_finalization.finalization_branch(current_tag),
            )
            self.assertEqual(result.paths, frozenset({"UPSTREAM.md"}))
            self.assertEqual(git("status", "--porcelain"), "")
            self.assertEqual(git("worktree", "list", "--porcelain").count("worktree "), 1)
            root.joinpath("unexpected.txt").write_text("unrelated change")
            git("add", "unexpected.txt")
            git("commit", "--quiet", "-m", "unrelated change")
            with self.assertRaisesRegex(release_finalization.ReleaseFinalizationError, "not the deterministic"):
                release_finalization.validate_finalization(root, base=base, head=git("rev-parse", "HEAD"))
            self.assertEqual(git("worktree", "list", "--porcelain").count("worktree "), 1)

    def test_required_status_is_exact(self) -> None:
        errors: list[str] = []
        check_release.validate_required_status(TAG, "published", "planned", errors)
        self.assertEqual(len(errors), 1)
        self.assertIn("expected 'planned'", errors[0])

    def test_mapping_only_accepts_published_noncurrent_tag(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            root.joinpath("UPSTREAM.md").write_text(
                "| Custom Release | Official Release | Official Commit | Status |\n"
                "| --- | --- | --- | --- |\n"
                f"| `{TAG}` | `{OFFICIAL_TAG}` | `{OFFICIAL_COMMIT}` | published |\n",
                encoding="utf-8",
            )
            argv = [
                "check_release.py",
                "--tag",
                TAG,
                "--require-status",
                "published",
                "--mapping-only",
            ]
            with (
                mock.patch.object(check_release, "ROOT", root),
                mock.patch.object(sys, "argv", argv),
            ):
                self.assertEqual(check_release.main(), 0)


class MigrationBaselineTests(unittest.TestCase):
    def test_release_base_uses_previous_eligible_tag(self) -> None:
        tags = [
            "v1.2.3+custom.004",
            "v1.2.3+custom.005",
            "v1.2.3+custom.006",
        ]
        statuses = {
            "v1.2.3+custom.004": "published",
            "v1.2.3+custom.005": "historical",
            "v1.2.3+custom.006": "planned",
            TAG: "planned",
        }
        self.assertEqual(
            check_new_migrations.resolve_release_base(
                TAG,
                tags=tags,
                statuses=statuses,
            ),
            "v1.2.3+custom.005",
        )

    def test_release_base_is_required(self) -> None:
        with self.assertRaisesRegex(ValueError, "no earlier published or historical"):
            check_new_migrations.resolve_release_base(
                TAG,
                tags=[TAG],
                statuses={TAG: "planned"},
            )


class WorkflowProvenanceTests(unittest.TestCase):
    @staticmethod
    def run(
        workflow: str,
        *,
        branch: str = "main",
        sha: str = OFFICIAL_COMMIT,
        status: str = "completed",
        conclusion: str = "success",
    ) -> dict[str, object]:
        return {
            "workflowName": workflow,
            "event": "push",
            "headBranch": branch,
            "headSha": sha,
            "status": status,
            "conclusion": conclusion,
        }

    def test_exact_successful_main_runs_pass(self) -> None:
        runs = [self.run("CI"), self.run("Security Scan")]
        self.assertEqual(
            runs,
            workflow_provenance.require_successful_workflows(
                runs,
                branch="main",
                sha=OFFICIAL_COMMIT,
            ),
        )

    def test_wrong_branch_or_sha_does_not_satisfy_provenance(self) -> None:
        runs = [
            self.run("CI", branch="release/candidate"),
            self.run("Security Scan", sha="b" * 40),
        ]
        with self.assertRaisesRegex(
            workflow_provenance.WorkflowProvenanceError,
            "CI: missing; Security Scan: missing",
        ):
            workflow_provenance.require_successful_workflows(
                runs,
                branch="main",
                sha=OFFICIAL_COMMIT,
            )

    def test_missing_required_workflow_fails(self) -> None:
        with self.assertRaisesRegex(
            workflow_provenance.WorkflowProvenanceError,
            "Security Scan: missing",
        ):
            workflow_provenance.require_successful_workflows(
                [self.run("CI")],
                branch="main",
                sha=OFFICIAL_COMMIT,
            )

    def test_any_unsuccessful_exact_run_fails(self) -> None:
        runs = [
            self.run("CI"),
            self.run("CI", conclusion="failure"),
            self.run("Security Scan"),
        ]
        with self.assertRaisesRegex(
            workflow_provenance.WorkflowProvenanceError,
            "CI: completed/failure",
        ):
            workflow_provenance.require_successful_workflows(
                runs,
                branch="main",
                sha=OFFICIAL_COMMIT,
            )


class PublishedReleaseCheckTests(unittest.TestCase):
    def test_ci_wrapper_verifies_remote_tag_workflow_and_assets_read_only(self) -> None:
        published = mock.Mock(target=OFFICIAL_COMMIT)
        argv = [
            "check_published_release.py",
            "--repository",
            "LuckyKuang/sub2api-plus",
            "--tag",
            TAG,
        ]
        with (
            mock.patch.object(sys, "argv", argv),
            mock.patch.object(check_published_release.release_cli, "validate_tag") as validate,
            mock.patch.object(
                check_published_release.release_cli,
                "require_published_remote_tag",
                return_value=published,
            ) as remote_tag,
            mock.patch.object(
                check_published_release.release_cli,
                "require_release_workflow_success",
            ) as workflow,
            mock.patch.object(
                check_published_release.release_cli,
                "verify_release",
            ) as release,
            mock.patch.object(
                check_published_release.release_cli,
                "github_gate",
            ) as github_gate,
        ):
            self.assertEqual(check_published_release.main(), 0)

        validate.assert_called_once_with(TAG)
        remote_tag.assert_called_once_with("LuckyKuang/sub2api-plus", TAG)
        workflow.assert_called_once_with(
            "LuckyKuang/sub2api-plus",
            TAG,
            OFFICIAL_COMMIT,
        )
        release.assert_called_once_with("LuckyKuang/sub2api-plus", TAG)
        github_gate.assert_not_called()

class WorkflowPolicyTests(unittest.TestCase):
    def test_external_actions_are_pinned_to_commits(self) -> None:
        action_re = re.compile(r"^\s*uses:\s*([^@\s]+)@([^\s#]+)", re.MULTILINE)
        for path in sorted(ROOT.joinpath(".github/workflows").glob("*.yml")):
            with self.subTest(path=path.name):
                text = path.read_text(encoding="utf-8")
                floating = [
                    f"{action}@{revision}"
                    for action, revision in action_re.findall(text)
                    if not action.startswith("./")
                    and re.fullmatch(r"[0-9a-f]{40}", revision) is None
                ]
                self.assertEqual(floating, [])
                self.assertNotIn("@latest", text)

    def test_release_checkout_does_not_persist_credentials(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/release.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("persist-credentials: false", workflow)

    def test_release_pricing_assets_are_integrity_bound_and_not_replaced(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/release.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("Publish pricing release assets", workflow)
        self.assertIn("model-pricing.json", workflow)
        self.assertIn("model-pricing-manifest.json", workflow)
        self.assertIn("./cmd/pricing-manifest-build", workflow)
        self.assertIn("Refusing to replace immutable pricing asset", workflow)
        self.assertIn("name: release", workflow)
        self.assertIsNone(
            re.search(r"PRICING_MANIFEST_(?:SIGNING|PUBLIC)_KEY", workflow)
        )
        self.assertIsNone(
            re.search(r"model-pricing-manifest\.json\.sig", workflow)
        )
        self.assertNotIn("--clobber", workflow)

    def test_release_publish_is_automatic_only_after_verification(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/release.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("name: Build and publish", workflow)
        self.assertIn("needs: verify", workflow)
        self.assertIn("environment:\n      name: release", workflow)
        self.assertNotIn("required reviewers", workflow)

    def test_release_verifies_exact_main_workflow_provenance(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/release.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("name: Verify release provenance", workflow)
        self.assertIn("tools/workflow_provenance.py", workflow)
        self.assertIn("git merge-base --is-ancestor", workflow)
        self.assertNotIn("uses: ./.github/workflows/backend-ci.yml", workflow)
        self.assertRegex(
            workflow,
            r"verify:\n(?:.|\n)*?permissions:\n\s+actions: read\n\s+contents: read",
        )

    def test_every_required_remote_context_classifies_finalization_first(self) -> None:
        expected_jobs = {
            "backend-ci.yml": {
                "deployment-config",
                "test",
                "frontend",
                "golangci-lint",
                "goreleaser-config",
                "repository-policy",
            },
            "security-scan.yml": {"backend-security", "frontend-security"},
        }
        job_re = re.compile(
            r"^  (?P<job>[a-z0-9-]+):\n(?P<body>.*?)(?=^  [a-z0-9-]+:\n|\Z)",
            re.MULTILINE | re.DOTALL,
        )
        for workflow_name, required_jobs in expected_jobs.items():
            workflow = ROOT.joinpath(".github", "workflows", workflow_name).read_text(
                encoding="utf-8"
            )
            jobs = {match.group("job"): match.group("body") for match in job_re.finditer(workflow)}
            self.assertEqual(required_jobs - jobs.keys(), set())
            for job in sorted(required_jobs):
                with self.subTest(workflow=workflow_name, job=job):
                    body = jobs[job]
                    self.assertIn(
                        "uses: ./.github/actions/classify-release-finalization",
                        body,
                    )
                    self.assertIn("fetch-depth: 0", body)

    def test_finalization_repository_policy_rechecks_published_release(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/backend-ci.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("tools/check_published_release.py", workflow)
        self.assertIn("--require-status published", workflow)
        self.assertIn("--mapping-only", workflow)
        self.assertIn(
            "if: steps.validation-profile.outputs.profile == 'release-finalization'",
            workflow,
        )

    def test_security_scan_does_not_run_for_tag_pushes(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/security-scan.yml").read_text(
            encoding="utf-8"
        )
        self.assertRegex(workflow, r"push:\n\s+branches:\n\s+- \"\*\*\"")

    def test_actionlint_container_is_pinned_to_a_digest(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/backend-ci.yml").read_text(
            encoding="utf-8"
        )
        self.assertRegex(
            workflow,
            r"rhysd/actionlint:1\.7\.12@sha256:[0-9a-f]{64}",
        )

    def test_goreleaser_validation_uses_embedded_release_version(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/backend-ci.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn("backend/cmd/server/VERSION", workflow)
        self.assertIn(
            "DOCKER_TAG_VERSION: ${{ steps.tool-versions.outputs.docker_tag_version }}",
            workflow,
        )
        self.assertIsNone(
            re.search(
                r"DOCKER_TAG_VERSION:\s+v\d+\.\d+\.\d+-custom\.\d{3}",
                workflow,
            )
        )

    def test_repository_policy_runs_all_cli_self_tests(self) -> None:
        workflow = ROOT.joinpath(".github/workflows/backend-ci.yml").read_text(
            encoding="utf-8"
        )
        self.assertIn(
            "python skills/compress-cli/tests/test_compress_cli.py",
            workflow,
        )
        self.assertIn(
            "python skills/push-cli/tests/test_push_cli.py",
            workflow,
        )
        self.assertIn(
            "python skills/release-cli/tests/test_release_cli.py",
            workflow,
        )


class ReleaseTagTests(unittest.TestCase):
    def test_tag_creation_preserves_markdown_headings(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            notes_file = root / "release-notes.md"
            notes = valid_notes()
            notes_file.write_text(notes, encoding="utf-8")

            commands = (
                ("git", "init", "--quiet"),
                ("git", "config", "user.name", "Release Policy Test"),
                ("git", "config", "user.email", "release-policy@example.invalid"),
                ("git", "commit", "--allow-empty", "--quiet", "-m", "initial"),
                release_preflight.tag_creation_command(TAG, "HEAD", notes_file),
            )
            for command in commands:
                result = release_preflight.run(command, cwd=root, capture=True)
                self.assertEqual(
                    result.returncode,
                    0,
                    result.stderr or result.stdout,
                )

            result = release_preflight.run(
                (
                    "git",
                    "for-each-ref",
                    "--format=%(contents)",
                    f"refs/tags/{TAG}",
                ),
                cwd=root,
                capture=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), notes.strip())
            self.assertIn("## Highlights", result.stdout)
            self.assertIn("## Upstream baseline", result.stdout)


class ReleaseDocumentTests(unittest.TestCase):
    CURRENT = "v0.1.166+custom.008"
    ROLLBACK = "v0.1.166+custom.006"
    OLD = "v0.1.165+custom.004"

    @staticmethod
    def document_text(rule: release_docs.DocumentRule) -> str:
        lines = [
            *(
                f"install --version '{ReleaseDocumentTests.OLD}'"
                for _ in range(rule.install_commands)
            ),
            *(
                f"rollback '{ReleaseDocumentTests.OLD}'"
                for _ in range(rule.rollback_commands)
            ),
        ]
        old = ReleaseDocumentTests.OLD
        old_application = old.removeprefix("v")
        old_oci = old.replace("+", "-")
        current_value_fixtures = {
            "deploy/README.md": (
                f"Git/GitHub: {old}",
                f"GHCR: ghcr.io/luckykuang/sub2api-plus:{old_oci}",
            ),
            "deploy/DOCKER.md": (
                f"Immutable release, for example `{old_oci}`",
                f"Git/GitHub: {old}",
                f"GHCR: ghcr.io/luckykuang/sub2api-plus:{old_oci}",
            ),
            "deploy/APPLE_CONTAINER.md": (
                f"Git/GitHub: {old}",
                f"Application: {old_application}",
                f"Apple/OCI image: ghcr.io/luckykuang/sub2api-plus:{old_oci}",
                f"--build-arg VERSION={old_application} \\",
                f"--tag ghcr.io/luckykuang/sub2api-plus:{old_oci} \\",
                f"APPLE_CONTAINER_SUB2API_IMAGE=ghcr.io/luckykuang/sub2api-plus:{old_oci}",
            ),
            "deploy/.env.example": (
                f"this source revision is tagged sub2api-plus:{old_oci}; use that value",
            ),
            "UPSTREAM.md": (
                f"Git/GitHub: {old}",
                f"Application: {old_application}",
                f"GHCR: ghcr.io/luckykuang/sub2api-plus:{old_oci}",
            ),
        }
        lines.extend(current_value_fixtures.get(rule.path, ()))
        return "\n".join(lines) + "\n"

    @classmethod
    def upstream_rows(cls) -> str:
        return (
            "| Custom Release | Official Release | Official Commit | Status |\n"
            "| --- | --- | --- | --- |\n"
            f"| `{cls.ROLLBACK}` | `v0.1.166` | `{'a' * 40}` | published |\n"
            f"| `{cls.CURRENT}` | `v0.1.166` | `{'a' * 40}` | planned |\n"
        )

    def test_document_rollback_skips_invalid_iteration(self) -> None:
        tags = [
            "v0.1.166+custom.005",
            self.ROLLBACK,
            "v0.1.166+custom.007",
            self.CURRENT,
        ]
        statuses = {
            "v0.1.166+custom.005": "published",
            self.ROLLBACK: "published",
            "v0.1.166+custom.007": "invalid",
            self.CURRENT: "published",
        }
        self.assertEqual(
            release_docs.select_previous_release_tag(
                tags,
                statuses,
                self.CURRENT,
                eligible_statuses=release_docs.DOCUMENT_ROLLBACK_STATUSES,
            ),
            self.ROLLBACK,
        )

    def test_document_rollback_uses_only_published_releases(self) -> None:
        published = "v0.1.166+custom.003"
        tags = [
            published,
            "v0.1.166+custom.004",
            "v0.1.166+custom.005",
            self.ROLLBACK,
            "v0.1.166+custom.007",
        ]
        statuses = {
            published: "published",
            "v0.1.166+custom.004": "planned",
            "v0.1.166+custom.005": "withdrawn",
            self.ROLLBACK: "historical",
            "v0.1.166+custom.007": "invalid",
        }
        self.assertEqual(
            release_docs.select_previous_release_tag(
                tags,
                statuses,
                self.CURRENT,
                eligible_statuses=release_docs.DOCUMENT_ROLLBACK_STATUSES,
            ),
            published,
        )

    def test_rewrite_updates_every_expected_command_and_mapping(self) -> None:
        for rule in release_docs.DOCUMENT_RULES:
            with self.subTest(path=rule.path):
                updated = release_docs.rewrite_document(
                    rule,
                    self.document_text(rule),
                    self.CURRENT,
                    self.ROLLBACK,
                )
                self.assertEqual(
                    release_docs.INSTALL_COMMAND_RE.findall(updated),
                    [
                        ("install --version '", self.CURRENT, "'")
                        for _ in range(rule.install_commands)
                    ],
                )
                self.assertEqual(
                    release_docs.ROLLBACK_COMMAND_RE.findall(updated),
                    [
                        ("rollback '", self.ROLLBACK, "'")
                        for _ in range(rule.rollback_commands)
                    ],
                )
                for current_value in rule.current_values:
                    expected = release_docs._current_value(
                        self.CURRENT,
                        current_value.value_type,
                    )
                    self.assertEqual(
                        [
                            match[1]
                            for match in current_value.pattern.findall(updated)
                        ],
                        [expected] * current_value.expected_count,
                    )

    def test_rewrite_rejects_missing_and_duplicate_commands(self) -> None:
        rule = release_docs.DOCUMENT_RULES[0]
        valid = self.document_text(rule)
        missing = valid.replace(f"install --version '{self.OLD}'\n", "")
        duplicate = valid + f"rollback '{self.OLD}'\n"

        with self.assertRaisesRegex(
            release_docs.ReleaseDocsError,
            "has 0 install command",
        ):
            release_docs.rewrite_document(
                rule,
                missing,
                self.CURRENT,
                self.ROLLBACK,
            )
        with self.assertRaisesRegex(
            release_docs.ReleaseDocsError,
            "has 2 rollback command",
        ):
            release_docs.rewrite_document(
                rule,
                duplicate,
                self.CURRENT,
                self.ROLLBACK,
            )

    def test_generation_failure_leaves_all_documents_unchanged(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            version_path = root / "backend/cmd/server/VERSION"
            version_path.parent.mkdir(parents=True)
            version_path.write_text(
                self.CURRENT.removeprefix("v") + "\n",
                encoding="utf-8",
            )
            originals: dict[Path, str] = {}
            for rule in release_docs.DOCUMENT_RULES:
                path = root / rule.path
                path.parent.mkdir(parents=True, exist_ok=True)
                text = self.document_text(rule)
                if rule.path == "UPSTREAM.md":
                    text = self.upstream_rows() + text
                if rule.path == "deploy/README.md":
                    text += f"install --version '{self.OLD}'\n"
                path.write_text(text, encoding="utf-8")
                originals[path] = text

            with self.assertRaisesRegex(
                release_docs.ReleaseDocsError,
                "deploy/README.md has 3 install command",
            ):
                release_docs.generate_release_doc_updates(root)

            for path, original in originals.items():
                self.assertEqual(path.read_text(encoding="utf-8"), original)

    def test_release_check_reports_stale_files_and_update_command(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            version_path = root / "backend/cmd/server/VERSION"
            version_path.parent.mkdir(parents=True)
            version_path.write_text(
                self.CURRENT.removeprefix("v") + "\n",
                encoding="utf-8",
            )
            for rule in release_docs.DOCUMENT_RULES:
                path = root / rule.path
                path.parent.mkdir(parents=True, exist_ok=True)
                text = self.document_text(rule)
                if rule.path == "UPSTREAM.md":
                    text = self.upstream_rows() + text
                path.write_text(text, encoding="utf-8")

            errors: list[str] = []
            check_release.validate_release_documentation(root, errors)

            self.assertEqual(
                [error for error in errors if "stale release-version" in error],
                [
                    f"{Path(rule.path)} has stale release-version examples"
                    for rule in release_docs.DOCUMENT_RULES
                ],
            )
            self.assertEqual(errors[-1], "Run: python3 tools/update_release_docs.py")


if __name__ == "__main__":
    unittest.main()
