#!/usr/bin/env python3
"""Observe requested error wording in the independently verified official CLI.

Uses only the Go test's isolated gateway, synthetic credentials and a temporary
Codex home. Expected text comes from the product acceptance requirements.
"""

import argparse
from pathlib import Path
import tempfile
import urllib.parse

from test_codex_cli_wire import IDENTITIES, managed_config, parse_events, run_process
from validation_runtime import require_in_validation


def main() -> None:
    require_in_validation(tool="Official Codex error message regression")
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--url", required=True)
    parser.add_argument("--code", choices=("upstream_response_interrupted", "upstream_unavailable", "session_account_lost"), required=True)
    parser.add_argument("--http-rejection", action="store_true")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="sub2api-codex-error-") as temporary:
        root = Path(temporary)
        home, work = root / "home", root / "work"
        home.mkdir(mode=0o700)
        work.mkdir(mode=0o700)
        port = urllib.parse.urlparse(args.url).port
        (home / "config.toml").write_text(managed_config(IDENTITIES[0], port), encoding="utf-8")
        result = run_process(args.binary, ["exec", "--json", "--ephemeral", "--skip-git-repo-check", "--model", "gpt-6.1-sol", "-"], home, work, 9, prompt="Answer the isolated fixture request without tools.", access_token="synthetic-downstream-key")
        events = parse_events(result.stdout)
        messages = [event.get("message", "") for event in events if event["type"] == "error"]
        expected_by_code = {
            "upstream_response_interrupted": "本次响应已中断。请先检查已收到的内容，再决定是否继续。",
            "upstream_unavailable": "服务暂时不可用，请稍后再试。",
            "session_account_lost": "当前会话可能无法恢复，请新开会话继续。",
        }
        expected = expected_by_code.get(args.code, "服务处理请求时出错，请稍后再试。")
        if result.returncode == 0 or not any(expected in message for message in messages):
            raise RuntimeError("official_cli_error_message_missing")
        if any("original diagnostic" in message for message in messages):
            raise RuntimeError("official_cli_original_error_leaked")
        if not any(event["type"] == "turn.failed" for event in events):
            raise RuntimeError("official_cli_failure_terminal_missing")
    print("PASS: official CLI requested message and failed terminal")


if __name__ == "__main__":
    main()
