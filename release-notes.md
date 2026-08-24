Sub2API Plus v0.1.178+custom.003

## Highlights

- Restore Content Moderation to current direct-user text and images so a
  policy violation is attributed only to a user submission, not to platform
  or tool content.
- Keep Prompt Audit on the same canonical extraction document, including
  instructions, tool traffic, and assistant or model items, so the security
  boundary stays fully visible.
- Treat incomplete extraction as a failure for both engines before either
  selection policy is applied.

## Changed

- Select Chat and Anthropic user-role content, plus the protocol-defined
  roleless user forms in Responses, Live, and Gemini, as Content Moderation
  inputs. Direct Alpha Search queries, embedding strings, and media prompts
  remain eligible.
- Exclude instructions, system or developer context, reusable prompt
  variables, assistant or model messages, reasoning, tool definitions,
  calls, results, approval responses, and tool-produced images from Content
  Moderation while leaving those segments available to Prompt Audit.
- Keep the official v0.1.178 baseline and Plus customizations; this
  iteration does not change the embedded Codex identity precedence.

## Fixed

- Restore the `v0.1.177+custom.003` user-attribution rule that a later
  shared-extractor expansion had broadened beyond direct-user content.
- Satisfy audit-content lint after the extraction-scope change.

## Compatibility and migration

- None. Existing data remains compatible and this iteration adds no database
  migrations.

## Known issues

- None known.

## Upstream baseline

Official release: v0.1.178
Official commit: e0c48a19ed794a565e3858662520afe0a1f9f0ba
