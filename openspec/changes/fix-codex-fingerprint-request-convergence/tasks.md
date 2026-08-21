## 1. Contract

- [x] 1.1 Supersede the native/legacy compact exclusion with explicit request policies.
- [x] 1.2 Specify cross-transport staging, cache authority, and malformed metadata behavior.

## 2. Core implementation

- [x] 2.1 Add shared credential-owner preparation for map and raw bodies.
- [x] 2.2 Apply full native and installation-only legacy compact convergence.
- [x] 2.3 Make `off` free of fingerprint-owned body/header mutation.
- [x] 2.4 Rebuild malformed, null, and non-object embedded metadata safely.
- [x] 2.5 Exclude response retrieve/cancel and other non-create subpaths.

## 3. Entry points and identity

- [x] 3.1 Integrate Chat/Messages Responses bridges.
- [x] 3.2 Integrate HTTP-to-WebSocket and direct WebSocket turns.
- [x] 3.3 Remove the unused parallel outbound identity resolver.
- [x] 3.4 Compare final stable WS handshake carriers without mode gating.

## 4. Documentation and verification

- [x] 4.1 Update protocol documentation and source-priority/endpoint matrices.
- [x] 4.2 Add mode, compact, bridge, WebSocket, shadow, and malformed metadata tests.
- [x] 4.3 Add scheduler-cache mode preservation coverage.
- [x] 4.4 Run available frontend and static working-tree checks.
- [ ] 4.5 Run focused Go tests, OpenSpec validation, and the repository promotion gate when available.
