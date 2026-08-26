## Scope

This change corrects identity finalization only. It does not add an identity
source, client family, compatibility profile, or header override.

## Finalization

Endpoint-specific authentication, account metadata, fingerprint, session, and
beta headers are prepared first. Generic account overrides run before identity
finalization. The existing account-aware resolver then writes User-Agent,
Originator, and Version together as the last identity operation.

Messages retains its required Responses beta header. Native Alpha Search still
removes Responses-only headers, while its identity triple is finalized after
that removal. The PAT fallback retains its Responses beta header and finalizes
the same way as ordinary Codex Responses.

OAuth model synchronization resolves identity once. That result supplies both
the manifest client_version query and the final three headers, preventing a
URL/header version split. Agent Identity authentication uses the same resolved
identity helper.

## Regression Boundary

Focused tests cover account precedence despite ForceCodexCLI and conflicting
inbound/global values, global and compiled fallbacks, explicit legacy
compatibility, synchronized versions, and credential shadows. A static checker
forbids the displaced legacy finalizers and independent identity inputs in the
three repaired paths.
