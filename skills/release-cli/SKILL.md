---
name: release-cli
description: Promote a locally validated Sub2API Plus pull request through protected GitHub auto-merge, create an immutable vX.Y.Z+custom.NNN tag at the tested main merge commit, publish and monitor the automatically gated Release workflow, verify immutable assets, and submit post-publication metadata through a follow-up PR. Use for release PR promotion, tag creation/publication, release-environment monitoring, verification, or UPSTREAM.md finalization. Require an authenticated GitHub CLI, exact submit-pr base/head proof, protected default-branch required checks, repository auto-merge, an automatic tag-only release Environment, immutable custom-tag rules, and successful Actions. Never use admin bypass, directly push main, repeat the full local application matrix, approve a deployment, or combine tag publication with monitor/verify.
---

# Release CLI

Run from the repository root with an explicit custom tag:

    python3 skills/release-cli/scripts/release_cli.py inspect --tag vX.Y.Z+custom.NNN --pr <number>
    python3 skills/release-cli/scripts/release_cli.py promote-pr --tag vX.Y.Z+custom.NNN --pr <number> --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py validate --tag vX.Y.Z+custom.NNN --pr <number> --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py tag --tag vX.Y.Z+custom.NNN --pr <number> --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py publish --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py monitor --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py verify --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py finalize --tag vX.Y.Z+custom.NNN

## Release Boundary

The release candidate must first be submitted with `push-cli submit-pr`.
`promote-pr` accepts only an explicit open, non-draft, same-repository PR to the
GitHub default branch. Its PR marker and `sub2api/local-validation` status must
match the current head and current default-branch base exactly.

Before enabling auto-merge, require repository Auto-merge and merge-commit
mode, an active default-branch `pull_request` rule, strict current-branch
policy, and every repository CI, security, and local-validation status context.
Wait for GitHub required checks, recheck the unchanged proof, then run protected
native auto-merge. Never pass `--admin` or directly call a merge API that
bypasses branch policy.

After merge, resolve the actual merge commit, require it in `origin/main`, and
wait for both `CI` and `Security Scan` push workflows at that exact SHA. A tag
cannot be created before those runs succeed.

## Tag and Publication

`validate` and `tag` require the merged PR number. They run only the focused
release metadata/notes/tag-absence gate; the complete application matrix was
already performed by `submit-pr` and GitHub Actions. The checked-out tree must
match the merged commit tree. `tag` creates one verified annotated local tag at
the PR's merge commit and never pushes it.

`publish` verifies that exact annotated tag is contained by the fetched default
branch and absent remotely. Before transfer it requires an automatic `release`
Environment limited to `v*+custom.*` tags with administrator bypass disabled,
plus an active no-bypass Tag ruleset that blocks custom-tag updates and
deletion. It then pushes only the named tag and returns. It never monitors,
verifies, uses `git push --tags`, or creates a GitHub Release manually.

`monitor` resolves the canonical remote annotated tag and observes its
tag-triggered Release workflow through automatic `Build and publish` completion.
A waiting Environment gate is policy drift and fails closed; the CLI never
approves it. `verify` is separate and requires that same remote tag, a
successfully completed workflow, non-draft Release, and both immutable pricing
assets.

## Finalization

After verification, `finalize` fetches the latest `origin/main`, creates a
deterministic `release/finalize-<version>` branch, and changes exactly one
`UPSTREAM.md` status from `planned` to `published`. It validates that historical
mapping independently from the current embedded version, synchronizes rollback
examples when a newer release has already been prepared, commits only the
mapping and those generated documentation updates, then invokes `push-cli
submit-pr`. It never commits or pushes main.
Promote the resulting PR through the same `promote-pr` policy after its Actions
pass, omitting `--notes-file`; that form is accepted only for the deterministic
finalization branch and requires `published` metadata.

## Safety

- Never promote a PR whose head/base differs from its local-validation proof.
- Never auto-merge without repository Auto-merge and required protected rules.
- Never use administrator bypass or treat the current account's admin role as
  permission to skip checks.
- Never tag an unmerged or untested commit, reuse a tag, retag, force push, or
  overwrite a published asset.
- Never publish when the release Environment or immutable custom-tag ruleset
  differs from the checked automatic policy.
- Never combine `publish`, `monitor`, and `verify`; each is independently
  resumable.
- Never switch branches with a dirty worktree or overwrite an existing
  finalization branch.

Read `references/release-cli.md` for action contracts, repository prerequisites,
recovery behavior, and exact state transitions.
