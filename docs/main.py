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


# Files mkdocs copies verbatim instead of running through the macro engine.
#
# Only pages are rendered, so a placeholder in a static file is published as its
# own source. llms.txt is the worst place for that to happen: its audience is
# machines, which will not recognise "[[ bb_version_tag ]]" as a mistake the way
# a human skimming a page would.
STATIC_MACRO_FILES = ("llms.txt",)

# The same delimiters mkdocs-macros uses, chosen in ADR-057 so documented Jinja
# reaches the reader unevaluated. Substitution here is a plain name lookup
# rather than a Jinja render: a static file should interpolate variables, not
# execute template logic.
PLACEHOLDER_PATTERN = re.compile(r"\[\[\s*([a-z_][a-z0-9_]*)\s*\]\]")

# Rendered output that is ours rather than the theme's. The bundled search and
# lunr scripts contain "[[" of their own, so the leftover check would trip on
# them forever.
GUARDED_SUFFIXES = frozenset({".html", ".txt", ".json", ".xml", ".md"})


def _render_placeholders(text: str, variables: dict, origin: str) -> str:
    def replace(match: re.Match) -> str:
        name = match.group(1)
        if name not in variables:
            raise ValueError(f"{origin} uses [[ {name} ]], which is not a defined macro variable")
        return str(variables[name])

    return PLACEHOLDER_PATTERN.sub(replace, text)


def on_post_build(env) -> None:
    """Interpolate variables into the files mkdocs copied without rendering.

    Then confirm no placeholder survived anywhere in the output. The check is
    the point: substituting is easy to get right once and easy to forget when
    the next static file is added, and the failure is silent -- a published
    page that shows its own template source.
    """
    site_dir = pathlib.Path(env.conf["site_dir"])

    for name in STATIC_MACRO_FILES:
        path = site_dir / name
        if not path.exists():
            continue
        original = path.read_text(encoding="utf-8")
        rendered = _render_placeholders(original, env.variables, name)
        if rendered != original:
            path.write_text(rendered, encoding="utf-8")

    unrendered = []
    for path in sorted(site_dir.rglob("*")):
        if not path.is_file() or path.suffix.lower() not in GUARDED_SUFFIXES:
            continue
        relative = path.relative_to(site_dir)
        if relative.parts and relative.parts[0] == "assets":
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except (OSError, UnicodeDecodeError):
            continue
        for match in PLACEHOLDER_PATTERN.finditer(text):
            unrendered.append(f"{relative}: {match.group(0)}")

    if unrendered:
        joined = "\n  ".join(unrendered)
        raise ValueError(
            "macro placeholders reached the built site unrendered:\n  "
            + joined
            + "\nAdd the file to STATIC_MACRO_FILES in docs/main.py if mkdocs copies it verbatim."
        )
