## 1. Planning and merge

- [x] 1.1 Record official v0.1.172 baseline, ownership, migration, identity,
  and release rules.
- [x] 1.2 Merge the official tag on an isolated local upgrade branch.
- [x] 1.3 Resolve merge conflicts without dropping Plus identity, quota,
  deployment, release, or usage-observability behavior.
- [x] 1.4 Import upstream migrations under local prefixes 200 and 201 and
  regenerate Ent and Wire.

## 2. Official v0.1.172 behavior

- [x] 2.1 Integrate OAuth pending-exchange security protection and regression
  tests.
- [x] 2.2 Integrate upstream response-model auditing across persistence, API,
  administration UI, filtering, exports, and locale keys.
- [x] 2.3 Integrate the Codex TUI 0.147.0 Ubuntu 24.04 default identity through
  the Plus resolver and keep User-Agent, Originator, and Version synchronized.
- [x] 2.4 Integrate quota window, gateway, billing, transport, captcha, and
  provider compatibility corrections.
- [x] 2.5 Enforce valid account UA > valid global UA > compiled default as the
  immutable identity-source order, and prevent stale automatic version values
  from moving the selected identity below the compiled version baseline.

## 3. Release preparation and verification

- [x] 3.1 Synchronize the embedded version, UPSTREAM mapping, release notes,
  and release configuration for v0.1.172+custom.001.
- [x] 3.2 Run focused backend and frontend regression coverage, generated-code,
  migration, identity, locale, release, lint, typecheck, and full test checks.
- [x] 3.3 Verify the identity-source matrix, synchronized version floor,
  default triple, OpenSpec delta, and outbound fallback paths after the final UA
  change.

Verification completed: `go generate ./ent`, `go generate ./cmd/server`,
`go mod tidy -diff`, full Go unit and integration suites, frontend lint,
typecheck, Vitest, and root `make test`.

Final identity verification completed: full unit coverage for
`internal/pkg/openai`, `internal/repository`, and `internal/service`; service
integration coverage; the focused identity/version matrix; frontend lint,
typecheck, locale and settings Vitest coverage; strict OpenSpec validation;
`make test-docs`; and `git diff --check`.
