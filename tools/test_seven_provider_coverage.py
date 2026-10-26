#!/usr/bin/env python3
"""Requirements: required regressions execute; missing or skipped cases fail."""

from __future__ import annotations

import contextlib
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import check_seven_provider_coverage as coverage
import validation_runtime


PACKAGE = "example.invalid/sub2api/internal/service"
TEST = "TestRequiredBusinessFlow"
CHILD = TEST + "/timeout"


def event(action: str, test: str | None = None, *, package: str = PACKAGE) -> dict:
    result = {"Action": action, "Package": package}
    if test is not None:
        result["Test"] = test
    return result


def successful_events(*, tests: tuple[str, ...] = (TEST,),
                      children: tuple[str, ...] = (), package: str = PACKAGE) -> list[dict]:
    events = [event("start", package=package)]
    for test in tests:
        events.append(event("run", test, package=package))
        for child in children:
            if child.startswith(test + "/"):
                events.extend([event("run", child, package=package),
                               event("pass", child, package=package)])
        events.append(event("pass", test, package=package))
    events.append(event("pass", package=package))
    return events


class GoEventCoverageTests(unittest.TestCase):
    def errors(self, events: list[dict], *, tests: tuple[str, ...] = (TEST,),
               required_subtests: tuple[str, ...] = (), returncode: int = 0) -> list[str]:
        return coverage.coverage_errors(
            [json.dumps(item) for item in events], package=PACKAGE, tests=tests,
            required_subtests=required_subtests, returncode=returncode,
        )

    def test_executed_parent_and_children_must_all_pass(self) -> None:
        self.assertEqual(self.errors(successful_events(children=(CHILD,)),
                                     required_subtests=(CHILD,)), [])

    def test_empty_testmain_success_does_not_prove_any_test_executed(self) -> None:
        # Integration TestMain may exit zero when its database is unavailable.
        errors = self.errors([event("start"), event("pass")])
        self.assertIn(TEST + ": missing run", errors)

    def test_a_different_passing_test_cannot_replace_required_case(self) -> None:
        errors = self.errors(successful_events(tests=("TestOtherFlow",)))
        self.assertIn(TEST + ": missing run", errors)

    def test_top_level_skip_or_failure_cannot_be_covered_by_package_success(self) -> None:
        for action in ("skip", "fail"):
            with self.subTest(action=action):
                errors = self.errors([event("run", TEST), event(action, TEST), event("pass")])
                self.assertIn(f"{TEST}: {action}", errors)
                self.assertIn(TEST + ": missing pass", errors)

    def test_any_observed_child_skip_or_failure_rejects_passing_parent(self) -> None:
        # Unlisted descendants are still required once their parent runs them.
        for action in ("skip", "fail"):
            with self.subTest(action=action):
                errors = self.errors([
                    event("run", TEST), event("run", CHILD), event(action, CHILD),
                    event("pass", TEST), event("pass"),
                ])
                self.assertIn(f"{CHILD}: {action}", errors)

    def test_later_pass_cannot_erase_an_observed_skip(self) -> None:
        errors = self.errors([
            event("run", TEST), event("run", CHILD), event("skip", CHILD),
            event("pass", CHILD), event("pass", TEST), event("pass"),
        ])
        self.assertIn(CHILD + ": skip", errors)

    def test_started_parent_or_child_must_finish(self) -> None:
        for unfinished in (TEST, CHILD):
            with self.subTest(unfinished=unfinished):
                events = [event("run", TEST)]
                if unfinished == CHILD:
                    events.extend([event("run", CHILD), event("pass", TEST)])
                events.append(event("pass"))
                self.assertIn(unfinished + ": incomplete test", self.errors(events))

    def test_all_fourteen_provider_failover_scenarios_are_required(self) -> None:
        children = tuple(f"{TEST}/{provider}/{status}"
                         for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
                         for status in (503, 429))
        self.assertEqual(self.errors(successful_events(children=children),
                                     required_subtests=children), [])
        errors = self.errors(successful_events(children=children[:-1]),
                             required_subtests=children)
        self.assertIn(children[-1] + ": missing run", errors)

    def test_every_pinned_business_case_needs_its_own_execution(self) -> None:
        # A passing table-driven parent must not hide a removed provider,
        # proxy failure, storage fault or malformed observation row.
        for profile, groups in coverage.PROFILES.items():
            for group in groups:
                if not group.required_subtests:
                    continue
                events = successful_events(tests=group.tests, children=group.required_subtests)
                self.assertEqual(self.errors(events, tests=group.tests,
                                             required_subtests=group.required_subtests), [])
                for missing in group.required_subtests:
                    with self.subTest(profile=profile, package=group.package, missing=missing):
                        incomplete = [item for item in events if item.get("Test") != missing]
                        errors = self.errors(incomplete, tests=group.tests,
                                             required_subtests=group.required_subtests)
                        self.assertIn(missing + ": missing run", errors)

    def test_manifest_has_unique_cases_and_every_child_has_a_required_parent(self) -> None:
        for profile, groups in coverage.PROFILES.items():
            for group in groups:
                with self.subTest(profile=profile, package=group.package):
                    self.assertEqual(len(group.tests), len(set(group.tests)))
                    self.assertEqual(len(group.required_subtests), len(set(group.required_subtests)))
                    self.assertTrue(all(child.split("/", 1)[0] in group.tests
                                        for child in group.required_subtests))

    def test_bridge_token_and_reset_manifests_pin_independent_sender_boundaries(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/service")
        expected = {
            "TestGrokChatResponsesBridgeRetainsErrorObservationOwner": {
                f"{status}/{change}"
                for status in ("Unauthorized", "Too_Many_Requests", "Bad_Gateway")
                for change in ("unchanged", "relinked", "proxy-edited", "manual-pause", "other-cooldown", "storage-failure")
            },
            "TestGrokChatResponsesBridgeRejectsBrokenProxyBeforeCredentialLookup": {
                "valid-bound", "explicitly-unbound", "missing-bound-snapshot", "mismatched-bound-owner",
                "malformed-bound-host", "incomplete-bound-auth",
            },
            "TestGrokRequestTokenAcquisitionRetainsCapturedOwner": {
                f"{phase}/{change}"
                for phase in ("cache-hit", "version-adoption", "refresh-reread", "refresh-response", "refresh-persisted", "lock-wait")
                for change in ("unchanged", "relinked", "proxy-edited", "identity-edited", "storage-failure")
            },
            "TestGrokManualTokenRefreshRetainsCapturedOwner": {
                "unchanged", "relinked", "proxy-edited", "identity-edited", "storage-failure",
            },
            "TestGrokOwnedTokenRotationPreservesOfficialProfileMetadata": {
                f"{mode}/{change}" for mode in ("request", "manual")
                for change in ("profile-metadata", "changed-subject", "changed-team", "changed-client", "changed-identity", "changed-base")
            },
            "TestOAuthCredentialRecoveryOnlyClearsCapturedRuntimeGeneration": {
                f"{provider}/{change}" for provider in ("openai", "grok")
                for change in ("unchanged", "same-owner-new-failure", "relinked", "proxy-edited", "no-prior-block", "storage-failure", "owner-cas-miss")
            },
            "TestGrokManualResetRejectsPreResetOutcomePublication": {
                f"{operation}/{boundary}"
                for operation in ("account-cooldown", "model-free-usage", "model-capacity")
                for boundary in ("unchanged", "reset-after-commit")
            },
            "TestAdminResetGrokAccountClearsItsModelOwnersAndPreservesTeamLimits": {
                "free_usage", "transient_capacity",
            },
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_native_refresh_and_redacted_dto_manifests_require_every_provider(self) -> None:
        expected = {
            "./internal/handler/admin": (
                "TestNativeAccountsWithoutBusinessRefreshNeverUseForeignIssuer",
                {f"{provider}/{marker}" for provider in ("deepseek", "zhipu")
                 for marker in ("native-marker", "missing-native-marker")},
            ),
            "./internal/handler/dto": (
                "TestAccountDTOExposesOpaqueOAuthOwnerWithoutLeakingCredentials",
                {"openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu"},
            ),
        }
        for package, (test, scenarios) in expected.items():
            with self.subTest(package=package):
                group = next(group for group in coverage.PROFILES["unit"] if group.package == package)
                self.assertIn(test, group.tests)
                self.assertEqual(set(group.required_subtests), {test + "/" + scenario for scenario in scenarios})

    def test_model_token_count_and_specialized_billing_entrypoints_remain_required(self) -> None:
        service_group = next(group for group in coverage.PROFILES["unit"]
                             if group.package == "./internal/service")
        required = {
            "TestStepFunOAuthCatalogFailureKeepsGrantUsable",
            "TestStepFunOAuthCatalogDiscardsResponseAfterCancellation",
            "TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_APIKeyUsesResponsesInputTokens",
            "TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_CodexProfileGateRunsBeforeCredentialLookup",
            "TestOpenAIGatewayService_OpenAIOAuthInputTokensFallbackUsesMinimumWhenEstimateFails",
            "TestForwardResponsesInputTokensCustomRelayUsesLocalEstimate",
            "TestForwardResponsesInputTokensGrokOAuthUsesLocalEstimate",
            "TestForwardResponsesInputTokensUpstream404FallsBackLocally",
            "TestRunLiveControllerClosesExpiredSession",
            "TestFinalizeLiveCallIsIdempotentAndWritesZeroUsage",
            "TestFinalizeLiveCallUsageLogFallsBackToSyncCreate",
            "TestForwardGrokResponses_PropagatesSearchCountFromJSON",
            "TestForwardGrokResponses_PropagatesSearchCountFromSSE",
            "TestIsGrokVideoStatusBillable",
            "TestExtractGrokVideoBillingFromStatusBodyPrefersUpstreamParams",
            "TestExtractGrokVideoBillingFromStatusBodyFallsBackToPending",
            "TestExtractGrokVideoBillingRejectsNonDoneStatus",
            "TestGrokMediaUsageFromResponseVideoCreateDoesNotBill",
            "TestGrokMediaUsageFromResponseVideoStatusBillsOnOfficialDone",
        }
        self.assertTrue(required.issubset(service_group.tests))
        expected = {
            "TestStepFunOAuthCatalogBeforeCreation": {"cn", "global"},
            "TestStepFunOAuthCatalogRejectsInvalidSessionBeforeUpstream": {
                "owner", "platform", "pending", "cancelled", "expired", "grant-expired", "committing",
            },
            "TestStepFunModelRestrictionControlsActualAccountSelection": {
                f"{kind}/{model}" for kind in ("oauth", "apikey") for model in ("step-3.7-flash", "public-step")
            },
            "TestOpenAIGatewayService_ForwardCountTokensAsAnthropic_OAuthFallsBackWhenPlatformEndpointUnsupported": {
                "401_missing_responses_write_scope", "403_missing_responses_write_scope",
                "403_html_proxy_page", "404_input_tokens_unsupported",
            },
            "TestEstimateOpenAIInputTokens_RequestSamples": {
                "simple_text_input", "instructions_plus_tool_schema", "input_parts_and_tool_output",
            },
            "TestLiveSessionEndedTreatsLeaseLossAsTerminal": {
                "租约丢失", "租约丢失（被包装）", "上游报告会话已关闭", "到达会话时长上限",
                "控制权被他人接管", "临时读错误", "无错误",
            },
            "TestObserveLiveCallStoreOutageFallsBackToExpiryFinalize": {"GetLiveCall_持续报错", "ClaimLiveController_报错"},
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, service_group.tests)
                self.assertEqual({child for child in service_group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})
        handler_group = next(group for group in coverage.PROFILES["unit"]
                             if group.package == "./internal/handler")
        self.assertTrue({
            "TestGrokRealtimeTurnTrackerDeduplicatesPendingResponseAndAuditsAdmin",
            "TestGrokRealtimeBillingResultRequiresObservedAudio",
            "TestGrokRealtimeBillingResultUsesForcedUniqueID",
        }.issubset(handler_group.tests))

    def test_count_tokens_independent_senders_and_local_paths_remain_required(self) -> None:
        expected = {
            "./internal/service": {
                "TestCountTokensUpstreamRejectsBrokenProxyBeforeCredentialLookup": {
                    f"{entry}/{binding}"
                    for entry in ("responses-native", "messages-count")
                    for binding in ("valid-bound", "explicitly-unbound", "missing-bound-snapshot",
                                    "mismatched-bound-owner", "malformed-bound-host", "incomplete-bound-auth")
                },
                "TestCountTokensUpstreamKeepsCapturedOwnerDuringTokenLookup": {
                    f"{entry}/{change}"
                    for entry in ("responses-native", "messages-count")
                    for change in ("proxy-route", "credential-relink", "identity", "saved-unbinding", "saved-binding")
                },
                "TestCountTokensLocalEstimatesKeepZeroCredentialAndNetworkEffects": {
                    f"{entry}/{provider}"
                    for entry, providers in (
                        ("responses-native", ("grok", "custom-relay", "deepseek", "kimi", "minimax", "stepfun", "zhipu")),
                        ("messages-count", ("deepseek", "kimi", "minimax", "stepfun", "zhipu")),
                    )
                    for provider in providers
                },
            },
            "./internal/repository": {
                "TestCountTokensProductionProxyContract": {
                    f"{entry}/{mode}"
                    for entry in ("responses-native", "messages-count")
                    for mode in ("success", "unbound-direct", "refused", "timeout", "authentication-required", "missing-bound-route")
                },
            },
        }
        for package, tests in expected.items():
            group = next(group for group in coverage.PROFILES["unit"] if group.package == package)
            for test, scenarios in tests.items():
                with self.subTest(package=package, test=test):
                    self.assertIn(test, group.tests)
                    self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                     {test + "/" + scenario for scenario in scenarios})

    def test_admin_oauth_and_shadow_manifests_require_transaction_and_concurrency_boundaries(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/repository")
        scenarios = ("success", "relinked", "proxy_edited", "manual_pause", "newer_cooldown", "row_failure", "outbox_failure")
        expected = {
            "TestSevenProviderAdminRecoveryCommitsAllStateOrNothing": {
                f"{provider}/{outcome}" for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
                for outcome in ("success", "middle_row_failure", "final_outbox_failure")
            },
            "TestAdminApplyOAuthCredentialsKeepsFullOwnerAndAtomicRecovery": {
                f"{provider}/{outcome}" for provider in ("openai", "grok") for outcome in scenarios
            },
            "TestAdminSingleAndBatchRefreshCannotPublishLateOAuthOwner": {
                f"{provider}/{operation}/{outcome}" for provider in ("openai", "grok")
                for operation in ("single", "batch") for outcome in scenarios
            },
            "TestAdminApplyOAuthCredentialsRejectsOwnerChangedBeforeHandlerRead": {
                f"{provider}/{outcome}" for provider in ("openai", "grok")
                for outcome in ("relinked", "proxy_edited", "manual_pause", "newer_cooldown", "missing_owner", "malformed_owner")
            },
            "TestShadowCreationRejectsDeletedCredentialParentWithoutSideEffects": {"Create", "CreateWithAccountGroups"},
            "TestAdminShadowDeletionPreventsCreationAfterFamilyEnumeration": {"Create", "CreateWithAccountGroups", "AdminCreateShadow"},
            "TestAdminShadowDeletionRechecksFamilyAfterInFlightCreation": {
                f"{method}/{outcome}" for method in ("Create", "CreateWithAccountGroups")
                for outcome in ("creation_rollback", "creation_commit")
            },
            "TestOAuthParentTypeConversionSerializesWithShadowCreation": {
                f"{method}/{outcome}" for method in ("Create", "CreateWithAccountGroups", "AdminCreateShadow")
                for outcome in ("conversion_rollback", "conversion_commit")
            },
            "TestOAuthParentTypeConversionRechecksShadowCreationBeforeCommit": {
                f"{method}/{outcome}" for method in ("Create", "CreateWithAccountGroups", "AdminCreateShadow")
                for outcome in ("creation_rollback", "creation_commit")
            },
            "TestOAuthCredentialTypeChangeRechecksShadowCommittedWhileWaiting": {"creation_rollback", "creation_commit"},
            "TestAdminShadowDeletionCommitsWholeCredentialFamilyOrNothing": {"success", "parent_storage_failure"},
            "TestAdminShadowProxyRestorationCommitsFamilyBindingOrNothing": {"success", "shadow_storage_failure"},
            "TestAdminRecoveryCallerTransactionPublishesRuntimeOnlyAfterCommit": {"rollback", "commit"},
            "TestAccountRepoSuite": {
                "TestDelete", "TestDelete_RemovesSchedulerAccountSnapshot",
                "TestDelete_TransactionOwnerPublishesSnapshotOnlyAfterCommit", "TestDelete_WithGroupBindings",
            },
            "TestOAuthCredentialOwnerCASMergesCurrentMetadataAndKeepsRecoveryBoundary": {"refresh", "reauthorize"},
            "TestOAuthCredentialOwnerCASPreservesNewerModelAndReauthorizationState": {
                "extra=extra_||_'{\"model_rate_limits\":{\"other\":{\"reason\":\"newer_model_failure\"}}}'::jsonb",
                "extra=extra_||_'{\"antigravity_quota_scopes\":{\"newer\":true}}'::jsonb",
                "extra=extra_||_'{\"grok_needs_reauth_reason\":\"newer_billing_failure\"}'::jsonb",
                "extra=extra_||_'{\"grok_needs_reauth_at\":\"2030-01-01T00:00:00Z\"}'::jsonb",
            },
            "TestOAuthCredentialOwnerCASWaitsForConcurrentAccountOrProxyWriter": {"account", "proxy"},
            "TestOAuthCredentialOwnerCASOutboxFaultAndCallerRollbackRetainPriorOwner": {"outbox_failure", "caller_rollback"},
        }
        for test, children in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in children})
        self.assertIn("TestAdminRecoveryPreservesManualPauseAndExpirationConfiguration", group.tests)
        self.assertIn("TestOAuthCredentialTypeChangeRejectsShadowCreatedAfterPreflight", group.tests)
        self.assertIn("TestAdminShadowCreationRechecksParentTypeAtPersistence", group.tests)

    def test_grok_diagnostic_and_credential_failure_regressions_remain_required(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"] if group.package == "./internal/service")
        expected = {
            "TestAccountTestService_Grok429PersistsRateLimitReset": set(),
            "TestAccountTestService_Grok429WithoutQuotaHeadersUsesFallback": set(),
            "TestGetRequestCredentialMapsTransientAndProviderFailuresSeparately": {
                "selected_owner_missing_refresh_credential_permanently_blocks_the_account",
                "refresh_added_after_selected_owner_structural_failure_wins_conditional_mutation",
                "refresh_removed_after_selection_rejects_replacement_owner_without_quarantine",
            },
            "TestCredentialFailureCASMissDoesNotRecoverIneligibleLatestCredential": {
                f"{failure}/independently_runtime_blocked" for failure in ("permanent", "transient")
            },
            "TestGetRequestCredentialSharedCredentialPersistenceFailureStopsWithoutAccountMutation": set(),
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_runtime_recovery_manifest_keeps_commit_owner_and_observation_boundaries(self) -> None:
        expected = {
            ("unit", "./internal/service"): {
                "TestGrokModelCooldownSelectionUsesActualMappedModel": {
                    "mapped-to-failing-model", "mapped-to-healthy-sibling",
                },
                "TestGrokOutcomePublicationUsesActualCommitRecoveryGeneration": {
                    f"{operation}/{order}"
                    for operation in ("account-cooldown", "model-capacity")
                    for order in ("unchanged", "reset-before-commit", "remote-reset-after-commit", "storage-failure")
                },
                "TestGrokModelRecoveryKeepsNewGenerationFailureAndPrunesExpiredHistory": set(),
                "TestOAuthCredentialOwnerIncludesSavedRecoveryState": set(),
                "TestOAuthCredentialCommitRejectsCallerRecoveryGeneration": set(),
                "TestGrokOutcomePublicationRetainsOwnerWhenRepositoryMutatesCaller": set(),
                "TestGrokAdminRecoveryKeepsNewFailureCommittedBeforeLocalCleanup": set(),
                "TestGrokOutcomeCommittedAfterRecoveryKeepsItsNewGenerationDespiteEarlierEpoch": set(),
                "TestSevenProviderTempUnschedStatusUsesAuthoritativeSavedState": {
                    f"{provider}/{scenario}"
                    for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
                    for scenario in ("recovered_account", "newer_plain_reason", "newer_structured_reason",
                                     "matching_saved_state", "deleted_account", "account_read_failure", "cache_unavailable")
                },
                "TestOpenAIAPIKeyModelFailureKeepsRecoveryObservationBoundary": {
                    "unchanged", "reset-before-read", "reset-after-read", "relink-before-read", "proxy-edit-before-read", "read-failure",
                },
                "TestOpenAIAPIKeyModelSuccessClearsOnlyItsRecoveryGeneration": {"current-success", "historical-success"},
            },
            ("integration", "./internal/repository"): {
                "TestRuntimeRecoveryGenerationResetCommitsOrRollsBackWithAccount": {
                    f"{provider}/{kind}/{scenario}"
                    for provider, kind in (("grok", "oauth"), ("grok", "apikey"), ("openai", "apikey"))
                    for scenario in ("success", "outbox_failure", "caller_rollback")
                },
                "TestRuntimeRecoveryGenerationResetPreservesUnrelatedProviderState": {
                    "openai", "deepseek", "kimi", "minimax", "stepfun", "zhipu",
                },
                "TestRuntimeRecoveryGenerationOAuthRecoveryAndRefreshKeepDifferentContracts": {
                    "reauthorize", "refresh", "outbox_failure", "caller_rollback",
                },
                "TestRuntimeRecoveryGenerationIsPreservedAcrossOrdinaryMetadataWrites": {"Update", "UpdateExtra", "BulkUpdate"},
                "TestRuntimeRecoveryGenerationCannotBeImportedOnAccountCreation": {"Create", "CreateWithAccountGroups"},
                "TestGrokRequestOutcomeReturnsGenerationOfItsAtomicCommit": {
                    "failure_before_recovery", "failure_after_recovery", "recovery_after_statement_before_return",
                },
                "TestSevenProviderPublicSchedulingRecoveryIsAtomic": {
                    f"{provider}/{mode}/{scenario}"
                    for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
                    for mode in ("rates", "temp")
                    for scenario in ("success", "account_write_failure", "outbox_failure", "caller_rollback")
                },
                "TestSuccessfulTestRecoveryCommitsOnlyRequiredState": {
                    "error_only", "rates_only", "error_and_rates", "disabled_and_rates",
                    "no_recoverable_state", "account_write_failure", "outbox_failure", "caller_rollback",
                },
            },
        }
        for (profile, package), tests in expected.items():
            group = next(group for group in coverage.PROFILES[profile] if group.package == package)
            for test, scenarios in tests.items():
                with self.subTest(profile=profile, package=package, test=test):
                    self.assertIn(test, group.tests)
                    self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                     {test + "/" + scenario for scenario in scenarios})

    def test_cross_process_recovery_manifest_requires_live_parent_scenarios_without_helper(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/integration")
        test = "TestGrokAdminResetReachesFailureHoldingProcessThroughOutbox"
        self.assertIn(test, group.tests)
        self.assertNotIn("TestGrokAdminRecoveryIndependentProcess", group.tests)
        self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")}, {
            test + "/" + failure for failure in ("model-capacity", "model-free-usage", "account-unauthorized")
        })
        openai_test = "TestOpenAIAPIKeyAdminResetRecoversObservedModelTransient"
        self.assertIn(openai_test, group.tests)
        self.assertEqual({child for child in group.required_subtests if child.startswith(openai_test + "/")}, {
            openai_test + "/" + where for where in ("same-process", "other-process")
        })

    def test_logged_subprocess_pass_cannot_replace_a_provider_run(self) -> None:
        child = TEST + "/zhipu"
        events = successful_events()
        events.insert(2, {**event("output", TEST), "Output": f"--- PASS: {child}\n"})
        self.assertIn(child + ": missing run", self.errors(events, required_subtests=(child,)))

    def test_child_from_other_owner_or_status_does_not_satisfy_required_case(self) -> None:
        required = TEST + "/kimi/429"
        for observed in (TEST + "/kimi/503", TEST + "/deepseek/429"):
            with self.subTest(observed=observed):
                self.assertIn(required + ": missing run", self.errors(
                    successful_events(children=(observed,)), required_subtests=(required,)))

    def test_events_for_a_different_package_cannot_satisfy_manifest(self) -> None:
        errors = self.errors(successful_events(package="example.invalid/other/internal/service"))
        self.assertIn(TEST + ": missing run", errors)
        self.assertIn("missing package pass", errors)

    def test_build_failure_or_process_failure_cannot_be_reported_as_covered(self) -> None:
        errors = self.errors(successful_events() + [event("build-fail")])
        self.assertIn("Go package build failed", errors)
        self.assertIn("go test exited 1", self.errors(successful_events(), returncode=1))

    def test_package_failure_skip_and_missing_completion_reject_test_success(self) -> None:
        for action in ("fail", "skip", "output"):
            with self.subTest(action=action):
                events = successful_events()[:-1] + [event(action)]
                self.assertIn("missing package pass", self.errors(events))

    def test_pass_without_execution_is_not_a_successful_test(self) -> None:
        errors = self.errors([event("pass", TEST), event("pass")])
        self.assertIn(TEST + ": pass without a run event", errors)
        self.assertIn(TEST + ": missing run", errors)

    def test_invalid_json_and_invalid_event_shapes_fail(self) -> None:
        good_lines = [json.dumps(item) for item in successful_events()]
        for invalid in ("not JSON", "[]", '{"Package":"x"}', '{"Action":17}'):
            with self.subTest(invalid=invalid):
                errors = coverage.coverage_errors(good_lines + [invalid], package=PACKAGE, tests=(TEST,))
                self.assertTrue(any("invalid go test" in error for error in errors))
        errors = self.errors(successful_events() + [{"Action": "run", "Package": PACKAGE, "Test": 17}])
        self.assertTrue(any("invalid test name" in error for error in errors))


