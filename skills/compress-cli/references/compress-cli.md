# Compress CLI Reference

## Scope

This skill maintains only the root `AGENTS.md` for Sub2API Plus. The document is
a compact index of normative rules, not a substitute for `CONTRIBUTING.md`,
`docs/RELEASING.md`, deployment documentation, protocol documentation, or an
local OpenSpec change. OpenSpec implementation plans are untracked working
artifacts; durable behavior belongs in the owning documentation and tests.

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

Treat `Security Audit` and `Codex Identity` as same-priority security rules.
Compression must retain the following behavior unless an explicit policy
change updates this reference, the validator, its tests, and the linked source:

- Every accepted HTTP or WebSocket request or turn enters the audit boundary
  after authentication/basic validation and before account selection, billing,
  concurrency acquisition, upstream writes, or other side effects.
- API-key or OAuth account type, session affinity, routing, retries, probes,
  protocol adapters, transformations, classification, and upstream merges
  cannot bypass the audit boundary.
- Content Moderation and Prompt Audit consume the same canonical extraction
  contract. In compatibility with `v0.1.177+custom.003`, unknown item types,
  unknown Responses/Live frames, unknown sibling fields, valid-JSON
  unrecognized structures, and other incomplete or unextractable content pass
  through without an audit-derived block. Successfully extracted sibling
  content remains auditable.
- Extraction failures cannot become policy violations, unavailable decisions,
  HTTP 503 responses, or WebSocket closes. Every extraction, evaluation, or
  audit-dependency exception emits a structured log with request ID, endpoint,
  protocol, stage, a stable error code/reason, and available byte counts,
  without raw content, credentials, or unsanitized user fields. Invalid syntax
  remains the endpoint basic-validation responsibility.
- Endpoint or payload changes update the security-audit coverage document and
  semantic, transport, account-type, pass-through, and side-effect-order tests.
- Codex outbound identity keeps the credential-owning account, valid global
  setting, and compiled default precedence. Header or request paths cannot
  bypass it, and identity changes update the complete outbound-path test set.

Also preserve the rule that cross-cutting OpenSpec plans stay local and
untracked while durable behavior is committed to its owning documentation and
tests. Preserve secret handling, generated-code, migration, pnpm,
default-branch, container-only validation, protected PR, immutable tag,
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
validator, tests, this reference, and the linked source in the same change. Do
not relax the validator first merely to make a changed document pass.
