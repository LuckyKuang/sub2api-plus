"""Requirements: docs/CI_VALIDATION.md exact proof and disposable source state."""

from pathlib import Path
import json
import os
import subprocess
import sys
import tempfile
import unittest

import validation_workspace as workspace


class WorkspaceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.source = Path(self.temporary.name) / "source with spaces"
        self.source.mkdir()
        workspace.git(self.source, "init", "-b", "fix/test")
        workspace.git(self.source, "config", "user.name", "Fixture")
        workspace.git(self.source, "config", "user.email", "fixture@example.test")
        (self.source / "frontend").mkdir()
        (self.source / "frontend/source.txt").write_text("accepted source\n")
        (self.source / ".gitignore").write_text("node_modules/\n")
        workspace.git(self.source, "add", ".")
        workspace.git(self.source, "commit", "-qm", "fixture")
        self.head = workspace.git(self.source, "rev-parse", "HEAD")

    def test_snapshot_executes_exact_tree_and_does_not_share_writable_source(self):
        modules = self.source / "frontend/node_modules"
        modules.mkdir()
        (modules / "dependency").write_text("cache")
        with workspace.snapshot(self.source, self.head, self.head) as target:
            self.assertEqual(self.head, workspace.git(target, "rev-parse", "HEAD"))
            self.assertEqual("accepted source\n", (target / "frontend/source.txt").read_text())
            self.assertTrue((target / "frontend/node_modules").is_dir())
            (target / "frontend/source.txt").write_text("modified by validation")
            workspace.git(target, "add", "frontend/source.txt")
            self.assertEqual("", workspace.git(self.source, "status", "--porcelain=v1"))
        self.assertFalse(target.exists())
        self.assertEqual("accepted source\n", (self.source / "frontend/source.txt").read_text())
        self.assertEqual("cache", (modules / "dependency").read_text())

    def test_dirty_tracked_or_untracked_source_cannot_receive_exact_commit_proof(self):
        for name in ("frontend/source.txt", "untracked.txt"):
            with self.subTest(name=name):
                path = self.source / name
                path.write_text("unvalidated change")
                with self.assertRaisesRegex(RuntimeError, "clean"):
                    with workspace.snapshot(self.source, self.head, self.head):
                        self.fail("dirty source was accepted")
                if name == "untracked.txt":
                    path.unlink()
                else:
                    workspace.git(self.source, "checkout", "--", name)

    def test_wrong_head_is_rejected_and_failure_removes_writable_snapshot(self):
        with self.assertRaisesRegex(RuntimeError, "recorded HEAD"):
            with workspace.snapshot(self.source, "0" * 40, self.head):
                self.fail("wrong commit accepted")
        with self.assertRaisesRegex(RuntimeError, "child failure"):
            with workspace.snapshot(self.source, self.head, self.head) as target:
                raise RuntimeError("child failure")
        self.assertFalse(target.exists())

    def test_fresh_snapshots_preserve_go_build_cache_identity(self):
        # Real Go export IDs include the absolute source path without -trimpath.
        # Rebuilding unchanged source must reuse its identity across fresh trees.
        (self.source / "go.mod").write_text("module example.com/sample\n\ngo 1.23\n")
        (self.source / "sample.go").write_text("package sample\nfunc Add(a, b int) int { return a + b }\n")
        workspace.git(self.source, "add", ".")
        workspace.git(self.source, "commit", "-qm", "Go cache fixture")
        head = workspace.git(self.source, "rev-parse", "HEAD")
        env = dict(os.environ, GOCACHE=str(Path(self.temporary.name) / "go-cache"), GOFLAGS="", GOWORK="off")
        build_ids = []
        for _ in range(2):
            with workspace.snapshot(self.source, head, self.head) as target:
                self.assertFalse((target / "previous-run").exists())
                build_ids.append(subprocess.check_output(
                    ["go", "list", "-export", "-f", "{{.BuildID}}"], cwd=target, env=env, text=True).strip())
                (target / "previous-run").write_text("must not survive")
            self.assertFalse(target.exists())
        self.assertTrue(build_ids[0])
        self.assertEqual(build_ids[0], build_ids[1], "fresh checkout paths invalidate the Go build cache")

    def test_active_or_stale_snapshot_is_never_reused_or_deleted(self):
        with workspace.snapshot(self.source, self.head, self.head) as target:
            sentinel = target.parent / "owner"
            sentinel.write_text("active")
            with self.assertRaises(FileExistsError):
                with workspace.snapshot(self.source, self.head, self.head):
                    self.fail("overlapping source snapshot was accepted")
            self.assertEqual("active", sentinel.read_text())
        # Simulate a prior interrupted invocation in this same container.
        directory = target.parent
        directory.mkdir()
        sentinel.write_text("stale")
        try:
            with self.assertRaises(FileExistsError):
                with workspace.snapshot(self.source, self.head, self.head):
                    self.fail("historical writable snapshot was reused")
            self.assertEqual("stale", sentinel.read_text())
        finally:
            sentinel.unlink()
            directory.rmdir()

    def test_comparison_measures_identical_bytes_in_both_locations(self):
        report = workspace.benchmark(self.source, self.head, self.head)
        self.assertEqual(3, report["rounds"])
        self.assertEqual(3, len(report["samples"]["bind"]))
        self.assertEqual(3, len(report["samples"]["linux"]))
        for bind, linux in zip(report["samples"]["bind"], report["samples"]["linux"]):
            self.assertEqual(bind["sha256"], linux["sha256"])
            self.assertEqual(bind["bytes"], linux["bytes"])
            self.assertEqual(2, bind["files"])

    def test_windows_checkout_conversion_preserves_measured_bytes(self):
        workspace.git(self.source, "config", "core.autocrlf", "true")
        path = self.source / "frontend/source.txt"
        path.unlink()
        workspace.git(self.source, "checkout", "--", "frontend/source.txt")
        self.assertEqual(b"accepted source\r\n", path.read_bytes())
        with workspace.snapshot(self.source, self.head, self.head) as target:
            self.assertEqual(path.read_bytes(), (target / "frontend/source.txt").read_bytes())

    def test_mixed_clean_line_endings_do_not_invalidate_the_io_comparison(self):
        workspace.git(self.source, "config", "core.autocrlf", "true")
        # Existing LF files are still Git-clean on Windows, while a fresh
        # checkout converts them. Measure the same physical bytes on both sides.
        report = workspace.benchmark(self.source, self.head, self.head)
        self.assertEqual(2, report["checkout_representation_files"])
        self.assertEqual(report["samples"]["bind"][0]["sha256"],
                         report["samples"]["linux"][0]["sha256"])
        self.assertEqual(b"accepted source\n", (self.source / "frontend/source.txt").read_bytes())

    def test_cli_rewrites_source_arguments_and_cleans_up_after_child_failure(self):
        script = self.source / "child.py"
        evidence = Path(self.temporary.name) / "child.json"
        script.write_text("import json,pathlib,sys; "
                          "pathlib.Path(sys.argv[2]).write_text(json.dumps([str(pathlib.Path.cwd()), sys.argv[1]])); "
                          "sys.exit(7)\n")
        workspace.git(self.source, "add", "child.py")
        workspace.git(self.source, "commit", "-qm", "child fixture")
        head = workspace.git(self.source, "rev-parse", "HEAD")
        result = subprocess.run([sys.executable, str(Path(workspace.__file__).resolve()), "run",
                                 "--source", str(self.source), "--head", head, "--base", self.head,
                                 "--", sys.executable, str(script), str(self.source / "frontend/source.txt"), str(evidence)],
                                env=dict(os.environ, SUB2API_IN_VALIDATION="1"), capture_output=True)
        self.assertEqual(7, result.returncode, result.stderr.decode())
        root, argument = json.loads(evidence.read_text())
        self.assertNotEqual(str(self.source), root)
        self.assertEqual(str(Path(root) / "frontend/source.txt"), argument)
        self.assertFalse(Path(root).exists())
        self.assertEqual("", workspace.git(self.source, "status", "--porcelain=v1"))


if __name__ == "__main__":
    unittest.main()
