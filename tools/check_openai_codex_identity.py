#!/usr/bin/env python3
"""Guard Codex identity finalizers, request snapshots and documented defaults."""

from __future__ import annotations

from pathlib import Path
import re
import sys


ROOT = Path(__file__).resolve().parents[1]
SOURCE_ROOT = ROOT / "backend"
FORBIDDEN_LITERALS = ("codex-cli/0.91.0",)
FORBIDDEN_BYPASSES = (
    "SetCodexIdentityEnforcementEnabled",
    "enforceCodexIdentityHeadersWithUA",
    "codexIdentityOverrideUA",
)
PATH_GUARDS = {
    Path("backend/internal/service/account_header_override.go"): {
        "required": (("outboundidentity.IsIdentityHeader(lowerName)", 1),),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_outbound_profile.go"): {
        "required": (("outboundidentity.IsIdentityHeader(name)", 1),),
        "forbidden": ("includeVersion", 'headers.Get("Version")'),
    },
    Path("backend/internal/service/openai_ws_forwarder_ingress.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("factoryCtx = carryOutboundIdentityScope(factoryCtx, ctx)", 1),
        ),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_ws_forwarder_v2.go"): {
        "required": (("factoryCtx = carryOutboundIdentityScope(factoryCtx, ctx)", 1),),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_ws_pool.go"): {
        "required": (
            ('firstOpenAIWSHeaderValue(headers, "User-Agent")', 1),
            ('firstOpenAIWSHeaderValue(headers, "Originator")', 1),
            ('firstOpenAIWSHeaderValue(headers, "Version")', 1),
        ),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_live.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, nil)", 3),
            ("ctx := WithOutboundIdentityScope(context.Background(), nil)", 1),
            ("s.dialLiveSideband(ctx, record)", 2),
            ("s.applyOpenAIOutboundIdentity(ctx, account, headers, true)", 1),
        ),
        "forbidden": ("s.dialLiveSideband(context.Background(), record)",),
    },
    Path("backend/internal/service/openai_gateway_forward.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("s.applyOpenAIOutboundIdentity(ctx, account, req.Header, account.UsesOpenAICodexProtocol())", 1),
        ),
        "forbidden": ('req.Header.Set("User-Agent", codexCLIUserAgent)',),
    },
    Path("backend/internal/service/openai_gateway_passthrough.go"): {
        "required": (("s.applyOpenAIOutboundIdentity(ctx, account, req.Header, account.UsesOpenAICodexProtocol())", 1),),
        "forbidden": ('req.Header.Set("User-Agent", codexCLIUserAgent)',),
    },
    Path("backend/internal/service/openai_ws_forwarder_payload.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("s.applyOpenAIOutboundIdentity(ctx, account, headers, account != nil && account.UsesOpenAICodexProtocol())", 1),
        ),
        "forbidden": ('headers.Set("User-Agent", codexCLIUserAgent)',),
    },
    Path("backend/internal/service/openai_oauth_service.go"): {
        "required": (("ctx = WithOutboundIdentityScope(ctx, nil)", 2),),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_codex_models_service.go"): {
        "required": (
            ("carryOutboundIdentityScope(fetchCtx, ctx)", 1),
            ("carryOutboundIdentityScope(ctx, sourceCtx)", 1),
        ),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_gateway_messages.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("ctx = WithAccountOutboundIdentity(ctx, account)", 1),
            ("s.applyOpenAIOutboundIdentity(ctx, account, upstreamReq.Header, true)", 1),
        ),
        "forbidden": ("enforceCodexIdentityHeaders(upstreamReq.Header)",),
    },
    Path("backend/internal/service/openai_gateway_chat_completions.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("ctx = WithAccountOutboundIdentity(ctx, account)", 1),
        ),
        "forbidden": (),
    },
    Path("backend/internal/service/openai_images.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("ctx = WithAccountOutboundIdentity(ctx, account)", 1),
            ("s.applyOpenAIOutboundIdentity(ctx, account, headers, account != nil && account.UsesOpenAICodexProtocol())", 1),
        ),
        "forbidden": (),
    },
    Path("backend/internal/repository/http_upstream.go"): {
        "required": (
            ("applyGrokCLIProxyAuthentication(req)", 2),
            ("outboundidentity.ApplyContext(req)", 2),
        ),
        "forbidden": (
            "applyGrokCLIProxyHeaders",
            "GROK_CLI_VERSION",
            'fallbackReq.Header.Del("User-Agent")',
            'fallbackReq.Header.Del("X-Grok-Client-Version")',
        ),
    },
    Path("backend/internal/service/openai_alpha_search.go"): {
        "required": (
            ("ctx = WithOutboundIdentityScope(ctx, c)", 1),
            ("ctx = WithAccountOutboundIdentity(ctx, account)", 1),
            ("s.applyOpenAIOutboundIdentity(ctx, account, req.Header, true)", 1),
            ("s.applyOpenAIOutboundIdentity(ctx, account, req.Header, account.UsesOpenAICodexProtocol())", 1),
        ),
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
        for snippet in FORBIDDEN_BYPASSES:
            if snippet in content:
                violations.append(f"{path.relative_to(ROOT)}: forbidden obsolete identity bypass {snippet!r}")

    for relative_path, guards in PATH_GUARDS.items():
        path = ROOT / relative_path
        content = path.read_text(encoding="utf-8")
        for snippet, minimum_count in guards["required"]:
            actual_count = content.count(snippet)
            if actual_count < minimum_count:
                violations.append(
                    f"{relative_path}: expected at least {minimum_count} identity guard(s) "
                    f"matching {snippet!r}, found {actual_count}"
                )
        for snippet in guards["forbidden"]:
            if snippet in content:
                violations.append(f"{relative_path}: forbidden identity bypass {snippet!r}")

    # The Go registry owns managed declarations. Keep the editor and explicit
    # save/runtime regression matrix aligned when that registry grows.
    registry = (ROOT / "backend/internal/pkg/outboundidentity/identity.go").read_text(encoding="utf-8")
    registry_body = re.search(r"func IsIdentityHeader\(name string\) bool \{(.*?)\n\}", registry, re.S)
    if registry_body is None:
        violations.append("outboundidentity: cannot locate managed identity header registry")
    else:
        managed_names = set(re.findall(r'"([a-z-]+)"', registry_body.group(1)))
        for filename, pattern in (
            ("frontend/src/components/account/credentialsBuilder.ts", r"const HEADER_OVERRIDE_BLOCKED_NAMES = new Set\(\[(.*?)\]\)"),
            ("backend/internal/service/account_header_override_test.go", r"var managedIdentityOverrideTestNames = \[\]string\{(.*?)\n\}"),
        ):
            content = (ROOT / filename).read_text(encoding="utf-8")
            declarations = re.search(pattern, content, re.S)
            declared_names = set(re.findall(r"['\"]([A-Za-z-]+)['\"]", declarations.group(1))) if declarations else set()
            missing = managed_names - {name.lower() for name in declared_names}
            if missing:
                violations.append(f"{filename}: missing managed identity headers: {', '.join(sorted(missing))}")

    defaults: dict[str, str] = {}
    for filename, names in (
        ("backend/internal/service/openai_gateway_service.go", ("codexCLIVersion", "codexCLIUserAgentSuffix")),
        ("backend/internal/pkg/openai/request.go", ("CodexDefaultOriginator",)),
    ):
        content = (ROOT / filename).read_text(encoding="utf-8")
        for name in names:
            match = re.search(rf'\b{re.escape(name)}\s*=\s*"([^"\n]*)"', content)
            if match is None:
                violations.append(f"{filename}: cannot locate compiled identity declaration {name}")
            else:
                defaults[name] = match.group(1)
    if len(defaults) == 3:
        version = defaults["codexCLIVersion"]
        originator = defaults["CodexDefaultOriginator"]
        user_agent = f"{originator}/{version}{defaults['codexCLIUserAgentSuffix']}"
        doc = ROOT / "docs/protocols/CODEX_CLIENT_PROFILES.md"
        documented_lines = doc.read_text(encoding="utf-8").splitlines()
        for declaration in (f"User-Agent: {user_agent}", f"Originator: {originator}", f"Version: {version}"):
            if declaration not in documented_lines:
                violations.append(f"{doc.relative_to(ROOT)}: missing exact default {declaration!r}")

    if violations:
        print("OpenAI Codex outbound identity check failed:", file=sys.stderr)
        print("\n".join(violations), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
