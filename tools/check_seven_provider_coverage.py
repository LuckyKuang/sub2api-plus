#!/usr/bin/env python3
"""Execute required seven-provider regressions and reject missing/skipped cases.

Owning business requirements and remaining limits: docs/SEVEN_PROVIDER_TEST_MATRIX.md.
This gate supplements the complete Go suites; it does not establish full business
coverage or permit a skipped integration environment to count as a passing run.
"""

from __future__ import annotations

import argparse
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from typing import Iterable

import validation_runtime


ROOT = Path(__file__).resolve().parents[1]


@dataclass(frozen=True)
class RegressionGroup:
    package: str
    tests: tuple[str, ...]
    required_subtests: tuple[str, ...] = ()


FAILOVER_TEST = "TestSevenProviderChatFailoverPreservesWireOwnerStickySessionAndBilling"
CHAT_PROXY_TEST = "TestSevenProviderChatProductionRejectsBrokenProxyBindingsBeforeSend"
PROVIDERS = ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
BUSINESS_CHAT_TEST = "TestSevenProviderFailoverPersistsRedisAffinityAndPostgresDebits"
BUSINESS_NATIVE_TEST = "TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits"
APIKEY_OBSERVATION_TEST = "TestCNAPIKeyObservationRejectsLateOwnerAndPersistenceFailure"
APIKEY_SCHEDULING_TEST = "TestCNAPIKeyWalletSchedulingRejectsChangesAfterObservation"
APIKEY_CODING_TEST = "TestCNAPIKeyCodingQuotaRejectsUnrecognizedOrMalformedObservation"
APIKEY_BOUNDED_RESPONSE_TEST = "TestCNAPIKeyControlRequiresCompleteBoundedResponse"
DEEPSEEK_WALLET_TEST = "TestDeepSeekAPIKeyWalletRejectsIncompleteObservationBeforeScheduling"
APIKEY_REDIRECT_TEST = "TestCNAPIKeyControlProductionNeverFollowsRedirects"
APIKEY_POSTGRES_OBSERVATION_TEST = "TestCNAPIKeyControlServicePersistsOnlyCurrentOwnerInPostgres"
APIKEY_POSTGRES_SCHEDULING_TEST = "TestCNAPIKeyBalanceScheduleCASRetainsOwnerAndScheduler"
LIVE_PROXY_TEST = "TestLiveSidebandProductionProxyHandshakeAndFailureNeverDialDirect"
ZHIPU_START_PLAN_TEST = "TestZhipuStartPlanMaterialForwardsOfficialBearerWithoutManualScheme"
STRICT_BINDINGS = ("valid-bound", "explicitly-unbound", "missing-bound-snapshot",
                   "mismatched-bound-owner", "malformed-bound-host", "incomplete-bound-auth")
GROK_PROXY_MODES = ("success", "refused", "timeout", "authentication-required", "cancelled", "edited-after-connect")
GROK_ADMIN_MODES = ("default", "text", "image", "video", "search", "tts", "stt", "realtime")
GROK_HTTP_ACTIONS = ("tts", "stt", "custom-voice-create", "custom-voice-list", "custom-voice-get",
                    "custom-voice-audio", "custom-voice-delete", "image-create", "image-edit",
                    "video-create", "video-edit", "video-extend", "video-status", "video-content")
GROK_HTTP_PREFLIGHT = "TestGrokAuxiliaryHTTPRejectsBrokenProxyBeforeCredentialOrSend"
GROK_WS_PREFLIGHT = "TestGrokRealtimeRejectsBrokenProxyBeforeDial"
GROK_WS_WIRE = "TestGrokRealtimeProductionProxyHandshakeAndFailureNeverDialDirect"
GROK_HTTP_WIRE = "TestGrokAuxiliaryHTTPProductionProxyContract"
MODEL_OBSERVATION_OWNER = "TestSevenProviderModelObservationRequiresCurrentCredentialAndProxyOwner"
GROK_BRIDGE_OWNER = "TestGrokChatResponsesBridgeRetainsErrorObservationOwner"
GROK_BRIDGE_PREFLIGHT = "TestGrokChatResponsesBridgeRejectsBrokenProxyBeforeCredentialLookup"
GROK_REQUEST_TOKEN_OWNER = "TestGrokRequestTokenAcquisitionRetainsCapturedOwner"
GROK_MANUAL_TOKEN_OWNER = "TestGrokManualTokenRefreshRetainsCapturedOwner"
GROK_TOKEN_PROFILE = "TestGrokOwnedTokenRotationPreservesOfficialProfileMetadata"
OAUTH_RUNTIME_RECOVERY = "TestOAuthCredentialRecoveryOnlyClearsCapturedRuntimeGeneration"
GROK_RESET_PUBLICATION = "TestGrokManualResetRejectsPreResetOutcomePublication"
GROK_ADMIN_RESET = "TestAdminResetGrokAccountClearsItsModelOwnersAndPreservesTeamLimits"
NATIVE_UNSUPPORTED_REFRESH = "TestNativeAccountsWithoutBusinessRefreshNeverUseForeignIssuer"
NATIVE_REFRESH_COORDINATOR = "TestNativeBackgroundRefreshRequiresCoordinator"
NATIVE_REFRESH_SUCCESS = "TestNativeRefreshCoordinatorFencesSuccessBeforeCleanup"
NATIVE_REFRESH_FAILURE = "TestNativeBackgroundRefreshFailureCannotMutateGrantOrScheduling"
UNSAVED_CREDENTIAL_OWNER = "TestUnsavedCredentialOwnerSurvivesSnapshotsWithoutCrossingAccountOrJSONBoundary"
PROJECT_TOKEN_ACCOUNT_ISOLATION = "TestProjectTokenCacheSeparatesIdenticalGrantsAcrossAccounts"
ZHIPU_QUOTA_401_RECOVERY = "TestZhipuProjectQuotaRecoversExplicit401Once"
ZHIPU_QUOTA_FINAL_401 = "TestZhipuProjectQuotaRejectedPATRequiresFreshIssuance"
ZHIPU_QUOTA_403 = "TestZhipuProjectQuotaForbiddenDoesNotReplayOrEvict"
CONTROL_CAS_CACHE_BOUNDARY = "TestControlCASCacheBoundaryCannotRepublishSupersededLogin"
OAUTH_OWNER_DTO = "TestAccountDTOExposesOpaqueOAuthOwnerWithoutLeakingCredentials"
ADMIN_RECOVERY = "TestSevenProviderAdminRecoveryCommitsAllStateOrNothing"
ADMIN_APPLY = "TestAdminApplyOAuthCredentialsKeepsFullOwnerAndAtomicRecovery"
ADMIN_REFRESH = "TestAdminSingleAndBatchRefreshCannotPublishLateOAuthOwner"
ADMIN_APPLY_PREHANDLER = "TestAdminApplyOAuthCredentialsRejectsOwnerChangedBeforeHandlerRead"
GROK_CROSS_PROCESS_RECOVERY = "TestGrokAdminResetReachesFailureHoldingProcessThroughOutbox"
COUNT_TOKENS_PREFLIGHT = "TestCountTokensUpstreamRejectsBrokenProxyBeforeCredentialLookup"
COUNT_TOKENS_OWNER = "TestCountTokensUpstreamKeepsCapturedOwnerDuringTokenLookup"
COUNT_TOKENS_LOCAL = "TestCountTokensLocalEstimatesKeepZeroCredentialAndNetworkEffects"
COUNT_TOKENS_WIRE = "TestCountTokensProductionProxyContract"
STEP_PROFILES = ("oauth_plan_cn", "oauth_plan_global", "apikey_payg_cn", "apikey_payg_global",
                 "apikey_plan_cn", "apikey_plan_global")
