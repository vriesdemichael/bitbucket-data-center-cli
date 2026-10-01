---
search:
  boost: 0.3
---

# ADR-033: Automated Conventional Commit release on main

A push to `main` runs `.github/workflows/release.yml`, which reads the commits since the last release tag and cuts a release when one of them calls for it. A breaking change, marked by `!` or a `BREAKING CHANGE:` footer, cuts a major; `feat` cuts a minor; `fix`, `perf` and `revert` cut a patch. Every other type cuts no release, and its commits ship with the next one that does. That reading lives in `tools/conventionalcommits`, and the release notes and the Release Flow gate read the same package, so none of them can disagree about what breaks.

The workflow builds every platform's archives and Linux packages, with an SBOM for each binary, through `release-artifacts.yml`, which CI also runs on every pull request. It attests each archive's SBOM, tags the commit, writes `sha256sums.txt`, signs the archives, packages, SBOMs and checksum manifest with keyless Sigstore bundles, attests the build provenance, and publishes the GitHub release. The notes are generated from the commits, under a hand-written introduction when `docs/release-notes/<version>.md` exists, with `changelog.json` beside them as an asset. The docs (ADR-037) and the WinGet, Scoop and Homebrew updates follow. A run can be repeated: a tag already on the commit is kept and an existing release is updated, and a version that another run tagged on a different commit stops this one.

Releases run from `refs/heads/main` only, because `bb update` verifies the signed checksum manifest against the identity of this workflow on `main`. Only the jobs that sign or attest may mint that identity's token; the docs and the package-manager updates run in jobs that cannot. A manual run with a version is the override, for a backfill or a release candidate. A version with a suffix is a prerelease: GitHub marks it so, `bb update` does not offer it, and the docs and package managers are left alone.

Type a commit by what an adopter sees. A change no adopter can observe, to tooling, CI, tests or documentation, is `ci`, `chore`, `test`, `refactor` or `docs`, and cuts no release. Do not make those types cut one.

Adopters run binaries through change approval. A release for every change is a rate they cannot absorb, so they pin one version and stop updating, which defeats the security fixes the automation exists to deliver, and a version number that moves for nothing tells them nothing.

## Not chosen

- **Release only by a manual run**: Adds a step after every merge, and release timing comes to depend on someone remembering it.
- **External release tooling, such as release-please**: Harder to debug and less predictable than a workflow this repository owns.
- **Release on every push, whatever the commits say**: Ignores what the commit types say about the version, and releases noise.
- **A patch release for every Conventional Commit type**: Releases changes no adopter can see, at a rate change approval cannot follow, and the version number stops saying that anything changed.
