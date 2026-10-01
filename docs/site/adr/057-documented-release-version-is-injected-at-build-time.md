---
search:
  boost: 0.3
---

# ADR-057: Documented release version is injected at build time

Documentation names no bb release as a literal. A page the site builds gets the version when the site is built: `docs/main.py` gives mkdocs-macros `bb_version`, bare, for asset file names, and `bb_version_tag`, with its v, for release tags. It takes them from `BB_DOCS_VERSION`, else the newest git tag, else the placeholder X.Y.Z, and `task docs:deploy-version` sets `BB_DOCS_VERSION` to the release being published, so each published build shows the release it was built from. The macros take double square brackets rather than double braces, so the Ansible and Taskfile Jinja the pages show reaches the reader as written.

Every release asset is published twice: under its versioned name, `bb_1.2.3_linux_amd64.tar.gz`, and without the version, `bb_linux_amd64.tar.gz`, which `/releases/latest/download/` serves. The copies are made before the checksum and signing steps, so `sha256sums.txt` lists both names, `sha256sum -c --ignore-missing` verifies whichever was downloaded, and both are signed. Homebrew, WinGet and Scoop use the versioned names.

So instructions that install the newest release name no version: the README and the quickstart use `latest/download` URLs. Instructions that pin a release on purpose, to mirror it into an internal registry, set a Dockerfile `ARG` or verify one named artifact, use the macros. `tools/docs-lint` fails a literal version that is not the newest release, and `task docs:sync-version` rewrites one.

Never write a literal bb release version into documentation. In a page MkDocs builds, use the `bb_version` or `bb_version_tag` macro in double square brackets. In the README, and anything else read outside MkDocs, state no version: point at the releases page or use a `latest/download` URL. To show literal Jinja for another tool, write `{{ ... }}`; it passes through untouched.

A literal goes stale at the next release. A rewrite made by the release workflow reaches the built site but not the repository, because `main` takes no commit from the workflow, so the checked-in docs would fall a release behind and fail docs-lint for the next person to push. With nothing pinned, nothing goes stale. GitHub renders the README without substituting anything, which is why the README names no version at all.

## Not chosen

- **Commit the synchronised docs back to main from the release workflow**: `main` takes changes through pull requests, so the workflow would have to open and merge one against itself, a large change to release automation to keep a mechanism that injection makes unnecessary.
- **Pin literals and drop the check that holds them current**: Nobody is blocked, and the README and the site advertise an old release until someone notices.
- **Publish the version for a snippet to read, as a VERSION.txt asset or from changelog.json**: The reader makes two requests and carries a shell variable, a failed fetch leaves it empty and builds a nonsense URL, and reading changelog.json needs jq or a brittle grep. Aliased assets need no variable at all.
- **Publish only version-less names**: Homebrew, WinGet and Scoop reference the versioned names, and so does anyone pinning a release.
- **The macros plugin's default double-brace delimiters**: They would evaluate the Ansible and Taskfile Jinja the pages show, and silently corrupt working examples.
