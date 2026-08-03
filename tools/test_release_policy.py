#!/usr/bin/env python3
"""Regression tests for release-policy helpers."""

from __future__ import annotations

import re
import tempfile
import unittest
from pathlib import Path

import check_release
import check_new_migrations
import release_docs
import release_preflight


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


class ReleaseBaselineTests(unittest.TestCase):
    def test_docker_tag_version_uses_oci_safe_separator(self) -> None:
        self.assertEqual(
            release_preflight.docker_tag_version("v0.1.170+custom.002"),
            "v0.1.170-custom.002",
        )

    def test_previous_release_uses_only_eligible_lower_versions(self) -> None:
        tags = [
            "v1.2.3+custom.004",
            "v1.2.3+custom.005",
            "v1.2.3+custom.006",
            "v1.2.3+custom.010",
            "v9.9.9+custom.999",
        ]
        statuses = {
            "v1.2.3+custom.004": "historical",
            "v1.2.3+custom.005": "published",
            "v1.2.3+custom.006": "planned",
            "v1.2.3+custom.010": "published",
        }
        self.assertEqual(
            release_preflight.select_previous_release_tag(tags, statuses, TAG),
            "v1.2.3+custom.005",
        )

    def test_required_status_is_exact(self) -> None:
        errors: list[str] = []
        check_release.validate_required_status(TAG, "published", "planned", errors)
        self.assertEqual(len(errors), 1)
        self.assertIn("expected 'planned'", errors[0])


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
                    f"{rule.path} has stale release-version examples"
                    for rule in release_docs.DOCUMENT_RULES
                ],
            )
            self.assertEqual(errors[-1], "Run: python3 tools/update_release_docs.py")


if __name__ == "__main__":
    unittest.main()
