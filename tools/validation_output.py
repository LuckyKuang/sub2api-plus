"""Live validation output and durable summaries without captured user content."""

from collections import deque
from contextlib import contextmanager
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time


ACTIVE_REPORT = None


def configure_console():
    # Windows pipes can default to a legacy code page. Live Unicode tool
    # output must not abort an otherwise successful validation action.
    for stream in (sys.stdout, sys.stderr):
        if hasattr(stream, "reconfigure"):
            stream.reconfigure(encoding="utf-8", errors="replace")


def stream(command, *, cwd, lock, label):
    started = time.monotonic()
    stopped = threading.Event()
    tail = deque(maxlen=80)
    process = subprocess.Popen(command, cwd=cwd, text=True, encoding="utf-8", errors="replace",
                               stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    def heartbeat():
        while not stopped.wait(20):
            with lock:
                print(f"[{label}] running for {time.monotonic() - started:.0f}s", flush=True)
    reporter = threading.Thread(target=heartbeat, daemon=True)
    reporter.start()
    try:
        for line in process.stdout:
            line = line.rstrip("\n")
            tail.append(line)
            with lock:
                print(f"[{label}]", line, flush=True)
        return subprocess.CompletedProcess(command, process.wait(), "\n".join(tail))
    finally:
        stopped.set()
        reporter.join()
        process.stdout.close()
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()


def resources():
    result = {"scope": "whole-validation-container"}
    for name in ("memory.peak", "memory.max", "cpu.stat"):
        try:
            result[name] = (Path("/sys/fs/cgroup") / name).read_text().strip()
        except OSError:
            pass
    return result


class Report:
    def __init__(self, directory, metadata):
        self.lock = threading.Lock()
        self.started = time.monotonic()
        self.path = Path(directory) / (datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S.%fZ") + f"-{os.getpid()}.json")
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self.data = {"schema": 1, "status": "running", "metadata": metadata, "steps": []}
        self.save()

    def save(self):
        temporary = self.path.with_suffix(".tmp")
        temporary.write_text(json.dumps(self.data, indent=2, sort_keys=True) + "\n")
        temporary.chmod(0o600)
        temporary.replace(self.path)

    def step(self, name, lane, seconds, returncode, queued_seconds=0):
        # Deliberately exclude argv, stdout/stderr, env values and payloads.
        with self.lock:
            self.data["steps"].append({"name": name, "lane": lane, "seconds": seconds,
                                       "queued_seconds": queued_seconds, "returncode": returncode})
            self.save()

    def finish(self, status):
        with self.lock:
            self.data.update(status=status, seconds=time.monotonic() - self.started,
                             resources=resources())
            self.save()
        print(f"Validation execution summary: {self.path}", flush=True)


@contextmanager
def report_scope(root, *, base, profile="full"):
    global ACTIVE_REPORT
    directory = os.environ.get("SUB2API_VALIDATION_REPORT_DIR")
    if not directory:
        yield
        return
    metadata = {"profile": profile, "image_generation": os.environ.get("SUB2API_VALIDATION_IMAGE_GENERATION"),
                "cache_generation": os.environ.get("SUB2API_VALIDATION_CACHE_GENERATION")}
    metadata["dependency_cache"] = {
        "frontend_install_present": (Path(root) / "frontend/node_modules/.modules.yaml").is_file(),
        "go_module_cache_present": (Path(os.environ.get("GOMODCACHE", "/nonexistent")) / "cache/download").is_dir(),
        "go_build_cache_present": (Path(os.environ.get("GOCACHE", "/nonexistent")) / "README").is_file(),
    }
    for name, ref in (("head", "HEAD"), ("tree", "HEAD^{tree}"), ("base", base)):
        if ref:
            result = subprocess.run(["git", "rev-parse", "--verify", ref], cwd=root,
                                    text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            metadata[name] = result.stdout.strip() if result.returncode == 0 else None
    previous = ACTIVE_REPORT
    ACTIVE_REPORT = Report(directory, metadata)
    status = "failed"
    try:
        yield
        status = "success"
    except BaseException as error:
        status = "interrupted" if isinstance(error, KeyboardInterrupt) else "failed"
        raise
    finally:
        ACTIVE_REPORT.finish(status)
        ACTIVE_REPORT = previous
