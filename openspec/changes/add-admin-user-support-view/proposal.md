# Add administrator user support view

## Problem

Administrators currently have a personal user-menu section, but it always uses
the administrator's authenticated identity. Support staff cannot safely inspect
the same account-scoped data that another user sees. Reusing the existing user
pages is unsafe because those pages include mutations, API-key plaintext, and
asynchronous-image calls authenticated with the user's API key.

## Proposal

- Replace the administrator sidebar's static personal-section title with a
  searchable account selector that defaults to the authenticated administrator.
- Preserve every existing personal route and mutation when the selected account
  is the authenticated administrator.
- Route every different selected account into an administrator-only support
  namespace that exposes read-only account views.
- Return API-key metadata without plaintext credentials from all administrator
  endpoints that enumerate another user's keys.
- Add administrator support queries for asynchronous-image history by user ID,
  without retrieving or using any of that user's API keys.
- Record the authenticated administrator as actor and the selected account as
  target for sensitive support reads.

## Non-goals

- Impersonating another user, issuing a token for another user, or changing the
  authenticated browser session.
- Adding write operations under the support namespace.
- Removing the existing user-management actions under `/admin/users`.
- Making batch-image, purchase, redemption, affiliate transfer, password,
  passkey, TOTP, or identity-binding workflows available in support mode.
- Changing ordinary users' API-key or asynchronous-image ownership semantics.

## Impact

- Public administrator API: new GET-only support endpoints are added and the
  existing administrator user-key list stops returning plaintext `key` values.
- Security boundary: actor and target identities remain distinct, and all
  non-self support pages are read-only.
- Backend handlers, services, repositories, routes, audit metadata, frontend
  routing, sidebar navigation, support pages, locales, and tests change.
- Persistent data schema does not change; asynchronous-image history already
  stores `user_id` and `api_key_id`.
