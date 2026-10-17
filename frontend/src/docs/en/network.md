# Network and proxy

If direct API access works but a proxy causes slow handshakes, dropped streams, or unexpected 401/404 responses, check the routing rules first.

## Clash Verge Rev

1. In Profiles / subscriptions, right-click the current profile and choose "Edit rules".
2. Choose "Prepend rules".
3. Add this rule, save, and keep the mode on Rule:

```text
DOMAIN-SUFFIX,{{host}},DIRECT
```

4. If you use an extension script, prepend the same rule to `rules`. Script syntax differs between Clash versions; follow the official documentation.

## Verify direct routing

1. Open Connections.
2. Visit `{{baseUrl}}`.
3. Check that the connection matches `DOMAIN-SUFFIX,{{host}}` and that the chain is `DIRECT`.

If it still uses a proxy, the real host is not `{{host}}`. Add a rule for the host shown in Connections.

## Other proxy tools

Add `{{host}}` to the direct / bypass / no-proxy list.

## Notes

- Do not disable all proxying just to make the API look direct; route only the API host through DIRECT.
- If the site uses a CDN or a custom origin host, use the host the browser or client actually connects to.
