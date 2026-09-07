"""Conventional Commit classification, in one place.

Two things read commit messages and have to agree about them: the release
workflow, which decides from them whether a release is cut and how the version
moves, and the release-flow gate, which refuses a breaking change on a pull
request into main. Disagreement between those two fails in the one direction the
gate exists to prevent -- the gate saying "not breaking, allow it" while the
releaser says "breaking, cut a major" -- so they read the same code (ADR-065).

They did not, before this existed. The bump detection asked whether the body
contained the string "BREAKING CHANGE:" anywhere; the changelog matched a footer
anchored to the start of a line. A body mentioning the phrase mid-sentence
therefore cut a major release and was not listed as a breaking change in the
notes it cut. The line-anchored form is kept, because a footer is what the
Conventional Commits specification describes and what the release notes have
always reported.
"""

from __future__ import annotations

import re
from typing import List, NamedTuple, Optional

# The subject line: type, optional (scope), optional ! marker, description.
SUBJECT_PATTERN = re.compile(
    r"^(?P<type>[a-z]+)(?:\((?P<scope>[^)]+)\))?(?P<bang>!)?: (?P<description>.+)$"
)

# Captures the whole footer, not its first line. `(.+)$` stopped at the first
# newline, so a footer wrapped at the usual commit width reached the release
# notes cut off -- five of the six breaking changes accumulated for one release
# ended mid-sentence, one of them mid-word. Since CHANGELOG.md only points at
# these generated notes, that truncation was the migration instructions. Ends at
# a blank line so a footer stays separable from anything following it.
BREAKING_FOOTER_PATTERN = re.compile(
    r"^BREAKING(?:-| )CHANGE:\s*(.+?)(?=\n\s*\n|\Z)",
    re.MULTILINE | re.DOTALL,
)

# Only user-visible change releases. Every valid Conventional Commit type used
# to cut a patch release, which produced 95 releases in six months -- a rate
# adopters running binaries through change approval cannot consume, and which
# left the version number carrying no signal about whether anything changed for
# them. ci/chore/docs/style/refactor/test/build accumulate and ship with the
# next feat or fix.
RELEASING_PATCH_TYPES = frozenset({"fix", "perf", "revert"})

# What `git log --pretty=format:LOG_FORMAT` produces, and what parse_log reads.
# Field and record separators rather than newlines, because a commit body
# contains newlines and a subject can contain almost anything.
LOG_FORMAT = "%H%x1f%s%x1f%b%x1e"
LOG_ARGS = ("log", "--no-merges", f"--pretty=format:{LOG_FORMAT}")

BUMP_NONE = 0
BUMP_PATCH = 1
BUMP_MINOR = 2
BUMP_MAJOR = 3


class Commit(NamedTuple):
    """One commit, as the release machinery understands it."""

    sha: str
    subject: str
    body: str
    # "other" when the subject is not a Conventional Commit; conventional says
    # which it was, because the two are treated differently -- an unconventional
    # subject never contributes to the version but can still carry a breaking
    # footer into the notes.
    type: str
    scope: Optional[str]
    description: str
    breaking: bool
    conventional: bool


def classify(sha: str, subject: str, body: str) -> Commit:
    """Read one commit the way the release workflow reads it."""
    has_breaking_footer = bool(BREAKING_FOOTER_PATTERN.search(body))
    match = SUBJECT_PATTERN.match(subject.strip())

    if match is None:
        return Commit(
            sha=sha,
            subject=subject,
            body=body,
            type="other",
            scope=None,
            description=subject,
            breaking=has_breaking_footer,
            conventional=False,
        )

    return Commit(
        sha=sha,
        subject=subject,
        body=body,
        type=match.group("type"),
        scope=match.group("scope"),
        description=match.group("description"),
        breaking=bool(match.group("bang")) or has_breaking_footer,
        conventional=True,
    )


def parse_log(raw: str) -> List[Commit]:
    """Turn `git log --pretty=format:LOG_FORMAT` output into commits."""
    commits: List[Commit] = []
    for record in raw.split("\x1e"):
        if not record.strip():
            continue
        parts = record.split("\x1f", 2)
        if len(parts) < 2:
            continue
        commits.append(
            classify(
                sha=parts[0].strip(),
                subject=parts[1].strip(),
                body=parts[2] if len(parts) > 2 else "",
            )
        )

    return commits


def bump_level(commits: List[Commit]) -> int:
    """How far the version moves for these commits.

    BUMP_NONE when nothing here releases, which is not the same as "no
    conventional commits": a run of chore and docs commits is well formed and
    still releases nothing.
    """
    level = BUMP_NONE
    for commit in commits:
        if not commit.conventional:
            continue
        if commit.breaking:
            level = max(level, BUMP_MAJOR)
        elif commit.type == "feat":
            level = max(level, BUMP_MINOR)
        elif commit.type in RELEASING_PATCH_TYPES:
            level = max(level, BUMP_PATCH)

    return level


def has_conventional(commits: List[Commit]) -> bool:
    """Whether anything here was a Conventional Commit at all."""
    return any(commit.conventional for commit in commits)
