## Authorization boundary

`release-cli publish` remains a separate maintainer action and is the only
operation that transfers the reviewed annotated tag. PR merge does not create
or push a tag. Publication, workflow monitoring, Release verification, and
metadata finalization remain independent recovery boundaries.

Once the tag is transferred, the tag-triggered Release workflow verifies the
exact ref through the reusable backend matrix. `Build and publish` retains
`needs: verify` and starts automatically only after that matrix succeeds.
There is no API call that approves a pending deployment and no privileged
self-approval credential.

## External policy preflight

Before tag transfer, release-cli reads the GitHub Environment and ruleset APIs.
The `release` Environment must disable administrator bypass, expose no blocking
protection rule, use custom deployment policies only, and contain exactly one
tag policy named `v*+custom.*`. A repository-scoped active Tag ruleset must
match `refs/tags/v*+custom.*`, have no exclusions or bypass actors, allow
initial creation, and block update and deletion.

All checks fail closed before `git push`. The checks do not attempt to create
or repair governance because policy mutation requires an explicit repository
administration decision. A policy change between preflight and deployment is
still visible: monitor treats a waiting `Build and publish` job as configuration
drift and stops with a recovery diagnostic.

## Immutability and recovery

The Environment limits which refs may deploy; the Tag ruleset prevents a
published version from being moved or deleted. Existing annotated-tag checks,
default-branch containment, exact release-note subject checks, Release workflow
identity, immutable pricing assets, pinned Actions, least-privilege job
permissions, and serialized publication remain unchanged.

If observation is interrupted after tag transfer, the operator resumes with
`monitor`, then `verify`, then `finalize`. A failed publication is corrected
with a new custom version. No retry reuses, moves, or deletes a published tag.

## Rollout

Complete any already-started release under its existing contract first. Merge
this source change through the normal PR gate, then update the GitHub
Environment and create the active Tag ruleset. Verify both settings through the
same APIs used by release-cli before the next release tag is created.
