# In-app documentation center

The documentation center is a bundled set of setup guides at `/docs`. It is
disabled by default and is controlled from **System Settings → Feature
switches → Docs & Guides**.

## Availability

| Setting | Default | Behavior |
| --- | --- | --- |
| `docs_enabled` | `false` | When false, `/docs` redirects non-admin visitors and every header/home entry is hidden. |
| `docs_require_auth` | `true` | When true, anonymous visitors are redirected to `/login?redirect=...`. An explicit `false` makes the guides public. |

The gate is intentionally fail-closed: a missing `docs_require_auth` value keeps
login required. Only an administrator can persist `false`; the admin UI shows a
public-exposure warning before that setting is used.

Administrators may preview the guides while the feature is disabled with
`/docs?preview=1`. The preview flag is ignored for non-admins. Backend mode
keeps the existing restriction: non-admin users cannot open public pages.

The documentation entry follows the same precedence as the Model Plaza entry:
the in-app center wins whenever it is enabled, and the legacy `doc_url` remains
the external fallback. The docs footer shows `doc_url` as "More documentation"
when configured, and `contact_info` as the support block.

## Content ownership

Guides live in `frontend/src/docs/{zh,en}/*.md` and are registered in
`frontend/src/docs/manifest.ts`. The manifest is ordered by the outbound client
families declared by the gateway. MiniMax OAuth and API Key share one article;
OpenCode, Cline, Command Code, and TypeSafe / System One live in the
"Compatible clients" appendix because they do not have a dedicated outbound
preset; CC Switch has its own import guide under the troubleshooting group.

Every client guide links its official documentation; re-check that source before changing commands or config paths.

Both locales are required. `frontend/src/docs/__tests__/manifest.spec.ts`
checks that every manifest section has exactly one Chinese and one English file,
that every file starts with a level-one heading, and that only known runtime
placeholders are used.

No screenshots are shipped. Guides use numbered steps, code blocks, tables, and
callouts so English and Chinese can be maintained independently and light/dark
mode needs only CSS.

Code fences may carry an `os=unix`, `os=cmd`, or `os=powershell` marker. The
renderer turns them into labelled blocks, and the page-level operating-system
selector shows only the selected variant. Articles without OS-tagged blocks do
not render the selector.

## Runtime variables

Markdown may use these placeholders:

| Placeholder | Source |
| --- | --- |
| `{{baseUrl}}` | Public `api_base_url`, or the browser origin |
| `{{openaiBaseUrl}}` | `baseUrl` with a single `/v1` suffix |
| `{{host}}` | Hostname parsed from `baseUrl` |
| `{{siteName}}` | Public `site_name` |
| `{{keysUrl}}` | Absolute `/keys` URL |
| `{{modelPlazaUrl}}` | Absolute `/model-plaza` URL |

Interpolation happens before Markdown parsing and the resulting HTML is
sanitized with DOMPurify. Model IDs are deliberately not interpolated: guides
use `YOUR_MODEL_ID` and point readers at the Keys page or Model Plaza because
the catalog depends on the key's group and channels.

The proxy guide derives `DOMAIN-SUFFIX,{{host}},DIRECT` from the runtime host;
it never hard-codes a deployment domain.

## Security boundary

The guide content is compiled into the frontend bundle. `docs_require_auth`
is a UX gate, not a confidentiality boundary: anyone who can download the
JavaScript bundle can read the guides. Do not put API keys, internal hostnames,
upstream account details, or other secrets in the guides. If content must be
server-gated, serve it from an authenticated backend endpoint instead of
bundling it.

## Adding a guide

1. Add the section to `frontend/src/docs/manifest.ts` with a group, Chinese
   title/description, and English title/description.
2. Add `frontend/src/docs/zh/<id>.md` and `frontend/src/docs/en/<id>.md`.
3. Use only the runtime variables above; use `YOUR_MODEL_ID` for examples.
4. Tag system-specific fences with `os=unix`, `os=cmd`, or `os=powershell`;
   provide all three variants for client setup commands.
5. Run the docs manifest and render tests.
6. Update this document if the access model or content contract changes.