STEP_PROTOCOLS = ("chat", "responses", "messages")
STEP_BUSINESS_TEST = "TestStepFunBusinessMatrix"
CAPABILITY_SERVICE_TESTS = (
    "TestStepFunCapabilityBasicMatrix",
    "TestStepFunCapabilityCompletedWithoutUsageKeepsUnknownBillingSource",
    "TestStepFunCapabilityParallelToolsAndResults",
    "TestStepFunCapabilityFailuresNeverBecomeSuccess",
    "TestStepFunCapabilityStreamFailureAndTerminationContract",
    "TestStepFunCapabilityLate401CannotQuarantineReplacementOwner",
    "TestStepFunPricingOfficialRatesAndReferenceCoverage",
    "TestStepFunPricingCatalogAndChannelOverrides",
    "TestStepFunOfficialCachedUsageAcrossProtocols",
    "TestStepFunCreditsAndWalletsNeverBecomePercentageQuota",
    "TestProviderFilesUploadWireAndIdempotentReuse",
    "TestProviderFilesUncertainAndCompensation",
    "TestProviderFilesMiniMaxBusinessFailureAndChatReference",
    "TestProviderFilesDeleteFailureRecoveryDoesNotRepeat",
    "TestProviderFilesDeepSeekUploadInferenceReuse",
    "TestProviderFilesMiniMaxUploadActualChatInference",
    "TestProviderFilesRejectCredentialReplacementAtActualSend",
    "TestProviderFilesCanonicalAuditContract",
    "TestProviderFilesModerationAllowsBinaryAndLogsSafeDiagnostics",
    "TestProviderFilesContractValidation",
    "TestProviderFilesPrincipalDoesNotUseProxyOrDisplayIdentity",
    "TestProviderFilesRewriteAndACL",
    "TestProviderFilesUnknownLocalIDNeverBecomesNative",
    "TestProviderFiles64MiBContentBoundary",
    "TestProviderFilesRestartCleanupUsesKnownOriginalSnapshot",
    "TestProviderFilesMultipleOwnerReferencesFailBeforeSend",
    "TestProviderFilesSchedulerCannotFailoverToDifferentFileOwner",
    "TestProviderFilesExplicitRetryPinsOriginalOwner",
    "TestProviderFilesMiniMaxLocalRevokeIsExplicitlyUnsupportedRemoteDelete",
    "TestZhipuStartPlanOfficialBalanceWireAndIsolation",
    "TestZhipuStartPlanAvailabilityBoundaries",
    "TestZhipuStartPlanFailureAndOwnerFence",
    "TestZhipuStartPlanSelectionAndPublicCatalog",
    "TestZhipuStartPlanObservationCannotCrossOwnerOrReactivateOnTime",
    "TestZhipuStartPlanCatalogSyncUsesBalanceAndNeverProjectOrRegistry",
    "TestZhipuStartPlanIdentityPrecedenceAndRequiredLogin",
    "TestZhipuStartPlanPublicCatalogRechecksCachedOwnerAndIdentity",
    "TestZhipuOffPeakSnapshotEncryptionAndRotation",
    "TestZhipuOffPeakSnapshotKeyIDIsAuthenticated",
    "TestZhipuOffPeakSettlementDoesNotRestoreTakeEligibility",
    "TestZhipuOffPeakGenerationTracksOwnerNotObservations",
    "TestZhipuOffPeakDurableUncertainTakeNeverReplaysAfterRestart",
    "TestZhipuOffPeakDurableQueueTimeoutRetainsTicketForNextRequest",
    "TestZhipuOffPeakDurableTakeCommitFailureCannotAdmit",
    "TestZhipuOffPeakDurableSettlementACKMustPersistBeforeClear",
    "TestZhipuOffPeakDurableLostHeartbeatPreservesRequestSafetyWindow",
    "TestZhipuOffPeakDurableMissingStatusNeverClearsOrRetakes",
    "TestZhipuOffPeakDurableRetentionWipesSnapshotWithoutChangingOwner",
    "TestZhipuOffPeakMissingStatusPreservesExistingTicket",
    "TestZhipuOffPeakAdminSegmentRejectsUnsupportedModeAndProfile",
    "TestZhipuOffPeakAdminSegmentMapsErrorsWithoutSecretDisclosure",
    "TestZhipuOffPeakAdminTerminateDrainsAndRetriesOriginalSnapshot",
    "TestStepFunReviewMessagesRejectsInvalidCompletedToolInput",
    "TestProviderFilesReviewEscapedIDsPreserveACLRewriteAndOwner",
    "TestProviderFilesReviewMiniMaxRejectsNonVideoISOBrands",
    "TestZhipuOffPeakMemoryPartialTakeRetainsKnownIDWithoutAdmissionOrRetake",
    "TestZhipuOffPeakMemoryTakeWithoutTicketEvidenceCannotAdmit",
    "TestZhipuOffPeakMemoryUncertainStatusFailureKeepsBoundedPollWait",
    "TestStepFunReviewMessagesPreservesMaxTokenToolTruncation",
    "TestStepFunReviewResponsesPreservesMaxTokenToolTruncation",
    "TestZhipuOffPeakDurablePartialTakeRetainsEvidenceBeforeRestartAdmission",
    "TestProviderFilesReviewInferenceAuditKeepsTextAndOpaqueReferencesSeparate",
)
CAPABILITY_SERVICE_SUBTESTS = tuple(
    f"{test}/{profile}/{protocol}/stream={stream}"
    for test in ("TestStepFunCapabilityBasicMatrix", "TestStepFunCapabilityParallelToolsAndResults",
                 "TestStepFunCapabilityCompletedWithoutUsageKeepsUnknownBillingSource")
    for profile in STEP_PROFILES for protocol in STEP_PROTOCOLS for stream in ("false", "true")
) + tuple(
    f"TestStepFunCapabilityFailuresNeverBecomeSuccess/{profile}/{protocol}/status={status}"
    for profile in STEP_PROFILES for protocol in STEP_PROTOCOLS for status in (401, 403, 429, 503, 200)
) + tuple(
    f"TestStepFunCapabilityStreamFailureAndTerminationContract/{profile}/{protocol}/{scenario}"
    for profile in STEP_PROFILES for protocol in STEP_PROTOCOLS
    for scenario in ("empty_connection", "partial_clean_eof", "partial_read_failure", "error_envelope",
                     "error_after_output_and_usage", "cancelled_after_usage", "deadline_after_usage",
                     "complete_without_usage", "missing_done_with_finish", "missing_done_with_usage",
                     "complete_exact_usage")
) + tuple(
    f"TestStepFunCapabilityLate401CannotQuarantineReplacementOwner/{profile}/{protocol}/{change}"
    for profile in STEP_PROFILES for protocol in STEP_PROTOCOLS
    for change in ("unchanged", "credential_replaced", "proxy_rebound", "same_proxy_edited", "region_changed")
) + tuple(
    f"TestStepFunPricingOfficialRatesAndReferenceCoverage/catalog={catalog}/{model}"
    for catalog in ("false", "true")
    for model in ("step-5-preview", "step-3.7-flash", "step-3.5-flash", "step-3.5-flash-2603", "step-1o-turbo-vision")
) + tuple(
    f"TestStepFunOfficialCachedUsageAcrossProtocols/{kind}/{protocol}/stream={stream}"
    for kind in ("oauth", "apikey") for protocol in STEP_PROTOCOLS for stream in ("false", "true")
) + tuple(
    f"{test}/{scenario}"
    for test, scenarios in (
        ("TestStepFunCreditsAndWalletsNeverBecomePercentageQuota", ("token-plan", "credit-bucket", "cny-wallet", "usd-wallet")),
        ("TestProviderFilesUploadWireAndIdempotentReuse", ("apikey", "oauth")),
        ("TestProviderFilesUncertainAndCompensation", (
            "lost_response_never_replays", "mapping_failure_cannot_publish_and_cleans_known_remote")),
        ("TestProviderFilesContractValidation", ("DeepSeek_actual_media_bytes_and_expiry",
            "MiniMax_unsupported_purpose_never_falls_through", "filename_length_is_bytes")),
        ("TestProviderFilesRewriteAndACL", ("other_key", "other_user", "other_group", "revoked", "expired")),
        ("TestProviderFilesRestartCleanupUsesKnownOriginalSnapshot", (
            "same-owner", "proxy-changed", "credential-changed", "unknown-remote",
            "account-disabled", "proxy-disabled", "proxy-expiry-changed")),
        ("TestZhipuStartPlanOfficialBalanceWireAndIsolation", ("bigmodel", "zai")),
        ("TestZhipuStartPlanAvailabilityBoundaries", (
            "missing-models", "empty-balances-unknown", "explicit-empty-capability", "expired-model-bucket",
            "foreign-model-scope", "pending", "date-beats-old-json-clock", "no-active-start", "missing-plans",
            "wrong-plans-type", "damaged-active-plan", "missing-product-identity", "invalid-server-clock",
            "missing-future-effective-time", "damaged-capability", "business-failure", "invalid-json")),
        ("TestZhipuStartPlanFailureAndOwnerFence", (
            "network", "401", "403", "relink", "plan", "estate", "proxy", "save-failure")),
        ("TestZhipuStartPlanSelectionAndPublicCatalog", (
            "no-observation", "models-unknown", "explicit-empty", "authorized", "unauthorized", "pending", "unavailable")),
        ("TestZhipuStartPlanObservationCannotCrossOwnerOrReactivateOnTime", (
            "token", "plan", "estate", "proxy", "identity", "stale-positive", "stale-negative", "pending-elapsed", "expired")),
        ("TestZhipuStartPlanIdentityPrecedenceAndRequiredLogin", (
            "global-version", "account-version", "legacy-no-mode", "missing-jwt")),
        ("TestZhipuOffPeakSnapshotEncryptionAndRotation", ("record", "account", "generation", "task", "ciphertext")),
        ("TestZhipuOffPeakDurableMissingStatusNeverClearsOrRetakes", (
            "empty", "missing-ticket", "unknown-state", "conflicting-duplicate")),
        ("TestZhipuOffPeakAdminSegmentRejectsUnsupportedModeAndProfile", (
            "memory-mode", "nil-account", "invalid-id", "other-platform", "coding-plan", "unsupported-type")),
        ("TestZhipuOffPeakAdminSegmentMapsErrorsWithoutSecretDisclosure", (
            "not-found", "database-failure", "fence-conflict", "empty-record", "empty-generation")),
    ) for scenario in scenarios
) + tuple(
    f"TestZhipuOffPeakMissingStatusPreservesExistingTicket/{initial}/{scenario}"
    for initial in ("ready", "queued")
    for scenario in ("empty", "unmatched", "unknown-state", "conflicting-duplicate", "duplicate-ready")
) + tuple(
    f"TestStepFunReviewMessagesRejectsInvalidCompletedToolInput/stream={stream}/{scenario}"
    for stream in ("false", "true")
    for scenario in ("object", "empty_arguments", "whitespace_arguments", "truncated_json",
                     "trailing_json", "array_input", "scalar_input", "null_input")
) + tuple(
    f"TestProviderFilesReviewEscapedIDsPreserveACLRewriteAndOwner/{platform}/{encoding}/{access}"
    for platform in ("deepseek", "minimax") for encoding in ("hyphens", "first-letter")
    for access in ("authorized", "other-key", "unknown")
) + tuple(
    f"{test}/{scenario}"
    for test, scenarios in (
        ("TestProviderFilesReviewMiniMaxRejectsNonVideoISOBrands", (
            "mp4-isom", "mp4-v1", "mp4-v2", "mov-quicktime", "avif-image",
            "avif-image-sequence", "heic-image", "heix-image",
            "generic-major-avif-compatibility", "generic-major-heif-compatibility")),
        ("TestZhipuOffPeakMemoryPartialTakeRetainsKnownIDWithoutAdmissionOrRetake", (
            "unknown-state", "missing-or-empty-state", "partial-error-with-ready-state")),
        ("TestZhipuOffPeakMemoryTakeWithoutTicketEvidenceCannotAdmit", (
            "nil-ticket", "missing-ticket-id")),
        ("TestZhipuOffPeakDurablePartialTakeRetainsEvidenceBeforeRestartAdmission", (
            "ready-with-error", "queued-with-error", "unknown-with-error")),
    ) for scenario in scenarios
) + tuple(
    f"TestStepFunReviewMessagesPreservesMaxTokenToolTruncation/stream={stream}/{scenario}"
    for stream in ("false", "true")
    for scenario in ("length_complete_object", "length_truncated_object",
                     "tool_calls_truncated_control", "stop_truncated_control")
) + tuple(
    f"TestProviderFilesReviewInferenceAuditKeepsTextAndOpaqueReferencesSeparate/{protocol}/{encoding}"
    for protocol in ("chat", "responses", "messages") for encoding in ("literal", "escaped")
) + tuple(
    f"TestStepFunReviewResponsesPreservesMaxTokenToolTruncation/stream={stream}/{scenario}"
    for stream in ("false", "true")
    for scenario in ("length_complete_object", "length_truncated_object",
                     "tool_calls_truncated_control", "stop_truncated_control")
)
CAPABILITY_REPOSITORY_TESTS = (
    "TestProviderFilesPersistentUploadReferenceAndRestartCleanup",
    "TestProviderFilesRepositoryPersistenceIdempotencyAndFencing",
    "TestProviderFilesRepositoryCrossInstanceUploadReservation",
    "TestProviderFilesRepositoryQuotaAndProxyCAS",
    "TestProviderFilesRepositoryPaginationCursorCannotReadAnotherScope",
    "TestProviderFilesRepositoryOwnerCASLocksProxyBeforeAccount",
    "TestProviderFilesRepositoryOwnerCASRejectsChangedAdmissionSnapshot",
    "TestZhipuStartPlanPostgresCommitOutboxAndCrossInstanceSelection",
    "TestZhipuStartPlanPostgresLateObservationCannotReplaceNewObservation",
    "TestZhipuStartPlanPostgresOwnerAndOutboxFailuresPreserveObservation",
    "TestZhipuOffPeakStoreCrossInstanceOpenHasOneCurrentGeneration",
    "TestZhipuOffPeakStoreExpiredOperationLeaseFencesLateSaveAndAdmission",
    "TestZhipuOffPeakStoreBeginTakeCrashRemainsUncertainAfterRestart",
    "TestZhipuOffPeakStoreSharedRequestLeasesBlockSettlementUntilAllFinish",
    "TestZhipuOffPeakStoreSettlementRetryKeepsIntentUntilAcknowledged",
    "TestZhipuOffPeakStoreRetiresOldOwnerAndRegistersLateTicketForOriginalGeneration",
    "TestZhipuOffPeakStoreAdmissionRejectsDifferentCredentialOwner",
    "TestZhipuOffPeakStoreFailedAckWriteCannotErasePendingIntent",
    "TestZhipuOffPeakStoreRetiredGenerationWaitsForOriginalInflightRequest",
    "TestZhipuOffPeakStoreSettlementRechecksRequestLeaseAfterParentLockWait",
    "TestZhipuOffPeakStoreReleaseLocksParentBeforeRequestLease",
    "TestZhipuOffPeakManagerPostgresQueueTimeoutRestartsWithSameTicketStatus",
    "TestZhipuOffPeakManagerPostgresAcceptedTakeResponseLossDoesNotRetakeAfterRestart",
    "TestZhipuOffPeakManagerPostgresConfirmedExpiryCreatesOneNewTaskAndTicket",
    "TestZhipuOffPeakManagerPostgresSettledTicketDoesNotRestoreTakeQuota",
    "TestZhipuOffPeakStoreTerminateFencesOldSegmentAndIsIdempotent",
    "TestZhipuOffPeakStoreTerminateWaitsForSharedInflightRequests",
    "TestZhipuOffPeakStoreTerminateRejectsUnmatchedOwnerAndGeneration",
    "TestZhipuOffPeakStoreTerminateDatabaseFailureKeepsOriginalIntent",
    "TestZhipuOffPeakStoreTerminateRetainsLateTakeOnlyForCleanup",
    "TestZhipuOffPeakManagerPostgresTerminateCannotRetakeFromOldAcquire",
    "TestZhipuOffPeakManagerPostgresKnownStatusFailureRecoversSameTicketAfterRestart",
    "TestZhipuOffPeakManagerPostgresAdmissionFailureRecoversSameTicketAfterRestart",
    "TestZhipuOffPeakManagerPostgresConcurrentQuotaRejectionPreservesResetTime",
    "TestZhipuOffPeakManagerPostgresUnknownTakeStateRetainsKnownTicketForRestart",
)

