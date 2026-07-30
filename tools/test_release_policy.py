#!/usr/bin/env python3
"""Regression tests for release-policy helpers."""

from __future__ import annotations

import unittest

import check_release
import release_preflight


TAG = "v1.2.3+custom.009"
OFFICIAL_TAG = "v1.2.3"
OFFICIAL_COMMIT = "a" * 40


def valid_notes() -> str:
    return f"""Sub2API Plus {TAG}

## Highlights

One useful change.

## Compatibility and migration

No migration is required.

## Known issues

No known release blockers.

## Upstream baseline

Official release: {OFFICIAL_TAG}
Official commit: {OFFICIAL_COMMIT}
"""


class ReleaseNotesTests(unittest.TestCase):
    def validate(self, notes: str) -> list[str]:
        errors: list[str] = []
        check_release.validate_notes(
            notes,
            TAG,
            OFFICIAL_TAG,
            OFFICIAL_COMMIT,
            errors,
            require_subject=True,
        )
        return errors

    def test_valid_notes_pass(self) -> None:
        self.assertEqual(self.validate(valid_notes()), [])

    def test_wrong_subject_fails(self) -> None:
        notes = valid_notes().replace(
            f"Sub2API Plus {TAG}",
            "Sub2API Plus wrong-version",
            1,
        )
        self.assertTrue(
            any("first non-empty release-notes line" in error for error in self.validate(notes))
        )

    def test_duplicate_required_heading_fails(self) -> None:
        notes = valid_notes() + "\n## Highlights\n\nDuplicated.\n"
        self.assertTrue(
            any("duplicate '## Highlights'" in error for error in self.validate(notes))
        )

    def test_upstream_identifiers_must_be_in_upstream_section(self) -> None:
        notes = valid_notes().replace(
            f"Official release: {OFFICIAL_TAG}\nOfficial commit: {OFFICIAL_COMMIT}",
            "Baseline recorded below.",
        )
        notes = notes.replace(
            "## Upstream baseline",
            f"Official release: {OFFICIAL_TAG}\n"
            f"Official commit: {OFFICIAL_COMMIT}\n\n"
            "## Upstream baseline",
            1,
        )
        errors = self.validate(notes)
        self.assertTrue(any("does not name official release" in error for error in errors))
        self.assertTrue(any("does not name official commit" in error for error in errors))


class ReleaseBaselineTests(unittest.TestCase):
    def test_previous_release_uses_only_eligible_lower_versions(self) -> None:
        tags = [
            "v1.2.3+custom.004",
            "v1.2.3+custom.005",
            "v1.2.3+custom.006",
            "v1.2.3+custom.010",
            "v9.9.9+custom.999",
        ]
        statuses = {
            "v1.2.3+custom.004": "historical",
            "v1.2.3+custom.005": "published",
            "v1.2.3+custom.006": "planned",
            "v1.2.3+custom.010": "published",
        }
        self.assertEqual(
            release_preflight.select_previous_release_tag(tags, statuses, TAG),
            "v1.2.3+custom.005",
        )

    def test_required_status_is_exact(self) -> None:
        errors: list[str] = []
        check_release.validate_required_status(TAG, "published", "planned", errors)
        self.assertEqual(len(errors), 1)
        self.assertIn("expected 'planned'", errors[0])


if __name__ == "__main__":
    unittest.main()
