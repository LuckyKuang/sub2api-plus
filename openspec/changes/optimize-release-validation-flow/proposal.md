## Why

Release publication currently repeats the complete backend CI matrix after an
immutable tag is created, even though that tag targets the exact merge commit
whose `main` CI and Security Scan runs already succeeded. Post-publication
finalization also changes only deterministic release metadata, but submits that
change through the same complete local and remote application matrices as a
source change.

These repetitions add material wall-clock time without producing a distinct
application-tree proof. The release flow needs explicit validation profiles so
it can reuse immutable evidence while continuing to fail closed when a commit,
tag, generated document, workflow result, or repository policy differs from the
reviewed state.

## What Changes

- Replace the tag-triggered reusable backend matrix with a focused provenance
  gate that verifies the annotated tag, release metadata, `main` containment,
  and successful `CI` and `Security Scan` push runs at the exact target SHA.
- Add an explicit `release-finalization` submission profile. It is available
  only for a verified published tag and a deterministic finalization branch
  whose complete tree difference can be regenerated from the latest base.
- Make every required CI and security context execute the finalization
  classifier before it skips expensive application checks. Ordinary changes
  continue to run the existing complete checks.
- Add duration reporting and bounded lane scheduling to the complete local
  platform-container matrix without reducing its commands or test coverage.
- Stop tag pushes from starting an unrelated standalone Security Scan run.

## Capabilities

### Modified Capabilities

- `push-release-pr-promotion`: Adds typed validation proofs and a deterministic
  post-publication finalization profile while preserving the complete profile
  for ordinary and release-candidate pull requests.
- `automated-release-publication`: Replaces repeated application tests after tag
  publication with exact successful-main-run provenance.

## Impact

- **Release latency**: Tag verification and post-publication finalization no
  longer repeat complete application matrices for an unchanged application
  tree.
- **Repository governance**: Existing required context names, protected native
  auto-merge, immutable tags, and the automatic release Environment remain in
  force.
- **Local validation**: The complete matrix keeps all existing checks but may
  schedule independent lanes concurrently within the existing container CPU
  and memory limits.
- **Compatibility**: No application API, database, release tag, image tag, or
  published asset format changes.
