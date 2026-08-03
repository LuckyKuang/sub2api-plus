## 1. Module identity migration

- [x] 1.1 Change the backend Go module declaration to the Plus repository.
- [x] 1.2 Update non-generated Go imports and lint package rules.
- [x] 1.3 Regenerate Ent and Wire output using the new module identity.
- [x] 1.4 Preserve official-upstream references and existing Plus distribution
  links.

## 2. Verification

- [x] 2.1 Verify no obsolete module import remains outside the upstream map.
- [ ] 2.2 Run Go module, unit, integration, and lint checks required by the
  repository.
