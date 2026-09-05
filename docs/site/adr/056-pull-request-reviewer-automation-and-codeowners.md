# ADR 056: Pull request reviewer automation, default reviewers, and CODEOWNERS

This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.

- Number: `056`
- Title: `Pull request reviewer automation, default reviewers, and CODEOWNERS`
- Category: `architecture`
- Status: `accepted`
- Provenance: `guided-ai`
- Source: `docs/decisions/056-pull-request-reviewer-automation-and-codeowners.yaml`

## Decision

`bb pr create` and `bb pr review reviewer add` automate pull request reviewer assignment to achieve full behavioral parity with the Bitbucket Data Center web interface:
1. Default Reviewers by Default on Creation: `bb pr create` automatically queries repository and
   project default-reviewer conditions for the source and target branch pair and populates matching
   reviewers and expanded reviewer groups (`--default-reviewers` defaults to true). Users can opt out
   via `--no-default-reviewers` or `--default-reviewers=false`.
   Branch names are normalized to fully qualified ref IDs (`refs/heads/...`) and the repository ID is
   resolved before the condition query, because Bitbucket matches condition patterns against ref IDs
   and returns nothing for a bare branch name.

2. Code Owners on Creation: `--codeowners` defaults to true and `--no-codeowners` opts out, and a
   repository that does not use CODEOWNERS is skipped without error. Who the owners are is
   Bitbucket's answer, not bb's -- see ADR-080. This clause and the group selection strategies that
   followed it described bb reading the file, matching the diff and expanding groups itself, which
   assigned reviewers the web interface did not.

3. Attaching Automated Reviewers to Open Pull Requests: `bb pr review reviewer add <id>` provides
   explicit `--default-reviewers` and `--codeowners` flags to evaluate and attach matching reviewers to
   an existing pull request after creation. Reviewers are attached one request at a time, so the command
   reports every reviewer that was added even when a later one fails.

4. Author-Aware Filtering: Pull request authors are strictly excluded from reviewer assignment prior to
   assigning them, preventing Bitbucket 400 rejection.
   The author is identified by the username the server reports for the authenticated session rather than
   the configured username, which may be an email address or differ in case.

5. Failure Is Never Silent: Because both automations are on by default, a lookup that fails for a reason
   other than "not configured" prints a warning on stderr and pull request creation continues; passing
   `--default-reviewers` explicitly makes the same failure fatal. A reviewer group whose membership
   cannot be read is an error, never an empty group and never a username invented from the group name.

## Agent Instructions

When adding pull request reviewer functionality or modifying reviewer commands, preserve the default automatic evaluation of default reviewers and CODEOWNERS on PR creation. Ensure group expansion handles both repository-level and project-level scopes, and strictly filters out the PR author. Never parse CODEOWNERS: ADR-080 says who answers that question. Register flag aliases with a flag set normalization function, never as a second flag bound to the same slice: pflag tracks "has this flag been set" per flag, so a second binding silently discards values supplied under the other spelling.

## Rationale

In the Bitbucket Data Center web interface, opening a pull request automatically pre-fills both configured default reviewers and matching code owners from `.bitbucket/CODEOWNERS`. `POST /pull-requests` evaluates neither: it expects explicit reviewer usernames. So bb resolves them before creating, with opt-out flags, and a pull request opened from the CLI arrives with the reviewers it would have had from the browser. Code owners are resolved by asking Bitbucket rather than by reading the file, which ADR-080 explains.

## Rejected Alternatives

- `Opt-in only CODEOWNERS and default reviewers requiring explicit flags on pr create`: Forces users to remember multiple flags (`--default-reviewers --codeowners`) to match the web UI, leading to unassigned pull requests and broken review workflows when migrating from the browser to the CLI.
- `Defer CODEOWNERS evaluation entirely to server-side third-party marketplace apps`: Bitbucket Data Center 8.14+ natively introduced `.bitbucket/CODEOWNERS` for web UI reviewer suggestions. Relying on marketplace apps introduces external dependencies and fails in environments without those apps installed.
