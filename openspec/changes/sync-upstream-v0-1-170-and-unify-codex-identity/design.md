## Merge and release identity

The official baseline is `v0.1.170` at
`b22f73e725236790f97d89bf0c3b908a48e591d5`. It is merged through a normal
three-way merge so Git retains upstream ancestry and Plus-only changes are
resolved explicitly. The merge keeps the current Plus version until release
preparation, then synchronizes the embedded version, Docker build args,
deployment examples, and `UPSTREAM.md` to `0.1.170+custom.001`.

The upstream tag carries a stale embedded `0.1.169` value. Plus release
metadata is independently authoritative and must never inherit that value.

## Outbound identity model

An OpenAI outbound identity contains the paired User-Agent, Originator,
Version, selected source, and credential-owning account. The resolver follows
this order:

1. A valid `credentials.user_agent` from the credential-owning account.
2. A valid administrator global `openai_codex_user_agent` setting.
3. The compiled `DefaultOpenAICodexUserAgent`.

The resolver accepts a value only when `PairCodexClientIdentity` produces a
valid paired originator and a client version. It is invoked after Spark shadow
resolution and its immutable result is passed through nested OAuth, PAT,
enrichment, and retry calls. OAuth/ChatGPT requests receive User-Agent,
Originator, and Version; non-OAuth API-key requests receive only the headers
their protocol permits.

Reauthorization attaches an existing account ID to the opaque server-side
OAuth session when the authorization URL is generated. Code exchange loads
that account from the session. The browser does not control which account's
identity applies. Initial account creation remains global/default because no
account exists yet.

## Compatibility boundaries

Account User-Agent is an optional existing JSON credential field, so no
schema migration is needed. New writes validate and normalize it. Empty means
inherit. Legacy invalid values remain runtime fail-safe and may be surfaced as
an administrator warning; they must not prevent startup.

Spark shadows inherit the credential parent identity. Shadows do not expose a
separate UA override because their authentication and client fingerprint must
remain associated with the same OpenAI account.

For OAuth Codex model manifests, the identity version is used in both the
request query and Version header. A custom API-key upstream retains its
client-provided version behavior.
