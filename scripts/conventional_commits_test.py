"""Tests for the shared Conventional Commit classification.

Standard library unittest, because this is the only Python in the repository
that needs testing and a test runner is a dependency to install, pin and keep
current for one file. Run with `task quality:scripts:test`.

What matters here is the boundary: which commits move the version and which
count as breaking. Both are read by the release workflow, which cuts versions
from them, and by the release-flow gate, which refuses breaking changes on a
pull request into main. A disagreement between the two is the failure the shared
module exists to make impossible, so these cases are the contract.
"""

import unittest

import conventional_commits as cc


def commits(*subjects_and_bodies):
    """Build commits from (subject, body) pairs, or bare subjects."""
    built = []
    for index, item in enumerate(subjects_and_bodies):
        subject, body = item if isinstance(item, tuple) else (item, "")
        built.append(cc.classify(f"{index:040x}", subject, body))

    return built


class ClassifySubject(unittest.TestCase):
    def test_reads_type_scope_and_description(self):
        commit = cc.classify("abc", "feat(auth): add a thing", "")
        self.assertTrue(commit.conventional)
        self.assertEqual(commit.type, "feat")
        self.assertEqual(commit.scope, "auth")
        self.assertEqual(commit.description, "add a thing")
        self.assertFalse(commit.breaking)

    def test_scope_is_optional(self):
        commit = cc.classify("abc", "fix: a thing", "")
        self.assertEqual(commit.type, "fix")
        self.assertIsNone(commit.scope)

    def test_bang_marks_breaking_with_and_without_scope(self):
        self.assertTrue(cc.classify("a", "feat!: a thing", "").breaking)
        self.assertTrue(cc.classify("a", "feat(api)!: a thing", "").breaking)

    def test_unconventional_subject_is_other_and_never_conventional(self):
        commit = cc.classify("abc", "Revert to a project per test", "")
        self.assertFalse(commit.conventional)
        self.assertEqual(commit.type, "other")
        self.assertEqual(commit.description, "Revert to a project per test")


class BreakingFooter(unittest.TestCase):
    def test_both_spellings_count(self):
        self.assertTrue(cc.classify("a", "fix: x", "BREAKING CHANGE: gone").breaking)
        self.assertTrue(cc.classify("a", "fix: x", "BREAKING-CHANGE: gone").breaking)

    def test_footer_may_follow_a_body(self):
        body = "Some prose about the change.\n\nBREAKING CHANGE: the flag is gone"
        self.assertTrue(cc.classify("a", "fix: x", body).breaking)

    def test_mid_line_mention_is_not_a_footer(self):
        # The reason this module exists. Bump detection used to ask whether the
        # body contained the string anywhere, so a sentence mentioning it cut a
        # major release while the changelog -- which matched a footer anchored
        # to a line -- did not list it as breaking. A footer is what the
        # specification describes, and it is what the notes have reported.
        body = "This is deliberately not a BREAKING CHANGE: it only looks like one."
        self.assertFalse(cc.classify("a", "fix: x", body).breaking)

    def test_an_unconventional_subject_still_carries_a_footer(self):
        commit = cc.classify("a", "Merge branch whatever", "BREAKING CHANGE: gone")
        self.assertFalse(commit.conventional)
        self.assertTrue(commit.breaking)


class BumpLevel(unittest.TestCase):
    def test_nothing_releasing_is_none(self):
        self.assertEqual(
            cc.bump_level(commits("chore: x", "docs: y", "test: z", "refactor: w")),
            cc.BUMP_NONE,
        )

    def test_fix_perf_and_revert_are_patch(self):
        for subject in ("fix: x", "perf: x", "revert: x"):
            with self.subTest(subject=subject):
                self.assertEqual(cc.bump_level(commits(subject)), cc.BUMP_PATCH)

    def test_feat_is_minor(self):
        self.assertEqual(cc.bump_level(commits("feat: x")), cc.BUMP_MINOR)

    def test_breaking_is_major_however_it_is_spelled(self):
        self.assertEqual(cc.bump_level(commits("feat!: x")), cc.BUMP_MAJOR)
        self.assertEqual(
            cc.bump_level(commits(("fix: x", "BREAKING CHANGE: gone"))),
            cc.BUMP_MAJOR,
        )

    def test_the_highest_wins(self):
        self.assertEqual(
            cc.bump_level(commits("chore: a", "fix: b", "feat: c")),
            cc.BUMP_MINOR,
        )
        self.assertEqual(
            cc.bump_level(commits("chore: a", "feat: b", "fix!: c")),
            cc.BUMP_MAJOR,
        )

    def test_an_unconventional_subject_does_not_release(self):
        # Not even with a breaking footer: a subject the parser cannot read has
        # no type to release under, and the non-conventional revert commit on
        # the v4 branch is exactly this shape.
        self.assertEqual(
            cc.bump_level(commits(("Revert something", "BREAKING CHANGE: gone"))),
            cc.BUMP_NONE,
        )


class ParseLog(unittest.TestCase):
    def test_reads_the_log_format_it_declares(self):
        raw = (
            "1111111\x1ffeat(x): first\x1f\x1e"
            "2222222\x1ffix: second\x1fBREAKING CHANGE: gone\x1e"
        )
        parsed = cc.parse_log(raw)
        self.assertEqual([c.sha for c in parsed], ["1111111", "2222222"])
        self.assertEqual(parsed[0].type, "feat")
        self.assertTrue(parsed[1].breaking)

    def test_ignores_empty_records(self):
        self.assertEqual(cc.parse_log(""), [])
        self.assertEqual(cc.parse_log("\x1e\x1e"), [])

    def test_has_conventional_distinguishes_none_from_non_releasing(self):
        self.assertFalse(cc.has_conventional(commits("Merge pull request #1")))
        self.assertTrue(cc.has_conventional(commits("chore: x")))


if __name__ == "__main__":
    unittest.main()
