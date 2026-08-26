## 1. Implementation

- [x] 1.1 Finalize Messages and both Alpha Search request builders with the
  account-aware outbound identity resolver.
- [x] 1.2 Resolve OAuth model-manifest identity once and reuse its Version in
  the URL and coherent request headers.
- [x] 1.3 Make AccountTestService fallback identity resolution honor its
  SettingService.
- [x] 1.4 Reuse one resolved identity snapshot across Agent Identity task
  registration and the retried upstream request.

## 2. Regression Coverage

- [x] 2.1 Cover account identity precedence when ForceCodexCLI and inbound or
  global values conflict.
- [x] 2.2 Cover global, compiled-default, legacy-compatible, synchronized
  version, and credential-shadow model-manifest identities.
- [x] 2.3 Add static path guards and update protocol documentation.
- [x] 2.4 Cover an identity setting update during Agent Identity task recovery.

## 3. Verification

- [x] 3.1 Run formatting, focused backend tests, the identity checker, strict
  OpenSpec validation, and git diff checks.
- [x] 3.2 Run the official backend verification profile in WSL2 Debian Docker.
