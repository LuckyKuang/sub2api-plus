## 1. Baseline and merge

- [x] 1.1 Verify official annotated tag `v0.1.181` at
      `3af5443b224823ae507a50c7b113aa50604409c8`.
- [x] 1.2 Create `release/0.1.181-custom.001` from latest `origin/main`.
- [x] 1.3 Merge the tag with a no-ff merge and inventory conflicts by domain.

## 2. Conflict resolution

- [x] 2.1 Retain the Plus module path, brand, workflows, release identity, and
      distribution links. Take Go `1.27.0` and golangci-lint `2.13`.
- [x] 2.2 Three-way merge `go.mod`/`go.sum` and frontend lockfiles. Keep Plus
      Node/pnpm/Vite/Vitest/security overrides; absorb official plugin deps and
      `dompurify 3.4.14`.
- [x] 2.3 Keep published migrations 224/225/226/228 unchanged. Land official
      new SQL as `229`–`233`. Exclude seed backfill and seed lifecycle code.
      Update `FS.ReadFile(...)` assertions.
- [x] 2.4 Take official OpenAI/WS/HTTP/Grok/Gemini/Responses/billing paths, then
      restore Plus session affinity, cache identity, fingerprint, TPS, usage
      dedup, and audit hooks.
- [x] 2.5 Compose `auditcontent` and security-audit config reloads with the Plus
      extraction contract, fail-open unknown structures, and pre-side-effect
      order.
- [x] 2.6 Import the complete plugin unit: migrations, API, proto/generated
      code, routes, admin UI, config, deploy examples, and docs. Default off.
- [x] 2.7 Keep admin user support view, IP master switch (default off), S3 date
      path, image-task cleanup, version-badge hiding, and Codex version
      observability.
- [x] 2.8 Keep generated Ent/Wire output from the merge. Regenerating after
      schema/provider changes remains a follow-up if CI finds drift.

## 3. Metadata and tests

- [x] 3.1 Prepare `0.1.181+custom.001`, planned `UPSTREAM.md` mapping, and
      synchronized install/rollback examples.
- [x] 3.2 Write release notes covering long-context billing, Plus migration
      mapping `229`–`233`, experimental plugins default-off, and known issues.
- [x] 3.3 Update protocol/provider docs and
      `docs/SECURITY_AUDIT_CONTENT_COVERAGE.md`.
- [x] 3.4 Clean security-scan exceptions: no inherited `nanoid` waiver; expired
      lodash/axios waivers upgraded, removed, or re-audited with real owners.
- [ ] 3.5 Run `go mod tidy -diff` and verify no official module imports remain
      in active Go source. `plugin.pb.go` still embeds the official
      `go_package` inside the protobuf raw descriptor on purpose.

## 4. Verification and handoff

- [x] 4.1 Run focused backend tests for identity and migration filenames.
      Broader audit/billing/plugin/Fast-tier/181-compat tests remain for
      `submit-pr`.
- [ ] 4.2 Run frontend lint, typecheck, locale parity, and relevant Vitest.
- [ ] 4.3 Verify generated-code, migration, release-document, and diff checks.
- [x] 4.4 Do not create or push tags, Releases, or images in this change.
      `submit-pr` is a later explicit request and must use WSL2 Debian Docker.
