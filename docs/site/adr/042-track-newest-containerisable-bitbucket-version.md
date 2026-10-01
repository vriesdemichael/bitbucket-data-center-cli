---
search:
  boost: 0.3
---

# ADR-042: Track the newest containerisable Bitbucket version

bb is generated against the newest Bitbucket Data Center release that runs in this project's container stack and passes the live suite, and the harness runs that release. A release the stack cannot boot, or that boots and fails the suite, is not tracked however recent it is. Older releases are served as well (ADR-088).

The release lives in one place: the base image tag in docker/harness/Dockerfile. The harness takes its JVM, its git and the product version it provisions from that image, tools/openapi-spec derives the vendored OpenAPI reference from the tag (ADR-068), and the window of releases served ends with it, which `task quality:bitbucket-releases:verify` checks. Documentation names it only through the bitbucket_version macro in docs/main.py, which renders the tag. No code, output, configuration default or other document states it, and no record names a Bitbucket release in its title or rule; `TestRecordsInForceDoNotNameABitbucketVersion` fails on one.

Dependabot proposes every published release: .github/dependabot.yml watches /docker/harness daily, with no ignore for atlassian/bitbucket. The live suite runs on every pull request, forks included, and CI Complete does not accept a skipped result, so a release the stack cannot run cannot go green. The live suite's result on the Dependabot pull request is the evidence that a release can be adopted; merging it is the decision. dependabot-automerge.yml holds the product image for a person whatever the update type (ADR-069), and a bump on its own fails openapi:verify until the vendored reference is refreshed in the same change.

Do not open a release bump by hand: Dependabot has proposed it already. Editing the tag locally to reproduce or bisect a release-specific bug is fine. Do not add an ignore for atlassian/bitbucket to .github/dependabot.yml; it suppresses the proposal, so nobody learns a release exists and the suite never runs against it. When adopting a release, run `task openapi:refresh` in the same change.

A release stated in more than one place drifts: a copy stops matching what the suite runs, and a reader trusts the copy. Adopting a release changes what bb claims to support, which is why a person merges it.

## Not chosen

- **Pin a supported release in configuration and documentation**: The copies drift at the next upgrade, and a pin claims support that nothing verifies.
- **Merge the product image bump unattended**: It changes a user-facing claim, and a bare bump does not pass openapi:verify.
- **Remove bitbucketVersionTarget from bb auth status**: Scripts read the field. It reports what BITBUCKET_VERSION_TARGET records for an operator's own environment, and bb does not act on it.
