## 1. Contract and classification

- [x] 1.1 Add the Codex client-profile OpenSpec requirements and the upstream
  profile maintenance rule.
- [x] 1.2 Implement the closed built-in profile registry and coherent
  UA/Originator/version classifier.
- [x] 1.3 Replace arbitrary header-prefix evidence with a fixed known-header
  requirement and remove zero-required/fingerprint-bypass authorization paths.

## 2. Enforce one boundary

- [x] 2.1 Remove `ForceCodexCLI` and generic App Server behaviour from inbound
  authorization while preserving its unrelated outbound use.
- [x] 2.2 Route Responses, Chat Completions, Messages, Messages Count Tokens,
  and Alpha Search through the unified detector.
- [x] 2.3 Enforce the detector before WebSocket credential/upstream use and
  skip policy-ineligible WebSocket account candidates without health effects.

## 3. Administration and regression coverage

- [x] 3.1 Remove obsolete generic App Server and fingerprint settings, account
  fields, audit fields, and tests; retain the explicit compatibility profile
  allow-list.
- [x] 3.2 Update English and Chinese admin descriptions and protocol/security
  documentation.
- [x] 3.3 Add unit and integration coverage for coherent official profiles,
  strict semantic versions and bounds, mismatches, fake headers,
  `ForceCodexCLI`, Messages, Messages Count Tokens, Alpha Search, HTTP,
  WebSocket, and mixed eligible/ineligible account selection.
- [x] 3.4 Run formatting, focused backend/frontend checks, strict OpenSpec
  validation, and `git diff --check`.

## 4. Explicit historical compatibility mode

- [x] 4.1 Add a default-off global switch and a closed four-name historical
  profile registry that is distinct from the upstream official registry.
- [x] 4.2 Apply the switch to enabled-account ingress enforcement, configured
  account/global outbound UA validation, resolution, OAuth tuple handling,
  settings cache, audit log, and bilingual admin UI.
- [x] 4.3 Add regressions proving strict-default rejection, exact
  compatibility-mode admission, evidence/version enforcement, source fallback,
  case rejection, and OAuth tuple coherence.
