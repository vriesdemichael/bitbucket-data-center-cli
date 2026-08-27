# ADR 057: Documented release version is injected at build time

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `057`
- Title: `Documented release version is injected at build time`
- Category: `development`
- Status: `accepted`
- Supersedes: `055`
- Provenance: `human`
- Source: `docs/decisions/057-documented-release-version-is-injected-at-build-time.yaml`

## Decision

No source file pins a release version. The documentation site resolves it once, when the site is built, and the README does not state one at all.
1. Build-time Injection: `docs/main.py` supplies `bb_version` and `bb_version_tag` to
   mkdocs-macros, reading `BB_DOCS_VERSION` when set, otherwise the newest git tag, otherwise the
   placeholder `X.Y.Z`. `task docs:deploy-version` passes the release version through that
   variable, so each published snapshot renders the release it was built for.

2. Non-colliding Delimiters: the macros plugin uses double square brackets rather than its default double
   braces, because the documentation shows Ansible and Taskfile snippets containing literal
   Jinja that must reach the reader unevaluated. `enterprise-hardening.md` already carries an
   Ansible `{{ bb_version }}` belonging to the reader's own playbook run.

3. Version-less Asset Aliases: every release publishes each download twice, once as
   `bb_1.2.3_linux_amd64.deb` and once as `bb_linux_amd64.deb`. The second name is reachable at
   `/releases/latest/download/`, so an install snippet needs no version at all. The aliases are
   created before the checksum step, so `sha256sums.txt` lists both names and
   `sha256sum -c --ignore-missing` verifies whichever was downloaded; the signing step's globs
   cover them too. The versioned names are unchanged, because Homebrew, AUR, WinGet and Scoop all
   reference them.

4. Instructions That Install the Newest Release Name No Version: the README and the quickstart use
   the `latest/download` URLs. The README is rendered by GitHub, so no substitution can reach it,
   and it now needs none.

5. Instructions That Pin Deliberately Keep the Version: the enterprise hardening and threat model
   pages mirror into internal registries, pin a Dockerfile ARG, pass `--version` to WinGet and
   verify one named artifact. Pinning is the point there, so those keep the build-time macro, which
   renders a concrete current release as a worked example.

6. Static Validation Is Retained: `tools/docs-lint` still fails any literal version older than the
   newest tag, and `task docs:sync-version` still rewrites one. Nothing currently pins a version,
   so both are guards against a future hardcoding rather than part of the release path.

## Agent Instructions

Never write a literal release version into documentation. In pages built by MkDocs use the bb_version macro (bare, for asset filenames) or bb_version_tag (v-prefixed, for release tags), in double square brackets. In the README, and anywhere else rendered outside MkDocs, do not state a version at all -- point the reader at the releases page. When a documentation snippet must show literal Jinja for another tool, write it as `{{ ... }}` and it will pass through untouched.

## Rationale

ADR-055 pinned literals and kept them current with `task docs:sync-version`, run by the release workflow before publishing. The rewrite reached the built site but never reached the repository: `mike deploy` pushes the built output to gh-pages and the modified markdown was discarded.
So every release left the checked-in documentation a version behind, `docs-lint` failed on a clean checkout of main, and the next contributor's push was blocked by a release they had nothing to do with. This happened twice within a single afternoon, across v2.12.0 and v2.13.0.
Committing the rewrite back would fix it, but main is protected and rebase-only, so the release workflow would have to open a pull request against itself -- a CI overhaul out of proportion to the problem. Injecting at build time removes the class of failure instead: with nothing pinned, there is nothing to go stale, and the release workflow already knows the version to inject.
ADR-055 rejected this approach because template tags are not evaluated when markdown renders on GitHub. That objection was correct and is answered rather than ignored: the only file GitHub renders directly, the README, now states no version at all.

## Rejected Alternatives

- `Commit the synchronized markdown back to main from the release workflow`: main is protected and rebase-only, so the workflow would have to raise and merge a pull request against itself. That is a substantial change to release automation to keep a mechanism whose only job is preventing staleness that build-time injection makes impossible.
- `Keep ADR-055 and move the version check out of the pre-push and quality gates`: Stops contributors being blocked but leaves the published README and site advertising an old release until someone notices, which is the failure ADR-055 existed to prevent.
- `Publish a VERSION.txt asset for the README to read`: Its filename carries no version, so /releases/latest/download/VERSION.txt would resolve and a snippet could read the current release from it. Rejected because it still leaves the reader making two requests and carrying a shell variable, and a failed fetch leaves that variable empty and builds a nonsense URL. Aliasing the assets removes the variable entirely.
- `Read the version from the existing changelog.json asset`: It is already published at a version-less path and its first field is the version, so no new asset would be needed. Rejected for the same reason as VERSION.txt, and because parsing it in a copy-paste snippet needs jq, which is not reliably present, or a brittle grep of JSON.
- `Publish only version-less names and drop the versioned ones`: Homebrew, AUR, WinGet and Scoop all reference the versioned filenames, and anyone pinning a release depends on them. Publishing both costs duplicate assets and nothing else.
- `Use the macros plugin with its default double-brace delimiters`: The documentation shows Ansible and Taskfile snippets whose literal Jinja must survive to the reader. Default delimiters would evaluate them and silently corrupt working examples.