REVIEW_HANDLER_TESTS = (
    "TestPartialFailureReviewKeepsSingleSchedulingOutcomeAndBill",
    "TestProviderFilesReviewHandlerLocalIDsRespectProtocolMatrix",
    "TestProviderFilesReviewMiniMaxBridgesIgnoreNonReferenceMentions",
    "TestProviderFilesReviewMiniMaxBridgesRejectActualLocalReferences",
    "TestProviderFilesReviewDeepSeekBridgesNeverDiscardLocalImages",
    "TestProviderFilesReviewDeepSeekBridgesKeepNativeAndInlineCompatibility",
)
REVIEW_HANDLER_SUBTESTS = tuple(
    f"TestPartialFailureReviewKeepsSingleSchedulingOutcomeAndBill/{protocol}"
    for protocol in ("chat", "responses", "messages")
) + tuple(
    f"TestProviderFilesReviewHandlerLocalIDsRespectProtocolMatrix/{protocol}/{encoding}/known={known}"
    for protocol in ("chat_completions", "anthropic", "responses")
    for encoding in ("literal", "hyphens", "first-letter") for known in ("true", "false")
) + tuple(
    f"TestProviderFilesReviewMiniMaxBridgesIgnoreNonReferenceMentions/{protocol}/{scenario}"
    for protocol in ("anthropic", "responses")
    for scenario in ("plain-text", "text-block", "tool-input", "unknown-sibling",
                     "native-reference-with-unknown-sibling")
) + tuple(
    f"TestProviderFilesReviewMiniMaxBridgesRejectActualLocalReferences/{protocol}/{encoding}"
    for protocol in ("anthropic", "responses") for encoding in ("literal", "escaped")
) + tuple(
    f"TestProviderFilesReviewDeepSeekBridgesNeverDiscardLocalImages/{shape}"
    for shape in ("native-image", "chat-file", "chat-image-url", "responses-file", "responses-image")
) + tuple(
    f"TestProviderFilesReviewDeepSeekBridgesKeepNativeAndInlineCompatibility/{protocol}/{shape}"
    for protocol in ("chat_completions", "responses") for shape in ("native-reference", "inline-image")
)

# These names deliberately pin the business cases rather than discovering cases
# from the implementation. Removing a provider, mutation or invalid observation
# from a Go table must make the gate fail even when its parent still passes.
APIKEY_OBSERVATION_SUBTESTS = tuple(
    f"{APIKEY_OBSERVATION_TEST}/{probe}/{change}"
    for probe in ("deepseek-payg", "kimi-payg", "kimi-coding", "zhipu-coding")
    for change in ("api-key", "nested-credentials", "proxy-address", "proxy-auth",
                   "proxy-status", "proxy-expiry", "unbound", "storage-failure", "same-owner")
)
APIKEY_CODING_SUBTESTS = tuple(
    f"{APIKEY_CODING_TEST}/{probe}/{kind}/{body}"
    for probe, observations in (
        ("kimi-coding", (
            ("missing", '{}'), ("unknown", '{"unrelated":"metadata"}'),
            ("recognized", '{"limits":[{"detail":{"limit":100,"remaining":75}}],"usage":{"limit":1000,"remaining":800}}'),
            ("recognized", '{"limits":[{"detail":{}}]}'),
            ("recognized", '{"usage":{"limit":100}}'),
            ("recognized", '{"usage":{"limit":0,"remaining":0}}'),
            ("recognized", '{"usage":{"limit":"NaN","remaining":0}}'),
            ("recognized", '{"limits":[{"detail":{"limit":100,"remaining":75}}],"usage":{"limit":1000,"remaining":800}'),
        )),
        ("zhipu-coding", (
            ("missing", '{}'), ("unknown", '{"unrelated":"metadata"}'),
            ("recognized", '{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}],"level":"max"}}'),
            ("recognized", '{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3}]}}'),
            ("recognized", '{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":"bad","unit":3}]}}'),
            ("recognized", '{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}],"level":"max"}'),
        )),
        ("minimax-coding", (
            ("missing", '{}'), ("unknown", '{"unrelated":"metadata"}'),
            ("recognized", '{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":100}]}'),
            ("recognized", '{"model_remains":[{"model_name":"general"}]}'),
            ("recognized", '{"model_remains":[{"model_name":"general","current_interval_remaining_percent":"Inf"}]}'),
            ("recognized", '{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":100}]'),
        )),
    )
    for kind, body in observations
)

