"""Refuse a pull request into main that is not part of the release flow.

main is a release pointer. Everything that lands on it either arrives by the
fast-forward promotion from next, or is a dependency bump or a hotfix -- and a
release workflow that reads conventional commits off main will cut a version
from whatever else appears there. A single `feat!:` merged into main outside the
flow cuts a major release, which is the thing next exists to manage (ADR-066).

The rules, and why each one:

  * A branch outside the allowlist is refused. The flow is only worth having if
    it cannot be left by accident.
  * next is refused too, with its own message. Merging a next pull request would
    use rebase-merge -- the only method main allows -- which replays every
    commit onto main with new shas while next keeps the originals, so the two
    branches end up holding duplicate copies of the same work. That is the exact
    thing the fast-forward promotion avoids, so the promotion is a push and
    never a pull request.
  * A breaking commit is refused wherever it comes from, because a hotfix branch
    is allowed into main and a breaking hotfix would cut a major outside the
    flow just as surely as a feature would.
  * The head must be a branch in this repository. Anyone can name a branch on a
    fork `hotfix/anything`, so matching the name alone would let a fork choose
    its own way in.

The breaking-change reading comes from conventional_commits, which the release
workflow uses too. If this file had its own copy they could disagree, and the
disagreement that matters is this one saying "not breaking, allow it" while the
releaser says "breaking, cut a major" -- the guard failing in the direction it
exists to prevent.
"""

from __future__ import annotations

import argparse
import fnmatch
import subprocess
import sys
from typing import List

import conventional_commits as cc

# Branches that may open a pull request into main. next is deliberately absent:
# it reaches main by fast-forward push, never by pull request.
ALLOWED_HEAD_PATTERNS = ("dependabot/*", "hotfix/*")

PROMOTION_ADVICE = (
    "next reaches main by fast-forward push, not by pull request. Merging this "
    "would replay every commit onto main with new shas while next keeps the "
    "originals, leaving two copies of the same history.\n"
    "  Promote with: task release:promote:check && git push origin next:main"
)

REDIRECT_ADVICE = (
    "Open this against next instead:\n"
    "  gh pr edit <number> --base next\n"
    "Only dependabot/* and hotfix/* may go straight to main, and only when they "
    "carry no breaking change."
)


def commits_in_range(base_sha: str, head_sha: str) -> List[cc.Commit]:
    raw = subprocess.check_output(
        ["git", *cc.LOG_ARGS, f"{base_sha}..{head_sha}"], text=True
    )

    return cc.parse_log(raw)


def refuse(message: str) -> int:
    print(f"::error::{message.splitlines()[0]}")
    print()
    print(message)
    print()

    return 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-ref", required=True, help="branch being merged into")
    parser.add_argument("--head-ref", required=True, help="branch being merged from")
    parser.add_argument("--base-sha", required=True)
    parser.add_argument("--head-sha", required=True)
    parser.add_argument(
        "--head-repo",
        required=True,
        help="owner/name the head branch lives in",
    )
    parser.add_argument(
        "--repo",
        required=True,
        help="owner/name of this repository",
    )
    args = parser.parse_args()

    if args.base_ref != "main":
        print(f"Base is {args.base_ref}, not main; the release flow does not apply.")

        return 0

    if args.head_repo != args.repo:
        return refuse(
            f"A pull request into main must come from a branch in {args.repo}, "
            f"and this one comes from {args.head_repo}.\n\n" + REDIRECT_ADVICE
        )

    if args.head_ref == "next":
        return refuse(PROMOTION_ADVICE)

    allowed = any(
        fnmatch.fnmatch(args.head_ref, pattern) for pattern in ALLOWED_HEAD_PATTERNS
    )
    if not allowed:
        return refuse(
            f"{args.head_ref} may not be merged into main.\n\n" + REDIRECT_ADVICE
        )

    breaking = [
        commit
        for commit in commits_in_range(args.base_sha, args.head_sha)
        if commit.breaking
    ]
    if breaking:
        listed = "\n".join(
            f"  {commit.sha[:7]} {commit.subject}" for commit in breaking
        )

        return refuse(
            f"{args.head_ref} carries a breaking change, which cuts a major "
            f"release:\n{listed}\n\n"
            "A major release goes through next, so that it ships with the rest "
            "of the work meant for it (ADR-066). Open this against next, or drop "
            "the breaking change from the hotfix."
        )

    print(f"{args.head_ref} into main: allowed, and carries no breaking change.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
