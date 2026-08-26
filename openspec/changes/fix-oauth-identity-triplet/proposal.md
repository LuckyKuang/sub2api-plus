## Why

Three OpenAI OAuth outbound paths still performed late identity work outside
the account-aware resolver. Messages replaced a valid account identity with the
compiled default, Alpha Search allowed ForceCodexCLI and inbound identity
headers to participate, and OAuth model synchronization built its manifest URL
version separately from the final request headers.

## What Changes

- Make the account-aware resolver the final identity authority for Messages,
  native Alpha Search, the PAT Responses fallback, and OAuth model manifests.
- Use one resolved Version for both the model-manifest query and the coherent
  User-Agent, Originator, and Version headers.
- Preserve credential-owner, global, compiled-default, legacy-compatibility,
  and credential-shadow behavior with focused regression tests.
- Reject reintroduction of the known path-specific bypasses in the repository
  identity checker.

## Impact

The public API and persisted credential schema do not change. Requests that
previously lost a valid higher-priority identity now retain the configured
credential-owner or global identity. Invalid or empty candidates continue to
fall through according to the existing source order.
