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
