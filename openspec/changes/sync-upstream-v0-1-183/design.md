## Merge boundary

| Item | Decision |
| --- | --- |
| Starting point | completed `release/0.1.182-custom.001` |
| Official input | annotated `v0.1.183` / `e8cb019fabf8b55199436229044cbf9aa7a82564` |
| Prepared Plus version | `v0.1.183+custom.001` |
| Publication | none during this change |

The 182-custom line already contains `origin/main` and the resolved 181/182
Plus integration. The official merge is reviewed as the v0.1.182-to-v0.1.183
delta, rather than repeating those earlier conflict resolutions.

## Conflict policy

Keep Plus module paths and `codexSessionIDHeader`. Keep Plus
`bindOpenAIStickySessionAccount` while adding official `stickySpillover` so a
full wait queue cannot rewrite the durable binding. Keep Plus
`IsOpenAIRequestBodyTooLarge` / `IsOpenAIProxyRetryBufferLimit` and adopt
official `newOpenAIAccountFailoverErrorWithClassificationHeaders` for OAuth
429 quota classification.

No existing migration is modified. The existing Plus 229–233 migration set is
retained, and no v0.1.183 migration is introduced.
