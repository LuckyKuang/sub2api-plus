#!/usr/bin/env python3
"""Requirements: docs/CI_VALIDATION.md coverage, container boundary and profiles."""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import ci_validation
import validation_checks
import validation_runtime


ROOT = Path(__file__).resolve().parents[1]
BASE = "a" * 40


class FullCoverageTests(unittest.TestCase):
    def commands(self, lane: str) -> list[tuple[str, ...]]:
        return [tuple(step.command) for step in validation_checks.ci_steps(
            ROOT, lane, python="python3", base=BASE,
        )]

    def test_full_frontend_coverage_includes_test_suite_and_production_build(self) -> None:
        # Requirement: replacing local full must preserve full tests and build.
        self.assertEqual(self.commands("frontend"), [
            ("pnpm", "--dir", "frontend", "install", "--frozen-lockfile"),
            ("pnpm", "--dir", "frontend", "run", "lint:check"),
            ("pnpm", "--dir", "frontend", "run", "typecheck"),
            ("pnpm", "--dir", "frontend", "run", "test:run", "--maxWorkers=2"),
            ("pnpm", "--dir", "frontend", "run", "build"),
        ])
        self.assertFalse(any("test-frontend-critical" in command for command in self.commands("frontend")))

    def test_local_full_commands_all_have_ci_ownership(self) -> None:
        expected = {tuple(step.command) for step in validation_checks.full_steps(
            ROOT, python="python3", migration_base=BASE,
        )}
        actual = [command for lane in validation_checks.CI_STEP_NAMES
                  for command in self.commands(lane)]
        self.assertEqual(expected, set(actual))
        self.assertEqual(len(actual), len(set(actual)), "duplicate full-profile CI ownership")

    def test_full_backend_preserves_tidy_unit_integration_and_lint(self) -> None:
        self.assertEqual(self.commands("test-unit"),
                         [("go", "mod", "tidy", "-diff"), ("go", "test", "-tags=unit", "./...")])
        self.assertEqual(self.commands("test-integration"),
                         [("go", "test", "-tags=integration", "./...")])
        self.assertEqual(self.commands("golangci-lint"), [("golangci-lint", "run", "./...")])

    def test_ci_contains_previously_local_only_and_ci_only_policies(self) -> None:
        actual = {command for lane in validation_checks.CI_STEP_NAMES
                  for command in self.commands(lane)}
        for command in [
            ("bash", "-n", "deploy/install.sh"),
            ("sh", "deploy/tests/docker-compose-gateway-env-test.sh"),
            ("python3", "tools/test_test_build_tags.py"),
            ("python3", "tools/test_ci_validation.py"),
            ("python3", "tools/check_new_migrations.py", "--base", BASE),
        ]:
            self.assertIn(command, actual)


class ContainerAndProfileTests(unittest.TestCase):
    def test_no_host_execution_even_with_an_environment_flag(self) -> None:
        with mock.patch.dict(os.environ, {"SUB2API_IN_VALIDATION": "1"}), \
             mock.patch.object(validation_runtime, "in_validation_container", return_value=False), \
             mock.patch.object(ci_validation, "execute") as execute:
            with self.assertRaises(validation_runtime.ValidationRuntimeError):
                ci_validation.run_lane("frontend", base=BASE)
        execute.assert_not_called()

    def test_lane_failure_stops_remaining_checks(self) -> None:
        with mock.patch.object(validation_runtime, "require_in_validation"), \
             mock.patch.object(ci_validation, "execute", side_effect=RuntimeError("failed")) as execute:
            with self.assertRaisesRegex(RuntimeError, "failed"):
                ci_validation.run_lane("frontend", base=BASE)
        self.assertEqual(execute.call_count, 1)

    def test_invalid_base_stops_before_checks(self) -> None:
        with mock.patch.object(validation_runtime, "require_in_validation"), \
             mock.patch.object(ci_validation, "execute") as execute:
            with self.assertRaisesRegex(ValueError, "exact base SHA"):
                ci_validation.run_lane("frontend", base="main")
        execute.assert_not_called()

    def test_finalization_runs_only_its_metadata_gate(self) -> None:
        with mock.patch.object(validation_runtime, "require_in_validation"), \
             mock.patch.object(ci_validation, "execute") as execute:
            ci_validation.run_lane("finalization", base=BASE, tag="v1.2.3+custom.009")
        self.assertEqual(execute.call_count, 1)
        self.assertEqual(execute.call_args.args[1], [
            sys.executable, "tools/check_release.py", "--tag", "v1.2.3+custom.009",
            "--require-status", "published", "--mapping-only",
        ])

    def test_finalization_requires_published_tag_input(self) -> None:
        with mock.patch.object(validation_runtime, "require_in_validation"), \
             mock.patch.object(ci_validation, "execute") as execute:
            with self.assertRaisesRegex(ValueError, "published tag"):
                ci_validation.run_lane("finalization", base=BASE)
        execute.assert_not_called()

    def test_goreleaser_validation_derives_oci_tag_from_embedded_version(self) -> None:
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            version = root / "backend/cmd/server/VERSION"
            version.parent.mkdir(parents=True)
            version.write_text("1.2.3+custom.009\n")
            with mock.patch.object(ci_validation, "ROOT", root), \
                 mock.patch.object(validation_runtime, "require_in_validation"), \
                 mock.patch.object(ci_validation.subprocess, "run") as run:
                ci_validation.run_lane("goreleaser-config", base=BASE)
        self.assertEqual(run.call_args.args[0], ["goreleaser", "check"])
        env = run.call_args.kwargs["env"]
        self.assertEqual(env["DOCKER_TAG_VERSION"], "v1.2.3-custom.009")
        self.assertNotIn("DOCKERHUB_USERNAME", env)

    def test_release_specific_migration_base_remains_supported(self) -> None:
        with mock.patch.object(validation_runtime, "require_in_validation"), \
             mock.patch.object(ci_validation, "execute") as execute:
            ci_validation.run_lane("repository-policy", base=BASE, tag="v1.2.3+custom.009")
        commands = [call.args[1] for call in execute.call_args_list]
        self.assertIn([sys.executable, "tools/check_new_migrations.py",
                       "--target-release", "v1.2.3+custom.009"], commands)

    def test_generic_launch_uses_managed_container_cleanup(self) -> None:
        selected = validation_runtime.Runtime("apple-containers")
        with mock.patch.object(ci_validation, "selected_runtime", return_value=selected), \
             mock.patch.object(validation_runtime, "ensure_validation_image", return_value="image") as ensure, \
             mock.patch.object(validation_runtime, "launch_in_validation") as launch:
            ci_validation.launch(["python3", "tools/check_release.py"])
        self.assertFalse(ensure.call_args.kwargs["allow_build"])
        self.assertEqual(launch.call_args.args, (selected, ["python3", "tools/check_release.py"]))

    def test_docker_socket_is_not_enabled_outside_isolated_linux_ci(self) -> None:
        selected = validation_runtime.Runtime("apple-containers")
        def launch(_runtime, _argv, **kwargs):
            kwargs["run_step"]("In-container validation", ["container", "run", "image"])
        with mock.patch.object(ci_validation, "selected_runtime", return_value=selected), \
             mock.patch.object(validation_runtime, "ensure_validation_image", return_value="image"), \
             mock.patch.object(validation_runtime, "launch_in_validation", side_effect=launch), \
             mock.patch.object(ci_validation, "execute") as execute:
            with self.assertRaisesRegex(RuntimeError, "isolated GitHub Linux job"):
                ci_validation.launch(["go", "test"], integration=True)
        execute.assert_not_called()


