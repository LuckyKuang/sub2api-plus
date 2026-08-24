# Release CLI Reference

## Action Contract

| Action | Required state | Mutation | Result |
| --- | --- | --- | --- |
| `inspect` | Tag; optional PR | None | Reports PR, tag, workflow, and Release state. |
| `promote-pr` | Submitted PR and notes | Protected GitHub auto-merge | PR merged and exact main SHA Actions green. |
| `validate` | Merged PR and notes | None | Focused metadata gate passes at merge commit. |
| `tag` | Merged PR and notes | One local annotated tag | Tag targets tested merge commit. |
| `publish` | Reviewed local tag | Exact remote tag push | Release workflow is triggered. |
| `monitor` | Remote tag | None | Watches automatic workflow publication to completion. |
| `verify` | Successful workflow | None | Release and immutable assets verified. |
| `finalize` | Verified Release | Branch, commit, submit-pr | Published status and any generated rollback-doc updates submitted. |

## Repository Prerequisites

The repository must expose:

- GitHub default branch `main`.
- Repository Auto-merge enabled.
- An active rule applying `pull_request` to `main`.
- Active strict `required_status_checks` for every repository CI, security, and
  local-validation context.
- Merge-commit mode enabled.
- A `release` Actions environment with administrator bypass disabled, no
  reviewer/timer/custom gate, and exactly one tag policy `v*+custom.*`.
- An active repository Tag ruleset matching `refs/tags/v*+custom.*`, with no
  bypass actors, that allows initial creation and blocks update and deletion.

The exact required contexts are `sub2api/local-validation`,
`deployment-config`, `test`, `frontend`, `golangci-lint`, `goreleaser-config`,
`repository-policy`, `backend-security`, and `frontend-security`.
`promote-pr` fails closed when Auto-merge, merge-commit mode, current-branch
strictness, a protected rule, or a context is missing. It invokes
`gh pr merge --auto --merge` only. It never invokes `--admin`.

## Submitted PR Proof

`push-cli submit-pr` owns the proof. Release promotion requires exactly one
typed PR marker with 40-character base/head SHAs, a matching current PR
base/head, and a successful profile-specific `sub2api/local-validation` status
on the head. `full` forbids a tag; `release-finalization` requires the exact
published tag. The PR must come from `LuckyKuang/sub2api-plus`, remain open and
non-draft, and target the GitHub default branch.

After required checks complete, promotion refetches the default branch and PR.
Any head or base change stops the merge and requires another `submit-pr`.
Release-candidate promotion requires the notes file, `planned` metadata, and a
`full` proof. Promotion without notes requires the matching
`release-finalization` proof and deterministic branch, then independently
regenerates the tree and verifies the published Release and immutable assets.

## Main Commit Gate

Auto-merge may create a commit different from the PR head. Promotion reads the
actual PR `mergeCommit.oid`, fetches `origin/main`, requires the merge commit to
be contained there, and discovers push-triggered Actions matching both branch
and SHA. It requires workflows named `CI` and `Security Scan`, then watches every
matching run with `--exit-status`.

## Focused Release Gate

`tools/release_preflight.py` no longer runs Go, frontend, lint, or deployment
matrices. It requires a clean worktree apart from an explicitly untracked notes
file, validates that the checked-out tree equals the requested merge-commit
tree, checks release metadata and notes with `tools/check_release.py`, verifies
local/remote tag absence, and optionally creates the annotated tag.

The tag message is the validated release notes verbatim. The tag target is the
merged PR commit, even when local HEAD remains the PR head with an identical
tree.

## Publication State Machine

Before mutation, `publish` reads the Environment, deployment-policy, and
ruleset APIs and fails closed on drift. It then pushes only:

    git push origin vX.Y.Z+custom.NNN

It does not wait for Actions. The Release workflow verifies the tag first, then
automatically starts `Build and publish`. `monitor` resolves the exact remote
annotated tag and finds the Release push run by its target SHA, so recovery does
not depend on a local tag. A waiting `Build and publish` job is external policy
drift and fails with its URL; release-cli never approves or bypasses it.
The tag-triggered workflow first validates the annotated tag, release notes,
planned mapping, `main` containment, and successful push-triggered `CI` and
`Security Scan` runs at the exact target SHA. This focused provenance gate does
not rerun the complete application matrix. `verify` does not monitor. It
requires the same remote tag, the workflow already completed successfully, and
checks:

- a non-draft, non-prerelease GitHub Release for the exact tag;
- `model-pricing.json`;
- `model-pricing-manifest.json`.

## Recovery

- If auto-merge remains pending, rerun `promote-pr`; an existing auto-merge
  request is not duplicated.
- If PR or main SHAs move, rerun `push-cli submit-pr`; never rewrite its proof.
- If a local tag is created but not published, inspect it before `publish`.
- If remote tag transfer succeeds but the terminal disconnects, resume with
  `monitor`; never rerun `publish` against an existing immutable tag.
- If the workflow unexpectedly waits at an Environment gate, restore the
  checked automatic Environment policy and rerun `monitor`; never approve or
  bypass the drifted gate through release-cli.
- If a deterministic finalize branch already exists, inspect it manually;
  `finalize` never resets or overwrites it.
- If a newer release was prepared before finalization, validate the historical
  mapping independently and include only the rollback examples generated by
  `tools/update_release_docs.py` with the status change.