class ManifestAndExecutionTests(unittest.TestCase):
    def test_both_manifests_have_distinct_nonempty_test_groups(self) -> None:
        for profile in ("unit", "integration", "storage"):
            coverage.validate_manifest(coverage.PROFILES[profile])

    def test_storage_manifest_requires_migrations_before_the_minio_workflow(self) -> None:
        self.assertEqual(coverage.PROFILES["storage"], (
            coverage.RegressionGroup("./internal/repository", ("TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate",)),
            coverage.RegressionGroup("./internal/handler", ("TestAsyncImageContainerWorkflowWithMinIO",)),
        ))

    def test_storage_gate_requires_explicit_services_and_the_migrated_database(self) -> None:
        environment = {
            "SUB2API_TEST_POSTGRES_DSN": "postgres://fixture.invalid/disposable",
            "SUB2API_TEST_REDIS_ADDR": "fixture.invalid:6379",
            "SUB2API_TEST_REDIS_DB": "3",
            "SUB2API_ASYNC_TEST_DATABASE_URL": "postgres://fixture.invalid/disposable",
            "SUB2API_ASYNC_TEST_S3_ENDPOINT": "http://fixture.invalid:9000",
        }
        for missing in environment:
            with self.subTest(missing=missing), \
                 mock.patch.dict(os.environ, {key: value for key, value in environment.items() if key != missing}, clear=True), \
                 mock.patch.object(validation_runtime, "require_in_validation"), \
                 mock.patch.object(coverage.subprocess, "run") as run:
                with self.assertRaisesRegex(ValueError, missing):
                    coverage.run_profile("storage", root=Path("/nonexistent"))
                run.assert_not_called()
        with mock.patch.dict(os.environ, {**environment, "SUB2API_ASYNC_TEST_DATABASE_URL": "postgres://fixture.invalid/other"}, clear=True), \
             mock.patch.object(validation_runtime, "require_in_validation"), \
             mock.patch.object(coverage.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "same disposable database"):
                coverage.run_profile("storage", root=Path("/nonexistent"))
            run.assert_not_called()

    def test_storage_gate_executes_uncached_integration_and_rejects_skipped_or_missing_steps(self) -> None:
        environment = {
            "SUB2API_TEST_POSTGRES_DSN": "postgres://fixture.invalid/disposable",
            "SUB2API_TEST_REDIS_ADDR": "fixture.invalid:6379",
            "SUB2API_TEST_REDIS_DB": "3",
            "SUB2API_ASYNC_TEST_DATABASE_URL": "postgres://fixture.invalid/disposable",
            "SUB2API_ASYNC_TEST_S3_ENDPOINT": "http://fixture.invalid:9000",
        }
        steps = (
            ("./internal/repository", "TestMigrationsRunner_IsIdempotent_AndSchemaIsUpToDate"),
            ("./internal/handler", "TestAsyncImageContainerWorkflowWithMinIO"),
        )
        for failed_step, action in ((None, None), (0, "skip"), (0, "missing"), (1, "skip"), (1, "missing")):
            with self.subTest(failed_step=failed_step, action=action), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                backend = root / "backend"
                backend.mkdir()
                (backend / "go.mod").write_text("module example.invalid/sub2api\n")
                outputs = []
                for index, (package, test) in enumerate(steps):
                    module_package = "example.invalid/sub2api" + package[1:]
                    events = successful_events(tests=(test,), package=module_package)
                    if index == failed_step:
                        if action == "skip":
                            events = [event("run", test, package=module_package),
                                      event("skip", test, package=module_package), event("pass", package=module_package)]
                        else:
                            events = [event("pass", package=module_package)]
                    outputs.append(subprocess.CompletedProcess([], 0, stdout="\n".join(json.dumps(item) for item in events)))
                with mock.patch.dict(os.environ, environment, clear=True), \
                     mock.patch.object(validation_runtime, "require_in_validation"), \
                     mock.patch.object(coverage.subprocess, "run", side_effect=outputs) as run, \
                     contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                    self.assertEqual(coverage.run_profile("storage", root=root), int(failed_step is not None))
                expected_steps = steps[:1] if failed_step == 0 else steps
                self.assertEqual([call.args[0] for call in run.call_args_list], [
                    ["go", "test", "-json", "-count=1", "-tags=integration", "-timeout=5m",
                     "-run", "^(" + test + ")$", package]
                    for package, test in expected_steps
                ])
                self.assertTrue(all(call.kwargs["cwd"] == backend for call in run.call_args_list))

    def test_local_plugin_process_manifest_requires_every_lifecycle_boundary(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/service")
        test = "TestLocalSignedPluginRuntimeIntegration"
        expected_children = {
            test + "/request-response-config-and-passive-health",
            test + "/preflight-refusal-sends-no-http-request",
            test + "/delivered-transport-failure-forbids-account-replay",
            test + "/upload-read-failure-closes-body-without-http-replay",
            test + "/cancellation-reaches-child-http-and-releases-lease",
        }
        self.assertEqual(group.tests, (test,))
        self.assertEqual(set(group.required_subtests), expected_children)
        # The optional externally supplied release package has a separate
        # compatibility boundary and cannot replace this mandatory local exec.
        self.assertNotIn("TestPluginRuntimeIntegration", group.tests)

    def test_invalid_manifest_entries_are_rejected_before_execution(self) -> None:
        group = coverage.RegressionGroup("./internal/service", (TEST,))
        for groups in (
            (), (group, group),
            (coverage.RegressionGroup("../other", (TEST,)),),
            (coverage.RegressionGroup(group.package, ()),),
            (coverage.RegressionGroup(group.package, (TEST, TEST)),),
            (coverage.RegressionGroup(group.package, (TEST + "/child",)),),
            (coverage.RegressionGroup(group.package, (TEST,), ("TestOther/child",)),),
            (coverage.RegressionGroup(group.package, (TEST,), (CHILD, CHILD)),),
        ):
            with self.subTest(groups=groups), self.assertRaises(ValueError):
                coverage.validate_manifest(groups)

    def test_handler_manifest_requires_every_provider_and_status(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/handler")
        self.assertEqual({child for child in group.required_subtests
                          if child.startswith("TestSevenProviderChatFailoverPreservesWireOwnerStickySessionAndBilling/")}, {
            f"TestSevenProviderChatFailoverPreservesWireOwnerStickySessionAndBilling/{provider}/{status}"
            for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
            for status in (503, 429)
        })

    def test_handler_manifest_requires_realtime_route_admission(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/handler")
        self.assertIn("TestGrokRealtimeHandlerRetainsRouteAcrossCredentialLookup", group.tests)
        self.assertEqual({child for child in group.required_subtests
                          if child.startswith("TestGrokRealtimeHandlerRejectsBrokenProxyBeforeCredential/")}, {
            f"TestGrokRealtimeHandlerRejectsBrokenProxyBeforeCredential/{binding}"
            for binding in ("missing", "mismatched", "malformed", "incomplete-auth")
        })

    def test_chat_transport_manifest_requires_every_provider_and_binding(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/repository")
        self.assertEqual({child for child in group.required_subtests
                          if child.startswith("TestSevenProviderChatProductionRejectsBrokenProxyBindingsBeforeSend/")}, {
            f"TestSevenProviderChatProductionRejectsBrokenProxyBindingsBeforeSend/{provider}/{binding}"
            for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
            for binding in ("valid-bound", "unbound-direct", "missing-bound-route",
                            "mismatched-proxy-owner", "malformed-proxy-host", "incomplete-proxy-auth")
        })

    def test_service_manifest_requires_api_key_owners_wallet_inputs_and_scheduling(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/service")
        expected = {
            "TestCommandCodeQuotaProbeKeepsWindowsWhenPeriodEndpointsFail": set(),
            "TestZhipuOAuthBuildAccountMaterialForEachPlan": {
                "start_plan_and_off-peak_are_estate_independent",
            },
            "TestCNAPIKeyObservationRejectsLateOwnerAndPersistenceFailure": {
                f"{probe}/{change}"
                for probe in ("deepseek-payg", "kimi-payg", "kimi-coding", "zhipu-coding")
                for change in ("api-key", "nested-credentials", "proxy-address", "proxy-auth",
                               "proxy-status", "proxy-expiry", "unbound", "storage-failure", "same-owner")
            },
            "TestKimiAPIKeyWalletRejectsUnknownOrInvalidBalance": {
                "missing", "missing-data", "missing-balance", "null", "text", "boolean", "nan",
                "infinity", "overflow", "business-error", "truncated", "missing-status",
                "false-status", "valid-zero", "valid-numeric-string",
            },
            "TestCNAPIKeyWalletSchedulingRejectsChangesAfterObservation": {
                f"{provider}/{balance}/{change}"
                for provider in ("deepseek", "kimi")
                for balance in ("low", "healthy")
                for change in ("api-key", "other-cooldown", "manual-pause", "same-owner")
            },
            "TestCNAPIKeyCodingQuotaPartialSnapshotCannotRefreshOldWindows": {
                "kimi", "zhipu", "minimax",
            },
            "TestCNAPIKeyControlRequiresCompleteBoundedResponse": {
                f"{provider}/{mode}/{scenario}"
                for provider, mode in (("deepseek", "payg"), ("kimi", "payg"), ("kimi", "coding"),
                                       ("zhipu", "coding"), ("minimax", "coding"))
                for scenario in ("complete", "exact-limit", "one-byte-over-limit", "read-failure")
            },
            "TestDeepSeekAPIKeyWalletRejectsIncompleteObservationBeforeScheduling": {
                f"{scenario}/{state}"
                for scenario in ("truncated-json", "text-availability", "numeric-availability", "null-availability",
                                 "mixed-invalid-currency-wallet", "mixed-missing-balance", "mixed-non-object",
                                 "mixed-infinite-balance", "missing-currency", "wrong-type-currency",
                                 "unsupported-currency", "valid-healthy-dual-wallet", "valid-unavailable-zero")
                for state in ("active", "recovering")
            },
            "TestLiveSidebandProductionProxyHandshakeAndFailureNeverDialDirect": {
                "success", "refused", "timeout", "authentication-required", "cancelled", "edited-after-connect",
            },
            "TestZhipuStartPlanMaterialForwardsOfficialBearerWithoutManualScheme": {
                "bigmodel/fresh", "bigmodel/legacy-scheme", "zai/fresh", "zai/legacy-scheme",
            },
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_grok_issuer_failure_manifest_pins_proxy_row_and_same_grant_recovery(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"] if group.package == "./internal/service")
        expected = {
            "TestGrokCredentialIssuerFailureRetainsCapturedProxyRow": {
                f"{failure}/{change}"
                for failure in ("permanent", "transient")
                for change in ("unchanged", "display-metadata", "protocol", "host", "port",
                               "username", "password", "status", "expiry")
            },
            "TestGrokCredentialFailureCASMissRetainsCapturedGrant": {
                f"{failure}/{change}"
                for failure in ("permanent", "transient")
                for change in ("same-grant-access-rotation", "replacement-grant", "subject", "team", "client",
                               "identity", "base", "proxy-auth", "proxy-lifecycle")
            },
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_grok_credential_failure_postgres_manifest_pins_transaction_and_restoration_boundaries(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"] if group.package == "./internal/repository")
        expected = {
            "TestGrokCredentialFailureCASPublishesOnlyCapturedRoute": {
                f"{mode}/{route}" for mode in ("permanent", "transient")
                for route in ("direct", "bound", "proxy_metadata_changed")
            },
            "TestGrokCredentialFailureCASRejectsSameRowProxyChanges": {
                f"{mode}/{change}" for mode in ("permanent", "transient")
                for change in ("host", "port", "username", "password", "protocol", "disabled", "expired", "deleted")
            },
            "TestGrokCredentialFailureCASWaitsForConcurrentOwnerOrProxyEdit": {
                f"{mode}/{writer}" for mode in ("permanent", "transient") for writer in ("account", "proxy")
            },
            "TestGrokCredentialFailureCASOutboxFailureRollsBackState": {"permanent", "transient"},
            "TestGrokCredentialFailureCASCallerTransactionDoesNotPublishBeforeCommit": {
                f"{mode}/{boundary}" for mode in ("permanent", "transient")
                for boundary in ("context_rollback", "managed_rollback", "managed_commit")
            },
            "TestGrokCredentialFailureCASMissingProxyQuarantineRejectsRestoration": {"still_missing", "same_ID_restored"},
            "TestGrokCredentialFailureCASWaitsForConcurrentMissingProxyRestoration": set(),
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_coding_quota_manifest_keeps_success_missing_unknown_and_malformed_cases(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/service")
        test = "TestCNAPIKeyCodingQuotaRejectsUnrecognizedOrMalformedObservation"
        self.assertIn(test, group.tests)
        # Match the independently specified response shapes, including invalid
        # numeric types and missing fields, rather than accepting any N rows.
        expected = {
            'kimi-coding/missing/{}',
            'kimi-coding/unknown/{"unrelated":"metadata"}',
            'kimi-coding/recognized/{"limits":[{"detail":{"limit":100,"remaining":75}}],"usage":{"limit":1000,"remaining":800}}',
            'kimi-coding/recognized/{"limits":[{"detail":{}}]}',
            'kimi-coding/recognized/{"usage":{"limit":100}}',
            'kimi-coding/recognized/{"usage":{"limit":0,"remaining":0}}',
            'kimi-coding/recognized/{"usage":{"limit":"NaN","remaining":0}}',
            'kimi-coding/recognized/{"limits":[{"detail":{"limit":100,"remaining":75}}],"usage":{"limit":1000,"remaining":800}',
            'zhipu-coding/missing/{}',
            'zhipu-coding/unknown/{"unrelated":"metadata"}',
            'zhipu-coding/recognized/{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}],"level":"max"}}',
            'zhipu-coding/recognized/{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3}]}}',
            'zhipu-coding/recognized/{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":"bad","unit":3}]}}',
            'zhipu-coding/recognized/{"success":true,"data":{"limits":[{"type":"TOKENS_LIMIT","percentage":25,"unit":3}],"level":"max"}',
            'minimax-coding/missing/{}',
            'minimax-coding/unknown/{"unrelated":"metadata"}',
            'minimax-coding/recognized/{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":100}]}',
            'minimax-coding/recognized/{"model_remains":[{"model_name":"general"}]}',
            'minimax-coding/recognized/{"model_remains":[{"model_name":"general","current_interval_remaining_percent":"Inf"}]}',
            'minimax-coding/recognized/{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_remaining_percent":100}]',
        }
        self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                         {test + "/" + scenario for scenario in expected})

    def test_repository_manifest_requires_api_key_postgres_owner_and_scheduler_cases(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/repository")
        expected = {
            "TestCNAPIKeyControlServicePersistsOnlyCurrentOwnerInPostgres": {
                f"{probe}/{change}"
                for probe in ("deepseek-payg", "kimi-payg", "kimi-coding", "zhipu-coding", "minimax-coding")
                for change in ("same-owner", "api-key", "nested-credentials")
            },
            "TestCNAPIKeyBalanceScheduleCASRetainsOwnerAndScheduler": {
                f"{provider}/{balance}/{change}"
                for provider in ("deepseek", "kimi")
                for balance in ("low", "healthy")
                for change in ("same-owner", "api-key", "other-cooldown", "manual-pause")
            },
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_api_key_redirect_manifest_requires_every_control_status_and_origin(self) -> None:
        group = next(group for group in coverage.PROFILES["unit"]
                     if group.package == "./internal/repository")
        test = "TestCNAPIKeyControlProductionNeverFollowsRedirects"
        self.assertIn(test, group.tests)
        self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")}, {
            f"{test}/{provider}/{mode}/{status}/{origin}"
            for provider, mode in (("deepseek", "payg"), ("kimi", "payg"), ("kimi", "coding"),
                                   ("zhipu", "coding"), ("minimax", "coding"))
            for status in (200, 301, 302, 307, 308)
            for origin in ("same-origin", "different-origin")
        })

    def test_quota_manifest_requires_final_write_races_storage_and_cache_boundaries(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/repository")
        expected = {
            "TestOpenAIQuotaOwnerRefreshFinalWriteCannotOverwriteExternalRelink": {
                "same-owner-rotation", "external-relink-after-final-validation",
            },
            "TestOpenAIQuotaOwnerTaskFinalWriteCannotOverwriteExternalRelink": {
                "same-owner-task", "external-relink-after-final-validation",
            },
            "TestOpenAIQuotaOwnerTaskCASMissReusesConcurrentSameOwnerTask": set(),
            "TestOpenAIQuotaOwnerPersistenceFailureStopsQuotaAndCache": {
                "refresh/storage", "refresh/outbox", "agent-task/storage", "agent-task/outbox",
            },
            "TestOpenAIQuotaOwnerCASOutboxFailureAndCallerRollbackPublishNothing": {
                "storage-failure", "outbox-failure", "caller-rollback",
            },
            "TestOpenAIQuotaOwnerCASRechecksConcurrentAccountAndProxyWriters": {
                "relink", "same-proxy-route-edit",
            },
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_business_integration_manifest_requires_seven_chat_and_native_providers(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/integration")
        inference_tests = {
            "TestSevenProviderFailoverPersistsRedisAffinityAndPostgresDebits",
            "TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits",
        }
        self.assertTrue(inference_tests.issubset(group.tests))
        self.assertEqual({child for child in group.required_subtests
                          if child.split("/", 1)[0] in inference_tests}, {
            f"TestSevenProviderFailoverPersistsRedisAffinityAndPostgresDebits/{provider}/{status}"
            for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
            for status in (503, 429)
        } | {
            f"TestSevenProviderNativeStreamingToolsPersistRedisAffinityAndPostgresDebits/{provider}"
            for provider in ("openai", "grok", "deepseek", "kimi", "minimax", "stepfun", "zhipu")
        })

    def test_management_manifest_requires_each_platform_mode_race_and_storage_fault(self) -> None:
        group = next(group for group in coverage.PROFILES["integration"]
                     if group.package == "./internal/integration")
        expected = {
            "TestSevenProviderManagementEditsReachOtherInstanceBeforeForwarding": set(coverage.PROVIDERS),
            "TestSevenProviderManagementProxyEditOutboxFailureRollsBackRoute": set(),
            "TestSevenProviderManagementLateProxyProbeCannotReplaceRepairedRoute": {
                "late-success-without-new-probe", "late-success-after-new-success",
                "late-failure-after-new-success", "late-quality-failure-after-new-success",
            },
            "TestSevenProviderManagementCreatedProxyBackgroundProbeVisibleOnOtherInstance": set(),
            "TestSevenProviderExpiredProxyFallbackAndRepairReachOtherInstance": {
                f"{provider}/{mode}" for provider in coverage.PROVIDERS for mode in ("proxy", "direct", "none")
            },
            "TestSevenProviderExpirySweepWaitsForRenewalAndRetainsRenewedBinding": set(),
            "TestSevenProviderExpiryAndRevertOutboxFailuresRollbackSavedBinding": {"expiry", "revert"},
            "TestSevenProviderProxyDeletionRechecksBindingAfterAdminPreflight": set(),
            "TestSevenProviderBindingWritersRejectProxyDeletedByConcurrentInstance": {
                "create", "update", "bulk-update", "revert",
            },
            "TestSevenProviderAccountDeletionOutboxFailurePreservesAccountAndCache": set(),
            "TestSevenProviderAccountDeletionRespectsCallerTransactionRollback": set(),
            "TestSevenProviderAccountDeletionAndRecreationReachOtherInstance": set(coverage.PROVIDERS),
        }
        for test, scenarios in expected.items():
            with self.subTest(test=test):
                self.assertIn(test, group.tests)
                self.assertEqual({child for child in group.required_subtests if child.startswith(test + "/")},
                                 {test + "/" + scenario for scenario in scenarios})

    def test_container_boundary_precedes_file_reads_and_go_execution(self) -> None:
        with mock.patch.object(validation_runtime, "require_in_validation",
                               side_effect=validation_runtime.ValidationRuntimeError("container required")), \
             mock.patch.object(coverage.subprocess, "run") as run:
            with self.assertRaises(validation_runtime.ValidationRuntimeError):
                coverage.run_profile("unit", root=Path("/nonexistent"))
        run.assert_not_called()

    def test_gate_executes_uncached_exact_tests_and_the_requested_build_tag(self) -> None:
        group = coverage.RegressionGroup("./internal/service", (TEST, "TestSecondFlow"), (CHILD,))
        for profile in ("unit", "integration"):
            with self.subTest(profile=profile), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                backend = root / "backend"
                backend.mkdir()
                (backend / "go.mod").write_text("module example.invalid/sub2api\n")
                output = "\n".join(json.dumps(item) for item in successful_events(
                    tests=group.tests, children=(CHILD,)))
                with mock.patch.dict(coverage.PROFILES, {profile: (group,)}), \
                     mock.patch.object(validation_runtime, "require_in_validation") as boundary, \
                     mock.patch.object(coverage.subprocess, "run", return_value=subprocess.CompletedProcess(
                         [], 0, stdout=output)) as run, contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(coverage.run_profile(profile, root=root), 0)
                boundary.assert_called_once()
                self.assertEqual(run.call_args.args[0], [
                    "go", "test", "-json", "-count=1", f"-tags={profile}", "-timeout=5m",
                    "-run", "^(TestRequiredBusinessFlow|TestSecondFlow)$", "./internal/service",
                ])
                self.assertEqual(run.call_args.kwargs["cwd"], backend)
                self.assertEqual(run.call_args.kwargs["stderr"], subprocess.STDOUT)

    def test_zero_exit_without_tests_fails_and_diagnostics_omit_raw_output(self) -> None:
        group = coverage.RegressionGroup("./internal/service", (TEST,))
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "backend").mkdir()
            (root / "backend/go.mod").write_text("module example.invalid/sub2api\n")
            output = json.dumps({**event("output"), "Output": "secret diagnostic"}) + "\n" + json.dumps(event("pass"))
            stderr = io.StringIO()
            with mock.patch.dict(coverage.PROFILES, {"integration": (group,)}), \
                 mock.patch.object(validation_runtime, "require_in_validation"), \
                 mock.patch.object(coverage.subprocess, "run", return_value=subprocess.CompletedProcess(
                     [], 0, stdout=output)), contextlib.redirect_stdout(io.StringIO()), \
                 contextlib.redirect_stderr(stderr):
                self.assertEqual(coverage.run_profile("integration", root=root), 1)
            self.assertIn(TEST + ": missing run", stderr.getvalue())
            self.assertNotIn("secret diagnostic", stderr.getvalue())


if __name__ == "__main__":
    unittest.main()
