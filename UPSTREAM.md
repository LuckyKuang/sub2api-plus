# Upstream Mapping

This file is the authoritative mapping of Plus tags to official baselines and
publication status. Release naming and procedures live in
[RELEASING.md](docs/RELEASING.md).

## Integrated Baseline

The current tree integrates official `v0.2.19`, peeled commit
`dece26153621959ccd7635814f4b2d08304e681a`, following the previously imported
`0a2a29b32e6724714c69822296ccd05a6b86bfdd` snapshot. Plus is the authoritative
implementation when upstream behavior conflicts with an intentional Plus contract.
The candidate release is `v0.2.19+custom.001`; its mapping remains `planned`
until publication and immutable artifact verification complete.

The v0.2.19 follow-up adds configurable Gemini same-account retries, bound-cache
pool retries, declared-model discovery, current Gemini catalogs, model-cache
singleflight rechecks and channel web-search dimension preservation. Bound-cache
retries retain the first authorized credential owner and proxy route. The frontend
retains Plus shared controls and every supported platform's model options.
GPT-6.1 Terra already has Plus catalog/forwarding support; this release adds
verified official pricing, including cache writes, Fast/Flex and long-context
rates, while preserving explicit operator prices and frozen request billing.
Importing the official tag does not change any historical release mapping.
For OpenAI, xAI, DeepSeek, Kimi, MiniMax, StepFun and Zhipu, the mapped official
agent sources in [OFFICIAL_AGENT_BASELINES.md](docs/OFFICIAL_AGENT_BASELINES.md)
govern external protocol behavior. Wei-Shaw/sub2api patches are reviewed against
those sources; Plus credential ownership, outbound identity, ingress audit,
strict proxy binding and frozen billing contracts remain mandatory.

This import adds scoped administrator keys, owner-bound Grok catalog diagnostics,
and opt-in image-count billing for dedicated image requests. New upstream SQL
migrations 247/248 are imported as 302/303, preserving deployed Plus migrations.
Codex keeps its manually selected 0.165.0 identity and reviewed Terra descriptor;
Terra API pricing/specifications are verified separately from the pinned client
descriptor. Shared domestic-provider keyword/90% quota cooldown heuristics remain
deferred. DeepSeek Pro pricing no longer switches
to Flash on a calendar date, following the provider's continued Pro service.
The image-count option retains frozen prices and factors, rejects unidentified
prices, and does not reclassify mixed Messages/Chat/Responses/WS requests.
The follow-up review adds actual Terra forwarding regressions, consistent
DeepSeek cache usage across protocol bridges, captured-owner Grok failure
handling, complete image-size admission pricing, and nonblocking scoped OAuth
refresh when another refresh owns the lock. The requirement-to-test mapping is
maintained in [the seven-provider matrix](docs/SEVEN_PROVIDER_TEST_MATRIX.md).

