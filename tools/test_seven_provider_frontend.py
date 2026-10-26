"""Requirement: missing/skipped frontend business cases cannot pass the gate."""
from __future__ import annotations

from pathlib import Path
import unittest
from unittest import mock

import check_seven_provider_frontend as gate

FRONTEND = Path("/isolated/frontend")


def report() -> dict:
    return {"success": True, "testResults": [
        {"name": str(FRONTEND / file), "status": "passed", "assertionResults": [
            {"fullName": title, "status": "passed"} for title in titles
        ]} for file, titles in gate.REQUIRED.items()
    ]}


class FrontendCoverageTests(unittest.TestCase):
    def test_start_plan_states_models_reauthorization_and_owner_fences_remain_required(self) -> None:
        expected = {
            *(f"ZhipuStartPlanCell shows the independent {state} state"
              for state in ("available", "unavailable", "pending", "unknown")),
            "ZhipuStartPlanCell distinguishes unknown model details from an explicit empty model set",
            "ZhipuStartPlanCell rejects a late same-ID reauthorization result and permits the current owner",
            "ZhipuStartPlanCell keeps a proven negative state after query failure and never promotes pending on time alone",
            "ZhipuStartPlanCell shows reauthorization for business login failure and ignores uncommitted observations",
            *(f"ZhipuStartPlanCell does not probe or render {plan} as Start"
              for plan in ("off-peak", "individual-coding-plan", "team-coding-plan")),
        }
        self.assertEqual(set(gate.REQUIRED["src/components/account/__tests__/ZhipuStartPlanCell.spec.ts"]), expected)

    def test_each_required_platform_and_lifecycle_case_must_execute(self) -> None:
        self.assertEqual(gate.coverage_errors(report(), frontend=FRONTEND), [])
        for index, titles in enumerate(gate.REQUIRED.values()):
            for missing in range(len(titles)):
                fixture = report()
                del fixture["testResults"][index]["assertionResults"][missing]
                self.assertTrue(any("missing required case" in error
                                    for error in gate.coverage_errors(fixture, frontend=FRONTEND)))

    def test_skipped_todo_failed_and_unfinished_cases_reject_successful_parent(self) -> None:
        for status in ("skipped", "pending", "todo", "failed", "running", None):
            fixture = report()
            fixture["testResults"][0]["assertionResults"][0]["status"] = status
            with self.subTest(status=status):
                self.assertTrue(gate.coverage_errors(fixture, frontend=FRONTEND))

    def test_other_file_or_duplicate_case_cannot_replace_missing_case(self) -> None:
        for change in ("other-file", "duplicate", "other-name"):
            fixture = report()
            first = fixture["testResults"][0]
            if change == "other-file":
                first["name"] = str(FRONTEND / "src/unrelated.spec.ts")
            elif change == "duplicate":
                first["assertionResults"].append(dict(first["assertionResults"][0]))
            else:
                first["assertionResults"][0]["fullName"] = "unrelated passing scenario"
            with self.subTest(change=change):
                self.assertTrue(gate.coverage_errors(fixture, frontend=FRONTEND))

    def test_no_tests_global_failure_and_process_failure_cannot_pass(self) -> None:
        self.assertTrue(gate.coverage_errors({}, frontend=FRONTEND))
        self.assertTrue(gate.coverage_errors({"success": True}, frontend=FRONTEND))
        self.assertTrue(gate.coverage_errors({**report(), "success": False}, frontend=FRONTEND))
        self.assertTrue(gate.coverage_errors(report(), frontend=FRONTEND, returncode=1))

    def test_container_guard_precedes_any_process_execution(self) -> None:
        with mock.patch.object(gate.validation_runtime, "require_in_validation", side_effect=RuntimeError("container required")), \
                mock.patch.object(gate.subprocess, "run") as run:
            with self.assertRaisesRegex(RuntimeError, "container required"):
                gate.run()
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
