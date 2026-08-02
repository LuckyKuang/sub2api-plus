# Release Process

This is the canonical process for custom Sub2API Plus releases. Validate
locally, create the annotated tag locally, review it, and only then push that
single tag. Publication always requires an explicit maintainer action.

## Version Format

| Surface | Format |
| --- | --- |
| Git tag and GitHub Release | `vX.Y.Z+custom.NNN` |
| Embedded application version | `X.Y.Z+custom.NNN` |
| OCI image tag | `vX.Y.Z-custom.NNN` |

Increment `NNN` on the same upstream baseline and reset it to `001` after
merging a newer official baseline. `NNN` is always three digits.

## Prepare

1. Confirm the official upstream tag and commit.
2. Update `backend/cmd/server/VERSION`.
3. Update every `ARG VERSION=` in `Dockerfile` and `backend/Dockerfile`.
4. Add the version to `UPSTREAM.md` with status `planned`.
5. Synchronize the install, rollback, and image examples:
   `python3 tools/update_release_docs.py`.
6. Write `release-notes.md` from the template below.
7. Commit all repository changes; the notes file may remain untracked.
8. Get the release commit reviewed and ensure its branch CI is green.
9. Refresh local tags with `git fetch origin --tags`.

The documentation updater reads the current version from
`backend/cmd/server/VERSION`. Its rollback example uses the nearest lower
`published` entry in `UPSTREAM.md`; it deliberately skips `planned`,
`historical`, `withdrawn`, and `invalid` entries. Check synchronization without
writing files with:

```bash
python3 tools/update_release_docs.py --check
```

Do not create the release tag manually at this stage.

## Pricing Assets

Remote model pricing is published as immutable Release assets. Before the
first release using this flow, a repository administrator must create and
protect the GitHub Actions environment named `release`: require maintainer
review and restrict deployment to the reviewed release-tag policy. Release
publication authority is the runtime pricing trust boundary, so access to that
environment and permission to create Releases must remain limited to the sole
maintainer.

After GoReleaser publishes the normal release, the workflow copies the bundled
catalog and uploads exactly these immutable assets for the release tag:

- `model-pricing.json`
- `model-pricing-manifest.json`

The manifest binds the tag, the fixed asset URL, and the data SHA-256. The
runtime also validates dedicated HTTPS hosts, response sizes, JSON, and version
rollback before accepting it. On a retry the workflow only accepts an already
uploaded pricing asset when its bytes are identical; it never replaces an
asset. Correct a bad asset through a new custom version, never by retagging or
overwriting the existing Release.

The GitHub environment is necessary but not sufficient repository governance.
Before enabling publication, administrators must also require pull requests
and status checks for `main`, restrict tag creation to release maintainers,
keep Actions restricted to reviewed actions, and require review for changes to
`.github/workflows/`, release configuration, and deployment security files.
Those GitHub organization/repository settings cannot be safely created by a
source-code change and remain a maintainer-controlled prerequisite.

## Release Notes

The first non-empty line is the annotated-tag subject. Keep every required
section non-empty and keep the official identifiers inside `Upstream baseline`.

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

`Changed` and `Fixed` are optional. `Highlights`,
`Compatibility and migration`, `Known issues`, and `Upstream baseline` are
required.

## Validate and Create the Local Tag

Install the versions declared in `backend/go.mod`, `frontend/package.json`,
and `.tool-versions`, plus Python 3.10+ and Bash 4+. PostgreSQL and Redis must
be available when required by integration tests.

Run from the repository root:

```bash
python3 tools/release_preflight.py \
  --tag vX.Y.Z+custom.NNN \
  --notes-file release-notes.md \
  --create-tag
```

The command first checks all toolchains together, then verifies the clean
worktree, absent local/remote tag, `planned` upstream status, version sources,
release notes, README contracts, migrations, deployment scripts, Go module
tidiness, backend tests/lint, and frontend install/lint/typecheck/tests/build.
It also verifies that `HEAD` and the notes did not change during the run. Only
after every gate passes does it create and verify the local annotated tag; it
never pushes.

On macOS, preflight also runs the Apple container lifecycle test. On other
platforms that test remains a required macOS branch-CI gate, so do not release
unless the release commit's normal CI is green. The portable Caddy deployment
test runs locally on every platform.

To validate without creating a tag, omit `--create-tag`. Do not manually copy a
raw `git tag` command after that dry run; rerun with `--create-tag` so the final
commit, notes, and remote-tag checks remain coupled to tag creation.

## Review and Push

Inspect the locally created tag, then push only that tag:

```bash
git show --no-patch vX.Y.Z+custom.NNN
git push origin vX.Y.Z+custom.NNN
```

Never use `git push --tags`. The remote workflow reruns repository checks,
requires an annotated tag with the exact subject and target commit, validates
the complete tag message, and only then publishes the GitHub Release and
images.

## After Publication

1. Verify the GitHub Release, checksums, archives, and immutable GHCR tag.
2. Verify that the application reports the expected embedded version.
3. Change the `UPSTREAM.md` status from `planned` to `published`.
4. Keep the immutable version tag for rollback; treat `latest` as a moving
   convenience tag only.

## Failed or Invalid Releases

- Never reuse or retag a published version.
- Record an externally visible bad release as `withdrawn` or `invalid` in
  `UPSTREAM.md`.
- Publish corrections under the next custom iteration.
- Deleting an unpublished tag or artifact requires an explicit audit and
  maintainer decision.
