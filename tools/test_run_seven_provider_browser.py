from __future__ import annotations

import os
from copy import deepcopy
from pathlib import Path
import unittest
from unittest import mock

import run_seven_provider_browser as browser


class BrowserCoverageTests(unittest.TestCase):
    def report(self) -> dict:
        return {"suites": [{"suites": [{"specs": [
            {"title": title, "tests": [{
                "projectName": "chromium", "expectedStatus": "passed", "status": "expected",
                "results": [{"status": "passed"}],
            }]}
            for title in browser.REQUIRED_MANAGEMENT_TITLES
        ]}]}], "errors": []}

    def test_all_seven_platforms_and_both_account_types_must_execute(self) -> None:
        self.assertEqual(len(browser.REQUIRED_MANAGEMENT_TITLES), 14)
        self.assertEqual(browser.browser_coverage_errors(self.report()), [])
        for missing in range(14):
            report = self.report()
            del report["suites"][0]["suites"][0]["specs"][missing]
            with self.subTest(missing=missing):
                self.assertTrue(any("missing browser scenario" in error
                                    for error in browser.browser_coverage_errors(report)))

    def test_skipped_expected_failure_retries_and_other_browsers_cannot_pass(self) -> None:
        valid = self.report()
        for mutation in (
            {"status": "skipped", "results": [{"status": "skipped"}]},
            {"status": "unexpected", "results": [{"status": "failed"}]},
            {"expectedStatus": "failed", "results": [{"status": "failed"}]},
            {"results": []},
            {"results": [{"status": "failed"}, {"status": "passed"}]},
            {"projectName": "firefox"},
        ):
            report = deepcopy(valid)
            report["suites"][0]["suites"][0]["specs"][0]["tests"][0].update(mutation)
            with self.subTest(mutation=mutation):
                self.assertTrue(browser.browser_coverage_errors(report))

    def test_missing_empty_duplicate_and_global_errors_fail(self) -> None:
        self.assertTrue(browser.browser_coverage_errors({}))
        for change in ("tests", "duplicate", "global", "title"):
            report = self.report()
            specs = report["suites"][0]["suites"][0]["specs"]
            if change == "tests":
                specs[0]["tests"] = []
            elif change == "duplicate":
                specs.append(deepcopy(specs[0]))
            elif change == "global":
                report["errors"] = [{"message": "worker crashed"}]
            else:
                specs[0]["title"] = "unrelated scenario"
            with self.subTest(change=change):
                self.assertTrue(browser.browser_coverage_errors(report))

    def test_focused_filter_requires_its_selected_case_and_rejects_empty_selection(self) -> None:
        report = self.report()
        report["suites"][0]["suites"][0]["specs"] = report["suites"][0]["suites"][0]["specs"][:1]
        self.assertEqual(browser.browser_coverage_errors(report, grep="openai apikey"), [])
        self.assertTrue(browser.browser_coverage_errors(report, grep="unimplemented-provider"))


