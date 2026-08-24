## 1. Specification and policy

- [x] 1.1 Define full and release-finalization proof profiles.
- [x] 1.2 Define exact main-run provenance for tag-triggered publication.
- [x] 1.3 Define deterministic finalization tree regeneration and fail-closed CI
  classification.

## 2. Release provenance

- [x] 2.1 Add the shared exact-SHA Actions provenance checker and tests.
- [x] 2.2 Replace the reusable backend release verification matrix with the
  focused provenance job.
- [x] 2.3 Prevent tag pushes from starting the standalone Security Scan.

## 3. Finalization profile

- [x] 3.1 Add deterministic base/head finalization classification and fixtures.
- [x] 3.2 Extend push-cli proofs, status descriptions, and submission behavior
  with the explicit finalization profile.
- [x] 3.3 Require the matching profile and independently revalidate it during
  release promotion.
- [x] 3.4 Make all required CI/security contexts select focused finalization
  checks only after successful classification.

## 4. Local matrix scheduling

- [x] 4.1 Record command and lane durations in the complete local matrix.
- [x] 4.2 Schedule dependency-safe lanes within the existing resource limit.
- [x] 4.3 Benchmark serial and parallel execution on the same commit without
  reducing the command or test set.

## 5. Documentation and verification

- [x] 5.1 Synchronize AGENTS.md, both CLI skills and references, contributor
  guidance, release documentation, and policy tests.
- [x] 5.2 Run CLI, release-policy, finalization fixture, workflow lint, AGENTS,
  and strict OpenSpec checks.
- [x] 5.3 Run the complete platform-container matrix and record serial/parallel
  timing evidence or an explicit environment blocker.
