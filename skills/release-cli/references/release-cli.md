# Release CLI Reference

## Commands

Run from the repository root:

    python3 skills/release-cli/scripts/release_cli.py inspect --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py validate --tag vX.Y.Z+custom.NNN --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py tag --tag vX.Y.Z+custom.NNN --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py publish --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py monitor --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py verify --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py finalize --tag vX.Y.Z+custom.NNN

All actions first execute the GitHub CLI gate. An invalid, missing, expired, or
underprivileged GitHub CLI login fails before local preflight, tag mutation,
Docker access, or Git transport.

## Action Contract

| Action | Effect | Required input | Result |
| --- | --- | --- | --- |
| inspect | Read-only local and remote state | tag | Reports local tag, remote tag, Release workflow, and GitHub Release state. |
| validate | Canonical local release preflight | tag and notes file | Runs tools/release_preflight.py without tag creation. |
| tag | Local tag creation | tag and notes file | Repeats canonical preflight and creates one verified annotated local tag. |
| publish | Exact remote tag transfer | existing local tag | Runs gh auth setup-git, pushes only the named tag, then monitors Release. |
| monitor | GitHub Actions observation | tag | Finds the tag-triggered Release workflow and watches it in bounded intervals. |
| verify | Published release inspection | tag | Requires a non-draft GitHub Release with both immutable pricing assets. |
| finalize | Local post-publication record | verified published tag | Changes only the matching UPSTREAM.md row from planned to published and stages it. |

The tag must match vX.Y.Z+custom.NNN, where NNN is 001 through 999. The
embedded application version has the same value without the leading v.

## GitHub CLI and Git

The repository must be LuckyKuang/sub2api-plus. The mandatory gate runs:

    gh --version
    gh auth status --hostname github.com
    gh repo view LuckyKuang/sub2api-plus --json nameWithOwner
    gh api repos/LuckyKuang/sub2api-plus --jq .permissions.push

The script configures Git transport only after this gate:

    gh auth setup-git

It uses Git only for the required remote check performed by the existing
preflight and for this exact transfer:

    git push origin vX.Y.Z+custom.NNN

It never uses an alternative HTTP client, git push --tags, git push --all,
force push, or gh release create. The tag-triggered workflow is the only
release publication path.

## Protected Release Environment

The Release workflow has a Build and publish job guarded by the release
environment. monitor discovers the run through gh run list and obtains its
state through gh run view. It uses short gh run watch intervals while the run
is active. When the job or workflow is waiting, the command exits with status
2 and prints the Actions URL.

That is an intentional stop, not a failed release. The designated maintainer
must approve the release environment in GitHub. Do not call an approval API,
open a browser, dispatch another workflow, or change environment protection to
bypass this requirement. Run monitor again after the approval.

## Release Validation

validate and tag delegate to the canonical repository command:

    python3 tools/release_preflight.py --tag <tag> --notes-file <file>

tag adds --create-tag. That preflight owns validation of clean worktree,
versions, release notes, UPSTREAM.md planned mapping, README and deployment
contracts, migrations, toolchains, backend tests and lint, frontend checks,
and GoReleaser configuration. Never emulate it with a partial command list.

Before using tag, prepare a release commit and verify its branch CI through
push-cli. The notes file may be an untracked file as allowed by the canonical
preflight, but every other worktree change must be committed or removed.

## Publication and Verification

publish requires the named local tag to be annotated, use the exact required
subject, point at HEAD, and be absent from the remote. It rejects staged or
tracked worktree changes, while allowing untracked release notes that the
canonical preflight intentionally permits. A remote tag or existing GitHub
Release is immutable and stops the operation.

verify requires a completed successful tag-triggered Release workflow, then
uses gh release view to require a non-draft Release with these assets:

    model-pricing.json
    model-pricing-manifest.json

After verify, finalize rewrites exactly one planned status cell in UPSTREAM.md
to published, runs tools/check_release.py with the published requirement, and
stages UPSTREAM.md. It never commits or pushes the result. Use push-cli for the
follow-up commit and CI.
