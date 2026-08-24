## Evidence model

The flow uses two explicit local proof profiles. `full` is the default and is
the only profile accepted for ordinary or release-candidate pull requests. It
runs every platform-container check and binds the result to exact base and head
SHAs. `release-finalization` is accepted only after publication and binds the
same SHAs plus the exact custom tag to a deterministic metadata-tree proof.

The PR marker records `base`, `head`, `profile`, and `tag`. `tag` is absent for
`full` and required for `release-finalization`. The commit status description
identifies the profile, and release promotion independently reruns the profile
validator instead of trusting mutable PR text or a branch name.

## Deterministic finalization

The finalization validator receives explicit base and head commits. It requires
the base to be an ancestor of the head, identifies exactly one custom release
mapping that changed from `planned` to `published`, and rejects every other
mapping mutation. It creates a temporary worktree at the base, applies that one
status transition, runs the repository documentation generator, and compares
the resulting tree to the requested head tree. This proves both the path set
and file contents instead of relying on a file allowlist alone.

Local submission additionally requires the deterministic branch name. Remote
promotion additionally requires the published annotated tag, successful
Release workflow, non-draft Release, and immutable pricing assets. Required CI
jobs invoke the same deterministic tree validator before choosing their mode.
If classification or regeneration fails, the required job fails; it does not
silently select the focused path.

On a protected `main` push, the validator accepts only a single merge commit
whose first parent is the event's previous main SHA and whose second-parent tree
equals the merge tree. The deterministic comparison then uses the first parent
as base and the merge commit as head.

## Release provenance gate

The tag-triggered Release workflow checks out the exact tag with full history.
It requires an annotated custom tag whose peeled target is `HEAD`, validates
the tag subject, notes, and repository release metadata, fetches `main`, and
requires the target to be contained by it. Through the Actions API it discovers
the push-triggered `CI` and `Security Scan` runs for `main` at that exact SHA
and requires every discovered run to be completed successfully.

`Build and publish` retains `needs: verify`, its automatic `release`
Environment, least-privilege write permissions, serialization, GoReleaser
behavior, and immutable pricing-asset checks. A manually created tag cannot
publish unless it points to a commit with the required successful main-run
evidence and valid release metadata.

## Complete local matrix scheduling

The complete profile first validates toolchains, then schedules dependency-safe
lanes with a bounded worker count. Frontend install precedes all frontend
commands. Go module tidiness precedes backend tests. Commands that may mutate or
inspect shared generated state remain in a single lane. Each command records
elapsed monotonic time; output is labeled by lane; any failure prevents a
success proof and stops new work while already-running processes are joined.

No check is removed. The default worker count is constrained by the validation
container's four-CPU limit, and frontend/Go internal parallelism is reduced as
needed to avoid oversubscription. Serial execution remains available only as a
diagnostic command option, not as a weaker validation profile.

## Rollout and recovery

The provenance gate, finalization profile, and local scheduler are independently
testable and revertible. Already-published tags and Releases are never changed.
If provenance cannot be established, publication fails before the write-capable
job. If finalization classification is ambiguous, every required context runs
the complete path or fails; no branch-name-only fallback exists.