PROFILES = {
    "unit": (
        RegressionGroup("./internal/securityaudit", (
            "TestProviderFilesPromptAuditAllowsBinaryAndLogsSafeDiagnostics",
        )),
        RegressionGroup("./internal/config", ("TestZhipuOffPeakPersistenceConfiguration",), tuple(
            f"TestZhipuOffPeakPersistenceConfiguration/{scenario}"
            for scenario in ("default_remains_explicitly_process_local", "missing-key", "wrong-length", "bad-json",
                             "missing-active", "retention", "stable_env_keyring_and_retention")
        )),
        RegressionGroup("./internal/pkg/apicompat", (
            "TestStepFunParallelChatToolsKeepIndependentAnthropicBlockLifecycles",
            "TestStepFunReviewToolStreamKeepsClosedBlocksImmutable",
        ), tuple(
            f"TestStepFunReviewToolStreamKeepsClosedBlocksImmutable/{scenario}"
            for scenario in ("ordinary_text_before_tool", "ordinary_parallel_tools", "text_between_argument_fragments",
                             "refusal_between_argument_fragments", "reasoning_between_argument_fragments")
        )),
        RegressionGroup("./internal/pkg/xai", (
            "TestOIDCIDTokenCryptographicAndClaimBoundaries",
            "TestOIDCIDTokenOfficialAsymmetricAlgorithms",
            "TestOIDCNonceSurvivesRedisCrossInstanceCallback",
            "TestSSODevicePollingBusinessStates",
            "TestSSODeviceGrantExpiryIncludesConsentStages",
        )),
        RegressionGroup("./internal/pkg/cnoauth", (
            "TestOfficialDeviceProtocols",
            "TestDeepSeekPKCEAndState",
            "TestOAuthErrorsAndIdentitySnapshot",
            "TestKimiCNLoginExactWireContract",
            "TestKimiProductionClientProxyFailuresNeverDialDirect",
            "TestStepFunCloudPollingKeepsModelKeySeparateFromAccountSession",
            "TestStepFunCancelledResponseCannotDeliverGrant",
            "TestStepAccountRefreshFailureDoesNotDestroyStoredSession",
        )),
        RegressionGroup("./internal/pkg/zcode", (
            "TestOffPeakOfficialBareResponseMaintainsAdmissionAndQuota",
            "TestOffPeakOfficialEmptySettleAcknowledgement",
            "TestProjectTokenRequiresPositiveAbsoluteExpiry",
        )),
        RegressionGroup("./internal/pkg/cnmodels", (
            "TestMiniMaxOfficialFlashReleaseCandidate",
        )),
        RegressionGroup("./internal/service", (
            GROK_HTTP_PREFLIGHT,
            GROK_WS_PREFLIGHT,
            GROK_WS_WIRE,
            "TestGrokAuxiliaryHTTPCapturesOwnerBeforeCredentialLookup",
            "TestGrokCompressionRejectsBrokenProxyBeforeCapabilityOrCache",
            "TestGrokAdminProbesRejectBrokenProxyBeforeSend",
            "TestGrokAdminProbesRejectBrokenProxyBeforeRefresh",
            "TestResponsesCapabilityProbeRejectsBrokenProxyBeforeSendOrVerdict",
            "TestResponsesCapabilityProbeCannotPublishAnotherOwnersVerdict",
            "TestCodexManifestRejectsBrokenProxyBeforeCacheOrSend",
            "TestCodexManifestShadowUsesCredentialOwnersCapturedProxy",
            "TestAgentRegistrationRejectsBrokenProxyBeforeAuthPlaneSend",
            "TestOpenAICompatiblePublicModelListsRejectBrokenProxyBeforeCacheOrSend",
            "TestSevenProviderAdminModelListRejectsBrokenProxyBeforeCredentialOrSend",
            MODEL_OBSERVATION_OWNER,
            "TestShadowModelObservationPersistsOnlyCurrentCredentialParent",
            "TestGrokHTTPResponseCannotPublishAnotherOwnersErrorState",
            "TestGrokRuntimeOutcomeCannotBlockOwnerReplacedAfterCommit",
            "TestGrokRequestOutcomeRejectsCallerTransactionBeforeStorageOrRuntime",
            "TestGrokModelRuntimeOutcomeCannotBlockOwnerReplacedAfterCommit",
            "TestGrokTeamBlockKeepsCapturedTeamAfterAccountMoves",
            "TestGrokModelDelayedOldOwnerCannotReplaceNewOwnerCooldown",
            "TestGrokModelCooldownSelectionUsesActualMappedModel",
            "TestGrokOutcomePublicationUsesActualCommitRecoveryGeneration",
            "TestGrokModelRecoveryKeepsNewGenerationFailureAndPrunesExpiredHistory",
            "TestOAuthCredentialOwnerIncludesSavedRecoveryState",
            "TestOAuthCredentialCommitRejectsCallerRecoveryGeneration",
            "TestGrokOutcomePublicationRetainsOwnerWhenRepositoryMutatesCaller",
            "TestGrokAdminRecoveryKeepsNewFailureCommittedBeforeLocalCleanup",
            "TestGrokOutcomeCommittedAfterRecoveryKeepsItsNewGenerationDespiteEarlierEpoch",
            "TestSevenProviderTempUnschedStatusUsesAuthoritativeSavedState",
            "TestOpenAIAPIKeyModelFailureKeepsRecoveryObservationBoundary",
            "TestOpenAIAPIKeyModelSuccessClearsOnlyItsRecoveryGeneration",
            "TestGrokSuccessfulHTTPRequestRecoversOnlyObservedRateLimitOwner",
            "TestGrokDiagnosticObservationsRequireCurrentCredentialProxyAndSchedulerOwner",
            "TestGrokRuntimeOwnerEntriesExpireWithoutReplacingCurrentOwner",
            "TestGrokRuntimeOwnerPublicationAndRollbackAreIndependent",
            GROK_BRIDGE_OWNER,
            GROK_BRIDGE_PREFLIGHT,
            GROK_REQUEST_TOKEN_OWNER,
            GROK_MANUAL_TOKEN_OWNER,
            GROK_TOKEN_PROFILE,
            "TestAccountTestService_Grok429PersistsRateLimitReset",
            "TestAccountTestService_Grok429WithoutQuotaHeadersUsesFallback",
            "TestGetRequestCredentialMapsTransientAndProviderFailuresSeparately",
            "TestCredentialFailureCASMissDoesNotRecoverIneligibleLatestCredential",
            "TestGetRequestCredentialSharedCredentialPersistenceFailureStopsWithoutAccountMutation",
            "TestGrokCredentialIssuerFailureRetainsCapturedProxyRow",
            "TestGrokCredentialFailureCASMissRetainsCapturedGrant",
            OAUTH_RUNTIME_RECOVERY,
            GROK_RESET_PUBLICATION,
            GROK_ADMIN_RESET,
            "TestGrokNonFailoverErrorsDoNotReenterGenericAccountMutation",
            "TestGrokOIDCBrowserLoginRequiresValidatedPersonalIdentity",
            "TestGrokOIDCCanceledVerificationCannotPublishLateCredentials",
            "TestGrokOIDCCallbackRetainsOriginalRoute",
            "TestGrokSSOCanceledConversionCannotPublishLateCredentials",
            "TestOpenAIOAuthAccountOperationsRetainStrictProxySnapshot",
            "TestOpenAIOAuthCallbackRetainsOriginalProxy",
            "TestCNOAuthSelectedProxyRejectsUnavailableOrInvalidRouteBeforeHTTP",
            "TestCNOAuthReloginKeepsHydratedProxySnapshot",
            "TestCNOAuthUnboundLoginPreservesDirectOperation",
            "TestCNOAuthPreviewUsesFrozenLoginRoute",
            "TestCNOAuthRefreshBoundRouteMustValidateBeforeIssuerHTTP",
            "TestCNOAuthLateRefreshAfterProxyEditCannotPersistRotation",
            "TestCNOAuthLateRefreshCannotChangeCapturedProxyExpiry",
            "TestCNOAuthReloginLatePollAfterProxyEditCannotReplaceCredentials",
            "TestCNOAuthNativeControlBoundRouteCannotRereadOrDialDirect",
            "TestCNAPIKeyControlRejectsBrokenProxyBeforeHTTPOrStateWrites",
            "TestCNAPIKeyControlUsesCapturedBoundRouteOrSavedUnbinding",
            APIKEY_OBSERVATION_TEST,
            "TestKimiAPIKeyWalletRejectsUnknownOrInvalidBalance",
            APIKEY_SCHEDULING_TEST,
            APIKEY_CODING_TEST,
            "TestCNAPIKeyCodingQuotaPartialSnapshotCannotRefreshOldWindows",
            APIKEY_BOUNDED_RESPONSE_TEST,
            DEEPSEEK_WALLET_TEST,
            "TestCommandCodeQuotaProbeKeepsWindowsWhenPeriodEndpointsFail",
            "TestZhipuOAuthBuildAccountMaterialForEachPlan",
            "TestQuotaControlRejectsBrokenProxyBeforeCredentialsOrHTTP",
            "TestQuotaControlBrokenBindingStopsNearExpiryRefresh",
            "TestQuotaControlUsesCapturedRouteAndExplicitUnbinding",
            "TestOpenAIQuotaCapturesRouteBeforeTokenLookup",
            "TestOpenAIQuotaCredentialOwnerSnapshotDoesNotAdoptReplacementAgent",
            "TestOpenAIQuotaCredentialOwnerSnapshotKeepsCapturedAgentAssertion",
            "TestOpenAIQuotaCredentialOwnerSnapshotRejectsTaskRecoveryAfterRelink",
            "TestOpenAIQuotaTokenOwnerRejectsRelinkBeforeAdoptionOrRefresh",
            "TestGrokObservedModelsSyncRejectsBrokenProxyBeforeLookupHTTPOrSchemaWrite",
            "TestGrokObservedModelsSyncRejectsLateCredentialAndProxyOwnerChanges",
            "TestGrokObservedModelsScheduleFreezesDeepOwnerBeforeBackgroundWork",
            "TestCNOAuthTerminalSessionDiscardsOldLoginOwner",
            "TestCNOAuthCompletionOwnerSingleUseAndNoTokenEcho",
            "TestCNOAuthAmbiguousWriteCannotReplay",
            "TestCNOAuthCancelAndRelink",
            "TestCreateLiveCallFailoverOwnsOnlySuccessfulAccountMappingAndLease",
            "TestCreateLiveCallMappingFailureReleasesBothKindsOfCapacity",
            "TestCreateLiveCallLeaseFailureOrCancellationReleasesOrdinarySlot",
            "TestCreateLiveCallCancellationDuringSendReleasesAllAcquiredCapacity",
            "TestAlphaSearchPATMetadataRejectsBrokenProxyBeforeValidation",
            "TestLiveSidebandRejectsBrokenProxyBeforeDial",
            LIVE_PROXY_TEST,
            "TestStepFunOAuthAndAPIKeyActualForwardAndProbe",
            "TestStepFunOAuthCatalogBeforeCreation",
            "TestStepFunOAuthCatalogRejectsInvalidSessionBeforeUpstream",
            "TestStepFunOAuthCatalogFailureKeepsGrantUsable",
            "TestStepFunOAuthCatalogDiscardsResponseAfterCancellation",
            "TestStepFunModelRestrictionControlsActualAccountSelection",
            "TestStepFunStreamingToolsAndBillingAcrossInboundProtocols",
            "TestStepFunLoginAcrossReplicasConsumesGrantOnce",
            "TestDeepSeekNativeModelListingUsesSelectedIdentityVersion",
            "TestDeepSeekNativePublicModelCacheKeepsSelectedVersionGate",
            "TestDeepSeekNativeMetadataCannotPublishVersionRejectedModel",
            "TestDeepSeekNativeModelListingRejectsUnreadableOrUnusableRows",
            "TestZhipuOffPeakRejectsBrokenBoundProxyBeforeCredentialOrTicketSend",
            "TestZhipuOffPeakAcquireRejectsReplacementProxyArgument",
            "TestZhipuOffPeakProxySettlementFailureRecoveryRetiresExactlyOnce",
            "TestZhipuOffPeakSweepRetriesFailedSettles",
            "TestDomesticProviderNativeMessagesPreserveOfficialMediaAndToolChanges",
            "TestDomesticOutboundIdentityWireSnapshotAndFailover",
            "TestNativeOAuthUsageRejectsLateOwnerResponse",
            "TestNativeOAuthWalletRejectsLateOwnerResponse",
            "TestNativeOAuthRefreshRejectsLateIssuerResponseAfterRelink",
            "TestNativeModelCatalogSyncRejectsResponseAfterRelink",
            NATIVE_REFRESH_COORDINATOR,
            NATIVE_REFRESH_SUCCESS,
            NATIVE_REFRESH_FAILURE,
            UNSAVED_CREDENTIAL_OWNER,
            PROJECT_TOKEN_ACCOUNT_ISOLATION,
            ZHIPU_QUOTA_401_RECOVERY,
            ZHIPU_QUOTA_FINAL_401,
            ZHIPU_QUOTA_403,
            "TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_APIKeyUsesResponsesInputTokens",
            "TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_CodexProfileGateRunsBeforeCredentialLookup",
            "TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_OAuthFallsBackWhenPlatformEndpointUnsupported",
            "TestOpenAIGatewayService_OpenAIOAuthInputTokensFallbackUsesMinimumWhenEstimateFails",
            "TestForwardResponsesInputTokensCustomRelayUsesLocalEstimate",
            "TestForwardResponsesInputTokensGrokOAuthUsesLocalEstimate",
            "TestForwardResponsesInputTokensUpstream404FallsBackLocally",
            "TestEstimateOpenAIInputTokens_RequestSamples",
            COUNT_TOKENS_PREFLIGHT,
            COUNT_TOKENS_OWNER,
            COUNT_TOKENS_LOCAL,
            "TestRunLiveControllerClosesExpiredSession",
            "TestFinalizeLiveCallIsIdempotentAndWritesZeroUsage",
            "TestLiveSessionEndedTreatsLeaseLossAsTerminal",
            "TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize",
            "TestFinalizeLiveCallUsageLogFallsBackToSyncCreate",
            "TestForwardGrokResponses_PropagatesSearchCountFromJSON",
            "TestForwardGrokResponses_PropagatesSearchCountFromSSE",
            "TestIsGrokVideoStatusBillable",
            "TestExtractGrokVideoBillingFromStatusBodyPrefersUpstreamParams",
            "TestExtractGrokVideoBillingFromStatusBodyFallsBackToPending",
            "TestExtractGrokVideoBillingRejectsNonDoneStatus",
            "TestGrokMediaUsageFromResponseVideoCreateDoesNotBill",
            "TestGrokMediaUsageFromResponseVideoStatusBillsOnOfficialDone",
            ZHIPU_START_PLAN_TEST,
        ) + CAPABILITY_SERVICE_TESTS, ("TestZhipuOAuthBuildAccountMaterialForEachPlan/start_plan_and_off-peak_are_estate_independent",)
        + APIKEY_OBSERVATION_SUBTESTS + APIKEY_CODING_SUBTESTS + tuple(
            f"{NATIVE_REFRESH_COORDINATOR}/{provider}"
            for provider in ("deepseek", "kimi", "minimax", "stepfun")
        ) + tuple(
            f"{NATIVE_REFRESH_SUCCESS}/{provider}/{change}"
            for provider in ("kimi", "minimax", "stepfun")
            for change in ("unchanged", "relinked", "nested-metadata", "proxy-changed",
                           "new-cooldown", "store-error", "new-runtime")
        ) + tuple(
            f"{NATIVE_REFRESH_FAILURE}/{provider}/{failure}"
            for provider in ("kimi", "minimax", "stepfun")
            for failure in ("invalid-grant", "provider-unavailable", "late-old-login", "missing-contract")
        ) + tuple(
            f"{test}/{plan}"
            for test in (ZHIPU_QUOTA_401_RECOVERY, ZHIPU_QUOTA_FINAL_401, ZHIPU_QUOTA_403)
            for plan in ("individual-coding-plan", "off-peak")
        ) + tuple(
            f"TestKimiAPIKeyWalletRejectsUnknownOrInvalidBalance/{scenario}"
            for scenario in ("missing", "missing-data", "missing-balance", "null", "text", "boolean",
                             "nan", "infinity", "overflow", "business-error", "truncated",
                             "missing-status", "false-status", "valid-zero", "valid-numeric-string")
        ) + tuple(
            f"{APIKEY_SCHEDULING_TEST}/{provider}/{balance}/{change}"
            for provider in ("deepseek", "kimi")
            for balance in ("low", "healthy")
            for change in ("api-key", "other-cooldown", "manual-pause", "same-owner")
        ) + tuple(
            f"TestCNAPIKeyCodingQuotaPartialSnapshotCannotRefreshOldWindows/{provider}"
            for provider in ("kimi", "zhipu", "minimax")
        ) + tuple(
            f"{APIKEY_BOUNDED_RESPONSE_TEST}/{provider}/{mode}/{scenario}"
            for provider, mode in (("deepseek", "payg"), ("kimi", "payg"), ("kimi", "coding"),
                                   ("zhipu", "coding"), ("minimax", "coding"))
            for scenario in ("complete", "exact-limit", "one-byte-over-limit", "read-failure")
        ) + tuple(
            f"{DEEPSEEK_WALLET_TEST}/{scenario}/{state}"
            for scenario in ("truncated-json", "text-availability", "numeric-availability", "null-availability",
                             "mixed-invalid-currency-wallet", "mixed-missing-balance", "mixed-non-object",
                             "mixed-infinite-balance", "missing-currency", "wrong-type-currency",
                             "unsupported-currency", "valid-healthy-dual-wallet", "valid-unavailable-zero")
            for state in ("active", "recovering")
        ) + tuple(
            f"{LIVE_PROXY_TEST}/{mode}"
            for mode in ("success", "refused", "timeout", "authentication-required", "cancelled", "edited-after-connect")
        ) + tuple(
            f"{ZHIPU_START_PLAN_TEST}/{provider}/{scenario}"
            for provider in ("bigmodel", "zai")
            for scenario in ("fresh", "legacy-scheme")
        ) + tuple(
            f"{GROK_HTTP_PREFLIGHT}/{kind}/{operation}/{binding}"
            for kind in ("apikey", "oauth") for operation in GROK_HTTP_ACTIONS for binding in STRICT_BINDINGS
        ) + tuple(
            f"{GROK_WS_PREFLIGHT}/{kind}/{operation}/{binding}"
            for kind in ("apikey", "oauth") for operation in ("open", "probe") for binding in STRICT_BINDINGS
        ) + tuple(
            f"{GROK_WS_WIRE}/{kind}/{operation}/{mode}"
            for kind in ("apikey", "oauth") for operation in ("open", "probe") for mode in GROK_PROXY_MODES
        ) + tuple(
            f"TestGrokAuxiliaryHTTPCapturesOwnerBeforeCredentialLookup/{operation}"
            for operation in ("voice", "image", "video-content")
        ) + tuple(
            f"TestGrokCompressionRejectsBrokenProxyBeforeCapabilityOrCache/{binding}" for binding in STRICT_BINDINGS
        ) + tuple(
            f"TestGrokAdminProbesRejectBrokenProxyBeforeSend/{kind}/{operation}/{binding}"
            for kind in ("apikey", "oauth") for operation in GROK_ADMIN_MODES for binding in STRICT_BINDINGS
        ) + tuple(
            f"TestGrokAdminProbesRejectBrokenProxyBeforeRefresh/{operation}/{binding}"
            for operation in GROK_ADMIN_MODES for binding in STRICT_BINDINGS[2:]
        ) + tuple(
            f"{test}/{binding}"
            for test in ("TestResponsesCapabilityProbeRejectsBrokenProxyBeforeSendOrVerdict",
                         "TestAgentRegistrationRejectsBrokenProxyBeforeAuthPlaneSend")
            for binding in STRICT_BINDINGS
        ) + tuple(
            f"TestCodexManifestRejectsBrokenProxyBeforeCacheOrSend/{kind}/{binding}"
            for kind in ("apikey", "oauth") for binding in STRICT_BINDINGS
        ) + tuple(
            f"TestResponsesCapabilityProbeCannotPublishAnotherOwnersVerdict/{change}"
            for change in ("unchanged", "relinked-key", "same-proxy-edited", "unbound", "storage-failure")
        ) + tuple(
            f"TestOpenAICompatiblePublicModelListsRejectBrokenProxyBeforeCacheOrSend/{provider}/{binding}"
            for provider in ("openai", "deepseek", "kimi", "minimax", "stepfun", "zhipu") for binding in STRICT_BINDINGS
        ) + tuple(
            f"TestSevenProviderAdminModelListRejectsBrokenProxyBeforeCredentialOrSend/{provider}/{binding}"
            for provider in PROVIDERS for binding in STRICT_BINDINGS
        ) + tuple(
            f"{MODEL_OBSERVATION_OWNER}/{provider}/{change}"
            for provider in PROVIDERS for change in ("unchanged", "key-replaced", "proxy-edited", "unbound", "storage-failure")
        ) + tuple(
            f"TestShadowModelObservationPersistsOnlyCurrentCredentialParent/{change}"
            for change in ("unchanged", "parent-relinked", "parent-proxy-edited", "storage-failure")
        ) + tuple(
            f"TestGrokHTTPResponseCannotPublishAnotherOwnersErrorState/{path}/{status}/{change}"
            for path in ("voice", "media", "responses", "chat")
            for status in ("Unauthorized", "Too_Many_Requests", "Bad_Gateway")
            for change in ("unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure")
        ) + tuple(
            f"TestGrokRuntimeOutcomeCannotBlockOwnerReplacedAfterCommit/{operation}/{change}"
            for operation in ("temporary-cooldown", "rate-limit")
            for change in ("unchanged", "relinked", "proxy-edited")
        ) + tuple(
            f"TestGrokRuntimeOwnerEntriesExpireWithoutReplacingCurrentOwner/{operation}"
            for operation in ("current-owner-read", "current-owner-publish", "expired-owner-peek", "clear-all-owner-generations")
        ) + tuple(
            f"TestGrokSuccessfulHTTPRequestRecoversOnlyObservedRateLimitOwner/{path}/{change}"
            for path in ("voice", "media", "responses", "chat")
            for change in ("unchanged", "relinked", "proxy-edited", "newer-rate-limit", "storage-failure")
        ) + tuple(
            f"TestGrokDiagnosticObservationsRequireCurrentCredentialProxyAndSchedulerOwner/{path}/{status}/{change}"
            for path in ("active-quota", "billing", "admin-voice")
            for status in ("OK", "Forbidden")
            for change in ("unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure")
        ) + tuple(
            f"TestGrokModelRuntimeOutcomeCannotBlockOwnerReplacedAfterCommit/{operation}/{change}"
            for operation in ("model-free-usage", "transient-capacity")
            for change in ("unchanged", "relinked", "proxy-edited")
        ) + tuple(
            f"TestGrokModelDelayedOldOwnerCannotReplaceNewOwnerCooldown/{operation}"
            for operation in ("free_usage", "transient_capacity")
        ) + tuple(
            f"TestGrokModelCooldownSelectionUsesActualMappedModel/{scenario}"
            for scenario in ("mapped-to-failing-model", "mapped-to-healthy-sibling")
        ) + tuple(
            f"TestGrokOutcomePublicationUsesActualCommitRecoveryGeneration/{operation}/{order}"
            for operation in ("account-cooldown", "model-capacity")
            for order in ("unchanged", "reset-before-commit", "remote-reset-after-commit", "storage-failure")
        ) + tuple(
            f"TestOpenAIAPIKeyModelFailureKeepsRecoveryObservationBoundary/{order}"
            for order in ("unchanged", "reset-before-read", "reset-after-read", "relink-before-read", "proxy-edit-before-read", "read-failure")
        ) + tuple(
            f"TestOpenAIAPIKeyModelSuccessClearsOnlyItsRecoveryGeneration/{scenario}"
            for scenario in ("current-success", "historical-success")
        ) + tuple(
            f"TestSevenProviderTempUnschedStatusUsesAuthoritativeSavedState/{provider}/{scenario}"
            for provider in PROVIDERS
            for scenario in ("recovered_account", "newer_plain_reason", "newer_structured_reason",
                             "matching_saved_state", "deleted_account", "account_read_failure", "cache_unavailable")
        ) + tuple(
            f"TestGrokNonFailoverErrorsDoNotReenterGenericAccountMutation/{path}/{kind}/{change}"
            for path in ("responses", "chat") for kind in ("model-capacity", "content-policy")
            for change in ("unchanged", "relinked", "proxy-edited")
        ) + tuple(
            f"{GROK_BRIDGE_OWNER}/{status}/{change}"
            for status in ("Unauthorized", "Too_Many_Requests", "Bad_Gateway")
            for change in ("unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure")
        ) + tuple(
            f"{GROK_BRIDGE_PREFLIGHT}/{binding}" for binding in STRICT_BINDINGS
        ) + tuple(
            f"{GROK_REQUEST_TOKEN_OWNER}/{phase}/{change}"
            for phase in ("cache-hit", "version-adoption", "refresh-reread", "refresh-response", "refresh-persisted", "lock-wait")
            for change in ("unchanged", "relinked", "proxy-edited", "identity-edited", "storage-failure")
        ) + tuple(
            f"{GROK_MANUAL_TOKEN_OWNER}/{change}"
            for change in ("unchanged", "relinked", "proxy-edited", "identity-edited", "storage-failure")
        ) + tuple(
            f"{GROK_TOKEN_PROFILE}/{mode}/{change}"
            for mode in ("request", "manual")
            for change in ("profile-metadata", "changed-subject", "changed-team", "changed-client", "changed-identity", "changed-base")
        ) + tuple(
            f"TestGetRequestCredentialMapsTransientAndProviderFailuresSeparately/{scenario}"
            for scenario in (
                "selected_owner_missing_refresh_credential_permanently_blocks_the_account",
                "refresh_added_after_selected_owner_structural_failure_wins_conditional_mutation",
                "refresh_removed_after_selection_rejects_replacement_owner_without_quarantine",
            )
        ) + tuple(
            f"TestCredentialFailureCASMissDoesNotRecoverIneligibleLatestCredential/{failure}/independently_runtime_blocked"
            for failure in ("permanent", "transient")
        ) + tuple(
            f"{test}/{failure}/{change}"
            for test, changes in (
                ("TestGrokCredentialIssuerFailureRetainsCapturedProxyRow", (
                    "unchanged", "display-metadata", "protocol", "host", "port",
                    "username", "password", "status", "expiry",
                )),
                ("TestGrokCredentialFailureCASMissRetainsCapturedGrant", (
                    "same-grant-access-rotation", "replacement-grant", "subject", "team", "client",
                    "identity", "base", "proxy-auth", "proxy-lifecycle",
                )),
            )
            for failure in ("permanent", "transient") for change in changes
        ) + tuple(
            f"{OAUTH_RUNTIME_RECOVERY}/{provider}/{change}"
            for provider in ("openai", "grok")
            for change in ("unchanged", "same-owner-new-failure", "relinked", "proxy-edited", "no-prior-block", "storage-failure", "owner-cas-miss")
        ) + tuple(
            f"{GROK_RESET_PUBLICATION}/{operation}/{boundary}"
            for operation in ("account-cooldown", "model-free-usage", "model-capacity")
            for boundary in ("unchanged", "reset-after-commit")
        ) + tuple(
            f"{GROK_ADMIN_RESET}/{operation}" for operation in ("free_usage", "transient_capacity")
        ) + tuple(
            f"{test}/{scenario}"
            for test, scenarios in (
                ("TestStepFunOAuthCatalogBeforeCreation", ("cn", "global")),
                ("TestStepFunOAuthCatalogRejectsInvalidSessionBeforeUpstream", (
                    "owner", "platform", "pending", "cancelled", "expired", "grant-expired", "committing",
                )),
                ("TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_OAuthFallsBackWhenPlatformEndpointUnsupported", (
                    "401_missing_responses_write_scope", "403_missing_responses_write_scope",
                    "403_html_proxy_page", "404_input_tokens_unsupported",
                )),
                ("TestEstimateOpenAIInputTokens_RequestSamples", (
                    "simple_text_input", "instructions_plus_tool_schema", "input_parts_and_tool_output",
                )),
                ("TestLiveSessionEndedTreatsLeaseLossAsTerminal", (
                    "租约丢失", "租约丢失（被包装）", "上游报告会话已关闭", "到达会话时长上限",
                    "控制权被他人接管", "临时读错误", "无错误",
                )),
                ("TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize", (
                    "GetLiveCall_持续报错", "ClaimLiveController_报错",
                )),
            )
            for scenario in scenarios
        ) + tuple(
            f"TestStepFunModelRestrictionControlsActualAccountSelection/{kind}/{model}"
            for kind in ("oauth", "apikey") for model in ("step-3.7-flash", "public-step")
        ) + tuple(
            f"{COUNT_TOKENS_PREFLIGHT}/{entry}/{binding}"
            for entry in ("responses-native", "messages-count") for binding in STRICT_BINDINGS
        ) + tuple(
            f"{COUNT_TOKENS_OWNER}/{entry}/{change}"
            for entry in ("responses-native", "messages-count")
            for change in ("proxy-route", "credential-relink", "identity", "saved-unbinding", "saved-binding")
        ) + tuple(
            f"{COUNT_TOKENS_LOCAL}/{entry}/{provider}"
            for entry, providers in (
                ("responses-native", ("grok", "custom-relay", "deepseek", "kimi", "minimax", "stepfun", "zhipu")),
                ("messages-count", ("deepseek", "kimi", "minimax", "stepfun", "zhipu")),
            )
            for provider in providers
        ) + CAPABILITY_SERVICE_SUBTESTS),
        RegressionGroup("./internal/repository", (
            GROK_HTTP_WIRE,
            COUNT_TOKENS_WIRE,
            "TestOpenAICodeAndDeviceProxyStagesNeverDialDirect",
            "TestGrokOIDCProductionBrowserProxyFailuresNeverDialDirect",
            "TestGrokSSOProductionClientProxyFailuresNeverDialDirect",
            "TestNativeMessagesAPIKeyProductionProxyContract",
            "TestNativeMessagesAPIKeyOfficialDefaults",
            APIKEY_REDIRECT_TEST,
            CHAT_PROXY_TEST,
            "TestZhipuOffPeakProductionClientProxyFailuresNeverDialDirect",
            "TestZhipuOffPeakProductionAcquisitionRejectsBrokenProxyBindings",
            "TestProviderFilesProductionProxyContract",
            "TestStepFunProductionProxyFailureAcrossProfilesAndBridges",
            "TestZhipuStartPlanProductionBalanceStrictProxy",
            "TestZhipuStartPlanSchedulerProjectionKeepsPermissionWithoutJWT",
        ), tuple(
            f"{CHAT_PROXY_TEST}/{platform}/{binding}"
            for platform in PROVIDERS
            for binding in ("valid-bound", "unbound-direct", "missing-bound-route",
                            "mismatched-proxy-owner", "malformed-proxy-host", "incomplete-proxy-auth")
        ) + tuple(
            f"{APIKEY_REDIRECT_TEST}/{provider}/{mode}/{status}/{destination}"
            for provider, mode in (("deepseek", "payg"), ("kimi", "payg"), ("kimi", "coding"),
                                   ("zhipu", "coding"), ("minimax", "coding"))
            for status in (200, 301, 302, 307, 308)
            for destination in ("same-origin", "different-origin")
        ) + tuple(
            f"{GROK_HTTP_WIRE}/{kind}/{operation}/{mode}"
            for kind in ("apikey", "oauth") for operation in ("voice", "image", "video", "status", "content") for mode in GROK_PROXY_MODES
        ) + tuple(
            f"{COUNT_TOKENS_WIRE}/{entry}/{mode}"
            for entry in ("responses-native", "messages-count")
            for mode in ("success", "unbound-direct", "refused", "timeout", "authentication-required", "missing-bound-route")
        ) + tuple(
            f"TestProviderFilesProductionProxyContract/{profile}/{operation}/{mode}"
            for profile, operations in (("deepseek-apikey", ("upload", "delete")),
                                        ("deepseek-oauth", ("upload", "delete")), ("minimax-apikey", ("upload",)))
            for operation in operations
            for mode in ("success", "unbound-direct", "refused", "timeout", "407", "cancelled")
        ) + tuple(
            f"TestStepFunProductionProxyFailureAcrossProfilesAndBridges/{profile}/{protocol}/{mode}"
            for profile in STEP_PROFILES for protocol in STEP_PROTOCOLS for mode in ("timeout", "refused", "407")
        ) + tuple(
            f"TestZhipuStartPlanProductionBalanceStrictProxy/{mode}"
            for mode in ("success", "timeout", "refused", "authentication_required", "missing", "mismatched")
        )),
        RegressionGroup("./internal/handler", (
            "TestGrokRealtimeHandlerRejectsBrokenProxyBeforeCredential",
            "TestGrokRealtimeHandlerRetainsRouteAcrossCredentialLookup",
            "TestGrokRealtimeHandlerLateRejectionCannotCoolReplacementOwner",
            FAILOVER_TEST,
            "TestDomesticProviderMediaAndToolChangesRetainCurrentUserAudit",
            "TestDomesticProviderOpaquePayloadsPassBlockingAudit",
            "TestDomesticProviderMessagesAuditBlocksBeforeSelectionAndConcurrency",
            "TestGrokRealtimeTurnTrackerDeduplicatesPendingResponseAndAuditsAdmin",
            "TestGrokRealtimeBillingResultRequiresObservedAudio",
            "TestGrokRealtimeBillingResultUsesForcedUniqueID",
            "TestProviderFilesMultipartCleanupAndValidation",
            "TestProviderFilesAuditPrecedesACLRead",
            "TestProviderFilesInferenceUnknownLocalAndUnsupportedBridges",
            "TestProviderFilesUploadAuditsBeforeSelection",
            "TestProviderFilesCancelledUploadDoesNotRetainBytes",
            "TestZhipuStartPlanUnknownPublicCatalogNeverInventsDefaultPermission",
            "TestZhipuStartPlanCompositeRouteAliasRequiresProvenTargetEntitlement",
            "TestZhipuStartPlanCompositeRouteFilteringKeepsOtherPlatforms",
            "TestZhipuStartPlanCompositeCatalogUsesWinningRouteAndActualTarget",
            "TestZhipuStartPlanCompositeCatalogPreservesTypedAndPersistedIdentityOwner",
            "TestProviderFilesAuditGatePrecedesEveryACLAndWriteBoundary",
        ) + REVIEW_HANDLER_TESTS, tuple(
            f"{FAILOVER_TEST}/{platform}/{status}"
            for platform in PROVIDERS
            for status in (503, 429)
        ) + tuple(
            f"TestGrokRealtimeHandlerRejectsBrokenProxyBeforeCredential/{binding}"
            for binding in ("missing", "mismatched", "malformed", "incomplete-auth")
        ) + tuple(
            f"TestGrokRealtimeHandlerLateRejectionCannotCoolReplacementOwner/{change}"
            for change in ("unchanged", "relinked", "same-row-proxy-edited", "manual-pause", "other-cooldown", "proxy-authentication-required")
        ) + tuple(
            f"TestProviderFilesMultipartCleanupAndValidation/{scenario}"
            for scenario in ("image_and_expiry", "no_expiry_means_permanent", "partial_expiry", "unknown_field", "fake_image")
        ) + tuple(
            f"TestProviderFilesAuditPrecedesACLRead/{operation}" for operation in ("list", "metadata", "delete")
        ) + tuple(
            f"TestZhipuStartPlanUnknownPublicCatalogNeverInventsDefaultPermission/{platform}/{endpoint}{allowlist}"
            for platform in ("zhipu", "composite") for endpoint in ("models", "codex") for allowlist in ("", "/allowlist")
        ) + tuple(
            f"TestZhipuStartPlanCompositeRouteAliasRequiresProvenTargetEntitlement/{scenario}/{endpoint}{allowlist}"
            for scenario in ("no-observation", "models-unknown", "unavailable", "pending", "expired", "stale",
                             "different-model-authorized", "fresh-target-authorized")
            for endpoint in ("models", "codex") for allowlist in ("", "/allowlist")
        ) + tuple(
            f"TestZhipuStartPlanCompositeRouteFilteringKeepsOtherPlatforms/{endpoint}" for endpoint in ("models", "codex")
        ) + tuple(
            f"TestZhipuStartPlanCompositeCatalogPreservesTypedAndPersistedIdentityOwner/{representation}"
            for representation in ("typed-selection", "persisted-json")
        ) + tuple(
            f"TestZhipuStartPlanCompositeCatalogUsesWinningRouteAndActualTarget/{scenario}/{endpoint}"
            for scenario in ("route-overrides-account-published-model", "lower-priority-proof-cannot-authorize-winner",
                             "winning-priority-has-proof", "responses-specific-unproven-target",
                             "responses-specific-proven-target", "account-mapping-resolves-actual-model",
                             "empty-upstream-passes-alias-through-account-mapping", "account-mapping-cannot-be-bypassed-by-raw-target")
            for endpoint in ("models", "codex")
        ) + tuple(
            f"TestProviderFilesAuditGatePrecedesEveryACLAndWriteBoundary/{boundary}"
            for boundary in ("upload-retry", "scheduler", "user-slot", "account-slot", "upload", "metadata", "list", "delete")
        ) + REVIEW_HANDLER_SUBTESTS),
        RegressionGroup("./internal/server/middleware", (
            "TestProviderFilesReviewAllowlistLeavesUploadBodyUnread",
            "TestProviderFilesReviewUploadExemptionPreservesModelAllowlist",
        ), tuple(
            f"TestProviderFilesReviewAllowlistLeavesUploadBodyUnread/{path}"
            for path in ("/v1/files", "/v1/files/upload")
        ) + tuple(
            f"TestProviderFilesReviewUploadExemptionPreservesModelAllowlist/{scenario}"
            for scenario in ("chat-allowed", "chat-denied", "live-session-allowed", "live-session-denied")
        )),
        RegressionGroup("./internal/handler/admin", (NATIVE_UNSUPPORTED_REFRESH,
            "TestZhipuOffPeakSegmentHandlerOpaqueReadAndExactTermination",
            "TestZhipuOffPeakSegmentHandlerValidatesBeforeControlSideEffects",
            "TestZhipuOffPeakSegmentHandlerReportsDisabledMissingAndStoreFailure",
            "TestZhipuOffPeakSegmentHandlerDuplicateOldHandleCannotTerminateNewSegment",
        ), tuple(
            f"{NATIVE_UNSUPPORTED_REFRESH}/{provider}/{marker}"
            for provider in ("deepseek", "zhipu") for marker in ("native-marker", "missing-native-marker")
        ) + tuple(
            f"TestZhipuOffPeakSegmentHandlerValidatesBeforeControlSideEffects/{profile}"
            for profile in ("zhipu/oauth/start-plan", "zhipu/oauth/individual-coding-plan",
                            "stepfun/oauth/off-peak", "zhipu/upstream/off-peak")
        ) + tuple(
            f"TestZhipuOffPeakSegmentHandlerReportsDisabledMissingAndStoreFailure/{scenario}"
            for scenario in ("disabled-persistence", "manager-unavailable", "missing-segment", "stale-generation", "database-unavailable")
        )),
        RegressionGroup("./internal/server/routes", ("TestZhipuOffPeakSegmentRoutesRequireRealAdmin",)),
        RegressionGroup("./internal/handler/dto", (OAUTH_OWNER_DTO,), tuple(
            f"{OAUTH_OWNER_DTO}/{provider}" for provider in PROVIDERS
        )),
    ),
    "integration": (
        RegressionGroup("./internal/service", ("TestLocalSignedPluginRuntimeIntegration",), tuple(
            f"TestLocalSignedPluginRuntimeIntegration/{scenario}"
            for scenario in (
                "request-response-config-and-passive-health",
                "preflight-refusal-sends-no-http-request",
                "delivered-transport-failure-forbids-account-replay",
                "upload-read-failure-closes-body-without-http-replay",
                "cancellation-reaches-child-http-and-releases-lease",
            )
        )),
        RegressionGroup("./internal/repository", (
            "TestGrokRequestOutcomeCASPublishesAtomicOwnerState",
            "TestGrokRequestOutcomeCASRejectsOwnerOrSchedulerChanges",
            "TestGrokRequestOutcomeCASRejectsSameProxyRouteEdits",
            "TestGrokRequestOutcomeCASWaitsForConcurrentOwnerOrProxyEdit",
            "TestGrokRequestOutcomeCASOutboxFailureRollsBackAllState",
            "TestGrokRequestOutcomeCASCallerRollbackDoesNotPublishState",
            "TestGrokRequestOutcomeCASDoesNotShortenCurrentCooldownOrRateLimit",
            "TestGrokRequestOutcomeCASAllowsOwnerClaimWithoutPersistentError",
            "TestGrokRequestOutcomeCASRateLimitRecoveryKeepsObservedOwner",
            "TestGrokRequestOutcomeCASModelCooldownMergesConcurrentSibling",
            "TestGrokCredentialFailureCASPublishesOnlyCapturedRoute",
            "TestGrokCredentialFailureCASRejectsSameRowProxyChanges",
            "TestGrokCredentialFailureCASWaitsForConcurrentOwnerOrProxyEdit",
            "TestGrokCredentialFailureCASOutboxFailureRollsBackState",
            "TestGrokCredentialFailureCASCallerTransactionDoesNotPublishBeforeCommit",
            "TestGrokCredentialFailureCASMissingProxyQuarantineRejectsRestoration",
            "TestGrokCredentialFailureCASWaitsForConcurrentMissingProxyRestoration",
            "TestCNProxyOwnerCASRejectsSameRowRouteEditsWithoutSideEffects",
            "TestCNProxyOwnerCASWaitsForConcurrentProxyEditAndRechecks",
            "TestCNOAuthLoginCASRecoveryCommitsWithCredentialsAndOutbox",
            "TestCNOAuthLoginCASStorageFaultLeavesOriginalLoginAndRecoveryState",
            "TestCNOAuthLoginCASPreservesNewerRuntimeFailures",
            "TestStepCloudLoginCASStorageFailureKeepsOldSession",
            "TestCNOAuthRefreshCASPublishesOnlyCommittedReplacementCredentials",
            "TestCNOAuthRefreshCASConcurrentReauthorizationPreservesNewLogin",
            "TestCNOAuthRefreshCASOutboxFailureRollsBackCredentials",
            "TestCNControlSnapshotCASMergesCurrentExtraAndInvalidatesCommittedSnapshot",
            "TestCNControlSnapshotCASRejectsChangedOwnerWithoutSideEffects",
            "TestCNControlSnapshotCASConcurrentRelinkWinsBeforeDelayedProbeWrite",
            "TestCNControlSnapshotCASOutboxFailureRollsBackObservation",
            "TestCNControlSnapshotCASCallerTransactionRollbackDoesNotPublishSnapshot",
            CONTROL_CAS_CACHE_BOUNDARY,
            "TestCNBalanceScheduleCASKeepsOwnerAndPublishesCurrentSnapshot",
            "TestCNBalanceScheduleCASRejectsConcurrentOwnerOrSchedulerChange",
            "TestCNBalanceScheduleCASOutboxFailureRollsBackCooldown",
            "TestCNBalanceScheduleCASCallerRollbackDoesNotPublishCooldown",
            "TestQuotaFollowReset_ConcurrentObservationBillingAndWorkers",
            APIKEY_POSTGRES_OBSERVATION_TEST,
            APIKEY_POSTGRES_SCHEDULING_TEST,
            "TestOpenAIQuotaOwnerRefreshFinalWriteCannotOverwriteExternalRelink",
            "TestOpenAIQuotaOwnerTaskFinalWriteCannotOverwriteExternalRelink",
            "TestOpenAIQuotaOwnerTaskCASMissReusesConcurrentSameOwnerTask",
            "TestOpenAIQuotaOwnerPersistenceFailureStopsQuotaAndCache",
            "TestOpenAIQuotaOwnerCASOutboxFailureAndCallerRollbackPublishNothing",
            "TestOpenAIQuotaOwnerCASRechecksConcurrentAccountAndProxyWriters",
            ADMIN_RECOVERY,
            "TestAdminShadowDeletionCommitsWholeCredentialFamilyOrNothing",
            "TestAdminShadowProxyRestorationCommitsFamilyBindingOrNothing",
            "TestAdminRecoveryCallerTransactionPublishesRuntimeOnlyAfterCommit",
            "TestAdminRecoveryPreservesManualPauseAndExpirationConfiguration",
            "TestShadowCreationRejectsDeletedCredentialParentWithoutSideEffects",
            "TestAdminShadowDeletionPreventsCreationAfterFamilyEnumeration",
            "TestAdminShadowDeletionRechecksFamilyAfterInFlightCreation",
            ADMIN_APPLY,
            ADMIN_REFRESH,
            ADMIN_APPLY_PREHANDLER,
            "TestOAuthCredentialOwnerCASMergesCurrentMetadataAndKeepsRecoveryBoundary",
            "TestOAuthCredentialOwnerCASPreservesNewerModelAndReauthorizationState",
            "TestOAuthCredentialOwnerCASWaitsForConcurrentAccountOrProxyWriter",
            "TestOAuthCredentialOwnerCASOutboxFaultAndCallerRollbackRetainPriorOwner",
            "TestAccountRepoSuite",
            "TestOAuthParentTypeConversionSerializesWithShadowCreation",
            "TestOAuthParentTypeConversionRechecksShadowCreationBeforeCommit",
            "TestOAuthCredentialTypeChangeRejectsShadowCreatedAfterPreflight",
            "TestAdminShadowCreationRechecksParentTypeAtPersistence",
            "TestOAuthCredentialTypeChangeRechecksShadowCommittedWhileWaiting",
            "TestRuntimeRecoveryGenerationResetCommitsOrRollsBackWithAccount",
            "TestRuntimeRecoveryGenerationResetPreservesUnrelatedProviderState",
            "TestRuntimeRecoveryGenerationOAuthRecoveryAndRefreshKeepDifferentContracts",
            "TestRuntimeRecoveryGenerationIsPreservedAcrossOrdinaryMetadataWrites",
            "TestRuntimeRecoveryGenerationCannotBeImportedOnAccountCreation",
            "TestGrokRequestOutcomeReturnsGenerationOfItsAtomicCommit",
            "TestSevenProviderPublicSchedulingRecoveryIsAtomic",
            "TestSuccessfulTestRecoveryCommitsOnlyRequiredState",
        ) + CAPABILITY_REPOSITORY_TESTS, tuple(
            f"{test}/{scenario}"
            for test, scenarios in (
                ("TestZhipuOffPeakStoreTerminateRejectsUnmatchedOwnerAndGeneration", ("other-account", "unknown-record", "wrong-generation")),
                ("TestZhipuOffPeakManagerPostgresTerminateCannotRetakeFromOldAcquire", ("waiting-operation-lease", "already-claimed-operation")),
                ("TestZhipuOffPeakManagerPostgresAdmissionFailureRecoversSameTicketAfterRestart", ("request-lease-insert-failure", "operation-lease-expired")),
                ("TestZhipuOffPeakManagerPostgresUnknownTakeStateRetainsKnownTicketForRestart", ("unknown-state", "missing-state", "empty-state")),
                ("TestProviderFilesRepositoryOwnerCASLocksProxyBeforeAccount", ("reserve", "publish")),
                ("TestZhipuStartPlanPostgresOwnerAndOutboxFailuresPreserveObservation", ("relink", "estate", "plan", "outbox")),
                (CONTROL_CAS_CACHE_BOUNDARY, ("managed=false", "managed=true")),
                ("TestGrokRequestOutcomeCASPublishesAtomicOwnerState", ("apikey", "oauth")),
                ("TestGrokRequestOutcomeCASRejectsOwnerOrSchedulerChanges", (
                    "credential_replacement", "platform_replacement", "account_type_replacement",
                    "manual_pause", "disabled_account", "other_cooldown", "newer_rate_limit",
                    "newer_overload", "account_expiry", "expiry_policy", "deleted_account",
                )),
                ("TestGrokRequestOutcomeCASRejectsSameProxyRouteEdits", (
                    "host='replacement.example'", "port=9090", "username='replacement-user'",
                    "password='replacement-password'", "protocol='socks5'", "status='inactive'",
                    "expires_at=NOW()", "deleted_at=NOW()", "binding_removed",
                )),
                ("TestGrokRequestOutcomeCASWaitsForConcurrentOwnerOrProxyEdit", ("account", "proxy")),
                ("TestGrokRequestOutcomeCASRateLimitRecoveryKeepsObservedOwner", (
                    "matching_owner", "newer_rate_limit", "replacement_credential", "same_proxy_edited",
                    "outbox_failure", "caller_rollback", "contradictory_reset",
                )),
                ("TestGrokRequestOutcomeCASModelCooldownMergesConcurrentSibling", (
                    "matching_owner", "replacement_credential", "outbox_failure", "caller_rollback",
                    "unsafe_extra_overwrite",
                )),
                ("TestRuntimeRecoveryGenerationResetPreservesUnrelatedProviderState", (
                    "openai", "deepseek", "kimi", "minimax", "stepfun", "zhipu",
                )),
                ("TestRuntimeRecoveryGenerationOAuthRecoveryAndRefreshKeepDifferentContracts", (
                    "reauthorize", "refresh", "outbox_failure", "caller_rollback",
                )),
                ("TestRuntimeRecoveryGenerationIsPreservedAcrossOrdinaryMetadataWrites", (
                    "Update", "UpdateExtra", "BulkUpdate",
                )),
                ("TestRuntimeRecoveryGenerationCannotBeImportedOnAccountCreation", ("Create", "CreateWithAccountGroups")),
                ("TestGrokRequestOutcomeReturnsGenerationOfItsAtomicCommit", (
                    "failure_before_recovery", "failure_after_recovery", "recovery_after_statement_before_return",
                )),
                ("TestSuccessfulTestRecoveryCommitsOnlyRequiredState", (
                    "error_only", "rates_only", "error_and_rates", "disabled_and_rates",
                    "no_recoverable_state", "account_write_failure", "outbox_failure", "caller_rollback",
                )),
            )
            for scenario in scenarios
        ) + tuple(
            f"TestRuntimeRecoveryGenerationResetCommitsOrRollsBackWithAccount/{provider}/{kind}/{scenario}"
            for provider, kind in (("grok", "oauth"), ("grok", "apikey"), ("openai", "apikey"))
            for scenario in ("success", "outbox_failure", "caller_rollback")
        ) + tuple(
            f"TestSevenProviderPublicSchedulingRecoveryIsAtomic/{provider}/{mode}/{scenario}"
            for provider in PROVIDERS for mode in ("rates", "temp")
            for scenario in ("success", "account_write_failure", "outbox_failure", "caller_rollback")
        ) + tuple(
            f"{ADMIN_RECOVERY}/{provider}/{scenario}"
            for provider in PROVIDERS for scenario in ("success", "middle_row_failure", "final_outbox_failure")
        ) + tuple(
            f"{test}/{scenario}"
            for test, scenarios in (
                ("TestAdminShadowDeletionCommitsWholeCredentialFamilyOrNothing", ("success", "parent_storage_failure")),
                ("TestAdminShadowProxyRestorationCommitsFamilyBindingOrNothing", ("success", "shadow_storage_failure")),
                ("TestAdminRecoveryCallerTransactionPublishesRuntimeOnlyAfterCommit", ("rollback", "commit")),
                ("TestShadowCreationRejectsDeletedCredentialParentWithoutSideEffects", ("Create", "CreateWithAccountGroups")),
                ("TestAdminShadowDeletionPreventsCreationAfterFamilyEnumeration", ("Create", "CreateWithAccountGroups", "AdminCreateShadow")),
                ("TestOAuthCredentialOwnerCASMergesCurrentMetadataAndKeepsRecoveryBoundary", ("refresh", "reauthorize")),
                ("TestOAuthCredentialOwnerCASPreservesNewerModelAndReauthorizationState", (
                    "extra=extra_||_'{\"model_rate_limits\":{\"other\":{\"reason\":\"newer_model_failure\"}}}'::jsonb",
                    "extra=extra_||_'{\"antigravity_quota_scopes\":{\"newer\":true}}'::jsonb",
                    "extra=extra_||_'{\"grok_needs_reauth_reason\":\"newer_billing_failure\"}'::jsonb",
                    "extra=extra_||_'{\"grok_needs_reauth_at\":\"2030-01-01T00:00:00Z\"}'::jsonb",
                )),
                ("TestOAuthCredentialOwnerCASWaitsForConcurrentAccountOrProxyWriter", ("account", "proxy")),
                ("TestOAuthCredentialOwnerCASOutboxFaultAndCallerRollbackRetainPriorOwner", ("outbox_failure", "caller_rollback")),
                ("TestAccountRepoSuite", (
                    "TestDelete", "TestDelete_RemovesSchedulerAccountSnapshot",
                    "TestDelete_TransactionOwnerPublishesSnapshotOnlyAfterCommit", "TestDelete_WithGroupBindings",
                )),
            )
            for scenario in scenarios
        ) + tuple(
            f"TestAdminShadowDeletionRechecksFamilyAfterInFlightCreation/{method}/{outcome}"
            for method in ("Create", "CreateWithAccountGroups") for outcome in ("creation_rollback", "creation_commit")
        ) + tuple(
            f"{test}/{method}/{outcome}"
            for test, outcomes in (
                ("TestOAuthParentTypeConversionSerializesWithShadowCreation", ("conversion_rollback", "conversion_commit")),
                ("TestOAuthParentTypeConversionRechecksShadowCreationBeforeCommit", ("creation_rollback", "creation_commit")),
            )
            for method in ("Create", "CreateWithAccountGroups", "AdminCreateShadow") for outcome in outcomes
        ) + tuple(
            f"TestOAuthCredentialTypeChangeRechecksShadowCommittedWhileWaiting/{outcome}"
            for outcome in ("creation_rollback", "creation_commit")
        ) + tuple(
            f"{ADMIN_APPLY}/{provider}/{scenario}"
            for provider in ("openai", "grok")
            for scenario in ("success", "relinked", "proxy_edited", "manual_pause", "newer_cooldown", "row_failure", "outbox_failure")
        ) + tuple(
            f"{ADMIN_REFRESH}/{provider}/{operation}/{scenario}"
            for provider in ("openai", "grok") for operation in ("single", "batch")
            for scenario in ("success", "relinked", "proxy_edited", "manual_pause", "newer_cooldown", "row_failure", "outbox_failure")
        ) + tuple(
            f"{ADMIN_APPLY_PREHANDLER}/{provider}/{scenario}"
            for provider in ("openai", "grok")
            for scenario in ("relinked", "proxy_edited", "manual_pause", "newer_cooldown", "missing_owner", "malformed_owner")
        ) + tuple(
            f"{APIKEY_POSTGRES_OBSERVATION_TEST}/{probe}/{change}"
            for probe in ("deepseek-payg", "kimi-payg", "kimi-coding", "zhipu-coding", "minimax-coding")
            for change in ("same-owner", "api-key", "nested-credentials")
        ) + tuple(
            f"{APIKEY_POSTGRES_SCHEDULING_TEST}/{provider}/{balance}/{change}"
            for provider in ("deepseek", "kimi")
            for balance in ("low", "healthy")
            for change in ("same-owner", "api-key", "other-cooldown", "manual-pause")
        ) + tuple(
            f"{test}/{scenario}"
            for test, scenarios in (
                ("TestOpenAIQuotaOwnerRefreshFinalWriteCannotOverwriteExternalRelink",
                 ("same-owner-rotation", "external-relink-after-final-validation")),
                ("TestOpenAIQuotaOwnerTaskFinalWriteCannotOverwriteExternalRelink",
                 ("same-owner-task", "external-relink-after-final-validation")),
                ("TestOpenAIQuotaOwnerPersistenceFailureStopsQuotaAndCache",
                 ("refresh/storage", "refresh/outbox", "agent-task/storage", "agent-task/outbox")),
                ("TestOpenAIQuotaOwnerCASOutboxFailureAndCallerRollbackPublishNothing",
                 ("storage-failure", "outbox-failure", "caller-rollback")),
                ("TestOpenAIQuotaOwnerCASRechecksConcurrentAccountAndProxyWriters",
                 ("relink", "same-proxy-route-edit")),
            )
            for scenario in scenarios
        ) + tuple(
            f"{test}/{mode}/{scenario}"
            for test, scenarios in (
                ("TestGrokCredentialFailureCASPublishesOnlyCapturedRoute", (
                    "direct", "bound", "proxy_metadata_changed",
                )),
                ("TestGrokCredentialFailureCASRejectsSameRowProxyChanges", (
                    "host", "port", "username", "password", "protocol", "disabled", "expired", "deleted",
                )),
                ("TestGrokCredentialFailureCASWaitsForConcurrentOwnerOrProxyEdit", ("account", "proxy")),
                ("TestGrokCredentialFailureCASCallerTransactionDoesNotPublishBeforeCommit", (
                    "context_rollback", "managed_rollback", "managed_commit",
                )),
            )
            for mode in ("permanent", "transient") for scenario in scenarios
        ) + tuple(
            f"TestGrokCredentialFailureCASOutboxFailureRollsBackState/{mode}"
            for mode in ("permanent", "transient")
        ) + tuple(
            f"TestGrokCredentialFailureCASMissingProxyQuarantineRejectsRestoration/{scenario}"
            for scenario in ("still_missing", "same_ID_restored")
        ) + tuple(
            f"TestProviderFilesRepositoryPaginationCursorCannotReadAnotherScope/{scope}/{direction}"
            for scope in ("user", "api-key", "group", "platform", "unknown") for direction in ("after", "before")
        ) + tuple(
            f"TestProviderFilesRepositoryOwnerCASRejectsChangedAdmissionSnapshot/{operation}/{mutation}"
            for operation in ("reserve", "publish") for mutation in ("account-status", "proxy-status", "proxy-expiry")
        )),
        RegressionGroup("./internal/integration", (
            BUSINESS_CHAT_TEST,
            BUSINESS_NATIVE_TEST,
            STEP_BUSINESS_TEST,
            "TestSevenProviderManagementEditsReachOtherInstanceBeforeForwarding",
            "TestSevenProviderManagementProxyEditOutboxFailureRollsBackRoute",
            "TestSevenProviderManagementLateProxyProbeCannotReplaceRepairedRoute",
            "TestSevenProviderManagementCreatedProxyBackgroundProbeVisibleOnOtherInstance",
            "TestSevenProviderExpiredProxyFallbackAndRepairReachOtherInstance",
            "TestSevenProviderExpirySweepWaitsForRenewalAndRetainsRenewedBinding",
            "TestSevenProviderExpiryAndRevertOutboxFailuresRollbackSavedBinding",
            "TestSevenProviderProxyDeletionRechecksBindingAfterAdminPreflight",
            "TestSevenProviderBindingWritersRejectProxyDeletedByConcurrentInstance",
            "TestSevenProviderAccountDeletionOutboxFailurePreservesAccountAndCache",
            "TestSevenProviderAccountDeletionRespectsCallerTransactionRollback",
            "TestSevenProviderAccountDeletionAndRecreationReachOtherInstance",
            GROK_CROSS_PROCESS_RECOVERY,
            "TestOpenAIAPIKeyAdminResetRecoversObservedModelTransient",
        ), tuple(
            f"{BUSINESS_CHAT_TEST}/{provider}/{status}"
            for provider in PROVIDERS
            for status in (503, 429)
        ) + tuple(
            f"{BUSINESS_NATIVE_TEST}/{provider}"
            for provider in PROVIDERS
        ) + tuple(
            f"{STEP_BUSINESS_TEST}/{profile}/{protocol}/stream={stream}/failure={failure}"
            for profile in STEP_PROFILES for protocol in STEP_PROTOCOLS
            for stream in ("false", "true") for failure in (503, 429)
        ) + tuple(
            f"TestOpenAIAPIKeyAdminResetRecoversObservedModelTransient/{where}"
            for where in ("same-process", "other-process")
        ) + tuple(
            f"{test}/{provider}"
            for test in ("TestSevenProviderManagementEditsReachOtherInstanceBeforeForwarding",
                         "TestSevenProviderAccountDeletionAndRecreationReachOtherInstance")
            for provider in PROVIDERS
        ) + tuple(
            f"TestSevenProviderManagementLateProxyProbeCannotReplaceRepairedRoute/{scenario}"
            for scenario in ("late-success-without-new-probe", "late-success-after-new-success",
                             "late-failure-after-new-success", "late-quality-failure-after-new-success")
        ) + tuple(
            f"TestSevenProviderExpiredProxyFallbackAndRepairReachOtherInstance/{provider}/{mode}"
            for provider in PROVIDERS for mode in ("proxy", "direct", "none")
        ) + tuple(
            f"TestSevenProviderExpiryAndRevertOutboxFailuresRollbackSavedBinding/{operation}" for operation in ("expiry", "revert")
        ) + tuple(
            f"TestSevenProviderBindingWritersRejectProxyDeletedByConcurrentInstance/{operation}" for operation in ("create", "update", "bulk-update", "revert")
        ) + tuple(
            f"{GROK_CROSS_PROCESS_RECOVERY}/{failure}"
            for failure in ("model-capacity", "model-free-usage", "account-unauthorized")
        )),
    ),
    "storage": (
        RegressionGroup("./internal/repository", ("TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate",)),
        RegressionGroup("./internal/handler", ("TestAsyncImageContainerWorkflowWithMinIO",)),
    ),
}


