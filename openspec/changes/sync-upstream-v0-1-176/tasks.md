## 1. Assessment

- [x] 1.1 Record official v0.1.176 commit, Plus HEAD, and merge-base.
- [x] 1.2 Inventory official 173→176 behavior and Plus-owned seams.
- [x] 1.3 Preview the merge with `git merge-tree` and classify conflicts.
- [x] 1.4 Assign migration 221 → Plus 220 and freeze 001–219.
- [x] 1.5 Write the session-policy vs fingerprint composition rule.
- [x] 1.6 Record the tag-only decision: no post-176 cherry-picks.
- [x] 1.7 Record official `session` as the unset fingerprint default.

## 2. Implementation (blocked on review)

- [ ] 2.1 Merge official `v0.1.176` on `upgrade/upstream-v0.1.176`.
- [ ] 2.2 Import official 221 SQL as `220_group_model_pricing.sql`.
- [ ] 2.3 Resolve conflicts using the ownership table; regenerate Ent/Wire.
- [ ] 2.4 Review auto-merged gateway, identity, wire, and usage files.
- [ ] 2.5 Restore Plus lockfile if conflict resolution disturbs it.
- [ ] 2.6 Prepare `0.1.176+custom.001` metadata without publishing.

## 3. Verification (after implementation)

- [ ] 3.1 Migration, release, documentation, and OpenSpec checks.
- [ ] 3.2 Backend lint plus unit/integration tests.
- [ ] 3.3 Frontend lint, typecheck, frozen install, and Vitest.
- [ ] 3.4 Add or keep regressions for client-profile deny, cross-group
      session deny, fingerprint modes, HTML 403, empty completed
      failover, and group model-pricing lookup.
