#!/usr/bin/env python3
"""Focused unit tests for deterministic release-finalization classification."""

from __future__ import annotations

import unittest

import release_finalization


TAG = "v1.2.3+custom.009"
ROW = "| `{tag}` | `v1.2.3` | `{commit}` | {status} |\n"


def upstream(status: str, *, tag: str = TAG, commit: str = "a" * 40) -> str:
    return (
        "| Custom Release | Official Release | Official Commit | Status |\n"
        "| --- | --- | --- | --- |\n"
        + ROW.format(tag=tag, commit=commit, status=status)
    )


class MappingTransitionTests(unittest.TestCase):
    def test_exact_planned_to_published_transition_is_detected(self) -> None:
        self.assertEqual(
            TAG,
            release_finalization.transition_tag(
                upstream("planned"),
                upstream("published"),
            ),
        )

    def test_missing_transition_fails(self) -> None:
        with self.assertRaisesRegex(
            release_finalization.ReleaseFinalizationError,
            "found 0",
        ):
            release_finalization.transition_tag(
                upstream("planned"),
                upstream("planned"),
            )

    def test_multiple_transitions_fail(self) -> None:
        second = "v1.2.3+custom.008"
        base = upstream("planned") + ROW.format(
            tag=second,
            commit="b" * 40,
            status="planned",
        )
        head = upstream("published") + ROW.format(
            tag=second,
            commit="b" * 40,
            status="published",
        )
        with self.assertRaisesRegex(
            release_finalization.ReleaseFinalizationError,
            "found 2",
        ):
            release_finalization.transition_tag(base, head)

    def test_duplicate_mapping_row_fails(self) -> None:
        with self.assertRaisesRegex(
            release_finalization.ReleaseFinalizationError,
            "repeats mapping row",
        ):
            release_finalization.mapping_statuses(
                upstream("planned") + ROW.format(
                    tag=TAG,
                    commit="a" * 40,
                    status="planned",
                )
            )


class BranchTests(unittest.TestCase):
    def test_tag_and_branch_round_trip(self) -> None:
        branch = "release/finalize-1.2.3-custom.009"
        self.assertEqual(branch, release_finalization.finalization_branch(TAG))
        self.assertEqual(TAG, release_finalization.tag_from_branch(branch))

    def test_non_deterministic_branch_is_rejected(self) -> None:
        with self.assertRaisesRegex(
            release_finalization.ReleaseFinalizationError,
            "invalid deterministic",
        ):
            release_finalization.tag_from_branch(
                "release/finalize-v1.2.3-custom.009"
            )


class ReplacementTests(unittest.TestCase):
    def test_only_exact_planned_row_is_replaced(self) -> None:
        updated = release_finalization.replace_planned_mapping(
            upstream("planned"), TAG
        )
        self.assertIn("| published |", updated)

    def test_already_published_row_is_rejected(self) -> None:
        with self.assertRaisesRegex(
            release_finalization.ReleaseFinalizationError,
            "0 planned mapping rows",
        ):
            release_finalization.replace_planned_mapping(
                upstream("published"), TAG
            )


if __name__ == "__main__":
    unittest.main()
