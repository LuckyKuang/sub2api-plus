# Release Process

This is the canonical pull-request-first process for custom Sub2API Plus
releases. Ordinary branch pushes are fast. The final PR submission performs the
complete local validation matrix once, and release-cli then relies on that exact
commit proof plus protected GitHub Actions before merging and tagging.

## Version Format

| Surface | Format |
| --- | --- |
| Git tag and GitHub Release | `vX.Y.Z+custom.NNN` |
| Embedded application version | `X.Y.Z+custom.NNN` |
| OCI image tag | `vX.Y.Z-custom.NNN` |

Increment `NNN` on the same upstream baseline and reset it to `001` after
merging a newer official baseline. `NNN` is always three digits.

## Repository Prerequisites

Before automatic PR promotion is enabled, repository administrators must:

1. Enable GitHub repository Auto-merge and merge-commit mode.
2. Protect `main` with a ruleset that requires pull requests.
3. Require the branch to be current with `main` and require these exact status
   contexts: `sub2api/local-validation`, `deployment-config`, `test`,
   `frontend`, `golangci-lint`, `goreleaser-config`, `repository-policy`,
   `backend-security`, and `frontend-security`.
4. Keep those contexts synchronized with the CI and Security Scan job IDs so a
   renamed or later unvalidated job cannot silently leave the merge gate.
5. Block force pushes and branch deletion; do not give release-cli an admin
   bypass.
6. Configure the Actions environment named `release` with administrator bypass
   disabled, no required reviewer, wait timer, or custom gate, and exactly one
   deployment policy for tags matching `v*+custom.*`.
7. Add an active repository Tag ruleset for `refs/tags/v*+custom.*`. Do not add
   bypass actors or a creation restriction; block tag updates and deletion.

The source tree cannot create these external governance settings safely.
`release-cli promote-pr` verifies Auto-merge, merge-commit mode, the required
pull-request rule, strict current-branch policy, and the complete context list.
Before tag transfer, `release-cli publish` verifies the complete Environment
and Tag ruleset policy. Both actions fail closed when an external prerequisite
is absent.

## Prepare the Release PR

Start a working branch from the latest `origin/main`, then:

1. Confirm the official upstream tag and commit.
2. Update `backend/cmd/server/VERSION`.
3. Update every `ARG VERSION=` in `Dockerfile` and `backend/Dockerfile`.
4. Add the custom version to `UPSTREAM.md` with status `planned`.
5. Synchronize install, rollback, and image examples:
   `python3 tools/update_release_docs.py`.
6. Write `release-notes.md` from the template below.
7. Commit all repository changes. Never commit on or push directly to `main`.

Intermediate pushes are intentionally fast and skip local validation:

```bash
python3 skills/push-cli/scripts/push_cli.py push
```

When the branch is the final merge candidate, submit it once through the local
promotion gate:

```bash
python3 skills/push-cli/scripts/push_cli.py submit-pr
```

`submit-pr` fetches the current `origin/main`, requires it in the branch,
records exact base/head SHAs, runs the complete matrix in the platform
validation container, refetches and rechecks both SHAs, pushes the exact head,
publishes `sub2api/local-validation`, and creates or reuses the PR. Any later
head or base change requires another `submit-pr`.

The documentation updater reads the current version from
`backend/cmd/server/VERSION`. Its rollback example uses the nearest lower
`published` entry in `UPSTREAM.md`; it skips `planned`, `historical`,
`withdrawn`, and `invalid`. Check without writing files with:

```bash
python3 tools/update_release_docs.py --check
```

## Release Notes

The first non-empty line is the annotated-tag subject. `Changed` and `Fixed`
are optional; every other section below is required and non-empty.

```markdown
Sub2API Plus vX.Y.Z+custom.NNN

## Highlights

Describe the primary user-visible changes.

## Changed

Optional details.

## Compatibility and migration

None.

## Known issues

None.

## Upstream baseline

Official release: vX.Y.Z
Official commit: <40-character commit>
```

## Promote the Release PR

Promotion requires the explicit PR number, intended tag, and reviewed notes:

```bash
python3 skills/release-cli/scripts/release_cli.py promote-pr \
  --tag vX.Y.Z+custom.NNN \
  --pr <number> \
  --notes-file release-notes.md
```

The tool verifies the PR is open, non-draft, same-repository, and targets
`main`. Its machine marker and successful local-validation status must match
the current head and current `main` base exactly. It waits for GitHub required
checks, rechecks both SHAs, and enables native `--auto --merge` without admin
bypass.

