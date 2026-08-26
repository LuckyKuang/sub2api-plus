#!/usr/bin/env python3
"""Reject obsolete upstream Codex identity literals in production Go code."""

from __future__ import annotations

from pathlib import Path
import sys


ROOT = Path(__file__).resolve().parents[1]
SOURCE_ROOT = ROOT / "backend"
FORBIDDEN_LITERALS = ("codex-cli/0.91.0",)
PATH_GUARDS = {
    Path("backend/internal/service/openai_gateway_messages.go"): {
        "required": (("s.applyOpenAIOutboundIdentity(ctx, account, upstreamReq.Header, true)", 1),),
        "forbidden": ("enforceCodexIdentityHeaders(upstreamReq.Header)",),
    },
    Path("backend/internal/service/openai_alpha_search.go"): {
        "required": (("s.applyOpenAIOutboundIdentity(ctx, account, req.Header, true)", 2),),
        "forbidden": (
            "enforceCodexIdentityHeadersWithUA(req.Header",
            'openAIAlphaSearchInboundHeader(c, "User-Agent")',
            'openAIAlphaSearchInboundHeader(c, "Originator")',
            'openAIAlphaSearchInboundHeader(c, "Version")',
        ),
    },
    Path("backend/internal/service/upstream_models.go"): {
        "required": (
            ("identity := s.resolveOpenAIOutboundIdentity(ctx, credentialAccount)", 1),
            ("buildAgentIdentityAuthenticationHeadersWithIdentity(", 1),
            ("applyResolvedOpenAIOutboundIdentity(req.Header, identity, true)", 1),
        ),
        "forbidden": (
            "enforceCodexIdentityHeadersWithUA(req.Header",
            "s.buildOpenAIAgentIdentityAuthenticationHeaders(ctx, credentialAccount)",
            "resolveCodexOutboundIdentity(credentialAccount.GetOpenAIUserAgent())",
            "CodexCanonicalClientVersion(),",
        ),
    },
    Path("backend/internal/service/openai_agent_identity.go"): {
        "required": (
            ("identity := s.resolveOpenAIOutboundIdentity(ctx, credAccount)", 1),
            ("credAccount, identity)", 1),
            ("applyResolvedOpenAIOutboundIdentity(refreshed, identity, true)", 1),
        ),
        "forbidden": (
            "func registerAgentIdentityTask(ctx context.Context, account *Account)",
            "func ensureAgentIdentityTaskForAccount(ctx context.Context",
            "applyResolvedOpenAIOutboundIdentity(refreshed, s.resolveOpenAIOutboundIdentity",
        ),
    },
}


def main() -> int:
    violations: list[str] = []
    for path in SOURCE_ROOT.rglob("*.go"):
        if path.name.endswith("_test.go"):
            continue
        try:
            content = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            violations.append(f"{path.relative_to(ROOT)}: source is not valid UTF-8")
            continue
        for literal in FORBIDDEN_LITERALS:
            if literal in content:
                violations.append(f"{path.relative_to(ROOT)}: forbidden obsolete Codex UA {literal!r}")

    for relative_path, guards in PATH_GUARDS.items():
        path = ROOT / relative_path
        content = path.read_text(encoding="utf-8")
        for snippet, minimum_count in guards["required"]:
            actual_count = content.count(snippet)
            if actual_count < minimum_count:
                violations.append(
                    f"{relative_path}: expected at least {minimum_count} account-aware finalizer(s) "
                    f"matching {snippet!r}, found {actual_count}"
                )
        for snippet in guards["forbidden"]:
            if snippet in content:
                violations.append(f"{relative_path}: forbidden identity bypass {snippet!r}")

    if violations:
        print("OpenAI Codex outbound identity check failed:", file=sys.stderr)
        print("\n".join(violations), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
