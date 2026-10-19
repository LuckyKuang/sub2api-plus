#!/usr/bin/env python3
"""Codex native identity gates must reject API-key omissions and retired sync."""

from contextlib import redirect_stderr
import io
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest.mock import patch

import check_openai_codex_identity as guard


class CodexIdentityGuardTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        paths = set(guard.PATH_GUARDS) | {
            Path("backend/internal/pkg/outboundidentity/identity.go"),
            Path("backend/internal/pkg/openai/request.go"),
            Path("backend/internal/service/openai_gateway_service.go"),
            Path("backend/internal/service/account_header_override_test.go"),
            Path("frontend/src/components/account/credentialsBuilder.ts"),
            Path("docs/protocols/CODEX_CLIENT_PROFILES.md"),
        }
        for relative in paths:
            target = self.root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(guard.ROOT / relative, target)

    def check(self) -> tuple[int, str]:
        errors = io.StringIO()
        with patch.object(guard, "ROOT", self.root), patch.object(guard, "SOURCE_ROOT", self.root / "backend"), redirect_stderr(errors):
            result = guard.main()
        return result, errors.getvalue()

    def test_current_native_declarations_and_exact_default_pass(self) -> None:
        result, errors = self.check()
        self.assertEqual(0, result, errors)

    def test_each_inference_path_rejects_oauth_only_declarations(self) -> None:
        # Official Codex inference declares Originator and Version for API keys
        # as well as OAuth. Restricting finalization to UsesOpenAICodexProtocol
        # omits API keys, and the guard must reject every such regression.
        for name, carrier, condition in (
            ("openai_gateway_forward.go", "req.Header", "account.UsesOpenAICodexProtocol()"),
            ("openai_gateway_passthrough.go", "req.Header", "account.UsesOpenAICodexProtocol()"),
            ("openai_ws_forwarder_payload.go", "headers", "account != nil && account.UsesOpenAICodexProtocol()"),
            ("openai_images.go", "headers", "account != nil && account.UsesOpenAICodexProtocol()"),
            ("openai_alpha_search.go", "req.Header", "account.UsesOpenAICodexProtocol()"),
        ):
            with self.subTest(path=name):
                target = self.root / "backend/internal/service" / name
                original = target.read_text(encoding="utf-8")
                native = f"s.applyOpenAIOutboundIdentity(ctx, account, {carrier}, true)"
                self.assertIn(native, original)
                target.write_text(original.replace(native, f"s.applyOpenAIOutboundIdentity(ctx, account, {carrier}, {condition})", 1), encoding="utf-8")
                try:
                    result, errors = self.check()
                    self.assertEqual(1, result, errors)
                    self.assertIn(name, errors)
                finally:
                    target.write_text(original, encoding="utf-8")

    def test_retired_version_sync_cannot_return_to_production(self) -> None:
        target = self.root / "backend/internal/service/openai_gateway_service.go"
        original = target.read_text(encoding="utf-8")
        for key in ("openai_codex_client_version_synced", "openai_codex_client_version_synced_checked_at", "openai_codex_client_version_sync_error", "openai_codex_version_auto_sync_enabled", "OpenAICodexVersionSyncService"):
            with self.subTest(key=key):
                target.write_text(original + f'\nconst restoredCodexSyncSetting = "{key}"\n', encoding="utf-8")
                try:
                    result, errors = self.check()
                    self.assertEqual(1, result, errors)
                    self.assertIn("forbidden obsolete identity bypass", errors)
                finally:
                    target.write_text(original, encoding="utf-8")


if __name__ == "__main__":
    unittest.main()
