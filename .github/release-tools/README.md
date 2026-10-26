# Plus release packaging

The Release workflow captures one application SHA, version and build date.
Workflow helpers and the pinned validation image definition come from the
immutable workflow revision, separately from the selected application source.
This allows a new workflow to rehearse historical tags without overlaying their
source files. All builds and validation execute in the pinned Docker container.
Actions execute from the immutable tooling artifact, which survives historical
source checkout and post hooks. Buildx is staged from Docker's reported plugin
path rather than a runner-specific home directory.

Preparation resolves the application once and verifies its release provenance
in the same job and prepared container environment. The selected strict tag
gate or branch-metadata gate must succeed before preparation completes or
the source plan is uploaded. Builds depend on that successful job; provenance
failure or cancellation cannot start a build or authorize publication.

The frontend builds once. Five independent runners compile Linux amd64/arm64,
Darwin amd64/arm64 and Windows amd64 using GoReleaser OSS snapshot builds.
Linux arm64 runners own arm64 targets. The target set and packaging flags come
from the selected source's `.goreleaser.yaml`. Each archive has a manifest bound
to the captured SHA, version, date, target, name and SHA256. Producers verify HEAD;
collection also checks Go binary build metadata. The consumer requires exactly
five archives and five manifests before any image build or publication.
Each compilation checks the captured HEAD and a clean Git status both before
and after GoReleaser runs. Only the workflow's exact runtime staging paths are
ignored; tracked changes and other untracked files stop the build. Producer and
consumer checks also require Go's own `vcs=git`, `vcs.revision` equal to the
captured SHA, and `vcs.modified=false`, with no missing or duplicate settings.
Matching linker declarations alone are insufficient, and `-buildvcs=false`
cannot bypass this check. A dirty flag can result from untracked staging files;
it does not by itself establish that application source was changed.
Both producer collection and consumer verification inspect each real tar/zip:
`resources/model-pricing/model_prices_and_context_window.json` must occur once
as a regular file and match the pricing blob at the captured application SHA
byte for byte. Missing files, the former `resources/model-pricing` file layout,
directories, links, duplicates and changed bytes reject the archive before a
leaf artifact, image build or external upload can proceed. The GoReleaser file
mapping uses `strip_parent: true` so its destination remains a directory.

Plus-specific adaptations are intentional: build metadata such as
`0.2.14+custom.002` stays in archive names; all three GoReleaser image sections
are disabled; only `release-images.sh` publishes images to
`ghcr.io/<owner-lower>/sub2api-plus`. OCI tags use
`v0.2.14-custom.002`, architecture suffixes, and numeric `latest`, `0.2`, `0`
moving tags. Context binaries live at `<arch>/linux/<arch>/sub2api`, matching
`COPY ${TARGETPLATFORM}/sub2api`, and retain executable permission. Runtime
files come from the same captured application source.

The publisher alone has write permissions and enters the `release` Environment.
Build and packaging gates handle skipped verification branches explicitly and
reject cancelled workflows, including before publication.
It packages verified archives through GoReleaser `extra_files`; it does not
compile the application again. Checksums are materialized and compared with
verified archive bytes before external writes. Pricing assets use the selected
source's Go command, compared before external writes, and uploaded last without
`--clobber`. Existing bytes must match; API/download errors fail closed.
Publication of GHCR images and GitHub Release assets is not atomic. A partial publication needs inspection
under [the recovery policy](../../docs/RELEASING.md), never retagging.

## Read-only rehearsals

From a branch containing this workflow:

```bash
gh workflow run release.yml --ref <tooling-branch> \
  -f tag=<application-branch> -f dry_run=true
gh workflow run release.yml --ref <tooling-branch> \
  -f tag=vX.Y.Z+custom.NNN -f dry_run=true
```

Branch rehearsal checks source metadata but is not publication provenance.
Historical-tag rehearsal requires the annotated tag, notes, planned mapping at
that tag, main containment and successful exact-SHA CI and Security Scan.
Provenance queries use GitHub's REST SHA filter through the container's `gh api`,
then independently check workflow name, event, branch, SHA and success.
Historical rehearsal also executes the published-release verifier used by
finalization CI with the actual container GitHub CLI and read-only token.
Historical sources and artifacts must satisfy the current tooling's clean-source,
compiler VCS and archive contracts. Legacy staging rules, dirty VCS stamps or
incorrect archive layouts fail closed; rehearsal never modifies a historical
tag or relaxes these checks to retroactively accept its release.
Real publication must run from the same eligible tag ref; a branch dispatch with
a tag input cannot publish. Tag pushes always select real publication.

Rehearsals have read-only permissions, no registry login and no Environment.
They use the same build/context/image helpers, export both OCI archives, and
inspect blob digests, platforms, labels, binary bytes, executable entrypoint and
fallback pricing. Pricing is generated and compared against existing assets.
Historical tags require both assets. Absence on branch rehearsal is recorded as
`absent-untested`, never an immutability success.

The `release-packaging-evidence` run artifact retains OCI archives, checksums,
GoReleaser artifacts/metadata, pricing outcomes and `report.json` for three days.
The report records source/tooling SHAs, version/date, targets and OCI digests.
Use Actions job timestamps and cache restore logs for wall time and runner-minute
audits. Target caches include architecture and exact toolchain/lock identities.
GitHub ref visibility applies: separate tags do not share each other's caches.
The release frontend uses the same `frontend` dependency-cache lane as default-
branch CI, so branch and tag runs can restore its visible cache. OS, runner
architecture, toolchain and both dependency-lock identities must still match;
there is no fallback across generations. Binary and publisher lanes remain
separate. Cache reuse never replaces the frozen install or production build.
Do not report rehearsal time as publication time or the 9-minute budget as a
measurement. Rehearsal cannot verify upload permissions or registry reliability.

Helper regressions and shell syntax are included in both maintained full local
and CI profiles. Run focused checks through the prescribed local container:

```bash
python3 tools/ci_validation.py ensure
python3 tools/ci_validation.py exec -- python3 -m unittest discover \
  -s .github/release-tools -p 'test_release_*.py'
python3 tools/ci_validation.py exec -- bash -n .github/release-tools/release-images.sh
```

The helper regression suite also runs the pinned GoReleaser against a small,
dependency-free fixture, producing actual Linux tar and Windows zip archives
with the repository's file mapping. It independently checks the runtime pricing
path, regular-file type and exact bytes. Synthetic negative archives exercise
both producer and consumer gates, including valid checksum/provenance metadata
that must not hide a broken runtime resource.
The real fixture also checks clean Go VCS stamps with workflow staging present.
Actual builds with a changed tracked source or disabled VCS stamping are rejected
by both archive gates. Separate build-worker regressions reject staged and
unstaged tracked changes, deleted tracked files, unexpected untracked files, and
changes introduced during compilation before collecting any leaf artifact.
