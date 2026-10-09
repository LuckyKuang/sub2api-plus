# Plus release packaging

The Release workflow captures one application SHA, version and build date.
Workflow helpers and the pinned validation image definition come from the
immutable workflow revision, separately from the selected application source.
This allows a new workflow to rehearse historical tags without overlaying their
source files. All builds and validation execute in the pinned Docker container.
Actions execute from the immutable tooling artifact, which survives historical
source checkout and post hooks. Buildx is staged from Docker's reported plugin
path rather than a runner-specific home directory.

The frontend builds once. Five independent runners compile Linux amd64/arm64,
Darwin amd64/arm64 and Windows amd64 using GoReleaser OSS snapshot builds.
Linux arm64 runners own arm64 targets. The target set and packaging flags come
from the selected source's `.goreleaser.yaml`. Each archive has a manifest bound
to the captured SHA, version, date, target, name and SHA256. Producers verify HEAD;
collection also checks Go binary build metadata. The consumer requires exactly
five archives and five manifests before any image build or publication.

Plus-specific adaptations are intentional: build metadata such as
`0.2.14+custom.002` stays in archive names; all three GoReleaser image sections
are disabled; only `release-images.sh` publishes images to
`ghcr.io/<owner-lower>/sub2api-plus`. OCI tags use
`v0.2.14-custom.002`, architecture suffixes, and numeric `latest`, `0.2`, `0`
moving tags. Context binaries live at `<arch>/linux/<arch>/sub2api`, matching
`COPY ${TARGETPLATFORM}/sub2api`, and retain executable permission. Runtime
files come from the same captured application source.

The publisher alone has write permissions and enters the `release` Environment.
It packages verified archives through GoReleaser `extra_files`; it does not
compile the application again. Checksums are materialized and compared with
verified archive bytes before external writes. Pricing assets use the selected
source's Go command, compared before external writes, and uploaded last without
`--clobber`. Existing bytes must match; API/download errors fail closed.
Cross-registry publication is not atomic. A partial publication needs inspection
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
