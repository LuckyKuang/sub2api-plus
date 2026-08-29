#!/usr/bin/env python3
"""Tests for the repository AGENTS.md validator."""

from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[3]
SCRIPT = ROOT / "skills/compress-cli/scripts/compress_cli.py"
SPEC = importlib.util.spec_from_file_location("compress_cli", SCRIPT)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError(f"cannot load {SCRIPT}")
compress_cli = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(compress_cli)


class CompressCliTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.valid_document = ROOT.joinpath("AGENTS.md").read_text(encoding="utf-8")

    def validate_text(self, content: str) -> list[str]:
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "AGENTS.md"
            path.write_text(content, encoding="utf-8")
            return compress_cli.validate_agents(path, repo_root=ROOT)

    def assert_error_contains(self, errors: list[str], expected: str) -> None:
        self.assertTrue(
            any(expected in error for error in errors),
            f"expected {expected!r} in errors: {errors}",
        )

    def test_current_agents_document_passes(self) -> None:
        self.assertEqual([], self.validate_text(self.valid_document))

    def test_cli_check_is_read_only_and_passes_current_document(self) -> None:
        before = ROOT.joinpath("AGENTS.md").read_bytes()
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "check", "AGENTS.md"],
            cwd=ROOT,
            check=False,
            text=True,
            encoding="utf-8",
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
        self.assertEqual(0, result.returncode, result.stdout)
        self.assertEqual(before, ROOT.joinpath("AGENTS.md").read_bytes())

    def test_missing_security_audit_category_fails(self) -> None:
        changed = "\n".join(
            line
            for line in self.valid_document.splitlines()
            if not line.startswith("|Security Audit:")
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "missing required category 'Security Audit'")

    def test_successful_sibling_remains_auditable(self) -> None:
        changed = self.valid_document.replace(
            "Successfully extracted sibling content remains auditable",
            "Extracted sibling content may be ignored",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(
            errors,
            "Successfully extracted sibling content remains auditable",
        )

    def test_extraction_failure_must_pass_through(self) -> None:
        changed = self.valid_document.replace(
            "pass through without an audit-derived block",
            "fail closed whenever blocking mode is active",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(
            errors,
            "pass through without an audit-derived block",
        )

    def test_extraction_failure_cannot_become_unavailable(self) -> None:
        changed = self.valid_document.replace(
            "must not become policy violations, unavailable decisions, HTTP 503 responses, or WebSocket closes",
            "may become an unavailable decision",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(
            errors,
            "must not become policy violations, unavailable decisions",
        )

    def test_audit_exceptions_require_safe_structured_logs(self) -> None:
        changed = self.valid_document.replace(
            "Every extraction, evaluation, or audit-dependency exception must emit a structured log",
            "Audit exceptions may be logged",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(
            errors,
            "Every extraction, evaluation, or audit-dependency exception must emit a structured log",
        )

    def test_account_session_routing_cannot_bypass_audit(self) -> None:
        changed = self.valid_document.replace(
            "routing, retries, probes, protocol adapters, transforms, request classification, and upstream merges must not bypass this boundary",
            "routing behavior is implementation-defined",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(
            errors,
            "routing, retries, probes, protocol adapters, transforms",
        )

    def test_codex_identity_precedence_cannot_be_removed(self) -> None:
        changed = self.valid_document.replace(
            "credentials.user_agent > valid global openai_codex_user_agent > compiled default",
            "a configured identity",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "credentials.user_agent")

    def test_codex_version_sync_cannot_change_identity_fingerprint(self) -> None:
        changed = self.valid_document.replace(
            "Version sync may update only selected identity version declarations and must not change source, client family, Originator, OS, architecture, or terminal fingerprint",
            "Version sync may replace the selected identity",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "terminal fingerprint")

    def test_openspec_plans_remain_local_and_untracked(self) -> None:
        changed = self.valid_document.replace(
            "Do not commit openspec/changes/",
            "Commit completed OpenSpec changes",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "Do not commit openspec/changes/")

    def test_openspec_durable_behavior_has_a_tracked_owner(self) -> None:
        changed = self.valid_document.replace(
            "Commit durable behavior to the owning documentation and tests",
            "Use local plans as the durable record",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "owning documentation and tests")

    def test_release_finalization_profile_cannot_lose_deterministic_gate(self) -> None:
        changed = self.valid_document.replace(
            "Only a verified published tag on its deterministic finalization tree may use release-finalization",
            "Finalization may use a smaller check set",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "deterministic finalization tree")

    def test_tag_workflow_must_reuse_exact_main_evidence(self) -> None:
        changed = self.valid_document.replace(
            "The tag workflow must reuse that exact evidence rather than rerun the application matrix",
            "The tag workflow validates the release",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(
            errors,
            "reuse that exact evidence rather than rerun the application matrix",
        )

    def test_unknown_source_path_fails(self) -> None:
        changed = self.valid_document.replace(
            "Go=backend/go.mod",
            "Go=backend/missing-go.mod",
        )
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "path does not exist: backend/missing-go.mod")

    def test_duplicate_category_fails(self) -> None:
        changed = self.valid_document + "\n|Secrets:Duplicate rule.\n"
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "duplicate category 'Secrets'")

    def test_non_index_line_fails(self) -> None:
        changed = self.valid_document + "\nThis is not an index line.\n"
        errors = self.validate_text(changed)
        self.assert_error_contains(errors, "must start with '|'")

    def test_document_is_not_rejected_for_exceeding_35_lines(self) -> None:
        extras = "\n".join(
            f"|Additional Rule {index}:Project-specific rule {index}."
            for index in range(1, 12)
        )
        changed = self.valid_document.rstrip() + "\n" + extras + "\n"
        self.assertGreater(len(changed.splitlines()), 35)
        self.assertEqual([], self.validate_text(changed))


if __name__ == "__main__":
    unittest.main()