class BrowserEnvironmentTests(unittest.TestCase):
    def setUp(self) -> None:
        self.source = {
            "SUB2API_E2E_ISOLATED": "1",
            "SUB2API_TEST_POSTGRES_DSN": "postgres://browser:fixture-password@disposable-postgres:5433/integration?sslmode=disable",
            "SUB2API_TEST_REDIS_ADDR": "disposable-redis:6380",
            "SUB2API_TEST_REDIS_DB": "13",
            "SUB2API_TEST_REDIS_PASSWORD": "fixture-redis-password",
            "SUB2API_TEST_REDIS_USERNAME": "browser-worker",
            "SUB2API_IN_VALIDATION": "1",
        }

    def test_real_application_uses_fresh_database_and_explicit_test_services(self) -> None:
        environment = browser.isolated_environment(self.source, "sub2api_browser_0123456789abcdef", Path("/tmp/disposable-browser"))
        self.assertEqual(environment["DATABASE_DBNAME"], "sub2api_browser_0123456789abcdef")
        self.assertEqual(environment["DATABASE_HOST"], "disposable-postgres")
        self.assertEqual(environment["DATABASE_PORT"], "5433")
        self.assertEqual(environment["DATABASE_PASSWORD"], "fixture-password")
        self.assertEqual(environment["REDIS_HOST"], "disposable-redis")
        self.assertEqual(environment["REDIS_PORT"], "6380")
        self.assertEqual(environment["REDIS_DB"], "13")
        self.assertEqual(environment["REDIS_USERNAME"], "browser-worker")
        self.assertEqual(environment["REDIS_PASSWORD"], "fixture-redis-password")
        self.assertEqual(environment["DATA_DIR"], "/tmp/disposable-browser/data")
        self.assertEqual(environment["SSL_CERT_FILE"], "/tmp/disposable-browser/ca.pem")
        self.assertIn("/sub2api_browser_0123456789abcdef?", environment["SUB2API_TEST_POSTGRES_DSN"])
        self.assertEqual(environment["ADMIN_PASSWORD"], environment["SUB2API_E2E_ADMIN_PASSWORD"])
        self.assertGreaterEqual(len(environment["JWT_SECRET"]), 64)

    def test_ambient_runtime_credentials_and_global_proxy_cannot_redirect_fixture(self) -> None:
        self.source.update({
            "DATABASE_HOST": "production-db", "DATABASE_DBNAME": "production",
            "ADMIN_PASSWORD": "production-password", "REDIS_HOST": "production-cache",
            "CONFIG_PATH": "/production/config.yaml", "CONFIG_FILE": "/production/config.yaml",
            "HTTP_PROXY": "http://production-proxy:8080", "https_proxy": "http://production-proxy:8080",
        })
        before = dict(self.source)
        environment = browser.isolated_environment(self.source, "sub2api_browser_0123456789abcdef", Path("/tmp/disposable-browser"))
        self.assertEqual(self.source, before)
        self.assertEqual(environment["DATABASE_HOST"], "disposable-postgres")
        self.assertEqual(environment["REDIS_HOST"], "disposable-redis")
        self.assertNotEqual(environment["ADMIN_PASSWORD"], "production-password")
        for key in ("HTTP_PROXY", "https_proxy", "CONFIG_FILE", "CONFIG_PATH"):
            self.assertNotIn(key, environment)

    def test_missing_isolation_or_explicit_service_configuration_fails(self) -> None:
        for key in ("SUB2API_E2E_ISOLATED", "SUB2API_TEST_POSTGRES_DSN", "SUB2API_TEST_REDIS_ADDR", "SUB2API_TEST_REDIS_DB"):
            with self.subTest(key=key):
                source = dict(self.source)
                del source[key]
                with self.assertRaises(ValueError):
                    browser.isolated_environment(source, "sub2api_browser_0123456789abcdef", Path("/tmp/disposable-browser"))
        for invalid_database in ("", "0", "-1", "not-a-database"):
            with self.subTest(redis_db=invalid_database):
                source = {**self.source, "SUB2API_TEST_REDIS_DB": invalid_database}
                with self.assertRaisesRegex(ValueError, "nonzero isolated logical database"):
                    browser.isolated_environment(source, "sub2api_browser_0123456789abcdef", Path("/tmp/disposable-browser"))

    def test_host_guard_rejects_before_builds_or_processes_start(self) -> None:
        with mock.patch.object(browser.validation_runtime, "require_in_validation", side_effect=RuntimeError("container required")) as guard, \
                mock.patch.object(browser.subprocess, "run") as run, \
                mock.patch.object(browser.subprocess, "Popen") as launch, \
                mock.patch.dict(os.environ, self.source, clear=True), \
                mock.patch("sys.argv", ["run_seven_provider_browser.py"]):
            with self.assertRaisesRegex(RuntimeError, "container required"):
                browser.main()
        guard.assert_called_once_with(tool="seven-provider real browser regression")
        run.assert_not_called()
        launch.assert_not_called()

    def test_non_root_validation_lane_rejects_before_builds(self) -> None:
        with mock.patch.object(browser.validation_runtime, "require_in_validation"), \
                mock.patch.object(browser.os, "geteuid", return_value=501), \
                mock.patch.object(browser.subprocess, "run") as run, \
                mock.patch.dict(os.environ, self.source, clear=True), \
                mock.patch("sys.argv", ["run_seven_provider_browser.py"]):
            with self.assertRaisesRegex(ValueError, "must run as root"):
                browser.main()
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
