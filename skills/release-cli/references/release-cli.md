# Release CLI Reference

## Action Contract

| Action | Required state | Mutation | Result |
| --- | --- | --- | --- |
| `inspect` | Tag; optional PR | None | Reports PR, tag, workflow, and Release state. |
| `promote-pr` | Submitted PR and notes | Protected GitHub auto-merge | PR merged and exact main SHA Actions green. |
| `validate` | Merged PR and notes | None | Focused metadata gate passes at merge commit. |
| `tag` | Merged PR and notes | One local annotated tag | Tag targets tested merge commit. |
| `publish` | Reviewed local tag | Exact remote tag push | Release workflow is triggered. |
| `monitor` | Remote tag | None | Watches workflow or pauses for manual approval. |
| `verify` | Successful workflow | None | Release and immutable assets verified. |
| `finalize` | Verified Release | Branch, commit, submit-pr | Published status follow-up PR created. |

## Repository Prerequisites

The repository must expose:

- GitHub default branch `main`.
- Repository Auto-merge enabled.
- An active rule applying `pull_request` to `main`.
- Active strict `required_status_checks` for every repository CI, security, and
  local-validation context.
- Merge-commit mode enabled.
- A protected `release` Actions environment with manual maintainer approval.

The exact required contexts are `sub2api/local-validation`,
`deployment-config`, `test`, `frontend`, `golangci-lint`, `goreleaser-config`,
`repository-policy`, `backend-security`, and `frontend-security`.
`promote-pr` fails closed when Auto-merge, merge-commit mode, current-branch
strictness, a protected rule, or a context is missing. It invokes
`gh pr merge --auto --merge` only. It never invokes `--admin`.

## Submitted PR Proof

`push-cli submit-pr` owns the proof. Release promotion requires exactly one PR
marker with 40-character base/head SHAs, a matching current PR base/head, and a
successful `sub2api/local-validation` status on the head. The PR must come from
`LuckyKuang/sub2api-plus`, remain open and non-draft, and target the GitHub
default branch.

After required checks complete, promotion refetches the default branch and PR.
Any head or base change stops the merge and requires another `submit-pr`.
Release-candidate promotion requires the notes file and `planned` metadata.
Promotion without notes is accepted only for the deterministic finalize branch
and requires the tag's metadata to be `published`.

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

`publish` pushes only:

    git push origin vX.Y.Z+custom.NNN

It does not wait for Actions. `monitor` resolves the exact remote annotated tag
and finds the Release push run by its target SHA, so recovery does not depend on
a local tag. A waiting `Build and publish` job returns status 2 and its URL;
approval must remain manual. `verify` does not monitor. It requires the same
remote tag, the workflow already completed successfully, and checks:

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
- If workflow approval is pending, approve only in GitHub Actions and rerun
  `monitor`.
- If a deterministic finalize branch already exists, inspect it manually;
  `finalize` never resets or overwrites it.
