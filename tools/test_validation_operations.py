"""Release audit regressions: ownership, lifecycle, install and live output."""

import importlib.util
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import validation_runtime as runtime
import validation_output

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("operations_push_cli", ROOT / "skills/push-cli/scripts/push_cli.py")
push = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = push
spec.loader.exec_module(push)


class OperationsTests(unittest.TestCase):
    def test_unicode_progress_survives_a_windows_legacy_console(self):
        raw = io.BytesIO()
        console = io.TextIOWrapper(raw, encoding="gbk")
        with mock.patch.object(sys, "stdout", console), mock.patch.object(sys, "stderr", console):
            validation_output.configure_console()
            print("progress\u2009→ 中文", flush=True)
        self.assertEqual("progress\u2009→ 中文\n", raw.getvalue().decode("utf-8"))
        console.detach()

    def test_wsl_dependency_mounts_use_persistent_linux_storage(self):
        # /tmp disappeared between real helper invocations on the workstation;
        # a later container could no longer find the installed vitest binary.
        selected = runtime.Runtime("wsl2-docker", ("fake-wsl",), "/mnt/c/source")
        commands = []
        def capture(command):
            commands.append(command)
            return "/home/fixture" if command[-2:] == ["printenv", "HOME"] else ""
        mounts = runtime.cache_mounts(selected, root=ROOT, generation="a" * 16, capture=capture)
        self.assertEqual(3, len(mounts))
        for source, _ in mounts:
            self.assertTrue(source.startswith("/home/fixture/.cache/sub2api-validation/" + "a" * 16 + "/"))
        self.assertIn(("/home/fixture/.cache/sub2api-validation/" + "a" * 16 + "/frontend-node-modules",
                       "/mnt/c/source/frontend/node_modules"), mounts)
    def test_utf16_wsl_output_is_decoded_before_token_parsing(self):
        value = "名称 状态 版本\r\n* Ubuntu Running 2\r\n"
        self.assertEqual(value, runtime.decode_output(value.encode("utf-16-le")))
        self.assertEqual(value, runtime.decode_output(value.encode("utf-16")))
        self.assertEqual("中文", runtime.decode_output("中文".encode()))

    def test_build_proxy_values_never_enter_arguments(self):
        with mock.patch.dict(os.environ, {"HTTPS_PROXY": "http://user:private@proxy.invalid:3128",
                                         "SUB2API_VALIDATION_WSL_NETWORK": "host"}, clear=True):
            command = runtime.image_build_command(runtime.Runtime("wsl2-docker", ("wsl", "--exec"), "/repo"),
                                                  ROOT, "sub2api-validation:fixture")
        self.assertIn("HTTPS_PROXY", command)
        self.assertNotIn("private", " ".join(command))
        self.assertEqual("host", command[command.index("--network") + 1])

    def test_failed_run_keeps_a_content_free_durable_summary(self):
        import json
        with tempfile.TemporaryDirectory() as directory:
            with mock.patch.dict(os.environ, {"SUB2API_VALIDATION_REPORT_DIR": directory,
                                             "HTTPS_PROXY": "http://user:private@proxy.invalid"}):
                with self.assertRaisesRegex(RuntimeError, "fixture failure"):
                    with validation_output.report_scope(ROOT, base="HEAD"):
                        validation_output.ACTIVE_REPORT.step("Frontend tests", "frontend", 1.5, 7, 0.2)
                        raise RuntimeError("fixture failure")
            paths = list(Path(directory).glob("*.json"))
            self.assertEqual(1, len(paths))
            report = json.loads(paths[0].read_text())
            self.assertEqual("failed", report["status"])
            self.assertEqual(7, report["steps"][0]["returncode"])
            self.assertEqual(0.2, report["steps"][0]["queued_seconds"])
            self.assertNotIn("private", paths[0].read_text())
            self.assertNotIn("stdout", report)
            self.assertFalse(list(Path(directory).glob("*.tmp")))

    def test_production_build_forces_fresh_compiler_state(self):
        # The release audit reproduced an OOM hidden by an incremental build.
        # Dependency caches may survive; the production compiler must run again.
        import json
        package = json.loads((ROOT / "frontend/package.json").read_text())
        self.assertIn("vue-tsc -b --force", package["scripts"]["build"])

    def test_changed_pr_inputs_cannot_publish_verified_proof(self):
        import json
        proof = push.ValidationProof("a" * 40, "b" * 40)
        valid = {"state": "open", "head": {"sha": proof.head},
                 "base": {"sha": proof.base}, "body": push.validation_marker(proof)}
        for changed in ({"head": {"sha": "c" * 40}}, {"base": {"sha": "d" * 40}},
                        {"body": "missing"}, {"body": valid["body"] * 2},
                        {"state": "closed", "merged": False}):
            with self.subTest(changed=changed), mock.patch.object(
                push, "capture", return_value=json.dumps({**valid, **changed})
            ):
                with self.assertRaises(push.PushCliError):
                    push.verify_pull_request_proof("LuckyKuang/sub2api-plus",
                                                  "https://github.com/LuckyKuang/sub2api-plus/pull/22", proof)

    def test_owned_session_ends_on_success_failure_and_interrupt(self):
        # A foreground stdin lease has an owner and closes through every exit.
        original = subprocess.Popen
        for error in (None, RuntimeError("failure"), KeyboardInterrupt()):
            processes = []
            def start(*args, **kwargs):
                process = original(*args, **kwargs)
                processes.append(process)
                return process
            try:
                with mock.patch.object(runtime.subprocess, "Popen", side_effect=start):
                    with runtime.runtime_session(runtime.Runtime("wsl2-docker")):
                        if error:
                            raise error
            except (RuntimeError, KeyboardInterrupt):
                pass
            self.assertEqual(1, len(processes))
            self.assertIsNotNone(processes[0].poll(), "session process leaked")
            self.assertTrue(processes[0].stdin.closed)

    def test_cache_ownership_never_comes_from_age_size_or_generic_workdir(self):
        base, apt, tools, browser, final, other = (character * 25 for character in "abcdef")
        records = [
            {"ID": base, "Description": "pulled from debian", "Parents": []},
            {"ID": apt, "Description": "RUN apt install", "Parents": [base]},
            {"ID": tools, "Description": "RUN printf marker >/etc/sub2api-validation", "Parents": [apt]},
            {"ID": browser, "Description": "RUN install /opt/sub2api-playwright", "Parents": [tools]},
            {"ID": final, "Description": "[6/6] WORKDIR /workspace", "Parents": [browser]},
            {"ID": other, "Description": "RUN unrelated application", "Parents": [tools]},
            {"ID": "z" * 25, "Description": "WORKDIR /workspace", "Parents": []},
        ]
        self.assertEqual(sorted([apt, tools, browser, final]), runtime.validation_build_cache_ids(records))
        self.assertEqual([], runtime.validation_build_cache_ids(records[-2:]))

    def test_download_failure_aborts_actual_validation_image_install_step(self):
        # A failed curl on the left of `curl | sh` used to be hidden by sh's
        # successful exit. Exercise the Dockerfile's real shell block offline.
        text = (ROOT / "deploy/Dockerfile.validation").read_text()
        script = "set -eu; " + text.split("RUN set -eu; \\\n", 1)[1].split("# Playwright", 1)[0]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            marker = root / "installed-marker"
            script = script.replace("/etc/sub2api-validation", str(marker))
            for name in ("tar", "ln", "corepack", "go", "python3", "rm", "mkdir", "chmod", "sh"):
                executable = root / name
                executable.write_text("#!/bin/sh\nexit 0\n")
                executable.chmod(0o755)
            curl = root / "curl"
            curl.write_text('''#!/bin/sh
case "$*" in
  *golangci-lint*) exit 35 ;;
  *) exit 0 ;;
esac
''')
            curl.chmod(0o755)
            environment = dict(os.environ, PATH=str(root), TARGETARCH="amd64",
                               **runtime.declared_validation_pins(ROOT))
            original = ('set -eu; curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh '
                        '| sh -s -- -b /usr/local/bin "v${GOLANGCI_LINT_VERSION}"; '
                        f"printf complete >{marker}")
            control = subprocess.run(["/bin/sh", "-c", original], env=environment,
                                     stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            self.assertEqual(0, control.returncode, "original pipeline no longer reproduces the masked failure")
            self.assertTrue(marker.exists())
            marker.unlink()
            result = subprocess.run(["/bin/sh", "-c", script], env=dict(os.environ, PATH=str(root), TARGETARCH="amd64",
                                                                       **runtime.declared_validation_pins(ROOT)),
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            self.assertEqual(35, result.returncode, "failed tool download was not the terminating error")
            self.assertFalse(marker.exists(), "install completion marker survived failed download")

    def test_step_output_is_visible_before_child_finishes(self):
        # The child needs acknowledgement of its first line to finish. Capturing
        # until process exit necessarily times out and detects the original bug.
        with tempfile.TemporaryDirectory() as directory:
            acknowledgement = Path(directory) / "ack"
            script = ("import pathlib,time,sys; print('progress-ready',flush=True); "
                      "p=pathlib.Path(sys.argv[1]); end=time.monotonic()+2; "
                      "exec('while not p.exists() and time.monotonic()<end: time.sleep(.01)'); "
                      "sys.exit(0 if p.exists() else 7)")
            def observe(*args, **kwargs):
                if any("progress-ready" == str(item).strip() for item in args):
                    acknowledgement.touch()
            with mock.patch("builtins.print", side_effect=observe):
                push.run_step("Streaming fixture", [sys.executable, "-c", script, str(acknowledgement)],
                              cwd=ROOT, lane="fixture", capture_output=True)
            self.assertTrue(acknowledgement.exists())


if __name__ == "__main__":
    unittest.main()
