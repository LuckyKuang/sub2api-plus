# Compress CLI Reference

## Scope

This skill maintains only the root `AGENTS.md` for Sub2API Plus. The document is
a compact index of normative rules, not a substitute for `CONTRIBUTING.md`,
`docs/RELEASING.md`, deployment documentation, protocol documentation, or an
OpenSpec change.

Do not import templates from unrelated repositories. This project uses Go,
Vue, pnpm, repository-local Python CLIs, and platform validation containers. It
does not use npm-only, Maven, or Spring Boot contributor workflows.

## Format Contract

- The title is exactly `# AGENTS.md`.
- Every later non-empty line is `|Category:value`.
- Category names are unique; new rules either extend the owning category or add
  one clearly owned category.
- There is no fixed 15-35 line target. A rule required for correctness or
  security must never be removed to satisfy a size recommendation.
- Explanations and command catalogs belong in linked sources of truth.

## Protected Semantics

Treat `Security Audit` and `Codex Identity` as immutable, same-priority
security rules. Compression must retain the following behavior:

- Every content-bearing HTTP or WebSocket request or turn is classified and
  audited after authentication/basic validation and before account selection,
  billing, concurrency acquisition, upstream writes, or other side effects.
- API-key or OAuth account type, session affinity, routing, retries, probes,
  protocol adapters, transformations, classification, and upstream merges
  cannot bypass the audit boundary.
- Content Moderation and Prompt Audit consume the same canonical extraction
  contract. Unknown or partially extracted content remains observable and
  fails closed whenever a blocking audit mode is active, even when a sibling
  field was extracted successfully.
- Endpoint or payload changes update the security-audit coverage document and
  semantic, transport, account-type, and side-effect-order tests.
- Codex outbound identity keeps the credential-owning account, valid global
  setting, and compiled default precedence. Header or request paths cannot
  bypass it, and identity changes update the complete outbound-path test set.

Also preserve OpenSpec routing, secret handling, generated-code, migration,
pnpm, default-branch, container-only validation, protected PR, immutable tag,
publication authorization, and upstream-merge rules.

## Validation

Run from the repository root:

    python3 skills/compress-cli/scripts/compress_cli.py check AGENTS.md
    python3 skills/compress-cli/tests/test_compress_cli.py

The CLI checks syntax, unique and required categories, repository source paths,
obsolete global-skill references, and critical semantic anchors. It does not
rewrite the document and does not claim that automated checks replace semantic
review of the diff.

When a legitimate policy change alters a protected anchor, update the rule,
validator, tests, and linked source in the same change. Do not relax the
validator first merely to make a changed document pass.