class WorkflowCoverageTests(unittest.TestCase):
    def test_required_test_context_includes_both_architectures(self) -> None:
        workflow = (ROOT / ".github/workflows/backend-ci.yml").read_text()
        self.assertIn("runner: [ubuntu-24.04, ubuntu-24.04-arm]", workflow)
        self.assertIn("fail-fast: false", workflow)
        self.assertIn("needs: [test-unit, test-integration]", workflow)
        self.assertIn('test "$UNIT_RESULT" = success', workflow)
        self.assertIn('test "$INTEGRATION_RESULT" = success', workflow)

    def test_each_candidate_lane_prepares_and_classifies_before_checks(self) -> None:
        import re
        for filename, lanes in [
            ("backend-ci.yml", ("deployment-config", "test-unit", "test-integration",
                                "frontend", "golangci-lint", "goreleaser-config",
                                "repository-policy")),
            ("security-scan.yml", ("backend-security", "frontend-security")),
        ]:
            workflow = (ROOT / ".github/workflows" / filename).read_text()
            jobs = dict(re.findall(r"^  ([a-z0-9-]+):\n(.*?)(?=^  [a-z0-9-]+:\n|\Z)",
                                   workflow, re.M | re.S))
            self.assertIn("contents: read", workflow)
            self.assertNotIn("contents: write", workflow)
            self.assertNotIn("statuses: write", workflow)
            self.assertNotIn("checks: write", workflow)
            for lane in lanes:
                with self.subTest(workflow=filename, lane=lane):
                    body = jobs[lane]
                    prepare = body.index("uses: ./.github/actions/prepare-validation")
                    classify = body.index("uses: ./.github/actions/classify-release-finalization")
                    check = body.index("tools/ci_validation.py lane --lane " + lane)
                    self.assertLess(prepare, classify)
                    self.assertLess(classify, check)
                    self.assertIn("if: steps.validation-profile.outputs.profile == 'full'", body)
                    self.assertIn("persist-credentials: false", body)

    def test_classification_itself_runs_in_container(self) -> None:
        action = (ROOT / ".github/actions/classify-release-finalization/action.yml").read_text()
        self.assertIn("tools/ci_validation.py exec -- python3 tools/release_finalization.py classify", action)
        self.assertIn('trap \'rm -f "$result_file"\' EXIT', action)
        self.assertIn('cat "$result_file" >> "$GITHUB_OUTPUT"', action)

    def test_image_and_dependency_caches_use_exact_identities(self) -> None:
        action = (ROOT / ".github/actions/prepare-validation/action.yml").read_text()
        self.assertIn("steps.identity.outputs.image_generation", action)
        self.assertIn("steps.identity.outputs.cache_generation", action)
        self.assertIn("runner.arch", action.lower())
        self.assertNotIn("ghcr.io/", action)
        self.assertNotIn("docker push", action)
        self.assertIn("default: tools/ci_validation.py", action)
        self.assertIn('python3 "$VALIDATION_LAUNCHER" ensure', action)


if __name__ == "__main__":
    unittest.main()
