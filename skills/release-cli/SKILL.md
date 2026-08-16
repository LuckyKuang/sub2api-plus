---
name: release-cli
description: Prepare, tag, publish, monitor, verify, and finalize immutable Sub2API Plus GitHub releases. Use when the user asks to create a vX.Y.Z+custom.NNN release tag, publish a release through GitHub Actions, check a release workflow, handle the protected release-environment approval, verify release assets, or mark a published version in UPSTREAM.md. Require a working authenticated GitHub CLI before any validation or release operation. Run validate and tag only inside the platform validation container: Apple Containers on macOS, Docker inside WSL2 Debian or Ubuntu on Windows, and Docker on Linux. Never validate on the host. Use the repository preflight and push only one reviewed annotated tag.
---

# Release CLI

Run the bundled command from the repository root. Every action requires an
explicit custom tag:

    python3 skills/release-cli/scripts/release_cli.py inspect --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py validate --tag vX.Y.Z+custom.NNN --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py tag --tag vX.Y.Z+custom.NNN --notes-file release-notes.md
    python3 skills/release-cli/scripts/release_cli.py publish --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py monitor --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py verify --tag vX.Y.Z+custom.NNN
    python3 skills/release-cli/scripts/release_cli.py finalize --tag vX.Y.Z+custom.NNN

Use validate for a read-only preflight. tag repeats the complete preflight and
creates a local annotated tag only. publish never creates a tag and pushes only
the named existing tag. monitor pauses at protected-environment approval.

validate and tag share push-cli's runtime rules. They probe Apple Containers on
macOS, Docker inside a running WSL2 Debian or Ubuntu distribution on Windows, or
Docker on Linux, then run `tools/release_preflight.py` inside
`deploy/Dockerfile.validation`. Direct host execution of that preflight is a
hard failure.

## Required GitHub CLI Gate

Before local verification, tag creation, release inspection, or any Git
transport, require all of the following:

1. gh --version succeeds.
2. gh auth status --hostname github.com succeeds.
3. The origin remote resolves exactly to LuckyKuang/sub2api-plus.
4. gh repo view confirms repository access.
5. gh api confirms the authenticated account has push permission.

Stop at once if any condition fails. Do not run gh auth login automatically.
Do not fall back to curl, browser automation, anonymous GitHub API access,
separate Git credentials, or a manually created GitHub Release. Before the
canonical preflight contacts the remote and before the actual tag push, run
gh auth setup-git. Git is only the transport for the exact tag object.

## Release Workflow

1. Prepare and commit release changes: version sources, Docker ARG values,
   UPSTREAM.md with planned status, synchronized release examples, and valid
   release notes. Follow docs/RELEASING.md.
2. Use push-cli to validate and push the release commit. Branch CI must pass.
3. Run validate with the intended tag and notes file. This launches the
   platform validation container; do not run tools/release_preflight.py on the
   host.
4. Run tag. Review the resulting local annotated tag.
5. Run publish. It pushes only git push origin <tag>; it does not use
   git push --tags, force push, or gh release create. It permits untracked
   release notes left by the canonical preflight, but never tracked changes.
6. Monitor the Release workflow. If Build and publish is waiting for the
   protected release environment, stop and give the maintainer the Actions URL.
   A maintainer must approve there; never try to approve it programmatically.
7. After the workflow succeeds, run verify. It requires the GitHub Release and
   both immutable pricing assets: model-pricing.json and
   model-pricing-manifest.json.
8. Run finalize only after verification. It changes the exact UPSTREAM.md row
   from planned to published, verifies the metadata, and stages only that file.
   Review, commit, and push this follow-up with push-cli.

The tool fails closed: it never deletes, moves, retags, overwrites, retries, or
releases a previously published version. It keeps protected-environment
approval as a manual terminal state, not an error to work around.

Read references/release-cli.md for action contracts, output interpretation, and
failure handling. Use scripts/release_cli.py for every state-changing action.
