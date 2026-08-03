## 1. Protocol and implementation

- [x] 1.1 Define dual-write compatibility and absolute-header precedence.
- [x] 1.2 Pass through upstream Primary and Secondary `Reset-At` headers.
- [x] 1.3 Clear both reset-header families before applying a local quota view.
- [x] 1.4 Emit local Primary and Secondary `Reset-At` values.
- [x] 1.5 Parse absolute reset timestamps into the existing relative snapshot.

## 2. Verification and documentation

- [x] 2.1 Cover local dual-write, passthrough, parser edge cases, and 429 use.
- [x] 2.2 Document HTTP, SSE, and WebSocket behavior and the App API-key limit.
- [x] 2.3 Run focused and repository-required backend checks.
