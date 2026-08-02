## 1. Baseline and security merge

- [x] 1.1 Merge official `v0.1.169` and retain the Plus release identity and
  upstream mapping.
- [x] 1.2 Add and test the upstream path-segment guard across Responses,
  Codex aliases and Gemini compatibility routes.
- [x] 1.3 Compose proxy stream circuit fail-open with Plus OAuth session-group
  and cross-group previous-response authorization checks.

## 2. Runtime and deployment

- [x] 2.1 Include pricing fallback resources in release images and direct
  archives, then test offline fallback loading from a release artifact.
- [x] 2.2 Apply no-new-privileges to every Compose app service and execute
  Compose hardening tests.
- [x] 2.3 Synchronize trusted-proxy, forwarded-IP trust and proxy-circuit
  environment bindings, examples, defaults and deployment guidance.

## 3. Release automation and documentation

- [x] 3.1 Upgrade Docker Actions to fixed Node.js 24-native versions and add
  release configuration validation.
- [x] 3.2 Keep release metadata, Docker build args and `UPSTREAM.md` aligned;
  update generated release documentation without adding untracked notes.
- [x] 3.3 Document production URL allowlist and Apple Containers hardening
  boundaries without changing an operator's actual environment.

## 4. Verification and handoff

- [x] 4.1 Run focused scheduler, circuit, path-guard, pricing and deployment
  tests, then backend and frontend checks appropriate to changed paths.
- [x] 4.2 Run OpenSpec strict validation, release policy/documentation checks,
  GoReleaser check and whitespace review.
- [x] 4.3 Review final diffs; separately hand off publication and GitHub
  governance actions that require maintainer authorization.

## 5. Post-merge security closure

- [x] 5.1 Replace mutable-branch pricing refresh with a latest-Release
  manifest, atomic validated cache writes, fallback preservation, and redirect
  host validation.
- [x] 5.2 Publish immutable pricing data and manifest assets from the protected
  release job, with no deployment key requirement.
- [x] 5.3 Reject whitespace-normalized Responses and Gemini path inputs, and
  restore HTTPS-only default image loading in CSP.
- [x] 5.4 Re-run backend, release, deployment, OpenSpec, and Apple Containers
  verification for the final source state.
