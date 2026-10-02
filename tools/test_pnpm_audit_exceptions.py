#!/usr/bin/env python3
"""Regression tests for the pnpm audit exception checker.

Covers the three exit paths that guard the risk-acceptance ledger:

* a reported high/critical advisory with no matching exception (missing),
* a matched exception whose ``expires_on`` is in the past (expired),
* an exception that matches no reported advisory (unused/rotted).

The ``pnpm-audit-xlsx-0.18.5.json`` fixture is the pre-change production audit
report captured from the tracked ``frontend/audit.json`` before that stale
artifact was removed: both vendored-``xlsx`` advisories are reported and no
``pnpm.auditConfig.ignoreCves`` filtering is applied.
"""

from __future__ import annotations

import contextlib
import datetime as dt
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path


TOOLS = Path(__file__).resolve().parent
FIXTURE = TOOLS / "fixtures" / "pnpm-audit-xlsx-0.18.5.json"

# The two advisories recorded in the captured pre-change report.
XLSX_ADVISORIES = (
    ("GHSA-4r6h-8v6p-xvw6", "CVE-2023-30533"),
    ("GHSA-5pgg-2g8v-p4x9", "CVE-2024-22363"),
)


def load_checker():
    spec = importlib.util.spec_from_file_location(
        "check_pnpm_audit_exceptions",
        TOOLS / "check_pnpm_audit_exceptions.py",
    )
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


checker = load_checker()


def exception_entry(advisory: str, expires_on: str) -> str:
    return (
        f"  - package: xlsx\n"
        f'    advisory: "{advisory}"\n'
        f"    severity: high\n"
        f'    reason: "write-only export"\n'
        f'    mitigation: "no untrusted spreadsheet input"\n'
        f'    expires_on: "{expires_on}"\n'
        f'    owner: "security@example.invalid"\n'
    )


class AuditExceptionCheckerTests(unittest.TestCase):
    def run_checker(
        self,
        *,
        audit: dict | str,
        ledger: str,
        today: dt.date = dt.date(2026, 10, 2),
    ) -> tuple[int, str, str]:
        """Run the checker against in-memory fixtures and capture its output."""
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            audit_path = root / "audit.json"
            exceptions_path = root / "audit-exceptions.yml"
            if isinstance(audit, str):
                audit_path.write_text(audit, encoding="utf-8")
            else:
                audit_path.write_text(json.dumps(audit), encoding="utf-8")
            exceptions_path.write_text(ledger, encoding="utf-8")

            stdout = io.StringIO()
            stderr = io.StringIO()
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                exit_code = checker.main(
                    today=today,
                    argv=[
                        "--audit",
                        str(audit_path),
                        "--exceptions",
                        str(exceptions_path),
                    ],
                )
        return exit_code, stdout.getvalue(), stderr.getvalue()

    def test_pre_change_report_without_exceptions_lists_both_xlsx_advisories(self) -> None:
        # 5.3(a): 未过滤的变更前报告 + 空清单必须失败，并列出两条 xlsx advisory。
        audit = json.loads(FIXTURE.read_text(encoding="utf-8"))
        self.assertEqual(2, audit["metadata"]["vulnerabilities"]["high"])
        self.assertEqual(set(), set(audit.get("muted") or []))

        reported = {
            advisory["github_advisory_id"]: advisory["cves"][0]
            for advisory in audit["advisories"].values()
            if advisory["severity"] in checker.HIGH_SEVERITIES
        }
        self.assertEqual(
            {ghsa: cve for ghsa, cve in XLSX_ADVISORIES},
            reported,
        )

        exit_code, _, stderr = self.run_checker(
            audit=audit,
            ledger="version: 1\nexceptions: []\n",
        )

        self.assertEqual(1, exit_code)
        self.assertIn("High/Critical vulnerabilities missing exceptions:", stderr)
        for ghsa, cve in XLSX_ADVISORIES:
            self.assertIn(f"xlsx (high) [{ghsa}]", stderr)
            self.assertIn(cve, FIXTURE.read_text(encoding="utf-8"))
        self.assertNotIn("Exceptions that match no reported advisory:", stderr)

    def test_matching_exceptions_with_past_expiry_fail(self) -> None:
        # 5.3(b): 包名 + advisory 完全匹配但 expires_on 已过期必须失败。
        audit = json.loads(FIXTURE.read_text(encoding="utf-8"))
        ledger = "version: 1\nexceptions:\n" + "".join(
            exception_entry(ghsa, "2026-01-01") for ghsa, _ in XLSX_ADVISORIES
        )

        exit_code, _, stderr = self.run_checker(
            audit=audit,
            ledger=ledger,
            today=dt.date(2026, 10, 2),
        )

        self.assertEqual(1, exit_code)
        self.assertIn("Exceptions expired:", stderr)
        for ghsa, _ in XLSX_ADVISORIES:
            self.assertIn(f"xlsx (high) [{ghsa}] expired on 2026-01-01", stderr)
        self.assertNotIn("missing exceptions", stderr)
        self.assertNotIn("match no reported advisory", stderr)

    def test_matching_exceptions_before_expiry_pass(self) -> None:
        audit = json.loads(FIXTURE.read_text(encoding="utf-8"))
        ledger = "version: 1\nexceptions:\n" + "".join(
            exception_entry(ghsa, "2026-10-06") for ghsa, _ in XLSX_ADVISORIES
        )

        exit_code, stdout, stderr = self.run_checker(
            audit=audit,
            ledger=ledger,
            today=dt.date(2026, 10, 2),
        )

        self.assertEqual(0, exit_code, stderr)
        self.assertEqual("Audit exceptions validated.\n", stdout)
        self.assertEqual("", stderr)

    def test_exception_matching_no_reported_advisory_fails(self) -> None:
        # 清单腐化：advisory 已修复，旧例外必须删除，不能静默通过。
        audit = {"advisories": {}, "metadata": {"vulnerabilities": {"high": 0}}}
        ledger = "version: 1\nexceptions:\n" + exception_entry(
            "GHSA-4r6h-8v6p-xvw6", "2026-10-06"
        )

        exit_code, _, stderr = self.run_checker(
            audit=audit,
            ledger=ledger,
            today=dt.date(2026, 10, 2),
        )

        self.assertEqual(1, exit_code)
        self.assertIn("Exceptions that match no reported advisory:", stderr)
        self.assertIn("xlsx [GHSA-4r6h-8v6p-xvw6]", stderr)
        self.assertNotIn("Exceptions expired:", stderr)

    def test_empty_ledger_and_clean_audit_pass(self) -> None:
        # 5.4: 变更后的空清单 + 干净审计必须通过并打印成功信息。
        audit = {"advisories": {}, "metadata": {"vulnerabilities": {"high": 0}}}

        exit_code, stdout, stderr = self.run_checker(
            audit=audit,
            ledger="version: 1\nexceptions: []\n",
        )

        self.assertEqual(0, exit_code, stderr)
        self.assertEqual("Audit exceptions validated.\n", stdout)
        self.assertEqual("", stderr)


if __name__ == "__main__":
    unittest.main()
