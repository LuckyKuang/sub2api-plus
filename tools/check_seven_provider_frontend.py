#!/usr/bin/env python3
"""Require execution of the seven-provider frontend business regressions."""
from __future__ import annotations

import json
from pathlib import Path
import subprocess
import sys
import tempfile

import validation_runtime

ROOT = Path(__file__).resolve().parents[1]
PROVIDERS = ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
OAUTH_GROUP = "OAuth proxy change and late browser callback isolation"
GROK_GROUP = "Grok independent import and saved-account completion boundaries"
PROBE_GROUP = "proxy management single and batch probe ownership"
REAUTH_GROUP = "existing-account reauthorization owner boundaries"
REAUTH_ENTRIES = ("openai exchange", "openai RT", "grok exchange", "grok RT", "grok SSO")
REQUIRED = {
    "src/components/account/__tests__/CodexCLIEditor.spec.ts": (
        "Codex CLI account editor shows defaults and disables task mode until the pinned runtime is ready",
        "Codex CLI account editor separates secret preservation, replacement and explicit clearing",
        "Codex CLI account editor revokes a prepared save when authorization changes under the same account ID",
    ),
    "src/components/account/__tests__/EditAccountModal.codexCli.spec.ts": (
        "EditAccountModal Codex CLI save workflow saves unrelated account edits without writing CLI defaults or credentials",
        "EditAccountModal Codex CLI save workflow keeps CLI drafts and reports partial success if its save fails",
        "EditAccountModal Codex CLI save workflow does not submit an old CLI draft after the selected account changes during Save",
        "EditAccountModal Codex CLI save workflow rejects changing proxy null to 7 with CLI edits before either write",
        "EditAccountModal Codex CLI save workflow rejects changing proxy 7 to null with CLI edits before either write",
        "EditAccountModal Codex CLI save workflow saves a proxy independently and binds subsequent CLI edits to the reloaded owner",
    ),
    "src/views/admin/settings/__tests__/CodexCLIRuntimeSettings.spec.ts": (
        "Codex CLI runtime settings checks installation without automatically installing or checking a latest release",
        "Codex CLI runtime settings does not call a version-matching system CLI ready without source verification",
        "Codex CLI runtime settings shows a detected unverified installation without claiming it is missing or executing it to read its version",
        "Codex CLI runtime settings keeps old installation information after a failed update and retries explicitly",
    ),
    "src/components/account/__tests__/GrokModelDiagnosticsPanel.spec.ts": (
        "GrokModelDiagnosticsPanel labels the current endpoint separately from models observed before a source change",
        "GrokModelDiagnosticsPanel keeps the historical catalog visible when refreshing the latest status fails",
    ),
    "src/components/common/__tests__/CheckboxDropdown.spec.ts": (
        "CheckboxDropdown keyboard interaction enters the teleported options and keeps repeated selections inside the menu",
        "CheckboxDropdown keyboard interaction opens from either arrow and restores form tab order at panel boundaries",
        "CheckboxDropdown keyboard interaction does not expose choices when disabled and closes if disabled while open",
    ),
    "src/components/admin/settings/__tests__/AdminAccessKeysSettings.spec.ts": (
        "管理员受限密钥配置 分组选择不会隐式授予操作或全局权限，并支持多个规则",
        "管理员受限密钥配置 保存失败时保留表单并展示错误，不显示成功密钥",
        "管理员受限密钥配置 编辑仅修改名称时保留所有规则且不会自动授予操作或全站权限",
    ),
    "src/components/account/__tests__/ZhipuStartPlanCell.spec.ts": (
        *(f"ZhipuStartPlanCell shows the independent {state} state"
          for state in ("available", "unavailable", "pending", "unknown")),
        "ZhipuStartPlanCell distinguishes unknown model details from an explicit empty model set",
        "ZhipuStartPlanCell rejects a late same-ID reauthorization result and permits the current owner",
        "ZhipuStartPlanCell keeps a proven negative state after query failure and never promotes pending on time alone",
        "ZhipuStartPlanCell shows reauthorization for business login failure and ignores uncommitted observations",
        *(f"ZhipuStartPlanCell does not probe or render {plan} as Start"
          for plan in ("off-peak", "individual-coding-plan", "team-coding-plan")),
    ),
    "src/api/__tests__/admin.accounts.oauthOwner.spec.ts": (
        *(f"admin account OAuth owner precondition rejects {kind} owner before sending credentials"
          for kind in ("missing", "empty", "truncated", "extended", "non-hex", "uppercase", "padded")),
        "admin account OAuth owner precondition sends the initiating owner unchanged with the replacement grant",
        "admin account OAuth owner precondition propagates an owner conflict without retrying against a new owner",
    ),
    "src/views/admin/__tests__/AccountsView.oauthOwner.spec.ts": (
        *(f"account list automatic refresh credential-owner propagation {boundary} reaches table consumers even when account updated_at and credential-presence flags are unchanged"
          for boundary in ("redacted credential owner", "same proxy endpoint")),
        "account list automatic refresh credential-owner propagation preserves the current owner when only account-observation freshness changes",
    ),
    "src/__tests__/integration/native-reauth-management.spec.ts": (
        *(f"native existing-account reauthorization management entrypoints {host} {provider} preserves ready native authorization during a same-owner observation refresh"
          for host, provider in (("menu reauthorization", "kimi"), ("account editor", "stepfun"))),
        *(f"native existing-account reauthorization management entrypoints {host} {provider} preserves ready native authorization during a same-owner proxy metadata refresh"
          for host, provider in (("menu reauthorization", "kimi"), ("account editor", "stepfun"))),
        *(f"native existing-account reauthorization management entrypoints {host} {provider} renews the saved owner through its native authorization service"
          for host in ("menu reauthorization", "account editor") for provider in ("deepseek", "kimi", "minimax", "stepfun")),
        *(f"native existing-account reauthorization management entrypoints {host} {provider} discards old {boundary} {stage} {outcome} without finishing the replacement"
          for host in ("menu reauthorization", "account editor") for provider in ("deepseek", "kimi", "minimax", "stepfun")
          for boundary in ("another account", "same account route", "close and reopen") for stage in ("complete", "reload")
          for outcome in ("success", "failure")),
        *(f"native existing-account reauthorization management entrypoints {host} {provider} discards old {boundary} {stage} {outcome} without finishing the replacement"
          for host, provider in (("menu reauthorization", "kimi"), ("account editor", "stepfun"))
          for boundary in ("redacted credential owner", "same proxy endpoint") for stage in ("complete", "reload")
          for outcome in ("success", "failure")),
        "native existing-account reauthorization management entrypoints does not offer unsupported ZCode in-place UI relinking or send a Claude login",
        *(f"native existing-account reauthorization management entrypoints {provider} does not offer an unpublished business-login refresh operation"
          for provider in ("deepseek", "zhipu")),
        *(f"native existing-account reauthorization management entrypoints {provider} retains its supported account-refresh action"
          for provider in ("kimi", "minimax", "stepfun")),
    ),
    "src/__tests__/integration/reauth-owner-generation.spec.ts": (
        *(f"{REAUTH_GROUP} {entry} keeps its active login when a fresh redacted DTO changes only usage metadata"
          for entry in ("openai RT", "grok SSO")),
        *(f"{REAUTH_GROUP} {entry} keeps its active login when a fresh redacted DTO changes only proxy name and timestamp"
          for entry in ("openai RT", "grok SSO")),
        f"{REAUTH_GROUP} selects Grok refresh-token reauthorization from the real redacted credential-status DTO",
        *(f"{REAUTH_GROUP} {entry} applies the current grant only to its initiating account"
          for entry in REAUTH_ENTRIES),
        *(f"{REAUTH_GROUP} {entry} discards old {boundary} {stage} {outcome} and the replacement can finish"
          for entry in REAUTH_ENTRIES for boundary in ("another account", "same account route", "close and reopen")
          for stage in ("grant", "apply") for outcome in ("success", "failure")),
        *(f"{REAUTH_GROUP} {entry} discards old {boundary} {stage} {outcome} and the replacement can finish"
          for entry in ("openai RT", "grok SSO") for boundary in ("redacted credential owner", "same proxy endpoint")
          for stage in ("grant", "apply") for outcome in ("success", "failure")),
        "OpenAI device existing-account reauthorization boundaries applies the current device grant only to its initiating account",
        *(f"OpenAI device existing-account reauthorization boundaries discards old {boundary} {stage} {outcome} and the replacement device flow can finish"
          for boundary in ("another account", "same account route", "close and reopen", "unmount")
          for stage in ("start", "poll", "apply") for outcome in ("success", "failure")),
        *(f"OpenAI device existing-account reauthorization boundaries {boundary} stops an old device loop during its polling interval"
          for boundary in ("another account", "same account route", "close and reopen", "unmount")),
    ),
    "src/__tests__/integration/seven-provider-proxy-lifecycle.spec.ts": (
        *(f"seven-provider proxy management business chain '{provider}' '{kind}' creates, binds, edits, repairs and deletes through actual API adapters"
          for provider in PROVIDERS for kind in ("apikey", "oauth")),
        *(f"proxy quality observations after an in-flight route edit {mode} quality results cannot certify a repaired endpoint"
          for mode in ("single", "batch")),
        "batch connectivity tests still reach proxies outside the displayed page",
    ),
    "src/__tests__/integration/oauth-proxy-generation.spec.ts": (
        *(f"{OAUTH_GROUP} {provider} {scenario}" for provider in ("openai", "grok") for scenario in (
            "ignores a late URL when the user starts on another route",
            "cannot create from a late exchange after a route change, and the replacement can finish",
            "stops an old RT batch before any second validation or account write",
            "cannot create from an exchange after the dialog closes",
        )),
        f"{GROK_GROUP} stops the old password batch after its route is replaced",
        *(f"{GROK_GROUP} ignores an old SSO import {outcome} after the replacement authorization starts"
          for outcome in ("success", "failure")),
        *(f"{GROK_GROUP} an old account POST {outcome} cannot finish the replacement Grok flow"
          for outcome in ("success", "failure")),
        *(f"OpenAI device login route and disposal boundaries ignores a late device {stage} after returning to change proxy"
          for stage in ("start", "poll")),
        "OpenAI device login route and disposal boundaries does not issue another device poll after its modal unmounts during the interval",
        *(f"ZCode link proxy-generation boundaries through production API adapter {scenario}" for scenario in (
            "drops a delayed start after proxy replacement and retains the replacement link",
            "drops a delayed poll after proxy replacement without exposing old credentials",
            "drops a delayed callback and clears its old-route input when a proxy is replaced",
            "does not report an old submitted creation as completion of a replacement link",
        )),
    ),
    "src/components/common/__tests__/ProxySelector.testing.spec.ts": (
        "proxy connection tests does not restart an individual test when a batch is started",
        "proxy connection tests shows per-proxy outcomes and allows another batch after a failure",
        *(f"proxy selector observations after route repair {scenario}" for scenario in (
            "clears cached connectivity when a saved route changes",
            "ignores an old success and keeps a replacement test pending until its own response",
            "does not attach a delayed failure to a deleted and recreated selector option",
        )),
    ),
    "src/views/admin/__tests__/ProxiesView.probe-owner.spec.ts": (
        *(f"{PROBE_GROUP} {kind} {scenario}" for kind in ("connectivity", "quality") for scenario in (
            "batch shares a pending single probe for an unchanged route",
            "old single completion cannot release or certify a replacement batch route",
        )),
        *(f"{PROBE_GROUP} {kind} {mode} {outcome} is discarded after leaving the management page"
          for kind in ("connectivity", "quality") for mode in ("single", "batch")
          for outcome in ("success", "failure")),
    ),
}


