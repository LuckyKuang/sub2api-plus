---
name: compress-cli
description: >-
  Compress and validate the Sub2API Plus root AGENTS.md without weakening its
  repository sources of truth or its protected security-audit, Codex identity,
  pull-request, and release invariants. Use when a request creates, compresses,
  validates, or updates AGENTS.md repository rules in this project. Do not
  import generic npm, Maven, or Spring Boot contributor templates here.
---

# Compress CLI

Use this skill only for the repository-root `AGENTS.md`. Preserve its compact
pipe-index format while treating semantic completeness as more important than
line count.

## Workflow

1. Read the current `AGENTS.md` and the linked sources of truth before editing.
2. Preserve every existing rule unless the user's current requirement replaces
   it. Never weaken the `Security Audit` or `Codex Identity` categories during
   compression.
3. Verify every added path and command against the repository. Commands must
   exist in repository scripts or Make targets.
4. Keep the first line exactly `# AGENTS.md`. Every later non-empty line must
   use `|Category:value` and each category must be unique.
5. Keep detailed commands and explanations in their linked documents. Do not
   enforce a fixed line-count target.
6. Run the validator and tests after every change:

       python3 skills/compress-cli/scripts/compress_cli.py check AGENTS.md
       python3 skills/compress-cli/tests/test_compress_cli.py

The validator is intentionally read-only. Do not add an automatic rewrite mode:
semantic compression of security and release rules requires review of the
actual repository context.

Read `references/compress-cli.md` for the protected categories, validation
contract, and maintenance rules.
