#!/usr/bin/env python3
from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import check_test_build_tags


class TestBuildTagPolicyTests(unittest.TestCase):
    def test_allows_known_constraints(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / "ok_test.go").write_text(
                "//go:build unit\n\npackage demo\n\nfunc TestOK(t *testing.T) {}\n",
                encoding="utf-8",
            )
            (root / "default_lint_test.go").write_text(
                "//go:build unit || !integration\n\npackage demo\n\nfunc TestDefault(t *testing.T) {}\n",
                encoding="utf-8",
            )
            (root / "shared_helpers_test.go").write_text(
                "//go:build !e2e\n\npackage demo\n",
                encoding="utf-8",
            )
            self.assertEqual([], check_test_build_tags.violations_for(root))

    def test_rejects_missing_and_unknown_constraints(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / "missing_test.go").write_text(
                "package demo\n\nfunc TestMissing(t *testing.T) {}\n",
                encoding="utf-8",
            )
            (root / "weird_test.go").write_text(
                "//go:build custom\n\npackage demo\n\nfunc TestWeird(t *testing.T) {}\n",
                encoding="utf-8",
            )
            problems = check_test_build_tags.violations_for(root)
        self.assertEqual(
            [
                "missing_test.go: missing //go:build constraint",
                "weird_test.go: unsupported //go:build custom",
            ],
            problems,
        )


if __name__ == "__main__":
    unittest.main()
