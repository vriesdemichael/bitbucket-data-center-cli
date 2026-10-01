---
search:
  boost: 0.3
---

# ADR-037: Versioned docs via MkDocs Material and mike

The documentation site is MkDocs with the Material theme, built from `docs/site` and published to GitHub Pages with mike. CI builds it strictly on every pull request (`task docs:validate`), and the release workflow publishes it with every release that is not a prerelease (`task docs:deploy-version`), so the site changes only when a release is cut.

The site is versioned by major. A release deploys to its major, v4.0.1 to v4, replacing that major's previous build, so the version selector holds one entry per major. The entry's title is the full release version, and the full version is also an alias of the major, so a link naming a release resolves to the newest build of its major. The `latest` alias follows the newest major and is the site's default.

Build, serve and deploy through the docs tasks, and do not deploy a release as a mike version of its own. Run the Python tooling with uv against `docs/pyproject.toml`, not an ad-hoc virtualenv.

MkDocs Material is a maintained site generator that asks little upkeep of a repository that is mostly Go, and mike gives versioned docs that follow release tags. Within a major, a newer release's docs describe everything a reader of an older one can use, so one build per major loses nothing a reader needs, while an entry per release grows past anything a reader can pick from.

## Not chosen

- **README-only documentation**: Does not scale to navigable, versioned public docs.
- **A Node-based docs stack**: Another ecosystem's tooling for this repository to carry.
- **Deploy each release as its own version and prune old ones**: Pruning is a second step every release has to remember, and the selector grows back whenever it is forgotten.
- **Deploy to the major without keeping the full version as an alias**: Published schemas take their $id from the full version, and release notes link to it, so each of those addresses would stop resolving at the next release.