Preserve intentional Plus behavior during every import. Current contracts live
in [outbound identity](docs/OUTBOUND_IDENTITY.md),
[ingress audit](docs/SECURITY_AUDIT_CONTENT_COVERAGE.md),
[provider/protocol documentation](docs/README.md),
[pricing](docs/CHANNEL_PRICING.md) and
[upgrade prerequisites](backend/migrations/README.md#upgrade-prerequisites).
Completed integration narratives remain in Git history.

The v0.2.18 integration preserves Plus Codex account/global/default identity
priority and complete configured client fingerprints. Official TUI-to-CLI
normalization does not override accepted Plus identities. Anthropic/Gemini
stream cancellation and upstream-error handling retain Plus partial usage,
completion diagnostics, timing, audit ordering, channel pricing and billing
idempotency. Vertex account quotas and Gemini image permissions use the same
Plus security boundaries. Official migration 246 is imported as Plus 295;
historical migrations remain unchanged. Plus retains its container CI/release
workflows, vendored SheetJS dependency and stronger dependency security floors.

The Codex egress review merges Plus `main` at
`0b5acf5796b40a695f1e42ebc01aa48875283823` and compares the client contract with
openai/codex `770ed6dcea40fedb83bb831f6d5e3760a933cc9a`. OpenAI OAuth and API-key
accounts keep native Codex identity; compatible upstream suppliers retain their
explicit family mapping. Codex version changes are maintained manually in code
or by an explicit administrator pin, without remote version synchronization.
The existing Plus identity priority, fingerprint controls and ingress audit
boundaries remain governed by the owning contracts above. Historical migrations
from both branches retain their complete filenames and checksums.

The October 28 Codex protocol review advances the agent baseline to
`154e8736a51224e12b2d990753622a2cd8118f54` and manually selects stable
`rust-v0.165.0`. It aligns policy-capacity errors, government API-key routing,
named tool context, workspace session import and Terra catalog metadata.
The exact sources and boundaries are recorded in
[OFFICIAL_AGENT_BASELINES.md](docs/OFFICIAL_AGENT_BASELINES.md); the immutable
Plus identity precedence, credential-owner isolation and ingress audit ordering
remain authoritative. This source alignment does not publish a Plus release.

The seven-platform synchronization starts from Plus branch commit
`90d9f67e0fc891bee6e9d13d97271d8daf7d1924` and merges Plus `main` at
`1818c299307ac394c0e3101b45d6c6ef22515c0c`. The existing official-agent baselines
in [OFFICIAL_AGENT_BASELINES.md](docs/OFFICIAL_AGENT_BASELINES.md) govern OpenAI,
xAI, DeepSeek, Kimi, MiniMax, StepFun and Zhipu. Codex retains its manually
selected version default, credential-owner/global/default precedence, trusted
fingerprints, model-rejection failover and audit boundary. Compatible upstream
fixes add semantic-output-aware overload recovery, stream cancellation billing,
quota/scheduler improvements and authenticated Redis health checks. DeepSeek
image aliases retain the branch's deployed compatibility behavior. Migrations
290–294 and the existing console UI remain intact.
The acceptance review also refreshes every bound account after proxy mutations
and keeps native provider account-test model catalogs separate from Claude's
defaults. These fixes retain the existing identity and protocol contracts.
The independently deployed model-visibility and Gemini-permission migrations
both retain prefix 295 with their complete original filenames and checksums;
future migrations start at 296. This is a branch integration, not publication
of a new release.

## Current Version

```text
Git/GitHub: v0.2.19+custom.001
Application: 0.2.19+custom.001
GHCR: ghcr.io/luckykuang/sub2api-plus:v0.2.19-custom.001
```

## Release Mapping

| Custom Release | Official Release | Official Commit | Status |
| --- | --- | --- | --- |
| `v0.1.164+custom.001` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.164+custom.003` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.164+custom.004` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.164+custom.005` | `v0.1.164` | `cd8bb98c44303b2c8f04c0da340447c992f0cb7d` | historical |
| `v0.1.165+custom.001` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.165+custom.002` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.165+custom.003` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.165+custom.004` | `v0.1.165` | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` | published |
| `v0.1.166+custom.001` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.002` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.003` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.004` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.005` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.006` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.007` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | invalid |
| `v0.1.166+custom.008` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.009` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.166+custom.010` | `v0.1.166` | `dc893dd0b8eab41df5be595ae9fcd1aa74a062b8` | published |
| `v0.1.168+custom.001` | `v0.1.168` | `99c8e4bf7564823bafbab369acab6539e734c1bb` | published |
| `v0.1.169+custom.001` | `v0.1.169` | `26d894ef4f50645a4bf1030e378ac892f17d0223` | published |
| `v0.1.169+custom.002` | `v0.1.169` | `26d894ef4f50645a4bf1030e378ac892f17d0223` | published |
| `v0.1.170+custom.001` | `v0.1.170` | `c043c24774228ba891ddf90d783aa6dc7d0855b5` | published |
| `v0.1.170+custom.002` | `v0.1.170` | `c043c24774228ba891ddf90d783aa6dc7d0855b5` | published |
| `v0.1.171+custom.001` | `v0.1.171` | `f0e7a9c7a23a7d02fb159b62fa809621eb0475a6` | published |
| `v0.1.172+custom.001` | `v0.1.172` | `155c494964c3ea6ecc31f52679525c1034bf0f16` | published |
| `v0.1.173+custom.002` | `v0.1.173` | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` | published |
| `v0.1.173+custom.003` | `v0.1.173` | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` | published |
| `v0.1.173+custom.004` | `v0.1.173` | `29009f0b2ea14edf3b11ae2564fb617ff91a03b4` | published |
| `v0.1.176+custom.001` | `v0.1.176` | `e803e3851c0a7e222cfadeafad7b8636ab959d11` | published |
| `v0.1.176+custom.002` | `v0.1.176` | `e803e3851c0a7e222cfadeafad7b8636ab959d11` | published |
| `v0.1.177+custom.001` | `v0.1.177` | `073e92d17178a1ccdb0a27017f572f10c9c7ab62` | published |
| `v0.1.177+custom.002` | `v0.1.177` | `073e92d17178a1ccdb0a27017f572f10c9c7ab62` | published |
| `v0.1.177+custom.003` | `v0.1.177` | `073e92d17178a1ccdb0a27017f572f10c9c7ab62` | published |
| `v0.1.178+custom.001` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.002` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.003` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.004` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.178+custom.005` | `v0.1.178` | `e0c48a19ed794a565e3858662520afe0a1f9f0ba` | published |
| `v0.1.183+custom.001` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.1.183+custom.002` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.1.183+custom.003` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.1.183+custom.004` | `v0.1.183` | `e8cb019fabf8b55199436229044cbf9aa7a82564` | published |
| `v0.2.0+custom.001` | `v0.2.0` | `aa236488351eb71e120fc2b6fb32e36b0374c918` | published |
| `v0.2.0+custom.002` | `v0.2.0` | `aa236488351eb71e120fc2b6fb32e36b0374c918` | published |
| `v0.2.0+custom.003` | `v0.2.0` | `aa236488351eb71e120fc2b6fb32e36b0374c918` | published |
| `v0.2.1+custom.001` | `v0.2.1` | `578785ee7fb35030b094b69624efe25670a36f5f` | published |
| `v0.2.1+custom.002` | `v0.2.1` | `578785ee7fb35030b094b69624efe25670a36f5f` | published |
| `v0.2.1+custom.003` | `v0.2.1` | `578785ee7fb35030b094b69624efe25670a36f5f` | published |
| `v0.2.4+custom.001` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.002` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.003` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.004` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.005` | `v0.2.4` | `5de5e2bed035d43591a2e10e51f420ef6a84eb98` | published |
| `v0.2.4+custom.006` | `v0.2.4` | `badfad8b7248b8aac0e6b503a06e392aa31cb294` | withdrawn |
| `v0.2.5+custom.001` | `v0.2.5` | `86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea` | published |
| `v0.2.7+custom.001` | `v0.2.7` | `aea725f2ea644d5592d0bbb1d63b607efa7e200a` | published |
| `v0.2.8+custom.001` | `v0.2.8` | `fd80b08c90b55edcad5b00171b53f08721d30da1` | published |
| `v0.2.8+custom.002` | `v0.2.8` | `fd80b08c90b55edcad5b00171b53f08721d30da1` | published |
| `v0.2.9+custom.001` | `v0.2.9` | `4c00df2e0183e2c70b7fa8ba45914205e36aad0c` | published |
| `v0.2.10+custom.001` | `v0.2.10` | `2f3fed2fdb0787141294cec81487a5df30426f7f` | published |
| `v0.2.11+custom.001` | `v0.2.11` | `96f4c115c9749078f90cbf210a01d39baf3f53b6` | published |
| `v0.2.11+custom.002` | `v0.2.11` | `96f4c115c9749078f90cbf210a01d39baf3f53b6` | published |
| `v0.2.13+custom.001` | `v0.2.13` | `3040209f205472038c1ba745a1bedd2edd9053b1` | published |
| `v0.2.14+custom.001` | `v0.2.14` | `0363b8cdba8cec3e2ba4b2dbd49c4481143fa55d` | published |
| `v0.2.14+custom.002` | `v0.2.14` | `0363b8cdba8cec3e2ba4b2dbd49c4481143fa55d` | published |
| `v0.2.14+custom.003` | `v0.2.14` | `0363b8cdba8cec3e2ba4b2dbd49c4481143fa55d` | published |
| `v0.2.14+custom.004` | `v0.2.14` | `0363b8cdba8cec3e2ba4b2dbd49c4481143fa55d` | published |
| `v0.2.15+custom.001` | `v0.2.15` | `f2669c8cf62555cd92389b3f55920e9e6e7c6ff2` | published |
| `v0.2.15+custom.002` | `v0.2.15` | `f2669c8cf62555cd92389b3f55920e9e6e7c6ff2` | published |
| `v0.2.15+custom.003` | `v0.2.15` | `ab80abc6d1f929a6864c8804fd077b509d21fda2` | published |
| `v0.2.17+custom.001` | `v0.2.17` | `060d0160417782cd962a0c7f460f6cd4565d4916` | published |
| `v0.2.18+custom.001` | `v0.2.18` | `ac1b8f1271be0d674fa6f074403c1410f30a3a81` | published |
| `v0.2.18+custom.002` | `v0.2.18` | `ac1b8f1271be0d674fa6f074403c1410f30a3a81` | withdrawn |
| `v0.2.18+custom.003` | `v0.2.18` | `ac1b8f1271be0d674fa6f074403c1410f30a3a81` | published |
| `v0.2.19+custom.001` | `v0.2.19` | `dece26153621959ccd7635814f4b2d08304e681a` | planned |

`v0.2.15+custom.003` records the integrated official main snapshot in the
Official Commit column. Its tagged official baseline remains
`v0.2.15` / `f2669c8cf62555cd92389b3f55920e9e6e7c6ff2`.

`v0.2.17+custom.001` records official tag `v0.2.17` / peeled
`060d0160417782cd962a0c7f460f6cd4565d4916`. Official tag VERSION is `0.2.16`;
Plus writes `0.2.17+custom.001`.

`v0.2.18+custom.001` records official tag `v0.2.18` / peeled
`ac1b8f1271be0d674fa6f074403c1410f30a3a81`. Official tag VERSION is `0.2.17`;
Plus writes `0.2.18+custom.001`. Its mapping is finalized after publication.

`v0.2.18+custom.002` added the seven-provider gateway work, but is withdrawn after
its post-publication audit found that binary archives placed fallback pricing at
the wrong path and the installer omitted resources. Its immutable tag and assets
are preserved. Docker's separate resource-copy path is outside this archive defect.

`v0.2.18+custom.003` prepares the binary archive and installer correction on the
same official baseline. Its mapping is finalized only after publication and
verification of the immutable release artifacts.

`v0.2.19+custom.001` records official tag `v0.2.19` / peeled
`dece26153621959ccd7635814f4b2d08304e681a`. Official tag VERSION is `0.2.18`;
Plus writes `0.2.19+custom.001`. Official migrations 247/248 already entered
Plus as immutable 302/303 during the preceding snapshot import; this tag adds
no duplicate migrations. Seven-provider agent identities, audit boundaries,
strict proxy ownership, frozen billing and the Plus UI remain authoritative.

`v0.2.4+custom.006` is marked withdrawn because official `v0.2.5` was imported before that snapshot overlay was published. Do not reuse or retag `.006`.

`v0.1.166+custom.007` is marked invalid because its tag contains embedded and
documented version `0.1.166+custom.006`. Remote Release and OCI artifact status
still require a maintainer audit. Do not reuse or retag `.007`.

## Repository Roles

- `origin`: [Sub2API Plus](https://github.com/LuckyKuang/sub2api-plus), the
  installation, update, rollback and release source.
- `upstream`: [official Sub2API](https://github.com/Wei-Shaw/sub2api), the source
  input for imports.

Update this mapping in the same change as an upstream integration. Keep tag,
embedded version and OCI naming synchronized through the release workflow;
published tags and artifacts are immutable.
