"""Supply the release version to the documentation at build time.

The documented version used to be a literal committed into the markdown and
rewritten by `task docs:sync-version`. Nothing committed the rewrite back to
main, so every release left the checked-in docs a version behind, docs-lint
failed on a clean checkout, and the next person to push anything was blocked by
a release they had nothing to do with.

Nothing is pinned in the sources any more. The version is resolved here, once,
whenever the site is built, so a published snapshot always shows the release it
was built for and no committed file can go stale.
"""

from __future__ import annotations

import os
import pathlib
import re
import subprocess

# The release workflow already knows the version it is publishing and passes it
# through the environment.
VERSION_ENV_VAR = "BB_DOCS_VERSION"

# Shown when no release version can be determined: a local preview, or a CI
# checkout with no tags. Published snapshots always come from the release
# workflow, which sets the variable, so this only ever surfaces in a preview.
# It is deliberately not a plausible version — a reader who sees it should know
# the value is missing rather than trust a wrong one.
FALLBACK_VERSION = "X.Y.Z"

# The Bitbucket version the live suite provisions, read from the stack
# definition rather than typed into the pages that mention it.
#
# ADR-042 keeps that pin in one place so an upgrade is one line with no copies
# to drift. The documentation was a copy: three sample outputs still read
# "expected version 9.4.16" long after CI had moved to 10.4.2, because nothing
# connected the prose to the thing being tested. Reading it here means the next
# upgrade updates the docs by updating the stack.
HARNESS_DOCKERFILE = pathlib.Path(__file__).resolve().parent.parent / "docker" / "harness" / "Dockerfile"
HARNESS_IMAGE_PATTERN = re.compile(r"^FROM\s+atlassian/bitbucket:(\S+)", re.MULTILINE)


def resolve_bitbucket_version() -> str:
    """Version of the Bitbucket image the live harness builds on.

    Falls back to the same placeholder the release version uses: a reader who
    sees it should know the value is missing rather than trust a wrong one.
    """
    try:
        dockerfile = HARNESS_DOCKERFILE.read_text(encoding="utf-8")
    except OSError:
        return FALLBACK_VERSION

    match = HARNESS_IMAGE_PATTERN.search(dockerfile)
    return match.group(1) if match else FALLBACK_VERSION



def _from_environment() -> str | None:
    value = os.environ.get(VERSION_ENV_VAR, "").strip()
    return value or None


def _from_git_tag() -> str | None:
    """Newest release tag, for local builds where nothing set the variable.

    A shallow CI clone has no tags and a source tarball has no repository at
    all, so every failure here is expected and falls through to the placeholder.
    """
    try:
        result = subprocess.run(
            ["git", "tag", "-l", "--sort=-v:refname"],
            capture_output=True,
            text=True,
            check=True,
            timeout=10,
        )
    except (OSError, subprocess.SubprocessError):
        return None

    for line in result.stdout.splitlines():
        tag = line.strip()
        if tag:
            return tag

    return None


def resolve_version() -> str:
    return _from_environment() or _from_git_tag() or FALLBACK_VERSION


def define_env(env) -> None:
    """Register the template variables mkdocs-macros substitutes."""
    version = resolve_version()

    # Two spellings because the snippets need both: release tags carry a v
    # prefix, release asset filenames do not.
    if version == FALLBACK_VERSION:
        bare = tag = FALLBACK_VERSION
    else:
        bare = version.lstrip("v")
        tag = version if version.startswith("v") else f"v{version}"

    env.variables["bb_version"] = bare
    env.variables["bb_version_tag"] = tag
    env.variables["bitbucket_version"] = resolve_bitbucket_version()
