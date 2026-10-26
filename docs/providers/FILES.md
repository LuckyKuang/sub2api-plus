# Provider Files

This document defines the first gateway Files capability. It is distinct from
passing a provider's native reference through an inference request. Official
account entitlement and browser authorization still require real-account checks.

## Reviewed contracts and scope

| Provider/profile | Gateway protocol | Upstream contract | Supported operations |
| --- | --- | --- | --- |
| DeepSeek account authorization | Anthropic Files | Harness account token, `X-Dsh-Auth-Token`, `/anthropic/v1/files` at its fixed inference origin | Upload, ACL-scoped local list/metadata, expiry, upstream delete, image references |
| DeepSeek API key with native Anthropic inference | Anthropic Files | `X-Api-Key`, `/anthropic/v1/files` on the official service | Same lifecycle |
| MiniMax global API key, Chat profile | MiniMax upload envelope | Bearer API key, `/v1/files/upload`, `video_understanding` | Upload, Chat video references, local revocation; remote metadata/list/delete are unsupported |

Sources: [DeepSeek Harness Files client at the reviewed revision](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/src/files-api.ts),
[Harness account transport contract](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/README.md),
[Harness default endpoint](https://github.com/deepseek-ai/deepseek-harness/blob/22ddee3792402c3e70ec5f86a63c3e3e895a2f60/packages/llm/llm-deepseek/src/config.ts),
[DeepSeek public Files documentation](https://api-docs.deepseek.com/guides/files_api/),
[MiniMax upload documentation](https://platform.minimax.io/docs/api-reference/file-management-upload),
[MiniMax retrieve documentation](https://platform.minimax.io/docs/api-reference/file-management-retrieve),
and [MiniMax Code's managed patcher](https://github.com/MiniMax-AI/minimax-code/blob/da22f11c55d4748155d7162c1030414228a00565/packages/local-runtime-v2/src/service/turn-system/agent-host/assembly/messages-file-api-patcher.ts).

The DeepSeek gateway selects the Anthropic family explicitly: `/v1/files` and
`/v1/files/:id`, with `anthropic-beta: files-api-2025-04-14`. There is no family
guessing from response shape or inbound credentials. API-key accounts must use
native Anthropic inference; another protocol configuration is rejected. Metadata
uses `type`, `size_bytes`, RFC3339 `created_at`, `filename`, and `mime_type`.
Pagination uses mutually exclusive `after_id`/`before_id`, default20, maximum1000.
The first implementation accepts JPEG/PNG/GIF/WebP, at most64MiB and512 filename
bytes. The smaller public limit is intentional despite the Harness's larger
constant. Expiry is omitted for permanent storage, or1hour–30days; both nested
expiry form fields must be supplied together. The gateway does not implement the
public OpenAI Files family in this batch.

DeepSeek local gateway references are supported only at the native Anthropic
Messages entrypoint, using an image `source` with `type: "file"` and `file_id`.
The Chat Completions and Responses conversion entries explicitly reject local
Files references before ACL reads, scheduling or inference. Their existing
converters cannot preserve these file-backed images; accepting the request as a
text-only inference would discard media and could charge for the wrong request.
This keeps the first batch within its reviewed native Anthropic scope. External
native references and inline media retain their existing compatibility paths.

MiniMax's public understanding-upload contract documents video and a maximum
retention of7days. The gateway conservatively accepts verified MP4/MOV, up to64MiB,
and expires its local references after6days; this is a gateway reuse limit, not a
promise about upstream retention. `base_resp.status_code` must be numeric zero;
HTTP200 alone never means success. Returned IDs remain strings, including safe
numeric provider IDs converted to strings. References are `mm_file://<id>`.
The bounded signature check requires a complete `ftyp` box within the first512
bytes and a reviewed major brand: `qt  ` for QuickTime, or `isom`, `iso2`–`iso9`,
`isoa`–`isod`, `mp41`, `mp42`, `avc1`, `av01`, or `M4V ` for MP4. Known AVIF/HEIF
image compatibility brands are rejected even under a generic ISO major brand;
unknown major brands are unsupported. The [MP4 registration authority](https://mp4ra.org/registered-types/brands)
distinguishes these image and container brands. This gate does not parse media
tracks or establish codec support, video duration, or decodability.
The public retrieve/delete contracts do not establish those operations for
understanding uploads. A MiniMax delete revokes the local reference but returns
an explicit unsupported remote-delete error.

MiniMax managed OAuth, CN API keys, `image_understanding`, unrelated speech/video
generation purposes, download, and upstream list/metadata/delete remain
unsupported here. Its official managed patcher calls a separately advertised
gateway: this is insufficient evidence that the corresponding authorization is
accepted at our direct provider endpoint. No unsupported combination is silently
redirected to another account type or region.

The MiniMax first batch accepts local references only in native Chat requests
using `video_url.url`. Local MiniMax Files in Messages and Responses bridges are
explicitly unsupported; the managed patcher's Anthropic-shaped payload is not
evidence for BYOK Anthropic authorization. Existing external references retain
their existing forwarding path.

## Gateway authorization and ownership

Files belong to the exact authenticated `(user ID, API-key row ID, group ID)`.
Other users, other keys belonging to the same user, and changed groups receive
the same not-found response as unknown files. Rotating the secret of the same
gateway API-key row preserves access. Creating a replacement key row does not.
Listing never exposes another caller's records or a shared upstream account's
external uploads. Pagination cursors must belong to the same active access
scope; the public endpoint rejects inaccessible IDs as not found, and the
repository treats them like unknown IDs rather than reading another scope's
timestamp. Account deletion, disablement, or removal from the caller's
group prevents inference; a recreated row does not inherit old mappings.

Only `file-s2a-<UUID>` is the gateway namespace. MiniMax wraps that identifier in
`mm_file://`. Unknown, malformed, expired, revoked, uploading and uncertain local
IDs always fail closed; lookup failure never converts a local ID into a native
reference. Existing external native references retain their compatibility path,
which does not acquire the gateway's ownership guarantee. The reserved namespace
must not be used for manually supplied native IDs. Inline media and unknown JSON
content keep their existing behavior.

Reference detection uses decoded protocol fields, so equivalent JSON escapes
have the same ACL, rewrite and owner requirements. Mentioning `file-s2a-` in text,
tool arguments or an unknown sibling object does not request a Files operation
and does not enable or reject an otherwise supported protocol conversion.

Long-lived ownership is a digest of platform, account type, account row creation,
endpoint namespace and a credential epoch. Imported API keys have no authoritative
provider account principal, so their key digest is the epoch: unproved key changes
require re-upload. DeepSeek account authorization binds both the server-created
login ID and the access-token digest. DeepSeek has no official account refresh
flow, and manual token replacement retaining an editable login ID cannot prove
the same provider principal. Token replacement or reauthorization requires
re-upload; proxy, client identity and display edits preserve mappings. Legacy
authorization without a login ID is scoped to its current access token. No
supported official refresh capability is removed by this conservative scope.
Email, display names and account row IDs alone never prove provider identity.

Every provider call uses a frozen current credential/identity/proxy snapshot. Its
result may publish only while that exact snapshot is still current; the database
checks this under locks acquired in proxy-before-account order, matching proxy
editing and rebinding. Account status and the proxy's status and configured
expiry are part of this per-request comparison and durable cleanup digest;
changing them fences the previous operation. Permanent ownership does not hash the
proxy. Inference resolves authorized references after security audit, rewrites the
actual remote ID and constrains every scheduler attempt to that owner and epoch.
Files from incompatible owners fail before account selection. Failure does not
trigger automatic upload or migration to another account.

## Upload, persistence and failures

Multipart fields and actual file signatures are checked before account selection.
The bounded input is spooled to a private, immediately unlinked temporary file to
compute a server-side digest and permit verified idempotency; model allowlist
middleware does not preload these two Files upload routes. Memory does not grow
with file size. The operating system reclaims the spool when its descriptor closes,
including process exit. No original bytes, credentials,
or prompts enter the Files table. Per-user outstanding reservations and active
records count towards a conservative25GiB/10000-file gateway limit, enforced under
a PostgreSQL advisory lock.

`Idempotency-Key` is optional and limited to128 printable ASCII bytes. Its digest
is isolated by caller/provider/protocol. Reuse requires the complete received
bytes, name, MIME, purpose and expiry to match, then rechecks ACL, owner and expiry.
A successful record can be reused across instances. Concurrent/incomplete,
uncertain or failed attempts return a conflict and are never blindly re-uploaded.
A repeated explicit key first pins scheduling to its original owner. A different
owner cannot replay the upload. An absent explicit key creates a new
reservation; clients wanting retry deduplication must supply one.

The durable reservation precedes the upstream write. Transport loss or an invalid
success response leaves the result uncertain. Mapping-save failure never publishes
a usable local ID. A known remote ID is retained for cleanup when storage permits;
DeepSeek cleanup is attempted with the original frozen owner/route. If both storage
and cleanup fail, the gateway cannot guarantee orphan recovery and keeps the
reservation non-usable. Unknown remote IDs are never found by scanning filenames
or deleting other uploads. No upstream exactly-once guarantee is claimed.

Delete first durably revokes local access. DeepSeek remote deletion is retried only
against the same verified owner; a failed attempt stays `delete_pending`. Concurrent
deletes use a database mutation lease, and repeated successful deletion does not
repeat the provider operation. A background recovery worker resumes only known
remote IDs with the original request's complete credential/proxy snapshot digest;
changed owners/routes remain pending and cannot borrow new credentials or routes.
It never scans upstream storage or retries an uncertain upload. There is no
inference-token charge for file CRUD;
provider storage/media fees are neither fabricated nor represented as Chat tokens.

All Files endpoints enter the common security audit after authentication and basic
validation, before ACL reads, scheduling, slots or provider writes. Binary bytes
have no prompt semantics; filename/purpose are metadata. Existing text siblings in
inference requests still enter both audit engines before local-ID resolution.
