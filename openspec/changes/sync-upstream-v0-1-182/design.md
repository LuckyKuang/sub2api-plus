## Merge boundary

| Item | Decision |
| --- | --- |
| Starting point | latest `origin/main` |
| Carry-forward input | local `release/0.1.181-custom.001` integration |
| Official input | annotated `v0.1.182` / `5a7d469622911a6b1291a692376df5fa03f9ac2e` |
| Prepared Plus version | `v0.1.182+custom.001` |
| Publication | none during this change |

The carry-forward merge preserves prior Plus conflict resolutions. The official
merge is then reviewed as the v0.1.181-to-v0.1.182 delta, rather than repeating
the already-resolved v0.1.179-to-v0.1.181 integration.

## Conflict policy

For Responses Lite, retain the official normalization and precision behavior,
then apply the Plus prompt-cache identity, account-scoped identity, security
audit, and model-policy stages in their established order. Image generation
keeps the Plus outbound identity and multipart limit while adding the official
instruction that preserves the user image prompt verbatim.

No existing migration is modified. The existing Plus 229–233 migration set is
retained, and no v0.1.182 migration is introduced.
