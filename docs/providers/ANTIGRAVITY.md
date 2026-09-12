# Antigravity

Sub2API Plus supports authorized Antigravity accounts for Claude and Gemini
traffic.

Outbound UA and client versions are managed in **System Settings → Outbound
identity**. Native OAuth retains the Antigravity preset; upstream accounts may
select another preset. See [outbound identity](../OUTBOUND_IDENTITY.md) for
inheritance, existing-setting defaults, and OAuth/probe coverage.

## OAuth and privacy identity

Code exchange and refresh-token validation select one global native Antigravity
identity before the first provider request. Token, user/project discovery and
privacy set/verify calls keep that snapshot through the operation; a later
operation observes new settings. Account refreshes use the credential-owning
account's configured identity. The `loadCodeAssist` request body's
`metadata.ideVersion` matches the selected Antigravity UA version.

The compiled UA is `antigravity/2.9.1 windows/amd64`. The privacy endpoints
`/v1internal:setUserSettings` and `/v1internal:fetchUserInfo` on
`daily-cloudcode-pa.googleapis.com` also send the trusted SDK declaration
`X-Goog-Api-Client: gl-node/22.21.1`. It is independent of the CLI version and is
preserved when identity is reapplied at send time. Client version changes retain
the chosen source and system fingerprint as well as this SDK pin.

This header belongs only to those two privacy requests. Other requests keep the
base preset's declarations. Generic header overrides cannot provide or replace
it. See the [source-priority matrix](../OUTBOUND_IDENTITY.md#presets-and-default-mappings)
for account, global and environment/compiled fallback behavior.

## Dedicated Endpoints

| Endpoint | Models |
| --- | --- |
| `/antigravity/v1/messages` | Claude |
| `/antigravity/v1beta/` | Gemini |

For Claude Code-style clients:

```bash
export ANTHROPIC_BASE_URL="https://your-sub2api.example.com/antigravity"
export ANTHROPIC_AUTH_TOKEN="sk-your-sub2api-key"
```

## Hybrid Scheduling

When hybrid scheduling is enabled, the general `/v1/messages` and `/v1beta/`
routes can also select Antigravity accounts.

Anthropic Claude and Antigravity Claude must not be mixed in the same
conversation context. Use separate groups to isolate them.
