# Seven-provider business regression matrix

Audit target: the `surgeonfish` working tree based on `1818c2993`, including its
pending changes. The earlier `2dab72aac` audit is a list of hypotheses to check,
not the source of implementation or coverage evidence for this tree.

Acceptance scope: all gateway business capabilities currently implemented for
these seven platforms. The [supported-business inventory](SEVEN_PROVIDER_SUPPORTED_BUSINESS.md)
defines applicable account types, protocols and platform-specific limits;
unimplemented official-agent capabilities remain explicitly unsupported.

This inventory maps business requirements to observable assertions. A test's
existence or a passing aggregate count does not establish complete provider
compatibility. Network tests below use production clients with isolated local
servers/proxies; they do not use subscription accounts or prove live entitlement.

## Protocol sources

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

## Login, transport and lifecycle assertions

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
| Live create failover and persistence failures | `service/openai_live_create_business_test.go` | Actual CreateLiveCall and scheduler with two real HTTP proxies; A fails, B owns the saved mapping and live lease; bearer/identity captured; ordinary slots always released; failed save, lease acquisition or cancellation leaves no invalid mapping or leaked capacity. Mapping, lease and concurrency use in-memory test fixtures; injected save failures do not establish real Redis storage-fault behavior |
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

## Observations, persistence and recovery

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
| Same proxy row changes while a request is in flight | `TestCNProxyOwnerCASRejectsSameRowRouteEditsWithoutSideEffects`, `TestCNProxyOwnerCASWaitsForConcurrentProxyEditAndRechecks` | Real PostgreSQL: endpoint/auth/protocol/status/expiry/deletion changes rejected; wait for concurrent proxy writer and recheck; no old credentials/usage/cooldown, outbox or cache publication |
| Existing-account login completes atomically | `TestCNOAuthLoginCASRecoveryCommitsWithCredentialsAndOutbox`, `TestCNOAuthLoginCASStorageFaultLeavesOriginalLoginAndRecoveryState`, `TestCNOAuthLoginCASPreservesNewerRuntimeFailures` | Credentials and old-error recovery commit together; storage failure keeps original state; a newer cooldown/manual pause cannot be erased by a delayed login; unrelated Extra survives |
| Automatic Codex quota reset is already covered | `TestQuotaFollowReset_ConcurrentObservationBillingAndWorkers` and the other `TestQuotaFollowReset_*` integration cases | Concurrent observations create one persisted event; concurrent charges survive competing reset workers; source, activation, membership, transaction and cache ordering checked. The earlier blanket “persisted concurrency/idempotency missing” claim does not apply to this tree |
| Native media and tool audit ordering | `handler/domestic_provider_audit_*_test.go`, `service/domestic_provider_native_payload_test.go` | Canonical content extraction and accepted unknown structures; audit occurs before account/billing/slot/upstream effects; supported native media/tool fields retain their semantics |
| StepFun catalog and actual model admission | `service/stepfun_model_restrictions_test.go` | CN/global catalog contract, invalid sessions, cancelled responses and failed discovery preserve the grant appropriately; OAuth/API Key account selection enforces the requested model restriction |
| Independent token-count entrypoints | `service/openai_gateway_count_tokens_test.go`, `openai_responses_input_tokens_test.go` | The OpenAI API Key Messages count bridge sends Responses input_tokens, including for custom OpenAI base URLs; Codex profile admission precedes credential lookup. Native Responses counting estimates Grok/custom-relay input locally without sending. Applicable unsupported OAuth endpoints and native upstream 404 use the documented estimate. Fixed request samples pin independent numeric expectations |
| Token-count owner and production proxy transport | `service/openai_count_tokens_owner_contract_test.go`, `repository/openai_count_tokens_proxy_wire_test.go` | Native Responses input_tokens and Messages count bridge independently reject broken bindings before token lookup and retain captured credentials/identity/route during edits. Real authenticated CONNECT/TLS sends to the official HTTPS hostname cover bound/direct success, refusal, timeout, 407 and missing binding; failures produce zero direct/target connections and release transport slots. Applicable local estimates perform no credential or network work |
| Live finalization and zero-usage billing | `service/openai_live_lifecycle_test.go` | Expiry, lease loss and temporary store outage terminate through the existing lifecycle; repeated finalization writes one zero-usage record. Usage persistence falls back to synchronous creation when its async writer is unavailable |
| Grok search, audio and video billing | `service/openai_gateway_grok_search_billing_test.go`, `grok_media_video_billing_test.go`, `handler/grok_audio_billing_test.go` | JSON/SSE search calls are counted and deduplicated. Video creation/pending/failed status does not bill; official done status uses observed parameters. Realtime billing requires observed audio and a stable unique identifier, with pending-turn deduplication. These focused billing assertions supplement the persisted inference debit flows |

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

The current acceptance inventory is mapped to executable regressions at the
observable boundaries stated above. The following checks passed in Apple
Containers on 2026-10-25 for the pending production tree based on `1818c2993`:

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

The unit/integration critical manifests contain 278 top-level tests and 1,668
mandatory children; the storage profile adds two top-level tests. These are
manifest counts, not a claim that every arbitrary combination is an end-to-end
scenario. All required critical Go cases executed without missing, skipped, failed or
unfinished results.

Production Go source hashes match the complete passing backend suites. The
only later backend delta strengthens four type assertions in the unit-only
`account_repo_temp_unsched_test.go`; an exact reverse-hash check limits the
delta to those assertions, followed by the complete repository unit package
and full lint. Closing race/policy checks use the resulting frozen source.
Frontend source has not changed since its full validation. Logs, red/green
defect evidence and source manifests remain in the local ignored OpenSpec
plan. The seven-provider fixtures use synthetic credentials and isolated
responders.

Complete-suite success includes the repository's optional skips. Live
TLS/fingerprint/OpenAI comparison tests and compatibility with a separately
supplied official plugin artifact are not passing local fixture evidence.
The S3 workflow skipped without storage settings in the ordinary integration
suite was executed successfully by the separate strict storage gate above.
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
parent cannot hide a removed required child. This execution check does not
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

The following remain explicit boundaries, rather than passing scenarios:

- Complete official-agent workspace/TUI/OS behavior, browser-to-provider grants
  and live-account entitlements are not proven by gateway regression fixtures.
- General DeepSeek/MiniMax Files upload, metadata/ownership tracking, expiry and
  deletion are outside the current gateway routes. Supported native file
  references and inline-media forwarding do not implement these management operations.
- The combined 21 inference flows cover their pinned protocols and account
  types; they do not compose every login, refresh, management and billing
  permutation into a single browser-to-provider test.
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
