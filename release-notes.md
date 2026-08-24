Sub2API Plus v0.1.178+custom.005

## Highlights

- Keep Content Moderation on current direct-user text and images.
- Restore Prompt Audit conversation-text selection to `v0.1.177+custom.003`, so ordinary Codex `hi` requests are not blocked by client tool schemas.
- Continue sharing one canonical extractor, without converting extraction failures into policy blocks.

## Changed

- Prompt Audit full/async scans messages, instructions/system context, reusable prompt variables, reasoning, and search/embedding/media prompts.
- Prompt Audit no longer sends static `tools`/`functions` schemas, structured tool-call arguments, or tool/function outputs to Qwen3Guard.
- Blocking latest-turn-only again scans only the latest user text plus the nearest preceding assistant/model output.

## Fixed

- Stop treating Codex and other client tool definitions as jailbreak prompt text.
- Keep `hi` plus a large tool schema from producing a Prompt Audit block while still scanning jailbreak text in user/system/assistant conversation content.

## Compatibility and migration

- No database migrations. Existing Prompt Audit endpoints, scanners, and jailbreak policy are unchanged; only the scanned conversation-text selection is restored.
- Content Moderation behavior is unchanged.

## Known issues

- None known.

## Upstream baseline

Official release: v0.1.178
Official commit: e0c48a19ed794a565e3858662520afe0a1f9f0ba