After GitHub merges the PR, release-cli resolves the actual merge commit,
fetches `origin/main`, and waits for push-triggered `CI` and `Security Scan`
runs at that exact SHA. A successful PR check alone is insufficient because
the merge commit may differ from the PR head.

If protected review or another required condition is still pending, promotion
returns status 2. Complete the GitHub requirement and rerun the same command.

## Validate and Create the Tag

After PR promotion, run the focused release metadata gate against the merged
PR commit, then repeat it while creating the local annotated tag:

```bash
python3 skills/release-cli/scripts/release_cli.py validate \
  --tag vX.Y.Z+custom.NNN \
  --pr <number> \
  --notes-file release-notes.md

python3 skills/release-cli/scripts/release_cli.py tag \
  --tag vX.Y.Z+custom.NNN \
  --pr <number> \
  --notes-file release-notes.md
```

This gate does not repeat Go, frontend, lint, integration, or deployment
matrices. Those ran in `submit-pr`, PR Actions, and merged-main Actions. It
validates release metadata, notes, synchronized examples, exact tree identity,
and local/remote tag absence. `tag` targets the PR's actual merge commit and
preserves the notes verbatim. It never pushes.

Review the local tag, then explicitly publish only that tag:

```bash
git show --no-patch vX.Y.Z+custom.NNN
python3 skills/release-cli/scripts/release_cli.py publish \
  --tag vX.Y.Z+custom.NNN
```

`publish` first verifies the automatic `release` Environment and immutable
custom-tag ruleset, then returns after exact tag transfer. This explicit command
is the irreversible publication authorization point. Never use `git push
--tags`, reuse a version, retag, force push, or create the GitHub Release
manually.

## Monitor and Verify

Monitor publication separately. The remote annotated tag is the source of
truth, so this action can resume without relying on a local tag:

```bash
python3 skills/release-cli/scripts/release_cli.py monitor \
  --tag vX.Y.Z+custom.NNN
```

The Release workflow reruns its tag verification before publishing. After the
verification matrix succeeds, the `Build and publish` job enters the checked
`release` Environment and starts automatically. If it unexpectedly waits,
`monitor` reports policy drift and the Actions URL; restore the Environment
policy and rerun `monitor`. The CLI never approves or bypasses a deployment.

After the workflow succeeds, verify the published state:

```bash
python3 skills/release-cli/scripts/release_cli.py verify \
  --tag vX.Y.Z+custom.NNN
```

Verification requires a successful workflow, a non-draft/non-prerelease GitHub
Release, and both immutable pricing assets:

- `model-pricing.json`
- `model-pricing-manifest.json`

The workflow accepts an existing pricing asset only when its bytes are
identical. Correct a bad asset with a new custom version, never by replacement
or retagging.

## Finalize Through a PR

After verification, finalize the published mapping:

```bash
python3 skills/release-cli/scripts/release_cli.py finalize \
  --tag vX.Y.Z+custom.NNN
```

`finalize` fetches the latest `origin/main`, creates deterministic branch
`release/finalize-X.Y.Z-custom.NNN`, and changes exactly one `UPSTREAM.md`
status from `planned` to `published`. It validates that historical mapping
independently from the current embedded version. If a newer release was already
prepared, it also synchronizes the generated rollback examples; otherwise only
`UPSTREAM.md` changes. It then calls `push-cli submit-pr` and never commits or
pushes `main` directly.

After the follow-up PR Actions pass, promote it without release notes:

```bash
python3 skills/release-cli/scripts/release_cli.py promote-pr \
  --tag vX.Y.Z+custom.NNN \
  --pr <finalization-pr-number>
```

The no-notes form is accepted only for the deterministic finalization branch
and requires the release mapping to be `published`.

## Pricing Assets

The manifest binds the release tag, fixed asset URL, and data SHA-256. Runtime
loading also validates dedicated HTTPS hosts, response sizes, JSON shape, and
version rollback. Release publication authority is the pricing trust boundary,
so tag-creation authority and Release/package permissions must remain limited
to trusted maintainers.

## Failed or Invalid Releases

- Never reuse or retag a published version.
- Record an externally visible bad release as `withdrawn` or `invalid` in
  `UPSTREAM.md` through a separate PR.
- Publish corrections under the next custom iteration.
- If tag push succeeded but local observation was interrupted, resume with
  `monitor`; do not rerun `publish`.
- If `Build and publish` unexpectedly waits, restore the checked Environment
  policy and rerun `monitor`; do not approve the drifted deployment through the
  CLI.
- Deleting an unpublished tag or artifact requires an explicit audit and
  maintainer decision.
