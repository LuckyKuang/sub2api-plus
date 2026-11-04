#!/usr/bin/env python3
"""Real interactive consumer for the Go gateway's isolated safety notice fixture.

The maintained official wire gate calls this through the Go test. Expectations
come from the reviewed official 0.165.0 TUI, not gateway projection internals:
codex-rs/tui/src/chatwidget/safety_buffering.rs at
360319d8dee62d87e2d59fc0c21805d79519cfed.
"""

from __future__ import annotations

import argparse
import errno
import fcntl
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time
import urllib.request
import urllib.parse

from test_codex_cli_wire import IDENTITIES, managed_config, task_environment
from validation_runtime import require_in_validation


NOTICE = "Giving this request a little extra thought"
CHOICES = ("Retry with a faster model", "Dismiss and keep waiting")
ANSI = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)")


def run_consumer(binary: Path, url: str, suppressed: bool) -> None:
    with tempfile.TemporaryDirectory(prefix="sub2api-codex-interactive-") as temporary:
        root = Path(temporary)
        home, work = root / "home", root / "work"
        home.mkdir(mode=0o700)
        work.mkdir(mode=0o700)
        port = urllib.parse.urlparse(url).port
        (home / "config.toml").write_text(
            managed_config(IDENTITIES[0], port, trusted_project=work).replace(
                "stream_idle_timeout_ms = 5000", "stream_idle_timeout_ms = 60000"
            ), encoding="utf-8"
        )
        environment = task_environment(home, 9, "synthetic-downstream-key")
        environment["TERM"] = "xterm-256color"
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 48, 160, 0, 0))

        def acquire_terminal() -> None:
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        process = subprocess.Popen(
            [str(binary), "--no-daemon", "--no-alt-screen", "--model", "gpt-6.1-sol",
             "Answer the fixture request without using tools."],
            cwd=work, env=environment, stdin=slave, stdout=slave, stderr=slave,
            preexec_fn=acquire_terminal,
        )
        os.close(slave)
        output = bytearray()

        def pump(seconds: float) -> str:
            deadline = time.monotonic() + seconds
            while time.monotonic() < deadline:
                if select.select([master], [], [], min(0.1, max(0, deadline - time.monotonic())))[0]:
                    try:
                        chunk = os.read(master, 65536)
                    except OSError as error:
                        if error.errno != errno.EIO:
                            raise
                        break
                    if not chunk:
                        break
                    output.extend(chunk)
                    if len(output) > 8 << 20:
                        raise RuntimeError("interactive_output_limit")
                    if b"\x1b[6n" in chunk:
                        os.write(master, b"\x1b[1;1R")
                    if b"\x1b[c" in chunk:
                        os.write(master, b"\x1b[?1;2c")
            return ANSI.sub(b"", output).decode("utf-8", errors="replace")

        try:
            deadline = time.monotonic() + 30
            text = ""
            while time.monotonic() < deadline:
                text = pump(0.2)
                with urllib.request.urlopen(url + "/ready", timeout=5) as response:
                    if response.status == 200:
                        break
                if process.poll() is not None:
                    raise RuntimeError("interactive_client_exited_before_response: " + text[-1600:])
            else:
                raise RuntimeError("interactive_response_not_observed: " + text[-1600:])
            # The upstream pauses here until /finish. OFF must positively prove
            # that this exact client can render the notice and both actions.
            text = pump(2)
            if suppressed:
                if any(phrase in text for phrase in (NOTICE, *CHOICES)):
                    raise RuntimeError("interactive_target_notice_leaked")
            else:
                deadline = time.monotonic() + 10
                while not all(phrase in text for phrase in (NOTICE, *CHOICES)) and time.monotonic() < deadline:
                    text = pump(0.2)
                if not all(phrase in text for phrase in (NOTICE, *CHOICES)):
                    raise RuntimeError("interactive_off_positive_control_missing: " + text[-2200:])
            with urllib.request.urlopen(url + "/finish", timeout=5) as response:
                if response.status != 200:
                    raise RuntimeError("interactive_fixture_finish_failed")
            deadline = time.monotonic() + 15
            while "safety-wire-ok" not in text and time.monotonic() < deadline:
                text = pump(0.2)
            if "safety-wire-ok" not in text:
                raise RuntimeError("interactive_answer_not_completed: " + text[-1600:])
            if process.poll() is not None:
                raise RuntimeError("interactive_session_did_not_remain_usable")
            if suppressed and any(phrase in text for phrase in (NOTICE, *CHOICES)):
                raise RuntimeError("interactive_late_target_notice_leaked")
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
            process.wait(timeout=5)
            os.close(master)


def main() -> None:
    require_in_validation(tool="Official interactive Codex safety notice regression")
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--url", required=True)
    parser.add_argument("--suppressed", choices=("true", "false"), required=True)
    args = parser.parse_args()
    run_consumer(args.binary, args.url, args.suppressed == "true")
    print("PASS: official interactive CLI notice, actions, answer and live session")


if __name__ == "__main__":
    main()
