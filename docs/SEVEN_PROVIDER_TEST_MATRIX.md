# Seven-provider business regression matrix

Current implementation starts at `fix/seven-platforms-main-sync@27c5e62f4` and
adds the capability work below. Its inherited merge `85fb3c82c` combined
`2dab72aac` with surgeonfish checkpoint `e2bbc80dc` (based on `1818c2993`).
Both parents contribute implementation and regression requirements. Prior
validation records apply only to their respective source trees. The
[capability-completion evidence](#capability-completion-verification) is separate
from the [historical merge verification](#current-merge-verification). The latest
[post-commit branch verification](#post-commit-branch-verification) records the
subsequent review fixes and their complete local validation.

Acceptance scope: all gateway business capabilities currently implemented for
these seven platforms. The [supported-business inventory](SEVEN_PROVIDER_SUPPORTED_BUSINESS.md)
defines applicable account types, protocols and platform-specific limits;
unimplemented official-agent capabilities remain explicitly unsupported.

This is a requirement inventory, not a coverage-percentage claim. A test name
records a concrete scenario and its acceptance oracle. It does not prove every
provider, account type, transport, or operation unless the test's case table
actually exercises those dimensions. All gates run in the container environment
defined by [CONTRIBUTING.md](../CONTRIBUTING.md).

The authoritative source/agent mapping and reviewed revisions are in
[OFFICIAL_AGENT_BASELINES.md](OFFICIAL_AGENT_BASELINES.md). Identity precedence,
wire declarations, and versionless exceptions belong to
[OUTBOUND_IDENTITY.md](OUTBOUND_IDENTITY.md); state ownership belongs to
[OFFICIAL_AGENT_REQUEST_STATE.md](OFFICIAL_AGENT_REQUEST_STATE.md); ingress audit
ordering and extraction belong to
[SECURITY_AUDIT_CONTENT_COVERAGE.md](SECURITY_AUDIT_CONTENT_COVERAGE.md).

Test symbols below are Go tests under `backend/internal/` unless stated
otherwise. A source-level mapping becomes release evidence only when its
applicable unit/integration profile passes on the final tree. Unit mocks do not
prove PostgreSQL locking, rollback, outbox atomicity, or multi-replica storage.

## October 28 upstream follow-up requirements

These scenarios supplement the pre-existing seven-provider matrix for the
`0a2a29b32` import. They do not change the pinned official client identities.
Kimi, MiniMax and StepFun share the native refresh locking regression; Zhipu's
unsupported business-login refresh remains unsupported. Existing all-provider
identity, audit, proxy, failover and billing scenarios still apply to shared code.

| Business requirement | Regression and observable acceptance |
| --- | --- |
| Terra Messages compatibility preserves the selected model, explicit effort and trusted identity | `TestGPT61TerraMessagesForwardBoundary`: OAuth/API key, account/global identity, suffix/mapping/explicit override; actual outbound Responses body, URL, auth, UA/Originator/Version and downstream response. `TestGPT61TerraMessagesDisabledThinkingStopsBeforeUpstream` asserts the existing local rejection policy and no upstream request. |
| DeepSeek's documented cache hits survive every accepted Chat bridge | `TestDeepSeekCacheUsageProtocolBridges` and `TestDeepSeekChatCacheUsageSurvivesDownstreamProtocols`: raw Chat, Responses and Messages, JSON/SSE, authoritative zero, independent operator-price settlement and ledger debit. Buffered typed Responses decoding must preserve the same alias priority as the accounting parser. |
| A Grok discovery failure cannot modify a repaired route or replacement grant | `TestGrokCatalogBackgroundTransportAndProxyBodyFailuresNeverWrite`, `TestGrokCatalogEarlyFailureCannotAdoptReplacementOwner`, `TestGrokCatalogPersistMissingCapturedOwnerNeverReadsOrWrites`: interrupted response, oversized/interrupted 407, early configuration failure, missing owner, unchanged-owner and genuine upstream-failure controls. |
| A forced image-count reservation cannot use a known size to price a missing size | `TestImageCountBillingRequiresEveryPotentialSizeBeforeReservation`: OpenAI/Grok, in-memory/durable frozen quote, each missing size, maximum known price, explicit zero, and no financial writes for unpriced settlement. Existing `fail_closed_on_unpriced` controls admission when no priced reservation can be obtained. |
| Scoped OAuth refresh cannot hold the account row while waiting for another refresh to persist its grant | `TestScopedAdminRefreshBusyDoesNotWaitWithAccountRowLocked`: Kimi/MiniMax/StepFun report busy, roll back, issue no second provider request, and allow the first rotation to finish. `TestOAuthRefreshTransactionWithoutContentionPreservesDistributedLockAndCAS` retains uncontended success and distributed lock failure behavior. |
| Catalog history and management selection remain usable | `GrokModelDiagnosticsPanel.spec.ts` distinguishes the current endpoint from historical models; `CheckboxDropdown.spec.ts` and real Chromium layout cases cover keyboard entry, repeated selection, disabled state, Tab order and Escape. Scoped-key UI tests retain explicit permissions and saved rules. |

`repository/TestScopedAdminRefreshBusyRollsBackRowLockBeforeOrdinaryRotationPersists`
adds real PostgreSQL evidence: a separate connection observes the scoped row
lock, busy rolls back and releases it, then the ordinary refresh commits the
rotated grant and exactly one scheduler outbox event. The Grok catalog CAS
integration tests retain real concurrent-writer and replacement-owner checks.

The unit, integration and frontend coverage manifests require these regression entrypoints;
Chromium layout checks remain a separate container command. Validation results
must be obtained on the current tree, not inferred from this mapping.

## Required dimensions

| Dimension | Cases that require distinct evidence |
| --- | --- |
| Credential ownership | OAuth, API key, applicable setup-token/AgentIdentity, selected shadow versus real parent, warm versus cold cache, missing/ambiguous evidence, reauthorization and ordinary refresh |
| Physical send | Native HTTP, SSE, native WS where supported, WS HTTP bridge, same-owner retry, account failover, probes, discovery, usage/balance, OAuth/control, media downloads and asynchronous work |
| Proxy lifecycle | Create and bind; idle edit; in-flight endpoint/auth/status edit; change proxy ID; explicit unbind; soft delete/missing hydration; timeout; refusal; proxy authentication failure; repair; expiry/backup/direct policy; rollback and stale scheduler state |
| Sessions | Same authorized tenant/conversation remains sticky; another key/user/group/conversation is isolated unless sharing is explicitly authorized; new owner remaps execution state; stale attempts cannot rebind |
| Result and side effects | Correct headers/body/identity; at most permitted retries; correct billing and terminal state; unchanged grant accepts evidence; obsolete grant cannot mutate quota, error/cooldown, cache, scheduler, reset events, or notifications |
| Failure boundaries | Preparation/auth/policy error before any send; proxy failure before any direct connection; audit before selection/concurrency/billing/upstream writes; storage failures never trigger an unconditional fallback |

An explicitly committed proxy-unbind or expiry policy is a configuration change.
It is not permission to retry a failed configured proxy through local networking.
An in-flight request retains its captured route; a fresh operation resolves the
new committed configuration. Retrying a proxy timeout must return the proxy
failure or use a separately authorized new account/route.

## Common contracts

| Business scenario | Required / forbidden effects | Concrete tests and limits |
| --- | --- | --- |
| Provider request reaches its configured proxy, which refuses or times out | Every OAuth/API-key adapter reaches its real preparation path; caller receives failure; reachable destination accepts zero direct TCP connections | `service/TestSevenProviderGatewayProxyFailureNeverDialsDirect`: seven real platform account configurations through public Chat Completions, both credential types, timeout/refusal. This is adapter evidence, not a replacement for the shared transport test. |
| Fresh auxiliary operation after proxy lifecycle changes | Bind, idle endpoint/auth edit, in-flight edit, changed ID, explicit unbind, deleted binding and repair obey the committed route; old operation retains its captured route; missing binding never becomes direct | `service/TestAuxiliarySendersProxyLifecycleUsesCommittedRouteForFreshOperation`: 36 concrete operations: Codex probe/WHAM/referral, Kimi/MiniMax/Zhipu quota, Kimi/DeepSeek balance, Grok quota/billing; 13 actual provider/auth model-discovery combinations (MiniMax native OAuth discovery is explicitly unsupported); Kimi/MiniMax native token refresh; six BigModel/Z.ai project-token saved-location/personal-discovery/team-discovery chains. OAuth login/exchange and media/download remain separate senders. |
| Auxiliary sender's proxy times out, refuses, or returns 407 | Real reachable destination sees zero direct TCP connections; no second direct request within multi-step operation | `service/TestAuxiliarySendersProxyFailureNeverDialsDirectDestination`: the listed 36 operations with distinct MiniMax control steps, WHAM details, referral-send step and BigModel/Z.ai issuance/discovery steps. `TestPreparedProviderTransportRequiresConfiguredRouteWithoutPriorBinding` covers seven provider configurations across gateway/account-test/account-test-TLS before any physical send. |
| Shared HTTP/TLS/fingerprint transport receives a failing explicit proxy | Repeated attempts retain the explicit proxy despite HTTP(S)_PROXY and NO_PROXY; timeout/refusal/407 never dial the destination | `repository/TestHTTPUpstreamProxyFailureNeverDialsDestination`. This is one shared transport contract; labeling the URL with seven provider names would not create seven adapter tests. |
| Native credential acquisition races with selected-row relinking | Actual bearer/PAT, UA and proxy remain from the acquired owner; resource ID/concurrency remain with the selected route; no extra request | `service/TestNativeAdaptersUseAcquiredOwnerAfterSelectedRelink`: DeepSeek/Kimi/MiniMax/Zhipu OAuth, gateway/account-test, same-provider and changed-provider/type relinks. `TestCodexHTTPMetadataUsesAcquiredOwnerAfterSelectedRowRelink` and `TestCodexWSProxyAndIdentityUseAcquiredCredentialOwner` cover Codex separately. |
| Native refresh completes after acquisition | Final request and terminal-error mutation use the refreshed transmitted grant; old outer token cannot supply identity/proxy or poison a replacement | `service/TestNativeRefreshUsesFinalOwnerIdentityAndProxy` covers Kimi pre-expiry, 401 recovery, models and quota; `TestNativeAuxiliaryRefreshContainsConcurrentOwnerChanges` covers Kimi models/quota and MiniMax four-step quota chain, proactive/401 refresh, unchanged grant, login/proxy endpoint/proxy auth/unbind/disabled replacement, persistence failure and cancellation; only unchanged rotation may persist/send/record quota. `TestHTTP401NativeTransportPublishesFinalSentGrant` and `TestHTTP401NativePublicEntrypointsUseActuallySentGeneration` cover terminal errors. Unsupported providers must not invent refresh. |
| Native background refresh requires its coordinator | Missing coordinator rejects before calling the issuer, persisting credentials, cleaning cooldowns, invalidating tokens or changing runtime scheduling | `service/TestNativeBackgroundRefreshRequiresCoordinator`: four required children (`deepseek`, `kimi`, `minimax`, `stepfun`). DeepSeek is a negative configuration case, not evidence that its business login can refresh. |
| Supported native background refresh commits before cleanup | Kimi/MiniMax rotation and StepFun Passport-session renewal compare the complete owner; stale login, nested metadata, route or storage failure cannot trigger cleanup/cache publication; newer cooldown/runtime failures survive | `service/TestNativeRefreshCoordinatorFencesSuccessBeforeCleanup`: 21 required children, Kimi/MiniMax/StepFun × unchanged/relinked/nested-metadata/proxy-changed/new-cooldown/store-error/new-runtime. Every child asserts one issuer request and one conditional persistence attempt. StepFun renews only its optional Passport account session and preserves the static inference key. |
| Native background issuer failures stay contained | Invalid grant, network failure, late old login and missing CAS contract cannot mutate credentials, account status, cooldown, token cache, scheduler or runtime state; missing CAS sends nothing and other failures send once without generic retry | `service/TestNativeBackgroundRefreshFailureCannotMutateGrantOrScheduling`: 12 required children, Kimi/MiniMax/StepFun × invalid-grant/provider-unavailable/late-old-login/missing-contract. These are issuer-client/repository fixtures, not additional physical proxy transport tests. |
| Unsaved credential-owner snapshots retain private lineage | Same ID=0 preview and its snapshots retain the original token/proxy after edits; independent ID=0 accounts and JSON copies cannot borrow that evidence; private identity never appears in serialized data | `service/TestUnsavedCredentialOwnerSurvivesSnapshotsWithoutCrossingAccountOrJSONBoundary`: one independent test with direct assertions, no named child cases. |
| Failed/unavailable sticky owner competes with another conversation | Eligible B replaces failed A; A's later recovery cannot steal B's conversation; another conversation can still use A; no available backup returns an error | `service/TestProviderSchedulerCommitsReplacementAndKeepsConversationsIndependent`: six non-Codex real account configurations, OAuth/API key, attempt exclusion/account disable/account deletion and unavailable backup. Codex uses the distinct canonical/shared route tests below. |
| Invalid acquired-owner evidence | No provider request or fallback to selected mutable row | `service/TestAmbiguousAcquiredOwnerStopsTransport`, `TestDomesticAmbiguousCredentialEvidenceStopsPreparation`, `TestGrokAuxiliaryUnavailableOwnerNeverSends`. |
| Global identity settings change mid-operation; later failover selects another owner | Same-owner retry retains one complete identity; next owner resolves its own valid source; inbound/SDK/foreign family never wins | `service/TestAcquiredPresetKeepsGlobalSnapshotAndUsesActualAccountCandidate`, `TestDomesticOutboundIdentityWireSnapshotAndFailover`, `TestStepFunRetrySnapshotAndFailoverResolveCredentialOwner`, `TestOpenAIIdentityContractHTTPRetrySnapshot`, `TestGrokOfficialMediaPreservesSourcePrioritySnapshotAndFailover`. |
| Permanent 401 after reauthorization | Attempt remains eligible for bounded failover; replacement account stays active; rejected CAS causes no runtime block or unconditional write | `service/TestHTTP401PermanentCredentialQuarantineUsesExactGrant`: all seven providers × OAuth/API key × unchanged/replaced. `TestHTTP401ForwardKeepsFailoverWithoutBlockingNewGrant` exercises the forwarding wrapper and persistence failures. |
| Same proxy row changes after credential failure or refresh begins | Old protocol/host/port/auth/status/deletion evidence cannot authorize error/cooldown/refresh/quota mutation | `service/TestHTTP401CredentialMutationCarriesFrozenProxyEvidence`, `TestRefreshCredentialMutationCarriesAttemptedProxyEvidence`; PostgreSQL `repository/TestCredentialProxyCASRejectsSameRowReplacement`, `TestCredentialProxyCASRejectsMissingHydrationAndChangedAccountStatus`. |
| Proxy repair and account mutation contend on locks | Only the matching committed route can mutate; rejected evidence produces zero scheduler/cache/outbox effects | PostgreSQL `repository/TestCredentialProxyCASSerializesAgainstInFlightRepair`, `TestCredentialProxyCASSerializesRepairWhileAccountIsLocked`, `TestGrokMissingProxyCASCannotQuarantineRestoredRow`; success oracle `TestCredentialProxyCASAcceptsUnchangedHydratedRoute`. |
| Proxy CRUD and scheduler publication | Omitted update fields preserved; credential clearing explicit; edits publish all bound account IDs and roll back on outbox failure; deletion rechecks live bindings and rejects before mutation/outbox; an out-of-band missing configured proxy fails without unbinding or direct fallback | `service/TestAdminProxyPartialUpdatePreservesOmittedSettings`, `TestAdminProxyUpdateCredentials`; handler `TestProxyHandlerUpdateCredentialPresence`; `repository/ProxyRepoSuite/TestUpdatesPublishEveryBoundAccountWithoutQuotaSnapshots`, `TestDeleteRejectsLiveBindingsAndPreservesDanglingBinding`, `TestProxyWriteFailurePreservesBoundAccountState` (update outbox failure and deletion rejected before outbox). These are persistence/admin tests, not all-provider next-request wire evidence. |
| Explicit expiry policy | Direct/healthy backup/none policies apply only by saved configuration; inactive backup skipped; repeated sweeps preserve original binding | PostgreSQL `repository/ProxyExpirySuite/TestSweep_DirectMode`, `TestSweep_ProxyMode_Healthy`, `TestSweep_NoneMode_KeepsAccount`, `TestSweep_SkipsInactiveBackup`, `TestSweep_RepeatedExpiryPreservesOriginalProxy`. |
| Durable typed IDs and sessions | Random canonical IDs persist across replicas/restarts; unknown owner/store failure stops preparation; new grant cannot inherit another owner's opaque state | PostgreSQL `repository/TestOutboundMappedIDRepositoryAtomicAndPersistent`; `service/TestDomesticConversationLoginEpochAndAPIKeyReplacement`, `TestDomesticOwnerScopeUsesTransmittedGrantAndShadowParent`, `TestNativeConversationSessionRoutingAndOwnerIsolation`, `TestNativeConversationSessionSurvivesRealForwardingTurnsAndFailover`. Native session tests cover DeepSeek/MiniMax; they are not a seven-provider sticky-routing test. |
| Audit before side effects | Both engines inspect the canonical current-user content; accepted HTTP/WS turns enter audit before selection, billing/concurrency or upstream writes; unknown content passes through without audit-derived blocking | handler `TestPromptAuditGatePrecedesAccountBillingAndUpstreamSideEffects`, `TestOpenAIResponsesAuditsImmutableInboundBodyBeforePostAuditSideEffects`, `TestResponsesPassthroughAuditsRawPayloadOutsideResponseCreateGate`, `TestSecurityAuditBlockingFailuresLeaveAllDownstreamCountersAtZero`; service `TestCodexWSFollowupAuditPrecedesPersistentIdentityMapping`. Extraction compatibility uses the complete canonical extraction suites linked in the owning document. |

## Provider-specific operations

### OpenAI / Codex

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| OAuth/PKCE, PAT, reauthorization, refresh and lock contention | State required and exact; PAT never invents refresh; successful refresh CAS cannot overwrite new login; cache repair requires the same durable owner | `TestOpenAIOAuthService_ExchangeCode_StateMismatch`, `TestOpenAIOAuthService_RefreshAccountToken_PATIgnoresStaleRefreshToken`, `TestOpenAIRefreshSuccessCASPreservesConcurrentAuthorization`, `TestOpenAIRefreshLockWinnerTokenRequiresSameDurableOwner`, `TestOpenAIRefreshNeverReplaysActualRejectedTokenFromCache` |
| Manual account refresh and deletion revocation through their production OAuth client | Missing/mismatched/invalid configured proxy stops before credential transmission; timeout/refusal/407 never opens a direct TCP connection; a valid proxy sends successfully; explicitly unbound account may connect directly | `repository/TestOpenAIAccountOAuthProxyFailuresNeverDialDirect` exercises each of these two public account service methods with the production repository OAuth client. Code/device network sends have separate production-client rows below; this refresh/revoke test does not establish those paths. |
| AgentIdentity bootstrap before the inference send guard | Missing/mismatched/invalid proxy and timeout/refusal/407 fail without direct TCP connections or persisted task; valid proxy and unbound cases work | `service/TestAgentIdentityTaskProxyFailuresNeverDialDirect` enters `ensureAgentIdentityTask` and uses its real auth-plane HTTP client. Its lifecycle coverage is the captured task snapshot, not every account deletion/reauthorization route. |
| PAT refresh and existing-account reauthorization route | Missing proxy stops before whoami; captured PAT and proxy survive replacement of the mutable caller; omitted reauthorization proxy selection retains the existing route; an unavailable existing route cannot become direct | `TestOpenAIPATAccountRefreshMissingProxyStopsBeforeWhoami`, `TestOpenAIPATRefreshRetainsCapturedCredentialAndProxy`, `TestOpenAIReauthorizationWithoutProxySelectionKeepsAccountRoute`. The PAT wire test proves actual credential and proxy, while the reauthorization test inspects the frozen OAuth session. |
| Explicit proxy selection before account creation or code exchange | Missing repository/deleted record/lookup error/mismatched ID/invalid protocol reject URL generation, device start and code exchange before calling the credential client | `TestOpenAIOAuthExplicitProxyMustResolveBeforeStartingOrExchanging` is a preparation/forbidden-call test. The separate code/device production-client row below supplies the distinct network scenarios; this preparation test alone does not. |
| Explicit admin privacy mutation | Missing/mismatched/invalid binding prevents send; timeout/refusal/407 cannot connect directly; late privacy result after a same-row proxy edit cannot mutate the replacement grant | `TestAccountControlSendersRequireStrictProxy/privacy` exercises the independent privacy sender and positive proxy/unbound cases; `TestAdminPrivacyRejectsOldOwnerResultAfterProxyEdit` checks the late result CAS. The CommandCode catalog cases in the same test table are not Codex evidence. |
| HTTP Responses/passthrough/Chat/Messages and metadata | Installation/thread/turn references follow the acquired owner; original logical IDs remap on failover; identity precedence stays fixed | `TestCodexHTTPMetadataUsesAcquiredOwnerAfterSelectedRowRelink`, `TestOpenAIIdentityContractOfficialClientTurnContextHeaders`, `TestOpenAIIdentityContractClientMetadataClientKeysSurviveProxy`, `TestPRI0CodexOAuthOutboundContractFrozen` |
| Native WS ctx_pool/dedicated/passthrough and HTTP bridge | Acquired route/identity retained; a new grant cannot reuse an old authenticated socket; late prewarm cannot revive old generation; old 401 cannot quarantine successor | `TestCodexWSProxyAndIdentityUseAcquiredCredentialOwner`, `TestOpenAIWSCredentialGenerationSeparatesRealSockets`, `TestOpenAIWSCredentialGenerationRejectsLatePrewarmAndReconnectsCurrentGrant`, `TestOpenAIWSCredentialGenerationLate401CannotQuarantineReplacementGrant` |
| Usage response arrives after admin edits during a real send | HTTP four adapters and WS four modes carry old grant/proxy to CAS; unchanged result accepted; new login/proxy edit/proxy removal/disabled account rejects; handler's background context/current account cannot replace WS result evidence | `TestCodexHTTPQuotaObservationUsesActualWireOwner`, `TestCodexWSQuotaResultUsesActualWireOwner`; helper/side-effect regressions `TestCodexObservationRejectsStaleGrantWithoutSecondaryEffects`, `TestCodexResultMustCarryCapturedOwnerEvidence`, `TestCodexSnapshotThrottleDoesNotSuppressSuccessorOwner` |
| WHAM, account probe, credits, compact and weekly reset | One operation uses one owner; missing proxy fails before transport; snapshots monotonic and owner-conditional; rejected late probe cannot merge stale state in memory; probes/handshake do not become inference-reset evidence; shadow windows remain separate | `TestWHAMUsesOneOwnerAcrossQuotaAndCreditRequests`, `TestWHAMProxyFailuresNeverReachDirectEndpoint`, `TestCodexUsageProbeRejectsMissingConfiguredProxyBeforeTransport`, `TestCodexAdminProbeObservationUsesActualWireOwner` (connection/compact, unchanged/login/proxy edit/unbind/disabled), `TestCodexUsageProbeUsesActualRefreshedCredentialOwner`, `TestCodexProbeThrottleTracksCredentialAndProxyGeneration`, `TestWeeklyQuotaSnapshotEntryPoints`, `TestOpenAIGatewayService_OAuthPassthrough_CompactUsesJSONAndKeepsNonStreaming` |
| PostgreSQL observation/reset and concurrent billing | Exact owner/proxy guard, no stale cache/outbox writes; weekly pending evidence cannot cross principal or refresh; reauthorization during billing lock wait prevents old queued reset | `repository/TestCodexObservationCASRejectsCredentialAndProxyLifecycleChanges`, `TestCodexObservationCASAcceptsCurrentOwnerPreservesOrderingAndNeutralOutbox`, `TestCodexObservationCASRechecksProxyAfterConcurrentEdit`, `TestCodexWeeklyResetRejectsDelayedGrantAndQueuedSuccessorReset`, `TestCodexWeeklyPendingCannotCorroborateAcrossTokenRefresh`, `TestCodexWeeklyBillingRechecksReauthorizationAfterLockWait` |
| Shared OAuth affinity versus ordinary key isolation | Explicit sharing only for the same authenticated user and allowed groups; ordinary route hashes remain key-scoped; policy replacement and another conversation/user cannot borrow binding | `TestOpenAIOAuthSharedAffinityAcrossAuthorizedKeys`, `TestOpenAIOAuthSharedAffinityRejectsChangedPolicyScope`, `TestOpenAIOAuthSharedAffinityIsolation`, `TestOpenAIOAuthSharedAffinityAllExplicitSessionSources` |
| Deferred publication of shared OAuth affinity | Selecting an uncommitted candidate writes no ordinary/shared binding; cache-only WS client-policy rejection cannot publish a route; terminally admitted OAuth accounts can publish authorized cross-key affinity | `service/TestOpenAIOAuthSharedAffinityCannotPublishDuringUncommittedSelection`; `TestOpenAIOAuthSharedAffinityCacheOnlyWSWaitsForAdmission` uses actual legacy/advanced selection and terminal admission with cross-key lookup; `handler/TestOpenAIResponsesWebSocket_CacheOnlyClientPolicyRejectionPublishesNoAffinity` runs real downstream/upstream WS and rejects every cache write for the denied OAuth candidate. |
| Canonical sticky route, takeover and continuation | Accepted owner remains canonical until commit; stale attempt cannot rebind; migration timeout/cancellation fences writes; permission/dependency failure does not silently choose another account | `TestCodexSessionTakeoverWireRegression`, `TestCodexSessionStaleAttemptCannotRebindRegression`, `TestCodexContinuationDependencyWireRegression`, `TestCodexRouteFaultsFenceTransportRegression`, `TestCodexCanonicalSharedResponseAuthorizationRegression`, `TestCodexWSMigrationDirectionsRegression` |
| Images/downloads and Live | Download follows captured route; unsafe download origins rejected; Live registration uses same identity; audit precedes sideband write | `TestImageBackfillFreezesAcquiredProxyAndRejectsUncertainPreparation`, `TestBackfillOpenAIImagesB64JSON_RejectsPrivateHosts`, `TestOpenAIIdentityContractLiveRegistrationSnapshot`, handler `TestLiveSidebandAuditHookPrecedesUpstreamWrite` |

### xAI / grok-build

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| OAuth/OIDC, SSO conversion, refresh, reconciliation | Validate before consuming session; no password authorization; preserve absent refresh rotation; stale reauthorization cannot be blocked by repair | `TestGrokOAuthServiceExchangeCodeConsumesOnlyAfterValidation`, `TestGrokOAuthServiceAuthorizePasswordAlwaysDisabled`, `TestGrokOAuthServiceRefreshTokenPreservesOriginalRefreshTokenWhenNotRotated`, `TestTokenRefreshService_ReconcileGrokOAuthSkipsStaleBlockAfterConcurrentReauthorization` |
| Account refresh versus mutable proxy repository | Old refresh token uses its captured proxy even if a later lookup would return a changed endpoint, deleted record, or lookup error; no replacement-proxy request/direct TCP connection/proxy reread | `repository/TestGrokAccountRefreshNeverMixesOldTokenWithNewProxy` uses the public account service and production Grok OAuth HTTP client. `TestGrokAccountOAuthRefreshProxyFailuresNeverDialDirect` separately covers missing/mismatched/invalid captured proxy, timeout/refusal/407 with zero direct TCP connections. It does not certify the independent OIDC discovery/userinfo/SSO/device exchanges. |
| Request and background refresh preparation dependency failures | Proxy hydration read failure or unusable captured route stops before credential transmission; provider failure cannot become account cooldown, quarantine, runtime block or cache invalidation; background failure contains the provider cycle without retrying the account | `service/TestGetRequestCredentialMapsTransientAndProviderFailuresSeparately/proxy_repository_read_failure_stops_without_account_mutation` models the actual `AccountRepository.GetByID` proxy-hydration boundary; `/unusable_captured_proxy_preparation_stops_without_account_mutation` locks the typed provider-scope request regression. `TestPathA_GrokProxyPreparationFailureContainsProviderWithoutAccountMutation` checks one read despite three allowed attempts, zero transport/side effects and immediate cycle containment. These are preparation and forbidden-call tests, not proxy transport timeout tests. |
| Explicit login proxy selection | Nonpositive ID/missing repository/deleted row/lookup failure/mismatched row/invalid protocol/empty host/invalid port stop URL generation, code exchange, SSO conversion and refresh-token validation before any credential send; proxy preparation failure leaves OAuth session usable | `TestGrokOAuthExplicitProxyMustResolveBeforeAnyCredentialSend` uses a real generated session and asserts the specific proxy error plus zero credential sends. `TestGrokOAuthExplicitValidProxyIsUsedForEveryCredentialFlow` supplies a fully identified valid proxy and checks all three credential-client route arguments. These are sender-call/preparation tests, not OIDC/SSO transport failure wire tests. |
| Responses/raw Chat, tools and side calls | Acquired owner selects destination/header overrides; cache key does not grant tools; explicit hosted/function collision handled; auxiliary prompt gets its own request ID while retaining conversation | `TestGrokAcquiredOwnerControlsPhysicalTargetAndOverrides`, `TestGrokOfficialForwardDoesNotInjectOrConvertSearchTools`, `TestGrokOfficialCacheFieldDoesNotGrantTools`, `TestGrokOfficialSideCallKeepsParentSessionAndIndependentConversation` |
| Compression discovery/retry and cache isolation | Only advertised capability compresses; withdrawal/encode failure/plain fallback affects representation only, never network route; cache keyed by owner/proxy/target | `TestGrokCompressionProbeUsesAcquiredPrincipalAndProxy`, `TestGrokCompressionCacheIsolatedByCredentialAndProxyGeneration`, `TestGrokRequestCompressionUnknownAndWithdrawnCapabilityStaysPlain`, `TestGrokRequestCompressionWireSendOverHTTPAndTLS` |
| Independent observed-models and compression-settings senders | Missing proxy/lookup failure/mismatched ID/invalid protocol stops before sending; timeout/refusal/407 never opens a direct TCP connection; valid proxy and explicit unbind follow their routes | `TestGrokIndependentControlProxyFailuresNeverDialDirect` executes each independent sender through a real HTTP proxy with a reachable direct TCP tripwire. Observed-models intentionally treats an HTTP error as a best-effort no-update result; no model result may persist. |
| In-flight Grok control request versus same-row proxy edit | Old request keeps its captured proxy; the next request uses the replacement; late observed-model result cannot update the replacement grant; malformed captured proxy cannot use a positive warm compression cache | `TestGrokIndependentControlRetainsCapturedRouteDuringProxyEdit` uses a barrier around the actual proxy send, changes endpoint and access token, and checks old/new routes and the model-result CAS. `TestGrokCompressionWarmCacheCannotAuthorizeInvalidCapturedProxy` seeds the exact warm cache key. These are two senders, not all Grok OAuth/media control operations. |
| Auxiliary HTTP, native realtime, prewarm, media identity | HTTP/WS/media retain acquired destination and identity; unavailable owner prevents send; signed declarations survive rendering | `TestGrokAuxiliaryHTTPUsesAcquiredOwner`, `TestGrokAuxiliaryRealtimeUsesAcquiredOwner`, `TestOpenAIWSConnPoolGrokPrewarmRetainsOwnerSnapshot`, `TestCoderWebSocketGrokRendererPreservesOtherSignedDeclarations`, `TestGrokOfficialMediaPreservesSourcePrioritySnapshotAndFailover` |
| Billing/quota, cooldown, free and paid plans | Billing-only refresh must not consume an inference; transient billing retry bounded; billing 429 does not pause models; stale results do not poison successor | `TestGrokQuotaServiceQueryQuotaPaidBillingSkipsActiveProbe`, `TestAccountUsageServiceGrokRefreshUsesBillingOnly`, `TestGrokQuotaServiceFetchBillingStopsAfterSingleTransientRetry`, `TestGrokQuotaServiceBilling429DoesNotPauseModelScheduling`, `TestGrokBillingLateResponseCannotAuthorizeNewCredential` |
| Account switching and media billing | Credential failover bounded; missing selected row retries healthy account without mutation; create/pending video not billed; terminal done billed once by returned dimensions | handler `TestGrokOAuthCredentialFailoverAcrossHTTPHandlers`, `TestResponsesGrok429FailoverIsBounded`, `TestGrokOAuthMissingSelectedRowRetriesHealthyAccountWithoutMutation`; service `TestGrokMediaUsageFromResponseVideoCreateDoesNotBill`, `TestGrokMediaUsageFromResponseVideoStatusBillsOnOfficialDone` |
| Protected/signed media downloads | Credential sent only to trusted upstream; range preserved; untrusted or deceptive origin rejected | `TestForwardGrokMediaContentUsesUpstreamCredentialAndStreamsRange`, `TestForwardGrokMediaContentFetchesValidatedSignedURLWithoutCredentials`, `TestForwardGrokMediaContentRejectsUntrustedSignedURL` |
| Unsupported quota reset | Explicit unsupported error, zero reset request | `TestGrokQuotaServiceResetQuotaUnsupported` |

### DeepSeek / deepseek-harness

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| OAuth PKCE/login, selected environment and login epoch | State/PKCE verified; selected environment frozen; each authorization has its own epoch; expired or rejected business login requires reauthorization, with no foreign issuer fallback | `pkg/cnoauth/TestDeepSeekPKCEAndState`, `service/TestDeepSeekOAuthPersistsSelectedEnvironmentAcrossSettingsChange`, `TestCNOAuthAuthorizationCreatesEpochAndRefreshRejectsRelink` (DeepSeek epoch assertions only), `handler/admin/TestNativeAccountsWithoutBusinessRefreshNeverUseForeignIssuer` (DeepSeek children). |
| OAuth/native API-key Messages discovery | Correct distinct auth headers; official `/models`; client minimum-version, Messages capability, capacity and modalities gates; metadata write conditional | `TestDeepSeekHarnessNativeDiscoveryCredentialsAndGates`, `TestDeepSeekHarnessDiscoveryFreezesCredentialOwnerOnRotation`, `TestDeepSeekHarnessCatalogPersistenceIsCredentialConditional` |
| Chat/Responses gateway extensions, reasoning/cache and names | Compatible endpoint retained; model ID unmodified; reasoning placeholders only where required; cached reasoning retained | `TestDeepSeekHarnessDiscoveryKeepsCompatibleProtocols`, `TestDeepSeekModelNamesForwardUnchanged`, `TestForwardResponses_DeepSeekChatFallbackInjectsReasoningPlaceholderOnCacheMiss`, `TestForwardResponses_DeepSeekChatFallbackKeepsCachedReasoningContent` |
| Session, balance and usage accounting | Persistent harness session isolates tenant/conversation/grant; delayed zero/nonzero balance cannot install/clear successor cooldown; cache and uncached token buckets billed correctly | `TestNativeConversationSessionRoutingAndOwnerIsolation`, `TestCNBalanceOwnerReplacementSeparatesFlightsAndRejectsLateState`, `TestCNProviderAnthropicUsageBillsUncachedInput` |

### Kimi / kimi-code

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| Device OAuth, cancellation and refresh | Device flow per official protocol; completion owner/single use; no token echo; cancellation/relink cannot replay grant | `pkg/cnoauth/TestOfficialDeviceProtocols`; `service/TestCNOAuthCompletionOwnerSingleUseAndNoTokenEcho`, `TestCNOAuthCancelAndRelink`, `TestCNOAuthSendRejectsChangedOwner` |
| Native Messages and Chat/Responses adapters, test and discovery | Correct bearer/device/version declaration set; pre-expiry and one allowed 401 refresh use final owner; provider/type relink cannot hijack old request; models/quota stop when refresh loses grant/proxy ownership | `TestCNOAuthHTTPAdaptersAndProbeUseNativeAuthentication`, `TestNativeAdaptersUseAcquiredOwnerAfterSelectedRelink`, `TestNativeRefreshUsesFinalOwnerIdentityAndProxy`, `TestNativeAuxiliaryRefreshContainsConcurrentOwnerChanges`, `pkg/kimi/TestDefaultIdentityRendersTheOfficialDeclarationSet` |
| Quota, balance, cache and monitor | Late exhaustion cannot poison new grant; concurrent same-account grants use separate flights; no conditional port means no unconditional write; installed cooldown cannot be cleared by earlier healthy result | `TestCNQuotaProbeReauthorizationSeparatesFlightsAndRejectsLateExhaustion`, `TestCNQuotaMonitorSeparatesConcurrentGrantsAndCachesTheHealthyOwner`, `TestCNQuotaMissingCASDoesNotWriteUnconditionally`, `TestCNBalanceSameGrantCannotReplaceCooldownInstalledDuringProbe` |
| Coding endpoint validation and cached input | Unsafe coding URLs rejected; fully cached prompt does not double count input; stream aliases normalize to billable buckets | `TestGetCodingPlanProvider_KimiRejectsUnsafeURLs`, `TestParseSSEUsagePassthroughKimiFullyCachedInputReplacesStartTotal`, `TestMergeAnthropicUsageNormalizesKimiStreamForOpenAIBilling` |

### MiniMax / minimax-code

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| Managed and BYOK declarations | Managed versionless family and BYOK SDK fingerprint remain distinct; configured source priority preserved | `pkg/minimax/TestMatrixUsesOfficialAccountClientWithoutChangingOwnerSource`, `service/TestDomesticOutboundIdentityWireSnapshotAndFailover`, `TestNativeAdaptersUseAcquiredOwnerAfterSelectedRelink` |
| OAuth session, refresh and Messages adapters | Device grant single-use; final refreshed owner supplies bearer/route/identity; terminal auth after output cannot replay on another account | `TestCNOAuthCompletionOwnerSingleUseAndNoTokenEcho`, `TestCNOAuthHTTPAdaptersAndProbeUseNativeAuthentication`, `TestHTTP401NativePublicErrorSideEffectsApplyOnceAndRespectSharedFailures` |
| Personal workspace/region quota operation | One region and personal grant throughout; refresh restarts entire scope; concurrent grant/proxy changes, failed persistence or cancellation stop all follow-up calls; unknown/failed/invalid/unlimited windows never invent exhaustion; 401 recovery bounded | `TestMiniMaxOAuthQuotaFixedRegionPersonalGrantAndControlIdentity`, `TestMiniMaxOAuthQuotaRefreshRestartsEntireBillingScope`, `TestNativeAuxiliaryRefreshContainsConcurrentOwnerChanges`, `TestMiniMaxOAuthQuotaUnknownOrFailedNeverPenalizesAccount`, `TestMiniMaxOAuthQuota401RecoveryBounded`, `TestMiniMaxOAuthQuotaLegacyUnlimitedAndInvalidWindows` |
| Sticky owner and reactive plan/credits errors | Actual exhaustion switches eligible sticky owner; replacement stays sticky; plan/credits errors do not invalidate credential; old cooldown cannot poison replacement | `TestMiniMaxOAuthExhaustionSwitchesStickyOwnerAndRetainsReplacement`, `TestMiniMaxReactivePlanAndCreditsErrorsNeverInvalidateCredentials`, `TestMiniMaxUsageCooldownCannotPoisonAnotherGrant` |

### StepFun / Step-Code

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| OAuth login, CN/global, coding/payg and API key | Official region/plan endpoints; no invented refresh; OAuth completion single-use across replicas; concurrent session read cannot lose completion receipt | `TestStepFunOAuthAndAPIKeyActualForwardAndProbe`, `TestStepFunGrantCannotEscapeRegionOrInventRefresh`, `TestStepFunLoginAcrossReplicasConsumesGrantOnce`, `TestStepFunCompletionReceiptSurvivesConcurrentSessionRead` |
| Models and unsupported capability advertising | Only discovered Chat capabilities offered; ID-only model retained without invented tools/vision; invalid/cancelled session sends nothing and does not consume usable key | `TestStepFunModelCatalogOnlyAdvertisesDiscoveredChatCapabilities`, `TestStepFunSyncPersistsIDOnlyModelsWithoutInventingCapabilities`, `TestStepFunOAuthCatalogRejectsInvalidSessionBeforeUpstream`, `TestStepFunOAuthCatalogDiscardsResponseAfterCancellation` |
| Public request adapters, stream tools, billing and restriction | Chat/Responses/Messages/probe/model paths use pinned SDK; tools/usage translate correctly; discovered restriction controls actual selection | `TestStepFunStreamingToolsAndBillingAcrossInboundProtocols`, `TestStepFunOfficialCachedUsageAcrossProtocols`, `TestStepFunModelRestrictionControlsActualAccountSelection`; handler `TestStepFunClientCatalogsRespectDiscoveryAndRestrictions` |
| Editing a model restriction after sticky binding | Removing the active model moves the conversation to the eligible replacement; restoring the original model does not steal that binding; removing replacement's model permits the original again | `TestStepFunStickyOwnerRespectsEditedModelRestriction`: OAuth/API key, actual `GatewayService` selection. |

### Zhipu / ZCode

| Operation and boundary | Acceptance oracle | Concrete tests |
| --- | --- | --- |
| Linked login and manual API keys, all implemented plans | Poll token server-side; callback session exact and single-use; each plan obtains required material; missing material fails before provider effects; manual keys remain unchanged | `TestZhipuOAuthStartLinkKeepsPollTokenServerSide`, `TestZhipuOAuthSessionIsSingleUse`, `TestZhipuOAuthBuildAccountMaterialForEachPlan`, `TestZhipuPublicCredentialManagedPlansKeepProtocolAndAuthentication`, `TestZhipuPublicCredentialMissingGrantAllowsTypedFailoverBeforeSideEffects` |
| Project-token acquisition/cache/recovery | Correct owner and estate; missing/disabled/malformed material rejected; each 401 invalidates rejected token; retry bounded; cancellation/login generation cannot leak token; identical grants on different account IDs have independent PAT issuance and rejection | `TestZhipuProjectTokenCacheOwnersRefreshAndRejection`, `TestZhipuProjectTokenEvery401InvalidatesAttempt`, `TestZhipuProjectTokenInferenceProbeAnd401Replay`, `TestZhipuProjectTokenCancellationAndLoginGeneration`, `TestZhipuProjectTokenDestinationAndOffPeakIsolation`; critical `TestProjectTokenCacheSeparatesIdenticalGrantsAcrossAccounts` checks exact PATs and issuer counts before and after one account's 401. Project-token renewal does not refresh the business login; that login requires reauthorization when invalid. |
| Independent project-quota 401 and 403 entrypoint | Explicit 401 reacquires PAT and replays once using the same owner/proxy; repeated 401 invalidates the final PAT; 403 neither replays nor evicts the PAT, and no failed observation persists | `TestZhipuProjectQuotaRecoversExplicit401Once`, `TestZhipuProjectQuotaRejectedPATRequiresFreshIssuance`, `TestZhipuProjectQuotaForbiddenDoesNotReplayOrEvict`: six required children, each test × individual-coding-plan/off-peak. The fixtures cover the BigModel personal project scope and assert actual monitor authorization and exact issuer/send counts; they do not certify the same error combinations for Z.ai/team scopes. |
| Off-peak admission/polling/settlement | No model send before admission; bounded budget includes blocked control send; active lease lasts through response read/close/cancel; credential rotation drains original owner only | `TestZhipuOffPeakForwardAndProbeRequireAdmissionBeforeAnySend`, `TestZhipuOffPeakBudgetIncludesBlockedUpstream`, `TestZhipuOffPeakForwardAndProbeLeaseLastsThroughEveryLiveResponse`, `TestZhipuOffPeakLeaseEndsOnSendErrorReadErrorCloseAndCancellation`, `TestZhipuOffPeakCredentialRotationDrainsOnlyItsOriginalOwner` |
| Physical request ID versus logical trace and selected proxy | Every physical control send gets new ID; logical trace and original owner retained; selected OAuth proxy never becomes direct or changes at exchange/create | `pkg/zcode/TestControlPreparationReplacesUntrustedRequestIDsEachTime`, `service/TestZhipuOffPeak401ControlRetryUsesNewRequestIDAndOwnerPAT`, `TestZhipuOAuthSelectedProxyCannotFallBackToDirect`, `TestZhipuOAuthProxySnapshotCannotChangeAtExchangeOrCreate` |

## Additional gateway and production-transport inventory

### Protocol sources

The audit used these independently inspected official client commits. Endpoint,
identity and capability details live in the owning provider documents.

| Official client | Inspected commit | Owning contract |
| --- | --- | --- |
| Codex | `970111f6cb62cc02fe36a82ea7d71b37a2889c97` | [Identity](OUTBOUND_IDENTITY.md), [client profiles](protocols/CODEX_CLIENT_PROFILES.md) |
| Grok Build | `2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8` | [Grok](providers/GROK.md) |
| DeepSeek Harness | `22ddee3792402c3e70ec5f86a63c3e3e895a2f60` | [Native login and controls](providers/DOMESTIC_OAUTH.md) |
| Kimi Code | `7ccac2b45b8a3481c76f771bb99c043b562de2c5` | [Native login and controls](providers/DOMESTIC_OAUTH.md) |
| MiniMax Code | `da22f11c55d4748155d7162c1030414228a00565` | [Model catalog](CN_PROVIDER_MODELS.md), [native login](providers/DOMESTIC_OAUTH.md) |
| Step-Code | `81e79bc4b699148d6823fedde9b5e70d9a80255f` | [StepFun](providers/STEPFUN.md) |
| ZCode | `3c5b227649371507ddc4eb46ac5133afe6db1cab` | [Zhipu](providers/ZHIPU.md) |

### Login, transport and lifecycle assertions

All paths must preserve the credential owner's identity and captured route.
An explicitly unbound account may connect directly; a broken configured proxy
does not authorize direct egress. Runtime expiry/fallback changes the stored
binding in its own workflow instead of silently replacing an in-flight route.
Codex's documented raw login header omissions remain unchanged.

| Business scenario | Test entrypoints | Observable boundary |
| --- | --- | --- |
| Codex authorization-code exchange and each device stage | `repository/openai_code_device_proxy_contract_test.go` | Production login client, exact request fields; missing/mismatched/invalid binding, refusal, timeout and 407 fail without direct TCP connections or leaked secrets; success retains the selected route |
| Codex refresh/revoke keep the acquired account route | `TestOpenAIOAuthAccountOperationsRetainStrictProxySnapshot` | Service-level client spies observe the captured URL; broken bindings stop before the client call, and mutable proxy repository data cannot replace the route. This test does not execute the production refresh/revoke network client |
| Grok personal browser identity is verified | `service/grok_oidc_contract_test.go`, `pkg/xai/oidc_contract_test.go` | Missing/unverified ID token rejected; signature, issuer, audience, expiry, issue time, key, algorithm and nonce checked; verified identity is authoritative; exact Team-principal exception; Redis nonce survives cross-instance callback |
| Grok discovery/JWKS use the production browser client | `TestGrokOIDCProductionBrowserProxyFailuresNeverDialDirect` | A failed configured proxy route cannot reach the metadata destination directly; discovery/JWKS carry trusted control identity and no tokens; bounded same-origin metadata. The local HTTP fixture exercises HTTP proxy requests rather than CONNECT tunnels |
| Browser callback retains its starting route | `TestGrokOIDCCallbackRetainsOriginalRoute` | Callback cannot bind a new proxy or substitute another ID; editing the same row cannot reroute the captured session |
| Grok SSO device lifecycle | `pkg/xai/sso_lifecycle_contract_test.go`, `repository/grok_sso_proxy_contract_test.go`, service SSO cancellation tests | Pending/slow-down/denial/expiry/cancellation; consent time counts toward device expiry; no new poll after expiry; late responses cannot issue credentials; refused/timed-out/407/malformed proxies cannot fall back to direct egress or expose secrets |
| Native login route resolution and relogin | `service/cn_oauth_proxy_contract_test.go` | Missing repository/row and invalid route produce safe errors before issuer calls; relogin uses hydrated owner; preview uses session route; late login/refresh cannot replace an edited owner |
| Kimi device start, poll and refresh | `pkg/cnoauth/kimi_proxy_contract_test.go` | Literal official URL/form/identity assertions; production pooled transport through refused, timed-out and 407 proxies; direct dial count stays zero |
| DeepSeek Messages API key and MiniMax BYOK Anthropic | `repository/native_messages_proxy_contract_test.go` | Public native Messages forwarder plus production HTTPUpstream; exact native path, body and API-key auth; real proxy success/failure, no direct TCP fallback or credential leakage; official defaults checked separately |
| Seven-provider Chat proxy bindings | `TestSevenProviderChatProductionRejectsBrokenProxyBindingsBeforeSend` | Production Chat forwarder plus HTTPUpstream; valid proxy and explicitly unbound direct controls; missing/mismatched/malformed/incomplete-auth bindings fail before any direct or replacement-proxy send; errors omit credentials |
| API-key balance and coding quota retain their captured route | `TestCNAPIKeyControlRejectsBrokenProxyBeforeHTTPOrStateWrites`, `TestCNAPIKeyControlUsesCapturedBoundRouteOrSavedUnbinding` | DeepSeek/Kimi payg balance and Kimi/Zhipu coding quota: 24 broken-binding cases reject before the HTTPUpstream boundary or state writes, without mutable proxy lookups or secret-bearing errors; eight positive controls check the exact captured/direct route, endpoint, authorization, account/concurrency and one observation write, preserving credentials and unrelated metadata. These are service recorder assertions, not real-proxy TCP failure tests |
| Step cloud login and optional account session | `pkg/cnoauth/stepfun_cloud_test.go`, `stepfun_test.go`, `stepfun_refresh_test.go` | Cloud init/poll contract; static model key remains separate from optional Passport session; cancelled/malformed responses cannot create a grant; missing optional session cannot resurrect old account fields |
| ZCode background ticket lifecycle | `service/zhipu_offpeak_proxy_*_test.go`, `repository/zhipu_offpeak_strict_proxy_wire_test.go`, `pkg/zcode/offpeak_official_response_test.go` | Take/Status/Settle via production client; original owner/JWT/identity/proxy retained; failure keeps ticket; restored empty 200/204 acknowledgement retires once; third sweep sends nothing; bare official JSON preserves admission and quota |
| Live create failover and persistence failures | `service/openai_live_create_business_test.go`, `service/openai_live_create_incomplete_test.go` | Actual CreateLiveCall and scheduler with real HTTP proxies; pre-creation A failure can select B. Disabled control transport rejects before create. A successful create with failed mapping save or unusable SDP cannot replay: its original grant/identity/proxy closes the known call, or capacity remains in bounded compensation while unconfirmed. Missing/invalid call location keeps a bounded reservation without invalid dialing. Ordinary slots always release; no unusable mapping is saved. Mapping and leases here use in-memory fixtures; the separate repository Live snapshot tests prove policy/identity persistence, not real Redis storage-fault atomicity |
| Alpha Search PAT metadata preflight and Live sideband route admission | `TestAlphaSearchPATMetadataRejectsBrokenProxyBeforeValidation`, `TestLiveSidebandRejectsBrokenProxyBeforeDial` | Twelve cases: each path rejects four broken bindings and accepts the captured-bound/explicitly-unbound controls, ignoring residual proxy associations after unbinding. Alpha uses real local whoami/proxy responders and a recorded inference boundary: broken routes produce zero metadata requests, inference sends or credential mutation. Live uses a dial spy: broken routes produce zero Dial calls and preserve the supplied call record; successful calls retain bearer/account/attestation and Codex identity. These tests do not perform real Live WebSocket proxy-failure handshakes |
| UI proxy changes during login | `frontend/src/components/account/__tests__/CNOAuthPanel.spec.ts`, `useCNOAuth.spec.ts` | Four native platforms; old start, poll/exchange and completion responses cannot replace a new flow; old session cancelled; timers stop on cancel/expiry/completion |
| Seven-provider management proxy lifecycle | `frontend/src/__tests__/integration/seven-provider-proxy-lifecycle.spec.ts`, `oauth-proxy-generation.spec.ts`, `ProxySelector.testing.spec.ts` | Seven platforms × OAuth/API Key: create, bind, idle/in-flight edit, failed/late probe, repair, unbind, delete and rebind; quality and cross-page batches preserve their captured routes. Real Vue components and admin API/Axios serialization with a stateful backend adapter; this is not browser/server E2E. Late OpenAI/Grok/ZCode authorization results cannot complete a replaced or closed flow; OpenAI device cancellation/unmount stops polling |
| Chromium proxy-management chain | `frontend/e2e/seven-provider-management.spec.ts`, `tools/run_seven_provider_browser.py` | Fourteen defined scenarios drive the actual login/compliance/proxy/account-edit pages, production backend, PostgreSQL and Redis. Real local proxy responders check refused routes, in-flight edits, late success, repair and zero direct geo-target TCP connections. Account records are prepared through the real admin API with synthetic stored credentials. This suite does not perform provider grants, provider-specific UI account creation, inference, refresh or billing; its final execution result must be recorded separately from component tests. |
| Seven-provider HTTP failover and subsequent affinity | `handler/seven_provider_failover_business_test.go` | API-key Chat entrypoint through production HTTPUpstream and two proxies; A fails, B succeeds; A recovers, then a new gateway instance still uses B through a shared in-memory session-binding store; actual bearer/identity/proxy authentication and slot release; two successful B billing commands with independently specified token prices, no failed-A bill. This does not execute Redis persistence or PostgreSQL debits |
| Seven-provider persisted Chat failover, affinity and billing | `integration/seven_provider_business_integration_test.go`, `TestSevenProviderFailoverPersistsRedisAffinityAndPostgresDebits` | Fourteen platform/status scenarios (503 and 429): real API-key middleware, gateway/handler, PostgreSQL repositories/debits/usage/dedup, Redis affinity and concurrency, production HTTPUpstream and two HTTP proxies. Failed A has no debit; B succeeds twice, remains sticky after A recovers and a new gateway starts, and bills independently specified $0.00006000 per response. Actual credentials/identity/proxy auth, slots during sends and final release, account/user/key quotas and replay idempotency checked |
| Seven-provider native streaming tools and persisted billing | `integration/seven_provider_business_native_integration_test.go`, `TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits` | Seven additional native scenarios: Codex/Grok Responses, DeepSeek/Kimi/MiniMax Messages, Step Plan Chat and ZCode Start Plan Messages. Actual official HTTPS destinations and verification through isolated CA subprocesses and authenticated CONNECT proxies; production HTTPUpstream, Redis and PostgreSQL. SSE tool argument fragments and second-turn tool results survive A→B failover, subsequent affinity and independently priced debits. ZCode plan requests send Bearer, not the manual API-key auth scheme; four public forwarder regressions also pin that contract |
| Live sideband WebSocket transport | `service/openai_live_proxy_wire_test.go` | Production coder WebSocket dialer and real CONNECT/TLS/WS echo: success, refusal, timeout, 407, cancellation and proxy edit after CONNECT. Failed routes have zero direct destination connections; handshake credentials/identity remain captured and record/lease state is unchanged. Only the official target host is adapted to the isolated TLS server |
| Grok auxiliary HTTP and Realtime strict admission | `service/grok_auxiliary_strict_proxy_test.go`, `grok_admin_strict_proxy_test.go`, `handler/grok_realtime_owner_preflight_test.go` | Voice, image, video, status/content, compression, eight admin probe modes and the actual Realtime handler validate the captured binding before credential lookup, cache or send; malformed routing cannot invalidate a healthy credential; acquired concurrency slots are released on rejection |
| Grok auxiliary production transports | `repository/grok_auxiliary_proxy_wire_test.go`, `service/grok_realtime_proxy_wire_test.go` | 60 HTTP and 24 WebSocket scenarios: API Key/OAuth, independent media/status/content or open/probe paths, success/refusal/timeout/407/cancellation/in-flight proxy edit. Actual HTTPUpstream/WebSocket clients, authenticated proxies and TLS targets verify identity, original route and zero direct fallback |
| Grok delayed errors and recovery | `service/grok_request_outcome_owner_test.go`, `grok_model_runtime_owner_test.go`, `handler/grok_realtime_late_rejection_test.go` | Voice/media/Responses/Chat errors and successful quota recovery compare the captured credential, route and scheduling state. Account/model runtime cooldowns belong to that owner even if replacement happens after SQL commit; real team/model limits still apply to siblings of the captured team. Caller transactions without a commit hook cannot authorize runtime publication. Realtime late 401 uses the captured handshake owner; 407 does not invalidate credentials |
| Grok atomic request observations | `repository/account_repo_grok_request_outcome_integration_test.go` | Actual PostgreSQL owner/scheduler/proxy CAS, Extra and outbox commit together; concurrent account/proxy writers, storage failure and caller rollback cannot publish replacement state. Recovery cannot clear a newer rate limit; model cooldown updates merge concurrent sibling models |
| Grok token acquisition and refresh ownership | `TestGrokRequestTokenAcquisitionRetainsCapturedOwner`, `TestGrokManualTokenRefreshRetainsCapturedOwner`, `TestGrokOwnedTokenRotationPreservesOfficialProfileMetadata` | Cache, version adoption, lock waiting, issuer response and committed rotation cannot borrow a replacement login, route or identity. Same-grant issuer scope/email changes succeed; changed subject, team, client, base URL or identity fail before a mixed-owner send |
| Grok diagnostic 429 persistence | `TestAccountTestService_Grok429PersistsRateLimitReset`, `TestAccountTestService_Grok429WithoutQuotaHeadersUsesFallback` | Service repository recorders require one owner-conditional observation call to save the actual reset from Retry-After or the documented fallback; an ID-only legacy rate-limit write cannot bypass that port. These tests establish service behavior; PostgreSQL atomicity is exercised by the request-observation integration cases |
| Grok credential failure and repair boundaries | `service/grok_credential_failure_test.go`: `TestGetRequestCredentialMapsTransientAndProviderFailuresSeparately`, `TestCredentialFailureCASMissDoesNotRecoverIneligibleLatestCredential`, `TestGetRequestCredentialSharedCredentialPersistenceFailureStopsWithoutAccountMutation` | A selected owner already missing its refresh credential is blocked only through the conditional owner mutation; a concurrent repair wins that mutation. Refresh removal after selection rejects the replacement owner before issuer work without quarantine or cache deletion. Permanent/transient CAS misses preserve the latest owner's independent runtime block while retiring the stale owner's speculative block. Failed refresh persistence stops as a shared provider failure, preserving the original credentials, base URL and account scheduling state without deleting cached credentials. These assertions use service repository/cache fixtures |
| Grok issuer-failure proxy owner and recovery grant | `service/grok_credential_failure_owner_test.go` | Permanent and transient issuer failures compare the captured proxy endpoint, authentication and lifecycle even when its ID is unchanged. Display-only edits retain normal failure handling. A failed quarantine CAS may adopt a concurrent access-token repair only for the same refresh grant, subject/team/client, identity, base URL and route; replacement logins require reselection without token/cache/runtime leakage. Two top-level tests pin 36 child scenarios using service repository/cache fixtures. |
| Grok credential-failure quarantine persistence | `repository/account_repo_grok_credential_failure_owner_integration_test.go` | Real PostgreSQL exercises both permanent and transient quarantine entries, matching direct/bound controls, same-row proxy edits and lock waits. Account state and scheduler outbox share the transaction; outbox failure and caller rollback publish no state/cache changes, while managed commit publishes once. A still-missing bound proxy may quarantine its captured account; committed or in-flight same-ID restoration wins the locked recheck. Seven top-level tests pin 36 child scenarios plus the independent concurrent-restoration test. |
| Existing-account OAuth apply and single/batch refresh | `repository/account_repo_admin_oauth_integration_test.go`, `account_repo_oauth_owner_integration_test.go`, `dto/account_oauth_owner_test.go` | Real handlers and PostgreSQL compare the initiating opaque owner and the final locked owner. Missing/malformed owners reject with 400, stale owners with 409. Credentials, supported Extra, recovery and outbox commit together; conflicts and storage/outbox failures leave no partial write, token-cache invalidation or recovery |
| Existing-account authorization UI | `frontend/src/__tests__/integration/reauth-owner-generation.spec.ts`, `native-reauth-management.spec.ts`, `views/admin/__tests__/AccountsView.oauthOwner.spec.ts`, `api/__tests__/admin.accounts.oauthOwner.spec.ts` | Real components and API/Axios serialization with a substitute backend cover OpenAI authorization-code/RT/device flows, Grok authorization-code/RT/SSO flows, and DeepSeek/Kimi/MiniMax/StepFun menu/editor reauthorization. Late grant, apply, start, poll and account-reload results cannot alter a replacement flow. Opaque owner changes propagate through list refresh; unrelated observations retain the active grant. API client spies verify that apply requires the initiating owner and sends no POST when it is missing or malformed. Unsupported DeepSeek/Zhipu refresh and Zhipu in-place reauthorization stay absent from the menu; these component tests do not perform provider authorization |
| Captured runtime recovery and reset publication ordering | `TestOAuthCredentialRecoveryOnlyClearsCapturedRuntimeGeneration`, `TestGrokManualResetRejectsPreResetOutcomePublication` | Successful authorization clears only captured OpenAI/Grok failure generations; newer failures, replacement owners and team limits survive. An observation committed before a same-process reset cannot publish its old local effect after reset completes |
| Persisted recovery and cross-process model admission | `integration/grok_cross_instance_recovery_integration_test.go`, `openai_model_reset_integration_test.go`, `repository/account_repo_runtime_recovery_generation_integration_test.go` | The process retaining an actual Grok model failure or OpenAI API Key transient circuit recovers after another process resets the account and the production outbox advances. Recovery generation commits atomically; imports/stale Extra edits cannot forge or erase it. Grok effects use the generation returned by their SQL commit. Later real failures still exclude the recovered model; historical successes cannot clear the new generation |
| Independent scheduling recovery and management status | `TestSevenProviderPublicSchedulingRecoveryIsAtomic`, `TestSuccessfulTestRecoveryCommitsOnlyRequiredState`, `TestSevenProviderTempUnschedStatusUsesAuthoritativeSavedState` | Seven-provider rate/temp reset modes preserve account error, pause and expiration settings while clearing their documented state atomically with outbox. Successful-test recovery clears only applicable state, preserves disabled accounts and performs no write for a no-op. Actual account/outbox faults and caller rollback forbid cache/runtime/token publication. Status reads use the current saved deadline and reason, including after recovery, deletion or cache failure |
| Recovery commit and delayed local cleanup | `TestGrokAdminRecoveryKeepsNewFailureCommittedBeforeLocalCleanup`, `TestGrokOutcomeCommittedAfterRecoveryKeepsItsNewGenerationDespiteEarlierEpoch` | A newly committed failure survives the earlier reset's delayed local cleanup. Recovery captures generations before SQL; cleanup conditionally retires that evidence. A request already in flight can still publish valid new evidence committed after reset |
| Account recovery and credential-family management | `repository/account_repo_admin_recovery_integration_test.go`, `account_repo_shadow_lifecycle_race_integration_test.go`, `account_repo_shadow_parent_type_integration_test.go`, `account_repo_oauth_shadow_race_integration_test.go` | All seven providers recover atomically while preserving manual pause/expiration settings. Whole-family deletion/restoration rolls back on failure; creation cannot reference a deleted parent. Parent type conversion and three shadow creation entrypoints serialize in both directions, including commits while waiting and rollback controls; events and scheduler cache publish only committed state |
| Cross-instance proxy management | `integration/seven_provider_management*_integration_test.go` | Seven platforms use actual PostgreSQL/Redis and authenticated proxy sends on a second scheduler instance after bind, edit, unbind and repair. Delayed success/failure and quality probes cannot certify the repaired route; background creation probes become visible on the other instance; route edits and scheduler outbox commit atomically |
| Proxy expiry and restoration | `integration/seven_provider_expiry_management_integration_test.go` | Seven platforms × backup/direct/keep-binding modes: actual post-expiry send, cached status, old-owner observation rejection and repair. Concurrent renewal is rechecked after the row lock; expiry/restore outbox failures roll back saved bindings |
| Deletion, binding races and account recreation | `integration/seven_provider_delete_management_integration_test.go`, `seven_provider_account_recreation_integration_test.go` | Deletion rechecks binding under the proxy lock; create/update/bulk/restore reject a concurrently deleted target. Account deletion and outbox are atomic, caller rollback publishes no cache deletion; seven-platform deletion/recreation reaches another instance and uses the replacement ID/credential/proxy on real sends |

### Observations, persistence and recovery

| Business scenario | Test entrypoints | Required/forbidden effects |
| --- | --- | --- |
| Kimi object reasoning and complete capability siblings | `service/kimi_official_schema_test.go` | Official effort/default/modalities/context/tools preserved; unknown reasoning does not discard recognized siblings |
| DeepSeek listing requirements | `service/deepseek_model_listing_requirements_test.go` | Admin IDs, saved metadata and public cached catalogs apply the official Messages/capacity/modality checks and minimum client version using the request's selected account/global/environment/compiled identity; lower-priority sources cannot upgrade that selection |
| Native usage and wallet observations | `service/domestic_control_owner_test.go`, `domestic_control_schema_test.go` | Unknown/malformed/non-finite observations do not manufacture zero usage or availability; DeepSeek recharge/bonus combine only within the same currency; unlimited Step plan clears obsolete limits |
| API Key observation owner, schema and freshness | `service/cn_control_apikey_observation_test.go`, `repository/account_repo_cn_apikey_observation_integration_test.go` | Late key/nested-credential/proxy changes and storage failures cannot persist an old snapshot; real PostgreSQL public-service tests check five balance/quota modes. Kimi wallet requires a successful finite balance envelope; coding quotas require recognized finite windows, preserve zero/overuse and clear unobserved windows/resets before advancing freshness |
| API Key wallet scheduling | `TestCNAPIKeyWalletSchedulingRejectsChangesAfterObservation`, `TestCNAPIKeyBalanceScheduleCASRetainsOwnerAndScheduler` | DeepSeek/Kimi low/healthy × unchanged key, replaced key, other cooldown and manual pause. The public scheduler and PostgreSQL writer reject stale-owner or stale-scheduler decisions without legacy unconditional writes, outbox or cache publication |
| API Key control response boundaries | `service/cn_apikey_control_response_boundary_test.go`, `repository/cn_apikey_control_redirect_test.go` | Five balance/quota modes use production HTTPUpstream against same/cross-origin 301/302/307/308: zero redirected requests and no persistence, with 200 success controls. Complete/exact-cap reads succeed; read errors and cap+1 fail before CAS. DeepSeek malformed JSON/availability/currency or a damaged wallet sibling cannot publish zero, stop a healthy account or clear recovery state; real zero/unavailability and dual-currency healthy controls remain valid |
| OpenAI WHAM and Grok quota/billing strict routes | `service/quota_proxy_owner_contract_test.go` | Public usage, credit reset/detail, targeted reset, active probe and billing paths; OpenAI OAuth/Agent Identity/shadow usage retain their applicable rules; 48 broken bindings reject before token/refresh/client/send or observation writes; 16 valid-bound/unbound controls; near-expiry refresh suppression and token-lookup route changes. Request factory/transport recorders establish entrypoint behavior, not real TCP proxy failure coverage |
| WHAM operation retains its credential owner | `TestOpenAIQuotaCredentialOwnerSnapshot*`, `TestOpenAIQuotaCapturesRouteBeforeTokenLookup` | Token lookup or client creation cannot switch the captured bearer/AgentAssertion, account ID, selected identity or route to a replacement login. Agent task recovery after relink cannot register a replacement task or replay the original operation |
| WHAM cache ownership and token rotation | `TestOpenAIQuotaTokenOwnerRejectsRelinkBeforeAdoptionOrRefresh` | Usage/reset reject an unowned token at the initial cache hit and both cache-wait returns, as well as replacement credentials at token-version adoption, refresh reread and refresh completion; rejected rereads cannot call the issuer; the captured owner's cached token and a rotation authorized by that owner remain usable |
| WHAM quota refresh/task final persistence | `repository/account_repo_openai_quota_owner_integration_test.go` | Actual quota service and PostgreSQL: refresh/task writes compare full credentials, platform/type and captured proxy atomically with scheduler outbox. External relink at the final write, account/proxy lock waits, outbox/storage failure and caller rollback cannot publish credentials, cache or a quota request. A same-owner task registered by another instance may be reused after CAS miss. This port is scoped to quota operations, not every unrelated OpenAI refresh caller |
| Grok detached model discovery | `service/grok_observed_models_strict_proxy_test.go` | Eight strict route, ten late-owner and five scheduling snapshot scenarios; captured nested credentials and proxy cannot be replaced; no mutable route reads or unconditional schema writes; model observations use the shared SQL owner-CAS port. These are service recorder/CAS-port tests, separate from the PostgreSQL port's integration regressions |
| Every provider's administrative model discovery and persistence | `service/openai_auxiliary_strict_proxy_test.go`, `seven_provider_model_observation_owner_test.go` | All seven manual-key paths validate strict routing before request construction; model metadata writes compare the credential/proxy owner, rejecting key replacement, same-row route edit, unbinding and storage failure. Shadow discovery uses its credential parent and never publishes that parent's observation on the shadow row |
| Public model discovery | `service/domestic_public_models_test.go`, `zhipu_project_models_test.go` | Native/PAT auth used at actual request boundary; relogin cannot reuse an old owner's cache or persist its late metadata |
| ZCode short project tokens | `service/zhipu_project_token*_test.go`, `pkg/zcode/project_token_contract_test.go` | Personal/team scope, generation/cache separation, expiry/reissue/backoff and hard-error invalidation; old ticket reissue keeps original identity and cannot poison a new login |
| MiniMax formal Flash catalog ID | `TestMiniMaxOfficialFlashReleaseCandidate` | Formal `MiniMax-M3.1-Flash` appears in the shared catalog; pricing is not inferred from a Preview alias |
| Native observation/refresh/wallet CAS | `repository/account_repo_cn_*_integration_test.go` | Real PostgreSQL: compare credential owner and binding in the write; merge current Extra; atomic scheduler outbox; transaction rollback/outbox failure cannot publish partial state |
| Control CAS commit versus a superseding login at the shared-cache boundary | `repository/TestControlCASCacheBoundaryCannotRepublishSupersededLogin` | Two required children, `managed=false` (direct CAS) and `managed=true` (managed transaction), commit a replacement login through PostgreSQL at the actual cache boundary. Neither may republish the prior snapshot; exact cache eviction, new-owner reload, retained observation and one durable outbox event are asserted. |
| Same proxy row changes while a request is in flight | `TestCNProxyOwnerCASRejectsSameRowRouteEditsWithoutSideEffects`, `TestCNProxyOwnerCASWaitsForConcurrentProxyEditAndRechecks` | Real PostgreSQL: endpoint/auth/protocol/status/expiry/deletion changes rejected; wait for concurrent proxy writer and recheck; no old credentials/usage/cooldown, outbox or cache publication |
| Existing-account login completes atomically | `TestCNOAuthLoginCASRecoveryCommitsWithCredentialsAndOutbox`, `TestCNOAuthLoginCASStorageFaultLeavesOriginalLoginAndRecoveryState`, `TestCNOAuthLoginCASPreservesNewerRuntimeFailures` | Credentials and old-error recovery commit together; storage failure keeps original state; a newer cooldown/manual pause cannot be erased by a delayed login; unrelated Extra survives |
| Automatic Codex quota reset is already covered | `TestQuotaFollowReset_ConcurrentObservationBillingAndWorkers` and the other `TestQuotaFollowReset_*` integration cases | Concurrent observations create one persisted event; concurrent charges survive competing reset workers; source, activation, membership, transaction and cache ordering checked. The earlier blanket “persisted concurrency/idempotency missing” claim does not apply to this tree |
| Native media and tool audit ordering | `handler/domestic_provider_audit_*_test.go`, `service/domestic_provider_native_payload_test.go` | Canonical content extraction and accepted unknown structures; audit occurs before account/billing/slot/upstream effects; supported native media/tool fields retain their semantics |
| StepFun catalog and actual model admission | `service/stepfun_model_restrictions_test.go` | CN/global catalog contract, invalid sessions, cancelled responses and failed discovery preserve the grant appropriately; OAuth/API Key account selection enforces the requested model restriction |
| Independent token-count entrypoints | `service/openai_gateway_count_tokens_test.go`, `openai_responses_input_tokens_test.go` | The OpenAI API Key Messages count bridge sends Responses input_tokens, including for custom OpenAI base URLs; Codex profile admission precedes credential lookup. Native Responses counting estimates Grok/custom-relay input locally without sending. Applicable unsupported OAuth endpoints and native upstream 404 use the documented estimate. Fixed request samples pin independent numeric expectations |
| Token-count owner and production proxy transport | `service/openai_count_tokens_owner_contract_test.go`, `repository/openai_count_tokens_proxy_wire_test.go` | Native Responses input_tokens and Messages count bridge independently reject broken bindings before token lookup and retain captured credentials/identity/route during edits. Real authenticated CONNECT/TLS sends to the official HTTPS hostname cover bound/direct success, refusal, timeout, 407 and missing binding; failures produce zero direct/target connections and release transport slots. Applicable local estimates perform no credential or network work |
| Live finalization and zero-usage billing | `service/openai_live_lifecycle_test.go` | Expiry, lease loss and temporary store outage terminate through the existing lifecycle; repeated finalization writes one zero-usage record. Usage persistence falls back to synchronous creation when its async writer is unavailable |
| Grok search, audio and video billing | `service/openai_gateway_grok_search_billing_test.go`, `grok_media_video_billing_test.go`, `handler/grok_audio_billing_test.go` | JSON/SSE search calls are counted and deduplicated. Video creation/pending/failed status does not bill; official done status uses observed parameters. Realtime billing requires observed audio and a stable unique identifier, with pending-turn deduplication. These focused billing assertions supplement the persisted inference debit flows |


## Evidence boundaries and extension rules

The following combinations need explicit review whenever their owning code
changes. The rows above and below apply only to their stated dimensions and assertions:

| Combination | Existing evidence | Required extension before claiming exhaustive coverage |
| --- | --- | --- |
| Proxy create/bind/idle edit/in-flight edit/unbind/delete/repair × every provider/auth × every operation | PostgreSQL seven-provider CRUD/outbox/CAS and 36-operation barrier-controlled auxiliary wire lifecycle tests; seven-provider gateway failure and captured-owner tests | Newly introduced sender boundaries need their own committed-route/old-operation wire cases; the 36-operation table does not certify independent OAuth login/exchange or media/download implementations. |
| Auxiliary timeout/refusal across all providers | Shared transport, seven-provider gateway timeout/refusal, 36-operation auxiliary timeout/refusal/407, OpenAI account refresh/revocation, AgentIdentity task bootstrap, Grok OAuth account refresh/observed-models/compression-settings wire matrices, and missing-proxy preparation tests | The independent code/device, OIDC/SSO, native login, Live and Grok media/realtime rows below add their own transport evidence. Any additional sender still needs its actual preparation and physical send mapped; provider names do not confer coverage. |
| Legitimate refresh × provider-specific auxiliary call × concurrent proxy/credential edit | Kimi models/quota and MiniMax quota proactive/401 refresh × grant/proxy endpoint/auth/unbind/disable/persistence/cancellation; ZCode project-token/lease and Grok token tests | Additional separately implemented refresh senders require the same handoff and side-effect oracles. DeepSeek has no published refresh. StepFun model keys remain static; its optional Passport account-session refresh is separate from model-key rotation. |
| Seven-provider routing/account failover × proxy configuration update | Codex canonical/shared routing; six-provider OAuth/API-key failed-owner replacement/conversation/backup tests; StepFun model-edit routing; Grok handler failover; MiniMax exhaustion and native session isolation | A new routing policy must prove committed replacement and stale-attempt fencing. Scheduler selection tests do not establish exactly-once upstream write/billing; use the protocol handler/turn tests for those effects. |
| Optional media/Live capabilities versus unsupported accounts | Codex image/Live and Grok media/realtime tests, StepFun discovered capability restrictions | For every newly exposed capability, add positive advertised case plus unsupported rejection before selection/billing/upstream write; do not treat an unknown upstream response as support. |
| Async image task URL offload | Final generation owner/identity/proxy survives caller edits and explicit unbinding; CDN redirect retains route; missing owner/bound proxy, timeout/refusal/407 and private redirects cannot use uploader default client or create a direct connection | `service/TestAsyncURLOnlyImageOffloadRetainsActualAccountProxy` covers actual OpenAI API-key, Grok API-key and Grok OAuth generation/offload with success/redirect/timeout/refusal/407/private-redirect. `TestAsyncImageOffloadMissingOwnerOrBoundProxyCannotUseUploaderDefault` and `handler/TestAsyncImageHandlerURLOnlyCompletionRequiresCapturedOwner` reject lost evidence at service and completion-handler boundaries. Object-storage upload remains a separate configured administrative route; these cases do not imply other providers expose image generation. |
| Adaptive model-protocol catalog | `model_protocol_catalog.go` runs only when `providerProfile.ModelCatalog` is enabled; the current profile table enables it for CommandCode alone | `TestAccountControlSendersRequireStrictProxy/protocol_catalog` covers this independent sender's missing/mismatched/invalid/timeout/refusal/407/valid/unbound matrix, and `TestModelProtocolCatalogWarmCacheCannotBypassInvalidBinding` checks warm-cache fencing. This sender is outside the seven official platform profiles and adds no seven-provider coverage. |

Closing a combination requires a concrete independent expected result, applicable
required and forbidden side effects, and a passing final-tree container gate.
New defects found by these cases must change production behavior; weakening the
assertion, replacing it with implementation-generated expectations, or increasing
an arbitrary timeout is not completion.

## Capability-completion implementation inventory

The capability batch adds the following requirement-derived cases to the existing
seven-provider regressions. These are implemented test contracts; their presence
does not establish a passing final-tree run. The current execution status is
recorded under [capability-completion verification](#capability-completion-verification).

| Business requirement | Independent oracle and cases | Test entrypoints |
| --- | --- | --- |
| Step official Chat execution and both HTTP bridges | OAuth Plan/API Key PayG/API Key Plan × CN/global × Chat/Responses/Messages × stream/nonstream: 36 ordinary responses and 36 parallel-tool/second-turn flows. Literal endpoints, Bearer, trusted Step identity, reasoning/text, independent tool IDs/arguments, protocol termination and client-facing usage; independently priced cache/output usage | `service/TestStepFunCapabilityBasicMatrix`, `TestStepFunCapabilityParallelToolsAndResults`; `pkg/apicompat/TestStepFunParallelChatToolsKeepIndependentAnthropicBlockLifecycles` |
| Step provider errors, truncation and completion | Each of six profiles and three ingress protocols: 401/403/429/503/HTTP-200 error envelopes; empty/truncated/read-failed/error/cancelled/deadline/missing-usage/missing-DONE streams. Delivered semantic output forbids replay. Raw Chat retains its documented completion indicators; each bridge requires its upstream DONE before synthesizing a success terminal. A completed response without provider metering retains zero observed tokens/cost with unknown usage source; it does not claim exact zero metering | `service/TestStepFunCapabilityFailuresNeverBecomeSuccess`, `TestStepFunCapabilityStreamFailureAndTerminationContract`, `TestStepFunCapabilityCompletedWithoutUsageKeepsUnknownBillingSource` |
| Step old-owner error isolation | Six profiles × three protocols × unchanged/replaced credentials/rebound proxy/same-ID endpoint edit/region edit: actual transmitted credential and route retained; a late 401 quarantines only the unchanged owner | `service/TestStepFunCapabilityLate401CannotQuarantineReplacementOwner` |
| Step prices, cached input and unsupported units | Independent USD cards for five models, explicit cache creation/read/output prices, catalog/channel overrides and manual pricing for unknown/router IDs. Twelve existing cached-usage protocol/type/stream cases supplement the 36-cell independent account/profile billing matrix. Token-plan credits, nonempty credit buckets and CNY/USD wallets cannot become percentage-window observations or refresh their persisted freshness; this is explicit unsupported-observation evidence, not new currency support | `service/TestStepFunPricingOfficialRatesAndReferenceCoverage`, `TestStepFunPricingCatalogAndChannelOverrides`, `TestStepFunOfficialCachedUsageAcrossProtocols`, `TestStepFunCreditsAndWalletsNeverBecomePercentageQuota` |
| Step production proxy failures | Six profiles × three protocols × timeout/refused/407: production HTTPUpstream and OAuth preparer; zero direct destination connection attempts, no credential leakage or model Authorization in CONNECT | `repository/TestStepFunProductionProxyFailureAcrossProfilesAndBridges`: 54 required children. Positive authenticated TLS/proxy flows are exercised by the separate business matrix |
| Step persistent business flow | Six profiles × three protocols × stream/nonstream × A=503/429: 72 cases. Production auth/handler/scheduler, verified TLS, saved authenticated CONNECT proxies, real PostgreSQL/Redis; A→B, recovered A cannot steal B, new gateway retains B, actual credentials/identity/proxy, exact SQL debits/usage/dedup and released slots | `integration/TestStepFunBusinessMatrix`. Outer JSON children verify every exact subprocess PASS, so removal/skip of a CA subprocess row fails the gate |
| Files wire, ACL and lifecycle | DeepSeek Anthropic image upload for API Key/OAuth and MiniMax global API Key Chat video upload; actual inference ID rewrite and owner restriction, isolated idempotency, unknown local ID never becomes native, expiry/revocation, upload uncertainty, known-ID compensation, credential replacement and safe delete retry | `service/TestProviderFilesUploadWireAndIdempotentReuse`, `TestProviderFilesUncertainAndCompensation`, `TestProviderFilesDeepSeekUploadInferenceReuse`, `TestProviderFilesMiniMaxUploadActualChatInference`, `TestProviderFilesDeleteFailureRecoveryDoesNotRepeat`, `TestProviderFilesRejectCredentialReplacementAtActualSend`, `TestProviderFilesRewriteAndACL`, `TestProviderFilesUnknownLocalIDNeverBecomesNative` |
| Files ingress audit and upload cleanup | Basic multipart validation precedes account effects; audit precedes upload selection and list/metadata/delete ACL reads. Cancellation removes temporary bytes; unsupported bridges reject local references; both engines share the canonical file-reference extraction contract and allow binary data while emitting safe structured diagnostics | `handler/TestProviderFilesMultipartCleanupAndValidation`, `TestProviderFilesAuditPrecedesACLRead`, `TestProviderFilesUploadAuditsBeforeSelection`, `TestProviderFilesCancelledUploadDoesNotRetainBytes`, `TestProviderFilesInferenceUnknownLocalAndUnsupportedBridges`; `service/TestProviderFilesCanonicalAuditContract`, `TestProviderFilesModerationAllowsBinaryAndLogsSafeDiagnostics`; `securityaudit/TestProviderFilesPromptAuditAllowsBinaryAndLogsSafeDiagnostics` |
| Files strict transport and persistence | Three supported profiles and each applicable upload/delete operation × success/direct/refused/timeout/407/cancelled; production transport with zero direct fallback. PostgreSQL checks mapping persistence, reservation uniqueness across instances, quota serialization and proxy/credential CAS | `repository/TestProviderFilesProductionProxyContract`, `TestProviderFilesRepositoryPersistenceIdempotencyAndFencing`, `TestProviderFilesRepositoryCrossInstanceUploadReservation`, `TestProviderFilesRepositoryQuotaAndProxyCAS`; migration `297_provider_files.sql` |
| Start entitlement is separate from coding/off-peak | Exact balance URL/auth/identity; available/unavailable/pending/unknown, models unknown versus explicit empty, server clock/expiry, malformed fields, old owner/proxy/identity/freshness and failed persistence. Actual mapped-model selection and public catalogs never invent permission; account metadata projection retains the grant digest without JWT | `service/TestZhipuStartPlanOfficialBalanceWireAndIsolation`, `TestZhipuStartPlanAvailabilityBoundaries`, `TestZhipuStartPlanFailureAndOwnerFence`, `TestZhipuStartPlanSelectionAndPublicCatalog`, `TestZhipuStartPlanObservationCannotCrossOwnerOrReactivateOnTime`, `TestZhipuStartPlanPublicCatalogRechecksCachedOwnerAndIdentity`; `handler/TestZhipuStartPlanUnknownPublicCatalogNeverInventsDefaultPermission`; `repository/TestZhipuStartPlanSchedulerProjectionKeepsPermissionWithoutJWT` |
| Start production balance, atomic publication and management UI | Real strict-proxy balance client; PostgreSQL owner/old-observation/outbox checks and cross-instance selection. Eleven Vue cases independently pin four states, model detail, late owner results, no time-only promotion, reauthorization and plan isolation | `repository/TestZhipuStartPlanProductionBalanceStrictProxy`, `TestZhipuStartPlanPostgresCommitOutboxAndCrossInstanceSelection`, `TestZhipuStartPlanPostgresLateObservationCannotReplaceNewObservation`, `TestZhipuStartPlanPostgresOwnerAndOutboxFailuresPreserveObservation`; `frontend/src/components/account/__tests__/ZhipuStartPlanCell.spec.ts` |
| Optional durable off-peak admission and settlement | Explicit opt-in and dedicated keyring; authenticated encrypted snapshot, owner generation/fencing, uncertain Take never replayed, queue reuse, admission only after ticket commit, individual request leases, lost heartbeat safety window, original-owner retirement, same-ticket settlement retry and durable ACK before clear | `config/TestZhipuOffPeakPersistenceConfiguration`; `service/TestZhipuOffPeakSnapshotEncryptionAndRotation`, `TestZhipuOffPeakGenerationTracksOwnerNotObservations`, `TestZhipuOffPeakDurableUncertainTakeNeverReplaysAfterRestart`, `TestZhipuOffPeakDurableQueueTimeoutRetainsTicketForNextRequest`, `TestZhipuOffPeakDurableTakeCommitFailureCannotAdmit`, `TestZhipuOffPeakDurableSettlementACKMustPersistBeforeClear`, `TestZhipuOffPeakDurableLostHeartbeatPreservesRequestSafetyWindow` |
| Durable off-peak PostgreSQL concurrency | One current generation across instances; expired operation lease fences late writes; crash keeps uncertain intent; all physical request leases protect settlement; ACK write failure retains intent; retired generation retains original in-flight protection; lock-wait recheck prevents renewal/settlement overlap | `repository/TestZhipuOffPeakStoreCrossInstanceOpenHasOneCurrentGeneration`, `TestZhipuOffPeakStoreExpiredOperationLeaseFencesLateSaveAndAdmission`, `TestZhipuOffPeakStoreBeginTakeCrashRemainsUncertainAfterRestart`, `TestZhipuOffPeakStoreSharedRequestLeasesBlockSettlementUntilAllFinish`, `TestZhipuOffPeakStoreSettlementRetryKeepsIntentUntilAcknowledged`, `TestZhipuOffPeakStoreRetiresOldOwnerAndRegistersLateTicketForOriginalGeneration`, `TestZhipuOffPeakStoreFailedAckWriteCannotErasePendingIntent`, `TestZhipuOffPeakStoreRetiredGenerationWaitsForOriginalInflightRequest`, `TestZhipuOffPeakStoreSettlementRechecksRequestLeaseAfterParentLockWait`; migration `298_zhipu_offpeak_tickets.sql` |
| Durable off-peak manager restart and quota behavior | Actual manager plus PostgreSQL store preserves the same queue ticket across restart, does not repeat a Take whose accepted response was lost, creates one new task only after confirmed expiry, and never restores rolling-window allowance merely because a prior ticket settled | `repository/TestZhipuOffPeakManagerPostgresQueueTimeoutRestartsWithSameTicketStatus`, `TestZhipuOffPeakManagerPostgresAcceptedTakeResponseLossDoesNotRetakeAfterRestart`, `TestZhipuOffPeakManagerPostgresConfirmedExpiryCreatesOneNewTaskAndTicket`, `TestZhipuOffPeakManagerPostgresSettledTicketDoesNotRestoreTakeQuota`; memory-mode `service/TestZhipuOffPeakSettlementDoesNotRestoreTakeEligibility` |
| Durable off-peak known-ticket Status and admission recovery | A protocol-port TCP read failure preserves the known ticket/task/ciphertext; a restarted production manager first revalidates the same ticket with Status. After a successful Status, a real PostgreSQL request-lease INSERT failure or expired operation lease cannot admit or discard the ticket. Following crash/lease expiry, restart admits the same ticket without a new Take and retains the captured owner/identity/proxy | `repository/TestZhipuOffPeakManagerPostgresKnownStatusFailureRecoversSameTicketAfterRestart`; `TestZhipuOffPeakManagerPostgresAdmissionFailureRecoversSameTicketAfterRestart`: request-lease-insert-failure / operation-lease-expired |
| Files reuse, cleanup and pagination isolation | The 64 MiB boundary uses actual bounded content; conflicting file owners and retry selection cannot fail over to a different owner. Restart cleanup only uses a known remote ID and the verified original snapshot. PostgreSQL pagination treats inaccessible user/key/group/platform cursors like unknown IDs in both directions; owner CAS takes the proxy lock before the account lock | `service/TestProviderFiles64MiBContentBoundary`, `TestProviderFilesMultipleOwnerReferencesFailBeforeSend`, `TestProviderFilesSchedulerCannotFailoverToDifferentFileOwner`, `TestProviderFilesExplicitRetryPinsOriginalOwner`, `TestProviderFilesRestartCleanupUsesKnownOriginalSnapshot`, `TestProviderFilesMiniMaxLocalRevokeIsExplicitlyUnsupportedRemoteDelete`; `repository/TestProviderFilesPersistentUploadReferenceAndRestartCleanup`, `TestProviderFilesRepositoryPaginationCursorCannotReadAnotherScope`, `TestProviderFilesRepositoryOwnerCASLocksProxyBeforeAccount` |
| Files every ingress side-effect boundary | The ordering regression requires upload retry lookup, scheduler selection, both concurrency slots, upload, metadata Get, list and delete to exist after the audit point; runtime blocking tests independently prove no forbidden call | `handler/TestProviderFilesAuditGatePrecedesEveryACLAndWriteBoundary`: eight required children; complements `TestProviderFilesAuditPrecedesACLRead` and `TestProviderFilesUploadAuditsBeforeSelection` |
| Start composite public aliases | Eight entitlement situations × Models/Codex × allowlist on/off: an exact route alias is advertised only for a fresh proven target model. Route configuration and group allowlists do not create upstream permission; unrelated non-Start routes and models remain visible | `handler/TestZhipuStartPlanCompositeRouteAliasRequiresProvenTargetEntitlement`: 32 children; `TestZhipuStartPlanCompositeRouteFilteringKeepsOtherPlatforms`: two children |
| Off-peak malformed Status and lease-release ordering | Ready/queued tickets retain the same ID/task and last valid state after empty, unmatched, unknown or duplicate Status; no admission, settlement or additional Take. Durable missing/ambiguous Status also retains its encrypted intent. PostgreSQL release uses the same parent-before-request-lease lock order as renewal and settlement | `service/TestZhipuOffPeakMissingStatusPreservesExistingTicket`: ten children; `TestZhipuOffPeakDurableMissingStatusNeverClearsOrRetakes`: four children; `repository/TestZhipuOffPeakStoreReleaseLocksParentBeforeRequestLease` |
| Off-peak administrator termination | Administrator GET exposes only opaque segment handles; exact-pair POST acknowledges durable retirement, not completed remote settlement. Disabled mode/profile/invalid handle/storage failure are explicit. PostgreSQL fences old control results, preserves shared in-flight leases, rolls back failed retirement, retries original-owner cleanup, and isolates delayed/duplicate termination from a replacement segment. Both waiting and already-claimed old acquisitions cannot retake or join a new segment | `service/TestZhipuOffPeakAdminSegmentRejectsUnsupportedModeAndProfile`, `TestZhipuOffPeakAdminSegmentMapsErrorsWithoutSecretDisclosure`, `TestZhipuOffPeakAdminTerminateDrainsAndRetriesOriginalSnapshot`; `repository/TestZhipuOffPeakStoreTerminate*`, `TestZhipuOffPeakManagerPostgresTerminateCannotRetakeFromOldAcquire`; four `admin/TestZhipuOffPeakSegmentHandler*`; `routes/TestZhipuOffPeakSegmentRoutesRequireRealAdmin` |
| Start composite catalog actual target and owner representation | Only the winning priority/endpoint-specific route and account-mapped target can use a fresh proven Start entitlement. Default catalog fallback cannot resurrect a filtered target. Equivalent typed/persisted identity representations preserve the same owner, while JWT replacement invalidates it | `handler/TestZhipuStartPlanCompositeCatalogUsesWinningRouteAndActualTarget`: sixteen children; `TestZhipuStartPlanCompositeCatalogPreservesTypedAndPersistedIdentityOwner`: two children |

The owning capability contracts are [StepFun](providers/STEPFUN.md),
[Files](providers/FILES.md), and [Zhipu](providers/ZHIPU.md). The critical manifest
pins each listed top-level case and the independently specified table children.
The parser regressions remove required execution events and reject missing,
skipped, failed and unfinished cases. Full suite and targeted race execution are
separate evidence, as are real-account authorization and entitlements.

### Post-commit branch review

The review starts from `488ca736c`, which committed the capability batch above.
It adds the following independent regressions. Execution results for this
review must be recorded separately from the earlier capability run.

| Business requirement | Independent oracle and cases | Test entrypoints |
| --- | --- | --- |
| Messages tool blocks remain immutable once stopped | Text before tools and ordinary parallel tools remain valid; text, refusal and reasoning interleaved with argument fragments cannot close an incomplete tool block or write to a stopped index | `pkg/apicompat/TestStepFunReviewToolStreamKeepsClosedBlocksImmutable` |
| Completed tool input must be an object | Buffered/streamed object, empty and whitespace controls succeed; truncated/trailing JSON, array, scalar and null cannot become a successful completed tool call. Preserve measured usage and forbid account replay | `service/TestStepFunReviewMessagesRejectsInvalidCompletedToolInput` |
| Token-limit truncation retains its successful stop reason | Streaming/buffered `length` retains `max_tokens`, exact observed usage and one terminal sequence even with incomplete tool arguments; buffered incomplete input has a safe empty-object representation and streaming keeps its original fragments. `tool_calls`/`stop` controls still reject damaged completed input | `service/TestStepFunReviewMessagesPreservesMaxTokenToolTruncation` |
| Responses incomplete output is distinct from completed output | Streaming/buffered `length` retains original argument strings and usage, marks truncated tools incomplete, and declares `status=incomplete` with `max_output_tokens`; streaming uses `response.incomplete`. Invalid completed arguments cannot be silently omitted or retain a complete-usage classification | `service/TestStepFunReviewResponsesPreservesMaxTokenToolTruncation` |
| A physical partial failure has one scheduling outcome and one bill | Real Chat/Responses/Messages handlers retain partial usage, release slots and report the one physical attempt once; the next same-session request is not prematurely moved to another account | `handler/TestPartialFailureReviewKeepsSingleSchedulingOutcomeAndBill` |
| Decoded local file references retain ACL and owner semantics | Literal/escaped local IDs share the protocol matrix, lookup, rewrite and hard owner constraint; another key or unknown local ID cannot fall through to native references | `service/TestProviderFilesReviewEscapedIDsPreserveACLRewriteAndOwner`; `handler/TestProviderFilesReviewHandlerLocalIDsRespectProtocolMatrix` |
| File mentions are distinct from file references | MiniMax bridge text, tool input, unknown siblings and native references retain existing handling; actual local references are explicitly unsupported. DeepSeek local files are native Messages-only and cannot silently disappear in Chat/Responses conversion | `handler/TestProviderFilesReviewMiniMaxBridgesIgnoreNonReferenceMentions`, `TestProviderFilesReviewMiniMaxBridgesRejectActualLocalReferences`, `TestProviderFilesReviewDeepSeekBridgesNeverDiscardLocalImages`, `TestProviderFilesReviewDeepSeekBridgesKeepNativeAndInlineCompatibility` |
| Both audit engines keep opaque references separate from sibling text | Chat/Responses/Messages literal and escaped local references plus unknown siblings preserve independently auditable user text; removing that text leaves no invented prompt or policy violation | `service/TestProviderFilesReviewInferenceAuditKeepsTextAndOpaqueReferencesSeparate` |
| Files uploads remain bounded through the full middleware chain | Only registered POST upload routes skip model extraction and leave the body unread for private spooling; ordinary Chat/Live requests still enforce their allowlist | `server/middleware/TestProviderFilesReviewAllowlistLeavesUploadBodyUnread`, `TestProviderFilesReviewUploadExemptionPreservesModelAllowlist` |
| MiniMax image containers cannot masquerade as video | Four explicit MP4/QuickTime signature controls succeed; AVIF/HEIF major brands and image-compatible generic ISO brands fail. This is bounded signature validation, not video-track or codec verification | `service/TestProviderFilesReviewMiniMaxRejectsNonVideoISOBrands` |
| Concurrent quota rejection preserves the actual reset | Two managers on real PostgreSQL observe one Take and the same quota/reset result; no waiter acquires a model lease or creates a replacement segment | `repository/TestZhipuOffPeakManagerPostgresConcurrentQuotaRejectionPreservesResetTime` |
| A partial Take preserves known ticket evidence without admitting | Unknown/missing/empty state survives restart as uncertain; the next manager uses Status on the same ticket. Durable ready/queued/unknown states accompanied by an error cannot admit until the restarted manager obtains a valid original-ticket Status. Memory mode also retains known IDs from partial errors, rejects nil/missing-ID results and bounds uncertain Status polling | `repository/TestZhipuOffPeakManagerPostgresUnknownTakeStateRetainsKnownTicketForRestart`; `service/TestZhipuOffPeakDurablePartialTakeRetainsEvidenceBeforeRestartAdmission`, `TestZhipuOffPeakMemoryPartialTakeRetainsKnownIDWithoutAdmissionOrRetake`, `TestZhipuOffPeakMemoryTakeWithoutTicketEvidenceCannotAdmit`, `TestZhipuOffPeakMemoryUncertainStatusFailureKeepsBoundedPollWait` |

The four durable off-peak environment settings are forwarded by every maintained
Compose variant, including the development build, with the documented opt-in
default and empty keyring. `deploy/tests/docker-compose-gateway-env-test.sh`
checks their exact defaults and unique bindings alongside the gateway settings;
`config/TestZhipuOffPeakPersistenceConfiguration` checks loading and validation.
Neither configuration test establishes that an operator supplied a valid shared
keyring to a real deployment.

## Shared image storage and plugin host

| Business scenario | Test entrypoints | Observable boundary |
| --- | --- | --- |
| Asynchronous image storage | `handler/TestAsyncImageContainerWorkflowWithMinIO`, `repository/TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate` | A separate disposable MinIO/PostgreSQL run applies the complete migrations and executes both tests without skipping. Production S3 permissions probes clean up their objects; image submission, upload, signed download and persisted history retain their asserted state. Image generation is an isolated responder, so this does not prove live provider image access. |
| Signed plugin installation and real process transport | `service/TestLocalSignedPluginRuntimeIntegration` | The production installer rejects an untrusted ephemeral publisher, then verifies its real Ed25519 signature and executable hash after explicit trust configuration. The installed test executable starts as a separate OS process through production go-plugin handshake and gRPC. Five mandatory scenarios assert config/HTTP round trips and passive health, pre-send refusal with no upstream request, delivered failure without account replay, upload-read failure/body cleanup, and cancellation reaching child HTTP with lease release. Drain stops the child and refuses new requests. |

The maintained storage gate is
`python3 tools/check_seven_provider_coverage.py --profile storage`. It pins two
top-level tests in order: migration/idempotency smoke, then the existing MinIO
workflow, both with `-tags=integration -count=1`. Missing configuration or
missing/skipped/failed tests fail the gate; a failed migration step stops it.
Supply explicit disposable PostgreSQL/Redis/MinIO services inside the validation
container as documented in [CONTRIBUTING.md](../CONTRIBUTING.md#development-checks).
`SUB2API_ASYNC_TEST_DATABASE_URL` must equal `SUB2API_TEST_POSTGRES_DSN`; the
MinIO endpoint is `SUB2API_ASYNC_TEST_S3_ENDPOINT`. Ordinary `unit`/`integration`
coverage profiles and the current shared full/CI profile do not provision MinIO
or execute this separate gate. A full integration run without those storage
settings skips the workflow; that skip is not passing storage evidence. Run the
storage gate separately when accepting the configured S3 result-storage path,
then remove its disposable service containers and writable state.

The local signed-process fixture exercises the host transport and lifecycle;
it does not verify an official external plugin release. The separate
`service/TestPluginRuntimeIntegration` requires a signed package supplied by
`SUB2API_TEST_PLUGIN_PACKAGE`. Its absence still skips that external-artifact
check and cannot count as passing official-package compatibility evidence.

## Verification and remaining boundaries

Use the maintained commands in [CONTRIBUTING.md](../CONTRIBUTING.md#development-checks)
inside the pinned platform validation container. Full backend unit tests include
the network fixtures; PostgreSQL/Redis cases require the integration environment.
Frontend changes require lint, typecheck, relevant Vitest and locale checks.
Identity changes also retain the complete existing Codex identity regressions.

### Post-commit branch verification

The reviewed capability checkpoint is `488ca736c`; production fixes and their
independent regressions are committed as `50dbaa500`. The final code candidate
is `804e31285`, whose only additional change uses a tagged switch in a test
fixture to satisfy staticcheck. On 2026-10-26, the maintained
`python3 skills/push-cli/scripts/push_cli.py check` full profile passed in
Apple Containers in 577.2 seconds. The image was
`sub2api-validation:8449ffd7679a1e27`, with dependency-cache generation
`7b5b2a85be071953`; PostgreSQL, Redis and MinIO services were disposable.
No push or PR submission was performed.

| Validation | Recorded result |
| --- | --- |
| Complete backend unit and integration suites | Both passed. The first full attempt on `50dbaa500` cleared the Go test cache and passed both suites before stopping on the test-fixture lint issue. The final complete run on `804e31285` reused valid Go cache entries for unchanged unit packages; full PostgreSQL/Redis integration ran again and passed in 165.4s |
| Critical unit gate | All 290 required top-level tests / 2,223 required children executed and passed, without missing, skipped or unfinished required cases |
| Critical integration gate | All 127 required top-level tests / 507 required children executed and passed, without missing, skipped or unfinished required cases |
| Complete backend lint | Zero issues after the test-fixture correction |
| Frontend | Frozen install, lint, typecheck, 436 Vitest files / 3,979 tests, all 322 required frontend cases, production build and production dependency-audit exception checks passed |
| Chromium management | Browser TypeScript check and all 14 required scenarios passed, with no retries or skipped cases; real Vue/backend/PostgreSQL/Redis/proxy flows used isolated provider responders |
| Independent storage gate | Full migration/idempotency smoke and actual MinIO workflow both executed and passed, without skips |
| Focused race | All four selected unit packages and the selected PostgreSQL repository integration tests passed with `CGO_ENABLED=1` and `-count=1`; this is not a full repository race run |
| Policy and migrations | Every maintained full-profile CLI, identity, README, build-tag, lifecycle, health, deployment-policy, release-policy and packaging check passed. The maintained migration policy accepted both new SQL migrations |

The branch review adds 21 top-level regressions and 126 required children,
including the standalone uncertain-poll and concurrent-quota cases. Its
requirement inventory is [above](#post-commit-branch-review). Completed tool
validation and normal output-budget truncation have separate positive and
negative controls; the latter cannot silently become a provider failure.
Required critical gates execute their cases independently of full-suite cache
reuse. Compilation, fixture setup and lint errors are not business-defect
evidence.

The focused race selections were:

```bash
go test -C backend -race -tags=unit ./internal/service ./internal/handler ./internal/pkg/apicompat ./internal/server/middleware -run '^Test(ProviderFiles|ZhipuStartPlan|ZhipuOffPeak|StepFunReview|PartialFailureReview|GroupModelAllowlist|StepFunCapabilityLate401|CNAPIKey|KimiAPIKey|OpenAIQuota.*Owner|LiveSidebandProduction)' -count=1 -timeout=5m
go test -C backend -race -tags=integration ./internal/repository -run '^Test(ProviderFiles|ZhipuStartPlan|ZhipuOffPeak|CNAPIKey|CNControlSnapshot|CNBalanceSchedule|CNProxyOwner|OpenAIQuotaOwner)' -count=1 -timeout=5m
```

The frozen manifest contains 5,175 backend/frontend/tools/deploy inputs. Its
canonical sorted path-to-file-hash JSON has SHA-256
`c3a60599a8ab5ad9e8f654c349c8c0f170462f062e6cc8fcf2ea0142f34036c4`.
The closing check compares those bytes after the full matrix and independent
browser/storage/race runs; subsequent evidence updates change documentation
only. Logs, manifests and red/green records remain in the ignored local
OpenSpec plan. Optional live-service and external-plugin checks are not
established by full-suite success. Real-account entitlements, official service
availability and external browser/OS authorization remain separate acceptance
requirements.

### Capability-completion verification

The capability batch starts from `27c5e62f4` on
`fix/seven-platforms-main-sync`. The following checks ran on 2026-10-26 in Apple
Containers using image `sub2api-validation:8449ffd7679a1e27` and dependency-cache
generation `7b5b2a85be071953`. PostgreSQL and Redis services were disposable.
These are historical results for the capability worktree committed as
`488ca736c`; they do not establish passing results for subsequent review fixes.
The merge results below retain
their separate historical commit attribution.

| Validation | Recorded result |
| --- | --- |
| Complete backend unit suite | Passed, including the Step/Files/Start/off-peak production fixes; service package 307.768s |
| Complete backend integration suite | Passed against PostgreSQL/Redis, including the 72-cell Step business matrix; integration package 163.703s and repository 20.900s |
| Final repository integration | Passed again after the last recovery-test fixture change, 22.533s; backend source bytes unchanged afterward |
| Critical unit gate | All 271 required top-level tests / 2,100 required children ran and passed, including the final key-ID authentication and non-refunded-quota pins; no missing/skipped/unfinished required cases |
| Critical integration gate | All 125 required top-level tests / 504 required children ran and passed, without missing/skipped/unfinished required cases |
| Complete backend lint | Zero issues |
| Focused race | Service unit and repository integration selections both passed, 5.661s / 5.342s; exact scope below, not a full race suite |
| Frontend | Lint, typecheck, production build, 436 Vitest files / 3,979 tests, all 322 required frontend cases and 104 locale tests passed; frontend source bytes unchanged afterward |
| Chromium management | All 14 required scenarios and the browser TypeScript check passed, without skipped or retried cases; real Vue/backend/PostgreSQL/Redis/proxies with isolated provider responders |
| Policy and migrations | Module tidiness, build tags, Codex identity guard, README synchronization, maintained Compose environment/security/resource checks, Apple lifecycle fixture and Redis health checks passed. Migrations 297/298 were also checked directly with the maintained validator; existing SQL was byte-identical to the baseline. Final tool-only changes passed all 51 coverage-parser and six frontend-parser self-tests |

Full suites can contain optional live-service or external-artifact skips;
they do not prove those integrations. Required critical cases must actually run
and pass. This batch did not rerun the independent MinIO/S3 storage gate: Files
does not retain originals or alter the S3 result-storage implementation. Its
earlier passing result below remains attributed only to the earlier commit.

The browser run explicitly set `NODE_OPTIONS=--max-old-space-size=5120` in its
8 GiB validation container. Two earlier attempts failed when Vite's type-checking
worker exhausted its default JavaScript heap; the diagnostic run recorded
`ERR_WORKER_OUT_OF_MEMORY`, with no cgroup OOM kill. The final run retained all
business steps and assertions; it did not disable typechecking or accept retries.

The focused race selections used `CGO_ENABLED=1`, `-count=1`, and a five-minute
timeout per package:

```bash
go test -C backend -race -tags=unit ./internal/service -run '^Test(ProviderFiles|ZhipuStartPlan|ZhipuOffPeak|StepFunCapabilityLate401|CNAPIKey|KimiAPIKey|OpenAIQuota.*Owner|LiveSidebandProduction)' -count=1 -timeout=5m
go test -C backend -race -tags=integration ./internal/repository -run '^Test(ProviderFiles|ZhipuStartPlan|ZhipuOffPeak|CNAPIKey|CNControlSnapshot|CNBalanceSchedule|CNProxyOwner|OpenAIQuotaOwner)' -count=1 -timeout=5m
```

Defect evidence includes independently failing business assertions for Step
parallel tools, failed/incomplete streams and missing usage; Files cursor scope,
lock order and late publication/cleanup; Start route aliases and owner
representation; and off-peak Status/lock ordering. A final Go overlay restored
only the old terminated-segment retry branch and reproduced the real PostgreSQL
business failure: the old waiting flow incorrectly joined a new segment. The
unchanged regression passes with the fix. Compilation and setup failures are
not counted as defect evidence.

The backend/repository closing check compared 5,162 source inputs and found no
change against its frozen manifest. Only the critical-manifest checker and its
self-tests subsequently changed; both were revalidated together with the full
expanded unit gate. The completed manifest covers 5,162 backend/frontend/tools/
deploy inputs; SHA-256 of its canonical sorted path-to-file-hash JSON is
`8ac75041d269566b7f1ce3ccb57413ebba25221589e0a5f3d823e43bc49a1c67`.
Detailed command logs, source manifests and red/green records are retained in
the local ignored OpenSpec plan. No live-account permission, official service
availability or full browser/OS authorization is inferred from these fixtures.

### Current merge verification

This is the historical pre-capability merge record, not a result for the current
worktree. The following checks ran in Apple Containers on 2026-10-26. Backend unit,
integration, critical, lint and selected race runs used the final backend/tools
bytes committed in `85fb3c82c`. The validation image was
`sub2api-validation:8449ffd7679a1e27`, with dependency cache generation
`7b5b2a85be071953`. PostgreSQL/Redis and MinIO were disposable services.
Seven-business integration fixtures use separate databases so another package's
table truncation, outbox sequence reset or fault trigger cannot satisfy or
invalidate their cross-instance assertions.

| Validation | Recorded result |
| --- | --- |
| Complete backend unit suite | Passed with uncached execution; 6 existing conditional skips listed below |
| Complete backend integration suite | Passed with uncached execution against PostgreSQL/Redis; 3 conditional skips listed below |
| Critical unit gate | All 196 required top-level tests and 1,310 required children executed and passed |
| Critical integration gate | All 91 required top-level tests and 403 required children executed and passed |
| Complete backend lint | Zero issues |
| Focused service/repository race checks | Both passed with `CGO_ENABLED=1`; exact selections below, not a full race suite |
| Frontend | Frozen install, lint, typecheck, build, 435 Vitest files / 3,968 tests, 104 locale tests and all 311 required frontend cases passed |
| Chromium management | All 14 required scenarios and the browser TypeScript check passed; synthetic accounts and real Vue/backend/PostgreSQL/Redis/proxy responders |
| Independent storage gate | Migration/idempotency smoke and real MinIO workflow both executed and passed, without skips |
| Scoped policy/tool checks | Codex identity and guard, README synchronization, Go build tags, CI/gate/browser/frontend parser tests, migration policy, module tidiness, AGENTS contract and validation launcher regressions passed; coverage parser suite contained 49 tests |

The race selections were:

```bash
CGO_ENABLED=1 go test -C backend -race -tags=unit ./internal/service -run 'Test(CNAPIKey|KimiAPIKey|OpenAIQuota.*Owner|LiveSidebandProduction|ZhipuProject|ZhipuOffPeak|GrokRequestOutcome|GrokRecovery|NativeAuxiliary|NativeRefreshCoordinator|NativeBackgroundRefresh|UnsavedCredentialOwner|NativeAdapters)' -count=1 -timeout=600s
CGO_ENABLED=1 go test -C backend -race -tags=integration ./internal/repository -run 'Test(CNAPIKey|CNControlSnapshot|CNBalanceSchedule|CNProxyOwner|OpenAIQuotaOwner|OAuthOwner|CredentialSnapshotCache|ControlCASCacheBoundary)' -count=1 -timeout=600s
```

The final source check compared all 3,959 backend/tools files against each of
the five backend run manifests and found no byte differences. SHA-256 of the
canonical sorted path-to-file-hash JSON was
`c1e23ac5a5c67834aa9af9e1a7b682e2ccf420b7f46cbcaa59ece9253e3be3a2`.
Frontend sources remained unchanged after their passing run. The browser
management and storage executions preceded the final task-only CAS recovery,
integration-fixture isolation and gate-name correction; those changes did not
alter the exercised management or storage paths. Local detailed logs, source
manifests and earlier failing attempts remain in the ignored OpenSpec plan.

The six unit skips were the disabled DingTalk fixture, three manual/live TLS
checks, the legacy null-session fixture and live OpenAI token-count comparison.
Integration skipped manual TLS capture and an externally supplied signed plugin
artifact; its default MinIO skip was covered by the independent storage gate.
The real signed local plugin process still executed through the critical gate.
No required critical case skipped. These scoped merge checks do not constitute
the repository's complete `submit-pr` full profile, full-suite race execution,
real provider authorization or live-account entitlement acceptance.

### Historical surgeonfish verification

The following results were recorded in Apple Containers on 2026-10-25 for the
pre-merge surgeonfish source tree, later captured by `e2bbc80dc` and based on
`1818c2993`. They are retained as historical evidence for that source only.
They do not validate the new merge, the retained target-only changes, or the
combined test manifest. Current merged-source results are recorded above.

| Validation | Recorded result |
| --- | --- |
| Complete backend unit and integration suites | Both passed; integration used disposable PostgreSQL and Redis |
| Complete backend lint | Zero issues; the complete repository unit package also passed after strengthening its test type assertions |
| Critical unit gate | 188 top-level tests and 1,267 mandatory child scenarios passed |
| Critical integration gate | 90 top-level tests and 401 mandatory child scenarios passed |
| Separate storage gate | Both required top-level tests passed against disposable PostgreSQL/Redis/MinIO, without skips |
| Focused service/handler/admin race checks | Passed, including Grok credential failures, owner/runtime recovery, token counting and Live transport |
| Focused PostgreSQL/integration/plugin-process race checks | Passed, including Grok credential-failure CAS and concurrent proxy restoration, caller transactions/outbox, cross-instance recovery and the real signed local plugin process |
| Frontend checks | Lint, typecheck, browser TypeScript, build and all 399 Vitest files / 3,616 tests passed; this includes 104 locale tests; all 311 mandatory frontend cases passed |
| Final real Chromium management run | All 14 required scenarios passed on the final backend source, with no skips or retries; actual Vue/backend/PostgreSQL/Redis and local proxy responders |
| Tooling and policy | Gate/browser/frontend parser tests, CI-matrix and validation-runtime tests, AGENTS contract and its tests, build tags, Codex identity, module tidiness and whitespace passed |

That source tree's unit/integration manifests contained 278 top-level tests and 1,668
mandatory children; its storage profile added two top-level tests. These are
manifest counts, not a claim that every arbitrary combination is an end-to-end
scenario. On that source tree, all required critical Go cases executed without missing,
skipped, failed or unfinished results.

The historical record matched production Go hashes to its complete passing
suites. Its later backend delta strengthened four type assertions in
`account_repo_temp_unsched_test.go`, followed by the complete repository unit
package and full lint; its closing race/policy checks used that resulting source.
Those hash and unchanged-frontend statements describe only the pre-merge tree.
Historical logs, red/green defect evidence and source manifests remain in the
local ignored OpenSpec plan. The current section records the new source manifest
and gate results. Seven-provider fixtures use synthetic credentials and isolated
responders.

The historical complete-suite result included the repository's optional skips. Live
TLS/fingerprint/OpenAI comparison tests and compatibility with a separately
supplied official plugin artifact are not passing local fixture evidence.
In that historical run, the S3 workflow skipped without storage settings in the
ordinary suite and passed separately under the strict storage gate. The current
merge's independent storage execution is recorded above.
Required critical cases cannot use those optional skips.

`tools/check_seven_provider_coverage.py --profile unit|integration|storage` executes the
critical manifest through `go test -json -count=1`. The shared full local/CI
validation profile includes the unit and integration gates alongside the
complete existing suites; the storage gate requires the separate explicit
temporary service environment above.
Missing, skipped, failed or unfinished critical cases fail the gate. Exact test
and child counts come from the current `PROFILES` manifest and actual gate
output. It includes every provider/status in the persisted Chat matrix, every
native provider, API Key control failure/boundary cases, WHAM final writes,
Grok issuer-failure ownership and the real local plugin process. A passing
parent cannot hide a removed required child. The native coordinator/success/failure
groups require 4/21/12 child executions; ZCode quota 401/repeated-401/403 requires
two plan children per entrypoint, and the control-CAS cache race requires both
direct and managed transaction children. ID=0/JSON owner isolation and identical-grant
cross-account PAT isolation are independent top-level tests with their own run/pass
events; their source defines no child test names. This execution check does not
judge whether the assertions themselves establish business correctness
or expand the trusted-CI authority documented in [CI_VALIDATION.md](CI_VALIDATION.md).

For each defect, retain evidence that the business assertion fails before the
fix and passes afterward. Compilation/setup failures are not defect evidence.
Do not mark a critical case covered when it is skipped or its expected test is
absent. New sending entrypoints need their own transport and lifecycle assertions;
Chat-protocol tests do not establish coverage of native Messages or background
control clients. API-key balance/quota route validation happens before
singleflight admission, so a malformed bound owner cannot join a valid owner's
probe or acquire a replacement route. These API-key assertions supplement the
native OAuth control tests; neither set alone proves every control entrypoint.
Test inventories cannot automatically establish business correctness.

### Remaining acceptance boundaries

The following remain explicit boundaries, rather than passing scenarios:

- Complete official-agent workspace/TUI/OS behavior, browser-to-provider grants
  and live-account entitlements are not proven by gateway regression fixtures.
- Files support is limited to the [explicit first-batch contracts](providers/FILES.md):
  DeepSeek Anthropic images and MiniMax global API Key Chat understanding-video
  upload/local references. DeepSeek's other file families, MiniMax managed
  authorization and other regions/purposes, arbitrary media and unsupported
  remote-management operations remain excluded. Native-reference/inline
  compatibility does not prove Files ACL or CRUD.
- DeepSeek and ZCode business logins have no implemented refresh contract and
  require reauthorization when invalid. Kimi/MiniMax token refresh, StepFun's
  optional Passport-session renewal and ZCode project-PAT renewal are distinct
  capabilities; passing their refresh tests does not expand another provider's
  login capabilities or rotate StepFun's static inference key.
- The historical 21 persisted inference flows and additional Step business
  matrix each cover their own pinned dimensions when actually run; they do not
  compose every login, refresh, management and billing permutation into a single
  browser-to-provider test.
- Step remote search MCP has a reviewed contract but no gateway implementation
  in this batch. Native Responses/Messages/WS, a Responses WS bridge and extra
  speech/image billing are not inferred from the Chat execution matrix.
- Durable off-peak recovery is disabled by default and requires a shared dedicated
  external keyring. Process-local mode does not gain restart/cross-instance recovery;
  durable mode does not recover an official agent's historical task/queue position
  or replay a possibly accepted model request. Start balance permission never
  grants coding-project or off-peak permission. Grok enterprise deployment remains
  outside scope by the user's decision.
- Administrator termination is implemented only in durable mode. Its exact-pair
  retirement fences new admission to that segment; shared physical requests drain
  before asynchronous original-owner settlement. Account disable/delete, request
  cancellation and shutdown remain separate operations. No test establishes
  forced interruption or synchronous remote settlement.
- Management evidence is layered: component lifecycle and reauthorization tests
  use backend adapters; the 14 defined Chromium flows exercise the application,
  backend, PostgreSQL and Redis for the pinned proxy-management workflow when
  actually executed. Separate integration cases exercise the pinned
  cross-instance cache invalidation paths. These browser flows use synthetic
  account records and do not perform provider grants, inference or billing.
  Provider login protocol fixtures and UI callback tests remain separate
  evidence; neither proves a real external authorization/entitlement result.
- WHAM quota refresh/task writes now have PostgreSQL owner CAS evidence. This
  does not establish that every unrelated OpenAI credential writer uses that
  quota-only repository port.
- The pinned validation image contains a C compiler and libc development files
  for focused `-race` service and PostgreSQL repository checks. Their actual
  selected tests and final source must accompany any passing result; they do
  not establish a full-suite race result.
