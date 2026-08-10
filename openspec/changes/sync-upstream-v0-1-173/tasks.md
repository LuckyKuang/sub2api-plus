## 1. Baseline and merge

- [x] 1.1 Record official v0.1.173 commit and Plus ownership boundaries.
- [x] 1.2 Merge the official tag on the local upgrade branch.
- [x] 1.3 Preserve intentional Plus identity, quota, migration, deployment, and
  distribution behavior while resolving conflicts.

## 2. Persistent data and public behavior

- [x] 2.1 Import official migrations under unique Plus prefixes 202 through
  218 without changing published migrations.
- [x] 2.2 Keep channel monitor V1 as the safe default and document V2 opt-in.
- [x] 2.3 Integrate Grok provider, pricing, media, voice, search, registration,
  administration, locale, and deployment behavior.
- [x] 2.4 Enforce strict opt-in for Grok cross-client model mapping and keep
  password-based OAuth hard-disabled.
- [x] 2.5 Integrate Gemini pool-mode, image accounting, and Antigravity
  response-observation fixes.
- [x] 2.6 Restore OAuth session-policy enforcement on every Responses
  transport, partial stream results, Grok Messages endpoint settings, and audio
  token aggregation.
- [x] 2.7 Restore the pre-merge Plus frontend lock graph and verify that frozen
  pnpm installation accepts every retained dependency and security override.

## 3. Release preparation and verification

- [x] 3.1 Synchronize embedded version, Docker arguments, UPSTREAM.md, release
  notes, and generated release documentation for v0.1.173+custom.001.
- [x] 3.2 Run migration, release, documentation, OpenSpec, formatting, lint,
  typecheck, backend, and frontend checks.
- [x] 3.3 Record final verification results and any environment-only limits.
- [x] 3.4 Add path-level regression coverage for the merge-sensitive fixes.

Verification completed with strict OpenSpec validation; release, README,
identity, migration, and documentation policy checks; `go mod tidy -diff`;
`golangci-lint run ./...`; full Go unit and integration suites; frontend lint,
typecheck, frozen installation, and all 227 Vitest files (1563 tests); generated
Wire refresh; and `git diff --check`. The Go suites required local loopback
access for existing `httptest` servers but had no remaining environment-limited
failures.
