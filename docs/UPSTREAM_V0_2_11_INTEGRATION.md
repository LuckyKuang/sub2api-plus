# Upstream v0.2.11 integration

This integration layers official `v0.2.11` at
`96f4c115c9749078f90cbf210a01d39baf3f53b6` onto the Plus tree at
`a751b5fa65183efef698a2a1944cde263f5b7fcf`. Its merge-base is the already
integrated official `v0.2.10` commit. Plus behavior is authoritative when
official changes overlap credential identity, ingress audit, pricing,
session/quota accounting, asynchronous tasks or distribution choices.

Source import and release preparation are separate. The embedded application
version remains `0.2.10+custom.001` until a separate preparation change updates
the version, Docker arguments, examples and planned release mapping together.
The v0.2.10 publication and finalization remain on their own branches. This
integration introduces no SQL migration or dependency-lock change.

## Imported behavior

- Redis in-flight balance reservations estimate text, images, video, search
  and HTTP voice requests. Handler ownership transfers a reference to each
  asynchronous billing task; the reservation remains until the handler and
  billing tasks finish. Synchronous balance-cache deduction precedes release.
  Cache failure retains the existing fail-open behavior; deferred deduction
  after a cache error cannot provide the same admission guarantee.
- API-key creation limits cover generated and custom keys: 200 non-deleted
  keys per user and 60 creation attempts per fixed one-hour window by default.
  Deleting keys does not refund creation counters. The two configured limits
  can independently be disabled with zero. Redis errors fail open for the
  frequency limit; database count failures remain errors.
- Claude native reset redemption adds an administrator confirmation flow,
  idempotency and account/organization leases. Unknown claim outcomes are
  fenced rather than retried. See [Claude behavior](providers/CLAUDE.md).
- GPT-6.1 Sol adds its model metadata, supported reasoning efforts and pricing.
  GPT-6 Astra gains Ultrafast handling. Sol/Luna offline metadata is retained.
  See [Responses behavior](protocols/OPENAI_RESPONSES.md) and
  [channel pricing](CHANNEL_PRICING.md).
- The Plus channel editor's missing Sonnet 5 reference card is repaired using
  Anthropic's published same-model prices. Manual addition and model sync
  resolve the same card; saved selling prices, including explicit zero, remain
  operator overrides. Sonnet 5.5 and unknown model names retain separate
  matching behavior.
- Generated Codex configuration defaults to the remote model catalog. A local
  catalog file remains selectable; responses over 1 MiB of UTF-8 bytes switch
  to the local file mode. The catalog URL and authentication stay on the user's
  gateway. OpenAI subscription labels retain their distinct SKU identities.
- Anthropic groups restricted to Claude Code can route Chat Completions and
  Responses requests to their configured fallback group. Without a fallback
  these endpoints still return 403. Audit precedes channel mapping, account
  selection, billing and any forwarding in the admitted fallback path.

## Plus integration decisions

| Area | Decision |
| --- | --- |
| Identity | Keep credential-owner precedence and all compiled identity/SDK pins. Claude profile, usage, redemption and fresh usage share the snapshot captured before token acquisition. |
| Audit | Reserve balances only after canonical audit permits the request/turn. Move standalone search and TTS billing behind audit. Preserve raw sibling extraction and unknown-content pass-through. |
| Billing tasks | Keep Plus synchronous fallback for every dropped billing task. A task keeps its reservation reference until it executes, including pool-overflow fallback. |
| WebSocket | Preserve Plus first/subsequent-turn audit, route and credential-owner checks, and per-turn billing eligibility. Add the Responses session reservation without replacing these checks. Do not import the Grok Realtime pre-handshake reservation: it has no canonical first-frame audit placement in this increment. |
| Models | Keep canonical Astra, its existing frontend context declaration and no standalone bare `gpt-6` preset. Add official models without duplicate entries or losing Sol/Luna pricing cards. |
| Distribution | Preserve Plus module paths, branding, toolchain, release metadata and removed upstream billing probes. Regenerate Wire in the prescribed container; never edit its generated output manually. |

## Configuration

The owning deployment defaults, environment names, failure semantics and
rollback controls are in [Deployment](../deploy/README.md). Back up persistent
data before upgrading. No migration is added by this increment.

## Validation and parallel release isolation

The integration uses an independent linked worktree and branch; it never
switches or resets the v0.2.10 release worktree. Upstream was fetched into a
dedicated reference without writing shared `FETCH_HEAD` or release tags.

While v0.2.10 validation is active, this branch performs source work only.
Container generation, formatting, tests and runtime/cache cleanup are deferred
to avoid interference. Those steps must subsequently use WSL2 Debian/Ubuntu
Docker on Windows, following [Contributing](../CONTRIBUTING.md).

Required evidence includes Wire generation, relevant backend unit/integration
tests, identity source/default/transport regressions, real-payload extraction
for both engines, HTTP/WS audit-order regressions, frontend lint/typecheck and
affected Vitest, locale and deployment checks. Source review is not validation
evidence. Before final PR submission, incorporate v0.2.10 publication
finalization and the latest `origin/main`, then use the maintained full
`submit-pr` profile against that exact tree.