def coverage_errors(
    lines: Iterable[str], *, package: str, tests: tuple[str, ...],
    required_subtests: tuple[str, ...] = (), returncode: int = 0,
) -> list[str]:
    """Check actual go test -json events, including every observed descendant."""
    errors: list[str] = []
    ran: set[str] = set()
    passed: set[str] = set()
    finished: set[str] = set()
    package_passed = False
    for number, line in enumerate(lines, 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            errors.append(f"line {number}: invalid go test JSON")
            continue
        if not isinstance(event, dict) or not isinstance(event.get("Action"), str):
            errors.append(f"line {number}: invalid go test event")
            continue
        action = event["Action"]
        if action == "build-fail":
            errors.append("Go package build failed")
        if event.get("Package") != package:
            continue
        test = event.get("Test")
        if test is not None and not isinstance(test, str):
            errors.append(f"line {number}: invalid test name")
            continue
        if not test:
            if action == "pass":
                package_passed = True
            elif action in {"fail", "skip"}:
                errors.append(f"package {action}")
            continue
        if action == "run":
            ran.add(test)
        elif action in {"pass", "fail", "skip"}:
            finished.add(test)
            if action != "pass":
                errors.append(f"{test}: {action}")
            elif test not in ran:
                errors.append(f"{test}: pass without a run event")
            else:
                passed.add(test)
    for test in (*tests, *required_subtests):
        if test not in ran:
            errors.append(f"{test}: missing run")
        elif test not in passed:
            errors.append(f"{test}: missing pass")
    for test in sorted(ran - finished):
        errors.append(f"{test}: incomplete test")
    if not package_passed:
        errors.append("missing package pass")
    if returncode:
        errors.append(f"go test exited {returncode}")
    return errors


def validate_manifest(groups: tuple[RegressionGroup, ...]) -> None:
    if not groups or len({group.package for group in groups}) != len(groups):
        raise ValueError("regression manifest requires distinct nonempty package groups")
    for group in groups:
        if not re.fullmatch(r"\./internal/[a-z0-9_/]+", group.package):
            raise ValueError("invalid regression package")
        if not group.tests or len(set(group.tests)) != len(group.tests):
            raise ValueError("regression manifest requires distinct nonempty tests")
        if any(not re.fullmatch(r"Test[A-Za-z0-9_]+", name) for name in group.tests):
            raise ValueError("manifest entries must name top-level Go tests")
        if len(set(group.required_subtests)) != len(group.required_subtests):
            raise ValueError("duplicate required subtest")
        if any(name.split("/", 1)[0] not in group.tests or "/" not in name
               for name in group.required_subtests):
            raise ValueError("required subtests must belong to a required top-level test")


def run_profile(profile: str, *, root: Path = ROOT) -> int:
    validation_runtime.require_in_validation(tool="seven-provider coverage gate")
    groups = PROFILES[profile]
    validate_manifest(groups)
    if profile == "storage":
        required = (
            "SUB2API_TEST_POSTGRES_DSN", "SUB2API_TEST_REDIS_ADDR", "SUB2API_TEST_REDIS_DB",
            "SUB2API_ASYNC_TEST_DATABASE_URL", "SUB2API_ASYNC_TEST_S3_ENDPOINT",
        )
        missing = [name for name in required if not os.environ.get(name, "").strip()]
        if missing:
            raise ValueError("storage profile requires explicit disposable services: " + ", ".join(missing))
        if os.environ["SUB2API_ASYNC_TEST_DATABASE_URL"] != os.environ["SUB2API_TEST_POSTGRES_DSN"]:
            raise ValueError("storage profile requires the async workflow and migrations to use the same disposable database")
    backend = root / "backend"
    match = re.search(r"^module\s+(\S+)\s*$", (backend / "go.mod").read_text(), re.MULTILINE)
    if match is None:
        raise ValueError("backend module declaration is missing")
    module = match.group(1)
    failed = False
    for group in groups:
        expression = "^(" + "|".join(re.escape(name) for name in group.tests) + ")$"
        build_tag = "integration" if profile == "storage" else profile
        command = ["go", "test", "-json", "-count=1", f"-tags={build_tag}",
                   "-timeout=5m", "-run", expression, group.package]
        print(f"Seven-provider {profile}: {group.package}", flush=True)
        result = subprocess.run(command, cwd=backend, text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, check=False)
        problems = coverage_errors(
            result.stdout.splitlines(), package=module + group.package[1:],
            tests=group.tests, required_subtests=group.required_subtests,
            returncode=result.returncode,
        )
        if problems:
            failed = True
            for problem in problems:
                print(f"  {group.package}: {problem}", file=sys.stderr)
            if profile == "storage":
                return 1
        else:
            print(f"  passed {len(group.tests)} required tests and {len(group.required_subtests)} "
                  "required subtests; all observed subtests passed", flush=True)
    return int(failed)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", required=True, choices=tuple(PROFILES))
    args = parser.parse_args(argv)
    try:
        return run_profile(args.profile)
    except (OSError, ValueError, validation_runtime.ValidationRuntimeError) as error:
        print(f"seven-provider coverage failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