def coverage_errors(report: dict, *, frontend: Path, returncode: int = 0) -> list[str]:
    errors: list[str] = []
    seen: set[tuple[str, str]] = set()
    required = {(file, title) for file, titles in REQUIRED.items() for title in titles}
    if report.get("success") is not True:
        errors.append("Vitest did not report success")
    for result in report.get("testResults", []):
        try:
            file = Path(result["name"]).resolve().relative_to(frontend.resolve()).as_posix()
        except (KeyError, ValueError, TypeError):
            errors.append("invalid frontend result filename")
            continue
        if result.get("status") != "passed":
            errors.append(f"{file}: file did not pass")
        for assertion in result.get("assertionResults", []):
            title = assertion.get("fullName")
            if not isinstance(title, str):
                errors.append(f"{file}: invalid assertion name")
                continue
            case = (file, title)
            if case in seen:
                errors.append(f"{file}: duplicate case {title}")
            seen.add(case)
            if assertion.get("status") != "passed":
                errors.append(f"{file}: {title}: did not execute successfully")
    for file, title in sorted(required - seen):
        errors.append(f"{file}: missing required case {title}")
    if returncode:
        errors.append(f"Vitest exited {returncode}")
    return errors


def run(*, root: Path = ROOT) -> int:
    validation_runtime.require_in_validation(tool="seven-provider frontend coverage gate")
    frontend = root / "frontend"
    with tempfile.TemporaryDirectory(prefix="sub2api-frontend-gate-") as temporary:
        report_file = Path(temporary) / "vitest.json"
        result = subprocess.run([
            "pnpm", "--dir", "frontend", "test:run", *REQUIRED,
            "--maxWorkers=2", "--reporter=json", f"--outputFile={report_file}",
        ], cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, check=False)
        if not report_file.is_file():
            print("Seven-provider frontend: missing Vitest JSON report", file=sys.stderr)
            return 1
        problems = coverage_errors(json.loads(report_file.read_text()), frontend=frontend,
                                   returncode=result.returncode)
        for problem in problems:
            print(f"Seven-provider frontend: {problem}", file=sys.stderr)
        if not problems:
            print(f"Seven-provider frontend: {sum(map(len, REQUIRED.values()))} required cases passed")
        return int(bool(problems))


if __name__ == "__main__":
    try:
        sys.exit(run())
    except (OSError, ValueError, validation_runtime.ValidationRuntimeError) as error:
        print(f"Seven-provider frontend coverage failed: {error}", file=sys.stderr)
        sys.exit(1)
