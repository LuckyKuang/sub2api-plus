Sub2API Plus v0.2.18+custom.003

## Highlights

- Correct the fallback pricing layout in all five binary archives and install the resources used by fresh and upgraded binary deployments.
- Require exact fallback pricing paths, source bytes, and clean-source build provenance before release artifacts can be uploaded.
- Propose marking `v0.2.18+custom.002` as withdrawn without replacing its immutable tag or assets.

## Fixed

- GoReleaser now stores the pricing JSON at `resources/model-pricing/model_prices_and_context_window.json`, matching the application's default. The `.002` archives instead stored a regular file named `resources/model-pricing`.
- The binary installer validates and installs packaged pricing resources. Previously it copied the executable and deployment files without the resources tree, so a new installation could miss its local pricing catalog.
- Leaf and publisher archive verification check the exact regular-file path and compare the pricing bytes with the captured source. Checksums and Go binary metadata alone cannot establish the resource layout.
- Compilation checks the captured commit and clean Git status before and after the build. Only exact workflow staging paths are ignored. Both archive gates require complete, unique Go VCS metadata with `vcs=git`, the captured `vcs.revision`, and `vcs.modified=false`; linker declarations or disabled VCS stamping cannot substitute for these checks.

## Compatibility and migration

The official baseline remains `v0.2.18` at `ac1b8f1271be0d674fa6f074403c1410f30a3a81`. No new SQL migrations or provider behavior changes are introduced by this packaging correction. The seven-provider capabilities and the 11 forward-only migrations introduced with `.002` retain their documented compatibility and recovery requirements. Back up PostgreSQL before upgrading from `.001`; an image downgrade cannot restore data removed by cleanup migrations.

Binary installations need the complete corrected archive and its `resources/` directory. The installer rejects missing, empty or incorrectly placed fallback resources rather than reporting a successful installation. Review custom installations that copy only the executable. The default service working directory and `pricing.fallback_file` must agree with the installed resources path.

The `.002` tag and assets are preserved. Its separate Docker resource-copy path did not use the incorrect binary-archive path; static registry verification does not establish runtime health or live-account behavior. See the release process and supported-provider documentation for those separate acceptance boundaries.

The audit also found `vcs.modified=true` in all five inspected `.002` binary
executables. The previous workflow left generated release staging files visible
to Git; an actual Go build reproduced this flag with unchanged tracked source.
The flag alone does not identify changed files or establish that application
source was modified. The previous archive gates did not enforce compiler VCS
cleanliness; this candidate closes that gap independently of the confirmed
pricing archive defect.

## Known issues

- Binary archives require the complete resources tree; manually copying only the executable does not provide the local pricing catalog.
- All seven-provider scope and real-account restrictions documented for `.002` continue to apply. This correction does not add official agent workspace/tool loops, Grok enterprise features, additional Files combinations, StepFun native Responses/Messages/WS or live regional/plan entitlement guarantees.

## Upstream baseline

Official release: v0.2.18
Official commit: ac1b8f1271be0d674fa6f074403c1410f30a3a81
